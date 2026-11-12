// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
)

func taxYear(t *testing.T, rec *model.Record, proj string, year int) *TaxYear {
	t.Helper()
	ty, err := TaxYearOf(rec, proj, year)
	must(t, err)
	return ty
}

// taxLine is the tax the asset's breakdown books in the calendar year.
func taxLine(t *testing.T, rec *model.Record, proj string, asset model.AssetID, from, to model.Date) float64 {
	t.Helper()
	bd, err := Breakdown(rec, proj, asset, from, to)
	must(t, err)
	l := line(bd, CauseTax)
	return l.Actual + l.Projected
}

// bookedTax checks that the tax a breakdown books in the year is the reconciliation of the year before plus the year's withholding: of the paychecks only if the asset holds only the checking account, else of vests too. That is what TaxYearOf reports for both years.
func bookedTax(t *testing.T, rec *model.Record, proj string, asset model.AssetID, year int, vests bool) {
	t.Helper()
	prev, cur := taxYear(t, rec, proj, year-1), taxYear(t, rec, proj, year)
	want := -(prev.Due + cur.WithheldPaychecks)
	if vests {
		want -= cur.WithheldVests
	}
	got := taxLine(t, rec, proj, asset, d(year-1, time.December, 31), d(year, time.December, 31))
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s %v %d: breakdown tax %.2f, want reconciliation %.2f + withholding = %.2f", proj, asset, year, got, prev.Due, want)
	}
}

func TestDemoWithholdingIsCloseToTaxDue(t *testing.T) {
	rec := demodata.Family()
	for _, year := range []int{2026, 2027, 2028} {
		ty := taxYear(t, rec, demodata.ProjBase, year)
		if ty.Result.Total <= 0 || ty.Withheld() <= 0 {
			t.Fatalf("%d: tax %v, withheld %v", year, ty.Result.Total, ty.Withheld())
		}
		if math.Abs(ty.Due) > 0.06*ty.Result.Total {
			t.Errorf("%d: withheld %.0f of %.0f due", year, ty.Withheld(), ty.Result.Total)
		}
		if math.Abs(ty.Due-(ty.Result.Total-ty.Withheld())) > 1e-6 {
			t.Errorf("%d: due %v is not tax less withholding", year, ty.Due)
		}
	}
	// The April payment or refund is the difference, on the checking account.
	bookedTax(t, rec, demodata.ProjBase, model.DefaultAssetID(demodata.Checking), 2027, false)
	bookedTax(t, rec, demodata.ProjBase, model.DefaultAssetID(demodata.Checking), 2028, false)
}

func TestTaxYearMatchesBreakdown(t *testing.T) {
	rec := demodata.Family()
	// The net worth holds every account: the tax it books in 2027 is the April reconciliation of 2026 plus 2027's withholding from paychecks and vests.
	bookedTax(t, rec, demodata.ProjBase, demodata.NetWorth, 2027, true)
}

func TestMoreSavingLowersTaxableWagesAndWithholding(t *testing.T) {
	rec := demodata.Family()
	more := model.Modification{
		Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "save-401k"}, Property: model.PropAmount, Values: model.Constant(1100),
	}
	must(t, rec.AddProjection(&model.Projection{Name: "more 401k", Parent: demodata.ProjBase, Mods: []model.Modification{more}}))
	base, got := taxYear(t, rec, demodata.ProjBase, 2027), taxYear(t, rec, "more 401k", 2027)
	if !near(got.Wages, base.Wages) || got.K401 <= base.K401 {
		t.Fatalf("wages %v and %v, 401k %v and %v", base.Wages, got.Wages, base.K401, got.K401)
	}
	if got.Result.AGI >= base.Result.AGI || got.Result.FederalTaxable >= base.Result.FederalTaxable {
		t.Errorf("income: base %+v, more %+v", base.Result, got.Result)
	}
	if got.WithheldPaychecks >= base.WithheldPaychecks {
		t.Errorf("withheld %.2f, not below %.2f", got.WithheldPaychecks, base.WithheldPaychecks)
	}
}

