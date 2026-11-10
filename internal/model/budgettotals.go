// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

// MonthlyEquivalent is what the item amounts to in an average month, in today's dollars: raises and inflation are not applied. A one-time item happens once and contributes nothing.
func (b BudgetItem) MonthlyEquivalent() float64 {
	switch b.Recurrence.Kind {
	case Monthly:
		return b.Amount
	case Semimonthly:
		return 2 * b.Amount
	case Biweekly:
		return b.Amount * 26 / 12
	case Annual:
		return b.Amount / 12
	}
	return 0
}

// ActiveOn reports whether the date is within the item's start and end, either of which may be absent. A biweekly item also is not active before its recurrence anchor, which is where its payments begin.
func (b BudgetItem) ActiveOn(d Date) bool {
	if b.Recurrence.Kind == Biweekly && b.Recurrence.Anchor != 0 && b.Recurrence.Anchor > d {
		return false
	}
	return (b.Start == nil || *b.Start <= d) && (b.End == nil || *b.End >= d)
}

// MonthlyTotals sums the monthly equivalents of the income items and of the expense items active on the date. Transfers are in neither.
func (b Budget) MonthlyTotals(on Date) (income, expenses float64) {
	for _, it := range b.Items {
		if !it.ActiveOn(on) {
			continue
		}
		switch it.Kind {
		case ItemIncome:
			income += it.MonthlyEquivalent()
		case ItemExpense:
			expenses += it.MonthlyEquivalent()
		}
	}
	return income, expenses
}
