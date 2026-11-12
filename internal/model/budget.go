// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"slices"
	"time"
)

// ItemKind classifies a budget item.
type ItemKind string

// Budget item kinds.
const (
	ItemIncome   ItemKind = "income"
	ItemExpense  ItemKind = "expense"
	ItemTransfer ItemKind = "transfer"
)

// IncomeType says how an income item is taxed. The zero value of an income item is wages.
type IncomeType string

// Income types.
const (
	// IncomeWages is pay subject to income tax and payroll tax.
	IncomeWages IncomeType = "wages"
	// IncomeOrdinary is other taxable income, such as interest: income tax but no payroll tax.
	IncomeOrdinary IncomeType = "ordinary"
	// IncomeTaxFree is not taxed.
	IncomeTaxFree IncomeType = "tax-free"
)

// IncomeTypes lists the income types.
var IncomeTypes = []IncomeType{IncomeWages, IncomeOrdinary, IncomeTaxFree}

// Valid reports whether t is one of the income types.
func (t IncomeType) Valid() bool { return slices.Contains(IncomeTypes, t) }

// RecurrenceKind is a calendar rule.
type RecurrenceKind string

// Recurrence kinds. Annual is shown as "Annually".
const (
	Weekly      RecurrenceKind = "weekly"
	Biweekly    RecurrenceKind = "biweekly"
	Semimonthly RecurrenceKind = "semimonthly"
	Monthly     RecurrenceKind = "monthly"
	Quarterly   RecurrenceKind = "quarterly"
	Annual      RecurrenceKind = "annual"
	OneTime     RecurrenceKind = "one-time"
	Custom      RecurrenceKind = "custom"
)

// RecurrenceUnit is the unit of a custom recurrence's Count.
type RecurrenceUnit string

// Recurrence units.
const (
	UnitDays   RecurrenceUnit = "days"
	UnitWeeks  RecurrenceUnit = "weeks"
	UnitMonths RecurrenceUnit = "months"
	UnitYears  RecurrenceUnit = "years"
)

// Valid reports whether the unit is one of the four.
func (u RecurrenceUnit) Valid() bool {
	return u == UnitDays || u == UnitWeeks || u == UnitMonths || u == UnitYears
}

// Recurrence says on which dates an item happens.
//
//	OneTime:     Anchor only
//	Weekly:      every 7 days from the phase
//	Biweekly:    every 14 days from the phase
//	Semimonthly: Day and Day2 of each month
//	Monthly:     Day of each month
//	Quarterly:   every 3 months from the phase
//	Annual:      Day of Month each year
//	Custom:      every Count Units from the phase
//
// The phase of an interval kind is Anchor when set, else the item's Start (see BudgetItem.Dates). Month-based intervals keep the phase's day of month and a day past the end of a month falls on the month's last day, without drifting afterwards. Count and Unit are for Custom only.
type Recurrence struct {
	Kind   RecurrenceKind
	Anchor Date
	Month  time.Month
	Day    int
	Day2   int
	Count  int
	Unit   RecurrenceUnit
}

// interval returns the step of an interval kind: days, or months when inMonths. It returns 0 for a kind that has none or a custom recurrence that is not valid.
func (r Recurrence) interval() (n int, inMonths bool) {
	switch r.Kind {
	case Weekly:
		return 7, false
	case Biweekly:
		return 14, false
	case Quarterly:
		return 3, true
	case Custom:
		if r.Count < 1 {
			return 0, false
		}
		switch r.Unit {
		case UnitDays:
			return r.Count, false
		case UnitWeeks:
			return 7 * r.Count, false
		case UnitMonths:
			return r.Count, true
		case UnitYears:
			return 12 * r.Count, true
		}
	}
	return 0, false
}