func TestEarlyWithdrawalPaysTaxAndPenalty(t *testing.T) {
	rec := demodata.Family()
	checking := model.DefaultAssetID(demodata.Checking)
	base, got := taxYear(t, rec, demodata.ProjBase, 2027), taxYear(t, rec, demodata.ProjWithdraw, 2027)
	if got.EarlyWithdrawal != 100000 || !near(got.Result.Penalty, 10000) {
		t.Fatalf("early %v, penalty %v", got.EarlyWithdrawal, got.Result.Penalty)
	}
	extra := got.Result.Total - base.Result.Total
	if extra <= 10000+20000 {
		t.Errorf("tax on the withdrawal is %.0f, want its income tax on top of the 10000 penalty", extra)
	}
	if !near(got.WithheldPaychecks, base.WithheldPaychecks) {
		t.Errorf("a withdrawal withholds nothing: %v and %v", base.WithheldPaychecks, got.WithheldPaychecks)
	}
	// The tax is paid with the 2027 return, in April 2028.
	from, to := d(2027, time.December, 31), d(2028, time.December, 31)
	diff := taxLine(t, rec, demodata.ProjBase, checking, from, to) - taxLine(t, rec, demodata.ProjWithdraw, checking, from, to)
	if math.Abs(diff-extra) > 0.01 {
		t.Errorf("April 2028 payment is %.2f more, want %.2f", diff, extra)
	}
	bd, err := Breakdown(rec, demodata.ProjWithdraw, model.DefaultAssetID(demodata.K401), d(2027, time.May, 31), d(2027, time.June, 30))
	must(t, err)
	if l := line(bd, CauseWithdrawal); !near(l.Projected, -100000) {
		t.Errorf("withdrawal: %+v", l)
	}
	if l := line(bd, CauseTax); l != (Line{}) {
		t.Errorf("the account is not taxed at the withdrawal: %+v", l)
	}
}

func TestLateWithdrawalHasNoPenaltyAndRothIsFree(t *testing.T) {
	rec := demodata.Family()
	rec.Household.Members[0].Birth = d(1960, time.March, 12)
	late := taxYear(t, rec, demodata.ProjWithdraw, 2027)
	if late.EarlyWithdrawal != 0 || late.Result.Penalty != 0 || late.Ordinary < 100000 {
		t.Errorf("withdrawal at 67: early %v, penalty %v, ordinary %v", late.EarlyWithdrawal, late.Result.Penalty, late.Ordinary)
	}
	rec = demodata.Family()
	roth := model.BudgetItem{
		ID: "roth-out", Kind: model.ItemTransfer, Amount: model.Constant(30000), From: demodata.Roth, To: demodata.Checking,
		Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: d(2027, time.June, 1)},
	}
	must(t, rec.AddProjection(&model.Projection{Name: "roth", Parent: demodata.ProjBase, Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: roth.ID}, Property: model.PropAddItem, Item: &roth},
	}}))
	base, got := taxYear(t, rec, demodata.ProjBase, 2027), taxYear(t, rec, "roth", 2027)
	if got.EarlyWithdrawal != 0 || !near(got.Result.Total, base.Result.Total) {
		t.Errorf("a Roth withdrawal is not taxed: %v and %v", base.Result.Total, got.Result.Total)
	}
}

func TestLargeVestYearOwesInApril(t *testing.T) {
	rec := demodata.Family()
	big := &model.FocalGrant{
		Symbol: demodata.Symbol, Value: 1500000, Date: d(2027, time.March, 1), Calendar: demodata.CalendarName,
		YearPercents: []float64{40, 30, 20, 10}, FirstVestMonths: 1, Brokerage: demodata.Brokerage,
	}
	must(t, rec.AddProjection(&model.Projection{Name: "big", Parent: demodata.ProjBase, Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Equity)}, Property: model.PropAddGrant, Grant: big},
	}}))
	base, got := taxYear(t, rec, demodata.ProjBase, 2027), taxYear(t, rec, "big", 2027)
	if got.VestWages <= base.VestWages+300000 {
		t.Fatalf("vest wages %v, base %v", got.VestWages, base.VestWages)
	}
	// The vests withhold 22% federal, below the brackets they land in.
	if got.Due < 10000 {
		t.Fatalf("2027 due %.0f, want a payment", got.Due)
	}
	if base.Due >= 0 {
		t.Errorf("the base year is refunded: %.0f", base.Due)
	}
	bookedTax(t, rec, "big", model.DefaultAssetID(demodata.Checking), 2028, false)
	// April 2028 pays it from checking: the tax booked up to the end of that month exceeds the base by the extra due.
	from, to := d(2027, time.December, 31), d(2028, time.April, 30)
	more := taxLine(t, rec, "big", model.DefaultAssetID(demodata.Checking), from, to) - taxLine(t, rec, demodata.ProjBase, model.DefaultAssetID(demodata.Checking), from, to)
	if !near(more, -(got.Due - base.Due)) {
		t.Errorf("April payment is %.2f more, want %.2f", -more, got.Due-base.Due)
	}
}

