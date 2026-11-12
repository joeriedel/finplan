// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// withEmptyAccount adds an account with no entries and returns the reloaded record.
func withEmptyAccount(t *testing.T, st *store.Store, rec *model.Record) *model.Record {
	t.Helper()
	if _, err := NewAccount(st, rec, model.Account{ID: "scratch", Name: "Scratch", Type: model.TypeSavings, Tax: model.Taxable}, Options{}); err != nil {
		t.Fatal(err)
	}
	return reload(t, st)
}

func TestCloseAndReopenAccount(t *testing.T) {
	date := model.D(2026, time.October, 31)
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return CloseAccount(st, rec, demodata.TSP, date, o)
	})
	rec := reload(t, st)
	if c := rec.Account(demodata.TSP).Closed; c == nil || *c != date || res.Account == nil || res.Account.Closed == nil {
		t.Fatalf("closed %v, result %+v", c, res.Account)
	}
	if len(rec.Account(demodata.TSP).Snapshots) == 0 {
		t.Error("closing lost the history")
	}

	for _, c := range []struct {
		name string
		id   model.AccountID
		date model.Date
		want error
	}{
		{"closed again", demodata.TSP, date, ErrInvalidValue},
		{"unknown", "nope", date, ErrUnknownObject},
	} {
		if _, err := CloseAccount(st, rec, c.id, c.date, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	dataRefused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return CloseAccount(st, rec, demodata.TSP, model.D(2026, time.June, 29), Options{})
	})
	dataRefused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return CloseAccount(st, rec, demodata.TSP, model.D(2026, time.July, 31), Options{})
	})

	if _, err := ReopenAccount(st, rec, demodata.Checking, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("reopen an open account: %v", err)
	}
	if _, err := ReopenAccount(st, rec, "nope", Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("reopen unknown: %v", err)
	}
	dry, err := ReopenAccount(st, rec, demodata.TSP, Options{DryRun: true})
	if err != nil || dry.Record.Account(demodata.TSP).Closed != nil || reload(t, st).Account(demodata.TSP).Closed == nil {
		t.Fatalf("dry-run reopen: %v", err)
	}
	if _, err := ReopenAccount(st, rec, demodata.TSP, Options{}); err != nil || reload(t, st).Account(demodata.TSP).Closed != nil {
		t.Errorf("reopen: %v", err)
	}
}

func TestCloseRefusesProjectionAccounts(t *testing.T) {
	st, rec := seed(t)
	if _, err := AddProjAccount(st, rec, demodata.ProjBase, model.ProjAccount{ID: "future", Name: "Future", Type: model.TypeSavings, Tax: model.Taxable, From: model.D(2027, time.January, 1)}, Options{}); err != nil {
		t.Fatal(err)
	}
	_, err := CloseAccount(st, reload(t, st), "future", model.D(2028, time.January, 1), Options{})
	if !errors.Is(err, ErrInvalidValue) || !strings.Contains(err.Error(), demodata.ProjBase) {
		t.Errorf("close a projection's account: %v", err)
	}
}

func TestDeleteAccount(t *testing.T) {
	st, rec := seed(t)
	rec = withEmptyAccount(t, st, rec)
	dry, err := DeleteAccount(st, rec, "scratch", Options{DryRun: true})
	if err != nil || dry.Record.Account("scratch") != nil || reload(t, st).Account("scratch") == nil {
		t.Fatalf("dry run: %v", err)
	}
	res, err := DeleteAccount(st, rec, "scratch", Options{})
	if err != nil || res.Removed != "scratch" || res.Account == nil {
		t.Fatalf("delete: %v, %+v", res, err)
	}
	after := reload(t, st)
	if after.Account("scratch") != nil || after.Asset("acct:scratch") != nil {
		t.Error("the account or its default asset is still there")
	}
	consecutive(t, after)
	if len(after.Accounts) != len(rec.Accounts)-1 {
		t.Error("deleted more than the account")
	}
	if _, err := after.Resolve(demodata.NetWorth); err != nil {
		t.Error(err)
	}
}

func TestDeleteAccountRefusals(t *testing.T) {
	st, rec := seed(t)
	_, err := DeleteAccount(st, rec, demodata.TSP, Options{})
	if !errors.Is(err, ErrInUse) || !strings.Contains(err.Error(), "close it instead") {
		t.Fatalf("got %v, want ErrInUse offering to close", err)
	}
	if reload(t, st).Account(demodata.TSP) == nil {
		t.Error("the account is gone")
	}
	dataRefused(t, ErrUnknownObject, func(st *store.Store, rec *model.Record) (*Result, error) {
		return DeleteAccount(st, rec, "nope", Options{})
	})
}

