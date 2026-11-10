// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

type wsegment struct {
	From  *string
	End   *string
	Value float64
}

type winterval struct {
	From  *string
	To    *string
	Value float64
}

type wasset struct {
	ID      string
	Name    string
	Members []struct {
		Seq     int
		Member  string
		Account *string
		Asset   *string
		Rule    *struct{ Types, Tax []string }
	}
	Invalid []struct {
		Seq        int
		Duplicates []string
	}
	Error   *string
	Default []winterval
	Params  map[string][]wsegment
}

type waccount struct {
	ID, Name, Type, Tax, Owner string
	StatedRate                 float64 `json:"stated_rate"`
	Default                    []winterval
	Closed                     *string
}

type wentry struct {
	ID      *int64
	Kind    string
	Date    string
	Amount  float64
	TxKind  *string `json:"tx_kind"`
	Counter *string
	Memo    *string
}

type wdata struct {
	DryRun  bool `json:"dry_run"`
	Changed bool
	Removed string
	Voided  bool
	Asset   *wasset
	Account *waccount
	Entry   *wentry
	Item    *struct {
		ID, Name, Kind string
		Amount         float64
		Growth         float64
		Start, End     *string
		Account        *string
		Recurrence     struct {
			Kind string
			Day  *int
			Day2 *int
		}
	}
}

func decodeData(t *testing.T, out []byte, args []string) wdata {
	t.Helper()
	var w wdata
	if err := json.Unmarshal(out, &w); err != nil {
		t.Fatalf("finplan %v: %v\n%s", args, err, out)
	}
	return w
}

// data runs a write command first as a dry run, which must leave the database as it was, then for real, and checks that the two print the same. It returns the real output.
func data(t *testing.T, path, stdin string, args ...string) wdata {
	t.Helper()
	before := dbSnapshot(t, path)
	run := func(extra ...string) wdata {
		full := append([]string{"--db", path}, args...)
		code, out := wcli(t, stdin, append(full, extra...)...)
		if code != 0 {
			t.Fatalf("finplan %v exited %d: %s", full, code, out)
		}
		return decodeData(t, out, full)
	}
	dry := run("--dry-run")
	if !dry.DryRun {
		t.Errorf("finplan %v --dry-run: dry_run is false", args)
	}
	if !reflect.DeepEqual(before, dbSnapshot(t, path)) {
		t.Fatalf("finplan %v --dry-run changed the database", args)
	}
	real := run()
	if real.DryRun {
		t.Errorf("finplan %v: dry_run is true", args)
	}
	dry.DryRun = false
	if dry.Entry != nil && real.Entry != nil {
		if dry.Entry.ID != nil && args[0] == "entry" && (args[1] == "add-snapshot" || args[1] == "add-transaction") {
			t.Errorf("finplan %v --dry-run: an unsaved entry has the id %d", args, *dry.Entry.ID)
		}
		if real.Entry.ID == nil {
			t.Errorf("finplan %v: the entry has no id", args)
		}
		realID := real.Entry.ID
		dry.Entry.ID, real.Entry.ID = nil, nil
		defer func() { real.Entry.ID = realID }()
	}
	if !reflect.DeepEqual(dry, real) {
		t.Errorf("finplan %v: the dry run printed %+v, the real run %+v", args, dry, real)
	}
	return real
}

func listAssets(t *testing.T, path string) map[string]wasset {
	t.Helper()
	var list []wasset
	cliJSON(t, &list, "--db", path, "asset", "list")
	out := map[string]wasset{}
	for _, a := range list {
		out[a.ID] = a
	}
	return out
}

func memberSeq(t *testing.T, a wasset, member string) int {
	t.Helper()
	for _, m := range a.Members {
		if m.Member == member {
			return m.Seq
		}
	}
	t.Fatalf("asset %s has no member %s: %+v", a.ID, member, a.Members)
	return 0
}