// payRecord is a household with two checking accounts and a salary and some interest-free side income, so that the year's tax exceeds its withholding. Its data starts 2026-01-01.
func payRecord(t *testing.T) *model.Record {
	t.Helper()
	rec := model.NewRecord(d(2026, time.January, 1))
	for _, id := range []model.AccountID{"main", "other"} {
		a := &model.Account{ID: id, Type: model.TypeChecking, Tax: model.Taxable}
		a.AddSnapshot(d(2025, time.December, 1), 1000)
		a.AddSnapshot(d(2026, time.January, 1), 1000)
		must(t, rec.AddAccount(a))
	}
	monthly := func(day int) model.Recurrence { return model.Recurrence{Kind: model.Monthly, Day: day} }
	rec.Budget.Items = []model.BudgetItem{
		{ID: "salary", Kind: model.ItemIncome, Amount: model.Constant(20000), Recurrence: monthly(5), Account: "main"},
		{ID: "side", Kind: model.ItemIncome, IncomeType: model.IncomeOrdinary, Amount: model.Constant(50000), Account: "main",
			Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: d(2026, time.June, 1)}},
		{ID: "gift", Kind: model.ItemIncome, IncomeType: model.IncomeTaxFree, Amount: model.Constant(9000), Account: "main",
			Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: d(2026, time.July, 1)}},
	}
	return rec
}

func TestReconciliationLandsOnApril15InTheCheckingAccount(t *testing.T) {
	rec := payRecord(t)
	ty := taxYear(t, rec, "", 2026)
	if ty.Due <= 0 || ty.ReconciliationDate != d(2027, time.April, 15) || ty.ReconciliationAccount != "main" {
		t.Fatalf("due %v on %v from %q", ty.Due, ty.ReconciliationDate, ty.ReconciliationAccount)
	}
	if !near(ty.Ordinary, 50000) || !near(ty.Wages, 240000) {
		t.Errorf("tax-free income is not taxed: wages %v, ordinary %v", ty.Wages, ty.Ordinary)
	}
	day := func(asset model.AccountID, after, to model.Date) float64 {
		return taxLine(t, rec, "", model.DefaultAssetID(asset), after, to)
	}
	if got := day("main", d(2027, time.April, 14), d(2027, time.April, 15)); !near(got, -ty.Due) {
		t.Errorf("paid on 15 April: %v, want %v", got, -ty.Due)
	}
	if got := day("main", d(2027, time.April, 15), d(2027, time.April, 30)); got != 0 {
		t.Errorf("tax after 15 April: %v", got)
	}
	if got := day("other", d(2027, time.April, 14), d(2027, time.April, 15)); got != 0 {
		t.Errorf("the other account paid: %v", got)
	}
	// A household that names its account pays from it, and the first checking account is not touched.
	rec.Household.Checking = "other"
	if got := day("other", d(2027, time.April, 14), d(2027, time.April, 15)); !near(got, -ty.Due) {
		t.Errorf("paid from the household's account: %v, want %v", got, -ty.Due)
	}
	if got := day("main", d(2027, time.April, 14), d(2027, time.April, 15)); got != 0 {
		t.Errorf("the first checking account paid: %v", got)
	}
	// Before the year's end the tax is not settled.
	if got := day("other", d(2026, time.December, 31), d(2027, time.April, 14)); got != 0 {
		t.Errorf("settled early: %v", got)
	}
}

