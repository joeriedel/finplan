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

func prepare(base *model.Record, projection string, target model.AssetID, horizon model.Date) (*run, error) {
	rec, err := base.For(projection)
	if err != nil {
		return nil, err
	}
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
	var growth []model.Setting[rateLayer]
	// A projection setting reaches the accounts under its target, whether or not the line's asset contains the target.
	targets := map[model.AssetID]*model.Resolution{}
	resolveTarget := func(id model.AssetID) error {
		if _, ok := targets[id]; ok {
			return nil
		}
		t, err := rec.Resolve(id)
		if err != nil {
			return err
		}
		targets[id] = t
		return nil
	}
	infl := rec.Globals[model.PropInflation]
	items := slices.Clone(rec.Budget.Items)
	amounts := map[string]model.Schedule{}
	growths := map[string]model.Schedule{}
	acctDefaults := map[model.AccountID][]model.Modification{}
	assetDefaults := map[model.AssetID][]model.Modification{}
	for _, p := range chain {
		if p.Start != nil {
			projStart = *p.Start
		}
		if p.OverrideHistory != nil {
			override = *p.OverrideHistory
		}
		for _, s := range p.AssetSettings {
			if err := resolveTarget(s.Asset); err != nil {
				return nil, err
			}
			if s.Start != nil {
				starts = append(starts, model.Setting[model.Date]{Asset: s.Asset, Value: *s.Start})
			}
			if s.OverrideHistory != nil {
				overrides = append(overrides, model.Setting[bool]{Asset: s.Asset, Value: *s.OverrideHistory})
			}
		}
		for i, m := range p.Mods {
			skip := func(reason string) {
				r.diag.Skipped = append(r.diag.Skipped, Skipped{Projection: p.Name, Index: i, Target: m.Target, Property: m.Property, Reason: reason})
			}
			itemIdx := func(id string) int {
				return slices.IndexFunc(items, func(it model.BudgetItem) bool { return it.ID == id })
			}
			itemExists := func(id string) bool { return itemIdx(id) >= 0 }
			switch m.Target.Kind {
			case model.KindBudgetItem:
				switch {
				case m.Property == model.PropAmount && itemExists(m.Target.ID):
					beneath, ok := amounts[m.Target.ID]
					if !ok {
						beneath = items[itemIdx(m.Target.ID)].Amount
					}
					amounts[m.Target.ID] = m.Over(beneath)
				case m.Property == model.PropGrowth && itemExists(m.Target.ID):
					beneath, ok := growths[m.Target.ID]
					if !ok {
						beneath = items[itemIdx(m.Target.ID)].Growth
					}
					growths[m.Target.ID] = m.Over(beneath)
				case m.Property == model.PropAmount || m.Property == model.PropGrowth:
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
					if err := resolveTarget(asset); err != nil {
						return nil, err
					}
					growth = append(growth, model.Setting[rateLayer]{Asset: asset, Value: rateLayer{m.Values, m.IsScale()}})
				case m.Property == model.PropDefault:
					// An account's default asset shares the account's default.
					acct, isAcct := model.AccountID(m.Target.ID), m.Target.Kind == model.KindAccount
					if !isAcct {
						acct, isAcct = model.AccountOfDefaultAsset(asset)
					}
					if isAcct {
						acctDefaults[acct] = append(acctDefaults[acct], m)
					} else {
						assetDefaults[asset] = append(assetDefaults[asset], m)
					}
				default:
					skip("unsupported property")
				}
			case model.KindGlobal:
				if m.Property == model.PropInflation {
					infl = m.Over(infl)
				} else {
					skip("unsupported property")
				}
			default:
				skip("unsupported object kind")
			}
		}
	}

	res.SetAssetDefaultMods(assetDefaults)

	// Settings apply in order, the last applied winning: asset definitions (innermost asset first), then the projection chain root first, each projection's settings in list order.
	newPath := func(id model.AccountID) *path {
		acct := rec.Account(id)
		underTarget := func(asset model.AssetID) bool { return targets[asset].Has(id) }
		p := &path{acct: acct, def: model.ModifiedDefault{Base: acct.Default, Mods: acctDefaults[id], Opens: acct.Opens}}
		start, _, _ := model.Apply(projStart, starts, underTarget, model.Replace)
		flowsAfter := start
		if acct.Opens != nil {
			start = max(start, *acct.Opens)
			flowsAfter = max(flowsAfter, acct.Opens.Add(-1))
		}
		ov, _, _ := model.Apply(override, overrides, underTarget, model.Replace)
		p.info = AccountStart{Account: id, OverrideHistory: ov, ProjectionStart: start}
		p.start = start
		if sp, ok := acct.Span(); ok {
			p.info.HasActual, p.info.LastActual = true, sp.To
			p.start = sp.To
			if ov {
				p.start = min(sp.To, start)
			}
		}
		p.flowsAfter = p.start
		if acct.Opens != nil {
			p.start = *acct.Opens
			p.flowsAfter = flowsAfter
		}
		p.info.Start = p.start
		p.startVal = p.readBack(p.start)
		if acct.Opens != nil && flowsAfter < *acct.Opens {
			// The path starts the day before the account opens, so the opening balance is a change like any other on its date.
			p.start = acct.Opens.Add(-1)
			p.startVal = p.readBack(p.start)
			p.changes = append(p.changes, change{date: *acct.Opens, cause: CauseOpening, amount: p.def.At(*acct.Opens) - p.startVal, rank: rankOpening})
		}
		_, rate := res.GrowthLayers(id)
		layered, _, _ := model.Apply(rateLayer{s: rate}, growth, underTarget, rateLayer.over)
		p.rate = layered.s
		return p
	}
	for _, id := range res.Accounts {
		r.paths[id] = newPath(id)
	}
	// Every side of an item that touches a hypothetical account counts only the occurrences after the latest cutoff of the accounts it touches, wherever they sit relative to the target, so both sides of a transfer agree.
	cutoffs := map[string]model.Date{}
	outside := map[model.AccountID]*path{}
	for _, it := range items {
		cut := model.MinDate
		hypothetical := false
		for _, id := range []model.AccountID{it.Account, it.From, it.To} {
			a := rec.Account(id)
			if a == nil {
				continue
			}
			hypothetical = hypothetical || a.Opens != nil
			p := r.paths[id]
			if p == nil {
				if outside[id] == nil {
					outside[id] = newPath(id)
				}
				p = outside[id]
			}
			cut = max(cut, p.flowsAfter)
		}
		if hypothetical {
			cutoffs[it.ID] = cut
		}
	}
	for _, id := range res.Accounts {
		r.paths[id].build(rec, items, cutoffs, amounts, growths, infl, horizon)
	}
	return r, nil
}

