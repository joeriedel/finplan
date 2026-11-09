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

// RecurrenceKind is a calendar rule.
type RecurrenceKind string

// Recurrence kinds.
const (
	Biweekly    RecurrenceKind = "biweekly"
	Semimonthly RecurrenceKind = "semimonthly"
	Monthly     RecurrenceKind = "monthly"
	Annual      RecurrenceKind = "annual"
	OneTime     RecurrenceKind = "one-time"
)

// Recurrence says on which dates an item happens.
//
//	Biweekly:    every 14 days from Anchor
//	Semimonthly: Day and Day2 of each month
//	Monthly:     Day of each month
//	Annual:      Day of Month each year
//	OneTime:     Anchor only
//
// A day past the end of a month falls on the month's last day.
type Recurrence struct {
	Kind   RecurrenceKind
	Anchor Date
	Month  time.Month
	Day    int
	Day2   int
}

// Between returns the dates after..upTo in order, excluding after and including upTo.
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
	case Biweekly:
		k := 0
		if after >= r.Anchor {
			k = int(after-r.Anchor)/14 + 1
		}
		for d := r.Anchor.Add(14 * k); d <= upTo; d = d.Add(14) {
			out = append(out, d)
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
	ID         string
	Name       string
	Kind       ItemKind
	Amount     float64
	Recurrence Recurrence
	Start, End *Date
	// Indexed items grow with inflation from the record's Today.
	Indexed  bool
	Account  AccountID
	From, To AccountID
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
