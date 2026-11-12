// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/benefits"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
)

// retireRecord is a household with an earner born in the year, filing single with no state, no inflation and no growth, and the accounts of the buckets with the balances given (an account with none is left out).
func retireRecord(t *testing.T, born int, sav, brk, k401, roth float64) *model.Record {
	t.Helper()
	rec := model.NewRecord(d(2026, time.January, 1))
	rec.Globals[model.PropInflation] = model.Constant(0)
	rec.Household = model.Household{Filing: tax.Single, Members: []model.HouseholdMember{{Name: "A", Role: model.RoleEarner, Birth: d(born, time.June, 15)}}}
	for _, a := range []struct {
		id  model.AccountID
		typ model.AccountType
		tr  model.TaxTreatment
		bal float64
	}{
		{"chk", model.TypeChecking, model.Taxable, 0},
		{"sav", model.TypeSavings, model.Taxable, sav},
		{"brk", model.TypeBrokerage, model.Taxable, brk},
		{"k401", model.Type401k, model.TaxDeferred, k401},
		{"roth", model.TypeRothIRA, model.TaxFree, roth},
	} {
		if a.id != "chk" && a.bal == 0 {
			continue
		}
		acct := &model.Account{ID: a.id, Type: a.typ, Tax: a.tr}
		acct.AddSnapshot(d(2025, time.December, 1), a.bal)
		acct.AddSnapshot(d(2026, time.January, 1), a.bal)
		must(t, rec.AddAccount(acct))
	}
	return rec
}

func addItems(rec *model.Record, items ...model.BudgetItem) {
	rec.Budget.Items = append(rec.Budget.Items, items...)
}

func monthlyOn(day int) model.Recurrence { return model.Recurrence{Kind: model.Monthly, Day: day} }

func bal(t *testing.T, rec *model.Record, proj string, id model.AccountID, date model.Date) float64 {
	t.Helper()
	return sample(t, rec, proj, model.DefaultAssetID(id), date).Amount
}

func retire(t *testing.T, rec *model.Record, name string, on model.Date, mutate func(*model.Projection)) {
	t.Helper()
	p := &model.Projection{Name: name, RetireOn: model.Ptr(on)}
	if mutate != nil {
		mutate(p)
	}
	must(t, rec.AddProjection(p))
}

func TestRetirementStopsWagesAndDrawsTaxableThenDeferredThenRoth(t *testing.T) {
	rec := retireRecord(t, 1960, 20000, 100000, 200000, 50000)
	addItems(rec,
		model.BudgetItem{ID: "salary", Kind: model.ItemIncome, Amount: model.Constant(5000), Recurrence: monthlyOn(1), Account: "chk", IncomeType: model.IncomeWages},
		model.BudgetItem{ID: "living", Kind: model.ItemExpense, Amount: model.Constant(5000), Recurrence: monthlyOn(10), Account: "chk", Timing: model.Lump},
		model.BudgetItem{ID: "save", Kind: model.ItemTransfer, Amount: model.Constant(100), Recurrence: monthlyOn(20), From: "chk", To: "roth"},
	)
	retire(t, rec, "retired", d(2027, time.January, 1), nil)
	if got, want := taxYear(t, rec, "retired", 2026).Wages, 60000.0; !near(got, want) {
		t.Errorf("wages in 2026: got %v, want %v", got, want)
	}
	if got := taxYear(t, rec, "retired", 2027).Wages; got != 0 {
		t.Errorf("wages in 2027: got %v, want none", got)
	}
	if got := taxYear(t, rec, "", 2027).Wages; !near(got, 60000) {
		t.Errorf("wages in 2027 without retirement: got %v, want 60000", got)
	}
	end := d(2027, time.December, 31)
	// Taxable money first: savings are used up before the brokerage; tax-deferred and Roth money is untouched, and no more goes into the Roth IRA.
	got := map[model.AccountID]float64{}
	for _, id := range []model.AccountID{"chk", "sav", "brk", "k401", "roth"} {
		got[id] = bal(t, rec, "retired", id, end)
	}
	// The 100 a month into the Roth IRA stops with retirement; it ran all of 2026.
	if math.Abs(got["chk"]) > 0.01 || got["sav"] > 0.01 || got["brk"] >= 100000 || got["brk"] <= 0 || !near(got["k401"], 200000) || !near(got["roth"], 51200) {
		t.Errorf("balances at the end of 2027: %v", got)
	}
	if n := taxYear(t, rec, "retired", 2027).Diagnostics.Notes; len(n) != 0 {
		t.Errorf("notes: %v", n)
	}
}

