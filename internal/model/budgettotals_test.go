// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"math"
	"testing"
)

func TestMonthlyEquivalent(t *testing.T) {
	for _, c := range []struct {
		name string
		r    Recurrence
		want float64
	}{
		{"monthly", Recurrence{Kind: Monthly}, 1200},
		{"semimonthly", Recurrence{Kind: Semimonthly}, 2400},
		{"biweekly", Recurrence{Kind: Biweekly}, 2600},
		{"weekly", Recurrence{Kind: Weekly}, 1200 * 4.348},
		{"quarterly", Recurrence{Kind: Quarterly}, 400},
		{"annual", Recurrence{Kind: Annual}, 100},
		{"one-time", Recurrence{Kind: OneTime}, 0},
		{"every 10 days", Recurrence{Kind: Custom, Count: 10, Unit: UnitDays}, 1200 * 30.4375 / 10},
		{"every 3 weeks", Recurrence{Kind: Custom, Count: 3, Unit: UnitWeeks}, 1200 * 4.348 / 3},
		{"every 2 months", Recurrence{Kind: Custom, Count: 2, Unit: UnitMonths}, 600},
		{"every 2 years", Recurrence{Kind: Custom, Count: 2, Unit: UnitYears}, 50},
	} {
		got := BudgetItem{Amount: Constant(1200), Recurrence: c.r}.MonthlyEquivalent(D(2026, 11, 9))
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMonthlyEquivalentUsesAmountInEffect(t *testing.T) {
	it := BudgetItem{Amount: MustSchedule(Segment{From: MinDate, End: Ptr(D(2029, 7, 31)), Value: 1400}, Segment{From: D(2029, 8, 1), Value: 600}), Recurrence: Recurrence{Kind: Monthly, Day: 1}}
	for _, c := range []struct {
		on   Date
		want float64
	}{{D(2026, 11, 9), 1400}, {D(2029, 8, 1), 600}} {
		if got := it.MonthlyEquivalent(c.on); got != c.want {
			t.Errorf("%v: %v, want %v", c.on, got, c.want)
		}
	}
	it.Amount = MustSchedule(Segment{From: D(2027, 1, 1), Value: 50})
	if got := it.MonthlyEquivalent(D(2026, 11, 9)); got != 0 {
		t.Errorf("no amount in effect: %v, want 0", got)
	}
}

func TestMonthlyTotals(t *testing.T) {
	today := D(2026, 11, 9)
	monthly := Recurrence{Kind: Monthly, Day: 1}
	b := Budget{Items: []BudgetItem{
		{Kind: ItemIncome, Amount: Constant(1000), Recurrence: monthly},
		{Kind: ItemIncome, Amount: Constant(500), Recurrence: monthly, Start: Ptr(D(2026, 12, 1))},
		{Kind: ItemExpense, Amount: Constant(300), Recurrence: monthly, End: Ptr(today)},
		{Kind: ItemExpense, Amount: Constant(200), Recurrence: monthly, End: Ptr(D(2026, 11, 8))},
		{Kind: ItemExpense, Amount: Constant(1200), Recurrence: Recurrence{Kind: Annual, Month: 6, Day: 1}},
		{Kind: ItemExpense, Amount: Constant(900), Recurrence: Recurrence{Kind: OneTime, Anchor: today}},
		{Kind: ItemTransfer, Amount: Constant(700), Recurrence: monthly},
		{Kind: ItemIncome, Amount: Constant(3000), Recurrence: Recurrence{Kind: Biweekly, Anchor: D(2027, 1, 8)}},
		{Kind: ItemExpense, Amount: Constant(1200), Recurrence: Recurrence{Kind: Biweekly, Anchor: today}},
	}}
	income, expenses := b.MonthlyTotals(today)
	if income != 1000 || expenses != 400+2600 {
		t.Errorf("income %v, expenses %v; want 1000 and 3000", income, expenses)
	}
}
