// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"testing"

	"github.com/joeriedel/finplan/internal/model"
)

type shownAccounts struct {
	DryRun   bool `json:"dry_run"`
	Changed  bool
	Accounts []struct {
		ID, Name, Type, Tax string
		Rate                float64 `json:"stated_rate"`
		Opening             float64
		From                string
	}
	Projection *struct{ Accounts []struct{ ID string } }
}

func projectionAccounts(t *testing.T, path, projection string) shownAccounts {
	t.Helper()
	var s shownAccounts
	cliJSON(t, &s, "--db", path, "projection", "show", projection)
	return s
}

// houseScenario adds "New job + house" under "New job": a home and a mortgage from 2027-06-01, a down payment out of checking that day and a monthly mortgage payment from the next month.
func houseScenario(t *testing.T, path string) {
	t.Helper()
	write(t, path, "", "projection", "new", "New job + house", "--parent", "New job")
	write(t, path, "", "projection", "add-account", "New job + house", "--id", "house", "--name", "New home", "--type", "property", "--tax", "taxable", "--rate", "0.03", "--opening", "400000", "--from", "2027-06-01")
	write(t, path, "", "projection", "add-account", "New job + house", "--id", "loan", "--name", "New mortgage", "--type", "mortgage", "--tax", "taxable", "--rate", "0.05", "--opening", "-320000", "--from", "2027-06-01")
	write(t, path, "", "item", "add", "New job + house", "--kind", "transfer", "--name", "Down payment", "--amount", "60000", "--every", "one-time", "--on", "2027-06-01", "--from", "checking", "--to", "outside")
	write(t, path, "", "item", "add", "New job + house", "--kind", "transfer", "--name", "Mortgage payment", "--amount", "2000", "--every", "monthly", "--on", "2027-07-01", "--from", "checking", "--to", "loan")
}

