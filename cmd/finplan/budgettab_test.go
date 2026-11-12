// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/timeline"
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
	if groups[0].Title != "Income" || groups[1].Title != "Expense" || groups[2].Title != "Transfer" {
		t.Errorf("titles %q, %q, %q", groups[0].Title, groups[1].Title, groups[2].Title)
	}
	salary := groups[0].Rows[0]
	want := budgetRow{
		ID: "salary-a", Name: "Salary (Parent A)", Schedule: "twice a month, days 15 and 31", Amount: "$5,000",
		Accounts: accountName(a.rec, demodata.Checking), Taxed: "Wages", Start: "-", End: "-", Indexed: "Yes", Monthly: "$10,000",
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
	if r := byID["vacation"]; r.Schedule != "annually on June 15" || r.Monthly != "$500" {
		t.Errorf("vacation row %+v", r)
	}
	if r := byID["salary-b"]; r.Schedule != "every 2 weeks from 2024-11-08" || r.Monthly != "$8,016.67" {
		t.Errorf("salary-b row %+v", r)
	}
	if r := byID["save-401k"]; r.Accounts != accountName(a.rec, demodata.Checking)+" to "+accountName(a.rec, demodata.K401) || r.Monthly != "$1,000" || r.Growth != "" {
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

// Scenario 1: a quarterly item and a custom every-3-weeks item are added through the Add form, and a semimonthly one with two days.
func TestBudgetTabAddsEveryRecurrence(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	add := func(pairs ...string) model.BudgetItem {
		t.Helper()
		bt.addItem = newItemForm()
		fillItem(&bt.addItem, append([]string{"kind", "expense", "account", string(demodata.Checking)}, pairs...)...)
		if got := itemProblem(a.rec, bt.addItem); got != "" {
			t.Fatalf("%v: %s", pairs, got)
		}
		if !bt.addNewItem() {
			t.Fatalf("%v: not added: %v", pairs, a.notices)
		}
		return a.rec.Budget.Items[len(a.rec.Budget.Items)-1]
	}

	q := add("name", "Insurance", "amount", "400", "every", "quarterly", "date", "2027-02-10")
	if q.Recurrence.Kind != model.Quarterly || q.Start == nil || *q.Start != model.D(2027, 2, 10) {
		t.Errorf("quarterly %+v start %v", q.Recurrence, q.Start)
	}
	c := add("name", "Water filter", "amount", "30", "every", "custom", "count", "3", "unit", "weeks")
	if c.Recurrence.Kind != model.Custom || c.Recurrence.Count != 3 || c.Recurrence.Unit != model.UnitWeeks || c.Start == nil || *c.Start != a.rec.Today {
		t.Errorf("custom %+v start %v", c.Recurrence, c.Start)
	}
	if got := budgetRowOf(a.rec, c).Schedule; got != "every 3 weeks" {
		t.Errorf("custom row %q", got)
	}
	w := add("name", "Lunch", "amount", "20", "every", "weekly", "end", "2030-01-01")
	if w.Recurrence.Kind != model.Weekly || w.End == nil || *w.End != model.D(2030, 1, 1) {
		t.Errorf("weekly %+v end %v", w.Recurrence, w.End)
	}
	// Choosing semimonthly offers the 1st and the 15th, and takes two different days.
	bt.addItem = newItemForm()
	fillItem(&bt.addItem, "kind", "expense", "account", string(demodata.Checking), "name", "Rent", "amount", "900", "every", "semimonthly")
	if bt.addItem.day != "1" || bt.addItem.day2 != "15" {
		t.Errorf("seeded days %q %q", bt.addItem.day, bt.addItem.day2)
	}
	bt.addItem.set("day2", "1")
	if itemProblem(a.rec, bt.addItem) == "" {
		t.Error("two equal days were accepted")
	}
	bt.addItem.set("day2", "20")
	if r := add("name", "Rent", "amount", "900", "every", "semimonthly", "day", "5", "day2", "20").Recurrence; r.Day != 5 || r.Day2 != 20 {
		t.Errorf("semimonthly %+v", r)
	}
	// A custom item needs a count.
	bt.addItem = newItemForm()
	fillItem(&bt.addItem, "kind", "expense", "account", string(demodata.Checking), "name", "X", "amount", "1", "every", "custom", "count", "")
	if got := itemProblem(a.rec, bt.addItem); got != "enter how many as a whole number" {
		t.Errorf("custom without count: %q", got)
	}
}

// Scenario 3: a monthly item becomes biweekly in place, and a semimonthly one weekly; other fields stay.
func TestBudgetTabEditRecurrenceInPlace(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	if !bt.setField("groceries", "every", "biweekly") {
		t.Fatalf("%v", a.notices)
	}
	g := baseItemOf(t, a, "groceries")
	if g.Recurrence.Kind != model.Biweekly || g.Recurrence.Anchor != a.rec.Today {
		t.Errorf("groceries %+v", g.Recurrence)
	}
	if !bt.setField("salary-a", "every", "weekly") {
		t.Fatalf("%v", a.notices)
	}
	s := baseItemOf(t, a, "salary-a")
	if s.Recurrence != (model.Recurrence{Kind: model.Weekly}) || s.Start == nil || *s.Start != a.rec.Today || valueNow(s.Amount, 0) != 5000 {
		t.Errorf("salary-a %+v start %v", s.Recurrence, s.Start)
	}
	if !bt.setField("salary-a", "every", "custom") || !bt.setField("salary-a", "count", "3") || !bt.setField("salary-a", "unit", "months") {
		t.Fatalf("%v", a.notices)
	}
	if r := baseItemOf(t, a, "salary-a").Recurrence; r != (model.Recurrence{Kind: model.Custom, Count: 3, Unit: model.UnitMonths}) {
		t.Errorf("custom %+v", r)
	}
	if !bt.setField("salary-a", "every", "semimonthly") || !bt.setField("salary-a", "day", "10") || !bt.setField("salary-a", "day2", "25") {
		t.Fatalf("%v", a.notices)
	}
	if r := baseItemOf(t, a, "salary-a").Recurrence; r != (model.Recurrence{Kind: model.Semimonthly, Day: 10, Day2: 25}) {
		t.Errorf("semimonthly %+v", r)
	}
}

// Scenario 2: an amount becomes a schedule with a step down in 2029 through the timeline commit path, the form shows it as a schedule, other edits keep it, and undo restores it.
func TestBudgetTabAmountScheduleThroughTimeline(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	before := baseItemOf(t, a, "groceries")
	amount := valueNow(before.Amount, 0)

	bt.openTimeline("groceries", "amount")
	if !bt.tl.open || bt.tl.property != "amount" || bt.tl.target.ID != "groceries" {
		t.Fatalf("timeline state %+v", bt.tl)
	}
	next, err := timeline.Split(before.Amount, 0, model.D(2029, 1, 1))
	if err == nil {
		next, err = timeline.SetValue(next, 1, amount-200)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bt.setSchedule("groceries", "amount", "edit", next) {
		t.Fatalf("%v", a.notices)
	}
	g := baseItemOf(t, a, "groceries")
	if v, _ := g.Amount.At(model.D(2029, 6, 1)); v != amount-200 || valueNow(g.Amount, a.rec.Today) != amount {
		t.Errorf("amount %+v", g.Amount.Segments())
	}
	if f := formOf(g, a.rec.Today); !f.amountSched || f.growthSched || f.amount != trimFloat(amount) {
		t.Errorf("form %+v", f)
	}
	// Another field's edit does not flatten the schedule.
	if !bt.setField("groceries", "name", "Food") || len(baseItemOf(t, a, "groceries").Amount.Segments()) != 2 {
		t.Errorf("the schedule was flattened")
	}
	a.undo()
	a.undo()
	if !reflect.DeepEqual(baseItemOf(t, a, "groceries"), before) {
		t.Error("undo did not restore the item")
	}

	// A growth that is none starts as a schedule of zero that the strip can edit.
	bt.openTimeline("groceries", "growth")
	if g := baseItemOf(t, a, "groceries"); len(g.Growth.Segments()) != 1 || !bt.tl.open {
		t.Errorf("growth %+v", g.Growth.Segments())
	}
	a.undo()
	if len(baseItemOf(t, a, "groceries").Growth.Segments()) != 0 {
		t.Error("undo did not remove the growth schedule")
	}
}

// Scenario 4: amounts have separators and keep cents, and every column has a header.
func TestBudgetRowFormatting(t *testing.T) {
	for _, c := range []struct {
		v    float64
		want string
	}{{5000, "$5,000"}, {1234.5, "$1,234.50"}, {0.05, "$0.05"}, {999, "$999"}, {1234567, "$1,234,567"}, {12.999, "$13"}, {-1234.5, "-$1,234.50"}} {
		if got := formatDollars(c.v); got != c.want {
			t.Errorf("formatDollars(%v) = %q, want %q", c.v, got, c.want)
		}
	}
	a := demoApp(t)
	it := baseItemOf(t, a, "groceries")
	it.Amount = model.Constant(1234.5)
	if r := budgetRowOf(a.rec, it); r.Amount != "$1,234.50" {
		t.Errorf("amount %q", r.Amount)
	}
	if got := strings.Join(budgetHeaders, ","); got != "Name,Repeats,Amount,Accounts,Taxed as,Starts,Ends,Indexed,Growth,Per month" {
		t.Errorf("headers %s", got)
	}
}

var allRecurrences = []model.RecurrenceKind{model.OneTime, model.Weekly, model.Biweekly, model.Semimonthly, model.Monthly, model.Quarterly, model.Annual, model.Custom}

// Every switch of the repeat kind of a base item, from any kind to any other, succeeds through the in-place editor.
func TestBudgetTabEveryKindSwitchSucceeds(t *testing.T) {
	for _, from := range allRecurrences {
		for _, to := range allRecurrences {
			if from == to {
				continue
			}
			a := demoApp(t)
			a.budget.setField("groceries", "every", string(from))
			if got := baseItemOf(t, a, "groceries").Recurrence.Kind; got != from {
				t.Fatalf("to %s: kind %s: %v", from, got, a.notices)
			}
			if !a.budget.setField("groceries", "every", string(to)) {
				t.Errorf("%s to %s: %v %q", from, to, a.notices, a.budget.fields.errs)
				continue
			}
			if got := baseItemOf(t, a, "groceries").Recurrence.Kind; got != to {
				t.Errorf("%s to %s: kind %s", from, to, got)
			}
		}
	}
}

// Deleting an item closes its timeline, so an item added later with the same id does not show a strip unasked.
func TestBudgetTabDeleteClosesTimeline(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	bt.openTimeline("groceries", "amount")
	if !bt.tl.open {
		t.Fatal("timeline did not open")
	}
	if !bt.deleteItem("groceries") {
		t.Fatalf("%v", a.notices)
	}
	if bt.tl.open {
		t.Errorf("timeline still open: %+v", bt.tl)
	}
}

func TestBudgetTabAskDeleteListsTheModificationsLeftBehind(t *testing.T) {
	a := demoApp(t)
	bt := a.budget
	bt.askDelete("salary-a")
	want := []string{`Projection "New job": its amount change will target nothing`}
	if !bt.del.opening || bt.delID != "salary-a" || !slices.Equal(bt.del.lines, want) || !bt.del.orphans || !strings.Contains(bt.del.what, "Salary (Parent A)") {
		t.Errorf("dialog %+v for %q", bt.del, bt.delID)
	}
	if _, ok := a.rec.Budget.Item("salary-a"); !ok {
		t.Error("asking deleted the item")
	}
	bt.askDelete("groceries")
	if len(bt.del.lines) != 0 || bt.del.orphans {
		t.Errorf("an item without modifications lists %q", bt.del.lines)
	}
}