func TestCLIAssetCommands(t *testing.T) {
	path := seededFile(t)

	w := data(t, path, "", "asset", "new", "Spending money")
	if !w.Changed || w.Asset.ID != "spending-money" || w.Asset.Name != "Spending money" || len(w.Asset.Members) != 0 || w.Asset.Params == nil || w.Asset.Default == nil {
		t.Errorf("new: %+v", w.Asset)
	}
	if listAssets(t, path)["spending-money"].Name != "Spending money" {
		t.Error("asset list does not show the new asset")
	}
	if w := data(t, path, "", "asset", "new", "Other", "--id", "my-other"); w.Asset.ID != "my-other" {
		t.Errorf("new with an id: %+v", w.Asset)
	}

	w = data(t, path, "", "asset", "rename", "Spending money", "Pocket money")
	if w.Asset.ID != "spending-money" || w.Asset.Name != "Pocket money" {
		t.Errorf("rename: %+v", w.Asset)
	}
	if w := data(t, path, "", "asset", "rename", "spending-money", "Pocket money"); w.Changed {
		t.Error("renaming to the same name changed")
	}

	data(t, path, "", "asset", "add-member", "spending-money", "--account", "Checking")
	data(t, path, "", "asset", "add-member", "spending-money", "--asset", "all-cash")
	w = data(t, path, "", "asset", "add-member", "spending-money", "--types", "savings,IRA", "--tax", "taxable")
	if len(w.Asset.Members) != 3 || w.Asset.Members[2].Rule == nil || !slices.Equal(w.Asset.Members[2].Rule.Types, []string{"savings", "IRA"}) || !slices.Equal(w.Asset.Members[2].Rule.Tax, []string{"taxable"}) {
		t.Errorf("add-member rule: %+v", w.Asset.Members)
	}
	w = data(t, path, "", "asset", "add-member", "spending-money", "--rule")
	if len(w.Asset.Members) != 4 || w.Asset.Members[3].Rule == nil || len(w.Asset.Members[3].Rule.Types) != 0 {
		t.Errorf("add-member empty rule: %+v", w.Asset.Members)
	}
	a := listAssets(t, path)["spending-money"]
	if a.Members[0].Account == nil || *a.Members[0].Account != "checking" || a.Members[1].Asset == nil || *a.Members[1].Asset != "all-cash" {
		t.Errorf("members %+v", a.Members)
	}

	w = data(t, path, "", "asset", "remove-member", "spending-money", itoa(a.Members[3].Seq))
	if len(w.Asset.Members) != 3 {
		t.Errorf("remove-member: %+v", w.Asset.Members)
	}

	w = data(t, path, "", "asset", "add-default", "spending-money", "--value", "250", "--from", "2026-01", "--to", "2026-06")
	d := w.Asset.Default
	if len(d) != 1 || d[0].Value != 250 || *d[0].From != "2026-01-01" || *d[0].To != "2026-06-30" {
		t.Errorf("add-default: %+v", d)
	}
	w = data(t, path, "", "asset", "add-default", "spending-money", "--value", "-5")
	if d := w.Asset.Default; len(d) != 2 || d[1].From != nil || d[1].To != nil || d[1].Value != -5 {
		t.Errorf("add-default open-ended: %+v", d)
	}
	w = data(t, path, "", "asset", "remove-default", "spending-money", "0")
	if len(w.Asset.Default) != 1 || w.Asset.Default[0].Value != -5 {
		t.Errorf("remove-default: %+v", w.Asset.Default)
	}

	// An account's default asset takes the account's interval, and asset list shows it.
	w = data(t, path, "", "asset", "add-default", "TSP", "--value", "100", "--to", "2024-12")
	if w.Asset.ID != "acct:tsp" || len(w.Asset.Default) != 1 || w.Asset.Default[0].Value != 100 {
		t.Errorf("default of an account: %+v", w.Asset)
	}
	var accts []waccount
	cliJSON(t, &accts, "--db", path, "account", "list")
	if i := slices.IndexFunc(accts, func(a waccount) bool { return a.ID == "tsp" }); i < 0 || len(accts[i].Default) != 1 {
		t.Errorf("account list: %+v", accts)
	}

	w = data(t, path, "", "asset", "set-param", "retirement", "--value", "0.045", "--from", "2027-01")
	if g := w.Asset.Params["growth"]; len(g) != 1 || g[0].Value != 0.045 || *g[0].From != "2027-01-01" {
		t.Errorf("set-param: %+v", w.Asset.Params)
	}
	w = data(t, path, `[{"from":"2026-01-01","end":"2026-12-31","value":0.01},{"from":"2027-01-01","value":0.07}]`, "asset", "set-param", "retirement")
	if g := w.Asset.Params["growth"]; len(g) != 2 || *g[0].From != "2026-01-01" || g[0].Value != 0.01 || *g[1].From != "2027-01-01" || g[1].Value != 0.07 {
		t.Errorf("set-param JSON: %+v", w.Asset.Params)
	}
	w = data(t, path, "", "asset", "set-param", "retirement", "--clear")
	if len(w.Asset.Params) != 0 {
		t.Errorf("set-param --clear: %+v", w.Asset.Params)
	}
	if w := data(t, path, "", "asset", "set-param", "retirement", "--clear"); w.Changed {
		t.Error("clearing nothing changed")
	}

	w = data(t, path, "", "asset", "delete", "spending-money")
	if w.Removed != "spending-money" || w.Asset == nil || len(w.Asset.Members) != 3 {
		t.Errorf("delete: %+v", w)
	}
	if _, ok := listAssets(t, path)["spending-money"]; ok {
		t.Error("the deleted asset is still listed")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestCLIAssetRefusals(t *testing.T) {
	path := seededFile(t)
	refusedWrite(t, path, "", 1, "in_use", "asset", "delete", "all-cash")
	refusedWrite(t, path, "", 1, "invalid", "asset", "delete", "acct:checking")
	refusedWrite(t, path, "", 1, "invalid", "asset", "rename", "acct:checking", "x")
	refusedWrite(t, path, "", 1, "not_found", "asset", "delete", "no-such-asset")
	refusedWrite(t, path, "", 1, "not_found", "asset", "rename", "no-such-asset", "x")
	refusedWrite(t, path, "", 1, "name_taken", "asset", "new", "X", "--id", "net-worth")
	refusedWrite(t, path, "", 1, "invalid", "asset", "new", "X", "--id", "acct:x")
	refusedWrite(t, path, "", 1, "invalid", "asset", "new", " ")
	refusedWrite(t, path, "", 1, "cycle", "asset", "add-member", "all-cash", "--asset", "liquid")
	refusedWrite(t, path, "", 1, "cycle", "asset", "add-member", "liquid", "--asset", "liquid")
	refusedWrite(t, path, "", 1, "not_found", "asset", "add-member", "liquid", "--account", "no-such-account")
	refusedWrite(t, path, "", 1, "not_found", "asset", "add-member", "liquid", "--asset", "no-such-asset")
	refusedWrite(t, path, "", 1, "invalid", "asset", "add-member", "liquid", "--types", "piggy bank")
	refusedWrite(t, path, "", 1, "invalid", "asset", "add-member", "acct:checking", "--account", "savings")
	refusedWrite(t, path, "", 2, "usage", "asset", "add-member", "liquid")
	refusedWrite(t, path, "", 2, "usage", "asset", "add-member", "liquid", "--account", "savings", "--rule")
	refusedWrite(t, path, "", 2, "usage", "asset", "add-member", "liquid", "--asset", "checking")
	refusedWrite(t, path, "", 1, "not_found", "asset", "remove-member", "liquid", "9999")
	refusedWrite(t, path, "", 1, "invalid", "asset", "remove-member", "acct:checking", "1")
	refusedWrite(t, path, "", 2, "usage", "asset", "remove-member", "liquid", "first")
	refusedWrite(t, path, "", 1, "invalid", "asset", "add-default", "net-worth", "--value", "1", "--from", "2026-02", "--to", "2026-01")
	refusedWrite(t, path, "", 1, "invalid", "asset", "remove-default", "net-worth", "0")
	refusedWrite(t, path, "", 2, "usage", "asset", "set-param", "retirement", "--clear", "--value", "1")
	refusedWrite(t, path, "", 2, "usage", "asset", "set-param", "retirement")
	refusedWrite(t, path, "", 2, "usage", "asset", "set-param", "retirement", "--param", "inflation", "--value", "1")
	refusedWrite(t, path, "", 1, "not_found", "asset", "set-param", "no-such-asset", "--value", "1")
	// A projection that names the asset keeps it.
	write(t, path, "", "mod", "add", "Base", "--target", "asset:liquid", "--property", "growth", "--value", "0.01")
	refusedWrite(t, path, "", 1, "in_use", "asset", "delete", "liquid")
}

func TestCLIAccountCommands(t *testing.T) {
	path := seededFile(t)
	w := data(t, path, "", "account", "new", "College fund", "--type", "529", "--tax", "529", "--owner", "Child 4", "--rate", "0.05")
	a := w.Account
	if !w.Changed || a.ID != "college-fund" || a.Name != "College fund" || a.Type != "529" || a.Tax != "529" || a.Owner != "Child 4" || a.StatedRate != 0.05 || a.Default == nil {
		t.Errorf("new: %+v", a)
	}
	if w.Asset == nil || w.Asset.ID != "acct:college-fund" || len(w.Asset.Members) != 1 {
		t.Errorf("new: default asset %+v", w.Asset)
	}
	var accts []waccount
	cliJSON(t, &accts, "--db", path, "account", "list")
	if i := slices.IndexFunc(accts, func(a waccount) bool { return a.ID == "college-fund" }); i < 0 || accts[i].StatedRate != 0.05 {
		t.Errorf("account list: %+v", accts)
	}
	var ents []wentry
	cliJSON(t, &ents, "--db", path, "account", "entries", "college-fund")
	if len(ents) != 0 {
		t.Errorf("a new account has entries: %+v", ents)
	}

	w = data(t, path, "", "account", "edit", "College fund", "--name", "Fund", "--type", "savings", "--tax", "taxable", "--owner", "", "--rate", "0.01")
	a = w.Account
	if a.ID != "college-fund" || a.Name != "Fund" || a.Type != "savings" || a.Tax != "taxable" || a.Owner != "" || a.StatedRate != 0.01 {
		t.Errorf("edit: %+v", a)
	}
	if got := listAssets(t, path)["acct:college-fund"].Name; got != "Fund" {
		t.Errorf("default asset name %q", got)
	}
	if w := data(t, path, "", "account", "edit", "college-fund", "--rate", "0.01"); w.Changed || w.Account.Name != "Fund" {
		t.Errorf("edit to the same: %+v", w)
	}
	if w := data(t, path, "", "account", "edit", "college-fund", "--rate", "-0.02"); w.Account.StatedRate != -0.02 {
		t.Errorf("negative rate: %+v", w.Account)
	}

	refusedWrite(t, path, "", 2, "usage", "account", "new", "X", "--tax", "taxable")
	refusedWrite(t, path, "", 2, "usage", "account", "new", "X", "--type", "piggy bank", "--tax", "taxable")
	refusedWrite(t, path, "", 1, "invalid", "account", "new", " ", "--type", "savings", "--tax", "taxable")
	refusedWrite(t, path, "", 1, "invalid", "account", "new", "X", "--type", "savings", "--tax", "taxable", "--rate", "3")
	refusedWrite(t, path, "", 1, "name_taken", "account", "new", "X", "--type", "savings", "--tax", "taxable", "--id", "checking")
	refusedWrite(t, path, "", 1, "name_taken", "account", "new", "X", "--type", "savings", "--tax", "taxable", "--id", "net-worth")
	refusedWrite(t, path, "", 2, "usage", "account", "edit", "checking")
	refusedWrite(t, path, "", 2, "usage", "account", "edit", "checking", "--type", "piggy bank")
	refusedWrite(t, path, "", 1, "invalid", "account", "edit", "checking", "--rate", "3")
	refusedWrite(t, path, "", 1, "invalid", "account", "edit", "checking", "--name", " ")
	refusedWrite(t, path, "", 1, "not_found", "account", "edit", "no-such-account", "--name", "x")
}

func entriesOf(t *testing.T, path, acct string) []wentry {
	t.Helper()
	var list []wentry
	cliJSON(t, &list, "--db", path, "account", "entries", acct)
	return list
}

func TestCLIEntryCommands(t *testing.T) {
	path := seededFile(t)
	n := len(entriesOf(t, path, "tsp"))

	w := data(t, path, "", "entry", "add-snapshot", "TSP", "--date", "2026-11-08", "--balance", "150000")
	e := w.Entry
	if !w.Changed || e.ID == nil || e.Kind != "snapshot" || e.Date != "2026-11-08" || e.Amount != 150000 || e.TxKind != nil || e.Counter != nil || e.Memo != nil {
		t.Errorf("add-snapshot: %+v", e)
	}
	list := entriesOf(t, path, "tsp")
	if len(list) != n+1 || list[len(list)-1].ID == nil || *list[len(list)-1].ID != *e.ID {
		t.Errorf("account entries: %d entries, last %+v", len(list), list[len(list)-1])
	}
	refusedWrite(t, path, "", 1, "invalid", "entry", "add-snapshot", "tsp", "--date", "2026-11-08", "--balance", "1")
	refusedWrite(t, path, "", 1, "not_found", "entry", "add-snapshot", "no-such-account", "--date", "2026-11-08", "--balance", "1")
	refusedWrite(t, path, "", 2, "usage", "entry", "add-snapshot", "tsp", "--balance", "1")
	refusedWrite(t, path, "", 2, "usage", "entry", "add-snapshot", "tsp", "--date", "2026-13-01", "--balance", "1")

	w = data(t, path, "", "entry", "add-transaction", "checking", "--date", "2026-11-03", "--amount", "-42.5", "--kind", "expense", "--memo", "Invented")
	tx := w.Entry
	if tx.ID == nil || tx.Kind != "transaction" || tx.Amount != -42.5 || *tx.TxKind != "expense" || *tx.Memo != "Invented" || tx.Counter != nil {
		t.Errorf("add-transaction: %+v", tx)
	}
	w = data(t, path, "", "entry", "add-transaction", "checking", "--date", "2026-11-03", "--amount", "-10", "--kind", "transfer", "--counter", "Savings")
	if *w.Entry.Counter != "savings" || *w.Entry.TxKind != "transfer" {
		t.Errorf("transfer: %+v", w.Entry)
	}
	refusedWrite(t, path, "", 1, "invalid", "entry", "add-transaction", "checking", "--date", "2026-11-03", "--amount", "-10", "--kind", "transfer")
	refusedWrite(t, path, "", 1, "not_found", "entry", "add-transaction", "checking", "--date", "2026-11-03", "--amount", "-10", "--kind", "transfer", "--counter", "nope")
	refusedWrite(t, path, "", 1, "not_found", "entry", "add-transaction", "nope", "--date", "2026-11-03", "--amount", "-10", "--kind", "expense")
	refusedWrite(t, path, "", 2, "usage", "entry", "add-transaction", "checking", "--date", "2026-11-03", "--amount", "-10", "--kind", "gift")
	refusedWrite(t, path, "", 2, "usage", "entry", "add-transaction", "checking", "--date", "2026-11-03", "--kind", "expense")

	id := itoa64(*tx.ID)
	w = data(t, path, "", "entry", "correct", id, "--amount", "-45", "--note", "misread the receipt")
	if w.Entry.Amount != -45 || *w.Entry.Memo != "Invented" || *w.Entry.TxKind != "expense" || w.Entry.Date != "2026-11-03" {
		t.Errorf("correct: %+v", w.Entry)
	}
	w = data(t, path, "", "entry", "correct", id, "--memo", "", "--date", "2026-11-04")
	if w.Entry.Memo != nil || w.Entry.Date != "2026-11-04" || w.Entry.Amount != -45 {
		t.Errorf("correct memo and date: %+v", w.Entry)
	}
	if w := data(t, path, "", "entry", "correct", id, "--amount", "-45"); w.Changed || w.Entry.Amount != -45 {
		t.Errorf("correct to the same values: %+v", w)
	}
	var hist struct {
		Revisions []struct {
			Amount float64
			Note   *string
			Void   bool
		}
	}
	cliJSON(t, &hist, "--db", path, "entry", "history", id)
	if len(hist.Revisions) != 3 || hist.Revisions[0].Amount != -42.5 || hist.Revisions[1].Note == nil || *hist.Revisions[1].Note != "misread the receipt" {
		t.Errorf("history: %+v", hist.Revisions)
	}

	// A snapshot is a date and a balance only, and not on a date that has one.
	sid := itoa64(*e.ID)
	refusedWrite(t, path, "", 1, "invalid", "entry", "correct", sid, "--memo", "x")
	refusedWrite(t, path, "", 1, "invalid", "entry", "correct", sid, "--kind", "income")
	refusedWrite(t, path, "", 1, "invalid", "entry", "correct", sid, "--date", "2026-06-30")
	refusedWrite(t, path, "", 1, "invalid", "entry", "correct", id, "--kind", "transfer")
	refusedWrite(t, path, "", 1, "not_found", "entry", "correct", "99999", "--amount", "1")
	refusedWrite(t, path, "", 1, "not_found", "entry", "void", "99999")
	refusedWrite(t, path, "", 2, "usage", "entry", "correct", id)
	refusedWrite(t, path, "", 2, "usage", "entry", "correct", "first", "--amount", "1")
	w = data(t, path, "", "entry", "correct", sid, "--amount", "151000")
	if w.Entry.Amount != 151000 || w.Entry.Date != "2026-11-08" {
		t.Errorf("snapshot correct: %+v", w.Entry)
	}

	before := len(entriesOf(t, path, "checking"))
	w = data(t, path, "", "entry", "void", id, "--note", "entered twice")
	if !w.Voided || w.Entry.Amount != -45 {
		t.Errorf("void: %+v", w)
	}
	if got := len(entriesOf(t, path, "checking")); got != before-1 {
		t.Errorf("entries after the void: %d, was %d", got, before)
	}
	if w := data(t, path, "", "entry", "void", id); w.Changed {
		t.Error("voiding twice changed")
	}
	cliJSON(t, &hist, "--db", path, "entry", "history", id)
	if n := len(hist.Revisions); n != 4 || !hist.Revisions[n-1].Void || *hist.Revisions[n-1].Note != "entered twice" {
		t.Errorf("history after the void: %+v", hist.Revisions)
	}
	data(t, path, "", "entry", "correct", id, "--amount", "-46")
	if got := len(entriesOf(t, path, "checking")); got != before {
		t.Errorf("entries after the restore: %d, want %d", got, before)
	}
}

func itoa64(n int64) string { return itoa(int(n)) }

func TestCLIBudgetCommands(t *testing.T) {
	path := seededFile(t)
	w := data(t, path, "", "budget", "add", "--kind", "income", "--name", "Side gig", "--amount", "800", "--every", "monthly", "--on", "2027-01-15", "--account", "checking")
	it := w.Item
	if !w.Changed || it.ID != "side-gig" || it.Amount != 800 || it.Recurrence.Kind != "monthly" || *it.Recurrence.Day != 15 || *it.Start != "2027-01-15" || *it.Account != "checking" {
		t.Errorf("add: %+v", it)
	}
	var list []struct{ ID string }
	cliJSON(t, &list, "--db", path, "budget", "list")
	if len(list) == 0 || list[len(list)-1].ID != "side-gig" {
		t.Errorf("budget list: %v", list)
	}
	w = data(t, path, `{"name":"Gift","kind":"income","amount":500,"account":"checking","recurrence":{"kind":"one-time","anchor":"2027-03-01"}}`, "budget", "add")
	if w.Item.ID != "gift" || w.Item.Recurrence.Kind != "one-time" {
		t.Errorf("add JSON: %+v", w.Item)
	}

	w = data(t, path, "", "budget", "edit", "Side gig", "--amount", "900", "--day", "20")
	if w.Item.Amount != 900 || *w.Item.Recurrence.Day != 20 || w.Item.Name != "Side gig" {
		t.Errorf("edit: %+v", w.Item)
	}
	if w := data(t, path, "", "budget", "edit", "side-gig", "--amount", "900"); w.Changed {
		t.Errorf("edit to the same: %+v", w)
	}
	// A semimonthly item can be edited, and keeps its days.
	w = data(t, path, "", "budget", "edit", "salary-a", "--amount", "5200")
	if w.Item.Amount != 5200 || w.Item.Recurrence.Kind != "semimonthly" || *w.Item.Recurrence.Day != 15 || *w.Item.Recurrence.Day2 != 31 {
		t.Errorf("edit semimonthly: %+v", w.Item)
	}
	w = data(t, path, "", "budget", "end", "side-gig", "2027-06-30")
	if w.Item.End == nil || *w.Item.End != "2027-06-30" {
		t.Errorf("end: %+v", w.Item)
	}
	if w := data(t, path, "", "budget", "end", "Side gig", "2027-06-30"); w.Changed {
		t.Error("ending on the same date changed")
	}
	w = data(t, path, "", "budget", "edit", "side-gig", "--end", "none")
	if w.Item.End != nil {
		t.Errorf("edit --end none: %+v", w.Item)
	}

	refusedWrite(t, path, "", 1, "not_found", "budget", "edit", "no-such-item", "--amount", "1")
	refusedWrite(t, path, "", 1, "not_found", "budget", "end", "no-such-item", "2027-01-01")
	refusedWrite(t, path, "", 1, "not_found", "budget", "end", "early-401k-withdrawal", "2027-01-01")
	refusedWrite(t, path, "", 1, "invalid", "budget", "end", "gift", "2027-02-28")
	refusedWrite(t, path, "", 1, "invalid", "budget", "end", "side-gig", "2026-12-31")
	refusedWrite(t, path, "", 2, "usage", "budget", "end", "side-gig", "2027-02-30")
	refusedWrite(t, path, "", 1, "name_taken", "budget", "add", "--id", "salary-a", "--kind", "income", "--name", "X", "--amount", "1", "--every", "monthly", "--on", "2027-01-15", "--account", "checking")
	refusedWrite(t, path, "", 1, "name_taken", "budget", "add", "--id", "early-401k-withdrawal", "--kind", "income", "--name", "X", "--amount", "1", "--every", "monthly", "--on", "2027-01-15", "--account", "checking")
	refusedWrite(t, path, "", 1, "unknown_target", "budget", "add", "--kind", "income", "--name", "X", "--amount", "1", "--every", "monthly", "--on", "2027-01-15", "--account", "nope")
	refusedWrite(t, path, "", 1, "invalid", "budget", "add", "--kind", "income", "--name", "X", "--amount", "0", "--every", "monthly", "--on", "2027-01-15", "--account", "checking")
	refusedWrite(t, path, `{"name":"Pay","kind":"income","amount":5,"account":"checking","recurrence":{"kind":"semimonthly","day":1,"day2":15}}`, 1, "invalid", "budget", "add")
	refusedWrite(t, path, "", 2, "usage", "budget", "add", "--name", "X")
}

func TestCLIManualBalanceScenario(t *testing.T) {
	path := seededFile(t)
	const date = "2026-11-08"
	var before balanceOut
	cliJSON(t, &before, "--db", path, "balance", "tsp", "--from", date, "--to", date)
	if before.Samples[0].Status == "actual" {
		t.Fatalf("the TSP has actual data on %s: %+v", date, before.Samples)
	}
	write(t, path, "", "entry", "add-snapshot", "tsp", "--date", date, "--balance", "151234.5")
	var after balanceOut
	cliJSON(t, &after, "--db", path, "balance", "tsp", "--from", date, "--to", date)
	if s := after.Samples[0]; s.Value != 151234.5 || s.Status != "actual" {
		t.Errorf("balance after the snapshot: %+v", s)
	}
}

func TestCLIDuplicateTransactionScenario(t *testing.T) {
	path := seededFile(t)
	const date = "2026-11-03"
	base := lastValue(t, path, "checking", "", date, date)
	add := func() int64 {
		w := data(t, path, "", "entry", "add-transaction", "checking", "--date", date, "--amount", "-42", "--kind", "expense", "--memo", "Invented")
		return *w.Entry.ID
	}
	first, second := add(), add()
	if first == second {
		t.Fatal("two additions got one id")
	}
	if got := lastValue(t, path, "checking", "", date, date); got != base-84 {
		t.Errorf("balance with both: %v, want %v", got, base-84)
	}
	data(t, path, "", "entry", "void", itoa64(second), "--note", "duplicate")
	if got := lastValue(t, path, "checking", "", date, date); got != base-42 {
		t.Errorf("balance after the void: %v, want %v", got, base-42)
	}
	var hist struct {
		Revisions []struct {
			Void bool
			Note *string
		}
	}
	cliJSON(t, &hist, "--db", path, "entry", "history", itoa64(second))
	if n := len(hist.Revisions); n != 2 || !hist.Revisions[1].Void || *hist.Revisions[1].Note != "duplicate" {
		t.Errorf("history: %+v", hist.Revisions)
	}
	ids := map[int64]bool{}
	for _, e := range entriesOf(t, path, "checking") {
		ids[*e.ID] = true
	}
	if !ids[first] || ids[second] {
		t.Errorf("account entries: first listed %v, voided one listed %v", ids[first], ids[second])
	}
}

func TestCLIOverlappingAggregateScenario(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "asset", "new", "Everyday money")
	write(t, path, "", "asset", "add-member", "everyday-money", "--asset", "all-cash")
	write(t, path, "", "asset", "add-member", "everyday-money", "--account", "savings")
	a := listAssets(t, path)["everyday-money"]
	if len(a.Members) != 2 || len(a.Invalid) != 1 || a.Invalid[0].Seq != memberSeq(t, a, `account "savings"`) || !slices.Equal(a.Invalid[0].Duplicates, []string{"savings"}) {
		t.Errorf("asset list: %+v", a)
	}
	if a.Error != nil {
		t.Errorf("error %v", *a.Error)
	}
	// Removing the earlier member makes the later one valid.
	write(t, path, "", "asset", "remove-member", "everyday-money", itoa(memberSeq(t, a, `asset "all-cash"`)))
	a = listAssets(t, path)["everyday-money"]
	if len(a.Members) != 1 || len(a.Invalid) != 0 {
		t.Errorf("after the removal: %+v", a)
	}
}

func TestCLIBaseBudgetItemScenario(t *testing.T) {
	path := seededFile(t)
	base := lastValue(t, path, "checking", "", "2027-01", "2027-12")
	write(t, path, "", "budget", "add", "--kind", "income", "--name", "Consulting", "--amount", "3000", "--every", "monthly", "--on", "2027-01-15", "--account", "checking")
	with := lastValue(t, path, "checking", "", "2027-01", "2027-12")
	if with < base+20000 {
		t.Errorf("checking at the end of 2027: %v with the item, %v without", with, base)
	}
	write(t, path, "", "budget", "end", "consulting", "2027-03-31")
	ended := lastValue(t, path, "checking", "", "2027-01", "2027-12")
	if ended >= with {
		t.Errorf("checking at the end of 2027: %v with the item ended, %v running", ended, with)
	}
}
