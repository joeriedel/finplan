// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// depositRecord returns a record whose checking is known to 16 January, with a wages item of 5000 on the 10th and, when contribution is above zero, a payroll deduction of that amount from each paycheck into a 401(k).
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
	expected := 3000.0
	occurrence(rec, "salary", paid, model.StatusPending, nil, &expected)
	v := periodOf(t, rec, "salary")
	if !near(v.Remaining, expected) || !near(*v.Expected, expected) {
		t.Fatalf("the period is %+v, want 3000 still to deposit", v)
	}
	// What is still to come lands as the deposit, with no tax taken from it, and the projected deposit of the next paycheck is what is booked for it, on the year's withholding as this paycheck leaves it.
	if got := chkOn(t, rec, d(2026, time.January, 17)); !near(got, 5000+expected) {
		t.Errorf("checking holds %v after the deposit, want %v", got, 5000+expected)
	}
	feb := d(2026, time.February, 10)
	if booked := chkOn(t, rec, feb) - chkOn(t, rec, feb.Add(-1)); !near(booked, deposit(t, rec, salary, feb)) {
		t.Errorf("the next paycheck books %v, its deposit is %v", booked, deposit(t, rec, salary, feb))
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

// Matching reaches the deposit hook while holding the match lock, so the hook must not need the matches itself.
func TestMatchingWagesWithDepositsInTheCutoffPeriodDoesNotDeadlock(t *testing.T) {
	rec, salary := depositRecord(t, 500)
	net := deposit(t, rec, salary, d(2026, time.January, 10))
	must(t, rec.Account("chk").AddTransaction(model.Transaction{Date: d(2026, time.January, 12), Amount: net, Kind: model.TxIncome, Memo: "salary"}))
	done := make(chan int)
	go func() { done <- len(rec.Matches().Item("salary")) }()
	select {
	case n := <-done:
		if n != 1 {
			t.Errorf("matched %d deposits, want 1", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("deriving the matches deadlocked")
	}
}

func TestEndingTheSalaryStopsItsDeduction(t *testing.T) {
	rec, _ := depositRecord(t, 500)
	end := d(2026, time.March, 31)
	rec.Budget.Items[0].End = &end
	k401 := func(date model.Date) float64 {
		return sample(t, rec, "", model.DefaultAssetID("k401"), date).Amount
	}
	// The paychecks of February and March are still to come; with the salary over nothing more is taken.
	if got := k401(end); !near(got, 3000+2*500) {
		t.Errorf("the 401(k) holds %v on %v, want 3000 plus the 2 deductions still to come", got, end)
	}
	if got := k401(d(2027, time.June, 30)); !near(got, k401(end)) {
		t.Errorf("the 401(k) holds %v after the salary ended, want %v", got, k401(end))
	}
	if got := taxYear(t, rec, "", 2026).K401; !near(got, 3*500) {
		t.Errorf("deferrals %v, want those of the 3 paychecks of the year", got)
	}
	if got := taxYear(t, rec, "", 2027).K401; got != 0 {
		t.Errorf("deferrals %v in a year with no wages, want none", got)
	}
}

func TestExtendingTheSalaryResumesItsDeduction(t *testing.T) {
	rec, _ := depositRecord(t, 500)
	end := d(2026, time.March, 31)
	rec.Budget.Items[0].End = &end
	k401 := func(date model.Date) float64 {
		return sample(t, rec, "", model.DefaultAssetID("k401"), date).Amount
	}
	stopped := k401(d(2026, time.June, 30))
	later := d(2026, time.May, 31)
	rec.Budget.Items[0].End = &later
	if got := k401(d(2026, time.June, 30)); !near(got, stopped+2*500) {
		t.Errorf("the 401(k) holds %v after the salary was extended by 2 paychecks, want %v", got, stopped+2*500)
	}
	rec.Budget.Items[0].End = nil
	if got := k401(d(2026, time.June, 30)); !near(got, stopped+3*500) {
		t.Errorf("the 401(k) holds %v with the salary open-ended, want %v", got, stopped+3*500)
	}
}

func TestDepositShownIsTheOneBookedAfterAnEarlierPaycheckPaidNothing(t *testing.T) {
	rec, salary := depositRecord(t, 500)
	occurrence(rec, "salary", d(2026, time.January, 10), model.StatusPending, nil, model.Ptr(0.0))
	feb := d(2026, time.February, 10)
	ps, err := Periods(rec, "", feb, 1)
	must(t, err)
	var shown float64
	for _, p := range ps {
		if p.Item.ID == "salary" {
			shown = p.Current().Budgeted
		}
	}
	if got := deposit(t, rec, salary, feb); !near(shown, got) {
		t.Errorf("the period shows %v, the deposit is %v", shown, got)
	}
	if booked := chkOn(t, rec, feb) - chkOn(t, rec, feb.Add(-1)); !near(booked, shown) {
		t.Errorf("February's paycheck books %v, the period shows %v", booked, shown)
	}
}

func TestOverriddenDeductionIsWhatTheDepositAndTheAccountSee(t *testing.T) {
	rec, salary := depositRecord(t, 500)
	paid := d(2026, time.June, 10)
	after := d(2026, time.June, 11)
	// The deposits of the paychecks still to come until the 11th: the deduction also changes the withholding of each a little.
	deposits := func() (sum float64) {
		for month := time.February; month <= time.June; month++ {
			sum += deposit(t, rec, salary, d(2026, month, 10))
		}
		return sum
	}
	base := deposit(t, rec, salary, paid)
	sum0, chk0, k0 := deposits(), chkOn(t, rec, after), sample(t, rec, "", model.DefaultAssetID("k401"), after).Amount
	for _, c := range []struct{ expected, diff float64 }{{0, -500}, {1000, 500}} {
		occurrence(rec, "save", paid, model.StatusAuto, nil, model.Ptr(c.expected))
		dep := deposit(t, rec, salary, paid)
		if c.diff < 0 && dep <= base || c.diff > 0 && dep >= base || math.Abs(dep-base+c.diff) > 250 {
			t.Errorf("deduction expected %v: deposit %v, want about %v", c.expected, dep, base-c.diff)
		}
		if got, want := chkOn(t, rec, after)-chk0, deposits()-sum0; !near(got, want) {
			t.Errorf("deduction expected %v: checking changed by %v, want the deposits' %v", c.expected, got, want)
		}
		if got := sample(t, rec, "", model.DefaultAssetID("k401"), after).Amount - k0; !near(got, c.diff) {
			t.Errorf("deduction expected %v: the 401(k) changed by %v, want %v", c.expected, got, c.diff)
		}
	}
}

func TestDepositShownForTheCurrentPaycheckIsTheOneBooked(t *testing.T) {
	// The gross of the current paycheck is rebuilt from its remainder, which is not bit for bit its budgeted gross for every amount.
	for i := range 24 {
		gross := 3846.15 + 13.37*float64(i)
		rec, _ := depositRecord(t, 0)
		rec.Budget.Items[0].Amount = model.Constant(gross)
		rec.Today = d(2026, time.August, 16)
		rec.Account("chk").AddSnapshot(rec.Today, 5000)
		// An unrelated transaction makes the account tracked, so the Aug 10 paycheck is missing and its remainder is booked after the cutoff.
		must(t, rec.Account("chk").AddTransaction(model.Transaction{Date: d(2026, time.August, 12), Amount: -30, Kind: model.TxExpense, Memo: "groceries"}))
		occurrence(rec, "salary", d(2026, time.January, 10), model.StatusPending, nil, model.Ptr(0.0))
		ps, err := Periods(rec, "", rec.Today, 1)
		must(t, err)
		var cur model.PeriodView
		for _, p := range ps {
			if p.Item.ID == "salary" {
				cur = p.Current()
			}
		}
		if !cur.Missing {
			t.Errorf("gross %v: the current paycheck is not missing", gross)
		}
		booked := chkOn(t, rec, rec.Today.Add(1)) - chkOn(t, rec, rec.Today)
		if !near(cur.Remaining, cur.Budgeted) || !near(booked, cur.Budgeted) {
			t.Errorf("gross %v: the paycheck books %v, the period shows %v budgeted and %v remaining", gross, booked, cur.Budgeted, cur.Remaining)
		}
	}
}

func TestDeductionOfAnEndedPaycheckHasNoPeriod(t *testing.T) {
	rec, _ := depositRecord(t, 500)
	end := d(2026, time.December, 31)
	rec.Budget.Items[0].End = &end
	has := func(date model.Date) (periods, status bool) {
		ps, err := Periods(rec, "", date, 1)
		must(t, err)
		for _, p := range ps {
			periods = periods || p.Item.ID == "save"
		}
		st, err := StatusAsOf(rec, date)
		must(t, err)
		for _, l := range st.Lines {
			status = status || l.Item.ID == "save"
		}
		return
	}
	if p, s := has(d(2026, time.December, 12)); !p || !s {
		t.Errorf("the deduction has periods %v and status %v while its paycheck runs, want both", p, s)
	}
	if p, s := has(d(2027, time.January, 12)); p || s {
		t.Errorf("the deduction has periods %v and status %v after its paycheck ended, want neither", p, s)
	}
}
