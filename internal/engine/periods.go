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

// periodViews evaluates the n periods of the item up to the one containing d against the cutoff. A wages paycheck is budgeted at its projected net deposit, which is what lands in the bank, with its gross kept in the view's Gross; the engine's own period views stay at the gross, which is what it taxes.
func periodViews(rec *model.Record, it model.BudgetItem, d model.Date, n int, cutoff model.Date, match func(model.Period) *model.PeriodMatch) []model.PeriodView {
	gross := func(p model.Period) float64 {
		v, _ := BudgetedAmount(rec, it, p.Start)
		return v
	}
	if !it.IsWages() {
		return model.PeriodHistory(it, d, n, rec.Occurrences, cutoff, gross, match)
	}
	deposit := Deposits(rec)
	net := func(p model.Period) float64 {
		if v, ok := deposit(it, p.Start); ok {
			return v
		}
		return gross(p)
	}
	views := model.PeriodHistory(it, d, n, rec.Occurrences, cutoff, net, match)
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
	var out []ItemPeriods
	for _, it := range rec.Budget.Items {
		cur, ok := it.PeriodAt(asOf)
		if !ok || it.End != nil && *it.End < cur.Start {
			continue
		}
		cutoff, ok := cutoffs[it.ID]
		if !ok {
			cutoff = asOf
		}
		out = append(out, ItemPeriods{it, cutoff, periodViews(rec, it, asOf, n, cutoff, func(p model.Period) *model.PeriodMatch { return rec.PeriodMatch(it, p, cutoff) })})
	}
	return out, nil
}

// Deposits returns a function giving the net deposit projected for the wages paycheck of the base budget item on d: its gross as BudgetedAmount counts it, less the income tax and payroll tax the engine withholds from that paycheck, which already reflect its pre-tax deferrals. It reports false when the item has no paycheck on d. The function keeps what it computed, so it should be made once for a batch of lookups.
func Deposits(rec *model.Record) func(it model.BudgetItem, d model.Date) (float64, bool) {
	ht := newHouseholdTax(rec, rec.Budget.Items, nil, nil, rec.Globals[model.PropInflation], nil, nil, &Diagnostics{})
	return func(it model.BudgetItem, d model.Date) (float64, bool) {
		if !it.IsWages() {
			return 0, false
		}
		gross, ok := BudgetedAmount(rec, it, d)
		if !ok {
			return 0, false
		}
		y, _, _ := d.YMD()
		withheld, ok := ht.year(y).withheld[paycheckKey{it.ID, d}]
		if !ok {
			return 0, false
		}
		return gross - withheld, true
	}
}

func init() { model.NetDeposits = Deposits }
