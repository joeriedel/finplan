// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"time"

	"github.com/joeriedel/finplan/internal/benefits"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
)

// Retirement. From the projection's retirement date wages and the saving into the household's accounts stop, and the household lives off its accounts: at the end of each month, if the reconciliation (checking) account is below zero, it is refilled from the buckets in the projection's withdrawal order, and the household's tax follows as for any income: a sale in a taxable account realizes its gains, a withdrawal from a tax-deferred account is ordinary income (early before 59 and a half) and a Roth withdrawal is untaxed. Required minimum distributions and Roth conversions are made on 31 December of each year. All of it is booked in date order together with the reconciliation of the earlier years' tax, since each depends on the balances the other leaves; the tax the withdrawals owe is paid on the next 15 April and covered by later withdrawals.

// drawPlan holds the retirement settings of the projection chain.
type drawPlan struct {
	retire  *model.Date
	order   []model.Bucket
	bracket *float64
}

// yearDraw is the income a year's drawdown, required distributions and conversions add. Drawn is the withdrawals from tax-deferred accounts that are not early, Early those before 59 and a half.
type yearDraw struct {
	drawn, early, rmd, conversion float64
}

// bucketOf returns the bucket the account is drawn from, false for an account that is not (checking, debts, equity, property, HSA and 529 accounts). The retirement account types (401(k), TSP, IRA and Roth IRA) are drawn from by their tax treatment, since the two are independent.
func bucketOf(a *model.Account) (model.Bucket, bool) {
	if a == nil {
		return "", false
	}
	retirement := a.Type == model.Type401k || a.Type == model.TypeTSP || a.Type == model.TypeIRA || a.Type == model.TypeRothIRA
	switch {
	case a.Tax == model.Taxable && (a.Type == model.TypeSavings || a.Type == model.TypeBrokerage):
		return model.BucketTaxable, true
	case a.Tax == model.TaxDeferred && retirement:
		return model.BucketDeferred, true
	case a.Tax == model.TaxFree && retirement:
		return model.BucketRoth, true
	}
	return "", false
}

// stopAtRetirement ends the wages items, and the transfers that save from an account outside the buckets into one of them, the day before retirement.
func stopAtRetirement(rec *model.Record, items []model.BudgetItem, retire model.Date) []model.BudgetItem {
	out := make([]model.BudgetItem, len(items))
	copy(out, items)
	for i, it := range out {
		_, toBucket := bucketOf(rec.Account(it.To))
		_, fromBucket := bucketOf(rec.Account(it.From))
		stops := (it.Kind == model.ItemIncome && it.Income() == model.IncomeWages) ||
			(it.Kind == model.ItemTransfer && toBucket && !fromBucket)
		if end := retire.Add(-1); stops && (it.End == nil || *it.End > end) {
			out[i].End = &end
		}
	}
	return out
}

// firstEarner returns the household's first earner, whose age sets the required distributions and the early withdrawal penalty.
func (h *householdTax) firstEarner() (model.HouseholdMember, bool) {
	for _, m := range h.rec.Household.Members {
		if m.Role == model.RoleEarner {
			return m, true
		}
	}
	return model.HouseholdMember{}, false
}

// active reports whether the query books drawdown, distributions or conversions.
func (h *householdTax) active(horizon model.Date) bool {
	if h.draw == nil {
		return false
	}
	if h.draw.retire != nil || h.draw.bracket != nil {
		return true
	}
	if m, ok := h.firstEarner(); ok {
		by, _, _ := m.Birth.YMD()
		hy, _, _ := horizon.YMD()
		return by+benefits.RMDStartAge(by) <= hy
	}
	return false
}

func (h *householdTax) yearDraw(y int) *yearDraw {
	if h.draws == nil {
		h.draws = map[int]*yearDraw{}
	}
	if d, ok := h.draws[y]; ok {
		return d
	}
	d := &yearDraw{}
	h.draws[y] = d
	return d
}