func TestCLIHouseScenario(t *testing.T) {
	path := seededFile(t)
	baseNW := lastValue(t, path, "Net worth", "Base", "2027-01", "2029-12")
	jobNW := lastValue(t, path, "Net worth", "New job", "2027-01", "2029-12")
	houseScenario(t, path)

	got := projectionAccounts(t, path, "New job + house")
	if len(got.Accounts) != 2 || got.Accounts[0].ID != "house" || got.Accounts[0].Opening != 400000 || got.Accounts[1].Type != "mortgage" || got.Accounts[1].Rate != 0.05 {
		t.Fatalf("projection show accounts: %+v", got.Accounts)
	}
	if n := len(projectionAccounts(t, path, "New job").Accounts); n != 0 {
		t.Errorf("New job has %d accounts of its own", n)
	}

	// Under Base and New job the numbers are as before, and the accounts do not exist.
	if v := lastValue(t, path, "Net worth", "Base", "2027-01", "2029-12"); v != baseNW {
		t.Errorf("Base net worth changed: %v, was %v", v, baseNW)
	}
	if v := lastValue(t, path, "Net worth", "New job", "2027-01", "2029-12"); v != jobNW {
		t.Errorf("New job net worth changed: %v, was %v", v, jobNW)
	}
	cliFail(t, 1, "--db", path, "balance", "house", "--from", "2027-06", "--to", "2027-06", "--projection", "New job")
	cliFail(t, 1, "--db", path, "balance", "house", "--from", "2027-06", "--to", "2027-06")

	// The home and the mortgage are in Net worth under the new projection: on the purchase date it is higher by their opening balances less the down payment.
	day := "2027-06-01"
	diff := lastValue(t, path, "Net worth", "New job + house", day, day) - lastValue(t, path, "Net worth", "New job", day, day)
	if !closeTo(diff, 400000-320000-60000) {
		t.Errorf("net worth on the purchase date is higher by %v, want 20000", diff)
	}
	if v := lastValue(t, path, "house", "New job + house", "2027-05-31", "2027-05-31"); v != 0 {
		t.Errorf("home before it exists: %v", v)
	}
	if v := lastValue(t, path, "house", "New job + house", day, day); !closeTo(v, 400000) {
		t.Errorf("home on the purchase date: %v", v)
	}
	// It grows at its rate: one year later the home is up by 3%, over exactly 365 days.
	if v, want := lastValue(t, path, "house", "New job + house", "2028-06-01", "2028-06-01"), 400000*math.Pow(1.03, 366/model.DaysPerYear); !closeTo(v, want) {
		t.Errorf("home a year on: got %v, want %v", v, want)
	}
	// The first payment lands a month after the purchase, on top of the interest.
	if v, want := lastValue(t, path, "loan", "New job + house", "2027-07-01", "2027-07-01"), -320000*math.Pow(1.05, 30/model.DaysPerYear)+2000; !closeTo(v, want) {
		t.Errorf("mortgage after the first payment: got %v, want %v", v, want)
	}
	// Checking pays the down payment and the mortgage payments.
	if got := breakdownLine(t, path, "checking", "New job + house", "2027-05-31", "2027-07-01", "transfer") - breakdownLine(t, path, "checking", "New job", "2027-05-31", "2027-07-01", "transfer"); !closeTo(got, -62000) {
		t.Errorf("checking transfers: %v, want -62000", got)
	}
	var starts []struct{ Account string }
	cliJSON(t, &starts, "--db", path, "starts", "--projection", "New job + house")
	if len(starts) != 15 {
		t.Errorf("starts under the new projection lists %d accounts, want 15", len(starts))
	}
	cliJSON(t, &starts, "--db", path, "starts")
	if len(starts) != 13 {
		t.Errorf("starts with no projection lists %d accounts, want 13", len(starts))
	}

	// A mod can aim at the projection's own account, and a child inherits it.
	write(t, path, "", "mod", "add", "New job + house", "--target", "account:New home", "--property", "growth", "--value", "0.0", "--from", "2028-01-01")
	write(t, path, "", "projection", "new", "Slump", "--parent", "New job + house")
	flat := lastValue(t, path, "house", "Slump", "2029-06-01", "2029-06-01")
	if v := lastValue(t, path, "house", "New job + house", "2029-06-01", "2029-06-01"); !closeTo(v, flat) || flat >= 400000*math.Pow(1.03, 2) {
		t.Errorf("home with flat prices from 2028: %v, in the child %v", v, flat)
	}
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "add", "New job", "--target", "account:house", "--property", "growth", "--value", "0.1")
}

// TestCLIFlowsOnPurchaseDate has a transfer into the home and the first mortgage payment on the purchase date itself: both sides of each count, so Net worth gains only the opening balances.
func TestCLIFlowsOnPurchaseDate(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Buy", "--parent", "New job")
	write(t, path, "", "projection", "add-account", "Buy", "--id", "house", "--name", "New home", "--type", "property", "--tax", "taxable", "--rate", "0.03", "--opening", "400000", "--from", "2027-06-01")
	write(t, path, "", "projection", "add-account", "Buy", "--id", "loan", "--name", "New mortgage", "--type", "mortgage", "--tax", "taxable", "--rate", "0.05", "--opening", "-320000", "--from", "2027-06-01")
	write(t, path, "", "item", "add", "Buy", "--kind", "transfer", "--name", "Deposit", "--amount", "5000", "--every", "one-time", "--on", "2027-06-01", "--from", "checking", "--to", "house")
	write(t, path, "", "item", "add", "Buy", "--kind", "transfer", "--name", "Mortgage payment", "--amount", "2000", "--every", "monthly", "--on", "2027-06-01", "--from", "checking", "--to", "loan")
	day := "2027-06-01"
	if v := lastValue(t, path, "house", "Buy", day, day); !closeTo(v, 405000) {
		t.Errorf("home on the purchase date: %v, want 405000", v)
	}
	if v := lastValue(t, path, "loan", "Buy", day, day); !closeTo(v, -318000) {
		t.Errorf("mortgage on the purchase date: %v, want -318000", v)
	}
	if diff := lastValue(t, path, "Net worth", "Buy", day, day) - lastValue(t, path, "Net worth", "New job", day, day); !closeTo(diff, 400000-320000) {
		t.Errorf("net worth is higher by %v, want 80000", diff)
	}
	if got := breakdownLine(t, path, "Net worth", "Buy", "2027-05-31", "2027-12-31", "unexplained"); got != 0 {
		t.Errorf("unexplained remainder %v", got)
	}
}

