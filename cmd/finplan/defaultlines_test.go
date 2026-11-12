// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

func TestDefaultLines(t *testing.T) {
	mustDo := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	acct := func(id model.AccountID) *model.Account {
		return &model.Account{ID: id, Name: string(id), Type: model.TypeChecking, Tax: model.Taxable}
	}

	if got := defaultLines(model.NewRecord(0), false); len(got) != 0 {
		t.Errorf("an empty record has lines %v", got)
	}

	accountsOnly := model.NewRecord(0)
	mustDo(accountsOnly.AddAccount(acct("zeta")))
	mustDo(accountsOnly.AddAccount(acct("alpha")))
	want := []model.AssetID{model.DefaultAssetID("zeta"), model.DefaultAssetID("alpha")}
	if got := defaultLines(accountsOnly, false); !reflect.DeepEqual(got, want) {
		t.Errorf("accounts only: %v, want %v", got, want)
	}

	withAggregates := model.NewRecord(0)
	mustDo(withAggregates.AddAccount(acct("zeta")))
	mustDo(withAggregates.AddAccount(acct("alpha")))
	mustDo(withAggregates.AddAsset(&model.Asset{ID: "single", Name: "Single", Members: []model.Member{model.AccountMember("zeta")}}))
	mustDo(withAggregates.AddAsset(&model.Asset{ID: "everything", Name: "Everything", Members: []model.Member{model.RuleMember(model.Rule{Types: []model.AccountType{model.TypeChecking}})}}))
	mustDo(withAggregates.AddAsset(&model.Asset{ID: "outer", Name: "Outer", Members: []model.Member{model.AssetMember("everything")}}))
	want = []model.AssetID{"everything", "outer"}
	if got := defaultLines(withAggregates, false); !reflect.DeepEqual(got, want) {
		t.Errorf("with aggregates: %v, want %v", got, want)
	}

	want = []model.AssetID{
		demodata.NetWorth, demodata.AllCash, demodata.Retirement,
		model.DefaultAssetID(demodata.Brokerage), model.DefaultAssetID(demodata.TSP), model.DefaultAssetID(demodata.Mortgage),
		model.DefaultAssetID(demodata.College1), model.DefaultAssetID(demodata.College2), model.DefaultAssetID(demodata.College3), model.DefaultAssetID(demodata.College4),
	}
	if got := defaultLines(demodata.Family(), true); !reflect.DeepEqual(got, want) {
		t.Errorf("demo: %v, want %v", got, want)
	}
}

func TestFirstAccountSeedsGraphLines(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "fin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Save(model.NewRecord(model.D(2026, time.November, 8))); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(st, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(a.graph.lines.All()); n != 0 {
		t.Fatalf("an empty record has %d lines", n)
	}
	if !a.edit("add account", func() (*ops.Result, error) {
		return ops.NewAccount(a.st, a.rec, model.Account{Name: "Checking", Type: model.TypeChecking, Tax: model.Taxable}, ops.Options{})
	}) {
		t.Fatal("add failed")
	}
	want := model.DefaultAssetID(a.rec.Accounts[0].ID)
	if lines := a.graph.lines.All(); len(lines) != 1 || lines[0].Asset != want {
		t.Fatalf("lines after the first account: %v, want the %s line", lines, want)
	}
	a.undo()
	if len(a.rec.Accounts) != 0 || len(a.graph.rec.Accounts) != 0 {
		t.Fatal("undo did not return to the empty record")
	}
	a.redo()
	if lines := a.graph.lines.All(); len(a.rec.Accounts) != 1 || len(lines) != 1 || lines[0].Asset != want {
		t.Fatalf("redo: %d accounts, lines %v", len(a.rec.Accounts), lines)
	}
}

func TestFirstAccountSeedsGraphLinesAfterAStaleLine(t *testing.T) {
	for _, remove := range []string{"undo", "delete"} {
		t.Run(remove, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "fin.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { st.Close() })
			if err := st.Save(model.NewRecord(model.D(2026, time.November, 8))); err != nil {
				t.Fatal(err)
			}
			a, err := newApp(st, false)
			if err != nil {
				t.Fatal(err)
			}
			add := func(name string) {
				t.Helper()
				if !a.edit("add account", func() (*ops.Result, error) {
					return ops.NewAccount(a.st, a.rec, model.Account{Name: name, Type: model.TypeChecking, Tax: model.Taxable}, ops.Options{})
				}) {
					t.Fatalf("add %s failed", name)
				}
			}
			add("Chequing")
			if remove == "undo" {
				a.undo()
			} else if !a.edit("delete account", func() (*ops.Result, error) {
				return ops.DeleteAccount(a.st, a.rec, a.rec.Accounts[0].ID, ops.Options{})
			}) {
				t.Fatal("delete failed")
			}
			if len(a.rec.Accounts) != 0 {
				t.Fatal("the account is still there")
			}
			add("Checking")
			want := model.DefaultAssetID(a.rec.Accounts[0].ID)
			if lines := a.graph.lines.All(); len(lines) != 1 || lines[0].Asset != want {
				t.Fatalf("lines: %v, want only the %s line", lines, want)
			}
		})
	}
}
