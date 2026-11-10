// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func baseItemOf(t *testing.T, a *app, id string) model.BudgetItem {
	t.Helper()
	it, ok := a.rec.Budget.Item(id)
	if !ok {
		t.Fatalf("no base item %q", id)
	}
	return it
}

// Scenario 1: the items are grouped by kind with every field, and the monthly totals are shown.
func TestBudgetTabGroupsRowsAndTotals(t *testing.T) {
	a := demoApp(t)
	groups := budgetGroups(a.rec)
	if len(groups) != 3 || groups[0].Kind != model.ItemIncome || groups[1].Kind != model.ItemExpense || groups[2].Kind != model.ItemTransfer {
		t.Fatalf("groups %+v", groups)
	}
	if len(groups[0].Rows) != 2 || len(groups[1].Rows) != 7 || len(groups[2].Rows) != 10 {
		t.Errorf("rows %d, %d, %d", len(groups[0].Rows), len(groups[1].Rows), len(groups[2].Rows))
	}
	salary := groups[0].Rows[0]
	want := budgetRow{
		ID: "salary-a", Name: "Salary (Parent A)", Schedule: "twice a month, days 15 and 31", Amount: "$5000",
		Accounts: accountName(a.rec, demodata.Checking), Start: "-", End: "-", Indexed: "indexed", Monthly: "$10,000 a month",
	}
	if salary != want {
		t.Errorf("salary row %+v, want %+v", salary, want)
	}
	byID := map[string]budgetRow{}
	for _, g := range groups {
		for _, r := range g.Rows {
			byID[r.ID] = r
		}
	}
	if r := byID["vacation"]; r.Schedule != "annually on June 15" || r.Monthly != "$500 a month" {
		t.Errorf("vacation row %+v", r)
	}
	if r := byID["salary-b"]; r.Schedule != "every 2 weeks from 2024-11-08" || r.Monthly != "$8,017 a month" {
		t.Errorf("salary-b row %+v", r)
	}
	if r := byID["save-401k"]; r.Accounts != accountName(a.rec, demodata.Checking)+" to "+accountName(a.rec, demodata.K401) || r.Monthly != "$1,000 a month" || r.Growth != "" {
		t.Errorf("transfer row %+v", r)
	}

	it := baseItemOf(t, a, "groceries")
	it.Growth, it.Start, it.End = model.Constant(0.03), model.Ptr(model.D(2026, 1, 5)), model.Ptr(model.D(2030, 12, 31))
	if r := budgetRowOf(a.rec, it); r.Growth != "3% raise" || r.Start != "2026-01-05" || r.End != "2030-12-31" {
		t.Errorf("groceries row %+v", r)
	}

	income, expenses, net := budgetTotalsText(a.rec)
	if income != "$18,017" || expenses != "$6,150" || net != "$11,867" {
		t.Errorf("totals %s, %s, %s", income, expenses, net)
	}
}

// Scenario 2: the Add form says why it cannot add yet, then adds through ops, and undo and redo restore the base budget.
func TestBudgetTabAddWithUndoAndRedo(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	before := a.rec
	count := len(before.Budget.Items)

	if got := itemProblem(a.rec, bt.addItem); got != "enter an amount" {
		t.Errorf("empty form: %q", got)
	}
	fillItem(&bt.addItem, "kind", "expense", "name", "Gym", "amount", "60", "every", "monthly", "day", "10")
	if got := itemProblem(a.rec, bt.addItem); got != "pick an account" {
		t.Errorf("no account: %q", got)
	}
	if bt.addNewItem() || len(a.rec.Budget.Items) != count {
		t.Fatal("an incomplete item was added")
	}
	fillItem(&bt.addItem, "account", string(demodata.Checking), "indexed", "true")
	if got := itemProblem(a.rec, bt.addItem); got != "" {
		t.Fatalf("complete form: %q", got)
	}
	if !bt.addNewItem() {
		t.Fatalf("not added: %v", a.notices)
	}
	gym := a.rec.Budget.Items[count]
	if gym.ID != "gym" || valueNow(gym.Amount, 0) != 60 || gym.Kind != model.ItemExpense || !gym.Indexed || gym.Recurrence.Day != 10 || gym.Account != demodata.Checking {
		t.Errorf("added %+v", gym)
	}
	if bt.addItem.name != "" || bt.addItem.kind != model.ItemExpense {
		t.Errorf("form after add %+v", bt.addItem)
	}
	if _, expenses, _ := budgetTotalsText(a.rec); expenses != "$6,210" {
		t.Errorf("expenses %s", expenses)
	}

	a.undo()
	if !reflect.DeepEqual(a.rec.Budget, before.Budget) {
		t.Error("undo did not remove the item")
	}
	a.redo()
	if _, ok := a.rec.Budget.Item("gym"); !ok {
		t.Error("redo did not add the item")
	}
	if got, _ := a.st.Load(); got == nil || !reflect.DeepEqual(got.Budget, a.rec.Budget) {
		t.Error("the store does not hold the redone budget")
	}
}