// balanceOn is the path's balance on d, false if the path has no projected balance then.
func (p *path) balanceOn(d model.Date) (float64, bool) {
	if p.idle || p.acct.ClosedOn(d) || d <= p.start {
		return 0, false
	}
	return p.valueAt(d), true
}

// drawPaths are the paths of the query by bucket, in the record's account order, and by account.
type drawPaths struct {
	buckets map[model.Bucket][]*path
	byID    map[model.AccountID]*path
}

func (h *householdTax) drawPaths(paths []*path) *drawPaths {
	dp := &drawPaths{buckets: map[model.Bucket][]*path{}, byID: map[model.AccountID]*path{}}
	for _, p := range paths {
		dp.byID[p.acct.ID] = p
	}
	for _, a := range h.rec.Accounts {
		if b, ok := bucketOf(a); ok && dp.byID[a.ID] != nil {
			dp.buckets[b] = append(dp.buckets[b], dp.byID[a.ID])
		}
	}
	return dp
}

// move books amount from src to dst on d.
func (h *householdTax) move(d model.Date, src, dst *path, amount float64) {
	cause := transferCause(h.rec, src.acct.ID, dst.acct.ID)
	src.addChange(change{date: d, cause: cause, amount: -amount, counter: dst.acct.ID, rank: rankTransfer})
	dst.addChange(change{date: d, cause: cause, amount: amount, counter: src.acct.ID, rank: rankTransfer})
	src.resettle(d)
	dst.resettle(d)
}

// reconcileWithDrawdown is reconcile for a query with retirement, distributions or conversions: the same reconciliations, and the month ends and year ends between them.
func (h *householdTax) reconcileWithDrawdown(paths, interest []*path, horizon model.Date) {
	h.savers = interest
	lo := horizon
	for _, p := range paths {
		lo = min(lo, p.start)
	}
	first, _, _ := lo.YMD()
	last, _, _ := horizon.YMD()
	dp := h.drawPaths(paths)
	for y := first; y <= last; y++ {
		for m := time.January; m <= time.December; m++ {
			if date := model.D(y, time.April, reconciliationDay); m == time.April && date <= horizon {
				h.reconcileYear(y-1, date, paths, interest)
			}
			end := model.D(y, m+1, 1).Add(-1)
			if end > horizon {
				return
			}
			if h.draw.retire != nil && end >= *h.draw.retire {
				h.refill(end, dp)
			}
		}
		end := model.D(y, time.December, 31)
		h.distribute(y, end, dp)
		h.convert(y, end, dp)
	}
}

// refill covers a negative balance of the reconciliation account on d from the buckets in the withdrawal order, and notes the first date the buckets cannot cover it.
func (h *householdTax) refill(d model.Date, dp *drawPaths) {
	chk := dp.byID[ReconciliationAccount(h.rec, d)]
	if chk == nil {
		return
	}
	bal, ok := chk.balanceOn(d)
	if !ok || bal >= -0.005 {
		return
	}
	need := -bal
	for _, b := range h.draw.order {
		for _, p := range dp.buckets[b] {
			avail, ok := p.balanceOn(d)
			if !ok || avail <= 0 || need <= 0 {
				continue
			}
			take := min(avail, need)
			h.move(d, p, chk, take)
			if b == model.BucketDeferred {
				y, _, _ := d.YMD()
				if h.earlyOn(d) {
					h.yearDraw(y).early += take
				} else {
					h.yearDraw(y).drawn += take
				}
			}
			need -= take
		}
	}
	if need > 0.005 && !h.ranOut {
		h.ranOut = true
		h.note("money runs out on %s: every taxable, tax-deferred and Roth account is empty, and the checking account goes below zero", d)
	}
}

