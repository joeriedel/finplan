// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package tax

import (
	"math"
	"testing"
)

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.005 {
		t.Errorf("%s = %.4f, want %.4f", what, got, want)
	}
}

func TestDefaultsValidate(t *testing.T) {
	d := Defaults()
	if len(d.Federal) != 3 || len(d.State) != 3 {
		t.Fatalf("defaults have %d federal and %d state tables, want 3 and 3", len(d.Federal), len(d.State))
	}
	for _, f := range d.Federal {
		if err := f.Validate(); err != nil {
			t.Errorf("federal %d: %v", f.Year, err)
		}
	}
	for _, s := range d.State {
		if err := s.Validate(); err != nil {
			t.Errorf("state %s %d: %v", s.Name, s.Year, err)
		}
	}
}

func TestDefaultsAreFresh(t *testing.T) {
	a := Defaults()
	a.Federal[0].Status[MarriedJoint].Brackets[0].Rate = 0.5
	a.State[0].Brackets[0].Rate = 0.5
	b := Defaults()
	if b.Federal[0].Status[MarriedJoint].Brackets[0].Rate != 0.10 || b.State[0].Brackets[0].Rate != 0.02 {
		t.Error("changing one copy of the defaults changed another")
	}
}

func one(wages, k401 float64) []Earner { return []Earner{{Wages: wages, K401: k401}} }

