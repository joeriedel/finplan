// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

func rowOf(rows []listRow, id model.AccountID) listRow {
	for _, r := range rows {
		if r.Account.ID == id {
			return r
		}
	}
	return listRow{}
}

func TestListAccountsRows(t *testing.T) {
	a := demoApp(t)
	rows := listAccounts(a.rec)
	if len(rows) != len(a.rec.Accounts) {
		t.Fatalf("%d rows for %d accounts", len(rows), len(a.rec.Accounts))
	}
	// The TSP's data ends 131 days before today; checking's is current.
	tsp, chk := rowOf(rows, demodata.TSP), rowOf(rows, demodata.Checking)
	if !tsp.HasData || !tsp.Stale || tsp.LastData > model.D(2026, 6, 30) {
		t.Errorf("tsp row %+v", tsp)
	}
	if !chk.HasData || chk.Stale || chk.LastData > a.rec.Today {
		t.Errorf("checking row %+v", chk)
	}
	res, err := engine.Balances(a.rec, "", model.DefaultAssetID(demodata.Checking), []model.Date{a.rec.Today})
	if err != nil || chk.Balance != res.Samples[0].Amount || chk.Balance == 0 {
		t.Errorf("checking balance %v, want %v (%v)", chk.Balance, res.Samples[0].Amount, err)
	}
}

func TestStaleBoundaryMatchesTheGraph(t *testing.T) {
	a := demoApp(t)
	acct := a.rec.Account(demodata.Checking)
	sp, _ := acct.Span()
	last := sp.To
	for _, c := range []struct {
		daysAfter int
		stale     bool
	}{{45, false}, {46, true}} {
		rec := a.rec.Clone()
		rec.Today = last.Add(c.daysAfter)
		if got := rowOf(listAccounts(rec), demodata.Checking).Stale; got != c.stale {
			t.Errorf("%d days after the last data: stale %v", c.daysAfter, got)
		}
	}
}

func TestGroupAccounts(t *testing.T) {
	a := demoApp(t)
	groups := groupAccounts(listAccounts(a.rec))
	var titles []string
	for _, g := range groups {
		titles = append(titles, g.Title)
		for _, r := range g.Rows {
			if string(r.Account.Type) != g.Title {
				t.Errorf("%s in the group %s", r.Account.ID, g.Title)
			}
		}
	}
	if titles[0] != string(model.TypeChecking) || slices.Contains(titles, closedGroup) {
		t.Errorf("groups %v", titles)
	}
	n := 0
	for _, g := range groups {
		n += len(g.Rows)
	}
	if n != len(a.rec.Accounts) {
		t.Errorf("%d accounts grouped of %d", n, len(a.rec.Accounts))
	}

	closeAccount(t, a, demodata.TSP)
	groups = groupAccounts(listAccounts(a.rec))
	last := groups[len(groups)-1]
	if last.Title != closedGroup || len(last.Rows) != 1 || last.Rows[0].Account.ID != demodata.TSP {
		t.Fatalf("last group %+v", last)
	}
	if last.Rows[0].Stale {
		t.Error("a closed account is flagged stale")
	}
	for _, g := range groups[:len(groups)-1] {
		if g.Title == string(model.TypeTSP) {
			t.Error("the closed account is still in its type group")
		}
	}
}

func closeAccount(t *testing.T, a *app, id model.AccountID) {
	t.Helper()
	d, _ := closeDate(a.rec, "")
	if sp, ok := a.rec.Account(id).Span(); ok {
		d = max(d, sp.To)
	}
	if !a.edit("close account", func() (*ops.Result, error) { return ops.CloseAccount(a.st, a.rec, id, d, ops.Options{}) }) {
		t.Fatalf("not closed: %v", a.notices)
	}
}

