// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package demodata provides invented, deterministic data for exercising the UI and the engine:
// Family builds a complete model record. None of the numbers describe a real person.
package demodata

import (
	"fmt"
	"math"
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
)

// FamilyToday is the fixed reference date of the Family record, so the data is deterministic.
var FamilyToday = model.D(2026, time.November, 8)

// Account ids of the Family record.
const (
	Checking  model.AccountID = "checking"
	Savings   model.AccountID = "savings"
	Card      model.AccountID = "card"
	Brokerage model.AccountID = "brokerage"
	K401      model.AccountID = "401k"
	Roth      model.AccountID = "roth"
	TSP       model.AccountID = "tsp"
	Home      model.AccountID = "home"
	Mortgage  model.AccountID = "mortgage"
	College1  model.AccountID = "529-oldest"
	College2  model.AccountID = "529-middle"
	College3  model.AccountID = "529-youngest"
	// Equity holds the restricted stock units of an invented employer, Acme Corp, whose ticker is Symbol.
	Equity model.AccountID = "equity"
)

// The invented employer's stock and vest calendar.
const (
	Symbol       = "ACME"
	CalendarName = "Acme Corp"
)

// Asset ids of the Family record.
const (
	AllCash    model.AssetID = "all-cash"
	Retirement model.AssetID = "retirement"
	Colleges   model.AssetID = "529s"
	NetWorth   model.AssetID = "net-worth"
	// Liquid contains All cash and also names checking itself, which overlaps, so that member is invalid.
	Liquid model.AssetID = "liquid"
)

// Projection names of the Family record.
const (
	ProjBase     = "Base"
	ProjNewJob   = "New job"
	ProjWithdraw = "Withdraw $100k"
	// ProjAcmeUp has the Acme stock growing 15% a year, ProjAcmeCrash a 40% drop over the year from 2027-06-01 and ProjAcmeGrants a new grant of Acme units every March from 2027.
	ProjAcmeUp     = "Acme +15%"
	ProjAcmeCrash  = "Acme crash"
	ProjAcmeGrants = "Acme yearly grants"
)

var (
	historyOpen = model.D(2024, time.October, 31)
	tspEnd      = model.D(2026, time.June, 30)
)

type accountSpec struct {
	id      model.AccountID
	name    string
	typ     model.AccountType
	tax     model.TaxTreatment
	rate    float64
	opening float64
	// end is the last date of real data; zero means FamilyToday.
	end model.Date
}

var accountSpecs = []accountSpec{
	{Checking, "Checking", model.TypeChecking, model.Taxable, 0, 18000, 0},
	{Savings, "Savings", model.TypeSavings, model.Taxable, 0.04, 40000, 0},
	{Card, "Credit card", model.TypeCreditCard, model.Taxable, 0, -2700, 0},
	{Brokerage, "Brokerage", model.TypeBrokerage, model.Taxable, 0.06, 120000, 0},
	{K401, "401k", model.Type401k, model.TaxDeferred, 0.06, 310000, 0},
	{Roth, "Roth IRA", model.TypeRothIRA, model.TaxFree, 0.06, 85000, 0},
	{TSP, "TSP", model.TypeTSP, model.TaxDeferred, 0.055, 140000, tspEnd},
	{Mortgage, "Mortgage", model.TypeMortgage, model.Taxable, 0.06, -412000, 0},
	{College1, "529 oldest", model.Type529, model.Tax529, 0.05, 38000, 0},
	{College2, "529 middle", model.Type529, model.Tax529, 0.05, 26000, 0},
	{College3, "529 youngest", model.Type529, model.Tax529, 0.05, 14000, 0},
}

func monthly(day int) model.Recurrence {
	return model.Recurrence{Kind: model.Monthly, Day: day}
}

