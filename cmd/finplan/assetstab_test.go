// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

func assetRowOf(rows []*assetRow, id model.AssetID) *assetRow {
	for _, r := range rows {
		if r.Asset.ID == id {
			return r
		}
	}
	return nil
}

func TestAssetTreeRowsAndBalances(t *testing.T) {
	a := demoApp(t)
	rows := buildAssets(a.rec)
	for _, r := range rows {
		if isDefaultAsset(r.Asset.ID) {
			t.Errorf("row for the default asset %s", r.Asset.ID)
		}
	}
	nw := assetRowOf(rows, demodata.NetWorth)
	if nw == nil || len(nw.Members) == 0 || nw.Err != "" {
		t.Fatalf("net worth row %+v", nw)
	}
	res, err := engine.Balances(a.rec, "", demodata.NetWorth, []model.Date{a.rec.Today})
	if err != nil || nw.Balance != res.Samples[0].Amount || nw.Balance == 0 {
		t.Errorf("net worth balance %v, want %v (%v)", nw.Balance, res.Samples[0].Amount, err)
	}
	// All cash is a rule member that shows what it matches.
	cash := assetRowOf(rows, demodata.AllCash)
	if len(cash.Members) != 1 || cash.Members[0].Member.Rule == nil || !strings.Contains(cash.Members[0].Detail, "Checking") || !strings.Contains(cash.Members[0].Detail, "Savings") {
		t.Errorf("all cash members %+v", cash.Members)
	}
	if want := rowOf(listAccounts(a.rec), demodata.Checking).Balance + rowOf(listAccounts(a.rec), demodata.Savings).Balance; cash.Balance != want || cash.Members[0].Balance != want {
		t.Errorf("all cash balance %v, rule %v, want %v", cash.Balance, cash.Members[0].Balance, want)
	}
	// Asset members nest.
	liquid := assetRowOf(rows, demodata.Liquid)
	if i := slices.IndexFunc(liquid.Members, func(m memberRow) bool { return m.Member.Asset == demodata.AllCash }); i < 0 || liquid.Members[i].Child == nil || len(liquid.Members[i].Child.Members) != 1 {
		t.Errorf("liquid members %+v", liquid.Members)
	}
}

func TestAssetOverlapReasonNamesTheDuplicate(t *testing.T) {
	a := demoApp(t)
	liquid := assetRowOf(buildAssets(a.rec), demodata.Liquid)
	var bad []memberRow
	for _, m := range liquid.Members {
		if m.Problem != "" {
			bad = append(bad, m)
		}
	}
	// Checking is in liquid assets twice: through all cash and as a member of its own.
	if len(bad) != 1 || bad[0].Member.Account != demodata.Checking {
		t.Fatalf("members with a problem: %+v", bad)
	}
	if p := bad[0].Problem; !strings.Contains(p, "Checking") || !strings.Contains(p, "All cash") {
		t.Errorf("problem %q", p)
	}
	// Removing the duplicate clears the problem.
	seq := bad[0].Member.Seq
	if !a.edit("remove member", func() (*ops.Result, error) {
		return ops.RemoveMember(a.st, a.rec, demodata.Liquid, seq, ops.Options{})
	}) {
		t.Fatal("remove member failed")
	}
	for _, m := range assetRowOf(buildAssets(a.rec), demodata.Liquid).Members {
		if m.Problem != "" {
			t.Errorf("member still has a problem: %+v", m)
		}
	}
}

