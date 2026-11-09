// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package engine is finplan's placeholder projection engine: a pure function of the record (including
// the base budget) and a projection with its parent chain. It reads no clock, keeps no globals and uses
// no randomness, and every output is in a deterministic order.
//
// Each account is read back from real data up to its own start and extrapolated from there by
// applying budget flows on their calendar dates and compounding growth over the exact time elapsed.
// An asset's value is the sum of its accounts, resolved by the model's asset rules. Taxes, growth
// and the lack of market classes are placeholders.
package engine

import (
	"slices"
	"sort"

	"github.com/joeriedel/finplan/internal/model"
)

// Placeholder tax rates; real tax rules are future work.
const (
	// PlaceholderIncomeTaxRate is withheld from every income flow.
	PlaceholderIncomeTaxRate = 0.25
	// PlaceholderWithdrawalTaxRate is charged, on top of the amount, to the tax-deferred account that
	// a withdrawal leaves.
	PlaceholderWithdrawalTaxRate = 0.32
)

// Status says how a sampled value was obtained.
type Status int

// Statuses. StatusDefault means no account under the asset had data, so only defaults were used.
const (
	StatusDefault Status = iota
	StatusActual
	StatusProjected
	StatusPartlyProjected // an aggregate where only some accounts are projected
)

func (s Status) String() string {
	return [...]string{"default", "actual", "projected", "partly projected"}[s]
}

// Sample is the asset's value at one date.
type Sample struct {
	Date   model.Date
	Amount float64
	Status Status
}

// Skipped reports a modification that was not applied.
type Skipped struct {
	Projection string
	Index      int // position in the projection's Mods
	Target     model.ObjectRef
	Property   string
	Reason     string
}

// Diagnostics are the problems found while answering a query. None of them stops the answer.
type Diagnostics struct {
	// Skipped modifications: target missing or property unsupported.
	Skipped []Skipped
	// Conflicts among sibling settings, per account.
	Conflicts []model.Conflict
	// Overlaps: members of assets under the target that were left out of the sums.
	Overlaps []model.Overlap
}

// BalanceResult answers Balances.
type BalanceResult struct {
	Target  model.AssetID
	Samples []Sample // in the order of the requested times
	Diagnostics
}

// AccountStart reports where an account switches from real data to extrapolation, and why.
type AccountStart struct {
	Account         model.AccountID
	Start           model.Date
	LastActual      model.Date
	HasActual       bool
	OverrideHistory bool
	ProjectionStart model.Date
}

// Balances returns the target asset's value at each time under the projection, and "" for reality
// (the record and base budget with no modifications). Extrapolation runs as far as the latest time asked.
func Balances(rec *model.Record, projection string, target model.AssetID, times []model.Date) (*BalanceResult, error) {
	horizon := model.MinDate
	for _, t := range times {
		horizon = max(horizon, t)
	}
	r, err := prepare(rec, projection, target, horizon)
	if err != nil {
		return nil, err
	}
	out := &BalanceResult{Target: target, Diagnostics: r.diag}
	for _, t := range times {
		e := r.eval(t)
		out.Samples = append(out.Samples, Sample{Date: t, Amount: e.Amount, Status: status(e)})
	}
	return out, nil
}

// Starts returns, per account under the target in record order, the resolved start of extrapolation.
func Starts(rec *model.Record, projection string, target model.AssetID) ([]AccountStart, error) {
	r, err := prepare(rec, projection, target, model.MinDate)
	if err != nil {
		return nil, err
	}
	var out []AccountStart
	for _, id := range r.res.Accounts {
		out = append(out, r.paths[id].info)
	}
	return out, nil
}

func status(e model.Evaluation) Status {
	switch {
	case e.Actual == 0 && e.Projected == 0:
		return StatusDefault
	case e.Projected == 0:
		return StatusActual
	case e.Actual == 0:
		return StatusProjected
	}
	return StatusPartlyProjected
}

// run is one query's prepared state.
type run struct {
	rec   *model.Record
	res   *model.Resolution
	paths map[model.AccountID]*path
	diag  Diagnostics
}

func (r *run) eval(d model.Date) model.Evaluation {
	return r.res.Eval(d, func(id model.AccountID) model.AccountData { return r.paths[id].data(d) })
}

