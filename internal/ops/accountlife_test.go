// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
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
	scratch := model.AccountID("scratch")
	mod := func(kind model.ObjectKind, id string) model.Modification {
		return model.Modification{Target: model.ObjectRef{Kind: kind, ID: id}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	}
	for _, c := range []struct {
		name  string
		block func(t *testing.T, st *store.Store, rec *model.Record)
		want  string
	}{
		{"entries", nil, "record entries"},
		{"account mod", func(t *testing.T, st *store.Store, rec *model.Record) {
			mustOp(t, func() (*Result, error) {
				return AddMod(st, rec, demodata.ProjBase, mod(model.KindAccount, "scratch"), Options{})
			})
		}, "projection"},
		{"asset mod", func(t *testing.T, st *store.Store, rec *model.Record) {
			mustOp(t, func() (*Result, error) {
				return AddMod(st, rec, demodata.ProjBase, mod(model.KindAsset, "acct:scratch"), Options{})
			})
		}, "projection"},
		{"asset setting", func(t *testing.T, st *store.Store, rec *model.Record) {
			mustOp(t, func() (*Result, error) {
				return AddAssetSetting(st, rec, demodata.ProjBase, model.AssetSetting{Asset: "acct:scratch", Start: model.Ptr(model.D(2027, time.January, 1))}, Options{})
			})
		}, "projection"},
		{"member", func(t *testing.T, st *store.Store, rec *model.Record) {
			mustOp(t, func() (*Result, error) {
				return AddMember(st, rec, demodata.Liquid, model.AccountMember(scratch), Options{})
			})
		}, "asset"},
		{"default asset member", func(t *testing.T, st *store.Store, rec *model.Record) {
			mustOp(t, func() (*Result, error) {
				return AddMember(st, rec, demodata.Liquid, model.AssetMember("acct:scratch"), Options{})
			})
		}, "asset"},
		{"budget item", func(t *testing.T, st *store.Store, rec *model.Record) {
			it := goodItem()
			it.Account = scratch
			mustOp(t, func() (*Result, error) { return AddBudgetItem(st, rec, it, Options{}) })
		}, "budget item"},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, rec := seed(t)
			target := demodata.TSP
			if c.block != nil {
				rec = withEmptyAccount(t, st, rec)
				c.block(t, st, rec)
				rec, target = reload(t, st), scratch
			}
			_, err := DeleteAccount(st, rec, target, Options{})
			if !errors.Is(err, ErrInUse) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want ErrInUse mentioning %q", err, c.want)
			}
			if reload(t, st).Account(target) == nil {
				t.Error("the account is gone")
			}
		})
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

func TestDeleteAssetChecksAccountModifications(t *testing.T) {
	growth := func(id model.AccountID) model.Modification {
		return model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(id)}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	}
	dataRefused(t, ErrInUse, func(st *store.Store, rec *model.Record) (*Result, error) {
		mustOp(t, func() (*Result, error) {
			return AddMod(st, rec, demodata.ProjBase, growth(demodata.Checking), Options{})
		})
		res, err := DeleteAsset(st, reload(t, st), demodata.Liquid, Options{})
		if err != nil && !strings.Contains(err.Error(), string(demodata.Checking)) {
			t.Errorf("the refusal does not name the checking modification: %v", err)
		}
		return res, err
	})
	// A modification of an account the asset does not contain does not stop the delete.
	st, rec := seed(t)
	mustOp(t, func() (*Result, error) {
		return AddMod(st, rec, demodata.ProjBase, growth(demodata.Brokerage), Options{})
	})
	if _, err := DeleteAsset(st, reload(t, st), demodata.Retirement, Options{}); err != nil {
		t.Errorf("unrelated delete: %v", err)
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