func TestAssetCoverageAndChart(t *testing.T) {
	a := demoApp(t)
	c := buildChart(a.rec, demodata.Liquid)
	if c.Err != "" || !c.HasActual || len(c.Coverage) == 0 || len(c.Segs) == 0 {
		t.Fatalf("chart %+v", c)
	}
	res, _ := a.rec.Resolve(demodata.Liquid)
	if !slices.Equal(c.Coverage, res.Coverage()) {
		t.Errorf("coverage %v, want %v", c.Coverage, res.Coverage())
	}
	if want, _ := res.LastActual(); c.LastActual != want {
		t.Errorf("last actual %v, want %v", c.LastActual, want)
	}
	seen := map[engine.Status]bool{}
	for _, s := range c.Segs {
		seen[s.Status] = true
		if len(s.Pts) < 2 && len(c.Segs) > 1 {
			t.Errorf("run of %d points", len(s.Pts))
		}
	}
	if !seen[engine.StatusActual] || !(seen[engine.StatusProjected] || seen[engine.StatusPartlyProjected]) {
		t.Errorf("statuses %v: want actual and projected runs", seen)
	}
	if c.Min > c.Max {
		t.Errorf("range %v..%v", c.Min, c.Max)
	}

	// An aggregate with no members has no coverage and no actual data.
	st, rec := a.st, a.rec
	r, err := ops.NewAsset(st, rec, "Empty", "", ops.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := buildChart(r.Record, "empty")
	if e.Err != "" || e.HasActual || len(e.Coverage) != 0 {
		t.Errorf("empty asset chart %+v", e)
	}
}

func TestAssetCoverageGapsStayGaps(t *testing.T) {
	a := demoApp(t)
	rec := a.rec.Clone()
	// Two accounts whose data does not meet leave a gap between their spans.
	early := rec.Account(demodata.Savings)
	early.Snapshots = early.Snapshots[:1]
	early.Transactions = nil
	late := rec.Account(demodata.Checking)
	late.Snapshots = late.Snapshots[len(late.Snapshots)-1:]
	late.Transactions = nil
	es, _ := early.Span()
	ls, _ := late.Span()
	if ls.From.Add(-1) <= es.To {
		t.Skipf("demo spans %v and %v meet", es, ls)
	}
	c := buildChart(rec, demodata.AllCash)
	if len(c.Coverage) != 2 || c.Coverage[0].To >= c.Coverage[1].From {
		t.Errorf("coverage %v", c.Coverage)
	}
}

func TestAssetProblems(t *testing.T) {
	a := demoApp(t)
	for _, c := range []struct {
		name, want string
	}{
		{"", "enter a name"},
		{"  ", "enter a name"},
		{"net WORTH", "net worth"},
		{"Savings", "account named"},
		{"Emergency fund", ""},
	} {
		got := newAssetProblem(a.st, a.rec, c.name)
		if (c.want == "") != (got == "") || !strings.Contains(strings.ToLower(got), strings.ToLower(c.want)) {
			t.Errorf("new %q: problem %q, want one with %q", c.name, got, c.want)
		}
	}
	if got := renameAssetProblem(a.st, a.rec, demodata.Retirement, "liquid assets"); !strings.Contains(got, "Liquid assets") {
		t.Errorf("rename to a taken name: %q", got)
	}
	if got := renameAssetProblem(a.st, a.rec, demodata.Retirement, "Retirement"); got != "" {
		t.Errorf("rename to the same name: %q", got)
	}
	if got := renameAssetProblem(a.st, a.rec, demodata.Retirement, "RETIREMENT"); got != "" {
		t.Errorf("rename to another case of its own name: %q", got)
	}
	if got := renameAssetProblem(a.st, a.rec, "acct:checking", "x"); got == "" {
		t.Error("an account's default asset can be renamed")
	}
	if got := deleteAssetProblem(a.st, a.rec, demodata.AllCash); got != "" {
		t.Errorf("delete of a member: %q", got)
	}
	if got := deleteAssetProblem(a.st, a.rec, demodata.NetWorth); got != "" {
		t.Errorf("net worth cannot be deleted: %q", got)
	}
	if got := deleteAssetProblem(a.st, a.rec, "acct:checking"); got == "" {
		t.Error("an account's default asset can be deleted")
	}
}

func TestEmergencyFundScenario(t *testing.T) {
	a := demoApp(t)
	var id model.AssetID
	if !a.edit("add asset", func() (*ops.Result, error) {
		r, err := ops.NewAsset(a.st, a.rec, "Emergency fund", "", ops.Options{})
		if err == nil {
			id = r.Asset.ID
		}
		return r, err
	}) {
		t.Fatal("new asset")
	}
	for _, acct := range []model.AccountID{demodata.Savings, demodata.Checking} {
		m := model.AccountMember(acct)
		if why := memberProblem(a.st, a.rec, id, memberForm{kind: memberAccount, account: acct}); why != "" {
			t.Fatalf("member problem %q", why)
		}
		if !a.edit("add member", func() (*ops.Result, error) { return ops.AddMember(a.st, a.rec, id, m, ops.Options{}) }) {
			t.Fatal("add member")
		}
	}
	row := assetRowOf(buildAssets(a.rec), id)
	want := rowOf(listAccounts(a.rec), demodata.Checking).Balance + rowOf(listAccounts(a.rec), demodata.Savings).Balance
	if row == nil || len(row.Members) != 2 || row.Balance != want {
		t.Fatalf("row %+v, want balance %v", row, want)
	}
	if !a.edit("rename asset", func() (*ops.Result, error) {
		return ops.RenameAsset(a.st, a.rec, id, "Rainy day", ops.Options{})
	}) || a.rec.Asset(id).Name != "Rainy day" {
		t.Fatal("rename")
	}
	if why := deleteAssetProblem(a.st, a.rec, id); why != "" {
		t.Fatalf("delete problem %q", why)
	}
	if !a.edit("delete asset", func() (*ops.Result, error) { return ops.DeleteAsset(a.st, a.rec, id, ops.Options{}) }) || a.rec.Asset(id) != nil {
		t.Fatal("delete")
	}
	// Undo of the delete brings the asset back with its members and its new name.
	a.undo()
	if got := a.rec.Asset(id); got == nil || got.Name != "Rainy day" || len(got.Members) != 2 {
		t.Errorf("after undo: %+v", got)
	}
	a.undo()
	if got := a.rec.Asset(id); got == nil || got.Name != "Emergency fund" {
		t.Errorf("after the second undo: %+v", got)
	}
}

func TestDeleteAssetCascadeIsOneUndoStep(t *testing.T) {
	a := demoApp(t)
	r, err := ops.NewAsset(a.st, a.rec, "Spare", "", ops.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a.install(r)
	if r, err = ops.AddMember(a.st, a.rec, demodata.Liquid, model.AssetMember("spare"), ops.Options{}); err != nil {
		t.Fatal(err)
	}
	a.install(r)
	if r, err = ops.AddAssetSetting(a.st, a.rec, demodata.ProjBase, model.AssetSetting{Asset: "spare", Start: model.Ptr(model.D(2027, 1, 1))}, ops.Options{}); err != nil {
		t.Fatal(err)
	}
	a.install(r)
	if why := deleteAssetProblem(a.st, a.rec, "spare"); why != "" {
		t.Fatalf("delete problem %q", why)
	}
	plan, err := ops.AssetDeletePlan(a.rec, "spare")
	if err != nil {
		t.Fatal(err)
	}
	lines := deleteLines(plan)
	if len(lines) != 2 || !strings.Contains(lines[0], string(demodata.Liquid)) || !strings.Contains(lines[1], demodata.ProjBase) {
		t.Errorf("lines %q", lines)
	}
	members := len(a.rec.Asset(demodata.Liquid).Members)
	if !a.edit("delete asset", func() (*ops.Result, error) { return ops.DeleteAsset(a.st, a.rec, "spare", ops.Options{}) }) {
		t.Fatal(a.notices)
	}
	if a.rec.Asset("spare") != nil || len(a.rec.Asset(demodata.Liquid).Members) != members-1 || len(a.rec.Projection(demodata.ProjBase).AssetSettings) != 0 {
		t.Fatal("the delete left the asset, its membership or its setting")
	}
	a.undo()
	if a.rec.Asset("spare") == nil || len(a.rec.Asset(demodata.Liquid).Members) != members || len(a.rec.Projection(demodata.ProjBase).AssetSettings) != 1 {
		t.Error("one undo did not restore the asset, its membership and its setting")
	}
	a.redo()
	if a.rec.Asset("spare") != nil || len(a.rec.Asset(demodata.Liquid).Members) != members-1 || len(a.rec.Projection(demodata.ProjBase).AssetSettings) != 0 {
		t.Error("redo did not repeat the whole delete")
	}
}

func TestDeleteLines(t *testing.T) {
	plan := ops.DeletePlan{Affected: []ops.Affected{
		{Kind: ops.AffectedMember, In: "liquid", Detail: `account "x"`, Effect: ops.EffectRemoved},
		{Kind: ops.AffectedAssetSetting, In: "Base", Detail: `asset "y"`, Effect: ops.EffectRemoved},
		{Kind: ops.AffectedModification, In: "Base", Detail: "growth of account \"x\"", Effect: ops.EffectRemoved},
		{Kind: ops.AffectedBudgetItem, In: "budget", Detail: `"Rent" (rent)`, Effect: ops.EffectDeleted},
		{Kind: ops.AffectedBudgetItem, In: "Base", Detail: `"Gig" (gig)`, Effect: ops.EffectDeleted},
	}}
	want := []string{
		`Asset "liquid": remove its member account "x"`,
		`Projection "Base": remove the setting for asset "y"`,
		`Projection "Base": remove the modification growth of account "x"`,
		`Base budget: delete the item "Rent" (rent)`,
		`Projection "Base": delete the item it adds, "Gig" (gig)`,
	}
	if got := deleteLines(plan); !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestRuleMemberTakesNewAccounts(t *testing.T) {
	a := demoApp(t)
	r, err := ops.NewAsset(a.st, a.rec, "Cash pool", "", ops.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a.install(r)
	f := memberForm{kind: memberRule, types: []model.AccountType{model.TypeChecking, model.TypeSavings}}
	m, missing := f.member()
	if missing != "" || memberProblem(a.st, a.rec, "cash-pool", f) != "" {
		t.Fatalf("rule form: %q", missing)
	}
	if !a.edit("add member", func() (*ops.Result, error) { return ops.AddMember(a.st, a.rec, "cash-pool", m, ops.Options{}) }) {
		t.Fatal("add member")
	}
	before := len(assetAccounts(t, a.rec, "cash-pool"))
	row := assetRowOf(buildAssets(a.rec), "cash-pool")
	if before != 2 || !strings.Contains(row.Members[0].Detail, "Checking") || strings.Contains(row.Members[0].Detail, "Vacation") {
		t.Fatalf("before: %d accounts, %+v", before, row.Members)
	}

	if !a.edit("add account", func() (*ops.Result, error) {
		return ops.NewAccount(a.st, a.rec, model.Account{Name: "Vacation", Type: model.TypeSavings, Tax: model.Taxable}, ops.Options{})
	}) {
		t.Fatal("new account")
	}
	if got := assetAccounts(t, a.rec, "cash-pool"); len(got) != before+1 || !slices.Contains(got, "vacation") {
		t.Errorf("accounts after: %v", got)
	}
	row = assetRowOf(buildAssets(a.rec), "cash-pool")
	if !strings.Contains(row.Members[0].Detail, "Vacation") {
		t.Errorf("tree after: %+v", row.Members[0])
	}
}

func assetAccounts(t *testing.T, rec *model.Record, id model.AssetID) []model.AccountID {
	t.Helper()
	res, err := rec.Resolve(id)
	if err != nil {
		t.Fatal(err)
	}
	return res.Accounts
}

func TestMemberFormProblems(t *testing.T) {
	a := demoApp(t)
	for _, c := range []struct {
		f    memberForm
		want string
	}{
		{memberForm{}, "kind"},
		{memberForm{kind: memberAccount}, "pick an account"},
		{memberForm{kind: memberAsset}, "pick an asset"},
		{memberForm{kind: memberAsset, asset: demodata.Liquid}, "cycle"},
		{memberForm{kind: memberAccount, account: demodata.Savings}, ""},
		{memberForm{kind: memberRule}, ""},
	} {
		got := memberProblem(a.st, a.rec, demodata.AllCash, c.f)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%+v: problem %q, want one with %q", c.f, got, c.want)
		}
	}
	if got := toggle([]string{"a"}, "b"); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("toggle on: %v", got)
	}
	if got := toggle([]string{"a", "b"}, "a"); !slices.Equal(got, []string{"b"}) {
		t.Errorf("toggle off: %v", got)
	}
}

func TestAssetTreeSurvivesACycle(t *testing.T) {
	a := demoApp(t)
	rec := a.rec.Clone()
	// The model refuses a cycle, so build one by hand to check that the tree does not recurse forever.
	rec.Asset(demodata.AllCash).Members = append(rec.Asset(demodata.AllCash).Members, model.Member{Seq: 99, Asset: demodata.Liquid})
	rows := buildAssets(rec)
	cash := assetRowOf(rows, demodata.AllCash)
	if cash.Err == "" {
		t.Error("a cycle shows no error")
	}
	if c := buildChart(rec, demodata.AllCash); c.Err == "" {
		t.Error("the chart of a cycle shows no error")
	}
}
