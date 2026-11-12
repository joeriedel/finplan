// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/tax"
)

type thousehold struct {
	Filing         string `json:"filing_status"`
	State          string
	Checking       *string
	Reconciliation *string `json:"reconciliation_account"`
	Members        []struct{ Name, Birth, Role string }
}

type thouseholdOut struct {
	Changed   bool
	Household thousehold
	Member    struct{ Name, Birth, Role string }
	Removed   string
}

type tcompute struct {
	Year           int
	Filing         string `json:"filing_status"`
	State          string
	DependentAges  []int `json:"dependent_ages"`
	AGI            float64
	FederalTaxable float64 `json:"federal_taxable"`
	FederalTax     float64 `json:"federal_tax"`
	Credits        float64
	StateTaxable   float64 `json:"state_taxable"`
	StateTax       float64 `json:"state_tax"`
	SocialSecurity float64 `json:"social_security"`
	Medicare       float64
	Total          float64
}

func showHousehold(t *testing.T, path string) thousehold {
	t.Helper()
	var h thousehold
	cliJSON(t, &h, "--db", path, "household", "show")
	return h
}

func computeTax(t *testing.T, path string, args ...string) tcompute {
	t.Helper()
	var c tcompute
	cliJSON(t, &c, append([]string{"--db", path, "tax", "compute"}, args...)...)
	return c
}