func TestComputeAppliesLimitsToEachEarner(t *testing.T) {
	// Each person is over the wage base and defers more than the 401(k) limit; the second spouse earns under the base.
	over := Earner{Wages: 200000, K401: 30000, IRA: 9000}
	under := Earner{Wages: 100000, K401: 30000, IRA: 3000}
	got, err := Compute(Defaults(), Input{Year: 2025, Status: MarriedJoint, Earners: []Earner{over, under}})
	if err != nil {
		t.Fatal(err)
	}
	wantSS := 0.062 * (176100 + 100000)
	if math.Abs(got.SocialSecurity-wantSS) > 0.005 {
		t.Errorf("social security %v, want %v", got.SocialSecurity, wantSS)
	}
	// Deductions: 2 x 23,500 of 401(k) and 7,000 + 3,000 of IRA.
	if want := 300000.0 - 47000 - 10000; got.AGI != want {
		t.Errorf("AGI %v, want %v", got.AGI, want)
	}
	if want := 0.0145 * 300000; math.Abs(got.Medicare-want) > 0.005 {
		t.Errorf("medicare %v, want %v", got.Medicare, want)
	}
	// Both over the wage base.
	got, err = Compute(Defaults(), Input{Year: 2025, Status: MarriedJoint, Earners: []Earner{{Wages: 200000}, {Wages: 180000}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := 2 * 176100 * 0.062; math.Abs(got.SocialSecurity-want) > 0.005 {
		t.Errorf("social security %v, want %v", got.SocialSecurity, want)
	}
}

func TestCompute(t *testing.T) {
	kids4 := []int{3, 6, 9, 12}
	tests := []struct {
		name string
		in   Input
		want Result
	}{
		{
			// 168,500 taxable: 2,385 + 8,772 + 15,741 = 26,898 before the 4 x 2,200 child credit. Virginia: 200,000 - 17,000 - 6 x 930 = 177,420 taxable.
			name: "2025 mfj 200k four children",
			in:   Input{Year: 2025, Status: MarriedJoint, State: "VA", Earners: one(200000, 0), DependentAges: kids4},
			want: Result{AGI: 200000, FederalTaxable: 168500, FederalTax: 26898, Credits: 8800, StateTaxable: 177420, StateTax: 9944.15,
				SocialSecurity: 10918.20, Medicare: 2900, Total: 18098 + 9944.15 + 10918.20 + 2900},
		},
		{
			name: "2025 single 100k, no state",
			in:   Input{Year: 2025, Status: Single, Earners: one(100000, 0)},
			want: Result{AGI: 100000, FederalTaxable: 84250, FederalTax: 13449, SocialSecurity: 6200, Medicare: 1450, Total: 13449 + 6200 + 1450},
		},
		{
			name: "social security stops at the wage base",
			in:   Input{Year: 2025, Status: MarriedJoint, Earners: one(300000, 0)},
			want: Result{AGI: 300000, FederalTaxable: 268500, FederalTax: 2385 + 8772 + 0.22*(206700-96950) + 0.24*(268500-206700),
				SocialSecurity: 176100 * 0.062, Medicare: 4350, AdditionalMedicare: 450,
				Total: 2385 + 8772 + 0.22*(206700-96950) + 0.24*(268500-206700) + 176100*0.062 + 4350 + 450},
		},
		{
			name: "401k above the limit deducts the limit and leaves FICA wages",
			in:   Input{Year: 2025, Status: MarriedJoint, Earners: one(200000, 30000)},
			want: Result{AGI: 176500, FederalTaxable: 145000, FederalTax: 2385 + 8772 + 0.22*(145000-96950),
				SocialSecurity: 176100 * 0.062, Medicare: 2900,
				Total: 2385 + 8772 + 0.22*(145000-96950) + 176100*0.062 + 2900},
		},
		{
			name: "HSA through payroll reduces FICA wages",
			in:   Input{Year: 2025, Status: Single, Earners: []Earner{{Wages: 100000, HSA: 9000}}},
			want: Result{AGI: 91450, FederalTaxable: 75700, FederalTax: 1192.5 + 4386 + 0.22*(75700-48475),
				SocialSecurity: 0.062 * 91450, Medicare: 0.0145 * 91450,
				Total: 1192.5 + 4386 + 0.22*(75700-48475) + 0.062*91450 + 0.0145*91450},
		},
		{
			name: "early withdrawal is income and takes the penalty",
			in:   Input{Year: 2025, Status: MarriedJoint, EarlyWithdrawal: 10000},
			want: Result{AGI: 10000, Penalty: 1000, Total: 1000},
		},
		{
			name: "NIIT on the investment income above the threshold",
			in:   Input{Year: 2025, Status: MarriedJoint, OtherIncome: 300000, NetInvestmentIncome: 100000},
			want: Result{AGI: 300000, FederalTaxable: 268500, FederalTax: 2385 + 8772 + 0.22*(206700-96950) + 0.24*(268500-206700),
				NIIT: 1900, Total: 2385 + 8772 + 0.22*(206700-96950) + 0.24*(268500-206700) + 1900},
		},
		{
			// 410,000 is 10,000 over the phase-out: the 4,400 credit shrinks by 500.
			name: "child credit phases out",
			in:   Input{Year: 2025, Status: MarriedJoint, OtherIncome: 410000, DependentAges: []int{5, 8}},
			want: Result{AGI: 410000, FederalTaxable: 378500, FederalTax: 2385 + 8772 + 0.22*(206700-96950) + 0.24*(378500-206700), Credits: 3900,
				Total: 2385 + 8772 + 0.22*(206700-96950) + 0.24*(378500-206700) - 3900},
		},
		{
			name: "a dependent of 17 earns the other dependent credit",
			in:   Input{Year: 2025, Status: MarriedJoint, OtherIncome: 100000, DependentAges: []int{17, 4}},
			want: Result{AGI: 100000, FederalTaxable: 68500, FederalTax: 2385 + 0.12*(68500-23850), Credits: 2700,
				Total: 2385 + 0.12*(68500-23850) - 2700},
		},
		{
			name: "529 deduction is capped for each account",
			in:   Input{Year: 2025, Status: MarriedJoint, State: "VA", OtherIncome: 100000, Contrib529: []float64{6000, 1000}},
			want: Result{AGI: 100000, FederalTaxable: 68500, FederalTax: 2385 + 0.12*(68500-23850),
				StateTaxable: 100000 - 17000 - 2*930 - 5000, StateTax: 60 + 60 + 600 + 0.0575*(100000-17000-2*930-5000-17000),
				Total: 2385 + 0.12*(68500-23850) + 60 + 60 + 600 + 0.0575*(100000-17000-2*930-5000-17000)},
		},
		{
			name: "a year before the tables uses the first",
			in:   Input{Year: 2020, Status: Single, OtherIncome: 14600 + 11600},
			want: Result{AGI: 26200, FederalTaxable: 11600, FederalTax: 1160, Total: 1160},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compute(Defaults(), tc.in)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range []struct {
				name      string
				got, want float64
			}{
				{"AGI", got.AGI, tc.want.AGI}, {"FederalTaxable", got.FederalTaxable, tc.want.FederalTaxable},
				{"FederalTax", got.FederalTax, tc.want.FederalTax}, {"Credits", got.Credits, tc.want.Credits},
				{"NIIT", got.NIIT, tc.want.NIIT}, {"Penalty", got.Penalty, tc.want.Penalty},
				{"StateTaxable", got.StateTaxable, tc.want.StateTaxable}, {"StateTax", got.StateTax, tc.want.StateTax},
				{"SocialSecurity", got.SocialSecurity, tc.want.SocialSecurity}, {"Medicare", got.Medicare, tc.want.Medicare},
				{"AdditionalMedicare", got.AdditionalMedicare, tc.want.AdditionalMedicare}, {"Total", got.Total, tc.want.Total},
			} {
				near(t, f.name, f.got, f.want)
			}
		})
	}
}

func TestComputeEachYear(t *testing.T) {
	// Taxable 100,000 married filing jointly, with the tax computed by hand from each year's brackets.
	for _, tc := range []struct {
		year int
		std  float64
		tax  float64
	}{
		{2024, 29200, 2320 + 0.12*(94300-23200) + 0.22*(129200-29200-94300)},
		{2025, 31500, 2385 + 0.12*(96950-23850) + 0.22*(131500-31500-96950)},
		{2026, 32200, 2480 + 0.12*(100000-24800)},
	} {
		got, err := Compute(Defaults(), Input{Year: tc.year, Status: MarriedJoint, OtherIncome: tc.std + 100000})
		if err != nil {
			t.Fatal(err)
		}
		near(t, "taxable", got.FederalTaxable, 100000)
		near(t, "tax", got.FederalTax, tc.tax)
	}
}

func TestIndexingPastTheTables(t *testing.T) {
	d := Defaults()
	f, err := d.federalFor(2028, 0.03)
	if err != nil {
		t.Fatal(err)
	}
	s := f.Status[MarriedJoint]
	// Two years at 3% is 1.0609, rounded to 50 for brackets and the deduction, 300 for the wage base, 500 for the 401(k) limit.
	near(t, "first bracket top", s.Brackets[1].Over, 26300)
	near(t, "standard deduction", s.StandardDeduction, 34150)
	near(t, "wage base", f.SocialSecurityWageBase, 195600)
	near(t, "401k limit", f.Limit401k, 26000)
	near(t, "IRA limit", f.LimitIRA, 8000)
	near(t, "other dependent credit", f.OtherDependentCredit, 500)
	near(t, "phase-out", s.ChildCreditPhaseOut, 400000)
	near(t, "additional Medicare", s.AdditionalMedicareThreshold, 250000)
	near(t, "NIIT", s.NIITThreshold, 250000)
	if got, _ := d.FederalYear(2026); got.Status[MarriedJoint].StandardDeduction != 32200 {
		t.Error("indexing changed the stored table")
	}

	// Taxable income of the first bracket's top: the tax at the indexed bracket is 10% of it.
	res, err := Compute(d, Input{Year: 2028, Status: MarriedJoint, OtherIncome: 34150 + 26300, Inflation: 0.03})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "tax", res.FederalTax, 2630)

	// With no inflation the last table is used as it is.
	res, err = Compute(d, Input{Year: 2030, Status: MarriedJoint, OtherIncome: 32200 + 24800})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "flat tax", res.FederalTax, 2480)
}

