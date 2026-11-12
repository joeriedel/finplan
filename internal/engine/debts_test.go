// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

// contribRecord is a checking account, a 401k and a card or mortgage as the test needs, with real data to 2026-01-01 and no budget.
func contribRecord(t *testing.T, extra ...*model.Account) *model.Record {
	t.Helper()
	rec := model.NewRecord(d(2026, time.January, 1))
	chk := &model.Account{ID: "chk", Type: model.TypeChecking, Tax: model.Taxable}
	k := &model.Account{ID: "k", Type: model.Type401k, Tax: model.TaxDeferred}
	for _, a := range append([]*model.Account{chk, k}, extra...) {
		bal := 0.0
		if a.ID == "chk" {
			bal = 1e6
		}
		if a.Type == model.TypeCreditCard || a.Type == model.TypeMortgage {
			bal = -a.OriginalAmount
			if a.Type == model.TypeCreditCard {
				bal = -1000
			}
		}
		a.AddSnapshot(d(2025, time.December, 1), bal)
		a.AddSnapshot(d(2026, time.January, 1), bal)
		must(t, rec.AddAccount(a))
	}
	return rec
}

func semimonthly() model.Recurrence {
	return model.Recurrence{Kind: model.Semimonthly, Day: 15, Day2: 31}
}

func TestContributionAboveTheLimitIsCappedAndReported(t *testing.T) {
	rec := contribRecord(t)
	rec.Budget.Items = []model.BudgetItem{
		{ID: "save", Name: "save", Kind: model.ItemTransfer, Amount: model.Constant(3500), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 16}, From: "chk", To: "k"},
	}
	limit, err := rec.Tax.Limit401k(2027, 0)
	must(t, err)
	if limit != 24500 {
		t.Fatalf("limit %v", limit)
	}
	from, to := d(2026, time.December, 31), d(2027, time.December, 31)
	bd, err := Breakdown(rec, "", model.DefaultAssetID("k"), from, to)
	must(t, err)
	if l := line(bd, CauseContribution); !near(l.Projected, limit) {
		t.Errorf("contributed %v, want the limit %v", l.Projected, limit)
	}
	var found bool
	for _, n := range bd.Notes {
		found = found || strings.Contains(n, "2027 401(k) limit of 24500.00 refuses 17500.00")
	}
	if !found {
		t.Errorf("notes %q", bd.Notes)
	}
	// The part past the limit stays in the paying account, and the tax counts what was contributed.
	chk, err := Breakdown(rec, "", model.DefaultAssetID("chk"), from, to)
	must(t, err)
	if l := line(chk, CauseContribution); !near(l.Projected, -limit) {
		t.Errorf("checking paid %v", l.Projected)
	}
	if ty := taxYear(t, rec, "", 2027); !near(ty.K401, limit) {
		t.Errorf("tax counts %v of contributions", ty.K401)
	}
	// A year that stays under the limit is untouched and has no note.
	rec.Budget.Items[0].Amount = model.Constant(1000)
	bd, err = Breakdown(rec, "", model.DefaultAssetID("k"), from, to)
	must(t, err)
	if l := line(bd, CauseContribution); !near(l.Projected, 12000) {
		t.Errorf("under the limit: %v", l.Projected)
	}
	for _, n := range bd.Notes {
		if strings.Contains(n, "limit") {
			t.Errorf("note under the limit: %q", n)
		}
	}
}

func TestEmployerMatchOnAFourPercentMatch(t *testing.T) {
	rec := contribRecord(t)
	pay := model.BudgetItem{
		ID: "pay", Kind: model.ItemIncome, Amount: model.Constant(150000.0 / 24), Recurrence: semimonthly(), Account: "chk", IncomeType: model.IncomeWages,
		MatchPercent: 100, MatchUpTo: 4, MatchAccount: "k",
	}
	save := model.BudgetItem{ID: "save", Kind: model.ItemTransfer, Amount: model.Constant(300), Recurrence: semimonthly(), From: "chk", To: "k"}
	rec.Budget.Items = []model.BudgetItem{pay, save}
	from, to := d(2026, time.December, 31), d(2027, time.December, 31)
	bd, err := Breakdown(rec, "", model.DefaultAssetID("k"), from, to)
	must(t, err)
	if l := line(bd, CauseMatch); !near(l.Projected, 6000) {
		t.Errorf("match %v, want 6000", l.Projected)
	}
	// The match is not the employee's contribution: it is not taxed wages nor a pre-tax deduction.
	if l := line(bd, CauseContribution); !near(l.Projected, 7200) {
		t.Errorf("contributions %v", l.Projected)
	}
	ty := taxYear(t, rec, "", 2027)
	if !near(ty.Wages, 150000) || !near(ty.K401, 7200) {
		t.Errorf("wages %v, 401k %v", ty.Wages, ty.K401)
	}

	// Contributing less than the match ceiling earns less, and half the match percent halves it.
	rec.Budget.Items[1].Amount = model.Constant(100)
	rec.Budget.Items[0].MatchPercent = 50
	bd, err = Breakdown(rec, "", model.DefaultAssetID("k"), from, to)
	must(t, err)
	if l := line(bd, CauseMatch); !near(l.Projected, 0.5*2400) {
		t.Errorf("match of 2400 contributed %v", l.Projected)
	}
	// Contributions refused by the limit earn no match.
	rec.Budget.Items[1].Amount = model.Constant(2500)
	rec.Budget.Items[0].MatchPercent = 100
	bd, err = Breakdown(rec, "", model.DefaultAssetID("k"), from, to)
	must(t, err)
	if l := line(bd, CauseMatch); !near(l.Projected, 6000) {
		t.Errorf("match above the limit %v", l.Projected)
	}
}

