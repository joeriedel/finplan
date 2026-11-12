// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import "testing"

func TestDefaultTiming(t *testing.T) {
	for _, c := range []struct {
		name string
		it   BudgetItem
		want Timing
	}{
		{"monthly expense", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Monthly, Day: 3}}, Spread},
		{"weekly expense", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Weekly}}, Spread},
		{"biweekly expense", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Biweekly}}, Spread},
		{"semimonthly expense", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Semimonthly, Day: 1, Day2: 15}}, Spread},
		{"custom month", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Custom, Count: 1, Unit: UnitMonths}}, Spread},
		{"custom 10 days", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Custom, Count: 10, Unit: UnitDays}}, Spread},
		{"custom 2 months", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Custom, Count: 2, Unit: UnitMonths}}, Lump},
		{"quarterly expense", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Quarterly}}, Lump},
		{"annual expense", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Annual, Month: 6, Day: 1}}, Lump},
		{"one-time expense", BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: OneTime}}, Lump},
		{"monthly income", BudgetItem{Kind: ItemIncome, Recurrence: Recurrence{Kind: Monthly, Day: 1}}, Lump},
		{"monthly transfer", BudgetItem{Kind: ItemTransfer, Recurrence: Recurrence{Kind: Monthly, Day: 1}}, Lump},
	} {
		if got := c.it.EffectiveTiming(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if it := (BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: Monthly, Day: 3}, Timing: Lump}); it.EffectiveTiming() != Lump {
		t.Error("a stored timing does not win")
	}
}

func TestPeriods(t *testing.T) {
	it := BudgetItem{ID: "x", Kind: ItemExpense, Recurrence: Recurrence{Kind: Monthly, Day: 3}, Start: Ptr(D(2026, 1, 1))}
	p, ok := it.PeriodAt(D(2026, 11, 12))
	if !ok || p != (Period{D(2026, 11, 3), D(2026, 12, 2)}) || p.Days() != 30 {
		t.Errorf("period %+v, %v", p, ok)
	}
	// A date on an occurrence starts its period; the day before ends the one before.
	if p, _ := it.PeriodAt(D(2026, 11, 3)); p.Start != D(2026, 11, 3) {
		t.Errorf("period on the occurrence %+v", p)
	}
	prev, ok := it.PeriodBefore(p)
	if !ok || prev != (Period{D(2026, 10, 3), D(2026, 11, 2)}) {
		t.Errorf("previous %+v, %v", prev, ok)
	}
	if _, ok := it.PeriodAt(D(2025, 12, 31)); ok {
		t.Error("a period before the item's start")
	}
	one := BudgetItem{Kind: ItemExpense, Recurrence: Recurrence{Kind: OneTime, Anchor: D(2026, 5, 5)}}
	if p, ok := one.PeriodAt(D(2026, 6, 1)); !ok || p != (Period{D(2026, 5, 5), D(2026, 5, 5)}) {
		t.Errorf("one-time period %+v, %v", p, ok)
	}
}

func TestEvalPeriod(t *testing.T) {
	spread := BudgetItem{ID: "s", Kind: ItemExpense, Timing: Spread}
	lump := BudgetItem{ID: "l", Kind: ItemIncome, Timing: Lump}
	p := Period{D(2026, 1, 1), D(2026, 1, 10)}
	v := EvalPeriod(spread, p, 100, nil, D(2026, 1, 4))
	if !v.PostedAuto || v.Posted != 40 || v.Remaining != 60 || v.Variance != -60 || v.Closed {
		t.Errorf("spread %+v", v)
	}
	v = EvalPeriod(lump, p, 100, nil, D(2026, 1, 4))
	if v.Posted != 100 || v.Remaining != 0 {
		t.Errorf("lump on or before the cutoff %+v", v)
	}
	v = EvalPeriod(lump, p, 100, nil, D(2025, 12, 31))
	if v.Posted != 0 || v.Remaining != 100 {
		t.Errorf("lump after the cutoff %+v", v)
	}
	v = EvalPeriod(lump, p, 100, &Occurrence{Status: StatusPending, Posted: Ptr(30.0)}, D(2026, 1, 4))
	if v.PostedAuto || v.Posted != 30 || v.Remaining != 70 {
		t.Errorf("pending %+v", v)
	}
	v = EvalPeriod(spread, p, 100, &Occurrence{Expected: Ptr(150.0)}, D(2026, 1, 20))
	if !v.Closed || v.Posted != 150 || v.Variance != 50 || v.Remaining != 0 {
		t.Errorf("closed with an expected amount %+v", v)
	}
}

func TestOccurrenceBookKeepsTheLatestAndClones(t *testing.T) {
	var b OccurrenceBook
	b.Add(Occurrence{ID: 1, Item: "a", Period: 5, Status: StatusPending})
	b.Add(Occurrence{ID: 2, Item: "a", Period: 5, Status: StatusPosted, Posted: Ptr(1.0)})
	if o, ok := b.Get("a", 5); !ok || o.ID != 2 || len(b.All()) != 1 {
		t.Errorf("get %+v, %v", o, ok)
	}
	c := b.clone()
	*c.by[occurrenceKey{"a", 5}].Posted = 9
	if o, _ := b.Get("a", 5); *o.Posted != 1 {
		t.Error("the clone shares an amount with the original")
	}
	var none *OccurrenceBook
	if _, ok := none.Get("a", 5); ok || none.All() != nil || none.clone() != nil {
		t.Error("a nil book is not empty")
	}
}