func TestNewAccountFormProblems(t *testing.T) {
	a := demoApp(t)
	var f newAcctForm
	for _, tc := range []struct {
		pairs []string
		want  string
	}{
		{nil, "name"},
		{[]string{"name", "My TSP"}, "type"},
		{[]string{"type", "TSP"}, "tax"},
		{[]string{"tax", "tax-deferred", "rate", "lots"}, "not a number"},
		{[]string{"name", "all CASH"}, "all cash"},
		{[]string{"name", "My TSP"}, ""},
		{[]string{"rate", "5"}, ""},
		{[]string{"rate", "500"}, "stated rate"},
		{[]string{"rate", ""}, ""},
	} {
		for i := 0; i+1 < len(tc.pairs); i += 2 {
			f.set(tc.pairs[i], tc.pairs[i+1])
		}
		got := newAcctProblem(a.st, a.rec, f)
		if tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
			t.Errorf("after %v: %q, want %q", tc.pairs, got, tc.want)
		}
	}
}

func TestAddAccountThenFirstBalanceThenGraph(t *testing.T) {
	a := demoApp(t)
	f := newAcctForm{name: "TSP Two", typ: model.TypeTSP, tax: model.TaxDeferred, rate: "5"}
	acct, err := f.account()
	if err != nil || acct.StatedRate != 0.05 {
		t.Fatalf("form: %+v, %v", acct, err)
	}
	if !a.edit("add account", func() (*ops.Result, error) { return ops.NewAccount(a.st, a.rec, acct, ops.Options{}) }) {
		t.Fatalf("not added: %v", a.notices)
	}
	const id = model.AccountID("tsp-two")
	asset := model.DefaultAssetID(id)
	if !slices.ContainsFunc(lineCandidates(a.rec, "tsp two"), func(c lineCandidate) bool { return c.id == asset }) {
		t.Error("the new account is not offered to the Graph")
	}
	if p := snapshotProblem(a.st, a.rec, id, "2026-10-31", ""); !strings.Contains(p, "not a number") {
		t.Errorf("no balance: %q", p)
	}
	if p := snapshotProblem(a.st, a.rec, id, "soon", "25000"); !strings.Contains(p, "date") {
		t.Errorf("bad date: %q", p)
	}
	if p := snapshotProblem(a.st, a.rec, id, "2026-10-31", "25000"); p != "" {
		t.Fatalf("snapshot: %q", p)
	}
	if !a.addEntry("record balance", func() (*ops.Result, error) {
		return ops.AddSnapshot(a.st, a.rec, id, model.D(2026, 10, 31), 25000, ops.Options{})
	}) {
		t.Fatalf("not recorded: %v", a.notices)
	}
	if s, _ := a.history.PeekUndo(); s.Label != "add account" {
		t.Errorf("a recorded balance pushed an undo step: latest is %q", s.Label)
	}
	if p := snapshotProblem(a.st, a.rec, id, "2026-10-31", "1"); p == "" {
		t.Error("a second balance on the same day is allowed")
	}
	// The Graph tab sees the account's balance.
	if a.graph.rec != a.rec {
		t.Error("the Graph tab does not see the record")
	}
	res, err := engine.Balances(a.graph.rec, "", asset, []model.Date{model.D(2026, 10, 31)})
	if err != nil || res.Samples[0].Amount != 25000 || res.Samples[0].Status != engine.StatusActual {
		t.Errorf("graph balance %+v, %v", res, err)
	}
	if row := rowOf(listAccounts(a.rec), id); !row.HasData || row.Stale || row.Balance < 25000 {
		t.Errorf("row %+v", row)
	}
}