// Between returns the dates after..upTo in order, excluding after and including upTo. An interval kind steps from Anchor.
func (r Recurrence) Between(after, upTo Date) []Date {
	if upTo <= after {
		return nil
	}
	var out []Date
	switch r.Kind {
	case OneTime:
		if r.Anchor > after && r.Anchor <= upTo {
			out = append(out, r.Anchor)
		}
	case Weekly, Biweekly, Quarterly, Custom:
		n, inMonths := r.interval()
		if n == 0 {
			return nil
		}
		if !inMonths {
			k := 0
			if after >= r.Anchor {
				k = int(after-r.Anchor)/n + 1
			}
			for d := r.Anchor.Add(n * k); d <= upTo; d = d.Add(n) {
				out = append(out, d)
			}
			return out
		}
		ay, am, ad := r.Anchor.YMD()
		k := 0
		if after >= r.Anchor {
			fy, fm, _ := after.YMD()
			k = max(0, ((fy-ay)*12+int(fm-am))/n-1)
		}
		for ; ; k++ {
			total := ay*12 + int(am) - 1 + k*n
			y, m := total/12, time.Month(total%12+1)
			d := D(y, m, min(ad, daysInMonth(y, m)))
			if d > upTo {
				break
			}
			if d > after {
				out = append(out, d)
			}
		}
	case Monthly, Semimonthly, Annual:
		y, m, _ := after.Add(1).YMD()
		for ; D(y, m, 1) <= upTo; m++ {
			if m > 12 {
				m, y = 1, y+1
			}
			days := []int{r.Day}
			switch r.Kind {
			case Semimonthly:
				days = append(days, r.Day2)
				slices.Sort(days)
			case Annual:
				if m != r.Month {
					days = nil
				}
			}
			last := MinDate
			for _, day := range days {
				d := D(y, m, min(day, daysInMonth(y, m)))
				if d > after && d <= upTo && d != last {
					out = append(out, d)
				}
				last = d
			}
		}
	}
	return out
}

// BudgetItem is one forecast flow. Amount is a positive magnitude: an income item lands in Account,
// an expense draws from it, and a transfer moves money from From to To. An account named in a
// transfer need not exist in the record; it stands for the outside.
type BudgetItem struct {
	ID   string
	Name string
	Kind ItemKind
	// Amount is the positive magnitude over time; a single number is a one-segment schedule.
	Amount     Schedule
	Recurrence Recurrence
	Start, End *Date
	// Indexed items grow with inflation from the record's Today.
	Indexed bool
	// Growth is the annual raise as a fraction over time: the amount steps up by the rate in effect once a year on the anniversary of the item's start (see engine raises). An empty schedule means no raises.
	Growth   Schedule
	Account  AccountID
	From, To AccountID
	// IncomeType is how an income item is taxed; empty reads as wages, and it is empty for the other kinds.
	IncomeType IncomeType
	// MatchPercent, MatchUpTo and MatchAccount are the employer match of a wages item: on each paycheck the employer deposits MatchPercent percent of the employee's contributions to MatchAccount, up to MatchUpTo percent of the paycheck's gross. Zero or an empty account means no match.
	MatchPercent, MatchUpTo float64
	MatchAccount            AccountID
	// Timing says when the item's amount is spent within a period; empty reads as the item's default (see EffectiveTiming).
	Timing Timing
}

// Income returns how the item is taxed: its income type, wages when it has none. It is meaningful for an income item only.
func (b BudgetItem) Income() IncomeType {
	if b.IncomeType == "" {
		return IncomeWages
	}
	return b.IncomeType
}

// Dates returns the dates after..upTo on which the item recurs; interval kinds take their phase from the recurrence's anchor, or from the item's start when it has none.
func (b BudgetItem) Dates(after, upTo Date) []Date {
	r := b.Recurrence
	if r.Anchor == 0 && b.Start != nil {
		r.Anchor = *b.Start
	}
	return r.Between(after, upTo)
}

// Touches reports whether the item changes the account's balance.
func (b BudgetItem) Touches(id AccountID) bool {
	if b.Kind == ItemTransfer {
		return b.From != b.To && (b.From == id || b.To == id)
	}
	return b.Account == id
}

// Budget is the base forecast of every flow.
type Budget struct {
	Items []BudgetItem
}

// Item returns the item with the id.
func (b Budget) Item(id string) (BudgetItem, bool) {
	for _, it := range b.Items {
		if it.ID == id {
			return it, true
		}
	}
	return BudgetItem{}, false
}
