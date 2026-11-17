// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// depositRecord is checking known to 16 January with a wages item of 5000 on the 10th and, when contribution is above zero, a payroll deduction of that amount from each paycheck into a 401(k).
func depositRecord(t *testing.T, contribution float64) (*model.Record, model.BudgetItem) {
	t.Helper()
	salary := periodItem("salary", 5000, 10, model.Lump)
	salary.Kind = model.ItemIncome
	items := []model.BudgetItem{salary}
	if contribution > 0 {
		save := periodItem("save", contribution, 10, model.Lump)
		save.Kind, save.Account, save.To, save.Paycheck = model.ItemTransfer, "", "k401", "salary"
		items = append(items, save)
	}
	rec := periodRecord(t, items...)
	k := &model.Account{ID: "k401", Type: model.Type401k, Tax: model.TaxDeferred}
	k.AddSnapshot(d(2026, time.January, 1), 3000)
	k.AddSnapshot(d(2026, time.January, 16), 3000)
	must(t, rec.AddAccount(k))
	return rec, salary
}

func deposit(t *testing.T, rec *model.Record, it model.BudgetItem, date model.Date) float64 {
	t.Helper()
	net, ok := Deposits(rec)(it, date)
	if !ok {
		t.Fatalf("no paycheck of %s on %v", it.ID, date)
	}
	return net
}

func TestWagesPeriodIsJudgedAgainstTheNetDeposit(t *testing.T) {
	rec, salary := depositRecord(t, 0)
	paid := d(2026, time.January, 10)
	net := deposit(t, rec, salary, paid)
	if net <= 0 || net >= 5000 {
		t.Fatalf("the deposit is %v, want less than the gross of 5000", net)
	}
	for _, c := range []struct {
		name     string
		amount   float64
		variance float64
	}{{"on target", net, 0}, {"short", net - 200, -200}} {
		rec, _ := depositRecord(t, 0)
		must(t, rec.Account("chk").AddTransaction(model.Transaction{Date: paid, Amount: c.amount, Kind: model.TxIncome, Memo: "salary"}))
		v := periodOf(t, rec, "salary")
		if !v.Matched || v.Gross != 5000 || !near(v.Budgeted, net) || !near(v.Posted, c.amount) || !near(v.Variance, c.variance) {
			t.Errorf("%s: %+v, want a match against the deposit %v with variance %v", c.name, v, net, c.variance)
		}
		st, err := StatusAsOf(rec, rec.Today)
		must(t, err)
		for _, l := range st.Lines {
			if l.Item.ID == "salary" && (l.State != StateMatched || !near(l.Variance, c.variance)) {
				t.Errorf("%s standing: %s, variance %v, want matched with %v", c.name, l.State, l.Variance, c.variance)
			}
		}
	}
}

func TestRaisingAPayrollDeductionLowersTheDepositAndLeavesCheckingAlone(t *testing.T) {
	paid, next := d(2026, time.February, 10), d(2026, time.February, 11)
	var deposits []float64
	for _, contribution := range []float64{500, 1500} {
		rec, salary := depositRecord(t, contribution)
		dep := deposit(t, rec, salary, paid)
		deposits = append(deposits, dep)
		// The paycheck lands net of the deduction and nothing else leaves checking; the whole deduction reaches the 401(k).
		if got := chkOn(t, rec, next); !near(got, 5000+dep) {
			t.Errorf("deduction %v: checking holds %v on %v, want 5000 plus the deposit %v", contribution, got, next, dep)
		}
		if got := sample(t, rec, "", model.DefaultAssetID("k401"), next).Amount; !near(got, 3000+contribution) {
			t.Errorf("deduction %v: the 401(k) holds %v, want 3000 plus the deduction", contribution, got)
		}
		if v := periodOf(t, rec, "save"); v.Matched || v.Missing || !near(v.Posted, contribution) || v.Remaining != 0 {
			t.Errorf("deduction %v: its period is %+v, want posted at its budgeted amount", contribution, v)
		}
	}
	if deposits[1] >= deposits[0] {
		t.Errorf("deposit %v with 500 deducted and %v with 1500, want less with the larger deduction", deposits[0], deposits[1])
	}
}

func TestWagesOverridesAreDepositsAndNotTaxedAgain(t *testing.T) {
	rec, salary := depositRecord(t, 0)
	paid := d(2026, time.January, 10)
	want := deposit(t, rec, salary, d(2026, time.February, 10))
	expected := 3000.0
	occurrence(rec, "salary", paid, model.StatusPending, nil, &expected)
	v := periodOf(t, rec, "salary")
	if !near(v.Remaining, expected) || !near(*v.Expected, expected) {
		t.Fatalf("the period is %+v, want 3000 still to deposit", v)
	}
	// What is still to come lands as the deposit, with no tax taken from it, and the other paychecks are taxed as before.
	if got := chkOn(t, rec, d(2026, time.January, 17)); !near(got, 5000+expected) {
		t.Errorf("checking holds %v after the deposit, want %v", got, 5000+expected)
	}
	if got := deposit(t, rec, salary, d(2026, time.February, 10)); !near(got, want) {
		t.Errorf("the next deposit is %v with the override, %v without: the tax of a paycheck stays at its gross", got, want)
	}
}

func TestWagesLumpTieIsBrokenByTheNetDeposit(t *testing.T) {
	rec, salary := depositRecord(t, 0)
	net := deposit(t, rec, salary, d(2026, time.January, 10))
	// The gross of 5000 on the 8th and the deposit on the 12th are both 2 days from the due date.
	for _, tx := range []model.Transaction{
		{Date: d(2026, time.January, 8), Amount: 5000, Kind: model.TxIncome, Memo: "salary"},
		{Date: d(2026, time.January, 12), Amount: net, Kind: model.TxIncome, Memo: "salary"},
	} {
		must(t, rec.Account("chk").AddTransaction(tx))
	}
	ms := rec.Matches().Item("salary")
	if len(ms) != 1 || ms[0].Transaction.Date != d(2026, time.January, 12) {
		t.Errorf("matched %+v, want the deposit of the 12th", ms)
	}
}
