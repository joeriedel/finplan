// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"math"
	"testing"
)

func TestMonthlyEquivalent(t *testing.T) {
	for _, c := range []struct {
		kind RecurrenceKind
		want float64
	}{
		{Monthly, 1200}, {Semimonthly, 2400}, {Biweekly, 2600}, {Annual, 100}, {OneTime, 0},
	} {
		got := BudgetItem{Amount: 1200, Recurrence: Recurrence{Kind: c.kind}}.MonthlyEquivalent()
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", c.kind, got, c.want)
		}
	}
}

func TestMonthlyTotals(t *testing.T) {
	today := D(2026, 11, 9)
	monthly := Recurrence{Kind: Monthly, Day: 1}
	b := Budget{Items: []BudgetItem{
		{Kind: ItemIncome, Amount: 1000, Recurrence: monthly},
		{Kind: ItemIncome, Amount: 500, Recurrence: monthly, Start: Ptr(D(2026, 12, 1))},
		{Kind: ItemExpense, Amount: 300, Recurrence: monthly, End: Ptr(today)},
		{Kind: ItemExpense, Amount: 200, Recurrence: monthly, End: Ptr(D(2026, 11, 8))},
		{Kind: ItemExpense, Amount: 1200, Recurrence: Recurrence{Kind: Annual, Month: 6, Day: 1}},
		{Kind: ItemExpense, Amount: 900, Recurrence: Recurrence{Kind: OneTime, Anchor: today}},
		{Kind: ItemTransfer, Amount: 700, Recurrence: monthly},
		{Kind: ItemIncome, Amount: 3000, Recurrence: Recurrence{Kind: Biweekly, Anchor: D(2027, 1, 8)}},
		{Kind: ItemExpense, Amount: 1200, Recurrence: Recurrence{Kind: Biweekly, Anchor: today}},
	}}
	income, expenses := b.MonthlyTotals(today)
	if income != 1000 || expenses != 400+2600 {
		t.Errorf("income %v, expenses %v; want 1000 and 3000", income, expenses)
	}
}
