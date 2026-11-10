// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

// Average months per week and per day, for the monthly equivalent of weekly and daily frequencies.
const (
	weeksPerMonth = 4.348
	daysPerMonth  = 30.4375
)

// MonthlyEquivalent is what the item amounts to in an average month on the date, in today's dollars: the amount in effect then, with raises and inflation not applied, and zero if there is none. A one-time item happens once and contributes nothing. A custom frequency counts by its average: every N days is 30.4375/N a month, N weeks 4.348/N, N months 1/N, N years 1/(12N).
func (b BudgetItem) MonthlyEquivalent(on Date) float64 {
	amt, _ := b.Amount.At(on)
	r := b.Recurrence
	switch r.Kind {
	case Weekly:
		return amt * weeksPerMonth
	case Monthly:
		return amt
	case Semimonthly:
		return 2 * amt
	case Biweekly:
		return amt * 26 / 12
	case Quarterly:
		return amt / 3
	case Annual:
		return amt / 12
	case Custom:
		if r.Count < 1 {
			return 0
		}
		n := float64(r.Count)
		switch r.Unit {
		case UnitDays:
			return amt * daysPerMonth / n
		case UnitWeeks:
			return amt * weeksPerMonth / n
		case UnitMonths:
			return amt / n
		case UnitYears:
			return amt / (12 * n)
		}
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
			income += it.MonthlyEquivalent(on)
		case ItemExpense:
			expenses += it.MonthlyEquivalent(on)
		}
	}
	return income, expenses
}