func TestNoCheckingAccountIsReportedNotAPanic(t *testing.T) {
	rec := payRecord(t)
	rec.Accounts = rec.Accounts[:0]
	rec = model.NewRecord(d(2026, time.January, 1))
	sav := &model.Account{ID: "sav", Type: model.TypeSavings, Tax: model.Taxable}
	sav.AddSnapshot(d(2025, time.December, 1), 1000)
	sav.AddSnapshot(d(2026, time.January, 1), 1000)
	must(t, rec.AddAccount(sav))
	rec.Budget.Items = payRecord(t).Budget.Items
	for i := range rec.Budget.Items {
		rec.Budget.Items[i].Account = "sav"
	}
	res, err := Balances(rec, "", model.DefaultAssetID("sav"), []model.Date{d(2027, time.June, 1)})
	must(t, err)
	if len(res.Notes) == 0 {
		t.Error("no note about the missing checking account")
	}
	if ty := taxYear(t, rec, "", 2026); ty.ReconciliationAccount != "" {
		t.Errorf("account %q", ty.ReconciliationAccount)
	}
}

func TestSocialSecurityStopsAtTheWageBase(t *testing.T) {
	rec := payRecord(t)
	r, err := prepare(rec, "", model.DefaultAssetID("main"), d(2026, time.December, 31))
	must(t, err)
	ty := r.tax.result(2026)
	base := ty.Result.SocialSecurity / 0.062
	if base >= 240000 || base < 100000 {
		t.Fatalf("wage base %v", base)
	}
	// Equal paychecks differ only in Social Security: the first pays 6.2% of it, the last none.
	first, last := r.tax.paycheckTax("salary", d(2026, time.January, 5)), r.tax.paycheckTax("salary", d(2026, time.December, 5))
	if !near(first-last, 0.062*20000) {
		t.Errorf("first paycheck withholds %v, last %v", first, last)
	}
	total := 0.0
	for m := time.January; m <= time.December; m++ {
		total += r.tax.paycheckTax("salary", d(2026, m, 5))
	}
	if want := ty.Result.SocialSecurity + ty.Result.Medicare + ty.Result.AdditionalMedicare; total < want {
		t.Errorf("payroll tax withheld %v is below the %v due", total, want)
	}
}

func TestRothContributionDoesNotLowerWagesOrWithholding(t *testing.T) {
	base := taxYear(t, payRecord(t), "", 2026)
	rec := payRecord(t)
	roth := &model.Account{ID: "roth401k", Type: model.Type401k, Tax: model.TaxFree}
	roth.AddSnapshot(d(2026, time.January, 1), 0)
	must(t, rec.AddAccount(roth))
	rec.Budget.Items = append(rec.Budget.Items, model.BudgetItem{
		ID: "save-roth", Kind: model.ItemTransfer, Amount: model.Constant(1500), From: "main", To: "roth401k",
		Recurrence: model.Recurrence{Kind: model.Monthly, Day: 6},
	})
	got := taxYear(t, rec, "", 2026)
	if got.K401 != 0 || !near(got.Result.AGI, base.Result.AGI) || !near(got.WithheldPaychecks, base.WithheldPaychecks) {
		t.Errorf("401k %v, AGI %v and withheld %v, want 0, %v and %v", got.K401, got.Result.AGI, got.WithheldPaychecks, base.Result.AGI, base.WithheldPaychecks)
	}
}