func nearTo(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.005 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Scenario 5: the household and the income types round-trip through the command line.
func TestCLIHouseholdAndIncomeTypes(t *testing.T) {
	path := seededFile(t)
	h := showHousehold(t, path)
	if h.Filing != "mfj" || h.State != "VA" || h.Checking != nil || h.Reconciliation == nil || *h.Reconciliation != "checking" || len(h.Members) != 6 || h.Members[0].Name != "Parent A" || h.Members[0].Role != "earner" {
		t.Fatalf("household %+v", h)
	}

	var w thouseholdOut
	run := func(args ...string) {
		t.Helper()
		w = thouseholdOut{}
		cliJSON(t, &w, append([]string{"--db", path}, args...)...)
	}
	run("household", "set", "--filing", "single", "--checking", "Checking")
	if !w.Changed || w.Household.Filing != "single" || w.Household.Checking == nil || *w.Household.Checking != "checking" {
		t.Errorf("set: %+v", w)
	}
	if h := showHousehold(t, path); h.Filing != "single" || h.State != "VA" || h.Checking == nil {
		t.Errorf("after set: %+v", h)
	}
	run("household", "set", "--filing", "single")
	if w.Changed {
		t.Error("setting the same filing status changed the household")
	}
	run("household", "set", "--filing", "mfj", "--checking", "none")
	if h := showHousehold(t, path); h.Filing != "mfj" || h.Checking != nil {
		t.Errorf("after reset: %+v", h)
	}

	run("household", "member", "add", "Baby", "--birth", "2026-03-01", "--role", "dependent")
	if !w.Changed || w.Member.Name != "Baby" || w.Member.Birth != "2026-03-01" || w.Member.Role != "dependent" || len(w.Household.Members) != 7 {
		t.Errorf("member add: %+v", w)
	}
	run("household", "member", "edit", "Baby", "--name", "Infant", "--birth", "2026-04-01")
	if w.Member.Name != "Infant" || w.Member.Birth != "2026-04-01" || w.Household.Members[6].Name != "Infant" {
		t.Errorf("member edit: %+v", w)
	}
	run("household", "member", "remove", "Infant")
	if w.Removed != "Infant" || len(w.Household.Members) != 6 || w.Member.Name != "Infant" {
		t.Errorf("member remove: %+v", w)
	}
	refusedWrite(t, path, "", exitFailure, "name_taken", "household", "member", "add", "parent a", "--birth", "1990-01-01", "--role", "spouse")
	refusedWrite(t, path, "", exitFailure, "invalid", "household", "member", "add", "Future", "--birth", "2030-01-01", "--role", "dependent")
	refusedWrite(t, path, "", exitFailure, "not_found", "household", "member", "remove", "Nobody")
	refusedWrite(t, path, "", exitFailure, "not_found", "household", "set", "--state", "ZZ")
	refusedWrite(t, path, "", exitFailure, "invalid", "household", "set", "--checking", "Savings")
	refusedWrite(t, path, "", exitUsage, "usage", "household", "set", "--filing", "widowed")

	type item struct {
		ID         string
		Kind       string
		IncomeType *string `json:"income_type"`
	}
	var list []item
	cliJSON(t, &list, "--db", path, "budget", "list")
	types := map[string]*string{}
	for _, it := range list {
		types[it.ID] = it.IncomeType
	}
	if types["salary-a"] == nil || *types["salary-a"] != "wages" || types["groceries"] != nil {
		t.Errorf("income types of the demo items: %v", types)
	}

	var added struct{ Item item }
	cliJSON(t, &added, "--db", path, "budget", "add", "--kind", "income", "--name", "Interest", "--amount", "100", "--every", "monthly", "--start", "2027-01-01", "--account", "checking", "--income-type", "ordinary")
	if added.Item.ID != "interest" || added.Item.IncomeType == nil || *added.Item.IncomeType != "ordinary" {
		t.Errorf("added item %+v", added.Item)
	}
	cliJSON(t, &added, "--db", path, "budget", "edit", "interest", "--income-type", "tax-free")
	if added.Item.IncomeType == nil || *added.Item.IncomeType != "tax-free" {
		t.Errorf("edited item %+v", added.Item)
	}
	cliJSON(t, &added, "--db", path, "budget", "edit", "interest", "--amount", "120")
	if added.Item.IncomeType == nil || *added.Item.IncomeType != "tax-free" {
		t.Errorf("an edit of the amount lost the income type: %+v", added.Item)
	}
	code, out := wcli(t, `{"name": "Gift", "kind": "income", "amount": 50, "income_type": "tax-free", "recurrence": {"kind": "monthly", "day": 2}, "start": "2027-01-01", "account": "checking"}`, "--db", path, "budget", "add")
	if code != 0 || json.Unmarshal(out, &added) != nil || added.Item.IncomeType == nil || *added.Item.IncomeType != "tax-free" {
		t.Errorf("JSON add: %d %s", code, out)
	}
	cliJSON(t, &added, "--db", path, "budget", "add", "--kind", "expense", "--name", "Fees", "--amount", "5", "--every", "monthly", "--start", "2027-01-01", "--account", "checking", "--income-type", "wages")
	if added.Item.IncomeType != nil {
		t.Errorf("an expense has income type %q", *added.Item.IncomeType)
	}
	refusedWrite(t, path, `{"name": "Bad", "kind": "income", "amount": 5, "income_type": "salary", "recurrence": {"kind": "monthly", "day": 2}, "start": "2027-01-01", "account": "checking"}`, exitFailure, "invalid", "budget", "add")
	refusedWrite(t, path, "", exitUsage, "usage", "budget", "add", "--kind", "income", "--name", "Bad", "--amount", "5", "--every", "monthly", "--start", "2027-01-01", "--account", "checking", "--income-type", "salary")
}

// Scenario 6: editing a Virginia bracket through tax table set changes tax compute, and tax table revert restores it.
func TestCLITaxTableSetAndRevert(t *testing.T) {
	path := seededFile(t)
	// The invented family has dependents aged 17, 13, 10 and 7 in 2025: one credit of 500 and three of 2,200.
	base := computeTax(t, path, "--year", "2025", "--wages", "200000")
	if base.Filing != "mfj" || base.State != "VA" || !reflect.DeepEqual(base.DependentAges, []int{17, 13, 10, 7}) {
		t.Fatalf("compute: %+v", base)
	}
	nearTo(t, "federal taxable", base.FederalTaxable, 168500)
	nearTo(t, "federal tax", base.FederalTax, 26898)
	nearTo(t, "credits", base.Credits, 7100)
	nearTo(t, "state taxable", base.StateTaxable, 177420)
	nearTo(t, "state tax", base.StateTax, 9944.15)
	nearTo(t, "social security", base.SocialSecurity, 10918.2)
	nearTo(t, "medicare", base.Medicare, 2900)
	nearTo(t, "total", base.Total, 26898-7100+9944.15+10918.2+2900)

	var va tax.State
	cliJSON(t, &va, "--db", path, "tax", "table", "show", "--state", "VA", "--year", "2025")
	if va.Name != "VA" || va.Year != 2025 || len(va.Brackets) != 4 || va.Brackets[3].Rate != 0.0575 {
		t.Fatalf("shown table %+v", va)
	}
	va.Brackets[3].Rate = 0.06
	body, err := json.Marshal(va)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "va.json")
	if err := os.WriteFile(file, body, 0o644); err != nil {
		t.Fatal(err)
	}
	before := dbSnapshot(t, path)
	var w struct {
		DryRun  bool `json:"dry_run"`
		Changed bool
		State   *tax.State
	}
	cliJSON(t, &w, "--db", path, "tax", "table", "set", "--json-file", file, "--dry-run")
	if !w.DryRun || !w.Changed || w.State == nil || w.State.Brackets[3].Rate != 0.06 || !reflect.DeepEqual(before, dbSnapshot(t, path)) {
		t.Fatalf("dry run: %+v", w)
	}
	cliJSON(t, &w, "--db", path, "tax", "table", "set", "--json-file", file)
	if w.DryRun || !w.Changed || w.State == nil || w.State.Brackets[3].Rate != 0.06 {
		t.Fatalf("set: %+v", w)
	}
	edited := computeTax(t, path, "--year", "2025", "--wages", "200000")
	nearTo(t, "edited state tax", edited.StateTax, 60+60+600+0.06*(177420-17000))
	nearTo(t, "edited federal tax", edited.FederalTax, base.FederalTax)
	cliJSON(t, &w, "--db", path, "tax", "table", "set", "--json-file", file)
	if w.Changed {
		t.Error("setting the same table changed it")
	}

	cliJSON(t, &w, "--db", path, "tax", "table", "revert", "--state", "VA", "--year", "2025")
	if !w.Changed || w.State == nil || w.State.Brackets[3].Rate != 0.0575 {
		t.Fatalf("revert: %+v", w)
	}
	if got := computeTax(t, path, "--year", "2025", "--wages", "200000"); !reflect.DeepEqual(got, base) {
		t.Errorf("after revert %+v, want %+v", got, base)
	}

	// A federal table is edited the same way and reverted by year.
	var f tax.Federal
	cliJSON(t, &f, "--db", path, "tax", "table", "show", "--year", "2025")
	mfj := f.Status[tax.MarriedJoint]
	mfj.StandardDeduction = 20000
	f.Status[tax.MarriedJoint] = mfj
	body, _ = json.Marshal(f)
	var fw struct {
		Changed bool
		Federal *tax.Federal
	}
	code, out := wcli(t, string(body), "--db", path, "tax", "table", "set")
	if code != 0 || json.Unmarshal(out, &fw) != nil || !fw.Changed || fw.Federal == nil {
		t.Fatalf("federal set: %d %s", code, out)
	}
	nearTo(t, "federal taxable with the lower deduction", computeTax(t, path, "--year", "2025", "--wages", "200000").FederalTaxable, 180000)
	cliJSON(t, &fw, "--db", path, "tax", "table", "revert", "--year", "2025")
	nearTo(t, "federal taxable after revert", computeTax(t, path, "--year", "2025", "--wages", "200000").FederalTaxable, 168500)

	var index struct {
		Federal []int
		State   []struct {
			Name string
			Year int
		}
	}
	cliJSON(t, &index, "--db", path, "tax", "table", "show")
	if !reflect.DeepEqual(index.Federal, []int{2024, 2025, 2026}) || len(index.State) != 3 || index.State[0].Name != "VA" {
		t.Errorf("table index %+v", index)
	}

	refusedWrite(t, path, "", exitFailure, "not_found", "tax", "table", "revert", "--year", "1999")
	refusedWrite(t, path, "", exitFailure, "not_found", "tax", "table", "revert", "--year", "2025", "--state", "ZZ")
	refusedWrite(t, path, `{"name": "VA", "year": 2025, "brackets": [{"over": 5, "rate": 0.1}]}`, exitFailure, "invalid", "tax", "table", "set")
	refusedWrite(t, path, `{"name": "VA", "year": 2025, "bogus": 1}`, exitUsage, "usage", "tax", "table", "set")
	cliFail(t, exitUsage, "--db", path, "tax", "table", "show", "--state", "VA")
	cliFail(t, exitFailure, "--db", path, "tax", "table", "show", "--year", "1999")
}