func familyBudget() model.Budget {
	income := func(id, name string, amount float64, rec model.Recurrence) model.BudgetItem {
		return model.BudgetItem{ID: id, Name: name, Kind: model.ItemIncome, Amount: model.Constant(amount), Recurrence: rec, Indexed: true, Account: Checking, IncomeType: model.IncomeWages}
	}
	expense := func(id, name string, from model.AccountID, amount float64, rec model.Recurrence) model.BudgetItem {
		return model.BudgetItem{ID: id, Name: name, Kind: model.ItemExpense, Amount: model.Constant(amount), Recurrence: rec, Indexed: true, Account: from}
	}
	transfer := func(id, name string, from, to model.AccountID, amount float64, day int) model.BudgetItem {
		return model.BudgetItem{ID: id, Name: name, Kind: model.ItemTransfer, Amount: model.Constant(amount), Recurrence: monthly(day), From: from, To: to}
	}
	return model.Budget{Items: []model.BudgetItem{
		income("salary-a", "Salary (Parent A)", 5000, model.Recurrence{Kind: model.Semimonthly, Day: 15, Day2: 31}),
		income("salary-b", "Salary (Parent B)", 3700, model.Recurrence{Kind: model.Biweekly, Anchor: model.D(2024, time.November, 8)}),
		transfer("mortgage-payment", "Mortgage payment", Checking, Mortgage, 2600, 1),
		transfer("card-payment", "Credit card payment", Checking, Card, 2700, 5),
		expense("groceries", "Groceries", Card, 1800, monthly(3)),
		expense("dining", "Dining out", Card, 900, monthly(6)),
		expense("utilities", "Utilities", Checking, 450, monthly(12)),
		expense("childcare", "Childcare and activities", Checking, 1200, monthly(3)),
		expense("transport", "Transportation", Checking, 600, monthly(8)),
		expense("insurance", "Insurance", Checking, 700, monthly(20)),
		expense("vacation", "Family vacation", Checking, 6000, model.Recurrence{Kind: model.Annual, Month: time.June, Day: 15}),
		transfer("save-401k", "401k contribution", Checking, K401, 1000, 16),
		transfer("save-tsp", "TSP contribution", Checking, TSP, 800, 16),
		transfer("save-savings", "Savings deposit", Checking, Savings, 500, 25),
		transfer("save-brokerage", "Brokerage deposit", Checking, Brokerage, 800, 25),
		transfer("save-roth", "Roth IRA contribution", Checking, Roth, 500, 25),
		transfer("save-529-1", "529 contribution (oldest)", Checking, College1, 170, 25),
		transfer("save-529-2", "529 contribution (middle)", Checking, College2, 170, 25),
		transfer("save-529-3", "529 contribution (youngest)", Checking, College3, 170, 25),
	}}
}

