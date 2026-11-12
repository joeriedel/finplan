// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package engine is finplan's placeholder projection engine: a pure function of the record (including
// the base budget) and a projection with its parent chain. It reads no clock, keeps no globals and uses
// no randomness, and every output is in a deterministic order.
//
// Each account is read back from real data up to its own start and extrapolated from there by
// applying budget flows on their calendar dates and compounding growth over the exact time elapsed.
// An asset's value is the sum of its accounts, resolved by the model's asset rules. Growth is a placeholder: an account with an allocation grows by market class, any other at its own rate; taxes follow the rules package (see taxes.go).
package engine

import (
	"math"
	"slices"
	"sort"
	"time"

	"github.com/joeriedel/finplan/internal/model"
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
	// Notes are problems with the tax calculation: a year the rules cannot compute, or tax that cannot be reconciled for want of a checking account; and contributions to a 401k or TSP account that the year's limit refuses.
	Notes []string
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
	tax   *householdTax
	// benefits are the Social Security and FERS items synthesized for the query.
	benefits []benefit
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
	var stockSettings, classSettings []model.Setting[rateLayer]
	focal := map[model.AccountID][]model.FocalGrant{}
	// An equity account's value is its shares at the stock's price, whatever else is said of its growth.
	focalAccts := map[model.AccountID]bool{}
	for _, p := range chain {
		for _, m := range p.Mods {
			if a := rec.Account(model.AccountID(m.Target.ID)); a != nil && m.Target.Kind == model.KindAccount && m.Property == model.PropAddGrant && a.Type == model.TypeEquity {
				focalAccts[a.ID] = true
			}
		}
	}
	equityMode := func(id model.AccountID) bool {
		a := rec.Account(id)
		return a != nil && (a.HoldsGrants() || focalAccts[id])
	}
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
				case m.Property == model.PropAddGrant:
					acct := rec.Account(model.AccountID(m.Target.ID))
					switch {
					case m.Target.Kind != model.KindAccount:
						skip("unsupported property")
					case m.Grant == nil:
						skip("no grant to add")
					case acct.Type != model.TypeEquity:
						skip("account is not an equity account")
					case rec.Account(m.Grant.Brokerage) == nil:
						skip("deposit account not found")
					default:
						if err := m.Grant.Check(rec.Calendar(m.Grant.Calendar)); err != nil {
							skip(err.Error())
						} else {
							focal[acct.ID] = append(focal[acct.ID], m.Grant.Clone())
						}
					}
				case m.Property == model.PropGrowth && equityAccountOf(m, equityMode) != "":
					skip("an equity account is worth its shares at the stock's price: change the stock's growth instead")
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
			case model.KindStock:
				switch {
				case !rec.HasStock(m.Target.ID):
					skip("stock not found")
				case m.Property == model.PropGrowth:
					stockSettings = append(stockSettings, model.Setting[rateLayer]{Asset: model.AssetID(m.Target.ID), Value: rateLayer{m.Values, m.IsScale()}})
				default:
					skip("unsupported property")
				}
			case model.KindClass:
				switch {
				case rec.Class(m.Target.ID) == nil:
					skip("class not found")
				case m.Property == model.PropGrowth:
					classSettings = append(classSettings, model.Setting[rateLayer]{Asset: model.AssetID(m.Target.ID), Value: rateLayer{m.Values, m.IsScale()}})
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

	// Social Security and FERS are income items of the query only.
	r.benefits = memberBenefits(rec, infl, horizon, &r.diag)
	kinds := map[string]benefitKind{}
	for _, b := range r.benefits {
		items = append(items, b.item)
		kinds[b.item.ID] = b.kind
	}

	// Shocks hit every account holding the class; a rebalance names its account.
	var shocks []model.Shock
	rebalances := map[model.AccountID][]model.Rebalance{}
	for _, p := range chain {
		for i, sh := range p.Shocks {
			skip := func(reason string) {
				r.diag.Skipped = append(r.diag.Skipped, Skipped{Projection: p.Name, Index: i, Target: model.ObjectRef{Kind: model.KindClass, ID: sh.Class}, Property: propShock, Reason: reason})
			}
			switch {
			case rec.Class(sh.Class) == nil:
				skip("class not found")
			case math.IsNaN(sh.Change) || sh.Change <= -1:
				skip("a shock must be above -100%")
			default:
				shocks = append(shocks, sh)
			}
		}
		for i, rb := range p.Rebalances {
			skip := func(reason string) {
				r.diag.Skipped = append(r.diag.Skipped, Skipped{Projection: p.Name, Index: i, Target: model.ObjectRef{Kind: model.KindAccount, ID: string(rb.Account)}, Property: propRebalance, Reason: reason})
			}
			acct := rec.Account(rb.Account)
			sum, bad := 0.0, ""
			for _, w := range rb.Allocation {
				sum += w.Share
				if rec.Class(w.Class) == nil {
					bad = "class not found"
				} else if math.IsNaN(w.Share) || w.Share < 0 {
					bad = "a share must not be negative"
				}
			}
			switch {
			case acct == nil:
				skip("account not found")
			case equityMode(acct.ID) || len(acct.Allocation) == 0:
				skip("account has no allocation")
			case bad != "":
				skip(bad)
			case math.Abs(sum-1) > model.AllocationTolerance/100:
				skip("the shares do not sum to 100%")
			default:
				rebalances[acct.ID] = append(rebalances[acct.ID], rb)
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
		p.amortized = acct.Amortized()
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
		legOf := func(class string, share float64) leg {
			var base model.Schedule
			if c := rec.Class(class); c != nil {
				base = c.Return
			}
			cls, _, _ := model.Apply(rateLayer{s: base}, classSettings, func(id model.AssetID) bool { return string(id) == class }, rateLayer.over)
			return leg{class: class, share: share, rate: cls.s}
		}
		for _, h := range acct.Allocation {
			p.legs = append(p.legs, legOf(h.Class, h.Percent/100))
		}
		index := func(class string) int { return slices.IndexFunc(p.legs, func(l leg) bool { return l.class == class }) }
		// A rebalance can trade into a class the allocation does not hold.
		for _, rb := range rebalances[id] {
			for _, w := range rb.Allocation {
				if index(w.Class) < 0 {
					p.legs = append(p.legs, legOf(w.Class, 0))
				}
			}
		}
		p.lots = !acct.Tax.Sheltered() && model.Investable(acct.Type) && !equityMode(id)
		if p.lots && len(p.legs) == 0 {
			p.legs = []leg{{share: 1, rate: p.rate}}
		}
		if len(acct.Allocation) > 0 && !equityMode(id) {
			for _, sh := range shocks {
				if index(sh.Class) >= 0 {
					p.events = append(p.events, legEvent{date: sh.Date, kind: eventShock, class: sh.Class, change: sh.Change})
				}
			}
			for _, rb := range rebalances[id] {
				target := make([]float64, len(p.legs))
				for _, w := range rb.Allocation {
					target[index(w.Class)] = w.Share
				}
				p.events = append(p.events, legEvent{date: rb.Date, kind: eventRebalance, target: target})
			}
			if acct.RebalanceYearly {
				p.yearly = true
			}
		}
		return p
	}
	for _, id := range res.Accounts {
		r.paths[id] = newPath(id)
	}
	// Every side of an item that touches a hypothetical account, or two record accounts, counts only the occurrences after the latest cutoff of the accounts it touches, wherever they sit relative to the target, so both sides of a transfer agree and no date is projected on one side while the other reads it from actual data.
	cutoffs := map[string]model.Date{}
	hyp := map[string]model.Date{}
	outside := map[model.AccountID]*path{}
	for _, it := range items {
		cut := model.MinDate
		hypothetical := false
		touched := 0
		for _, id := range []model.AccountID{it.Account, it.From, it.To} {
			a := rec.Account(id)
			if a == nil {
				continue
			}
			hypothetical = hypothetical || a.Opens != nil
			touched++
			p := r.paths[id]
			if p == nil {
				if outside[id] == nil {
					outside[id] = newPath(id)
				}
				p = outside[id]
			}
			cut = max(cut, p.flowsAfter)
		}
		if hypothetical || touched > 1 {
			cutoffs[it.ID] = cut
		}
		if hypothetical {
			hyp[it.ID] = cut
		}
	}
	// Every equity account is planned, in or out of the target, since its vests pay into other accounts.
	var plans []*equityPlan
	var equity []*model.Account
	pathOf := func(id model.AccountID) *path {
		if p := r.paths[id]; p != nil {
			return p
		}
		if outside[id] == nil {
			outside[id] = newPath(id)
		}
		return outside[id]
	}
	for _, a := range rec.Accounts {
		if equityMode(a.ID) {
			equity = append(equity, a)
		}
	}
	for _, a := range equity {
		start := pathOf(a.ID).start
		pl := newEquityPlan(rec, a, start, horizon, newStockPrices(rec, a, start, focal[a.ID], stockSettings), focal[a.ID])
		plans = append(plans, pl)
		if p := r.paths[a.ID]; p != nil {
			p.plan, p.rate = pl, model.Schedule{}
		}
	}
	// The tax of a year depends on the interest of the savings and checking accounts and lands on the reconciliation account, so their paths are built with the target's, wherever they sit.
	ht := newHouseholdTax(rec, items, amounts, growths, infl, plans, hyp, &r.diag)
	ht.benefits = kinds
	r.tax = ht
	var all, savers []*path
	seen := map[model.AccountID]bool{}
	add := func(id model.AccountID) *path {
		p := pathOf(id)
		if !seen[id] {
			seen[id] = true
			p.ht = ht
			all = append(all, p)
		}
		return p
	}
	for _, id := range res.Accounts {
		add(id)
	}
	if horizon > model.MinDate {
		for _, a := range rec.Accounts {
			// An amortized mortgage and the account that pays it are modelled together, wherever they sit.
			if a.Amortized() {
				add(a.ID)
				if rec.Account(a.PaidFrom) != nil {
					add(a.PaidFrom)
				}
			}
			if a.Type == model.TypeSavings || a.Type == model.TypeChecking {
				savers = append(savers, add(a.ID))
			}
			// The gains a taxable account realizes are taxed wherever the account sits.
			if !a.Tax.Sheltered() && model.Investable(a.Type) && !equityMode(a.ID) {
				add(a.ID)
			}
		}
		if id := rec.Household.Checking; id != "" && rec.Account(id) != nil {
			add(id)
		}
	}
	for _, p := range all {
		if p.lots {
			ht.lots = append(ht.lots, p)
		}
	}
	// An account whose data ends before a counterpart's receives the transfers the counterpart recorded with it in between, so a recorded transfer lands on both sides; the budget item takes over on both sides after the later end. Equity accounts are left out on either side, since their plans and recorded vests already book the vests and the deposits that pay for them.
	for _, p := range all {
		id := p.acct.ID
		if p.plan != nil {
			continue
		}
		for _, c := range rec.Accounts {
			if c.ID == id || equityMode(c.ID) {
				continue
			}
			for _, t := range c.TransactionsIn(p.start, pathOf(c.ID).start) {
				if t.Kind != model.TxTransfer || t.Counter != id {
					continue
				}
				src, dst := c.ID, id
				if t.Amount > 0 {
					src, dst = dst, src
				}
				p.mirrored = append(p.mirrored, change{date: t.Date, cause: transferCause(rec, src, dst), amount: -t.Amount, counter: c.ID, rank: rankTransfer})
			}
		}
	}
	for _, p := range all {
		p.collect(rec, items, cutoffs, amounts, growths, infl, horizon, plans)
	}
	byID := map[model.AccountID]*path{}
	for _, p := range all {
		byID[p.acct.ID] = p
	}
	for _, p := range all {
		if p.amortized {
			p.amortize(byID[p.acct.PaidFrom], horizon)
		}
	}
	for _, p := range all {
		p.settle()
	}
	if horizon > model.MinDate {
		ht.reconcile(all, savers, horizon)
	}
	return r, nil
}

// equityAccountOf returns the account whose growth the modification of an account or default asset changes if equityMode says it is an equity account, else "".
func equityAccountOf(m model.Modification, equityMode func(model.AccountID) bool) model.AccountID {
	id := model.AccountID(m.Target.ID)
	if m.Target.Kind == model.KindAsset {
		var ok bool
		if id, ok = model.AccountOfDefaultAsset(model.AssetID(m.Target.ID)); !ok {
			return ""
		}
	}
	if equityMode(id) {
		return id
	}
	return ""
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
	class   string          // the class a shock changed
}

type point struct {
	date      model.Date
	bal       float64
	cumGrowth float64
	// subs and cumSubs are the balance and the growth accumulated in each leg of an allocated path, in leg order; nil on a path without legs.
	subs, cumSubs []float64
}

// leg is the part of an allocated account held in one market class: new money enters it by share and it compounds at the class's rate.
type leg struct {
	class string
	share float64
	rate  model.Schedule
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
	// legs is the account's allocation, one sub-balance per class in the allocation's order; the account's rate does not apply while there are legs.
	legs []leg
	// lots is set on a taxable investment account: its sales realize capital gains.
	lots bool
	// events are the shocks and rebalances that apply to the legs, ordered by date and kind once settled; yearly is set if the account rebalances every year, to the shares in force then.
	events []legEvent
	yearly bool
	// shocked are the changes shocks made to the legs and gains the capital gains the sales realized, both made by settle.
	shocked []change
	gains   []gain
	// plan is set on an equity account, whose value follows its shares' prices instead of growing at rate.
	plan    *equityPlan
	changes []change // flows after start, ordered by date then rank
	points  []point  // balance after each date with a flow, from start
	// mirrored are the transfers other accounts recorded with this one as counter after this account's data ends.
	mirrored []change
	// ht is the household's tax, which withholds from the paychecks the path receives.
	ht *householdTax
	// idle is set when the query ends on or before start, so nothing is extrapolated.
	idle bool
	// amortized is set on a mortgage with its terms: its balance moves only by the principal of its payments, not by growth.
	amortized bool
}

// Order of flows on one date (docs/design.md, "The engine"): income, grants, vests, expenses, then transfers
// (debt payments, contributions, withdrawals).
const (
	rankOpening = iota
	rankIncome
	rankGrant
	rankVest
	rankExpense
	rankTransfer
	rankTax
)

// factor is the growth factor of the balance bal from..to. A credit card owing money compounds its stated rate daily and earns nothing when it does not, and an amortized mortgage does not grow: its interest is charged and paid with each payment.
func (p *path) factor(bal float64, from, to model.Date) float64 {
	switch {
	case p.amortized:
		return 1
	case p.acct.Type == model.TypeCreditCard:
		if bal < 0 {
			return p.rate.CompoundDaily(from, to)
		}
		return 1
	}
	return p.rate.Compound(from, to)
}

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
	if p.plan != nil {
		return pt.bal + p.plan.drift(pt.date, d)
	}
	if p.legs != nil {
		sum := 0.0
		for i, l := range p.legs {
			sum += pt.subs[i] * l.rate.Compound(pt.date, d)
		}
		return sum
	}
	return pt.bal * p.factor(pt.bal, pt.date, d)
}

// growthAt is the growth accumulated from start to d.
func (p *path) growthAt(d model.Date) float64 {
	if d <= p.start {
		return 0
	}
	pt := p.pointAt(d)
	if p.plan != nil {
		return pt.cumGrowth + p.plan.drift(pt.date, d)
	}
	if p.legs != nil {
		sum := 0.0
		for i := range p.legs {
			sum += p.legGrowthAt(i, d)
		}
		return sum
	}
	return pt.cumGrowth + pt.bal*(p.factor(pt.bal, pt.date, d)-1)
}

// legGrowthAt is the growth accumulated from start to d in leg i.
func (p *path) legGrowthAt(i int, d model.Date) float64 {
	if d <= p.start {
		return 0
	}
	pt := p.pointAt(d)
	return pt.cumSubs[i] + pt.subs[i]*(p.legs[i].rate.Compound(pt.date, d)-1)
}

// amountOn is the item's amount on d under the projection's amounts and raises: the schedule's value lifted by the raises and, if the item is indexed, by inflation. It reports false if the schedule has no value on d.
func amountOn(rec *model.Record, it model.BudgetItem, d model.Date, amounts, growths map[string]model.Schedule, infl model.Schedule) (float64, bool) {
	raises, ok := growths[it.ID]
	if !ok {
		raises = it.Growth
	}
	pays, ok := amounts[it.ID]
	if !ok {
		pays = it.Amount
	}
	amt, ok := pays.At(d)
	if !ok {
		return 0, false
	}
	amt *= raiseFactor(it, rec.Today, d, raises)
	if it.Indexed {
		amt *= infl.Compound(rec.Today, d)
	}
	return amt, true
}

// collect gathers the changes the budget, the equity plans and mirrored transfers make to the account up to the horizon.
func (p *path) collect(rec *model.Record, items []model.BudgetItem, cutoffs map[string]model.Date, amounts, growths map[string]model.Schedule, infl model.Schedule, horizon model.Date, plans []*equityPlan) {
	p.points = []point{{date: p.start, bal: p.startVal}}
	p.idle = horizon <= p.start
	if p.idle {
		return
	}
	for _, it := range items {
		if !it.Touches(p.acct.ID) && it.MatchAccount != p.acct.ID {
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
		for _, d := range it.Dates(after, upTo) {
			if amt, ok := amountOn(rec, it, d, amounts, growths, infl); ok {
				p.flows(rec, it, d, amt)
			}
		}
	}
	p.changes = append(p.changes, p.mirrored...)
	if p.yearly {
		for y, _, _ := p.start.Add(1).YMD(); ; y++ {
			d := model.D(y, time.January, 1)
			if d > horizon {
				break
			}
			if d > p.start {
				p.events = append(p.events, legEvent{date: d, kind: eventYearly})
			}
		}
	}
	if p.plan != nil {
		p.changes = append(p.changes, p.plan.changes()...)
	}
	for _, pl := range plans {
		p.changes = append(p.changes, pl.deposits(p.acct.ID, p.flowsAfter)...)
	}
}

// settle orders the changes and works out the balance after each date with one.
func (p *path) settle() {
	p.shocked, p.gains = nil, nil
	p.points = []point{{date: p.start, bal: p.startVal}}
	if p.legs != nil {
		p.points[0].subs, p.points[0].cumSubs = p.split(p.startVal), make([]float64, len(p.legs))
	}
	if p.idle {
		return
	}
	sort.SliceStable(p.changes, func(i, j int) bool {
		a, b := p.changes[i], p.changes[j]
		if a.date != b.date {
			return a.date < b.date
		}
		return a.rank < b.rank
	})
	if p.legs != nil {
		p.settleLegs()
		return
	}
	bal, cum, cur := p.startVal, 0.0, p.start
	for i := 0; i < len(p.changes); {
		d := p.changes[i].date
		g := bal*p.factor(bal, cur, d) - bal
		if p.plan != nil {
			g = p.plan.drift(cur, d)
		}
		bal += g
		cum += g
		cur = d
		for ; i < len(p.changes) && p.changes[i].date == d; i++ {
			bal += p.changes[i].amount
		}
		p.points = append(p.points, point{date: d, bal: bal, cumGrowth: cum})
	}
}

// split divides an amount among the legs by their shares.
func (p *path) split(amount float64) []float64 {
	out := make([]float64, len(p.legs))
	for i, l := range p.legs {
		out[i] = amount * l.share
	}
	return out
}

// settleLegs is settle for an allocated path: each leg compounds at its class's rate, money paid in is split by the current shares and money paid out comes from the legs pro rata to their balances (by share if the account holds nothing or less). On a date the changes come first, then shocks, then rebalances. A path with lots opens a lot for each purchase and realizes the gain of each sale.
func (p *path) settleLegs() {
	n := len(p.legs)
	subs, cum, cur := slices.Clone(p.points[0].subs), make([]float64, n), p.start
	shares, price := make([]float64, n), make([]float64, n)
	for j, l := range p.legs {
		shares[j], price[j] = l.share, 1
	}
	var book *lotBook
	if p.lots {
		book = &lotBook{lots: make([][]lot, n)}
		for j := range subs {
			book.buy(j, p.start, subs[j], 1)
		}
	}
	sell := func(j int, d model.Date, value float64) {
		if book == nil {
			return
		}
		if short, long := book.sell(j, d, value, price[j]); short != 0 || long != 0 {
			p.gains = append(p.gains, gain{date: d, short: short, long: long})
		}
	}
	events := slices.DeleteFunc(slices.Clone(p.events), func(e legEvent) bool { return e.date <= p.start })
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.date != b.date {
			return a.date < b.date
		}
		return a.kind < b.kind
	})
	for i, k := 0, 0; i < len(p.changes) || k < len(events); {
		d := model.MaxDate
		if i < len(p.changes) {
			d = p.changes[i].date
		}
		if k < len(events) {
			d = min(d, events[k].date)
		}
		for j, l := range p.legs {
			f := l.rate.Compound(cur, d)
			g := subs[j] * (f - 1)
			subs[j] += g
			cum[j] += g
			price[j] *= f
		}
		cur = d
		for ; i < len(p.changes) && p.changes[i].date == d; i++ {
			amt := p.changes[i].amount
			total := 0.0
			for _, v := range subs {
				total += v
			}
			for j := range p.legs {
				if amt >= 0 || total <= 0 {
					x := amt * shares[j]
					subs[j] += x
					if book != nil {
						book.buy(j, d, x, price[j])
					}
				} else {
					x := amt * subs[j] / total
					subs[j] += x
					sell(j, d, -x)
				}
			}
		}
		for ; k < len(events) && events[k].date == d; k++ {
			e := events[k]
			if e.kind == eventShock {
				for j, l := range p.legs {
					if l.class != e.class {
						continue
					}
					if delta := subs[j] * e.change; delta != 0 {
						p.shocked = append(p.shocked, change{date: d, cause: CauseShock, amount: delta, class: e.class})
					}
					subs[j] *= 1 + e.change
					price[j] *= 1 + e.change
				}
				continue
			}
			target := e.target
			if e.kind == eventYearly {
				target = slices.Clone(shares)
			}
			total := 0.0
			for _, v := range subs {
				total += v
			}
			for j := range subs {
				if total > 0 {
					want := total * target[j]
					if want < subs[j] {
						sell(j, d, subs[j]-want)
					} else if book != nil {
						book.buy(j, d, want-subs[j], price[j])
					}
					subs[j] = want
				}
				shares[j] = target[j]
			}
		}
		bal, growth := 0.0, 0.0
		for j := range subs {
			bal += subs[j]
			growth += cum[j]
		}
		p.points = append(p.points, point{date: d, bal: bal, cumGrowth: growth, subs: slices.Clone(subs), cumSubs: slices.Clone(cum)})
	}
}

func (p *path) flows(rec *model.Record, it model.BudgetItem, d model.Date, amt float64) {
	add := func(rank int, cause Cause, amount float64, counter model.AccountID) {
		p.changes = append(p.changes, change{date: d, cause: cause, amount: amount, counter: counter, rank: rank})
	}
	switch it.Kind {
	case model.ItemIncome:
		if it.Account == p.acct.ID {
			add(rankIncome, p.ht.incomeCause(it.ID), amt, "")
			if it.Income() == model.IncomeWages {
				add(rankIncome, CauseTax, -p.ht.paycheckTax(it.ID, d), "")
			}
		}
		if it.MatchAccount == p.acct.ID && !p.acct.ClosedOn(d) {
			if m := p.ht.match(it.ID, d); m != 0 {
				add(rankIncome, CauseMatch, m, "")
			}
		}
	case model.ItemExpense:
		add(rankExpense, CauseExpense, -amt, "")
	case model.ItemTransfer:
		amt = p.ht.moved(it, d, amt)
		if amt == 0 {
			return
		}
		cause := transferCause(rec, it.From, it.To)
		if it.From == p.acct.ID {
			add(rankTransfer, cause, -amt, it.To)
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
