// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

func fillAccount(f *acctForm, pairs ...string) {
	for i := 0; i+1 < len(pairs); i += 2 {
		f.set(pairs[i], pairs[i+1])
	}
}

// addHouse adds a home account to the projection through the tab's Add form.
func addHouse(t *testing.T, a *app, proj string) {
	t.Helper()
	pt := a.proj
	pt.addAcct = acctForm{}
	fillAccount(&pt.addAcct, "name", "House", "type", "property", "tax", "taxable", "rate", "3", "opening", "400000", "from", "2027-06-01")
	if got := acctProblem(a.st, a.rec, proj, pt.addAcct); got != "" {
		t.Fatalf("complete form: %q", got)
	}
	acct, err := pt.addAcct.account()
	if err != nil {
		t.Fatal(err)
	}
	if !a.edit("add account", func() (*ops.Result, error) {
		return ops.AddProjAccount(a.st, a.rec, proj, acct, ops.Options{})
	}) {
		t.Fatalf("not added: %v", a.notices)
	}
}

func TestAccountFormProblems(t *testing.T) {
	a := demoApp(t)
	var f acctForm
	for _, tc := range []struct {
		pairs []string
		want  string
	}{
		{nil, "opening date"},
		{[]string{"from", "2027-06-01"}, "type"},
		{[]string{"type", "property"}, "tax"},
		{[]string{"tax", "taxable"}, "name"},
		{[]string{"name", "House"}, ""},
		{[]string{"rate", "lots"}, "not a number"},
		{[]string{"rate", "3%", "opening", "x"}, "not a number"},
		{[]string{"opening", "-1000"}, ""},
	} {
		fillAccount(&f, tc.pairs...)
		got := acctProblem(a.st, a.rec, demodata.ProjNewJob, f)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("after %v: got %q, want a problem about %q", tc.pairs, got, tc.want)
		}
	}
	a2, _ := f.account()
	if a2.StatedRate != 0.03 || a2.Opening != -1000 {
		t.Errorf("parsed %+v", a2)
	}
}

func TestAccountsInTheTab(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	child := newChild(t, a, demodata.ProjNewJob)
	addHouse(t, a, demodata.ProjNewJob)

	rows := accountRows(a.rec, child)
	if len(rows) != 1 || rows[0].ID != "house" || rows[0].Own || rows[0].Owner != demodata.ProjNewJob {
		t.Fatalf("inherited rows: %+v", rows)
	}
	if rows := accountRows(a.rec, demodata.ProjNewJob); len(rows) != 1 || !rows[0].Own {
		t.Errorf("own rows: %+v", rows)
	}
	if rows := accountRows(a.rec, demodata.ProjBase); len(rows) != 0 {
		t.Errorf("Base rows: %+v", rows)
	}

	// The pickers offer it under the projection and its child, and not elsewhere.
	has := func(proj string) bool {
		return slices.ContainsFunc(objectsFor(a.rec, model.KindAccount, proj, 0), func(c objectChoice) bool { return c.id == "house" })
	}
	if !has(demodata.ProjNewJob) || !has(child) || has(demodata.ProjBase) || has(demodata.ProjWithdraw) {
		t.Error("wrong account choices")
	}
	if n := len(objectsFor(a.rec, model.KindAsset, child, 0)); n != len(objectsFor(a.rec, model.KindAsset, demodata.ProjBase, 0)) {
		t.Errorf("default assets leaked into the asset choices: %d", n)
	}
	if len(effective(a.rec, child).Accounts) != len(a.rec.Accounts)+1 || len(a.rec.Accounts) != 13 {
		t.Error("effective view")
	}

	// Edits go through the same ops, and an invalid one leaves the field's error.
	pt.editAccount(demodata.ProjNewJob, "house", "opening", "450000")
	if got := a.rec.Projection(demodata.ProjNewJob).Accounts[0].Opening; got != 450000 {
		t.Errorf("opening %v", got)
	}
	pt.editAccount(demodata.ProjNewJob, "house", "from", "soon")
	if pt.errs["accthousefrom"] == "" {
		t.Error("a bad date was not reported")
	}
	pt.editAccount(demodata.ProjNewJob, "house", "rate", "500")
	if a.rec.Projection(demodata.ProjNewJob).Accounts[0].StatedRate != 0.03 {
		t.Error("an out of range rate was saved")
	}

	// Net worth under the child counts it; undo takes it away again.
	day := model.D(2027, 6, 1)
	nw := func(proj string) float64 {
		s := a.graph.cache.samples(demodata.NetWorth, proj, graph.Grid{Step: 1, First: day, Last: day})
		if len(s) != 1 {
			t.Fatalf("no net worth under %q", proj)
		}
		return s[0].Amount
	}
	if diff := nw(child) - nw(demodata.ProjNewJob); diff != 0 {
		t.Errorf("the child differs from its parent by %v", diff)
	}
	with := nw(demodata.ProjNewJob)
	a.undo()
	a.undo()
	if len(a.rec.Projection(demodata.ProjNewJob).Accounts) != 0 {
		t.Error("undo kept the account")
	}
	if diff := with - nw(demodata.ProjNewJob); !closeTo(diff, 450000) {
		t.Errorf("net worth counts the account for %v, want 450000", diff)
	}
}

func TestLineCandidatesIncludeTheProjectionsAccounts(t *testing.T) {
	a := demoApp(t)
	addHouse(t, a, demodata.ProjNewJob)
	names := func(proj, search string) []string {
		var out []string
		for _, c := range lineCandidates(effective(a.rec, proj), search) {
			out = append(out, string(c.id)+"/"+c.kind.String())
		}
		return out
	}
	if !slices.Contains(names(demodata.ProjNewJob, ""), "acct:house/account") || slices.Contains(names(demodata.ProjBase, ""), "acct:house/account") {
		t.Error("the picker lists the wrong accounts")
	}
	if got := names(demodata.ProjNewJob, "hous"); !slices.Equal(got, []string{"acct:house/account"}) {
		t.Errorf("search: %v", got)
	}
	if a.graph.assetName("acct:house") != "House" {
		t.Errorf("line name %q", a.graph.assetName("acct:house"))
	}
	// A line of the account, pinned to the projection, draws and survives a reload.
	g := a.graph
	g.lines.Add("acct:house", true, demodata.ProjNewJob, graph.AssetColor(effective(a.rec, demodata.ProjNewJob), "acct:house"))
	day := model.D(2027, 6, 1)
	if s := g.cache.samples("acct:house", demodata.ProjNewJob, graph.Grid{Step: 1, First: day, Last: day}); len(s) != 1 || s[0].Amount != 400000 {
		t.Errorf("the line's data: %v", s)
	}
	has := recordHas(a.rec)
	if !has("acct:house", demodata.ProjNewJob) || has("acct:house", demodata.ProjBase) || !has("acct:house", "") {
		t.Error("recordHas")
	}
}
