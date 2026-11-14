// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// depositRecord is checking known to 16 January with a wages item of 5000 on the 10th and, when contribution is above zero, a monthly transfer of that amount into a 401(k).
func depositRecord(t *testing.T, contribution float64) (*model.Record, model.BudgetItem) {
	t.Helper()
	salary := periodItem("salary", 5000, 10, model.Lump)
	salary.Kind = model.ItemIncome
	items := []model.BudgetItem{salary}
	if contribution > 0 {
		save := periodItem("save", contribution, 11, model.Lump)
		save.Kind, save.Account, save.From, save.To = model.ItemTransfer, "", "chk", "k401"
		items = append(items, save)
	}
	rec := periodRecord(t, items...)
	k := &model.Account{ID: "k401", Type: model.Type401k, Tax: model.TaxDeferred}
	k.AddSnapshot(d(2026, time.January, 1), 3000)
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

func TestRaisingTheDeferralChangesTheExpectedDeposit(t *testing.T) {
	paid := d(2026, time.January, 10)
	low, salary := depositRecord(t, 500)
	high, _ := depositRecord(t, 1500)
	// A larger pre-tax contribution lowers the income tax the paycheck withholds, so more of it is deposited.
	if lo, hi := deposit(t, low, salary, paid), deposit(t, high, salary, paid); hi <= lo {
		t.Errorf("deposit %v with 500 deferred and %v with 1500, want more with the larger deferral", lo, hi)
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
