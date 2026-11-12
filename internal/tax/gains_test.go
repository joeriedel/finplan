// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package tax

import "testing"

func gainsInput(other, short, long float64) Input {
	return Input{Year: 2025, Status: MarriedJoint, OtherIncome: other, ShortTermGains: short, LongTermGains: long}
}

func TestLongTermGainsStackOnOrdinaryIncome(t *testing.T) {
	tables := Defaults()
	// Taxable income 28,500 after the 31,500 deduction: the 50,000 of gains ends at 78,500, below the 96,700 where 15% starts.
	r, err := Compute(tables, gainsInput(60000, 0, 50000))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "federal tax under the 15% threshold", r.FederalTax, 2943)
	near(t, "AGI", r.AGI, 110000)
	// Taxable income 68,500 plus 50,000 of gains ends at 118,500: 21,800 of the gains are taxed at 15%.
	r, err = Compute(tables, gainsInput(100000, 0, 50000))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "federal tax across the 15% threshold", r.FederalTax, 7743+0.15*21800)
	// Ordinary income past the 20% threshold: all the gains are taxed at 20%.
	r, err = Compute(tables, gainsInput(700000, 0, 10000))
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := Compute(tables, gainsInput(700000, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "gains above the 20% threshold", r.FederalTax-ordinary.FederalTax, 0.2*10000)
}

func TestShortTermGainsAreOrdinaryIncome(t *testing.T) {
	tables := Defaults()
	asGains, err := Compute(tables, gainsInput(60000, 10000, 0))
	if err != nil {
		t.Fatal(err)
	}
	asIncome, err := Compute(tables, gainsInput(70000, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "total", asGains.Total, asIncome.Total)
	near(t, "AGI", asGains.AGI, asIncome.AGI)
}

func TestGainsCountForInvestmentIncomeTax(t *testing.T) {
	tables := Defaults()
	with, err := Compute(tables, gainsInput(240000, 0, 40000))
	if err != nil {
		t.Fatal(err)
	}
	// AGI 280,000 is 30,000 over the 250,000 threshold, less than the 40,000 of gains.
	near(t, "NIIT", with.NIIT, 0.038*30000)
}

func TestGainsAndLossesNet(t *testing.T) {
	tables := Defaults()
	loss, err := Compute(tables, gainsInput(60000, 0, -5000))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "AGI with a net loss deducts 3,000", loss.AGI, 57000)
	offset, err := Compute(tables, gainsInput(60000, -2000, 5000))
	if err != nil {
		t.Fatal(err)
	}
	same, err := Compute(tables, gainsInput(60000, 0, 3000))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "a short-term loss offsets long-term gains", offset.Total, same.Total)
	shortNet, err := Compute(tables, gainsInput(60000, 8000, -3000))
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := Compute(tables, gainsInput(65000, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "a long-term loss offsets short-term gains", shortNet.Total, ordinary.Total)
	separate := gainsInput(60000, 0, -5000)
	separate.Status = MarriedSeparate
	r, err := Compute(tables, separate)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "married filing separately deducts 1,500", r.AGI, 58500)
}

func TestStateTaxesGainsAsOrdinaryIncome(t *testing.T) {
	tables := Defaults()
	a := gainsInput(60000, 0, 20000)
	a.State = VA
	b := gainsInput(80000, 0, 0)
	b.State = VA
	ra, err := Compute(tables, a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := Compute(tables, b)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "state tax", ra.StateTax, rb.StateTax)
}

func TestLongTermThresholdsAreIndexedAndBorrowed(t *testing.T) {
	tables := Defaults()
	f, err := tables.federalFor(2035, 0.03)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := tables.FederalYear(2026)
	if got := f.Status[Single].LongTermGains15Over; got <= base.Status[Single].LongTermGains15Over {
		t.Errorf("2035 15%% threshold %v is not above 2026's %v", got, base.Status[Single].LongTermGains15Over)
	}
	// A stored table from before the thresholds existed borrows the shipped ones.
	old := Defaults()
	for i := range old.Federal {
		for k, s := range old.Federal[i].Status {
			s.LongTermGains15Over, s.LongTermGains20Over = 0, 0
			old.Federal[i].Status[k] = s
		}
	}
	want, err := Compute(tables, gainsInput(100000, 0, 50000))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Compute(old, gainsInput(100000, 0, 50000))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "federal tax with borrowed thresholds", got.FederalTax, want.FederalTax)
}
