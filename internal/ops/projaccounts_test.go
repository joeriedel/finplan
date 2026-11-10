// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

var (
	houseDay = model.D(2027, 6, 1)
	house    = model.ProjAccount{ID: "house", Name: "House", Type: model.TypeProperty, Tax: model.Taxable, StatedRate: 0.03, Opening: 400000, From: houseDay}
)

func TestAddProjAccount(t *testing.T) {
	st, rec := seed(t)
	dry, err := AddProjAccount(st, rec, demodata.ProjNewJob, house, Options{DryRun: true})
	if err != nil || len(dry.Projection.Accounts) != 1 {
		t.Fatalf("dry run: %v %v", dry, err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Accounts; got != nil {
		t.Fatalf("a dry run saved %v", got)
	}
	res, err := AddProjAccount(st, rec, demodata.ProjNewJob, house, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Projection.Accounts, []model.ProjAccount{house}) {
		t.Errorf("result %v", res.Projection.Accounts)
	}
	got := reload(t, st)
	if !reflect.DeepEqual(got.Projection(demodata.ProjNewJob).Accounts, []model.ProjAccount{house}) {
		t.Errorf("saved %v", got.Projection(demodata.ProjNewJob).Accounts)
	}
	if got.Account("house") != nil || len(got.Accounts) != len(rec.Accounts) {
		t.Error("the account reached the record")
	}
	if len(rec.Projection(demodata.ProjNewJob).Accounts) != 0 {
		t.Error("the caller's record changed")
	}

	// An empty id is made from the name, and avoids the ids that are taken.
	h2 := house
	h2.ID = ""
	h2.Name = "Home"
	res, err = AddProjAccount(st, got, demodata.ProjNewJob, h2, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if id := res.Projection.Accounts[1].ID; id != "home-2" {
		t.Errorf("generated id %q", id)
	}
}

func TestAddProjAccountRefusals(t *testing.T) {
	st, rec := seed(t)
	with := func(f func(a *model.ProjAccount)) model.ProjAccount {
		a := house
		f(&a)
		return a
	}
	for name, tc := range map[string]struct {
		a    model.ProjAccount
		want error
	}{
		"no name":   {with(func(a *model.ProjAccount) { a.Name = "  " }), ErrInvalidValue},
		"bad type":  {with(func(a *model.ProjAccount) { a.Type = "castle" }), ErrInvalidValue},
		"bad tax":   {with(func(a *model.ProjAccount) { a.Tax = "free-ish" }), ErrInvalidValue},
		"bad rate":  {with(func(a *model.ProjAccount) { a.StatedRate = 5 }), ErrInvalidValue},
		"no date":   {with(func(a *model.ProjAccount) { a.From = 0 }), ErrInvalidValue},
		"record id": {with(func(a *model.ProjAccount) { a.ID = model.AccountID(demodata.Home) }), ErrNameTaken},
		"asset id":  {with(func(a *model.ProjAccount) { a.ID = model.AccountID(demodata.NetWorth) }), ErrNameTaken},
		"huge sum":  {with(func(a *model.ProjAccount) { a.Opening = 1e20 }), ErrInvalidValue},
		"reserved":  {with(func(a *model.ProjAccount) { a.ID = model.AccountID("acct:" + string(demodata.Home)) }), ErrNameTaken},
	} {
		if _, err := AddProjAccount(st, rec, demodata.ProjNewJob, tc.a, Options{}); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
	}
	if _, err := AddProjAccount(st, rec, "nonesuch", house, Options{}); !errors.Is(err, ErrUnknownProjection) {
		t.Errorf("unknown projection: %v", err)
	}
}

func TestProjAccountIDsAcrossTheChain(t *testing.T) {
	st, rec := seed(t)
	res, err := NewProjection(st, rec, "House", demodata.ProjNewJob, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	if res, err = AddProjAccount(st, rec, "House", house, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	// The parent may not take an id its child has, nor the child one its parent has.
	if _, err := AddProjAccount(st, rec, demodata.ProjNewJob, house, Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("parent taking the child's id: %v", err)
	}
	if res, err = AddProjAccount(st, rec, demodata.ProjNewJob, model.ProjAccount{ID: "other", Name: "Other", Type: model.TypeProperty, Tax: model.Taxable, From: houseDay}, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	if _, err := AddProjAccount(st, rec, "House", model.ProjAccount{ID: "other", Name: "Other", Type: model.TypeProperty, Tax: model.Taxable, From: houseDay}, Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("child taking the parent's id: %v", err)
	}
	// A sibling is free to use it.
	if res, err = NewProjection(st, rec, "Sibling", demodata.ProjNewJob, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddProjAccount(st, res.Record, "Sibling", house, Options{}); err != nil {
		t.Errorf("sibling of the child: %v", err)
	}
}

func TestEditProjAccount(t *testing.T) {
	st, rec := seed(t)
	res, err := AddProjAccount(st, rec, demodata.ProjNewJob, house, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	name, open, from := "Villa", 450000.0, houseDay.Add(30)
	ch := ProjAccountChange{Name: &name, Opening: &open, From: &from}
	dry, err := EditProjAccount(st, rec, demodata.ProjNewJob, "house", ch, Options{DryRun: true})
	if err != nil || dry.Projection.Accounts[0].Name != "Villa" {
		t.Fatalf("dry run: %v", err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Accounts[0]; got != house {
		t.Fatalf("a dry run saved %+v", got)
	}
	if _, err := EditProjAccount(st, rec, demodata.ProjNewJob, "house", ch, Options{}); err != nil {
		t.Fatal(err)
	}
	want := house
	want.Name, want.Opening, want.From = name, open, from
	if got := reload(t, st).Projection(demodata.ProjNewJob).Accounts[0]; got != want {
		t.Errorf("saved %+v, want %+v", got, want)
	}
	typ := model.AccountType("castle")
	if _, err := EditProjAccount(st, rec, demodata.ProjNewJob, "house", ProjAccountChange{Type: &typ}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("bad type: %v", err)
	}
	if _, err := EditProjAccount(st, rec, demodata.ProjNewJob, "house", ProjAccountChange{}, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("no change: %v", err)
	}
	if _, err := EditProjAccount(st, rec, demodata.ProjNewJob, "nonesuch", ch, Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown account: %v", err)
	}
}

func TestRemoveProjAccountInUse(t *testing.T) {
	st, rec := seed(t)
	steps := []func(rec *model.Record) (*Result, error){
		func(rec *model.Record) (*Result, error) {
			return AddProjAccount(st, rec, demodata.ProjNewJob, house, Options{})
		},
		func(rec *model.Record) (*Result, error) {
			return NewProjection(st, rec, "House", demodata.ProjNewJob, Options{})
		},
		func(rec *model.Record) (*Result, error) {
			it := model.BudgetItem{Kind: model.ItemTransfer, Name: "Rent house", Amount: model.Constant(100), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Start: model.Ptr(model.D(2027, 1, 1)), From: demodata.Checking, To: "house"}
			return AddItem(st, rec, "House", it, Options{})
		},
	}
	for _, step := range steps {
		res, err := step(rec)
		if err != nil {
			t.Fatal(err)
		}
		rec = res.Record
	}
	if _, err := RemoveProjAccount(st, rec, demodata.ProjNewJob, "house", Options{}); !errors.Is(err, ErrInUse) {
		t.Fatalf("an item of a child uses it: %v", err)
	}
	res, err := RemoveMod(st, rec, "House", 0, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// A modification of the account in the owning projection also holds it.
	m := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: "house"}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	if res, err = AddMod(st, res.Record, demodata.ProjNewJob, m, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveProjAccount(st, res.Record, demodata.ProjNewJob, "house", Options{}); !errors.Is(err, ErrInUse) {
		t.Fatalf("a modification uses it: %v", err)
	}
	if res, err = RemoveMod(st, res.Record, demodata.ProjNewJob, len(res.Record.Projection(demodata.ProjNewJob).Mods)-1, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	dry, err := RemoveProjAccount(st, rec, demodata.ProjNewJob, "house", Options{DryRun: true})
	if err != nil || len(dry.Projection.Accounts) != 0 {
		t.Fatalf("dry run: %v", err)
	}
	if len(reload(t, st).Projection(demodata.ProjNewJob).Accounts) != 1 {
		t.Fatal("a dry run removed the account")
	}
	if _, err := RemoveProjAccount(st, rec, demodata.ProjNewJob, "house", Options{}); err != nil {
		t.Fatal(err)
	}
	if len(reload(t, st).Projection(demodata.ProjNewJob).Accounts) != 0 {
		t.Error("the account was not removed")
	}
	if _, err := RemoveProjAccount(st, reload(t, st), demodata.ProjNewJob, "house", Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("removing it twice: %v", err)
	}
}

func TestProjectionAccountsAreTargets(t *testing.T) {
	st, rec := seed(t)
	res, err := AddProjAccount(st, rec, demodata.ProjNewJob, house, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	if res, err = NewProjection(st, rec, "House", demodata.ProjNewJob, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	grow := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: "house"}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	for _, proj := range []string{demodata.ProjNewJob, "House"} {
		if err := CheckMod(rec, proj, grow); err != nil {
			t.Errorf("%s: %v", proj, err)
		}
	}
	for _, proj := range []string{demodata.ProjBase, demodata.ProjWithdraw} {
		if err := CheckMod(rec, proj, grow); !errors.Is(err, ErrUnknownTarget) {
			t.Errorf("%s: %v", proj, err)
		}
	}
	asset := grow
	asset.Target = model.ObjectRef{Kind: model.KindAsset, ID: "acct:house"}
	if err := CheckMod(rec, "House", asset); err != nil {
		t.Errorf("default asset: %v", err)
	}
	// Items in the projection and its children may use it; a transfer needs one account of the record.
	item := model.BudgetItem{Kind: model.ItemTransfer, Name: "Rent house", Amount: model.Constant(100), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Start: model.Ptr(model.D(2027, 1, 1)), From: demodata.Checking, To: "house"}
	if _, err := AddItem(st, rec, "House", item, Options{DryRun: true}); err != nil {
		t.Errorf("item in the child: %v", err)
	}
	income := model.BudgetItem{Kind: model.ItemIncome, Name: "Rent out", Amount: model.Constant(100), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Start: model.Ptr(model.D(2027, 1, 1)), Account: "house"}
	if _, err := AddItem(st, rec, "House", income, Options{DryRun: true}); err != nil {
		t.Errorf("income in the child: %v", err)
	}
	if _, err := AddItem(st, rec, demodata.ProjBase, income, Options{DryRun: true}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("income in Base: %v", err)
	}
	if _, err := EditItem(st, rec, "House", "nonesuch", income, Options{DryRun: true}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("edit of a missing item: %v", err)
	}
	if err := CheckItem(effectiveOf(t, rec, "House"), item); err != nil {
		t.Errorf("CheckItem: %v", err)
	}
}

func effectiveOf(t *testing.T, rec *model.Record, proj string) *model.Record {
	t.Helper()
	view, err := rec.For(proj)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestSetParentRefusesAccountIDCollision(t *testing.T) {
	st, rec := seed(t)
	for _, step := range []func(*model.Record) (*Result, error){
		func(r *model.Record) (*Result, error) {
			return AddProjAccount(st, r, demodata.ProjNewJob, house, Options{})
		},
		func(r *model.Record) (*Result, error) {
			return NewProjection(st, r, "Other", demodata.ProjBase, Options{})
		},
		func(r *model.Record) (*Result, error) { return AddProjAccount(st, r, "Other", house, Options{}) },
		func(r *model.Record) (*Result, error) { return NewProjection(st, r, "Below", "Other", Options{}) },
	} {
		res, err := step(rec)
		if err != nil {
			t.Fatal(err)
		}
		rec = res.Record
	}
	for _, tc := range [][2]string{{"Other", demodata.ProjNewJob}, {demodata.ProjNewJob, "Other"}} {
		if _, err := SetParent(st, rec, tc[0], tc[1], Options{}); !errors.Is(err, ErrNameTaken) {
			t.Errorf("%v: got %v", tc, err)
		}
	}
	// Below has no accounts of its own, so it may move under New job.
	if _, err := SetParent(st, rec, "Below", demodata.ProjNewJob, Options{DryRun: true}); err != nil {
		t.Errorf("a reparent without a collision: %v", err)
	}
	if got := reload(t, st); got.Projection("Other").Parent != demodata.ProjBase || got.Projection(demodata.ProjNewJob).Parent != demodata.ProjBase {
		t.Error("a refused edit changed a parent")
	}
}

func TestRecordIDsAvoidProjectionAccounts(t *testing.T) {
	st, rec := seed(t)
	res, err := AddProjAccount(st, rec, demodata.ProjNewJob, house, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	acct := model.Account{ID: "house", Name: "House", Type: model.TypeChecking, Tax: model.Taxable}
	if _, err := NewAccount(st, rec, acct, Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("account with a projection account's id: %v", err)
	}
	if _, err := NewAsset(st, rec, "House", "house", Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("asset with a projection account's id: %v", err)
	}
	acct.ID = ""
	if res, err = NewAccount(st, rec, acct, Options{}); err != nil || res.Account.ID != "house-2" {
		t.Errorf("made id: %v, %v", res, err)
	}
}

func TestRestoreProjectionsRefusesIDCollision(t *testing.T) {
	st, rec := seed(t)
	res, err := AddProjAccount(st, rec, demodata.ProjNewJob, house, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	ps := rec.WithProjections(rec.Projections).Projections
	ps[slices.IndexFunc(ps, func(p *model.Projection) bool { return p.Name == demodata.ProjNewJob })].Accounts[0].ID = model.AccountID(demodata.Home)
	if res, err := RestoreProjections(st, rec, ps, nil, Options{}); !errors.Is(err, ErrNameTaken) || res != nil {
		t.Fatalf("got %v, %v", res, err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Accounts[0].ID; got != "house" {
		t.Errorf("a refused restore saved account %q", got)
	}
}
