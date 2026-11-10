// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// balanceOn is the balance the Graph draws for the account under the projection on the date.
func balanceOn(t *testing.T, a *app, acct model.AccountID, proj string, d model.Date) float64 {
	t.Helper()
	s := a.graph.cache.samples(model.DefaultAssetID(acct), proj, graph.Grid{Step: 1, First: d, Last: d})
	if len(s) != 1 {
		t.Fatalf("no balance of %s under %q", acct, proj)
	}
	return s[0].Amount
}

// newChild creates a projection under parent through the tab and selects it.
func newChild(t *testing.T, a *app, parent string) string {
	t.Helper()
	a.proj.selectProjection(parent)
	a.proj.newProjection()
	if a.proj.sel == parent || a.proj.sel == "" {
		t.Fatalf("no projection created: %v", a.notices)
	}
	return a.proj.sel
}

// fillItem sets the fields given as pairs of field and text, in order.
func fillItem(f *itemForm, pairs ...string) {
	for i := 0; i+1 < len(pairs); i += 2 {
		f.set(pairs[i], pairs[i+1])
	}
}

var endOfYear = model.D(2027, 12, 31)

// Scenario 1: a one-time transfer from the 401k to checking, added in a new projection under Base, lowers the 401k on the Graph.
func TestScenarioEarlyWithdrawalInTheGUI(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	name := newChild(t, a, demodata.ProjBase)
	base := balanceOn(t, a, demodata.K401, demodata.ProjBase, endOfYear)

	pt.addNew = true
	pt.addItem = newItemForm()
	fillItem(&pt.addItem, "kind", "transfer", "name", "Early draw", "amount", "20000", "every", "one-time", "date", "2027-06-01", "from", "401k")
	if got := itemProblem(a.rec, pt.addItem); !strings.Contains(got, "accounts") {
		t.Errorf("a transfer with one account: %q", got)
	}
	if pt.addNewItem(name) {
		t.Fatal("an incomplete transfer was added")
	}
	pt.addItem.set("to", "checking")
	if got := itemProblem(a.rec, pt.addItem); got != "" {
		t.Fatalf("complete transfer: %q", got)
	}
	if !pt.addNewItem(name) {
		t.Fatalf("not added: %v", a.notices)
	}

	mods := a.rec.Projection(name).Mods
	it := mods[len(mods)-1].Item
	if it == nil || it.Kind != model.ItemTransfer || it.Amount != 20000 || it.From != demodata.K401 || it.To != demodata.Checking || it.Recurrence.Kind != model.OneTime || it.Recurrence.Anchor != model.D(2027, 6, 1) {
		t.Fatalf("added %+v", it)
	}
	if got := balanceOn(t, a, demodata.K401, name, endOfYear); got >= base-19000 {
		t.Errorf("401k at the end of 2027: %v with the withdrawal, %v without", got, base)
	}
	if pt.addItem.name != "" || pt.addItem.amount != "" {
		t.Error("the form kept the added item")
	}
}

// Scenario 2: end the salary from a date, then add the new salary.
func TestScenarioNewJobInTheGUI(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	name := newChild(t, a, demodata.ProjBase)
	base := balanceOn(t, a, demodata.Checking, demodata.ProjBase, endOfYear)

	pt.addKind, pt.addObj, pt.addProp, pt.addStart = model.KindBudgetItem, "salary-a", propEndsOn, ""
	if got := modProblem(a.rec, name, pt.addKind, pt.addObj, pt.addProp, pt.addMode, pt.addValue, pt.addStart); !strings.Contains(got, "date") {
		t.Errorf("no end date: %q", got)
	}
	pt.addStart = "2027-03-01"
	if !pt.addMod(name) {
		t.Fatalf("not added: %v", a.notices)
	}
	m := a.rec.Projection(name).Mods[0]
	if d, ok := ops.EndedOn(m); !ok || d != model.D(2027, 3, 1) || m.Target.ID != "salary-a" {
		t.Errorf("end mod %+v", m)
	}
	ended := balanceOn(t, a, demodata.Checking, name, endOfYear)
	if ended >= base {
		t.Errorf("checking: %v with the job ended, %v without", ended, base)
	}

	pt.addNew = true
	pt.addItem = newItemForm()
	fillItem(&pt.addItem, "kind", "income", "name", "Salary B", "amount", "50000", "date", "2027-03-15", "every", "monthly", "day", "15", "account", "checking")
	if !pt.addNewItem(name) {
		t.Fatalf("not added: %v", a.notices)
	}
	if got := balanceOn(t, a, demodata.Checking, name, endOfYear); got <= base {
		t.Errorf("checking: %v with the new job, %v without", got, base)
	}
	mods := a.rec.Projection(name).Mods
	if it := mods[len(mods)-1].Item; it.Recurrence.Kind != model.Monthly || it.Recurrence.Day != 15 || it.Start == nil || *it.Start != model.D(2027, 3, 15) {
		t.Errorf("salary %+v", it)
	}
}