func TestDemoMortgageAmortizesToZeroWithFallingInterest(t *testing.T) {
	rec := demodata.Family()
	m := rec.Account(demodata.Mortgage)
	if !m.Amortized() || m.TermMonths != 360 {
		t.Fatalf("terms %+v", m)
	}
	id := model.DefaultAssetID(demodata.Mortgage)
	// The first payment is on 2021-02-01, so the 360th is on 2051-01-01.
	res, err := Balances(rec, "", id, []model.Date{d(2030, time.January, 2), d(2050, time.December, 31), d(2051, time.January, 2), d(2052, time.January, 1)})
	must(t, err)
	if res.Samples[0].Amount >= -1000 || res.Samples[1].Amount >= -1 {
		t.Errorf("the mortgage is paid off early: %v, %v", res.Samples[0].Amount, res.Samples[1].Amount)
	}
	if !near(res.Samples[2].Amount, 0) || !near(res.Samples[3].Amount, 0) {
		t.Errorf("balance after the term %v and %v", res.Samples[2].Amount, res.Samples[3].Amount)
	}

	// The payer's interest falls every year, and interest plus principal is the level payment.
	prev := math.Inf(1)
	for year := 2027; year <= 2050; year++ {
		bd, err := Breakdown(rec, "", model.DefaultAssetID(demodata.Checking), d(year-1, time.December, 31), d(year, time.December, 31))
		must(t, err)
		interest, principal := -line(bd, CauseInterest).Projected, -line(bd, CausePrincipal).Projected
		if interest <= 0 || interest >= prev {
			t.Fatalf("%d: interest %v after %v", year, interest, prev)
		}
		if !near(interest+principal, 12*m.MonthlyPayment()) {
			t.Fatalf("%d: payments %v, want %v", year, interest+principal, 12*m.MonthlyPayment())
		}
		prev = interest
	}

	// On the mortgage the payments are principal alone.
	bd, err := Breakdown(rec, "", id, d(2026, time.December, 31), d(2027, time.December, 31))
	must(t, err)
	if l := line(bd, CausePrincipal); l.Projected <= 0 {
		t.Errorf("principal %v", l.Projected)
	}
	if l := line(bd, CauseInterest); l != (Line{}) {
		t.Errorf("interest on the mortgage %v", l)
	}
	if l := line(bd, CauseGrowth); l != (Line{}) {
		t.Errorf("growth on the mortgage %v", l)
	}
}

func TestMortgageSchedule(t *testing.T) {
	mort := &model.Account{ID: "mort", Type: model.TypeMortgage, Tax: model.Taxable, StatedRate: 0.06, OriginalAmount: 100000, TermMonths: 12,
		FirstPayment: model.Ptr(d(2026, time.February, 1)), PaidFrom: "chk"}
	rec := contribRecord(t, mort)
	pay := mort.MonthlyPayment()
	if !near(pay, 8606.64) {
		t.Fatalf("payment %v", pay)
	}
	bd, err := Breakdown(rec, "", model.DefaultAssetID("chk"), d(2026, time.January, 31), d(2026, time.February, 1))
	must(t, err)
	if l := line(bd, CauseInterest); !near(l.Projected, -500) {
		t.Errorf("first month's interest %v", l.Projected)
	}
	if l := line(bd, CausePrincipal); !near(l.Projected, -(pay - 500)) {
		t.Errorf("first month's principal %v", l.Projected)
	}
	res, err := Balances(rec, "", model.DefaultAssetID("mort"), []model.Date{d(2026, time.December, 31), d(2027, time.January, 1), d(2027, time.March, 1)})
	must(t, err)
	if res.Samples[0].Amount >= -1 || !near(res.Samples[1].Amount, 0) || !near(res.Samples[2].Amount, 0) {
		t.Errorf("balances %v", res.Samples)
	}

	// An extra principal payment cuts the interest after it, and the payments still clear the loan on the term.
	rec.Budget.Items = []model.BudgetItem{{ID: "extra", Kind: model.ItemTransfer, Amount: model.Constant(20000), Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: d(2026, time.March, 10)}, Start: model.Ptr(d(2026, time.March, 10)), From: "chk", To: "mort"}}
	bd, err = Breakdown(rec, "", model.DefaultAssetID("chk"), d(2026, time.January, 1), d(2027, time.March, 1))
	must(t, err)
	paid := -(line(bd, CausePrincipal).Projected + line(bd, CauseTransfer).Projected)
	if !near(paid, 100000) {
		t.Errorf("principal paid %v", paid)
	}
	plain, err := Breakdown(contribRecord(t, mort), "", model.DefaultAssetID("chk"), d(2026, time.January, 1), d(2027, time.March, 1))
	must(t, err)
	if line(bd, CauseInterest).Projected <= line(plain, CauseInterest).Projected {
		t.Errorf("interest with the extra payment %v, without %v", line(bd, CauseInterest).Projected, line(plain, CauseInterest).Projected)
	}
}

