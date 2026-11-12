// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/tax"
)

func householdApp(t *testing.T) *app {
	t.Helper()
	a := demoApp(t)
	a.accts.rec = a.rec
	a.accts.reset()
	return a
}

func TestHouseholdFormSaysWhatIsMissing(t *testing.T) {
	a := householdApp(t)
	var f householdForm
	if !f.untouched() {
		t.Error("a blank form is touched")
	}
	steps := []struct{ field, text, problem string }{
		{"", "", "enter a name"},
		{"name", "Baby", "enter a birth date"},
		{"birth", "soon", `"soon" is not a date; use YYYY-MM-DD`},
		{"birth", "2026-03-01", "pick a role"},
		{"role", "dependent", ""},
		{"name", "parent a", "there is already a household member \"Parent A\""},
	}
	for _, s := range steps {
		f.set(s.field, s.text)
		got := householdProblem(a.st, a.rec, f)
		if s.problem == "" && got != "" || s.problem != "" && got == "" {
			t.Errorf("after %s=%q the problem is %q, want %q", s.field, s.text, got, s.problem)
		}
	}
	if f.untouched() {
		t.Error("a filled form is untouched")
	}
}

func TestHouseholdEditsAreUndoable(t *testing.T) {
	a := householdApp(t)
	tab := a.accts
	n := len(a.rec.Household.Members)

	f := householdForm{name: " Baby ", birth: "2026-03-01", role: model.RoleDependent}
	m, err := f.member()
	if err != nil || householdProblem(a.st, a.rec, f) != "" {
		t.Fatalf("form refused: %v, %q", err, householdProblem(a.st, a.rec, f))
	}
	if !a.edit("add household member", func() (*ops.Result, error) {
		return ops.AddHouseholdMember(a.st, a.rec, m, ops.Options{})
	}) || len(a.rec.Household.Members) != n+1 || a.rec.Household.Members[n].Name != "Baby" {
		t.Fatalf("member not added: %v", a.notices)
	}

	tab.editMember(n, "birth", "2026-04-02")
	if got := a.rec.Household.Members[n].Birth; got != model.D(2026, 4, 2) {
		t.Errorf("birth %v", got)
	}
	rec := a.rec
	tab.editMember(n, "birth", "later")
	if tab.errs["hhbirth6"] == "" || a.rec != rec {
		t.Errorf("an unreadable date changed the household or showed no error: %q", tab.errs["hhbirth6"])
	}
	tab.editMember(n, "role", "spouse")
	if a.rec.Household.Members[n].Role != model.RoleSpouse {
		t.Errorf("role %q", a.rec.Household.Members[n].Role)
	}

	tab.setHousehold(ops.HouseholdChange{Filing: model.Ptr(tax.Single), Checking: model.Ptr(demodata.Checking)})
	if h := a.rec.Household; h.Filing != tax.Single || h.Checking != demodata.Checking {
		t.Errorf("household %+v", h)
	}
	tab.setHousehold(ops.HouseholdChange{State: model.Ptr("ZZ")})
	if a.rec.Household.State != tax.VA {
		t.Error("an unknown state was set")
	}

	a.undo()
	if h := a.rec.Household; h.Filing != tax.MarriedJoint || h.Checking != "" {
		t.Errorf("undo left %+v", h)
	}
	a.redo()
	if a.rec.Household.Filing != tax.Single {
		t.Error("redo did not restore the filing status")
	}
	for range 4 {
		a.undo()
	}
	if len(a.rec.Household.Members) != n {
		t.Errorf("%d members after undoing everything, want %d", len(a.rec.Household.Members), n)
	}
}

func TestCheckingChoicesAndStateNames(t *testing.T) {
	a := demoApp(t)
	ids, names := checkingChoices(a.rec)
	if len(ids) != 2 || ids[0] != "" || ids[1] != string(demodata.Checking) || names[0] != "first checking account" {
		t.Errorf("choices %v %v", ids, names)
	}
	if got := stateNames(a.rec); len(got) != 1 || got[0] != tax.VA {
		t.Errorf("states %v", got)
	}
}

func TestIncomeTypeOfBaseItemsIsEditable(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	if r := budgetRowOf(a.rec, baseItemOf(t, a, "salary-a")); r.Taxed != "Wages" {
		t.Errorf("salary taxed as %q", r.Taxed)
	}
	if r := budgetRowOf(a.rec, baseItemOf(t, a, "groceries")); r.Taxed != "" {
		t.Errorf("groceries taxed as %q", r.Taxed)
	}
	if !bt.setField("salary-a", "income", string(model.IncomeOrdinary)) {
		t.Fatalf("not changed: %v", a.notices)
	}
	it := baseItemOf(t, a, "salary-a")
	if it.IncomeType != model.IncomeOrdinary || budgetRowOf(a.rec, it).Taxed != "Other income" {
		t.Errorf("income type %q", it.IncomeType)
	}
	a.undo()
	if baseItemOf(t, a, "salary-a").IncomeType != model.IncomeWages {
		t.Error("undo did not restore the income type")
	}

	fillItem(&bt.addItem, "kind", "income", "name", "Interest", "amount", "50", "every", "monthly", "day", "3", "account", string(demodata.Checking), "income", string(model.IncomeTaxFree))
	if got := itemProblem(a.rec, bt.addItem); got != "" {
		t.Fatalf("form: %q", got)
	}
	if !bt.addNewItem() || baseItemOf(t, a, "interest").IncomeType != model.IncomeTaxFree {
		t.Errorf("added income type: %v", a.notices)
	}
}

func TestFERSFormSaysWhatIsMissingAndAddsAsOneChange(t *testing.T) {
	a := householdApp(t)
	const name = "Parent A"
	var f fersForm
	if !f.untouched() {
		t.Error("a blank form is touched")
	}
	steps := []struct {
		field, text string
		ok          bool
	}{
		{"high3", "90000", false},
		{"years", "30", false},
		{"sep", "soon", false},
		{"sep", "57", false},
		{"start", "50", false},
		{"start", "57", true},
	}
	for _, s := range steps {
		f.set(s.field, s.text)
		if got := fersProblem(a.st, a.rec, name, f); (got == "") != s.ok {
			t.Errorf("after %s=%q the problem is %q", s.field, s.text, got)
		}
	}
	if f.untouched() {
		t.Error("a filled form is untouched")
	}
	f.set("survivor", "full")
	ch, err := f.change()
	if err != nil || !a.edit("add FERS", func() (*ops.Result, error) {
		return ops.EditHouseholdMember(a.st, a.rec, name, ch, ops.Options{})
	}) {
		t.Fatalf("form refused: %v %v", err, a.notices)
	}
	m := a.rec.Household.Members[0]
	if m.FERSHigh3 != 90000 || m.FERSYears != 30 || m.FERSSeparationAge != 57 || m.FERSStartAge != 57 || m.FERSSurvivor != model.SurvivorFull {
		t.Fatalf("member %+v", m)
	}

	if !a.accts.changeMember(name, "remove FERS", removeFERSChange()) {
		t.Fatalf("remove refused: %v", a.notices)
	}
	m = a.rec.Household.Members[0]
	if m.HasFERS() || m.FERSYears != 0 || m.FERSStartAge != 0 || m.FERSSeparationAge != 0 || m.FERSSurvivor != model.SurvivorNone {
		t.Errorf("FERS left on the member: %+v", m)
	}
}