// Scenario 3: a child of "Withdraw $100k" changes the amount of the withdrawal its parent adds.
func TestScenarioChangeParentsWithdrawalInTheGUI(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	name := newChild(t, a, demodata.ProjWithdraw)
	full := balanceOn(t, a, demodata.K401, name, endOfYear)

	choices := objectsFor(a.rec, model.KindBudgetItem, name, 0)
	want := objectChoice{"early-401k-withdrawal", "Early 401k withdrawal (added by " + demodata.ProjWithdraw + ")"}
	if !slices.Contains(choices, want) {
		t.Fatalf("the parent's item is not offered: %v", choices)
	}
	pt.addKind, pt.addObj, pt.addProp, pt.addValue, pt.addStart = model.KindBudgetItem, want.id, model.PropAmount, "40000", ""
	if !pt.addMod(name) {
		t.Fatalf("not added: %v", a.notices)
	}
	smaller := balanceOn(t, a, demodata.K401, name, endOfYear)
	if smaller <= full+50000 {
		t.Errorf("401k at the end of 2027: %v with 40000 withdrawn, %v with 100000", smaller, full)
	}
	pt.setModValue(name, 0, "30000")
	if got := balanceOn(t, a, demodata.K401, name, endOfYear); got <= smaller+9000 {
		t.Errorf("401k after the amount changed to 30000: %v, was %v", got, smaller)
	}
	// The parent keeps its own withdrawal.
	if it := a.rec.Projection(demodata.ProjWithdraw).Mods[0].Item; it.Amount != 100000 {
		t.Errorf("parent's item %+v", it)
	}
}

// Items added earlier in the chain are offered where they exist: after their addition, not before.
func TestObjectsForItemsAddedInTheChain(t *testing.T) {
	a := demoApp(t)
	name := newChild(t, a, demodata.ProjWithdraw)
	a.proj.addNew, a.proj.addItem = true, newItemForm()
	fillItem(&a.proj.addItem, "name", "Own", "amount", "10", "date", "2027-01-01", "account", "checking")
	if !a.proj.addNewItem(name) {
		t.Fatalf("not added: %v", a.notices)
	}
	ids := func(index int) []string {
		var out []string
		for _, c := range objectsFor(a.rec, model.KindBudgetItem, name, index) {
			if strings.Contains(c.name, "added by") {
				out = append(out, c.name)
			}
		}
		return out
	}
	if got := ids(0); len(got) != 1 || !strings.HasSuffix(got[0], "(added by "+demodata.ProjWithdraw+")") {
		t.Errorf("before the own item: %v", got)
	}
	if got := ids(1); len(got) != 2 || got[1] != "Own (added by "+name+")" {
		t.Errorf("after the own item: %v", got)
	}
}

