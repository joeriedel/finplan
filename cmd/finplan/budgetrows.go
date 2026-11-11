// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// budgetRow is one base budget item as the Budget tab shows it, every column as text.
type budgetRow struct {
	ID, Name, Schedule, Amount, Accounts, Start, End, Indexed, Growth, Monthly string
}

// budgetHeaders are the column titles of the budget rows, in the order of the cells of budgetRow after its ID.
var budgetHeaders = []string{"Name", "Repeats", "Amount", "Accounts", "Starts", "Ends", "Indexed", "Growth", "Per month"}

// formatDollars writes an amount as "$5,000", keeping the cents when there are any: "$1,234.50".
func formatDollars(v float64) string {
	cents := int64(math.Round(math.Abs(v) * 100))
	s := graph.FormatMoney(float64(cents / 100))
	if v < 0 && cents != 0 {
		s = "-" + s
	}
	if c := cents % 100; c != 0 {
		s += fmt.Sprintf(".%02d", c)
	}
	return s
}

// budgetGroup is the base budget items of one kind, in the budget's order.
type budgetGroup struct {
	Kind  model.ItemKind
	Title string
	Rows  []budgetRow
}

// budgetGroups groups the record's base budget items by kind: income, expense, then transfer. A kind without items still has its group.
func budgetGroups(rec *model.Record) []budgetGroup {
	groups := make([]budgetGroup, len(formItemKinds))
	for i, k := range formItemKinds {
		groups[i] = budgetGroup{Kind: k, Title: itemKindName(k)}
	}
	for _, it := range rec.Budget.Items {
		for i := range groups {
			if groups[i].Kind == it.Kind {
				groups[i].Rows = append(groups[i].Rows, budgetRowOf(rec, it))
			}
		}
	}
	return groups
}

// budgetRowOf describes the item for the Budget tab.
func budgetRowOf(rec *model.Record, it model.BudgetItem) budgetRow {
	row := budgetRow{ID: it.ID, Name: it.Name, Schedule: scheduleText(it.Recurrence), Amount: formatDollars(valueNow(it.Amount, rec.Today)), Start: "-", End: "-"}
	if it.Kind == model.ItemTransfer {
		row.Accounts = budgetAccountName(rec, it.From) + " to " + budgetAccountName(rec, it.To)
	} else {
		row.Accounts = budgetAccountName(rec, it.Account)
	}
	if it.Start != nil {
		row.Start = it.Start.String()
	}
	if it.End != nil {
		row.End = it.End.String()
	}
	if it.Indexed {
		row.Indexed = "indexed"
	}
	if g := valueNow(it.Growth, rec.Today); g != 0 {
		row.Growth = formatValue(model.PropGrowth, model.ModeSet, g) + " raise"
	}
	if m := it.MonthlyEquivalent(rec.Today); m != 0 {
		row.Monthly = formatDollars(m)
	}
	return row
}

// budgetAccountName names an account of an item: the outside for a transfer's end beyond the record, else the account's name.
func budgetAccountName(rec *model.Record, id model.AccountID) string {
	if id == outsideAccount {
		return "outside"
	}
	return accountName(rec, id)
}

// scheduleText says when the recurrence happens.
func scheduleText(r model.Recurrence) string {
	day := func(d int) string { return strconv.Itoa(d) }
	switch r.Kind {
	case model.Monthly:
		return "monthly on day " + day(r.Day)
	case model.Semimonthly:
		return "twice a month, days " + day(r.Day) + " and " + day(r.Day2)
	case model.Biweekly:
		return "every 2 weeks from " + r.Anchor.String()
	case model.Annual:
		return "annually on " + r.Month.String() + " " + day(r.Day)
	case model.Weekly:
		return "every week"
	case model.Quarterly:
		return "quarterly"
	case model.Custom:
		unit := string(r.Unit)
		if r.Count == 1 {
			unit = strings.TrimSuffix(unit, "s")
			return "every " + unit
		}
		return "every " + strconv.Itoa(r.Count) + " " + unit
	case model.OneTime:
		return "once on " + r.Anchor.String()
	}
	return string(r.Kind)
}

// budgetTotalsText says the monthly income, expenses and what is left of them for the record's today.
func budgetTotalsText(rec *model.Record) (income, expenses, net string) {
	in, out := rec.Budget.MonthlyTotals(rec.Today)
	return graph.FormatMoney(in), graph.FormatMoney(out), graph.FormatMoney(in - out)
}