func TestWithdrawalOrderChoosesTheBucketsAndTheirTax(t *testing.T) {
	rec := retireRecord(t, 1960, 20000, 100000, 200000, 50000)
	addItems(rec, model.BudgetItem{ID: "living", Kind: model.ItemExpense, Amount: model.Constant(5000), Recurrence: monthlyOn(10), Account: "chk", Timing: model.Lump})
	retire(t, rec, "roth first", d(2026, time.January, 1), func(p *model.Projection) {
		p.WithdrawalOrder = []model.Bucket{model.BucketRoth, model.BucketDeferred, model.BucketTaxable}
	})
	retire(t, rec, "deferred first", d(2026, time.January, 1), func(p *model.Projection) {
		p.WithdrawalOrder = []model.Bucket{model.BucketDeferred, model.BucketTaxable, model.BucketRoth}
	})
	end := d(2026, time.December, 31)
	if roth, k := bal(t, rec, "roth first", "roth", end), bal(t, rec, "roth first", "k401", end); !near(roth, 0) || !near(k, 200000-10000) {
		t.Errorf("roth first: roth %v, 401k %v", roth, k)
	}
	if got := taxYear(t, rec, "roth first", 2026); !near(got.Drawn, 10000) || got.Result.FederalTax != 0 {
		t.Errorf("the Roth withdrawals are untaxed and the 10000 from the 401k is under the standard deduction: drawn %v, tax %v", got.Drawn, got.Result.FederalTax)
	}
	if sav, k := bal(t, rec, "deferred first", "sav", end), bal(t, rec, "deferred first", "k401", end); !near(sav, 20000) || !near(k, 200000-60000) {
		t.Errorf("deferred first: savings %v, 401k %v", sav, k)
	}
	ty := taxYear(t, rec, "deferred first", 2026)
	if !near(ty.Drawn, 60000) || !near(ty.Ordinary, 60000) || ty.EarlyWithdrawal != 0 || ty.Result.FederalTax <= 0 {
		t.Errorf("deferred withdrawals are ordinary income: drawn %v, ordinary %v, early %v, tax %v", ty.Drawn, ty.Ordinary, ty.EarlyWithdrawal, ty.Result.FederalTax)
	}
}

func TestTaxableSalesRealizeGainsAndTheTaxIsPaidFromTheNextDrawdown(t *testing.T) {
	rec := retireRecord(t, 1960, 0, 100000, 0, 0)
	rec.Classes, rec.Correlations = model.DefaultClasses(), model.DefaultCorrelations()
	rec.Account("brk").Allocation = []model.Holding{{Class: model.ClassLargeCap, Percent: 100}}
	addItems(rec, model.BudgetItem{ID: "living", Kind: model.ItemExpense, Amount: model.Constant(5000), Recurrence: monthlyOn(10), Account: "chk", Timing: model.Lump})
	retire(t, rec, "retired", d(2026, time.January, 1), nil)
	ty := taxYear(t, rec, "retired", 2027)
	if ty.ShortTermGains+ty.LongTermGains <= 0 {
		t.Errorf("selling appreciated shares should realize gains: %+v", ty)
	}
	// The tax of 2026 is paid in April 2027 from checking and drawn from the brokerage like any expense, so checking is at zero after.
	if got := bal(t, rec, "retired", "chk", d(2027, time.April, 30)); math.Abs(got) > 0.01 {
		t.Errorf("checking after the tax payment: %v", got)
	}
}

func TestEarlyDeferredWithdrawalsArePenalized(t *testing.T) {
	rec := retireRecord(t, 1975, 0, 0, 200000, 0)
	addItems(rec, model.BudgetItem{ID: "living", Kind: model.ItemExpense, Amount: model.Constant(1000), Recurrence: monthlyOn(10), Account: "chk", Timing: model.Lump})
	retire(t, rec, "retired", d(2026, time.January, 1), nil)
	ty := taxYear(t, rec, "retired", 2026)
	if !near(ty.EarlyWithdrawal, 12000) || !near(ty.Result.Penalty, 1200) || ty.RMD != 0 {
		t.Errorf("early %v, penalty %v, rmd %v", ty.EarlyWithdrawal, ty.Result.Penalty, ty.RMD)
	}
}