// Scenario 7: a made-up flat-rate state table entered through the command line and named in the household is used instead of Virginia.
func TestCLIFlatStateTable(t *testing.T) {
	path := seededFile(t)
	flat := `{"name": "FLAT", "year": 2025, "brackets": [{"over": 0, "rate": 0.05}], "standard_deduction": {}, "exemption": 0, "dependent_exemption": 0, "cap_529": 0}`
	refusedWrite(t, path, "", exitFailure, "not_found", "household", "set", "--state", "FLAT")
	code, out := wcli(t, flat, "--db", path, "tax", "table", "set")
	if code != 0 {
		t.Fatalf("set: %d %s", code, out)
	}
	if got := showHousehold(t, path); got.State != "VA" {
		t.Errorf("adding a table changed the household's state to %q", got.State)
	}
	virginia := computeTax(t, path, "--year", "2025", "--wages", "200000")
	var h struct{ Household thousehold }
	cliJSON(t, &h, "--db", path, "household", "set", "--state", "FLAT")
	if h.Household.State != "FLAT" {
		t.Fatalf("household %+v", h)
	}
	got := computeTax(t, path, "--year", "2025", "--wages", "200000")
	if got.State != "FLAT" {
		t.Errorf("computed for state %q", got.State)
	}
	nearTo(t, "state taxable", got.StateTaxable, 200000)
	nearTo(t, "state tax", got.StateTax, 10000)
	if got.StateTax == virginia.StateTax || got.FederalTax != virginia.FederalTax {
		t.Errorf("Virginia %+v, flat %+v", virginia, got)
	}
	// Without --year the year of the record's today (2026) is used, and a year past the tables is indexed by the inflation given.
	if c := computeTax(t, path, "--wages", "100000"); c.Year != 2026 {
		t.Errorf("year %d", c.Year)
	}
	// Four years at 10% scale the 2026 deduction of 32,200 to 47,144, which rounds to 47,150.
	nearTo(t, "indexed taxable income", computeTax(t, path, "--year", "2030", "--wages", "100000", "--inflation", "0.1").FederalTaxable, 100000-47150)
}

