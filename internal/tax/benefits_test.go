// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package tax

import (
	"math"
	"testing"
)

func TestTaxableBenefitsFormula(t *testing.T) {
	cases := []struct {
		name            string
		status          FilingStatus
		benefits, other float64
		want            float64
	}{
		{"below the base", MarriedJoint, 30000, 10000, 0},
		{"first tier", MarriedJoint, 30000, 20000, 1500},
		{"second tier", MarriedJoint, 30000, 40000, 15350},
		{"capped at 85%", MarriedJoint, 30000, 100000, 25500},
		{"single first tier", Single, 20000, 20000, 2500},
		{"head of household has the single thresholds", HeadOfHousehold, 20000, 20000, 2500},
		{"married filing separately has no base", MarriedSeparate, 10000, 0, 4250},
	}
	for _, c := range cases {
		r, err := Compute(Defaults(), Input{Year: 2026, Status: c.status, State: VA, OtherIncome: c.other, BenefitIncome: c.benefits})
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(r.TaxableBenefits-c.want) > 1e-6 || math.Abs(r.AGI-(c.other+c.want)) > 1e-6 {
			t.Errorf("%s: taxable benefits %v, AGI %v; want %v and %v", c.name, r.TaxableBenefits, r.AGI, c.want, c.other+c.want)
		}
	}
}

func TestVirginiaDoesNotTaxBenefits(t *testing.T) {
	with, err := Compute(Defaults(), Input{Year: 2026, Status: MarriedJoint, State: VA, OtherIncome: 40000, BenefitIncome: 30000})
	if err != nil {
		t.Fatal(err)
	}
	without, _ := Compute(Defaults(), Input{Year: 2026, Status: MarriedJoint, State: VA, OtherIncome: 40000})
	if with.StateTaxable != without.StateTaxable || with.StateTax != without.StateTax {
		t.Errorf("state taxable %v and tax %v with benefits, %v and %v without", with.StateTaxable, with.StateTax, without.StateTaxable, without.StateTax)
	}
	if with.FederalTaxable <= without.FederalTaxable {
		t.Errorf("federal taxable %v with benefits is not above %v without", with.FederalTaxable, without.FederalTaxable)
	}
}

func TestStoredTableWithoutBenefitThresholdsBorrowsTheShippedOnes(t *testing.T) {
	tables := Defaults()
	for i := range tables.Federal {
		f := tables.Federal[i].Clone()
		for k, s := range f.Status {
			s.BenefitBase, s.BenefitAdjusted = 0, 0
			f.Status[k] = s
		}
		tables.Federal[i] = f
	}
	r, err := Compute(tables, Input{Year: 2026, Status: MarriedJoint, OtherIncome: 20000, BenefitIncome: 30000})
	if err != nil || r.TaxableBenefits != 1500 {
		t.Errorf("taxable benefits %v, %v; want 1500", r.TaxableBenefits, err)
	}
}