func mustOp(t *testing.T, f func() (*Result, error)) {
	t.Helper()
	if _, err := f(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAccountWithOnlyVoidedEntriesIsRefused(t *testing.T) {
	st, rec := seed(t)
	rec = withEmptyAccount(t, st, rec)
	res, err := AddSnapshot(st, rec, "scratch", model.D(2026, time.January, 1), 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VoidEntry(st, rec, res.Entry.ID, "", Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if len(rec.Account("scratch").Snapshots) != 0 {
		t.Fatal("the voided snapshot is still loaded")
	}
	if _, err := DeleteAccount(st, rec, "scratch", Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("got %v", err)
	}
}

func TestDeleteAssetKeepsAccountModifications(t *testing.T) {
	growth := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Checking)}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	st, rec := seed(t)
	mustOp(t, func() (*Result, error) { return AddMod(st, rec, demodata.ProjBase, growth, Options{}) })
	rec = reload(t, st)
	plan, err := AssetDeletePlan(st, rec, demodata.Liquid)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Affected {
		if a.Kind == AffectedModification {
			t.Errorf("the plan lists a modification: %+v", a)
		}
	}
	if _, err := DeleteAsset(st, rec, demodata.Liquid, Options{}); err != nil {
		t.Fatal(err)
	}
	after := reload(t, st)
	if after.Asset(demodata.Liquid) != nil || after.Account(demodata.Checking) == nil {
		t.Error("the asset is still there or the account is gone")
	}
	if !slices.ContainsFunc(after.Projection(demodata.ProjBase).Mods, func(m model.Modification) bool { return m.Target == growth.Target }) {
		t.Error("the modification of the account is gone")
	}
}

func TestRestoreDocumentsRestoresTheBaseBudget(t *testing.T) {
	st, rec := seed(t)
	res, err := AddBudgetItem(st, rec, baseItem1(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err = DeleteBudgetItem(st, res.Record, "groceries", Options{})
	if err != nil {
		t.Fatal(err)
	}
	back, err := RestoreDocuments(st, res.Record, rec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []*model.Record{back.Record, reload(t, st)} {
		if _, ok := r.Budget.Item("side-gig"); ok || len(r.Budget.Items) != len(rec.Budget.Items) {
			t.Errorf("items after undo: %d", len(r.Budget.Items))
		}
		if _, ok := r.Budget.Item("groceries"); !ok {
			t.Error("deleted item not restored")
		}
	}
}

func TestRestoreDocuments(t *testing.T) {
	st, rec := seed(t)
	before := rec
	name := "Renamed"
	res, err := EditAccount(st, rec, demodata.Checking, AccountChange{Name: &name}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// A snapshot added after the edit is kept by the undo.
	snap, err := AddSnapshot(st, res.Record, demodata.Checking, model.D(2030, time.January, 1), 9, Options{})
	if err != nil {
		t.Fatal(err)
	}
	back, err := RestoreDocuments(st, snap.Record, before, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := reload(t, st)
	if got.Account(demodata.Checking).Name == name || got.Asset("acct:checking").Name == name {
		t.Error("the name was not restored")
	}
	if n := len(got.Account(demodata.Checking).Snapshots); n != len(before.Account(demodata.Checking).Snapshots)+1 {
		t.Errorf("snapshots: %d", n)
	}
	if back.Record.Account(demodata.Checking).Name == name {
		t.Error("result record not restored")
	}

	// Undo of a new account: removed while it has no entries, refused once it has.
	st, rec = seed(t)
	added := withEmptyAccount(t, st, rec)
	if _, err := RestoreDocuments(st, added, rec, Options{DryRun: true}); err != nil || reload(t, st).Account("scratch") == nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := RestoreDocuments(st, added, rec, Options{}); err != nil || reload(t, st).Account("scratch") != nil {
		t.Fatalf("remove an account without entries: %v", err)
	}
	added = withEmptyAccount(t, st, rec)
	snap, err = AddSnapshot(st, added, "scratch", model.D(2026, time.January, 1), 1, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := RestoreDocuments(st, snap.Record, rec, Options{}); !errors.Is(err, ErrInUse) || res != nil {
		t.Errorf("remove an account with entries: %v", err)
	}
	if _, err := RestoreDocuments(st, added, rec, Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("remove an account whose entries are stored but not in the record given: %v", err)
	}
}

func TestRestoreDocumentsRestoresProjections(t *testing.T) {
	st, rec := seed(t)
	rec = withEmptyAccount(t, st, rec)
	growth := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: "scratch"}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	mustOp(t, func() (*Result, error) { return AddMod(st, rec, demodata.ProjBase, growth, Options{}) })
	before := reload(t, st)
	del, err := DeleteAccount(st, before, "scratch", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mods := reload(t, st).Projection(demodata.ProjBase).Mods; len(mods) != len(before.Projection(demodata.ProjBase).Mods)-1 {
		t.Fatalf("the delete left the modification: %d", len(mods))
	}
	back, err := RestoreDocuments(st, del.Record, before, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []*model.Record{back.Record, reload(t, st)} {
		if r.Account("scratch") == nil || !reflect.DeepEqual(r.Projections, before.Projections) {
			t.Error("the account or the projections were not restored")
		}
	}
	redo, err := RestoreDocuments(st, back.Record, del.Record, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if redo.Record.Account("scratch") != nil || !reflect.DeepEqual(reload(t, st).Projections, del.Record.Projections) {
		t.Error("the redo did not remove the account and its modification")
	}
}

func TestDeleteAccountNamedAsACounterIsRefused(t *testing.T) {
	st, rec := seed(t)
	rec = withEmptyAccount(t, st, rec)
	date := model.D(2026, time.January, 1)
	res, err := AddTransaction(st, rec, demodata.Checking, model.Transaction{Date: date, Amount: -5, Kind: model.TxTransfer, Counter: "scratch"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if _, err := DeleteAccount(st, rec, "scratch", Options{}); !errors.Is(err, ErrInUse) || !strings.Contains(err.Error(), string(demodata.Checking)) {
		t.Errorf("counter of a live entry: %v", err)
	}
	if plan, err := AccountDeletePlan(st, rec, "scratch"); err != nil || plan.Blocked == "" || len(plan.Affected) != 0 {
		t.Errorf("plan of a counter account: %+v, %v", plan, err)
	}
	if _, err := VoidEntry(st, rec, res.Entry.ID, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteAccount(st, reload(t, st), "scratch", Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("counter of a voided entry: %v", err)
	}
}

func TestEntriesAfterTheCloseDateAreRefused(t *testing.T) {
	st, rec := seed(t)
	rec = withEmptyAccount(t, st, rec)
	closed, after := model.D(2026, time.January, 31), model.D(2026, time.February, 1)
	res, err := AddSnapshot(st, rec, "scratch", model.D(2026, time.January, 1), 5, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if _, err := CloseAccount(st, rec, "scratch", closed, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if _, err := AddSnapshot(st, rec, "scratch", closed, 6, Options{}); err != nil {
		t.Errorf("a snapshot on the close date: %v", err)
	}
	if _, err := AddSnapshot(st, rec, "scratch", after, 6, Options{}); !errors.Is(err, ErrInvalidValue) || !strings.Contains(err.Error(), "closed") {
		t.Errorf("a snapshot after the close date: %v", err)
	}
	tx := model.Transaction{Date: after, Amount: 5, Kind: model.TxIncome}
	if _, err := AddTransaction(st, rec, "scratch", tx, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("a transaction after the close date: %v", err)
	}
	tx = model.Transaction{Date: after, Amount: -5, Kind: model.TxTransfer, Counter: "scratch"}
	if _, err := AddTransaction(st, rec, demodata.Checking, tx, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("a transfer to a closed account after its close date: %v", err)
	}
	if _, err := CorrectEntry(st, rec, res.Entry.ID, EntryChange{Date: &after}, "", Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("a correction moving an entry past the close date: %v", err)
	}
}

func TestCloseAccountChecksTransactionsAndCounters(t *testing.T) {
	st, rec := seed(t)
	rec = withEmptyAccount(t, st, rec)
	early, late := model.D(2026, time.November, 1), model.D(2026, time.November, 5)
	if _, err := AddTransaction(st, rec, "scratch", model.Transaction{Date: late, Amount: -5, Kind: model.TxExpense}, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CloseAccount(st, reload(t, st), "scratch", early, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("close before the last transaction: %v", err)
	}

	st, rec = seed(t)
	rec = withEmptyAccount(t, st, rec)
	res, err := AddTransaction(st, rec, demodata.Checking, model.Transaction{Date: late, Amount: -5, Kind: model.TxTransfer, Counter: "scratch"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if _, err := CloseAccount(st, rec, "scratch", early, Options{}); !errors.Is(err, ErrInvalidValue) || !strings.Contains(err.Error(), string(demodata.Checking)) {
		t.Errorf("close before a transfer into the account: %v", err)
	}
	if _, err := VoidEntry(st, rec, res.Entry.ID, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CloseAccount(st, reload(t, st), "scratch", early, Options{}); err != nil {
		t.Errorf("close before a voided transfer: %v", err)
	}
}

func TestRestoreDocumentsRefusesRemovingACounterAccount(t *testing.T) {
	st, rec := seed(t)
	added := withEmptyAccount(t, st, rec)
	tx := model.Transaction{Date: model.D(2026, time.January, 1), Amount: -5, Kind: model.TxTransfer, Counter: "scratch"}
	if _, err := AddTransaction(st, added, demodata.Checking, tx, Options{}); err != nil {
		t.Fatal(err)
	}
	if res, err := RestoreDocuments(st, reload(t, st), rec, Options{}); !errors.Is(err, ErrInUse) || res != nil {
		t.Errorf("remove a counter account: %v", err)
	}
}