// Wages, 401(k) and IRA are given for each earner, and the wage base and the limits apply to each.
func TestCLITaxComputeEarners(t *testing.T) {
	path := seededFile(t)
	c := computeTax(t, path, "--year", "2025", "--wages", "200000,100000", "--k401", "30000,30000", "--ira", "9000")
	nearTo(t, "social security", c.SocialSecurity, 0.062*(176100+100000))
	nearTo(t, "medicare", c.Medicare, 0.0145*300000)
	nearTo(t, "agi", c.AGI, 300000-2*23500-7000)
	cliFail(t, exitUsage, "--db", path, "tax", "compute", "--wages", "1,x")
}

type ttaxYear struct {
	Year   int
	Inputs struct {
		Wages         float64
		VestWages     float64                          `json:"vest_wages"`
		PreTax        struct{ K401, IRA, HSA float64 } `json:"pre_tax"`
		Ordinary      float64                          `json:"ordinary_income"`
		Early         float64                          `json:"early_withdrawal"`
		DependentAges []int                            `json:"dependent_ages"`
		Contrib529    []float64                        `json:"contrib_529"`
	}
	Tax struct {
		Penalty float64 `json:"early_withdrawal_penalty"`
		Total   float64
	}
	Withheld       struct{ Paychecks, Vests, Total float64 }
	Due            float64 `json:"due_minus_withheld"`
	Reconciliation struct {
		Date    string
		Account *string
		Amount  float64
	}
}

