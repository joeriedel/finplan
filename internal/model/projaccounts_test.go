// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"
	"testing"
)

func projAccountRecord(t *testing.T) *Record {
	t.Helper()
	rec := NewRecord(D(2026, 1, 1))
	for _, err := range []error{
		rec.AddAccount(&Account{ID: "chk", Name: "Checking", Type: TypeChecking, Tax: Taxable}),
		rec.AddAsset(&Asset{ID: "everything", Members: []Member{RuleMember(Rule{})}}),
		rec.AddProjection(&Projection{Name: "a", Accounts: []ProjAccount{{ID: "villa", Name: "Villa", Type: TypeProperty, Tax: Taxable, StatedRate: 0.03, Opening: 500, From: D(2027, 1, 1)}}}),
		rec.AddProjection(&Projection{Name: "b", Parent: "a", Accounts: []ProjAccount{{ID: "loan", Name: "Loan", Type: TypeMortgage, Tax: Taxable, Opening: -300, From: D(2027, 1, 1)}}}),
		rec.AddProjection(&Projection{Name: "c"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return rec
}

func TestForAddsTheChainsAccounts(t *testing.T) {
	rec := projAccountRecord(t)
	before := rec.Clone()
	view, err := rec.For("b")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Accounts) != 3 || view.Accounts[1].ID != "villa" || view.Accounts[2].ID != "loan" {
		t.Fatalf("accounts: %v", view.Accounts)
	}
	for _, id := range []AccountID{"villa", "loan"} {
		if view.Account(id) == nil || view.Asset(DefaultAssetID(id)) == nil {
			t.Errorf("view lacks %s or its default asset", id)
		}
	}
	a := view.Account("villa")
	if a.Opens == nil || *a.Opens != D(2027, 1, 1) || a.Default.At(D(2026, 12, 31)) != 0 || a.Default.At(D(2027, 1, 1)) != 500 {
		t.Errorf("villa as the engine sees it: %+v", a)
	}
	// A rule member picks them up.
	res, err := view.Resolve("everything")
	if err != nil || len(res.Accounts) != 3 {
		t.Errorf("rule asset: %v, %v", res, err)
	}
	if v, _ := rec.For("a"); len(v.Accounts) != 2 || v.Account("loan") != nil {
		t.Error("a parent sees a child's account")
	}
	if v, _ := rec.For("c"); v != rec {
		t.Error("a projection with no accounts should give the record")
	}
	if v, _ := rec.For(""); v != rec {
		t.Error("reality should give the record")
	}

	// The record is untouched.
	if len(rec.Accounts) != 1 || rec.Asset("acct:villa") != nil || rec.Account("villa") != nil {
		t.Error("the record changed")
	}
	if !reflect.DeepEqual(rec.Accounts, before.Accounts) || !reflect.DeepEqual(rec.Assets, before.Assets) {
		t.Error("the record's accounts or assets changed")
	}
	if res, err := rec.Resolve("everything"); err != nil || len(res.Accounts) != 1 {
		t.Errorf("rule asset on the record: %v, %v", res, err)
	}
}

func TestForRefusesCollisions(t *testing.T) {
	rec := projAccountRecord(t)
	rec.Projection("b").Accounts[0].ID = "villa"
	if _, err := rec.For("b"); err == nil {
		t.Error("two accounts of one chain with one id")
	}
	rec.Projection("b").Accounts[0].ID = "chk"
	if _, err := rec.For("b"); err == nil {
		t.Error("an account with a record account's id")
	}
	rec.Projection("b").Accounts[0].ID = ""
	if _, err := rec.For("b"); err == nil {
		t.Error("an account with no id")
	}
	if _, err := rec.For("nonesuch"); err == nil {
		t.Error("unknown projection")
	}
}

func TestCloneCopiesProjectionAccounts(t *testing.T) {
	rec := projAccountRecord(t)
	c := rec.Projection("a").Clone()
	c.Accounts[0].Opening = 1
	if rec.Projection("a").Accounts[0].Opening != 500 {
		t.Error("a clone shares the accounts")
	}
}

func TestDescendants(t *testing.T) {
	rec := projAccountRecord(t)
	got := rec.Descendants("a")
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("descendants of a: %v", got)
	}
	if len(rec.Descendants("b")) != 0 || len(rec.Descendants("c")) != 0 {
		t.Error("b and c have none")
	}
}