func TestReconciliationSkipsClosedCheckingAccount(t *testing.T) {
	rec := payRecord(t)
	closed := d(2026, time.December, 31)
	rec.Account("main").Closed = &closed
	ty := taxYear(t, rec, "", 2026)
	if ty.Due <= 0 || ty.ReconciliationAccount != "other" {
		t.Fatalf("due %v from %q", ty.Due, ty.ReconciliationAccount)
	}
	line := func(id model.AccountID) float64 {
		return taxLine(t, rec, "", model.DefaultAssetID(id), d(2027, time.April, 14), d(2027, time.April, 15))
	}
	if got := line("other"); !near(got, -ty.Due) {
		t.Errorf("the open account paid %v, want %v", got, -ty.Due)
	}
	if got := line("main"); got != 0 {
		t.Errorf("the closed account paid %v", got)
	}
	// A household that names a closed account falls back to an open checking account.
	rec.Household.Checking = "main"
	if id := ReconciliationAccount(rec, ty.ReconciliationDate); id != "other" {
		t.Errorf("account %q, want other", id)
	}
	if got := line("other"); !near(got, -ty.Due) {
		t.Errorf("the other account paid %v, want %v", got, -ty.Due)
	}
	if got := line("main"); got != 0 {
		t.Errorf("the closed account paid %v", got)
	}
	rec.Account("other").Closed = &closed
	if id := ReconciliationAccount(rec, ty.ReconciliationDate); id != "" {
		t.Errorf("account %q, want none", id)
	}
	res, err := Balances(rec, "", model.DefaultAssetID("other"), []model.Date{d(2027, time.June, 1)})
	must(t, err)
	if len(res.Notes) == 0 {
		t.Error("no note about the closed reconciliation account")
	}
}

func TestPaycheckFICAIncludesHSA(t *testing.T) {
	rec := payRecord(t)
	rec.Budget.Items[0].Amount = model.Constant(5000)
	hsa := &model.Account{ID: "hsa", Type: model.TypeHSA, Tax: model.TaxFree}
	hsa.AddSnapshot(d(2026, time.January, 1), 0)
	must(t, rec.AddAccount(hsa))
	rec.Budget.Items = append(rec.Budget.Items, model.BudgetItem{
		ID: "save-hsa", Kind: model.ItemTransfer, Amount: model.Constant(500), From: "main", To: "hsa",
		Recurrence: model.Recurrence{Kind: model.Monthly, Day: 6},
	})
	ty := taxYear(t, rec, "", 2026)
	if !near(ty.HSA, 6000) || !near(ty.Wages, 60000) {
		t.Fatalf("wages %v, HSA %v", ty.Wages, ty.HSA)
	}
	r, err := prepare(rec, "", model.DefaultAssetID("main"), d(2026, time.December, 31))
	must(t, err)
	hh := rec.Household
	w, err := tax.Compute(rec.Tax, tax.Input{
		Year: 2026, Status: hh.Filing, State: hh.State, DependentAges: hh.DependentAges(2026), Inflation: r.tax.inflation(2026),
		Earners: []tax.Earner{{Wages: ty.Wages, HSA: ty.HSA}},
	})
	must(t, err)
	fica := ty.WithheldPaychecks - (w.FederalTax - w.Credits + w.StateTax)
	if want := ty.Result.SocialSecurity + ty.Result.Medicare + ty.Result.AdditionalMedicare; math.Abs(fica-want) > 0.01 {
		t.Errorf("FICA withheld %.2f, due %.2f", fica, want)
	}
}

func TestInterestStopsWhenAccountCloses(t *testing.T) {
	rec := model.NewRecord(d(2026, time.January, 1))
	sav := &model.Account{ID: "sav", Type: model.TypeSavings, Tax: model.Taxable, StatedRate: 0.04}
	sav.AddSnapshot(d(2025, time.December, 1), 1000000)
	sav.AddSnapshot(d(2026, time.January, 1), 1000000)
	closed := d(2026, time.June, 30)
	sav.Closed = &closed
	must(t, rec.AddAccount(sav))
	if got := taxYear(t, rec, "", 2026).Interest; got < 15000 || got > 25000 {
		t.Errorf("2026 interest %v, want about half a year of 4%% on 1,000,000", got)
	}
	if got := taxYear(t, rec, "", 2027).Interest; got != 0 {
		t.Errorf("2027 interest %v after the account closed", got)
	}
}

func TestCheckingPreStartInterestIsTaxed(t *testing.T) {
	rec := model.NewRecord(d(2026, time.March, 1))
	chk := &model.Account{ID: "chk", Type: model.TypeChecking, Tax: model.Taxable}
	chk.AddSnapshot(d(2025, time.December, 1), 1000)
	chk.AddSnapshot(d(2026, time.February, 1), 1100)
	must(t, rec.AddAccount(chk))
	if got := taxYear(t, rec, "", 2026).Interest; !near(got, 100) {
		t.Errorf("interest %v, want the 100 the snapshots add", got)
	}
}
