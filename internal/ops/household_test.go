// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
)

func TestSetHousehold(t *testing.T) {
	st, rec := seed(t)
	single := tax.Single
	res, err := SetHousehold(st, rec, HouseholdChange{Filing: &single, Checking: model.Ptr(demodata.Checking)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Household; got.Filing != tax.Single || got.Checking != demodata.Checking || got.State != tax.VA || !reflect.DeepEqual(got, res.Record.Household) {
		t.Errorf("household %+v", got)
	}
	if _, err := SetHousehold(st, res.Record, HouseholdChange{Filing: &single}, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("setting the same filing status: %v", err)
	}
	back, err := SetHousehold(st, res.Record, HouseholdChange{Checking: model.Ptr(model.AccountID(""))}, Options{})
	if err != nil || back.Record.Household.Checking != "" {
		t.Errorf("clearing the checking account: %+v, %v", back, err)
	}
	if rec.Household.Filing != tax.MarriedJoint {
		t.Error("the input record was changed")
	}
}

func TestSetHouseholdRefusals(t *testing.T) {
	st, rec := seed(t)
	bad := tax.FilingStatus("widowed")
	for name, c := range map[string]HouseholdChange{
		"filing status":    {Filing: &bad},
		"unknown state":    {State: model.Ptr("ZZ")},
		"unknown account":  {Checking: model.Ptr(model.AccountID("nope"))},
		"not checking":     {Checking: model.Ptr(demodata.Savings)},
		"empty state name": {State: model.Ptr("")},
	} {
		if _, err := SetHousehold(st, rec, c, Options{}); err == nil || errors.Is(err, ErrNoChange) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := reload(t, st).Household; !reflect.DeepEqual(got, rec.Household) {
		t.Error("a refused edit changed the store")
	}
}

func TestHouseholdMembers(t *testing.T) {
	st, rec := seed(t)
	n := len(rec.Household.Members)
	born := model.D(2020, time.February, 3)
	res, err := AddHouseholdMember(st, rec, model.HouseholdMember{Name: " Baby ", Birth: born, Role: model.RoleDependent}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Household.Members; len(got) != n+1 || got[n] != (model.HouseholdMember{Name: "Baby", Birth: born, Role: model.RoleDependent}) {
		t.Errorf("members %+v", got)
	}
	if _, err := AddHouseholdMember(st, res.Record, model.HouseholdMember{Name: "baby", Birth: born, Role: model.RoleDependent}, Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("a name differing in case: %v", err)
	}
	renamed := "Baby Z"
	later := born.Add(30)
	res, err = EditHouseholdMember(st, res.Record, "Baby", MemberChange{Name: &renamed, Birth: &later}, Options{})
	if err != nil || res.Member.Name != renamed || res.Member.Birth != later {
		t.Fatalf("edit: %+v, %v", res.Member, err)
	}
	if _, err := EditHouseholdMember(st, res.Record, renamed, MemberChange{Name: &renamed}, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("an edit that changes nothing: %v", err)
	}
	res, err = RemoveHouseholdMember(st, res.Record, renamed, Options{})
	if err != nil || res.Removed != renamed || len(reload(t, st).Household.Members) != n {
		t.Errorf("remove: %+v, %v", res, err)
	}
	if _, err := RemoveHouseholdMember(st, res.Record, renamed, Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("removing a member twice: %v", err)
	}
}

func TestHouseholdMemberRefusals(t *testing.T) {
	st, rec := seed(t)
	ok := model.HouseholdMember{Name: "New", Birth: model.D(2000, time.January, 1), Role: model.RoleDependent}
	for name, m := range map[string]model.HouseholdMember{
		"empty name":  {Birth: ok.Birth, Role: ok.Role, Name: "  "},
		"duplicate":   {Name: "Parent A", Birth: ok.Birth, Role: ok.Role},
		"no birth":    {Name: "New", Role: ok.Role},
		"future":      {Name: "New", Birth: rec.Today.Add(1), Role: ok.Role},
		"before 1900": {Name: "New", Birth: model.D(1899, time.December, 31), Role: ok.Role},
		"no role":     {Name: "New", Birth: ok.Birth},
		"odd role":    {Name: "New", Birth: ok.Birth, Role: "pet"},
	} {
		if _, err := AddHouseholdMember(st, rec, m, Options{}); err == nil {
			t.Errorf("%s was added", name)
		}
	}
	if _, err := AddHouseholdMember(st, rec, ok, Options{DryRun: true}); err != nil || len(reload(t, st).Household.Members) != len(rec.Household.Members) {
		t.Errorf("dry run: %v", err)
	}
}

func TestIncomeTypeOfItems(t *testing.T) {
	st, rec := seed(t)
	it := baseItem1()
	it.IncomeType = model.IncomeTaxFree
	res, err := AddBudgetItem(st, rec, it, Options{})
	if err != nil || res.Item.IncomeType != model.IncomeTaxFree {
		t.Fatalf("add: %+v, %v", res, err)
	}
	if got, _ := reload(t, st).Budget.Item("side-gig"); got.IncomeType != model.IncomeTaxFree {
		t.Errorf("stored income type %q", got.IncomeType)
	}
	bad := baseItem1()
	bad.IncomeType = "salary"
	if _, err := AddBudgetItem(st, rec, bad, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("an unknown income type: %v", err)
	}
	expense := baseItem1()
	expense.Kind, expense.IncomeType = model.ItemExpense, model.IncomeWages
	res, err = AddBudgetItem(st, rec, expense, Options{})
	if err != nil || res.Item.IncomeType != "" {
		t.Errorf("an expense keeps income type %q, %v", res.Item.IncomeType, err)
	}
}

func TestReconciliationAccountNeverDangles(t *testing.T) {
	st, rec := seed(t)
	mustOp(t, func() (*Result, error) {
		return NewAccount(st, rec, model.Account{ID: "scratch", Name: "Scratch", Type: model.TypeChecking, Tax: model.Taxable}, Options{})
	})
	mustOp(t, func() (*Result, error) {
		return SetHousehold(st, reload(t, st), HouseholdChange{Checking: model.Ptr(model.AccountID("scratch"))}, Options{})
	})
	rec = reload(t, st)
	typ := model.TypeSavings
	if _, err := EditAccount(st, rec, "scratch", AccountChange{Type: &typ}, Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("changing the type of the reconciliation account: %v", err)
	}
	if got := reload(t, st); got.Household.Checking != "scratch" || got.Account("scratch").Type != model.TypeChecking {
		t.Error("a refused edit changed the store")
	}
	plan, err := AccountDeletePlan(st, rec, "scratch")
	if err != nil {
		t.Fatal(err)
	}
	if got := affectedKinds(plan); plan.Blocked != "" || got[AffectedHousehold] != 1 {
		t.Errorf("plan %+v", plan)
	}
	mustOp(t, func() (*Result, error) { return DeleteAccount(st, rec, "scratch", Options{}) })
	if got := reload(t, st).Household.Checking; got != "" {
		t.Errorf("household checking after the delete: %q", got)
	}
}