func TestMoneyRunningOutIsNotedOnce(t *testing.T) {
	rec := retireRecord(t, 1960, 10000, 0, 0, 0)
	addItems(rec, model.BudgetItem{ID: "living", Kind: model.ItemExpense, Amount: model.Constant(4000), Recurrence: monthlyOn(10), Account: "chk", Timing: model.Lump})
	retire(t, rec, "retired", d(2026, time.January, 1), nil)
	res, err := Balances(rec, "retired", model.DefaultAssetID("chk"), []model.Date{d(2026, time.December, 31)})
	must(t, err)
	var runOut []string
	for _, n := range res.Notes {
		if strings.Contains(n, "money runs out") {
			runOut = append(runOut, n)
		}
	}
	if len(runOut) != 1 || !strings.Contains(runOut[0], "2026-03-31") {
		t.Errorf("notes: %v", res.Notes)
	}
	// Savings pay January and February in full and March in part (2000 short), and nothing pays April to December.
	if got := res.Samples[0].Amount; !near(got, -38000) {
		t.Errorf("checking at the end of the year: %v", got)
	}
	plain, err := Balances(rec, "", model.DefaultAssetID("chk"), []model.Date{d(2026, time.December, 31)})
	must(t, err)
	if len(plain.Notes) != 0 {
		t.Errorf("without retirement nothing is drawn and nothing is noted: %v", plain.Notes)
	}
}

func TestRequiredDistributionsStartAtTheRightAgeAndAmount(t *testing.T) {
	for _, c := range []struct {
		born, first int
		age         int
	}{{1959, 2032, 73}, {1960, 2035, 75}, {1950, 2026, 76}} {
		rec := retireRecord(t, c.born, 0, 0, 600000, 0)
		for _, y := range []int{c.first - 1, c.first} {
			ty := taxYear(t, rec, "", y)
			if y < c.first {
				if ty.RMD != 0 {
					t.Errorf("born %d: RMD in %d: %v, want none", c.born, y, ty.RMD)
				}
				continue
			}
			want := 600000 / benefits.UniformLifetimeDivisor(c.age)
			if !near(ty.RMD, want) || !near(ty.Ordinary, want) || ty.Result.Penalty != 0 || !near(ty.Result.AGI, want) {
				t.Errorf("born %d, %d: RMD %v, ordinary %v, penalty %v, want %v", c.born, y, ty.RMD, ty.Ordinary, ty.Result.Penalty, want)
			}
		}
	}
	rec := retireRecord(t, 1950, 0, 0, 600000, 0)
	rmd := 600000 / benefits.UniformLifetimeDivisor(76)
	if got := bal(t, rec, "", "chk", d(2026, time.December, 31)); !near(got, rmd) {
		t.Errorf("checking after the distribution: got %v, want %v", got, rmd)
	}
	if got := bal(t, rec, "", "k401", d(2026, time.December, 31)); !near(got, 600000-rmd) {
		t.Errorf("401k after the distribution: got %v, want %v", got, 600000-rmd)
	}
}

func TestWithdrawalsCountTowardTheRequiredDistribution(t *testing.T) {
	rec := retireRecord(t, 1950, 0, 0, 600000, 0)
	addItems(rec, model.BudgetItem{ID: "living", Kind: model.ItemExpense, Amount: model.Constant(2000), Recurrence: monthlyOn(10), Account: "chk", Timing: model.Lump})
	retire(t, rec, "retired", d(2026, time.January, 1), nil)
	ty := taxYear(t, rec, "retired", 2026)
	rmd := 600000 / benefits.UniformLifetimeDivisor(76)
	if !near(ty.Drawn, 24000) || !near(ty.RMD, rmd-24000) || !near(ty.Ordinary, rmd) {
		t.Errorf("drawn %v, RMD %v, ordinary %v, want the distribution %v in all", ty.Drawn, ty.RMD, ty.Ordinary, rmd)
	}
}

