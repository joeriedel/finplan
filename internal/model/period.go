// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"slices"
	"sort"
)

// Timing says when a budget item's amount is spent within one of its periods: a lump on the occurrence's date, or spread evenly over the period's days.
type Timing string

// Timings.
const (
	Lump   Timing = "lump"
	Spread Timing = "spread"
)

// Timings lists the timings.
var Timings = []Timing{Lump, Spread}

// Valid reports whether t is one of the timings.
func (t Timing) Valid() bool { return slices.Contains(Timings, t) }

// DefaultTiming returns the timing an item has when none is chosen: an expense that recurs monthly or more often (a custom recurrence of at most a month) is spread, everything else is a lump.
func (b BudgetItem) DefaultTiming() Timing {
	if b.Kind != ItemExpense {
		return Lump
	}
	switch b.Recurrence.Kind {
	case Weekly, Biweekly, Semimonthly, Monthly:
		return Spread
	case Custom:
		if n, inMonths := b.Recurrence.interval(); n > 0 && (inMonths && n <= 1 || !inMonths && n <= 31) {
			return Spread
		}
	}
	return Lump
}

// EffectiveTiming returns the item's timing, or its default if it has none.
func (b BudgetItem) EffectiveTiming() Timing {
	if b.Timing == "" {
		return b.DefaultTiming()
	}
	return b.Timing
}

// Period is the span of one scheduled occurrence of an item, from its date to the day before the next one, both included. An item with no next occurrence has a period of one day.
type Period struct {
	Start, End Date
}

// Days returns the number of days in the period.
func (p Period) Days() int { return int(p.End-p.Start) + 1 }

// lastOnOrBefore returns the item's latest occurrence on or before d, from its start on.
func (b BudgetItem) lastOnOrBefore(d Date) (Date, bool) {
	for _, back := range []int{40, 400, 4000, 40000} {
		ds := b.Dates(d.Add(-back), d)
		for i := len(ds) - 1; i >= 0; i-- {
			if b.Start == nil || ds[i] >= *b.Start {
				return ds[i], true
			}
		}
	}
	return 0, false
}

// firstAfter returns the item's first occurrence after d.
func (b BudgetItem) firstAfter(d Date) (Date, bool) {
	for _, fwd := range []int{400, 4000, 40000} {
		if ds := b.Dates(d, d.Add(fwd)); len(ds) > 0 {
			return ds[0], true
		}
	}
	return 0, false
}

// PeriodAt returns the period that contains d, and false if d is before the item's first occurrence.
func (b BudgetItem) PeriodAt(d Date) (Period, bool) {
	start, ok := b.lastOnOrBefore(d)
	if !ok {
		return Period{}, false
	}
	next, ok := b.firstAfter(start)
	if !ok {
		return Period{start, start}, true
	}
	return Period{start, next.Add(-1)}, true
}

// PeriodBefore returns the period before p, and false if p is the item's first.
func (b BudgetItem) PeriodBefore(p Period) (Period, bool) {
	return b.PeriodAt(p.Start.Add(-1))
}

// OccurrenceStatus says what is known of an occurrence's posting: auto leaves it to the estimate.
type OccurrenceStatus string

// Occurrence statuses.
const (
	StatusAuto    OccurrenceStatus = "auto"
	StatusPosted  OccurrenceStatus = "posted"
	StatusPending OccurrenceStatus = "pending"
)

// OccurrenceStatuses lists the statuses.
var OccurrenceStatuses = []OccurrenceStatus{StatusAuto, StatusPosted, StatusPending}

// Valid reports whether s is one of the statuses.
func (s OccurrenceStatus) Valid() bool { return slices.Contains(OccurrenceStatuses, s) }

// Occurrence is what the user recorded of one period of a budget item: its status, the amount that has posted (which may exceed the budget) and the amount to expect in this period instead of the budgeted one. Nil amounts are unset.
type Occurrence struct {
	// ID is the occurrence's stored fact id; 0 before it is stored.
	ID     int64
	Item   string
	Period Date
	Status OccurrenceStatus
	// Posted and Expected are unset when nil.
	Posted, Expected *float64
}

// Clone returns a copy that shares nothing with o.
func (o Occurrence) Clone() Occurrence {
	o.Posted, o.Expected = clonePtr(o.Posted), clonePtr(o.Expected)
	return o
}

// Planned returns the amount the period is expected to total: the expected amount if set, else the budgeted one.
func (o Occurrence) Planned(budgeted float64) float64 {
	if o.Expected != nil {
		return *o.Expected
	}
	return budgeted
}

// Counted returns the amount the occurrence counts for in a total over a whole period, such as the tax of a year: what has posted if an amount is set or the status is posted, else the expected amount if set, else the budgeted one.
func (o Occurrence) Counted(budgeted float64) float64 {
	if o.Posted != nil {
		return *o.Posted
	}
	return o.Planned(budgeted)
}