// Scenario 4: change a modification's target, property, value and start in place, and an added item's name, amount, date and accounts.
func TestScenarioEditInPlaceInTheGUI(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	job := demodata.ProjNewJob // mod 0: amount of salary-a from 2027-03-01
	pt.setModTarget(job, 0, model.KindBudgetItem, "utilities", model.PropAmount)
	pt.setModValue(job, 0, "500")
	pt.setModStart(job, 0, "2028-01-01")
	m := a.rec.Projection(job).Mods[0]
	if v, from, ok := constantOf(m.Values); m.Target.ID != "utilities" || !ok || v != 500 || from == nil || *from != model.D(2028, 1, 1) {
		t.Errorf("amount mod %+v", m)
	}

	pt.setModTarget(job, 0, model.KindAccount, string(demodata.Savings), model.PropGrowth)
	if m := a.rec.Projection(job).Mods[0]; m.Target != (model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Savings)}) || m.Property != model.PropGrowth {
		t.Errorf("account mod %+v", m)
	}

	// A combination that does not fit is refused and changes nothing.
	before := a.rec
	pt.setModTarget(job, 0, model.KindAccount, string(demodata.Savings), model.PropAmount)
	if a.rec != before || len(a.notices) == 0 {
		t.Error("an account with an amount was accepted")
	}

	pt.setModTarget(job, 0, model.KindBudgetItem, "salary-a", propEndsOn)
	m = a.rec.Projection(job).Mods[0]
	if d, ok := ops.EndedOn(m); !ok || d != model.D(2028, 1, 1) || rowProperty(m) != propEndsOn {
		t.Errorf("ends mod %+v", m)
	}
	pt.setModStart(job, 0, "")
	if _, ok := ops.EndedOn(a.rec.Projection(job).Mods[0]); !ok || pt.errs["from0"] == "" {
		t.Error("the end date was cleared")
	}
	pt.setModStart(job, 0, "2028-06-01")
	if d, _ := ops.EndedOn(a.rec.Projection(job).Mods[0]); d != model.D(2028, 6, 1) {
		t.Errorf("end date %v", d)
	}
	pt.setModTarget(job, 0, model.KindBudgetItem, "salary-a", model.PropAmount)
	m = a.rec.Projection(job).Mods[0]
	if _, ok := ops.EndedOn(m); ok || m.Property != model.PropAmount || m.Target.ID != "salary-a" {
		t.Errorf("back to amount %+v", m)
	}

	// The item Withdraw $100k adds.
	w := demodata.ProjWithdraw
	item := func() model.BudgetItem { return *a.rec.Projection(w).Mods[0].Item }
	pt.setItemField(w, 0, "name", "Big draw")
	pt.setItemField(w, 0, "amount", "50000")
	pt.setItemField(w, 0, "date", "2027-09-01")
	pt.setItemField(w, 0, "to", string(demodata.Savings))
	pt.setItemField(w, 0, "from", string(demodata.Brokerage))
	it := item()
	if it.ID != "early-401k-withdrawal" || it.Name != "Big draw" || it.Amount != 50000 || it.Recurrence.Anchor != model.D(2027, 9, 1) || it.To != demodata.Savings || it.From != demodata.Brokerage {
		t.Errorf("edited item %+v", it)
	}
	pt.setItemField(w, 0, "every", "monthly")
	if it := item(); it.Recurrence.Kind != model.Monthly || it.Recurrence.Day != 1 {
		t.Errorf("monthly item %+v", it.Recurrence)
	}
	pt.setItemField(w, 0, "kind", "income")
	if it := item(); it.Kind != model.ItemIncome || it.Account != demodata.Savings {
		t.Errorf("income item keeps the account it moved to: %+v", it)
	}
	pt.setItemField(w, 0, "account", "checking")
	if it := item(); it.Kind != model.ItemIncome || it.Account != demodata.Checking || it.From != "" || it.To != "" {
		t.Errorf("income item %+v", it)
	}
	before = a.rec
	pt.setItemField(w, 0, "amount", "lots")
	pt.setItemField(w, 0, "day", "40")
	if a.rec != before || pt.errs[itemFieldID(0, "amount")] == "" {
		t.Error("bad input changed the item")
	}
}

// An added biweekly item that starts later than its anchor keeps that start when another field is edited.
func TestEditKeepsBiweeklyStart(t *testing.T) {
	a := demoApp(t)
	job := newChild(t, a, demodata.ProjWithdraw)
	start := model.D(2027, 1, 1)
	it := model.BudgetItem{ID: "side-pay", Name: "Side pay", Kind: model.ItemIncome, Amount: 1000, Account: demodata.Checking, Recurrence: model.Recurrence{Kind: model.Biweekly, Anchor: model.D(2025, 1, 3)}, Start: &start}
	if !a.edit("add budget item", func() (*ops.Result, error) {
		return ops.AddItem(a.st, a.rec, job, it, ops.Options{})
	}) {
		t.Fatalf("not added: %v", a.notices)
	}
	i := len(a.rec.Projection(job).Mods) - 1
	a.proj.setItemField(job, i, "name", "Renamed")
	got := *a.rec.Projection(job).Mods[i].Item
	if got.Name != "Renamed" || got.Start == nil || *got.Start != start || got.Recurrence.Anchor != model.D(2025, 1, 3) {
		t.Errorf("edited item %+v start %v", got, got.Start)
	}
}