// Scenario 2: every field of an item is edited in place through ops, and undo and redo restore the base budget.
func TestBudgetTabEditInPlace(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	before := a.rec

	for _, c := range [][2]string{{"name", "Food"}, {"amount", "1900"}, {"growth", "3"}, {"indexed", "false"}, {"day", "9"}, {"account", string(demodata.Checking)}, {"end", "2030-12-31"}} {
		if !bt.setField("groceries", c[0], c[1]) {
			t.Fatalf("%s: %v", c[0], a.notices)
		}
	}
	g := baseItemOf(t, a, "groceries")
	if g.Name != "Food" || valueNow(g.Amount, 0) != 1900 || valueNow(g.Growth, 0) != 0.03 || g.Indexed || g.Recurrence.Day != 9 || g.Account != demodata.Checking || g.End == nil || *g.End != model.D(2030, 12, 31) {
		t.Errorf("groceries %+v", g)
	}
	if !bt.setField("groceries", "indexed", "true") || !baseItemOf(t, a, "groceries").Indexed {
		t.Error("indexed was not set")
	}

	// A semimonthly item keeps its days when another field changes.
	if !bt.setField("salary-a", "amount", "5100") {
		t.Fatalf("salary: %v", a.notices)
	}
	if s := baseItemOf(t, a, "salary-a"); valueNow(s.Amount, 0) != 5100 || s.Recurrence != (model.Recurrence{Kind: model.Semimonthly, Day: 15, Day2: 31}) {
		t.Errorf("salary-a %+v", s)
	}
	// A biweekly item keeps its anchor.
	if !bt.setField("salary-b", "amount", "3800") {
		t.Fatalf("salary-b: %v", a.notices)
	}
	if s := baseItemOf(t, a, "salary-b"); valueNow(s.Amount, 0) != 3800 || s.Recurrence.Anchor != model.D(2024, 11, 8) {
		t.Errorf("salary-b %+v", s)
	}

	// A refusal changes nothing and is shown beside the field.
	cur := a.rec
	if bt.setField("groceries", "amount", "lots") || a.rec != cur || bt.fields.errs[itemPrefix("groceries")+"amount"] == "" {
		t.Error("an amount that is not a number was accepted")
	}
	if bt.setField("groceries", "amount", "0") || a.rec != cur || len(a.notices) == 0 {
		t.Error("an amount of zero was accepted")
	}

	after := a.rec.Budget
	for range 10 {
		a.undo()
	}
	if !reflect.DeepEqual(a.rec.Budget, before.Budget) {
		t.Error("undo did not restore the base budget")
	}
	for range 10 {
		a.redo()
	}
	if !reflect.DeepEqual(a.rec.Budget, after) {
		t.Error("redo did not restore the edits")
	}
}

// Scenario 2: End sets the last date through ops, and says why it cannot yet.
func TestBudgetTabEnd(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	before := a.rec
	if got := bt.endProblem("groceries", "soon"); !strings.Contains(got, "not a date") {
		t.Errorf("bad date: %q", got)
	}
	if got := bt.endProblem("nope", "2027-03-31"); got == "" {
		t.Error("an unknown item was endable")
	}
	bt.endOn["groceries"] = "2027-03-31"
	if bt.endProblem("groceries", "2027-03-31") != "" || !bt.endItem("groceries", "2027-03-31") {
		t.Fatalf("not ended: %v", a.notices)
	}
	if e := baseItemOf(t, a, "groceries").End; e == nil || *e != model.D(2027, 3, 31) {
		t.Errorf("end %v", e)
	}
	if _, ok := bt.endOn["groceries"]; ok {
		t.Error("the typed date was kept")
	}
	a.undo()
	if !reflect.DeepEqual(a.rec.Budget, before.Budget) {
		t.Error("undo did not restore the item")
	}
	a.redo()
	if e := baseItemOf(t, a, "groceries").End; e == nil {
		t.Error("redo did not end the item")
	}
}

// Scenario 2: deleting an item a projection modifies leaves that modification missing, and undo and redo restore the budget.
func TestBudgetTabDelete(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	before := a.rec
	job := a.rec.Projection(demodata.ProjNewJob)
	if job.Mods[0].Target.ID != "salary-a" {
		t.Fatalf("the demo's first New job modification targets %q", job.Mods[0].Target.ID)
	}

	if !bt.deleteItem("salary-a") {
		t.Fatalf("not deleted: %v", a.notices)
	}
	if _, ok := a.rec.Budget.Item("salary-a"); ok || len(a.rec.Budget.Items) != len(before.Budget.Items)-1 {
		t.Error("the item is still in the budget")
	}
	if missing, _ := a.rec.MissingTargets(demodata.ProjNewJob); missing[0] != "budget item salary-a" {
		t.Errorf("missing %q", missing)
	}
	if income, _, _ := budgetTotalsText(a.rec); income != "$8,017" {
		t.Errorf("income %s", income)
	}
	if bt.deleteItem("salary-a") {
		t.Error("an item was deleted twice")
	}

	a.undo()
	if !reflect.DeepEqual(a.rec.Budget, before.Budget) {
		t.Error("undo did not restore the item")
	}
	if missing, _ := a.rec.MissingTargets(demodata.ProjNewJob); missing[0] != "" {
		t.Errorf("missing after undo %q", missing)
	}
	a.redo()
	if _, ok := a.rec.Budget.Item("salary-a"); ok {
		t.Error("redo did not delete the item")
	}
}