func TestRothConversionsFillTheBracketAndAreTaxed(t *testing.T) {
	rec := retireRecord(t, 1970, 0, 0, 500000, 1)
	top, ded, err := rec.Tax.BracketTop(2026, tax.Single, 0, 0.12)
	must(t, err)
	retire(t, rec, "convert", d(2026, time.January, 1), func(p *model.Projection) { p.RothBracket = model.Ptr(0.12) })
	ty := taxYear(t, rec, "convert", 2026)
	if !near(ty.Conversion, top+ded) || !near(ty.Ordinary, top+ded) || ty.EarlyWithdrawal != 0 || ty.Result.Penalty != 0 {
		t.Fatalf("conversion %v, ordinary %v, want %v", ty.Conversion, ty.Ordinary, top+ded)
	}
	if !near(ty.Result.FederalTaxable, top) || ty.Result.FederalTax <= 0 {
		t.Errorf("taxable %v, tax %v: want the top of the bracket %v taxed", ty.Result.FederalTaxable, ty.Result.FederalTax, top)
	}
	end := d(2026, time.December, 31)
	if roth, k := bal(t, rec, "convert", "roth", end), bal(t, rec, "convert", "k401", end); !near(roth, 1+top+ded) || !near(k, 500000-top-ded) {
		t.Errorf("Roth %v, 401k %v", roth, k)
	}
	// The tax is paid from checking on the next reconciliation date; checking drawn on the deferred account refills it.
	if got := bal(t, rec, "convert", "chk", d(2027, time.April, 30)); math.Abs(got) > 0.01 {
		t.Errorf("checking after the tax payment: %v", got)
	}
	if conv := taxYear(t, rec, "", 2026).Conversion; conv != 0 {
		t.Errorf("without the setting: %v", conv)
	}
}

func TestRothConversionsLeaveRoomForOtherIncomeAndAreLimitedByTheBalance(t *testing.T) {
	rec := retireRecord(t, 1970, 0, 0, 500000, 1)
	top, ded, err := rec.Tax.BracketTop(2026, tax.Single, 0, 0.12)
	must(t, err)
	addItems(rec, model.BudgetItem{ID: "rent", Kind: model.ItemIncome, IncomeType: model.IncomeOrdinary, Amount: model.Constant(2500), Recurrence: monthlyOn(1), Account: "chk"})
	retire(t, rec, "convert", d(2026, time.January, 1), func(p *model.Projection) { p.RothBracket = model.Ptr(0.12) })
	if got := taxYear(t, rec, "convert", 2026).Conversion; !near(got, top+ded-30000) {
		t.Errorf("conversion with 30000 of other income: got %v, want %v", got, top+ded-30000)
	}
	small := retireRecord(t, 1970, 0, 0, 5000, 1)
	retire(t, small, "convert", d(2026, time.January, 1), func(p *model.Projection) { p.RothBracket = model.Ptr(0.12) })
	if got := taxYear(t, small, "convert", 2026).Conversion; !near(got, 5000) {
		t.Errorf("conversion limited by the balance: got %v", got)
	}
	big := retireRecord(t, 1970, 0, 0, 5000000, 1)
	addItems(big, model.BudgetItem{ID: "rent", Kind: model.ItemIncome, IncomeType: model.IncomeOrdinary, Amount: model.Constant(20000), Recurrence: monthlyOn(1), Account: "chk"})
	retire(t, big, "convert", d(2026, time.January, 1), func(p *model.Projection) { p.RothBracket = model.Ptr(0.12) })
	if got := taxYear(t, big, "convert", 2026).Conversion; got != 0 {
		t.Errorf("no room in the bracket: got %v", got)
	}
}

func TestRothConversionsInheritDownTheChain(t *testing.T) {
	rec := retireRecord(t, 1970, 0, 0, 500000, 1)
	retire(t, rec, "parent", d(2026, time.January, 1), func(p *model.Projection) { p.RothBracket = model.Ptr(0.12) })
	must(t, rec.AddProjection(&model.Projection{Name: "child", Parent: "parent"}))
	if got := taxYear(t, rec, "child", 2026).Conversion; got <= 0 {
		t.Errorf("a child inherits the bracket and the retirement date: %v", got)
	}
	must(t, rec.AddProjection(&model.Projection{Name: "later", Parent: "parent", RetireOn: model.Ptr(d(2030, time.January, 1))}))
	if got := taxYear(t, rec, "later", 2026).Conversion; got != 0 {
		t.Errorf("conversions wait for retirement: %v", got)
	}
}