func prepare(rec *model.Record, projection string, target model.AssetID, horizon model.Date) (*run, error) {
	res, err := rec.Resolve(target)
	if err != nil {
		return nil, err
	}
	var chain []*model.Projection
	if projection != "" {
		if chain, err = rec.Chain(projection); err != nil {
			return nil, err
		}
	}
	r := &run{rec: rec, res: res, paths: map[model.AccountID]*path{}}
	r.diag.Overlaps = res.Overlaps

	projStart := rec.Today
	override := false
	var starts []model.Setting[model.Date]
	var overrides []model.Setting[bool]
	var growth []model.Setting[model.Schedule]
	infl := rec.Globals[model.PropInflation]
	items := slices.Clone(rec.Budget.Items)
	amounts := map[string]model.Schedule{}
	for _, p := range chain {
		if p.Start != nil {
			projStart = *p.Start
		}
		if p.OverrideHistory != nil {
			override = *p.OverrideHistory
		}
		for _, s := range p.AssetSettings {
			if s.Start != nil {
				starts = append(starts, model.Setting[model.Date]{Asset: s.Asset, Value: *s.Start, Mode: s.Mode})
			}
			if s.OverrideHistory != nil {
				overrides = append(overrides, model.Setting[bool]{Asset: s.Asset, Value: *s.OverrideHistory, Mode: s.Mode})
			}
		}
		for i, m := range p.Mods {
			skip := func(reason string) {
				r.diag.Skipped = append(r.diag.Skipped, Skipped{Projection: p.Name, Index: i, Target: m.Target, Property: m.Property, Reason: reason})
			}
			itemExists := func(id string) bool {
				return slices.ContainsFunc(items, func(it model.BudgetItem) bool { return it.ID == id })
			}
			switch m.Target.Kind {
			case model.KindBudgetItem:
				switch {
				case m.Property == model.PropAmount && itemExists(m.Target.ID):
					amounts[m.Target.ID] = m.Values
				case m.Property == model.PropAmount:
					skip("budget item not found")
				case m.Property == model.PropAddItem && m.Item == nil:
					skip("no item to add")
				case m.Property == model.PropAddItem && itemExists(m.Item.ID):
					skip("budget item id already used")
				case m.Property == model.PropAddItem:
					items = append(items, *m.Item)
				default:
					skip("unsupported property")
				}
			case model.KindAccount, model.KindAsset:
				asset := model.AssetID(m.Target.ID)
				found := rec.Asset(asset) != nil
				if m.Target.Kind == model.KindAccount {
					asset = model.DefaultAssetID(model.AccountID(m.Target.ID))
					found = rec.Account(model.AccountID(m.Target.ID)) != nil
				}
				switch {
				case !found:
					skip("target not found")
				case m.Property == model.PropGrowth:
					growth = append(growth, model.Setting[model.Schedule]{Asset: asset, Value: m.Values, Mode: m.Mode})
				default:
					skip("unsupported property")
				}
			case model.KindGlobal:
				if m.Property == model.PropInflation {
					infl = m.Values
				} else {
					skip("unsupported property")
				}
			default:
				skip("unsupported object kind")
			}
		}
	}

	// Asset definitions keep outer-wins (a wrapper's Params beat what it wraps), which is what
	// ModeOverride gives.
	var assetGrowth []model.Setting[model.Schedule]
	scope := slices.Clone(res.Scope())
	for _, id := range res.Accounts {
		scope = append(scope, model.DefaultAssetID(id))
	}
	for _, id := range scope {
		if s, ok := rec.Asset(id).Params[model.PropGrowth]; ok {
			assetGrowth = append(assetGrowth, model.Setting[model.Schedule]{Asset: id, Value: s, Mode: model.ModeOverride})
		}
	}

	for _, id := range res.Accounts {
		acct := rec.Account(id)
		p := &path{acct: acct}
		note := func(c *model.Conflict) {
			if c != nil {
				r.diag.Conflicts = append(r.diag.Conflicts, *c)
			}
		}
		start, ov := projStart, override
		x := model.Precedence(res, id, "start", starts)
		note(x.Conflict)
		if x.Found {
			start = x.Value
		}
		y := model.Precedence(res, id, "override_history", overrides)
		note(y.Conflict)
		if y.Found {
			ov = y.Value
		}
		p.info = AccountStart{Account: id, OverrideHistory: ov, ProjectionStart: start}
		p.start = start
		if sp, ok := acct.Span(); ok {
			p.info.HasActual, p.info.LastActual = true, sp.To
			p.start = sp.To
			if ov {
				p.start = min(sp.To, start)
			}
		}
		p.info.Start = p.start
		p.startVal = p.readBack(p.start)
		g := model.Precedence(res, id, model.PropGrowth, growth, assetGrowth)
		note(g.Conflict)
		p.rate = model.Constant(acct.StatedRate)
		if g.Found {
			p.rate = g.Value
		}
		p.build(rec, items, amounts, infl, horizon)
		r.paths[id] = p
	}
	return r, nil
}

// change is one recorded change to an account's balance, with its cause.
type change struct {
	date    model.Date
	cause   Cause
	amount  float64
	counter model.AccountID // the other account of a transfer, withdrawal or contribution
	rank    int             // order among changes on one date
}

type point struct {
	date      model.Date
	bal       float64
	cumGrowth float64
}

// path is one account's balance: real data up to start, then extrapolation.
type path struct {
	acct     *model.Account
	info     AccountStart
	start    model.Date
	startVal float64
	rate     model.Schedule
	changes  []change // flows after start, ordered by date then rank
	points   []point  // balance after each date with a flow, from start
}

