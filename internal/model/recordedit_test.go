// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"
	"testing"
	"time"
)

func editRecord(t *testing.T) *Record {
	t.Helper()
	a, b := acct("a", TypeChecking, day(time.March, 1), 10), acct("b", TypeSavings, day(time.March, 1), 20)
	a.Default.Intervals = []Interval{{From: MinDate, To: Ptr(day(time.January, 31)), Value: 5}}
	a.AddTransaction(Transaction{Date: day(time.March, 2), Amount: 1, Kind: TxIncome})
	r := newRecord(t, a, b)
	addAsset(t, r, "all", RuleMember(Rule{Types: []AccountType{TypeChecking}}), AccountMember("b"))
	addAsset(t, r, "outer", AssetMember("all"), AccountMember("a"))
	r.Assets[2].Params = map[string]Schedule{PropGrowth: Constant(0.02)}
	r.Budget.Items = []BudgetItem{{ID: "x", End: Ptr(day(time.June, 1))}}
	return r
}

func TestCloneIsDeep(t *testing.T) {
	r := editRecord(t)
	c := r.Clone()
	if !reflect.DeepEqual(r, c) {
		t.Fatal("the clone differs from the original")
	}
	c.Accounts[0].Name = "changed"
	c.Accounts[0].Snapshots[0].Balance = 99
	c.Accounts[0].Transactions[0].Amount = 99
	*c.Accounts[0].Default.Intervals[0].To = day(time.February, 1)
	c.Assets[2].Members[0].Rule.Types[0] = TypeLoan
	c.Assets[2].Params[PropGrowth] = Constant(0.5)
	c.Assets[3].Members[0] = AccountMember("b")
	*c.Budget.Items[0].End = day(time.July, 1)
	c.Projections = append(c.Projections, &Projection{Name: "p"})
	if err := c.RemoveAsset("outer"); err != nil {
		t.Fatal(err)
	}
	if want := editRecord(t); !reflect.DeepEqual(r, want) {
		t.Error("changing the clone changed the original")
	}
	if r.Asset("outer") == nil || c.Asset("outer") != nil {
		t.Error("the clone shares the asset index")
	}
}

func TestRemoveMemberRenumbers(t *testing.T) {
	r := editRecord(t)
	// Seq: a 1, b 2, all (rule 3, b 4), outer (all 5, a 6).
	if err := r.RemoveMember("all", 3); err != nil {
		t.Fatal(err)
	}
	var seqs []int
	for _, a := range r.Assets {
		for _, m := range a.Members {
			seqs = append(seqs, m.Seq)
		}
	}
	if !reflect.DeepEqual(seqs, []int{1, 2, 3, 4, 5}) {
		t.Errorf("seqs after the removal: %v", seqs)
	}
	if err := r.AddMember("all", AccountMember("a")); err != nil {
		t.Fatal(err)
	}
	if got := r.Asset("all").Members[1].Seq; got != 6 {
		t.Errorf("the next member is numbered %d, want 6", got)
	}
	for _, c := range []struct {
		asset AssetID
		seq   int
	}{{"nope", 1}, {"all", 99}, {"acct:a", 1}} {
		if err := r.RemoveMember(c.asset, c.seq); err == nil {
			t.Errorf("RemoveMember(%q, %d) succeeded", c.asset, c.seq)
		}
	}
}

func TestRemoveAsset(t *testing.T) {
	r := editRecord(t)
	if err := r.RemoveAsset("all"); err == nil {
		t.Error("removed an asset another asset has as a member")
	}
	if err := r.RemoveAsset("acct:a"); err == nil {
		t.Error("removed a default asset")
	}
	if err := r.RemoveAsset("outer"); err != nil {
		t.Fatal(err)
	}
	if r.Asset("outer") != nil || r.Asset("all") == nil || r.Asset("acct:b") == nil {
		t.Error("the wrong assets are left")
	}
	if err := r.RemoveAsset("all"); err != nil {
		t.Fatal(err)
	}
	addAsset(t, r, "again", AccountMember("a"))
	if r.Asset("again") == nil || r.Assets[len(r.Assets)-1].ID != "again" {
		t.Error("the asset index is stale after removals")
	}
}