// tax year reports the inputs, the tax, the withholding and the April settlement of a year, and agrees with the breakdown: its tax line for 2027 is the reconciliation of 2026 plus 2027's withholding.
func TestCLITaxYearMatchesBreakdown(t *testing.T) {
	var y26, y27 ttaxYear
	cliJSON(t, &y26, "--demo", "tax", "year", "2026", "--projection", "Base")
	cliJSON(t, &y27, "--demo", "tax", "year", "2027", "--projection", "Base")
	if y27.Year != 2027 || y27.Inputs.Wages < 200000 || y27.Inputs.VestWages <= 0 || y27.Inputs.PreTax.K401 < 20000 || len(y27.Inputs.DependentAges) != 4 || len(y27.Inputs.Contrib529) != 4 {
		t.Fatalf("2027 inputs = %+v", y27.Inputs)
	}
	if y27.Reconciliation.Date != "2028-04-15" || y27.Reconciliation.Account == nil || *y27.Reconciliation.Account != "checking" {
		t.Errorf("reconciliation = %+v", y27.Reconciliation)
	}
	nearTo(t, "withheld total", y27.Withheld.Total, y27.Withheld.Paychecks+y27.Withheld.Vests)
	if math.Abs(y27.Due-(y27.Tax.Total-y27.Withheld.Total)) > 0.02 || y27.Reconciliation.Amount != y27.Due {
		t.Errorf("due %v, tax %v, withheld %v, reconciliation %v", y27.Due, y27.Tax.Total, y27.Withheld.Total, y27.Reconciliation.Amount)
	}
	var bd struct {
		Lines []struct {
			Cause             string
			Actual, Projected float64
		}
	}
	cliJSON(t, &bd, "--demo", "breakdown", "net-worth", "--from", "2026-12-31", "--to", "2027-12-31", "--projection", "Base")
	var got float64
	for _, l := range bd.Lines {
		if l.Cause == "tax" {
			got = l.Actual + l.Projected
		}
	}
	if want := -(y26.Reconciliation.Amount + y27.Withheld.Total); math.Abs(got-want) > 0.05 {
		t.Errorf("breakdown tax %v, want 2026 reconciliation %v plus 2027 withholding %v = %v", got, y26.Reconciliation.Amount, y27.Withheld.Total, want)
	}
}

func TestCLITaxYearEarlyWithdrawalAndErrors(t *testing.T) {
	var w ttaxYear
	cliJSON(t, &w, "--demo", "tax", "year", "2027", "--projection", "Withdraw $100k")
	if w.Inputs.Early != 100000 || w.Tax.Penalty != 10000 || w.Due <= 30000 {
		t.Errorf("withdrawal year = %+v", w)
	}
	cliFail(t, exitUsage, "--demo", "tax", "year", "soon")
	cliFail(t, exitUsage, "--demo", "tax", "year")
	if code, _ := cliFail(t, exitFailure, "--demo", "tax", "year", "2027", "--projection", "nope"); code != "not_found" {
		t.Errorf("unknown projection: code %q", code)
	}
}

// A copy of Virginia added through tax table copy is a state a household can name; the command refuses a name in use and an unknown source.
func TestCLITaxTableCopy(t *testing.T) {
	path := seededFile(t)
	before := dbSnapshot(t, path)
	var w struct {
		DryRun  bool `json:"dry_run"`
		Changed bool
		State   string
		Years   []int
	}
	cliJSON(t, &w, "--db", path, "tax", "table", "copy", "--state", "VA", "--name", "COPY", "--dry-run")
	if !w.DryRun || !w.Changed || w.State != "COPY" || !reflect.DeepEqual(before, dbSnapshot(t, path)) {
		t.Fatalf("dry run: %+v", w)
	}
	cliJSON(t, &w, "--db", path, "tax", "table", "copy", "--state", "VA", "--name", "COPY")
	if w.DryRun || !w.Changed || !reflect.DeepEqual(w.Years, []int{2024, 2025, 2026}) {
		t.Fatalf("copy: %+v", w)
	}
	var s tax.State
	cliJSON(t, &s, "--db", path, "tax", "table", "show", "--state", "COPY", "--year", "2025")
	if va, _ := tax.Defaults().StateYear("VA", 2025); s.Name != "COPY" || !reflect.DeepEqual(s.Brackets, va.Brackets) {
		t.Errorf("copied table %+v", s)
	}
	virginia := computeTax(t, path, "--year", "2025", "--wages", "200000")
	var h struct{ Household thousehold }
	cliJSON(t, &h, "--db", path, "household", "set", "--state", "COPY")
	if got := computeTax(t, path, "--year", "2025", "--wages", "200000"); got.State != "COPY" || got.StateTax != virginia.StateTax {
		t.Errorf("computed %+v, Virginia %+v", got, virginia)
	}
	refusedWrite(t, path, "", exitFailure, "name_taken", "tax", "table", "copy", "--state", "VA", "--name", "COPY")
	refusedWrite(t, path, "", exitFailure, "not_found", "tax", "table", "copy", "--state", "ZZ", "--name", "OTHER")
	refusedWrite(t, path, "", exitFailure, "invalid", "tax", "table", "copy", "--state", "VA", "--name", " X")
	cliFail(t, exitUsage, "--db", path, "tax", "table", "copy", "--state", "VA")
}