// rateLayer is a growth schedule, or a modification's values that scale one.
type rateLayer struct {
	s     model.Schedule
	scale bool
}

// over applies the layer v on top of prior.
func (prior rateLayer) over(v rateLayer) rateLayer {
	if v.scale {
		return rateLayer{s: prior.s.Scale(v.s)}
	}
	return rateLayer{s: prior.s.Overlay(v.s)}
}

// raiseFactor is the factor by which raises have lifted an item's amount on d. The item's raise anniversaries are the yearly repeats of its start (or, with none, its recurrence anchor or today); each one after both that start and today, up to d, steps the amount up by the rate g gives on that day, never below zero. A 29 February anniversary falls on 28 February in other years.
func raiseFactor(it model.BudgetItem, today, d model.Date, g model.Schedule) float64 {
	anchor := today
	switch {
	case it.Start != nil:
		anchor = *it.Start
	case it.Recurrence.Anchor != 0:
		anchor = it.Recurrence.Anchor
	}
	from := max(anchor, today)
	ay, month, day := anchor.YMD()
	fy, _, _ := from.YMD()
	dy, _, _ := d.YMD()
	f := 1.0
	for y := max(ay+1, fy); y <= dy; y++ {
		a := model.D(y, month, day)
		if _, m, _ := a.YMD(); m != month {
			a = model.D(y, month, 28)
		}
		if a <= from {
			continue
		}
		if a > d {
			break
		}
		if v, ok := g.At(a); ok {
			f *= max(1+v, 0)
		}
	}
	return f
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
	acct  *model.Account
	def   model.ModifiedDefault // the account's default balance under the projection
	info  AccountStart
	start model.Date
	// flowsAfter is the date budget flows are taken strictly after: start, except for a hypothetical account, which opens on a date that its flows count on and grows from before the projection starts.
	flowsAfter model.Date
	startVal   float64
	rate       model.Schedule
	changes    []change // flows after start, ordered by date then rank
	points     []point  // balance after each date with a flow, from start
}