func TestAccountAttributeAndIntervalEditsAreUndoable(t *testing.T) {
	a := demoApp(t)
	orig := a.rec.Account(demodata.Checking)
	name, typ, tax, rate := orig.Name, orig.Type, orig.Tax, orig.StatedRate
	for _, c := range [][2]string{{"name", "Everyday"}, {"type", string(model.TypeSavings)}, {"tax", string(model.TaxFree)}, {"owner", "Me"}, {"rate", "2.5"}} {
		ch, err := accountChange(c[0], c[1])
		if err != nil {
			t.Fatal(err)
		}
		if !a.edit("change account", func() (*ops.Result, error) {
			return ops.EditAccount(a.st, a.rec, demodata.Checking, ch, ops.Options{})
		}) {
			t.Fatalf("%s refused: %v", c[0], a.notices)
		}
	}
	got := a.rec.Account(demodata.Checking)
	if got.Name != "Everyday" || got.Type != model.TypeSavings || got.Tax != model.TaxFree || got.Owner != "Me" || got.StatedRate != 0.025 {
		t.Fatalf("after the edits: %+v", got)
	}
	if _, err := accountChange("rate", "x"); err == nil {
		t.Error("a rate that is not a number was read")
	}

	iv, err := interval("", "2030-12-31", "5000")
	if err != nil || iv.From != model.MinDate || iv.To == nil {
		t.Fatalf("interval %+v, %v", iv, err)
	}
	if p := intervalProblem(a.st, a.rec, demodata.Checking, "2031-01-01", "2030-01-01", "1"); p == "" {
		t.Error("an interval that ends before it starts is allowed")
	}
	if !a.edit("add default balance", func() (*ops.Result, error) {
		return ops.AddDefault(a.st, a.rec, model.DefaultAssetID(demodata.Checking), iv, ops.Options{})
	}) {
		t.Fatal(a.notices)
	}
	if n := len(a.rec.Account(demodata.Checking).Default.Intervals); n != 1 {
		t.Fatalf("%d intervals", n)
	}
	if !a.edit("remove default balance", func() (*ops.Result, error) {
		return ops.RemoveDefault(a.st, a.rec, model.DefaultAssetID(demodata.Checking), 0, ops.Options{})
	}) {
		t.Fatal(a.notices)
	}

	// Seven edits back out, each saved.
	for range 7 {
		a.undo()
	}
	got = a.rec.Account(demodata.Checking)
	if got.Name != name || got.Type != typ || got.Tax != tax || got.StatedRate != rate || got.Owner != orig.Owner || len(got.Default.Intervals) != 0 {
		t.Errorf("after the undos: %+v", got)
	}
	if a.rec.Asset(model.DefaultAssetID(demodata.Checking)).Name != name {
		t.Error("the default asset keeps the new name")
	}
	saved, err := a.st.Load()
	if err != nil || saved.Account(demodata.Checking).Name != name {
		t.Errorf("the undo was not saved: %v", err)
	}
	if a.graph.rec != a.rec {
		t.Error("the Graph tab does not see the undo")
	}
	a.redo()
	if a.rec.Account(demodata.Checking).Name != "Everyday" {
		t.Error("redo")
	}
}

func TestCloseReopenDeleteAreUndoable(t *testing.T) {
	a := demoApp(t)
	closeAccount(t, a, demodata.TSP)
	if !a.rec.Account(demodata.TSP).IsClosed() {
		t.Fatal("not closed")
	}
	a.undo()
	if a.rec.Account(demodata.TSP).IsClosed() {
		t.Error("undo left the account closed")
	}
	a.redo()
	if !a.rec.Account(demodata.TSP).IsClosed() {
		t.Error("redo did not close the account")
	}
	if !a.edit("reopen account", func() (*ops.Result, error) { return ops.ReopenAccount(a.st, a.rec, demodata.TSP, ops.Options{}) }) {
		t.Fatal(a.notices)
	}
	a.undo()
	if !a.rec.Account(demodata.TSP).IsClosed() {
		t.Error("undoing the reopen did not close it")
	}

	// A new account is deleted and the delete undone.
	f := newAcctForm{name: "Scratch", typ: model.TypeSavings, tax: model.Taxable}
	acct, _ := f.account()
	a.edit("add account", func() (*ops.Result, error) { return ops.NewAccount(a.st, a.rec, acct, ops.Options{}) })
	if p := deleteProblem(a.st, a.rec, "scratch"); p != "" {
		t.Fatalf("delete problem %q", p)
	}
	if p := deleteProblem(a.st, a.rec, demodata.Checking); !strings.Contains(p, "record entries") {
		t.Errorf("delete problem of an account with entries: %q", p)
	}
	if !a.edit("delete account", func() (*ops.Result, error) { return ops.DeleteAccount(a.st, a.rec, "scratch", ops.Options{}) }) {
		t.Fatal(a.notices)
	}
	if a.rec.Account("scratch") != nil {
		t.Fatal("not deleted")
	}
	a.undo()
	if a.rec.Account("scratch") == nil || a.rec.Asset("acct:scratch") == nil {
		t.Error("undo did not bring the account back")
	}
	a.undo()
	if a.rec.Account("scratch") != nil {
		t.Error("undoing the add left the account")
	}
	if saved, err := a.st.Load(); err != nil || saved.Account("scratch") != nil {
		t.Errorf("saved: %v", err)
	}
}