// Order of flows on one date (docs/design.md, "The engine"): income, expenses, then transfers
// (debt payments, contributions, withdrawals).
const (
	rankIncome = iota
	rankExpense
	rankTransfer
)

func (p *path) readBack(d model.Date) float64 {
	if v, ok := p.acct.ActualAt(d); ok {
		return v
	}
	return p.acct.Default.At(d)
}

func (p *path) data(d model.Date) model.AccountData {
	if d > p.start {
		return model.AccountData{Amount: p.valueAt(d), Kind: model.DataProjected}
	}
	if v, ok := p.acct.ActualAt(d); ok {
		return model.AccountData{Amount: v, Kind: model.DataActual}
	}
	return model.AccountData{Amount: p.acct.Default.At(d), Kind: model.DataNone}
}

func (p *path) pointAt(d model.Date) point {
	i := sort.Search(len(p.points), func(i int) bool { return p.points[i].date > d })
	return p.points[i-1]
}

// valueAt is the extrapolated balance on d, which is after start.
func (p *path) valueAt(d model.Date) float64 {
	pt := p.pointAt(d)
	return pt.bal * p.rate.Compound(pt.date, d)
}

// growthAt is the growth accumulated from start to d.
func (p *path) growthAt(d model.Date) float64 {
	if d <= p.start {
		return 0
	}
	pt := p.pointAt(d)
	return pt.cumGrowth + pt.bal*(p.rate.Compound(pt.date, d)-1)
}

func (p *path) build(rec *model.Record, items []model.BudgetItem, amounts map[string]model.Schedule, infl model.Schedule, horizon model.Date) {
	p.points = []point{{date: p.start, bal: p.startVal}}
	if horizon <= p.start {
		return
	}
	for _, it := range items {
		if !it.Touches(p.acct.ID) {
			continue
		}
		after, upTo := p.start, horizon
		if it.Start != nil {
			after = max(after, it.Start.Add(-1))
		}
		if it.End != nil {
			upTo = min(upTo, *it.End)
		}
		for _, d := range it.Recurrence.Between(after, upTo) {
			amt := it.Amount
			if s, ok := amounts[it.ID]; ok {
				if v, ok := s.At(d); ok {
					amt = v
				}
			}
			if it.Indexed {
				amt *= infl.Compound(rec.Today, d)
			}
			p.flows(rec, it, d, amt)
		}
	}
	sort.SliceStable(p.changes, func(i, j int) bool {
		a, b := p.changes[i], p.changes[j]
		if a.date != b.date {
			return a.date < b.date
		}
		return a.rank < b.rank
	})
	bal, cum, cur := p.startVal, 0.0, p.start
	for i := 0; i < len(p.changes); {
		d := p.changes[i].date
		g := bal*p.rate.Compound(cur, d) - bal
		bal += g
		cum += g
		cur = d
		for ; i < len(p.changes) && p.changes[i].date == d; i++ {
			bal += p.changes[i].amount
		}
		p.points = append(p.points, point{date: d, bal: bal, cumGrowth: cum})
	}
}

func (p *path) flows(rec *model.Record, it model.BudgetItem, d model.Date, amt float64) {
	add := func(rank int, cause Cause, amount float64, counter model.AccountID) {
		p.changes = append(p.changes, change{date: d, cause: cause, amount: amount, counter: counter, rank: rank})
	}
	switch it.Kind {
	case model.ItemIncome:
		add(rankIncome, CauseIncome, amt, "")
		add(rankIncome, CauseTax, -amt*PlaceholderIncomeTaxRate, "")
	case model.ItemExpense:
		add(rankExpense, CauseExpense, -amt, "")
	case model.ItemTransfer:
		cause := transferCause(rec, it.From, it.To)
		if it.From == p.acct.ID {
			add(rankTransfer, cause, -amt, it.To)
			if cause == CauseWithdrawal && p.acct.Tax == model.TaxDeferred {
				add(rankTransfer, CauseTax, -amt*PlaceholderWithdrawalTaxRate, "")
			}
		}
		if it.To == p.acct.ID {
			add(rankTransfer, cause, amt, it.From)
		}
	}
}

// transferCause classifies money moving from src to dst: a withdrawal out of a tax-advantaged account
// to one that is not, a contribution into one, otherwise a plain transfer. Accounts absent from the
// record count as unsheltered.
func transferCause(rec *model.Record, src, dst model.AccountID) Cause {
	sheltered := func(id model.AccountID) bool {
		a := rec.Account(id)
		return a != nil && a.Tax.Sheltered()
	}
	switch {
	case sheltered(src) && !sheltered(dst):
		return CauseWithdrawal
	case !sheltered(src) && sheltered(dst):
		return CauseContribution
	}
	return CauseTransfer
}