// Order of flows on one date (docs/design.md, "The engine"): income, expenses, then transfers
// (debt payments, contributions, withdrawals).
const (
	rankOpening = iota
	rankIncome
	rankExpense
	rankTransfer
)

func (p *path) readBack(d model.Date) float64 {
	if v, ok := p.acct.ActualAt(d); ok {
		return v
	}
	return p.def.At(d)
}

func (p *path) data(d model.Date) model.AccountData {
	if p.acct.ClosedOn(d) {
		return model.AccountData{Kind: model.DataActual}
	}
	if d > p.start {
		return model.AccountData{Amount: p.valueAt(d), Kind: model.DataProjected}
	}
	if v, ok := p.acct.ActualAt(d); ok {
		return model.AccountData{Amount: v, Kind: model.DataActual}
	}
	return model.AccountData{Amount: p.def.At(d), Kind: model.DataNone}
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

func (p *path) build(rec *model.Record, items []model.BudgetItem, cutoffs map[string]model.Date, amounts, growths map[string]model.Schedule, infl model.Schedule, horizon model.Date) {
	p.points = []point{{date: p.start, bal: p.startVal}}
	if horizon <= p.start {
		return
	}
	for _, it := range items {
		if !it.Touches(p.acct.ID) {
			continue
		}
		after, upTo := p.flowsAfter, horizon
		// An item touching a hypothetical account is modelled on every side only after the latest cutoff of the accounts it touches.
		if c, ok := cutoffs[it.ID]; ok {
			after = c
		}
		if it.Start != nil {
			after = max(after, it.Start.Add(-1))
		}
		if it.End != nil {
			upTo = min(upTo, *it.End)
		}
		// An item stops on both sides after the close date of any closed account it touches, so a transfer neither drains an account into a closed one nor creates money out of it.
		for _, id := range []model.AccountID{it.Account, it.From, it.To} {
			if a := rec.Account(id); a != nil && a.Closed != nil {
				upTo = min(upTo, *a.Closed)
			}
		}
		raises, ok := growths[it.ID]
		if !ok {
			raises = it.Growth
		}
		pays, ok := amounts[it.ID]
		if !ok {
			pays = it.Amount
		}
		for _, d := range it.Dates(after, upTo) {
			amt, ok := pays.At(d)
			if !ok {
				continue
			}
			amt *= raiseFactor(it, rec.Today, d, raises)
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

// AllStarts returns the resolved start of every account the projection sees, its own and inherited hypothetical accounts included, in the order the projection's view lists them.
func AllStarts(rec *model.Record, projection string) ([]AccountStart, error) {
	view, err := rec.For(projection)
	if err != nil {
		return nil, err
	}
	var out []AccountStart
	for _, a := range view.Accounts {
		starts, err := Starts(rec, projection, model.DefaultAssetID(a.ID))
		if err != nil {
			return nil, err
		}
		out = append(out, starts...)
	}
	return out, nil
}