func TestUndoOfNewAccountKeepsTheBalancesRecorded(t *testing.T) {
	a := demoApp(t)
	f := newAcctForm{name: "Scratch", typ: model.TypeSavings, tax: model.Taxable}
	acct, _ := f.account()
	a.edit("add account", func() (*ops.Result, error) { return ops.NewAccount(a.st, a.rec, acct, ops.Options{}) })
	a.addEntry("record balance", func() (*ops.Result, error) {
		return ops.AddSnapshot(a.st, a.rec, "scratch", model.D(2026, 10, 31), 5, ops.Options{})
	})
	a.undo()
	if a.rec.Account("scratch") == nil || len(a.rec.Account("scratch").Snapshots) != 1 || len(a.notices) == 0 {
		t.Errorf("undo removed an account with entries or said nothing: %v", a.notices)
	}
	if !a.history.CanUndo() {
		t.Error("the step was dropped")
	}
}

func TestClosedAccountsAreLeftOutOfPickers(t *testing.T) {
	a := demoApp(t)
	asset := model.DefaultAssetID(demodata.TSP)
	has := func() (line, obj, item bool) {
		line = slices.ContainsFunc(lineCandidates(effective(a.rec, demodata.ProjBase), ""), func(c lineCandidate) bool { return c.id == asset })
		obj = slices.ContainsFunc(objectsFor(a.rec, model.KindAccount, demodata.ProjBase, 0), func(c objectChoice) bool { return c.id == string(demodata.TSP) })
		ids, _ := itemAccountChoices(effective(a.rec, demodata.ProjBase))
		return line, obj, slices.Contains(ids, string(demodata.TSP))
	}
	if l, o, i := has(); !l || !o || !i {
		t.Fatalf("an open account is missing from a picker: line %v object %v item %v", l, o, i)
	}
	// A line and a modification that already use the account.
	a.graph.lines.Add(asset, false, "", graph.RGB(1))
	lines := len(a.graph.lines.All())
	m := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.TSP)}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	if !a.edit("add modification", func() (*ops.Result, error) { return ops.AddMod(a.st, a.rec, demodata.ProjBase, m, ops.Options{}) }) {
		t.Fatal(a.notices)
	}
	closeAccount(t, a, demodata.TSP)
	if l, o, i := has(); l || o || i {
		t.Errorf("a closed account is offered: line %v object %v item %v", l, o, i)
	}
	if len(a.graph.lines.All()) != lines {
		t.Error("closing removed a Graph line")
	}
	rec := effective(a.rec, demodata.ProjBase)
	ids, names := itemAccountChoices(rec)
	if got, want := itemAccountPreview(rec, ids, names, demodata.TSP), a.rec.Account(demodata.TSP).Name; got != want {
		t.Errorf("the picker shows %q for the closed account, want %q", got, want)
	}
	if n := len(a.rec.Projection(demodata.ProjBase).Mods); n == 0 {
		t.Error("closing dropped the modification")
	}
	if _, err := engine.Balances(a.rec, demodata.ProjBase, asset, []model.Date{model.D(2030, 1, 1)}); err != nil {
		t.Errorf("a modification of the closed account stopped the engine: %v", err)
	}
	// Other accounts and the asset pickers are untouched.
	if !slices.ContainsFunc(lineCandidates(effective(a.rec, demodata.ProjBase), ""), func(c lineCandidate) bool { return c.id == demodata.NetWorth }) {
		t.Error("an aggregate asset is missing from the line picker")
	}
}
