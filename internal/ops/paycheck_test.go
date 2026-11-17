// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// sideGig adds a wages item that starts in 2027 and returns it, the paycheck a deduction can be taken from.
func sideGig(t *testing.T, st *store.Store, rec *model.Record) model.BudgetItem {
	t.Helper()
	res, err := AddBudgetItem(st, rec, baseItem1(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return *res.Item
}

// deductionOf is a transfer into the account deducted from the paychecks of the wages item, on its schedule.
func deductionOf(gig model.BudgetItem, to model.AccountID) model.BudgetItem {
	return model.BudgetItem{Name: "Extra", Kind: model.ItemTransfer, Amount: model.Constant(100), Recurrence: gig.Recurrence, Start: gig.Start, To: to, Paycheck: gig.ID}
}

func TestPayrollDeductionRules(t *testing.T) {
	st, rec := seed(t)
	gig := sideGig(t, st, rec)
	rec = reload(t, st)
	good := deductionOf(gig, demodata.K401)
	good.From = demodata.Checking
	res, err := AddBudgetItem(st, rec, good, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Item.From != "" || res.Item.Paycheck != gig.ID {
		t.Errorf("the deduction was stored with from %q and paycheck %q, want no source account and %s", res.Item.From, res.Item.Paycheck, gig.ID)
	}
	later := deductionOf(gig, demodata.TSP)
	later.Start = model.Ptr(model.D(2028, 1, 15))
	mustOp(t, func() (*Result, error) { return AddBudgetItem(st, reload(t, st), later, Options{}) })

	salaryB, _ := rec.Budget.Item("salary-b")
	for name, change := range map[string]func(*model.BudgetItem){
		"into an account that is not a 401k or TSP": func(it *model.BudgetItem) { it.To = demodata.Roth },
		"into a savings account":                    func(it *model.BudgetItem) { it.To = demodata.Savings },
		"from an expense item":                      func(it *model.BudgetItem) { it.Paycheck = "groceries" },
		"from an item that does not exist":          func(it *model.BudgetItem) { it.Paycheck = "nothing" },
		"on another recurrence":                     func(it *model.BudgetItem) { it.Recurrence = salaryB.Recurrence },
		"starting before the paycheck does":         func(it *model.BudgetItem) { it.Start = model.Ptr(model.D(2026, 12, 15)) },
		"as an expense": func(it *model.BudgetItem) {
			it.Kind, it.Account, it.To = model.ItemExpense, demodata.Checking, ""
		},
		"without a destination": func(it *model.BudgetItem) { it.To = "" },
	} {
		it := deductionOf(gig, demodata.K401)
		change(&it)
		if err := CheckItem(rec, it); err == nil {
			t.Errorf("a deduction %s was accepted", name)
		}
	}
}

func TestAWagesItemThatNoLongerFitsReleasesItsDeductions(t *testing.T) {
	st, rec := seed(t)
	salary, _ := rec.Budget.Item("salary-a")
	// A raise changes nothing for the deduction.
	raise := salary
	raise.Growth = model.Constant(0.03)
	res, err := EditBudgetItem(st, rec, "salary-a", raise, Options{})
	if err != nil || len(res.Affected) != 0 {
		t.Fatalf("a raise: %v, affected %v", err, res.Affected)
	}
	if it, _ := reload(t, st).Budget.Item("save-401k"); it.Paycheck != "salary-a" || it.From != "" {
		t.Errorf("save-401k after the raise: %+v", it)
	}

	// Another schedule leaves it no paycheck: it keeps moving the money, out of the account the paychecks went into.
	moved := salary
	moved.Recurrence = model.Recurrence{Kind: model.Monthly, Day: 1}
	res, err = EditBudgetItem(st, reload(t, st), "salary-a", moved, Options{})
	if err != nil || len(res.Affected) != 1 || res.Affected[0].Effect != EffectRemoved {
		t.Fatalf("a new schedule: %v, affected %v", err, res.Affected)
	}
	if it, _ := reload(t, st).Budget.Item("save-401k"); it.Paycheck != "" || it.From != demodata.Checking || it.To != demodata.K401 {
		t.Errorf("save-401k after the schedule changed: %+v, want a transfer from checking", it)
	}

	// The same when the paycheck is deleted.
	res, err = DeleteBudgetItem(st, reload(t, st), "salary-b", Options{})
	if err != nil || len(res.Affected) != 1 {
		t.Fatalf("delete: %v, affected %v", err, res.Affected)
	}
	if it, _ := reload(t, st).Budget.Item("save-tsp"); it.Paycheck != "" || it.From != demodata.Checking {
		t.Errorf("save-tsp after its paycheck was deleted: %+v", it)
	}
}

func TestEndingAWagesItemLeavesItsDeductions(t *testing.T) {
	st, rec := seed(t)
	gig := sideGig(t, st, rec)
	rec = reload(t, st)
	res, err := AddBudgetItem(st, rec, deductionOf(gig, demodata.K401), Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Item.ID
	linked := func(when string) {
		t.Helper()
		got, _ := reload(t, st).Budget.Item(id)
		if got.Paycheck != gig.ID || got.End != nil {
			t.Errorf("%s: the deduction is %+v, want it still taken from %s and open-ended", when, got, gig.ID)
		}
	}
	end := model.D(2027, 6, 30)
	res, err = EndBudgetItem(st, reload(t, st), gig.ID, end, Options{})
	if err != nil || len(res.Affected) != 0 {
		t.Fatalf("end: %v, affected %v", err, res.Affected)
	}
	linked("after EndBudgetItem")

	cur, _ := reload(t, st).Budget.Item(gig.ID)
	cur.End = model.Ptr(model.D(2027, 3, 31))
	res, err = EditBudgetItem(st, reload(t, st), gig.ID, cur, Options{})
	if err != nil || len(res.Affected) != 0 {
		t.Fatalf("edit: %v, affected %v", err, res.Affected)
	}
	linked("after EditBudgetItem")
}