type occurrenceKey struct {
	item   string
	period Date
}

// OccurrenceBook is the occurrence facts of the budget items, one per item and period start: the latest added wins. The zero value and a nil book hold none.
type OccurrenceBook struct {
	by map[occurrenceKey]Occurrence
}

// Add sets the occurrence of its item and period, replacing an earlier one.
func (b *OccurrenceBook) Add(o Occurrence) {
	if b.by == nil {
		b.by = map[occurrenceKey]Occurrence{}
	}
	b.by[occurrenceKey{o.Item, o.Period}] = o
}

// Remove deletes the occurrence of the item and period start, if any.
func (b *OccurrenceBook) Remove(item string, period Date) {
	if b != nil {
		delete(b.by, occurrenceKey{item, period})
	}
}

// Get returns the occurrence of the item and period start, and false if there is none.
func (b *OccurrenceBook) Get(item string, period Date) (Occurrence, bool) {
	if b == nil {
		return Occurrence{}, false
	}
	o, ok := b.by[occurrenceKey{item, period}]
	return o, ok
}

// All returns every occurrence ordered by item and period.
func (b *OccurrenceBook) All() []Occurrence {
	if b == nil {
		return nil
	}
	out := make([]Occurrence, 0, len(b.by))
	for _, o := range b.by {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Item != out[j].Item {
			return out[i].Item < out[j].Item
		}
		return out[i].Period < out[j].Period
	})
	return out
}

func (b *OccurrenceBook) clone() *OccurrenceBook {
	if b == nil {
		return nil
	}
	c := &OccurrenceBook{by: make(map[occurrenceKey]Occurrence, len(b.by))}
	for k, o := range b.by {
		c.by[k] = o.Clone()
	}
	return c
}

// PeriodView is one period of an item as the user sees it against a cutoff, the last date the balances know.
type PeriodView struct {
	Item   string
	Period Period
	Timing Timing
	// Budgeted is the budgeted amount of the period; Expected the amount expected instead, if it was set.
	Budgeted float64
	Expected *float64
	// Posted is what has posted: the recorded amount, or the estimate if PostedAuto.
	Posted     float64
	PostedAuto bool
	Status     OccurrenceStatus
	// Remaining is what is still to come in the period: what it is planned for less what has posted, never below zero, and nothing once the status is posted.
	Remaining float64
	// Variance is posted less budgeted in the period that has not closed; once it has (it ends before the cutoff), the expected amount if set, else posted, less budgeted.
	Variance float64
	Closed   bool
	// Overridden is set when an occurrence fact exists for the period.
	Overridden bool
	Override   Occurrence
}

// EvalPeriod works out the period of the item against the cutoff, given the period's budgeted amount and its occurrence fact, if any (nil for none). A lump counts as posted when its date is on or before the cutoff; a spread item in proportion to the days of its period up to the cutoff, both inclusive.
func EvalPeriod(it BudgetItem, p Period, budgeted float64, ov *Occurrence, cutoff Date) PeriodView {
	v := PeriodView{Item: it.ID, Period: p, Timing: it.EffectiveTiming(), Budgeted: budgeted, Status: StatusAuto, Closed: p.End < cutoff}
	var o Occurrence
	if ov != nil {
		o = *ov
		v.Overridden, v.Override, v.Expected = true, o.Clone(), clonePtr(o.Expected)
		if o.Status != "" {
			v.Status = o.Status
		}
	}
	planned := o.Planned(budgeted)
	auto := 0.0
	switch {
	case cutoff < p.Start:
	case v.Timing == Lump || p.End <= cutoff:
		auto = planned
	default:
		auto = planned * float64(int(cutoff-p.Start)+1) / float64(p.Days())
	}
	switch {
	case o.Posted != nil:
		v.Posted = *o.Posted
	case v.Status == StatusPosted:
		v.Posted = planned
	case v.Status == StatusPending:
	default:
		v.Posted, v.PostedAuto = auto, true
	}
	if v.Status != StatusPosted {
		v.Remaining = max(0, planned-v.Posted)
	}
	if v.Closed && o.Expected != nil {
		v.Variance = *o.Expected - budgeted
	} else {
		v.Variance = v.Posted - budgeted
	}
	return v
}

// PeriodHistory returns the n periods of the item up to and including the one containing d, oldest first, evaluated against the cutoff. budgeted gives the budgeted amount of a period.
func PeriodHistory(it BudgetItem, d Date, n int, book *OccurrenceBook, cutoff Date, budgeted func(Period) float64) []PeriodView {
	var out []PeriodView
	p, ok := it.PeriodAt(d)
	for ; ok && len(out) < n; p, ok = it.PeriodBefore(p) {
		var ov *Occurrence
		if o, found := book.Get(it.ID, p.Start); found {
			ov = &o
		}
		out = append(out, EvalPeriod(it, p, budgeted(p), ov, cutoff))
	}
	slices.Reverse(out)
	return out
}