// Family returns an invented record for a family of six: thirteen accounts with two years of history,
// a basic budget, aggregate assets and three projections. Every number is invented. The TSP is
// entered by hand, so its data ends months before the synced accounts', and the home has only
// occasional valuations. One parent's employer pays in restricted stock units: the equity account has two grants, seven vests recorded with their brokerage deposits, and a price history. The Liquid asset contains an overlapping member.
func Family() *model.Record {
	rec := model.NewRecord(FamilyToday)
	rec.Globals[model.PropInflation] = model.Constant(0.025)
	rec.Budget = familyBudget()
	rec.Household = model.Household{
		Members: []model.HouseholdMember{
			{Name: "Parent A", Birth: model.D(1985, time.March, 12), Role: model.RoleEarner},
			{Name: "Parent B", Birth: model.D(1986, time.July, 4), Role: model.RoleSpouse},
			{Name: "Child 1", Birth: model.D(2008, time.April, 9), Role: model.RoleDependent},
			{Name: "Child 2", Birth: model.D(2012, time.September, 20), Role: model.RoleDependent},
			{Name: "Child 3", Birth: model.D(2015, time.May, 2), Role: model.RoleDependent},
			{Name: "Child 4", Birth: model.D(2018, time.January, 15), Role: model.RoleDependent},
		},
		Filing: tax.MarriedJoint,
		State:  tax.VA,
	}
	rsu := familyRSU()

	for _, s := range accountSpecs {
		a := &model.Account{ID: s.id, Name: s.name, Type: s.typ, Tax: s.tax, StatedRate: s.rate}
		if s.id == Roth {
			a.Default.Intervals = []model.Interval{{From: model.MinDate, To: model.Ptr(historyOpen.Add(-1)), Value: 70000}}
		}
		must(rec.AddAccount(a))
		if s.id == Brokerage {
			for _, d := range rsu.deposits {
				must(a.AddTransaction(d))
			}
		}
		generateHistory(rec, a, s)
	}
	home := &model.Account{ID: Home, Name: "Home", Type: model.TypeProperty, Tax: model.Taxable, StatedRate: 0.03}
	home.AddSnapshot(model.D(2024, time.December, 15), 520000)
	home.AddSnapshot(model.D(2025, time.September, 20), 535000)
	home.AddSnapshot(model.D(2026, time.May, 10), 548000)
	must(rec.AddAccount(home))
	rsu.addTo(rec)

	cash := []model.AccountType{model.TypeChecking, model.TypeSavings}
	must(rec.AddAsset(&model.Asset{ID: AllCash, Name: "All cash", Members: []model.Member{model.RuleMember(model.Rule{Types: cash})}}))
	must(rec.AddAsset(&model.Asset{ID: Retirement, Name: "Retirement", Members: []model.Member{
		model.AccountMember(K401), model.AccountMember(TSP), model.AccountMember(Roth),
	}}))
	must(rec.AddAsset(&model.Asset{ID: Colleges, Name: "529 accounts", Members: []model.Member{
		model.RuleMember(model.Rule{Tax: []model.TaxTreatment{model.Tax529}}),
	}, Params: map[string]model.Schedule{model.PropGrowth: model.MustSchedule(
		model.Segment{From: model.MinDate, Value: 0.04},
		model.Segment{From: model.D(2029, time.January, 1), Value: 0.06},
		model.Segment{From: model.D(2035, time.January, 1), Value: 0.02},
	)}}))
	must(rec.AddAsset(&model.Asset{ID: NetWorth, Name: "Net worth", Members: []model.Member{
		model.RuleMember(model.Rule{}),
	}}))
	must(rec.AddAsset(&model.Asset{ID: Liquid, Name: "Liquid assets", Members: []model.Member{
		model.AssetMember(AllCash), model.AccountMember(Brokerage), model.AccountMember(Checking),
	}}))

	growth := model.MustSchedule(
		model.Segment{From: model.MinDate, Value: 0.04},
		model.Segment{From: model.D(2029, time.January, 1), Value: 0.06},
		model.Segment{From: model.D(2035, time.January, 1), Value: 0.02},
	)
	must(rec.AddProjection(&model.Projection{Name: ProjBase, Start: model.Ptr(FamilyToday)}))
	must(rec.AddProjection(&model.Projection{Name: ProjNewJob, Parent: ProjBase, Mods: []model.Modification{
		{
			Target:   model.ObjectRef{Kind: model.KindBudgetItem, ID: "salary-a"},
			Property: model.PropAmount,
			Values:   model.MustSchedule(model.Segment{From: model.D(2027, time.March, 1), Value: 6500}),
		},
		{
			Target:   model.ObjectRef{Kind: model.KindAccount, ID: string(Brokerage)},
			Property: model.PropGrowth,
			Values:   growth,
		},
	}}))
	withdrawal := model.BudgetItem{
		ID: "early-401k-withdrawal", Name: "Early 401k withdrawal", Kind: model.ItemTransfer, Amount: model.Constant(100000),
		Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: model.D(2027, time.June, 1)}, From: K401, To: Checking,
	}
	must(rec.AddProjection(&model.Projection{Name: ProjWithdraw, Parent: ProjBase, Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: withdrawal.ID}, Property: model.PropAddItem, Item: &withdrawal},
	}}))
	must(rec.AddProjection(&model.Projection{Name: ProjAcmeUp, Parent: ProjBase, Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindStock, ID: Symbol}, Property: model.PropGrowth, Values: model.Constant(0.15)},
	}}))
	must(rec.AddProjection(&model.Projection{Name: ProjAcmeCrash, Parent: ProjBase, Mods: []model.Modification{
		{
			Target:   model.ObjectRef{Kind: model.KindStock, ID: Symbol},
			Property: model.PropGrowth,
			Values:   model.MustSchedule(model.Segment{From: model.D(2027, time.June, 1), End: model.Ptr(model.D(2028, time.May, 31)), Value: -0.40}),
		},
	}}))
	must(rec.AddProjection(&model.Projection{Name: ProjAcmeGrants, Parent: ProjBase, Mods: []model.Modification{
		{
			Target:   model.ObjectRef{Kind: model.KindAccount, ID: string(Equity)},
			Property: model.PropAddGrant,
			Grant: &model.FocalGrant{Symbol: Symbol, Value: 80000, Date: model.D(2027, time.March, 1), Yearly: true, Calendar: CalendarName,
				YearPercents: []float64{40, 30, 20, 10}, FirstVestMonths: 1, Brokerage: Brokerage},
		},
	}}))
	return rec
}

