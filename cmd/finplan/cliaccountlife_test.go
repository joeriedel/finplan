// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"strings"
	"testing"
)

type laccount struct {
	waccount
	Balance  float64
	LastData *string `json:"last_data"`
	Stale    bool
}

func listLife(t *testing.T, path string) map[string]laccount {
	t.Helper()
	var list []laccount
	cliJSON(t, &list, "--db", path, "account", "list")
	out := map[string]laccount{}
	for _, a := range list {
		out[a.ID] = a
	}
	return out
}

// cliBalanceOn is the value of the account on one date, from the balance command.
func cliBalanceOn(t *testing.T, path, account, date string) float64 {
	t.Helper()
	var b balanceOut
	cliJSON(t, &b, "--db", path, "balance", account, "--from", date, "--to", date)
	if len(b.Samples) != 1 {
		t.Fatalf("balance of %s on %s: %+v", account, date, b)
	}
	return b.Samples[0].Value
}

func TestCLIAccountListShowsBalanceLastDataAndStale(t *testing.T) {
	path := seededFile(t)
	list := listLife(t, path)
	// The TSP's records stop on 2026-06-30, 131 days before the record's today (2026-11-08); checking is current.
	tsp, chk := list["tsp"], list["checking"]
	if !tsp.Stale || tsp.LastData == nil || *tsp.LastData > "2026-06-30" {
		t.Errorf("tsp: %+v", tsp)
	}
	if chk.Stale || chk.LastData == nil || chk.Closed != nil {
		t.Errorf("checking: %+v", chk)
	}
	if want := cliBalanceOn(t, path, "checking", "2026-11-08"); chk.Balance != want || want == 0 {
		t.Errorf("checking balance %v, want %v", chk.Balance, want)
	}
	// An account with no snapshot has no last data and is not stale.
	data(t, path, "", "account", "new", "Empty", "--type", "savings", "--tax", "taxable")
	if e := listLife(t, path)["empty"]; e.LastData != nil || e.Stale || e.Balance != 0 {
		t.Errorf("empty: %+v", e)
	}
}

func TestCLIAccountLifeCycle(t *testing.T) {
	path := seededFile(t)

	// Add a manual TSP account and its first balance; it has a balance from then on.
	w := data(t, path, "", "account", "new", "TSP Two", "--type", "TSP", "--tax", "tax-deferred", "--rate", "0.05")
	if w.Account.ID != "tsp-two" || w.Account.Type != "TSP" {
		t.Fatalf("new: %+v", w.Account)
	}
	data(t, path, "", "entry", "add-snapshot", "tsp-two", "--date", "2026-10-31", "--balance", "25000")
	if got := cliBalanceOn(t, path, "tsp-two", "2026-10-31"); got != 25000 {
		t.Errorf("first snapshot reads %v", got)
	}
	if a := listLife(t, path)["tsp-two"]; a.Balance < 25000 || a.Stale || a.LastData == nil || *a.LastData != "2026-10-31" {
		t.Errorf("listed: %+v", a)
	}

	// Edit its attributes and default balance intervals.
	w = data(t, path, "", "account", "edit", "tsp-two", "--name", "TSP B", "--type", "IRA", "--tax", "tax-free", "--rate", "0.03")
	if a := w.Account; a.Name != "TSP B" || a.Type != "IRA" || a.Tax != "tax-free" || a.StatedRate != 0.03 {
		t.Errorf("edit: %+v", a)
	}
	data(t, path, "", "asset", "add-default", "tsp-two", "--value", "100", "--to", "2024-12")
	if a := listLife(t, path)["tsp-two"]; len(a.Default) != 1 || a.Default[0].Value != 100 {
		t.Errorf("default intervals: %+v", a.Default)
	}

	// An account with record entries is not deleted, one without is.
	refusedWrite(t, path, "", 1, "in_use", "account", "delete", "tsp-two")
	data(t, path, "", "account", "new", "Scratch", "--type", "savings", "--tax", "taxable")
	if w := data(t, path, "", "account", "delete", "scratch"); w.Removed != "scratch" || w.Account == nil {
		t.Errorf("delete: %+v", w)
	}
	if _, ok := listLife(t, path)["scratch"]; ok {
		t.Error("the deleted account is still listed")
	}
	refusedWrite(t, path, "", 1, "not_found", "account", "delete", "scratch")

	// Closing keeps the history and reads 0 after the close date.
	openTSP, openNet := cliBalanceOn(t, path, "tsp-two", "2026-11-09"), cliBalanceOn(t, path, "net-worth", "2026-11-09")
	refusedWrite(t, path, "", 1, "invalid", "account", "close", "tsp-two", "--date", "2026-10-01")
	w = data(t, path, "", "account", "close", "tsp-two", "--date", "2026-11-07")
	if w.Account.Closed == nil || *w.Account.Closed != "2026-11-07" {
		t.Errorf("close: %+v", w.Account)
	}
	refusedWrite(t, path, "", 1, "invalid", "account", "close", "tsp-two", "--date", "2026-11-09")
	if got := cliBalanceOn(t, path, "tsp-two", "2026-11-07"); got < 25000 {
		t.Errorf("on the close date: %v", got)
	}
	if got := cliBalanceOn(t, path, "tsp-two", "2026-11-09"); got != 0 {
		t.Errorf("the day after the close date: %v", got)
	}
	// An aggregate that includes it drops its balance too.
	if got, want := cliBalanceOn(t, path, "net-worth", "2026-11-09"), openNet-openTSP; math.Abs(got-want) > 1e-6 {
		t.Errorf("net worth after the close date %v, want %v without the closed account", got, want)
	}
	var ents []wentry
	cliJSON(t, &ents, "--db", path, "account", "entries", "tsp-two")
	if len(ents) != 1 {
		t.Errorf("the closed account lost its history: %+v", ents)
	}
	if a := listLife(t, path)["tsp-two"]; a.Closed == nil || a.Stale || a.Balance != 0 {
		t.Errorf("closed account listed as %+v", a)
	}
	refusedWrite(t, path, "", 1, "in_use", "account", "delete", "tsp-two")

	// An entry dated after the close date is refused.
	refusedWrite(t, path, "", 1, "invalid", "entry", "add-snapshot", "tsp-two", "--date", "2026-11-08", "--balance", "1")

	// Reopen it; reopening an open account changes nothing.
	if w := data(t, path, "", "account", "reopen", "tsp-two"); !w.Changed || w.Account.Closed != nil {
		t.Errorf("reopen: %+v", w)
	}
	if w := data(t, path, "", "account", "reopen", "tsp-two"); w.Changed {
		t.Errorf("second reopen: %+v", w)
	}
	if got := cliBalanceOn(t, path, "tsp-two", "2026-11-09"); got < 25000 {
		t.Errorf("after the reopen: %v", got)
	}
	refusedWrite(t, path, "", 1, "not_found", "account", "close", "nope")
}

