// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

type collegeAccount struct {
	ID          string
	Type        string
	Beneficiary *string
	Basis       *float64
}

func collegeAccounts(t *testing.T, path string) map[string]collegeAccount {
	t.Helper()
	var list []collegeAccount
	cliJSON(t, &list, "--db", path, "account", "list")
	out := map[string]collegeAccount{}
	for _, a := range list {
		out[a.ID] = a
	}
	return out
}

func TestCLIBeneficiaryAndBasis(t *testing.T) {
	path := seededFile(t)
	accts := collegeAccounts(t, path)
	for id, child := range map[string]string{"529-oldest": "Child 1", "529-middle": "Child 2", "529-youngest": "Child 3", "529-child4": "Child 4"} {
		if a := accts[id]; a.Beneficiary == nil || *a.Beneficiary != child || a.Basis == nil {
			t.Errorf("%s: %+v", id, a)
		}
	}
	if a := accts["checking"]; a.Beneficiary != nil || a.Basis != nil {
		t.Errorf("checking: %+v", a)
	}

	data(t, path, "", "account", "edit", "529-child4", "--beneficiary", "Child 3", "--basis", "1234.5")
	if a := collegeAccounts(t, path)["529-child4"]; *a.Beneficiary != "Child 3" || *a.Basis != 1234.5 {
		t.Errorf("after edit: %+v", a)
	}
	data(t, path, "", "account", "edit", "529-child4", "--clear-beneficiary", "--clear-basis")
	if a := collegeAccounts(t, path)["529-child4"]; a.Beneficiary != nil || a.Basis != nil {
		t.Errorf("after clearing: %+v", a)
	}
	data(t, path, "", "account", "edit", "529-child4", "--beneficiary", "Child 4")
	data(t, path, "", "account", "edit", "529-child4", "--beneficiary", "")
	if a := collegeAccounts(t, path)["529-child4"]; a.Beneficiary != nil {
		t.Errorf("an empty name clears: %+v", a)
	}
	data(t, path, "", "account", "new", "Fund", "--type", "529", "--tax", "529", "--beneficiary", "Child 2", "--basis", "10")
	if a := collegeAccounts(t, path)["fund"]; a.Beneficiary == nil || *a.Beneficiary != "Child 2" || *a.Basis != 10 {
		t.Errorf("new 529: %+v", a)
	}

	refusedWrite(t, path, "", 1, "not_found", "account", "edit", "529-child4", "--beneficiary", "Nobody")
	refusedWrite(t, path, "", 1, "invalid", "account", "edit", "529-child4", "--beneficiary", "Parent A")
	refusedWrite(t, path, "", 1, "invalid", "account", "edit", "529-child4", "--basis", "-5")
	refusedWrite(t, path, "", 1, "invalid", "account", "edit", "checking", "--beneficiary", "Child 1")
	refusedWrite(t, path, "", 1, "invalid", "account", "edit", "529-oldest", "--type", "savings")
	refusedWrite(t, path, "", 2, "usage", "account", "edit", "529-oldest", "--beneficiary", "Child 2", "--clear-beneficiary")

	// A member's rename follows, and removing the member clears the beneficiary and reports the account.
	data(t, path, "", "household", "member", "edit", "Child 1", "--name", "Alex")
	if a := collegeAccounts(t, path)["529-oldest"]; *a.Beneficiary != "Alex" {
		t.Errorf("after the rename: %+v", a)
	}
	w := data(t, path, "", "household", "member", "remove", "Alex", "--force")
	if len(w.Affected) != 1 || w.Affected[0].Kind != "beneficiary" || w.Affected[0].In != "529-oldest" {
		t.Errorf("affected %+v", w.Affected)
	}
	if a := collegeAccounts(t, path)["529-oldest"]; a.Beneficiary != nil {
		t.Errorf("after the removal: %+v", a)
	}
}

type collegeBalance struct {
	Samples []struct {
		Date  string
		Value float64
	}
	Diagnostics struct{ Notes []string }
}

// Scenario 1: the demo's oldest child's bills draw the 529 to empty, and checking pays the rest.
func TestCLICollegeCostsDrawThe529ThenChecking(t *testing.T) {
	var b collegeBalance
	cliJSON(t, &b, "--demo", "balance", "529-oldest", "--from", "2026-12-31", "--to", "2030-01-31", "--step", "year")
	if len(b.Samples) < 3 || b.Samples[0].Value <= 0 || b.Samples[1].Value >= b.Samples[0].Value {
		t.Fatalf("balances %+v", b.Samples)
	}
	last := b.Samples[len(b.Samples)-1]
	if last.Value < 0 || last.Value > 2000 {
		t.Errorf("the 529 on %s holds %v, want it drawn down to a little", last.Date, last.Value)
	}
	var chk collegeBalance
	cliJSON(t, &chk, "--demo", "balance", "checking", "--from", "2026-12-31", "--to", "2030-01-31", "--step", "year")
	found := false
	for _, n := range chk.Diagnostics.Notes {
		found = found || (strings.Contains(n, "529 oldest ran out;") && strings.Contains(n, "Child 1 college"))
	}
	if !found {
		t.Errorf("notes %q lack the 529 running out", chk.Diagnostics.Notes)
	}
}

type collegeTaxYear struct {
	Inputs struct {
		NonQualified float64 `json:"nonqualified_529"`
	}
	Tax struct {
		Penalty float64 `json:"early_withdrawal_penalty"`
	}
}

// Scenario 2: a transfer out of a 529 to checking pays tax and the additional tax on the earnings only.
func TestCLINonQualifiedWithdrawalPaysTaxAndPenaltyOnEarnings(t *testing.T) {
	path := seededFile(t)
	var before collegeTaxYear
	cliJSON(t, &before, "--db", path, "tax", "year", "2027")
	if before.Inputs.NonQualified != 0 {
		t.Fatalf("no withdrawal yet: %+v", before)
	}
	data(t, path, "", "budget", "add", "--kind", "transfer", "--name", "Fund a trip", "--amount", "10000", "--every", "one-time", "--on", "2027-06-01", "--from", "529-youngest", "--to", "checking")
	var after collegeTaxYear
	cliJSON(t, &after, "--db", path, "tax", "year", "2027")
	if after.Inputs.NonQualified <= 0 || after.Inputs.NonQualified >= 10000 {
		t.Fatalf("earnings %v, want a part of the 10000", after.Inputs.NonQualified)
	}
	if got, want := after.Tax.Penalty-before.Tax.Penalty, 0.1*after.Inputs.NonQualified; got < want-0.02 || got > want+0.02 {
		t.Errorf("penalty %v, want %v", got, want)
	}

	var c struct {
		Penalty float64 `json:"early_withdrawal_penalty"`
		AGI     float64 `json:"agi"`
	}
	cliJSON(t, &c, "--db", path, "tax", "compute", "--year", "2027", "--nonqualified-529", "2000")
	if c.Penalty != 200 || c.AGI != 2000 {
		t.Errorf("tax compute: %+v", c)
	}
}
