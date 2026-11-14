// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"

	"github.com/joeriedel/finplan/internal/model"
)

// LineState is where a budget line stands in its cycle on the date asked about.
type LineState string

const (
	// StateMatched: money was recorded for the cycle, by transactions or by hand; Amount and Variance say how much and how far over or under budget.
	StateMatched LineState = "matched"
	// StateMissing: a lump's due date has passed and nothing matched it.
	StateMissing LineState = "missing"
	// StateNotYetDue: the cycle's money is still to come.
	StateNotYetDue LineState = "not yet due"
	// StateEstimated: the due date has passed in a cycle with no recorded transactions, so the budgeted amount is only assumed.
	StateEstimated LineState = "estimated"
	// StateOnPace, StateHot and StateLight: a spread line's posted amount against its budget times the elapsed share of the cycle.
	StateOnPace LineState = "on pace"
	StateHot    LineState = "running hot"
	StateLight  LineState = "light"
)

// PaceTolerance is how far, as a share of the amount due so far, a spread line's posted amount may be from it and still be on pace.
const PaceTolerance = 0.10

// LineStatus is the status of one budget line's cycle.
type LineStatus struct {
	Item model.BudgetItem
	// View is the cycle containing the date asked about, evaluated as of Cutoff.
	View   model.PeriodView
	Cutoff model.Date
	State  LineState
	// Amount is what was recorded for the cycle and Variance that less the budgeted amount, for a matched line; for a spread line on pace, running hot or light, Amount is what has posted and Variance is it less the budget for the elapsed share of the cycle.
	Amount   float64
	Variance float64
	// Matches are the transactions matched to the line in this cycle and dated on or before Cutoff.
	Matches []model.Match
}

// StatusView is the budget's standing on a date.
type StatusView struct {
	Date  model.Date
	Lines []LineStatus
	// Unaccounted are the amounts no budget item accounts for in the calendar month containing Date, per account with any, whole month whatever the date.
	Unaccounted []model.UnaccountedMonth
}

// StatusAsOf evaluates each base budget line's cycle containing d as of d: transactions dated after d do not count and an occurrence due after d is not yet due. A date after the record's today is evaluated as of today, since nothing after it is recorded; the lines are then those of a projected date, whose future occurrences are not yet due. Manual posted amounts and statuses count as matched. The cutoff of each line is the earliest of d, today and the date its accounts' records end.
func StatusAsOf(rec *model.Record, d model.Date) (StatusView, error) {
	cutoffs, err := ItemCutoffs(rec, "")
	if err != nil {
		return StatusView{}, err
	}
	asOf := min(d, rec.Today)
	out := StatusView{Date: d}
	for _, it := range rec.Budget.Items {
		cur, ok := it.PeriodAt(d)
		if !ok || it.End != nil && *it.End < cur.Start {
			continue
		}
		cutoff := asOf
		if c, ok := cutoffs[it.ID]; ok {
			cutoff = min(cutoff, c)
		}
		budgeted := func(p model.Period) float64 {
			v, _ := BudgetedAmount(rec, it, p.Start)
			return v
		}
		hist := model.PeriodHistory(it, d, 1, rec.Occurrences, cutoff, budgeted, func(p model.Period) *model.PeriodMatch { return rec.PeriodMatchAsOf(it, p, cutoff) })
		ls := LineStatus{Item: it, View: hist[0], Cutoff: cutoff}
		ls.classify()
		for _, m := range rec.Matches().Item(it.ID) {
			if m.Period == ls.View.Period.Start && m.Transaction.Date <= cutoff {
				ls.Matches = append(ls.Matches, m)
			}
		}
		out.Lines = append(out.Lines, ls)
	}
	y, m, _ := d.YMD()
	out.Unaccounted = rec.Unaccounted("", model.D(y, m, 1), model.D(y, m+1, 0))
	return out, nil
}

// classify sets State, Amount and Variance from the cycle's view.
func (l *LineStatus) classify() {
	v := l.View
	started := v.Period.Start <= l.Cutoff
	switch {
	case v.Overridden && (v.Override.Posted != nil || v.Status == model.StatusPosted):
		l.State, l.Amount = StateMatched, v.Posted
		l.Variance = v.Posted - v.Budgeted
		return
	case !started:
		l.State = StateNotYetDue
		return
	case v.Timing == model.Lump:
		switch {
		case v.Matched:
			l.State, l.Amount, l.Variance = StateMatched, v.Posted, v.Posted-v.Budgeted
		case v.Missing:
			l.State = StateMissing
		default:
			l.State = StateEstimated
		}
		return
	}
	share := 1.0
	if v.Period.End > l.Cutoff {
		share = float64(int(l.Cutoff-v.Period.Start)+1) / float64(v.Period.Days())
	}
	due := v.Budgeted * share
	l.Amount, l.Variance = v.Posted, v.Posted-due
	switch {
	case math.Abs(l.Variance) <= PaceTolerance*math.Abs(due):
		l.State = StateOnPace
	case l.Variance > 0:
		l.State = StateHot
	default:
		l.State = StateLight
	}
}