func TestCarriedCardBalanceCompoundsDaily(t *testing.T) {
	card := &model.Account{ID: "card", Type: model.TypeCreditCard, Tax: model.Taxable, StatedRate: 0.2299}
	rec := contribRecord(t, card)
	to := d(2026, time.July, 1)
	res, err := Balances(rec, "", model.DefaultAssetID("card"), []model.Date{d(2026, time.January, 2), to})
	must(t, err)
	if want := -1000 * math.Pow(1+0.2299/365, 181); !near(res.Samples[1].Amount, want) {
		t.Errorf("card owes %v, want %v", res.Samples[1].Amount, want)
	}
	if want := -1000 * (1 + 0.2299/365); !near(res.Samples[0].Amount, want) {
		t.Errorf("after a day %v, want %v", res.Samples[0].Amount, want)
	}
	bd, err := Breakdown(rec, "", model.DefaultAssetID("card"), d(2026, time.January, 1), to)
	must(t, err)
	if l := line(bd, CauseInterest); !near(l.Projected, -1000*(math.Pow(1+0.2299/365, 181)-1)) || line(bd, CauseGrowth) != (Line{}) {
		t.Errorf("breakdown %+v", bd.Lines)
	}

	// A card with money on it earns nothing.
	card.Snapshots = nil
	card.AddSnapshot(d(2025, time.December, 1), 500)
	card.AddSnapshot(d(2026, time.January, 1), 500)
	res, err = Balances(rec, "", model.DefaultAssetID("card"), []model.Date{to})
	must(t, err)
	if !near(res.Samples[0].Amount, 500) {
		t.Errorf("a positive card balance grew to %v", res.Samples[0].Amount)
	}
}

func paidMortgage() *model.Account {
	return &model.Account{ID: "mort", Type: model.TypeMortgage, Tax: model.Taxable, StatedRate: 0.06, OriginalAmount: 100000, TermMonths: 12,
		FirstPayment: model.Ptr(d(2026, time.February, 1)), PaidFrom: "chk"}
}

func TestMortgagePaymentsReachThePayerSettledFirstBeforeTaxDay(t *testing.T) {
	mort := paidMortgage()
	rec := contribRecord(t, mort)
	pay := mort.MonthlyPayment()
	chk := model.DefaultAssetID("chk")
	res, err := Balances(rec, "", chk, []model.Date{d(2026, time.March, 1)})
	must(t, err)
	if got := res.Samples[0].Amount; math.Abs(got-(1e6-2*pay)) > 1 {
		t.Errorf("checking %v, want %v", got, 1e6-2*pay)
	}
	bd, err := Breakdown(rec, "", chk, d(2026, time.January, 1), d(2026, time.March, 1))
	must(t, err)
	if !near(bd.End-bd.Start, bd.Total()) || !near(-(line(bd, CauseInterest).Projected+line(bd, CausePrincipal).Projected), 2*pay) {
		t.Errorf("breakdown %+v", bd)
	}
}

func TestMortgageSchedulesAfterThePayersRecordedPayments(t *testing.T) {
	mort := paidMortgage()
	rec := contribRecord(t, mort)
	pay := mort.MonthlyPayment()
	mort.AddSnapshot(d(2026, time.October, 15), -50000)
	chk := rec.Account("chk")
	must(t, chk.AddTransaction(model.Transaction{Date: d(2026, time.November, 1), Amount: -pay, Kind: model.TxTransfer, Counter: "mort"}))
	chk.AddSnapshot(d(2026, time.November, 8), 1e6)
	bd, err := Breakdown(rec, "", model.DefaultAssetID("mort"), d(2026, time.October, 15), d(2026, time.November, 30))
	must(t, err)
	if l := line(bd, CausePrincipal); l != (Line{}) {
		t.Errorf("a scheduled payment on top of the recorded one: %+v", l)
	}
	if !near(line(bd, CauseTransfer).Actual+line(bd, CauseTransfer).Projected, pay) {
		t.Errorf("transfer %+v", line(bd, CauseTransfer))
	}
}

func TestMortgageStopsWhenThePayerCloses(t *testing.T) {
	mort := paidMortgage()
	rec := contribRecord(t, mort)
	rec.Account("chk").Closed = model.Ptr(d(2026, time.March, 15))
	bd, err := Breakdown(rec, "", model.DefaultAssetID("mort"), d(2026, time.March, 15), d(2026, time.June, 1))
	must(t, err)
	if l := line(bd, CausePrincipal); l != (Line{}) {
		t.Errorf("payments after the payer closed: %+v", l)
	}
}