// distribute makes the year's required minimum distribution from the tax-deferred accounts into the reconciliation account on d: the deferred balances at the end of the previous year over the Uniform Lifetime Table's period for the age the first earner reaches in the year, less what the year's withdrawals, by drawdown and by the budget, already took, taken from the accounts in proportion to their balances.
func (h *householdTax) distribute(y int, d model.Date, dp *drawPaths) {
	m, ok := h.firstEarner()
	chk := dp.byID[ReconciliationAccount(h.rec, d)]
	if !ok || chk == nil {
		return
	}
	by, _, _ := m.Birth.YMD()
	age := m.AgeIn(y)
	if age < benefits.RMDStartAge(by) {
		return
	}
	prior, total := model.D(y-1, time.December, 31), 0.0
	for _, p := range dp.buckets[model.BucketDeferred] {
		total += max(0, p.data(prior).Amount)
	}
	yd := h.yearDraw(y)
	need := total/benefits.UniformLifetimeDivisor(age) - yd.drawn - h.year(y).fromDeferred
	if need <= 0.005 {
		return
	}
	avails, sum := map[*path]float64{}, 0.0
	for _, p := range dp.buckets[model.BucketDeferred] {
		if v, ok := p.balanceOn(d); ok && v > 0 {
			avails[p], sum = v, sum+v
		}
	}
	need = min(need, sum)
	for _, p := range dp.buckets[model.BucketDeferred] {
		if v := avails[p]; v > 0 {
			h.move(d, p, chk, need*v/sum)
			yd.rmd += need * v / sum
		}
	}
}

// convert moves from the tax-deferred accounts into the first open tax-free retirement account (a Roth IRA by preference) on d as much as takes the year's ordinary taxable income to the top of the projection's bracket, counting the Social Security the conversion makes taxable, or all they hold if less. The conversion is ordinary income, never early.
func (h *householdTax) convert(y int, d model.Date, dp *drawPaths) {
	if h.draw.bracket == nil {
		return
	}
	if h.draw.retire != nil {
		if ry, _, _ := h.draw.retire.YMD(); y < ry {
			return
		}
	}
	var dst *path
	for _, roth := range []bool{true, false} {
		for _, p := range dp.buckets[model.BucketRoth] {
			if _, ok := p.balanceOn(d); ok && (p.acct.Type == model.TypeRothIRA) == roth && dst == nil {
				dst = p
			}
		}
	}
	if dst == nil {
		h.note("the Roth conversion bracket is set, but no Roth account is open to convert into")
		return
	}
	top, _, err := h.rec.Tax.BracketTop(y, h.rec.Household.Filing, h.inflation(y), *h.draw.bracket)
	if err != nil {
		h.note("tax %d: %v", y, err)
		return
	}
	var held float64
	for _, p := range dp.buckets[model.BucketDeferred] {
		if avail, ok := p.balanceOn(d); ok && avail > 0 {
			held += avail
		}
	}
	take, err := conversionRoom(h.rec.Tax, h.compute(y, h.savers).Input, top, held)
	if err != nil {
		h.note("tax %d: %v", y, err)
		return
	}
	moved := 0.0
	for _, p := range dp.buckets[model.BucketDeferred] {
		avail, ok := p.balanceOn(d)
		if !ok || avail <= 0 || take-moved <= 0.005 {
			continue
		}
		amount := min(avail, take-moved)
		h.move(d, p, dst, amount)
		moved += amount
	}
	h.yearDraw(y).conversion += moved
}

// conversionRoom is how much of held can be added to the input's other income before its ordinary taxable income passes top. The preferentially taxed gains sit on top of the ordinary income and do not use the bracket's room, but they count toward the provisional income, so a converted dollar can make Social Security taxable and the amount is found by bisection on the ordinary taxable income it leaves.
func conversionRoom(tables tax.Tables, in tax.Input, top, held float64) (float64, error) {
	other := in.OtherIncome
	var failed error
	ordinary := func(x float64) float64 {
		in.OtherIncome = other + x
		res, err := tax.Compute(tables, in)
		if err != nil {
			failed = err
		}
		return res.FederalTaxable - min(res.PreferredGains, res.FederalTaxable)
	}
	var take float64
	switch {
	case ordinary(0) >= top:
	case ordinary(held) <= top:
		take = held
	default:
		lo, hi := 0.0, held
		for range 60 {
			mid := (lo + hi) / 2
			if ordinary(mid) <= top {
				lo = mid
			} else {
				hi = mid
			}
		}
		take = lo
	}
	return take, failed
}