func TestCLIClosingAnAccountStopsTheTransfersIntoIt(t *testing.T) {
	path := seededFile(t)
	const day = "2027-06-01"
	open := cliBalanceOn(t, path, "checking", day)
	data(t, path, "", "account", "close", "tsp", "--date", "2026-11-07")
	if closed := cliBalanceOn(t, path, "checking", day); closed <= open {
		t.Errorf("checking on %s: %v with the TSP closed, %v while open: the transfers to it should stop", day, closed, open)
	}
}

func TestCLIAccountDeleteRemovesWhatRefersToIt(t *testing.T) {
	path := seededFile(t)
	data(t, path, "", "account", "new", "Scratch", "--type", "savings", "--tax", "taxable")
	write(t, path, "", "mod", "add", "Base", "--target", "account:scratch", "--property", "growth", "--value", "0.01")
	affected := errorAffected(t, path, "confirm_required", "account", "delete", "scratch")
	if len(affected) != 1 || affected[0].Kind != "modification" || affected[0].In != "Base" {
		t.Fatalf("affected %+v", affected)
	}
	data(t, path, "", "account", "delete", "scratch", "--force")
	var p struct {
		Mods []any `json:"modifications"`
	}
	cliJSON(t, &p, "--db", path, "projection", "show", "Base")
	if len(p.Mods) != 0 {
		t.Errorf("the modification of the deleted account is left: %+v", p)
	}
}

func TestCLIAccountWithRecordsIsClosedNotDeleted(t *testing.T) {
	path := seededFile(t)
	for _, extra := range [][]string{{}, {"--force"}, {"--dry-run"}} {
		code, out := wcli(t, "", append([]string{"--db", path, "account", "delete", "checking"}, extra...)...)
		if code != 1 || !strings.Contains(string(out), `"in_use"`) || !strings.Contains(string(out), "close it instead") {
			t.Errorf("delete %v exited %d: %s", extra, code, out)
		}
	}
	if w := data(t, path, "", "account", "close", "checking", "--date", "2026-11-09"); w.Account.Closed == nil {
		t.Errorf("close: %+v", w.Account)
	}
}