func TestEditDateMovesBiweeklyStart(t *testing.T) {
	a := demoApp(t)
	job := newChild(t, a, demodata.ProjWithdraw)
	start := model.D(2027, 1, 1)
	it := model.BudgetItem{ID: "side-pay", Name: "Side pay", Kind: model.ItemIncome, Amount: 1000, Account: demodata.Checking, Recurrence: model.Recurrence{Kind: model.Biweekly, Anchor: start}, Start: &start}
	if !a.edit("add budget item", func() (*ops.Result, error) {
		return ops.AddItem(a.st, a.rec, job, it, ops.Options{})
	}) {
		t.Fatalf("not added: %v", a.notices)
	}
	i := len(a.rec.Projection(job).Mods) - 1
	a.proj.setItemField(job, i, "date", "2026-06-05")
	got := *a.rec.Projection(job).Mods[i].Item
	if got.Recurrence.Anchor != model.D(2026, 6, 5) || got.Start != nil {
		t.Errorf("edited item %+v start %v", got, got.Start)
	}
}

// Scenario 5: what cannot be added says why, with the reason ops gives.
func TestScenarioIncompleteModification(t *testing.T) {
	a := demoApp(t)
	rec := a.rec
	job := demodata.ProjNewJob
	for _, c := range []struct {
		what                          string
		kind                          model.ObjectKind
		obj, prop, value, start, want string
	}{
		{"no object", model.KindAccount, "", model.PropGrowth, "4", "", "pick an object"},
		{"no property", model.KindAccount, "savings", "", "4", "", "pick a property"},
		{"no value", model.KindAccount, "savings", model.PropGrowth, "", "", "enter a value"},
		{"bad value", model.KindAccount, "savings", model.PropGrowth, "lots", "", "not a number"},
		{"bad date", model.KindAccount, "savings", model.PropGrowth, "4", "soon", "not a date"},
		{"no end date", model.KindBudgetItem, "salary-a", propEndsOn, "", "", "date"},
		{"huge value", model.KindAccount, "savings", model.PropGrowth, "1e30", "", "not a number"},
		{"item of a sibling", model.KindBudgetItem, "early-401k-withdrawal", model.PropAmount, "5", "", "no budget item"},
		{"property of another kind", model.KindAccount, "savings", model.PropAmount, "5", "", "growth"},
		{"fine", model.KindAccount, "savings", model.PropGrowth, "4", "", ""},
	} {
		got := modProblem(rec, job, c.kind, c.obj, c.prop, "", c.value, c.start)
		if c.want == "" && got != "" || !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", c.what, got, c.want)
		}
	}

	if got := modProblem(rec, job, model.KindBudgetItem, "salary-a", model.PropGrowth, model.ModeScale, "-5", ""); got != "" {
		t.Errorf("a scale of a budget item's growth: %q", got)
	}
	if got := modProblem(rec, job, model.KindBudgetItem, "salary-a", model.PropAmount, model.ModeScale, "-150", ""); !strings.Contains(got, "negative") {
		t.Errorf("a scale below zero: %q", got)
	}

	pt := a.proj
	pt.addKind, pt.addObj, pt.addProp, pt.addValue = model.KindAccount, "", model.PropGrowth, "4"
	if pt.addMod(job) || a.rec != rec {
		t.Error("an incomplete modification was added")
	}

	f := newItemForm()
	if got := itemProblem(rec, f); !strings.Contains(got, "amount") {
		t.Errorf("empty form: %q", got)
	}
	f.set("amount", "10")
	f.set("date", "2027-01-01")
	if got := itemProblem(rec, f); got != "pick an account" {
		t.Errorf("no account: %q", got)
	}
	f.set("account", "checking")
	if got := itemProblem(rec, f); !strings.Contains(got, "name") {
		t.Errorf("no name: %q", got)
	}
	f.set("name", "x")
	f.set("end", "2026-01-01")
	if got := itemProblem(rec, f); !strings.Contains(got, "before") {
		t.Errorf("end before start: %q", got)
	}
	f.set("end", "")
	f.set("every", "annual")
	if f.month != "1" || f.day != "1" {
		t.Errorf("seeded month %q day %q", f.month, f.day)
	}
	f.set("month", "13")
	if got := itemProblem(rec, f); got == "" {
		t.Error("month 13")
	}
	f.set("month", "6")
	if got := itemProblem(rec, f); got != "" {
		t.Errorf("complete item: %q", got)
	}
}

