// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"testing"
)

type budgetTotals struct {
	AsOf            string  `json:"as_of"`
	MonthlyIncome   float64 `json:"monthly_income"`
	MonthlyExpenses float64 `json:"monthly_expenses"`
	MonthlyNet      float64 `json:"monthly_net"`
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// Scenario 1 through the CLI: the list shows every field of each item, and the totals on the demo are the hand-computed ones.
func TestCLIBudgetListAndTotals(t *testing.T) {
	var items []struct {
		ID         string
		Kind       string
		Amount     float64
		Indexed    bool
		Growth     *float64
		Start, End *string
		Account    *string
		From, To   *string
		Recurrence struct{ Kind string }
	}
	cliJSON(t, &items, "--demo", "budget", "list")
	kinds := map[string]int{}
	for _, it := range items {
		kinds[it.Kind]++
		if it.Growth == nil {
			t.Errorf("%s lists no growth", it.ID)
		}
		if it.ID == "salary-a" && (it.Recurrence.Kind != "semimonthly" || it.Amount != 5000 || !it.Indexed || it.Account == nil || *it.Account != "checking") {
			t.Errorf("salary-a = %+v", it)
		}
		if it.ID == "save-401k" && (it.Kind != "transfer" || it.From == nil || it.To == nil || it.Account != nil) {
			t.Errorf("save-401k = %+v", it)
		}
	}
	if kinds["income"] != 2 || kinds["expense"] != 7 || kinds["transfer"] != 9 {
		t.Errorf("kinds %v", kinds)
	}

	// Income: salary-a twice a month (2 x 5000) and salary-b every 2 weeks (3700 x 26 / 12). Expenses: groceries 1800, dining 900, utilities 450, childcare 1200, transport 600, insurance 700 and the vacation 6000 / 12. The transfers count in neither.
	var tot budgetTotals
	cliJSON(t, &tot, "--demo", "budget", "totals")
	if tot.AsOf != "2026-11-08" || !near(tot.MonthlyIncome, 18016.67) || !near(tot.MonthlyExpenses, 6150) || !near(tot.MonthlyNet, 11866.67) {
		t.Errorf("totals %+v", tot)
	}
}

// Scenario 2 through the CLI: add (dry run, then real), edit, end and delete move the totals, and deleting an item a projection modifies leaves that modification reported as missing.
func TestCLIBudgetEditsAndTotals(t *testing.T) {
	path := seededFile(t)
	totals := func() budgetTotals {
		var tot budgetTotals
		cliJSON(t, &tot, "--db", path, "budget", "totals")
		return tot
	}
	before := totals()

	w := data(t, path, "", "budget", "add", "--kind", "expense", "--name", "Gym", "--amount", "60", "--every", "monthly", "--on", "2026-01-10", "--account", "checking", "--indexed", "--growth", "0.03")
	if !w.Changed || w.Item.ID != "gym" || w.Item.Growth != 0.03 {
		t.Fatalf("add: %+v", w.Item)
	}
	if got := totals().MonthlyExpenses; !near(got, before.MonthlyExpenses+60) {
		t.Errorf("expenses after add %v", got)
	}
	data(t, path, "", "budget", "edit", "gym", "--amount", "80")
	if got := totals().MonthlyExpenses; !near(got, before.MonthlyExpenses+80) {
		t.Errorf("expenses after edit %v", got)
	}
	data(t, path, "", "budget", "end", "gym", "2026-11-01")
	if got := totals().MonthlyExpenses; !near(got, before.MonthlyExpenses) {
		t.Errorf("expenses after end %v", got)
	}
	if w := data(t, path, "", "budget", "delete", "gym"); w.Removed != "gym" {
		t.Errorf("delete: %+v", w)
	}

	// New job modifies the amount of salary-a.
	w = data(t, path, "", "budget", "delete", "salary-a")
	if w.Removed != "salary-a" {
		t.Fatalf("delete: %+v", w)
	}
	if got := totals().MonthlyIncome; !near(got, before.MonthlyIncome-10000) {
		t.Errorf("income after delete %v", got)
	}
	var proj struct {
		Modifications []struct {
			Target        struct{ ID string }
			Missing       bool
			MissingTarget string `json:"missing_target"`
		}
	}
	cliJSON(t, &proj, "--db", path, "projection", "show", "New job")
	found := false
	for _, m := range proj.Modifications {
		if m.Target.ID == "salary-a" {
			found = true
			if !m.Missing || m.MissingTarget != "budget item salary-a" {
				t.Errorf("modification of the deleted item: %+v", m)
			}
		}
	}
	if !found {
		t.Error("New job lost its modification of salary-a")
	}
}