func TestRothConversionsStopAtTheBracketTopWithPartlyTaxedSocialSecurity(t *testing.T) {
	rec := retireRecord(t, 1960, 0, 0, 5000000, 1)
	rec.Household.Members[0].SSBenefit, rec.Household.Members[0].SSClaimAge = 3000, 67
	top, _, err := rec.Tax.BracketTop(2030, tax.Single, 0, 0.12)
	must(t, err)
	addItems(rec, model.BudgetItem{ID: "rent", Kind: model.ItemIncome, IncomeType: model.IncomeOrdinary, Amount: model.Constant(1000), Recurrence: monthlyOn(1), Account: "chk"})
	retire(t, rec, "convert", d(2026, time.January, 1), func(p *model.Projection) { p.RothBracket = model.Ptr(0.12) })
	ty := taxYear(t, rec, "convert", 2030)
	if ty.Benefits <= 0 || ty.Result.TaxableBenefits <= 0 || ty.Result.TaxableBenefits >= 0.85*ty.Benefits {
		t.Fatalf("want partly taxed benefits: benefits %v, taxable %v", ty.Benefits, ty.Result.TaxableBenefits)
	}
	if ty.Conversion <= 0 || ty.Result.FederalTaxable > top+0.01 || ty.Result.FederalTaxable < top-0.01 {
		t.Errorf("conversion %v leaves taxable income %v, want the bracket top %v", ty.Conversion, ty.Result.FederalTaxable, top)
	}
}

func TestBudgetWithdrawalsFromDeferredAccountsCountTowardTheRequiredDistribution(t *testing.T) {
	rec := retireRecord(t, 1950, 0, 0, 600000, 0)
	addItems(rec, model.BudgetItem{ID: "pull", Kind: model.ItemTransfer, Amount: model.Constant(5000), Recurrence: monthlyOn(10), From: "k401", To: "chk"})
	ty := taxYear(t, rec, "", 2026)
	if ty.RMD != 0 || !near(ty.Ordinary, 60000) {
		t.Errorf("the 60000 withdrawn by the budget covers the distribution: RMD %v, ordinary %v", ty.RMD, ty.Ordinary)
	}
}

func TestTaxFreeRetirementAccountsAreDrawnAsRothMoney(t *testing.T) {
	rec := retireRecord(t, 1960, 0, 0, 200000, 0)
	r401 := &model.Account{ID: "r401", Type: model.Type401k, Tax: model.TaxFree}
	r401.AddSnapshot(d(2025, time.December, 1), 100000)
	r401.AddSnapshot(d(2026, time.January, 1), 100000)
	must(t, rec.AddAccount(r401))
	addItems(rec, model.BudgetItem{ID: "living", Kind: model.ItemExpense, Amount: model.Constant(1000), Recurrence: monthlyOn(10), Account: "chk", Timing: model.Lump})
	retire(t, rec, "retired", d(2026, time.January, 1), func(p *model.Projection) {
		p.WithdrawalOrder = []model.Bucket{model.BucketRoth, model.BucketDeferred, model.BucketTaxable}
	})
	end := d(2026, time.December, 31)
	if r, k := bal(t, rec, "retired", "r401", end), bal(t, rec, "retired", "k401", end); !near(r, 88000) || !near(k, 200000) {
		t.Errorf("tax-free 401k %v, deferred 401k %v", r, k)
	}
	if ty := taxYear(t, rec, "retired", 2026); ty.Drawn != 0 || ty.Ordinary != 0 {
		t.Errorf("a tax-free withdrawal is untaxed: drawn %v, ordinary %v", ty.Drawn, ty.Ordinary)
	}
}

func TestConversionRoomCountsGainsInProvisionalIncomeAndNetting(t *testing.T) {
	tables := tax.Defaults()
	top, _, err := tables.BracketTop(2026, tax.MarriedJoint, 0, 0.12)
	must(t, err)
	for _, c := range []struct {
		name        string
		in          tax.Input
		wantTaxable bool
	}{
		{"long-term gains make benefits taxable", tax.Input{Year: 2026, Status: tax.MarriedJoint, BenefitIncome: 80000, LongTermGains: 10000}, true},
		{"short-term loss nets against long-term gain", tax.Input{Year: 2026, Status: tax.MarriedJoint, ShortTermGains: -5000, LongTermGains: 20000}, false},
	} {
		take, err := conversionRoom(tables, c.in, top, 5e6)
		must(t, err)
		c.in.OtherIncome = take
		res, err := tax.Compute(tables, c.in)
		must(t, err)
		if c.wantTaxable && res.TaxableBenefits <= 0 {
			t.Errorf("%s: want taxable benefits", c.name)
		}
		if got := res.FederalTaxable - min(res.PreferredGains, res.FederalTaxable); math.Abs(got-top) > 0.01 {
			t.Errorf("%s: ordinary taxable income %v with a conversion of %v, want the bracket top %v", c.name, got, take, top)
		}
	}
}