func TestReasonOf(t *testing.T) {
	if reasonOf(nil) != "" {
		t.Error("no error has a reason")
	}
	if got := reasonOf(ops.ErrNoObject); got != "pick an object" {
		t.Errorf("no object: %q", got)
	}
}

// Scenario 6: cut dining by 30% as a scale, then flip the mode, and give an item raises, in the GUI.
func TestScenarioScaleAndRaisesInTheGUI(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	name := newChild(t, a, demodata.ProjBase)
	base := balanceOn(t, a, demodata.Card, demodata.ProjBase, endOfYear)

	pt.addKind, pt.addObj, pt.addProp, pt.addMode, pt.addValue, pt.addStart = model.KindBudgetItem, "dining", model.PropAmount, model.ModeScale, "-30", "2026-01-01"
	if !pt.addMod(name) {
		t.Fatalf("the scale was not added: %v", a.notices)
	}
	m := a.rec.Projection(name).Mods[0]
	if v, _, ok := constantOf(m.Values); !ok || v != 0.7 || m.Mode != model.ModeScale {
		t.Fatalf("scale mod %+v", m)
	}
	if got := balanceOn(t, a, demodata.Card, name, endOfYear); !(got > base) {
		t.Errorf("the card is %v with less dining, %v without", got, base)
	}
	if got := formatValue(m.Property, m.Mode, 0.7); got != "-30%" {
		t.Errorf("the value shows as %q", got)
	}

	pt.setModValue(name, 0, "-50")
	if v, _, _ := constantOf(a.rec.Projection(name).Mods[0].Values); v != 0.5 {
		t.Errorf("a scale of -50 is %v", v)
	}
	pt.setModValue(name, 0, "-150")
	if v, _, _ := constantOf(a.rec.Projection(name).Mods[0].Values); v != 0.5 || len(a.notices) == 0 {
		t.Errorf("a scale below zero was accepted: %v", v)
	}
	pt.setModMode(name, 0, model.ModeSet)
	if m := a.rec.Projection(name).Mods[0]; m.IsScale() {
		t.Errorf("mode %q", m.Mode)
	}

	// An item the projection adds, with raises.
	pt.addNew = true
	pt.addItem = newItemForm()
	fillItem(&pt.addItem, "kind", "income", "name", "Salary B", "amount", "5000", "every", "monthly", "date", "2027-03-01", "day", "1", "account", "checking", "growth", "3")
	if !pt.addNewItem(name) {
		t.Fatalf("the item was not added: %v", a.notices)
	}
	mods := a.rec.Projection(name).Mods
	item := *mods[len(mods)-1].Item
	if item.Growth != 0.03 {
		t.Errorf("growth %v", item.Growth)
	}
	if f := formOf(item); f.growth != "3%" {
		t.Errorf("the form shows %q", f.growth)
	}
	pt.setItemField(name, len(mods)-1, "growth", "4")
	if got := a.rec.Projection(name).Mods[len(mods)-1].Item.Growth; got != 0.04 {
		t.Errorf("edited growth %v", got)
	}
	pt.setItemField(name, len(mods)-1, "growth", "")
	if got := a.rec.Projection(name).Mods[len(mods)-1].Item.Growth; got != 0 {
		t.Errorf("cleared growth %v", got)
	}
	f := newItemForm()
	fillItem(&f, "growth", "lots")
	if _, err := f.item(); err == nil {
		t.Error("a growth that is not a number was accepted")
	}

	// Raises are a property of a budget item to modify.
	if !slices.Contains(propertiesFor(model.KindBudgetItem), model.PropGrowth) {
		t.Error("no growth among the properties of a budget item")
	}
}