func TestFlatStateTable(t *testing.T) {
	d := Defaults()
	d.State = append(d.State, State{Name: "FLAT", Year: 2025, Brackets: []Bracket{{0, 0.05}}})
	res, err := Compute(d, Input{Year: 2025, Status: MarriedJoint, State: "FLAT", OtherIncome: 100000})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "state tax", res.StateTax, 5000)
	if _, err := Compute(d, Input{Year: 2025, Status: MarriedJoint, State: "XX"}); err == nil {
		t.Error("an unknown state was accepted")
	}
	if _, err := Compute(d, Input{Year: 2025, Status: "bogus"}); err == nil {
		t.Error("an unknown filing status was accepted")
	}
}

func TestSetKeepsOrder(t *testing.T) {
	d := Defaults()
	f2027 := d.Federal[2].Clone()
	f2027.Year = 2027
	d.SetFederal(f2027)
	f2023 := d.Federal[0].Clone()
	f2023.Year = 2023
	d.SetFederal(f2023)
	var years []int
	for _, f := range d.Federal {
		years = append(years, f.Year)
	}
	want := []int{2023, 2024, 2025, 2026, 2027}
	for i := range want {
		if years[i] != want[i] {
			t.Fatalf("federal years = %v, want %v", years, want)
		}
	}

	va := d.State[0].Clone()
	va.Year = 2023
	d.SetState(va)
	flat := State{Name: "FLAT", Year: 2025, Brackets: []Bracket{{0, 0.05}}}
	d.SetState(flat)
	flat.Year = 2024
	d.SetState(flat)
	var keys []string
	for _, s := range d.State {
		keys = append(keys, s.Name+string(rune('0'+s.Year%10)))
	}
	if got := len(d.State); got != 6 {
		t.Fatalf("%d state tables (%v), want 6", got, keys)
	}
	if d.State[0].Year != 2023 || d.State[1].Year != 2024 || d.State[3].Name != "VA" || d.State[3].Year != 2026 || d.State[4].Year != 2024 || d.State[5].Year != 2025 {
		t.Errorf("state order %v", keys)
	}
	edited := d.State[0].Clone()
	edited.Exemption = 1
	d.SetState(edited)
	if len(d.State) != 6 || d.State[0].Exemption != 1 {
		t.Error("setting an existing table did not replace it")
	}
}

