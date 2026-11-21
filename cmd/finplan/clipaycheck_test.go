// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import "testing"

type deductionRow struct {
	ID           string  `json:"id"`
	From         *string `json:"from"`
	To           *string `json:"to"`
	DeductedFrom *string `json:"deducted_from"`
}

func deductionRows(t *testing.T, path string) map[string]deductionRow {
	t.Helper()
	var list []deductionRow
	cliJSON(t, &list, "--db", path, "budget", "list")
	out := map[string]deductionRow{}
	for _, it := range list {
		out[it.ID] = it
	}
	return out
}

// The demo's 401k and TSP contributions are deducted from the salaries; the flag sets the wages item by id or name, none makes the transfer ordinary again, and the item must recur as the wages item does.
func TestCLIBudgetDeductedFromFlag(t *testing.T) {
	path := seededFile(t)
	rows := deductionRows(t, path)
	for id, salary := range map[string]string{"save-401k": "salary-a", "save-tsp": "salary-b"} {
		if r := rows[id]; r.DeductedFrom == nil || *r.DeductedFrom != salary || r.From != nil || r.To == nil {
			t.Errorf("%s: %+v, want deducted from %s with no source account", id, r, salary)
		}
	}
	if r := rows["save-roth"]; r.DeductedFrom != nil || r.From == nil {
		t.Errorf("save-roth: %+v, want an ordinary transfer", r)
	}

	write(t, path, "", "budget", "edit", "save-401k", "--deducted-from", "none", "--from", "checking")
	if r := deductionRows(t, path)["save-401k"]; r.DeductedFrom != nil || r.From == nil || *r.From != "checking" {
		t.Errorf("cleared: %+v, want a transfer from checking", r)
	}
	write(t, path, "", "budget", "edit", "save-401k", "--deducted-from", "Salary (Parent A)")
	if r := deductionRows(t, path)["save-401k"]; r.DeductedFrom == nil || *r.DeductedFrom != "salary-a" || r.From != nil {
		t.Errorf("set by name: %+v, want deducted from salary-a with no source account", r)
	}

	refusedWrite(t, path, "", 1, "invalid", "budget", "edit", "save-tsp", "--deducted-from", "salary-a")
	refusedWrite(t, path, "", 1, "invalid", "budget", "edit", "save-roth", "--deducted-from", "salary-a")
	refusedWrite(t, path, "", 1, "invalid", "budget", "edit", "groceries", "--deducted-from", "salary-a")

	// Deleting the paycheck leaves the deduction moving the money, out of the account the paychecks went into.
	write(t, path, "", "budget", "delete", "salary-a")
	if r := deductionRows(t, path)["save-401k"]; r.DeductedFrom != nil || r.From == nil || *r.From != "checking" {
		t.Errorf("after deleting its paycheck: %+v, want a transfer from checking", r)
	}
}

// A deduction is taken from the paycheck, so its period counts as posted at its budgeted amount with no transaction to wait for.
func TestCLIBudgetPeriodOfADeductionIsPosted(t *testing.T) {
	r := periodRows(t, seededFile(t))["save-401k"]
	if r.Budgeted != 360 || r.Posted != 360 || r.Remaining != 0 || r.Missing || r.Variance != 0 {
		t.Errorf("save-401k: %+v, want 360 posted with nothing to come", r)
	}
}
