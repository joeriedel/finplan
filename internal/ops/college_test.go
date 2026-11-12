// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestEditBeneficiaryAndBasis(t *testing.T) {
	st, rec := seed(t)
	if a := rec.Account(demodata.College4); a.Beneficiary != "Child 4" || a.Basis == nil {
		t.Fatalf("demo 529: %+v", a)
	}
	res, err := EditAccount(st, rec, demodata.College4, AccountChange{Beneficiary: model.Ptr("Child 3"), Basis: model.Ptr(1234.5)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Account(demodata.College4); got.Beneficiary != "Child 3" || got.Basis == nil || *got.Basis != 1234.5 {
		t.Errorf("stored %+v", got)
	}
	res, err = EditAccount(st, res.Record, demodata.College4, AccountChange{Beneficiary: model.Ptr(""), ClearBasis: true}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Account(demodata.College4); got.Beneficiary != "" || got.Basis != nil {
		t.Errorf("cleared %+v", got)
	}

	for _, c := range []struct {
		what string
		id   model.AccountID
		ch   AccountChange
		want error
	}{
		{"unknown member", demodata.College4, AccountChange{Beneficiary: model.Ptr("Nobody")}, ErrUnknownObject},
		{"an earner", demodata.College4, AccountChange{Beneficiary: model.Ptr("Parent A")}, ErrInvalidValue},
		{"negative basis", demodata.College4, AccountChange{Basis: model.Ptr(-1.0)}, ErrInvalidValue},
		{"beneficiary on checking", demodata.Checking, AccountChange{Beneficiary: model.Ptr("Child 1")}, ErrInvalidValue},
		{"basis on checking", demodata.Checking, AccountChange{Basis: model.Ptr(10.0)}, ErrInvalidValue},
	} {
		if _, err := EditAccount(st, res.Record, c.id, c.ch, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.what, err)
		}
	}
	// The type cannot leave 529 while a beneficiary or basis is set.
	typ := model.TypeSavings
	if _, err := EditAccount(st, rec, demodata.College1, AccountChange{Type: &typ}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("type change with a beneficiary: %v", err)
	}
	// A new 529 may name its beneficiary, and a new checking account may not.
	if res, err := NewAccount(st, rec, model.Account{Name: "Fund", Type: model.Type529, Tax: model.Tax529, Beneficiary: "Child 1", Basis: model.Ptr(5.0)}, Options{}); err != nil || res.Account.Beneficiary != "Child 1" {
		t.Errorf("new 529: %v", err)
	}
	if _, err := NewAccount(st, rec, model.Account{Name: "Fund", Type: model.TypeChecking, Tax: model.Taxable, Beneficiary: "Child 1"}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("new checking with a beneficiary: %v", err)
	}
}

func TestMemberRenameRemoveAndRoleFollowBeneficiaries(t *testing.T) {
	st, rec := seed(t)
	earner := model.RoleEarner
	if _, err := EditHouseholdMember(st, rec, "Child 1", MemberChange{Role: &earner}, Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("a beneficiary becoming an earner: %v", err)
	}
	// A dependent who is nobody's beneficiary may change role.
	extra, err := AddHouseholdMember(st, rec, model.HouseholdMember{Name: "Child 5", Birth: model.D(2020, 1, 1), Role: model.RoleDependent}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EditHouseholdMember(st, extra.Record, "Child 5", MemberChange{Role: &earner}, Options{}); err != nil {
		t.Errorf("role change of a non-beneficiary: %v", err)
	}

	res, err := EditHouseholdMember(st, rec, "Child 1", MemberChange{Name: model.Ptr("Alex")}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Account(demodata.College1).Beneficiary; got != "Alex" {
		t.Errorf("beneficiary after the rename: %q", got)
	}

	res, err = RemoveHouseholdMember(st, res.Record, "Alex", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Affected) != 1 || res.Affected[0].Kind != AffectedBeneficiary || res.Affected[0].In != string(demodata.College1) || res.Affected[0].Effect != EffectRemoved {
		t.Errorf("affected %+v", res.Affected)
	}
	if got := reload(t, st).Account(demodata.College1).Beneficiary; got != "" {
		t.Errorf("beneficiary after the removal: %q", got)
	}
}