// TestCLIPaymentBeforePurchase has a monthly mortgage payment that starts before the purchase: it is not modelled before the loan exists, so checking is untouched until the purchase.
func TestCLIPaymentBeforePurchase(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Buy", "--parent", "New job")
	write(t, path, "", "projection", "add-account", "Buy", "--id", "loan", "--name", "New mortgage", "--type", "mortgage", "--tax", "taxable", "--rate", "0.05", "--opening", "-320000", "--from", "2027-06-01")
	write(t, path, "", "item", "add", "Buy", "--kind", "transfer", "--name", "Mortgage payment", "--amount", "2000", "--every", "monthly", "--on", "2027-01-01", "--from", "checking", "--to", "loan")
	for _, day := range []string{"2027-03-01", "2027-05-31"} {
		if a, b := lastValue(t, path, "checking", "Buy", day, day), lastValue(t, path, "checking", "New job", day, day); !closeTo(a, b) {
			t.Errorf("checking on %s: %v, want %v", day, a, b)
		}
	}
	if got := breakdownLine(t, path, "Net worth", "Buy", "2027-01-15", "2027-12-31", "unexplained"); got != 0 {
		t.Errorf("unexplained remainder %v", got)
	}
}

func TestCLIProjectionAccountEdits(t *testing.T) {
	path := seededFile(t)
	houseScenario(t, path)

	dry := write(t, path, "", "projection", "add-account", "New job + house", "--name", "Garage", "--type", "property", "--tax", "taxable", "--from", "2028-01-01", "--dry-run")
	if !dry.DryRun || !dry.Changed {
		t.Fatalf("dry run: %+v", dry)
	}
	if n := len(projectionAccounts(t, path, "New job + house").Accounts); n != 2 {
		t.Fatalf("a dry run saved the account: %d accounts", n)
	}
	w := write(t, path, "", "projection", "add-account", "New job + house", "--name", "Garage", "--type", "property", "--tax", "taxable", "--from", "2028-01-01")
	if !w.Changed {
		t.Fatal("add-account changed nothing")
	}
	if got := projectionAccounts(t, path, "New job + house").Accounts; got[2].ID != "garage" {
		t.Errorf("generated id %q", got[2].ID)
	}

	// Edit by name; a dry run changes nothing.
	write(t, path, "", "projection", "edit-account", "New job + house", "New home", "--opening", "1", "--dry-run")
	write(t, path, "", "projection", "edit-account", "New job + house", "New home", "--opening", "500000", "--name", "Villa")
	if got := projectionAccounts(t, path, "New job + house").Accounts[0]; got.Opening != 500000 || got.Name != "Villa" {
		t.Errorf("after edit: %+v", got)
	}
	if v := lastValue(t, path, "house", "New job + house", "2027-06-01", "2027-06-01"); !closeTo(v, 500000) {
		t.Errorf("edited opening balance: %v", v)
	}

	refusedWrite(t, path, "", 1, "name_taken", "projection", "add-account", "New job + house", "--id", "home", "--name", "Dup", "--type", "property", "--tax", "taxable", "--from", "2028-01-01")
	refusedWrite(t, path, "", 1, "name_taken", "projection", "add-account", "New job + house", "--id", "house", "--name", "Dup", "--type", "property", "--tax", "taxable", "--from", "2028-01-01")
	// An id a projection building on this one has is taken too.
	write(t, path, "", "projection", "new", "Child", "--parent", "New job + house")
	write(t, path, "", "projection", "add-account", "Child", "--id", "shed", "--name", "Shed", "--type", "property", "--tax", "taxable", "--from", "2028-01-01")
	refusedWrite(t, path, "", 1, "name_taken", "projection", "add-account", "New job + house", "--id", "shed", "--name", "Shed", "--type", "property", "--tax", "taxable", "--from", "2028-01-01")
	refusedWrite(t, path, "", 1, "invalid", "projection", "add-account", "New job + house", "--name", " ", "--type", "property", "--tax", "taxable", "--from", "2028-01-01")
	refusedWrite(t, path, "", 1, "invalid", "projection", "edit-account", "New job + house", "garage", "--rate", "5")
	refusedWrite(t, path, "", 1, "not_found", "projection", "edit-account", "New job + house", "nothing", "--opening", "5")
	refusedWrite(t, path, "", 2, "usage", "projection", "add-account", "New job + house", "--name", "No date", "--type", "property", "--tax", "taxable")
	refusedWrite(t, path, "", 2, "usage", "projection", "edit-account", "New job + house", "garage")

	// Reparenting that would give two accounts one id is refused.
	write(t, path, "", "projection", "new", "Other", "--parent", "Base")
	write(t, path, "", "projection", "add-account", "Other", "--id", "shed", "--name", "Other shed", "--type", "property", "--tax", "taxable", "--from", "2028-01-01")
	refusedWrite(t, path, "", 1, "name_taken", "projection", "set-parent", "Other", "Child")
	refusedWrite(t, path, "", 1, "name_taken", "projection", "set-parent", "Child", "Other")

	// An account in use cannot be removed, by the projection or one building on it.
	refusedWrite(t, path, "", 1, "in_use", "projection", "remove-account", "New job + house", "loan")
	write(t, path, "", "mod", "add", "Child", "--target", "account:garage", "--property", "growth", "--value", "0.01")
	refusedWrite(t, path, "", 1, "in_use", "projection", "remove-account", "New job + house", "garage")
	write(t, path, "", "mod", "remove", "Child", "0")
	dry = write(t, path, "", "projection", "remove-account", "New job + house", "garage", "--dry-run")
	if !dry.DryRun || len(projectionAccounts(t, path, "New job + house").Accounts) != 3 {
		t.Fatal("a dry run removed the account")
	}
	write(t, path, "", "projection", "remove-account", "New job + house", "garage")
	if n := len(projectionAccounts(t, path, "New job + house").Accounts); n != 2 {
		t.Errorf("%d accounts after the remove", n)
	}
	// The mortgage payment is index 1 of the projection's modifications.
	write(t, path, "", "mod", "remove", "New job + house", "1")
	write(t, path, "", "projection", "remove-account", "New job + house", "loan")
	if v := lastValue(t, path, "Net worth", "New job + house", "2027-06-01", "2027-06-01"); v == 0 {
		t.Error("net worth is empty")
	}
}

func TestCLIProjectionAccountHelp(t *testing.T) {
	var help struct {
		Commands []struct {
			Name  string
			Write bool
			Flags []struct{ Name string }
		}
	}
	cliJSON(t, &help, "help", "--json")
	found := map[string]bool{}
	for _, c := range help.Commands {
		if c.Name != "projection add-account" && c.Name != "projection edit-account" && c.Name != "projection remove-account" {
			continue
		}
		found[c.Name] = true
		dry := false
		for _, f := range c.Flags {
			dry = dry || f.Name == "dry-run"
		}
		if !c.Write || !dry {
			t.Errorf("%s: write %v, dry-run flag %v", c.Name, c.Write, dry)
		}
	}
	if len(found) != 3 {
		t.Errorf("help lists %v", found)
	}
}
