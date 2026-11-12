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

// Current returns the period that contains the date Periods was asked about.
func (p ItemPeriods) Current() model.PeriodView { return p.Periods[len(p.Periods)-1] }

// Periods returns the last n periods, up to the one containing asOf, of each base budget item that has one and has not ended before it, evaluated against the item's cutoff under the projection (asOf for an item that touches no account of the record). Budgeted amounts are the base budget's.
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
		budgeted := func(p model.Period) float64 {
			v, _ := BudgetedAmount(rec, it, p.Start)
			return v
		}
		out = append(out, ItemPeriods{it, cutoff, model.PeriodHistory(it, asOf, n, rec.Occurrences, cutoff, budgeted)})
	}
	return out, nil
}