// generateHistory fills an account with the transactions its budget items imply, from just after the
// opening snapshot to the account's end, and with month-end snapshots that also include growth the
// transactions do not explain.
func generateHistory(rec *model.Record, a *model.Account, s accountSpec) {
	end := s.end
	if end == 0 {
		end = FamilyToday
	}
	for _, it := range rec.Budget.Items {
		if !it.Touches(a.ID) {
			continue
		}
		for _, d := range it.Dates(historyOpen, end) {
			amount, ok := it.Amount.At(d)
			if !ok {
				continue
			}
			t := model.Transaction{Date: d, Memo: it.Name}
			switch {
			case it.Kind == model.ItemIncome:
				t.Kind, t.Amount = model.TxIncome, amount*netPay(rec, d)
			case it.Kind == model.ItemExpense:
				t.Kind, t.Amount = model.TxExpense, -amount
			case it.From == a.ID:
				t.Kind, t.Amount, t.Counter = model.TxTransfer, -amount, it.To
			default:
				t.Kind, t.Amount, t.Counter = model.TxTransfer, amount, it.From
			}
			must(a.AddTransaction(t))
		}
	}
	bal := s.opening
	a.AddSnapshot(historyOpen, bal)
	rate := model.Constant(s.rate)
	for prev := historyOpen; prev < end; {
		y, m, _ := prev.Add(1).YMD()
		next := min(model.D(y, m+1, 0), end)
		bal = bal*rate.Compound(prev, next) + sumAmounts(a.TransactionsIn(prev, next))
		bal = math.Round(bal*100) / 100
		a.AddSnapshot(next, bal)
		prev = next
	}
}

// netPay is the share of gross pay that lands in an account on d: what remains after the year's tax on the budget's wages and 401k and TSP contributions, as the engine withholds it through the year.
func netPay(rec *model.Record, d model.Date) float64 {
	year, _, _ := d.YMD()
	var earner tax.Earner
	for _, it := range rec.Budget.Items {
		for _, day := range it.Dates(model.D(year, time.January, 1).Add(-1), model.D(year, time.December, 31)) {
			amount, ok := it.Amount.At(day)
			switch {
			case !ok:
			case it.Kind == model.ItemIncome:
				earner.Wages += amount
			case it.To == K401 || it.To == TSP:
				earner.K401 += amount
			}
		}
	}
	res, err := tax.Compute(rec.Tax, tax.Input{
		Year: year, Status: rec.Household.Filing, State: rec.Household.State,
		Earners: []tax.Earner{earner}, DependentAges: rec.Household.DependentAges(year), Inflation: 0.025,
	})
	must(err)
	return 1 - res.Total/earner.Wages
}

func sumAmounts(ts []model.Transaction) float64 {
	sum := 0.0
	for _, t := range ts {
		sum += t.Amount
	}
	return sum
}

func must(err error) {
	if err != nil {
		panic(fmt.Sprintf("demodata: invalid built-in record: %v", err))
	}
}