func TestValidate(t *testing.T) {
	good := Defaults()
	cases := []struct {
		name                 string
		edit                 func(f *Federal, s *State)
		federalBad, stateBad bool
	}{
		{"year", func(f *Federal, s *State) { f.Year = 0; s.Year = 0 }, true, true},
		{"first bracket", func(f *Federal, s *State) { s.Brackets[0].Over = 5 }, false, true},
		{"bracket order", func(f *Federal, s *State) { s.Brackets[2].Over = 100 }, false, true},
		{"rate", func(f *Federal, s *State) { s.Brackets[1].Rate = 1.5 }, false, true},
		{"no brackets", func(f *Federal, s *State) { s.Brackets = nil }, false, true},
		{"missing status", func(f *Federal, s *State) { delete(f.Status, Single) }, true, false},
		{"negative", func(f *Federal, s *State) { f.Limit401k = -1; s.Exemption = -1 }, true, true},
		{"state name", func(f *Federal, s *State) { s.Name = " " }, false, true},
		{"NaN", func(f *Federal, s *State) { f.ChildCredit = math.NaN(); s.Cap529 = math.NaN() }, true, true},
	}
	for _, tc := range cases {
		f, s := good.Federal[1].Clone(), good.State[1].Clone()
		tc.edit(&f, &s)
		if (f.Validate() != nil) != tc.federalBad {
			t.Errorf("%s: federal Validate = %v, want failure %v", tc.name, f.Validate(), tc.federalBad)
		}
		if (s.Validate() != nil) != tc.stateBad {
			t.Errorf("%s: state Validate = %v, want failure %v", tc.name, s.Validate(), tc.stateBad)
		}
	}
}

func TestBracketTop(t *testing.T) {
	tables := Defaults()
	f, ok := tables.FederalYear(2026)
	if !ok {
		t.Skip("no 2026 table")
	}
	rules := f.Status[Single]
	var twelve, twentyTwo float64
	for i, b := range rules.Brackets {
		if b.Rate == 0.12 {
			twelve = rules.Brackets[i+1].Over
		}
		if b.Rate == 0.22 {
			twentyTwo = rules.Brackets[i+1].Over
		}
	}
	top, ded, err := tables.BracketTop(2026, Single, 0, 0.12)
	if err != nil || top != twelve || ded != rules.StandardDeduction {
		t.Errorf("12%%: %v %v %v", top, ded, err)
	}
	later, _, err := tables.BracketTop(2036, Single, 0.03, 0.22)
	if err != nil || later <= twentyTwo {
		t.Errorf("22%% in 2036 is indexed past %v: %v %v", twentyTwo, later, err)
	}
	if top, _, err := tables.BracketTop(2026, Single, 0, 0.37); err != nil || !math.IsInf(top, 1) {
		t.Errorf("the last bracket has no top: %v %v", top, err)
	}
	if _, _, err := tables.BracketTop(2026, Single, 0, 0.5); err == nil {
		t.Error("a missing rate was accepted")
	}
	if !tables.HasBracketRate(0.22) || tables.HasBracketRate(0.23) {
		t.Error("HasBracketRate")
	}
}
