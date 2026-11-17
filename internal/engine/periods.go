// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import "github.com/joeriedel/finplan/internal/model"

// BudgetedAmount is the amount of the base budget item on d as the engine counts it: its schedule's value lifted by the item's raises and, if it is indexed, by inflation. It reports false if the schedule has no value on d. A projection's modifications are not applied.
func BudgetedAmount(rec *model.Record, it model.BudgetItem, d model.Date) (float64, bool) {
	return amountOn(rec, it, d, nil, nil, rec.Globals[model.PropInflation])
}

// ItemPeriods is the recent periods of one base budget item.
type ItemPeriods struct {
	Item model.BudgetItem
	// Cutoff is the date after which the item's flows count under the projection: what the balances know of the item ends there.
	Cutoff model.Date
	// Periods are the item's periods evaluated against the cutoff, oldest first; the last contains asOf.
	Periods []model.PeriodView
}

// cutoffID is the id of the item whose cutoff a budget line is judged by: a payroll deduction follows its paycheck, a line with no paycheck its own.
func cutoffID(rec *model.Record, it model.BudgetItem) string {
	if _, ok := rec.Budget.Item(it.Paycheck); ok {
		return it.Paycheck
	}
	return it.ID
}

// deductionMatch is what the matches say of a period of a payroll deduction: that of its paycheck in the same period. A matched or posted paycheck makes the deduction matched at the amount, a missing one makes it missing, and otherwise nothing is known. No transaction of the deduction's own is due.
func deductionMatch(rec *model.Record, ht *householdTax, src model.BudgetItem, p model.Period, amount float64, cutoff model.Date, match func(model.BudgetItem, model.Period) *model.PeriodMatch) *model.PeriodMatch {
	if sp, ok := src.PeriodAt(p.Start); !ok || sp.Start != p.Start {
		return nil
	}
	v := periodViews(rec, ht, src, p.Start, 1, cutoff, match)[0]
	switch {
	case v.Matched, v.Overridden && (v.Override.Posted != nil || v.Status == model.StatusPosted):
		return &model.PeriodMatch{Matched: true, Amount: amount}
	case v.Missing:
		return &model.PeriodMatch{}
	}
	return nil
}

// periodViews evaluates the n periods of the item up to the one containing d against the cutoff, with match giving what the transaction matches say of a period of an item. A payroll deduction stands as its paycheck does (see deductionMatch). A wages paycheck is budgeted at its projected net deposit, which is what lands in the bank, with its gross kept in the view's Gross, as the engine's own period views count it.
func periodViews(rec *model.Record, ht *householdTax, it model.BudgetItem, d model.Date, n int, cutoff model.Date, match func(model.BudgetItem, model.Period) *model.PeriodMatch) []model.PeriodView {
	gross := func(p model.Period) float64 {
		v, _ := BudgetedAmount(rec, it, p.Start)
		return v
	}
	if src, ok := rec.Budget.Item(it.Paycheck); ok {
		return model.PeriodHistory(it, d, n, rec.Occurrences, cutoff, gross, func(p model.Period) *model.PeriodMatch {
			return deductionMatch(rec, ht, src, p, gross(p), cutoff, match)
		})
	}
	itemMatch := func(p model.Period) *model.PeriodMatch { return match(it, p) }
	if !it.IsWages() {
		return model.PeriodHistory(it, d, n, rec.Occurrences, cutoff, gross, itemMatch)
	}
	net := func(p model.Period) float64 {
		if v, ok := ht.deposit(it, p.Start); ok {
			return v
		}
		return gross(p)
	}
	views := model.PeriodHistory(it, d, n, rec.Occurrences, cutoff, net, itemMatch)
	for i := range views {
		views[i].Gross = gross(views[i].Period)
	}
	return views
}

// Current returns the period that contains the date Periods was asked about.
func (p ItemPeriods) Current() model.PeriodView { return p.Periods[len(p.Periods)-1] }

// Periods returns the last n periods, up to the one containing asOf, of each base budget item that has one and has not ended before it, evaluated against the item's cutoff under the projection (asOf for an item that touches no account of the record). Budgeted amounts are the base budget's, a wages paycheck's as its projected net deposit.
func Periods(rec *model.Record, projection string, asOf model.Date, n int) ([]ItemPeriods, error) {
	cutoffs, err := ItemCutoffs(rec, projection)
	if err != nil {
		return nil, err
	}
	ht := newDeposits(rec, cutoffs)
	var out []ItemPeriods
	for _, it := range rec.Budget.Items {
		cur, ok := it.PeriodAt(asOf)
		if !ok || it.End != nil && *it.End < cur.Start || it.Paycheck != "" && !ht.sourceBooked(it, cur.Start, false) {
			continue
		}
		cutoff, ok := cutoffs[cutoffID(rec, it)]
		if !ok {
			cutoff = asOf
		}
		out = append(out, ItemPeriods{it, cutoff, periodViews(rec, ht, it, asOf, n, cutoff, func(it model.BudgetItem, p model.Period) *model.PeriodMatch { return rec.PeriodMatch(it, p, cutoff) })})
	}
	return out, nil
}

// Deposits returns a function giving the net deposit projected for the wages paycheck of the base budget item on d: its gross as BudgetedAmount counts it, less the income tax and payroll tax the engine withholds from that paycheck, which already reflect its pre-tax deferrals, and less the payroll deductions taken from it. It reports false when the item has no paycheck on d. The function keeps what it computed, so it should be made once for a batch of lookups.
func Deposits(rec *model.Record) func(it model.BudgetItem, d model.Date) (float64, bool) {
	cutoffs, err := ItemCutoffs(rec, "")
	if err != nil {
		cutoffs = nil
	}
	return newDeposits(rec, cutoffs).deposit
}

// newDeposits returns the household tax the engine books the base budget from, with the cutoffs of its items, so that the deposits it gives are the ones booked.
func newDeposits(rec *model.Record, cutoffs map[string]model.Date) *householdTax {
	ht := newHouseholdTax(rec, rec.Budget.Items, nil, nil, rec.Globals[model.PropInflation], nil, nil, &Diagnostics{})
	ht.cuts = cutoffs
	return ht
}

// matchDeposits is the model.NetDeposits hook: Deposits without the item cutoffs. Matching calls it while holding the record's match lock, and finding cutoffs needs the matches, so a cutoff-aware household tax would deadlock; matching only ranks candidates by the deposit, which the cutoffs change only slightly.
func matchDeposits(rec *model.Record) func(it model.BudgetItem, d model.Date) (float64, bool) {
	return newDeposits(rec, nil).deposit
}

func init() { model.NetDeposits = matchDeposits }
