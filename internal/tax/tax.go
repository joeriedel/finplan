// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package tax holds income tax and payroll tax rules as data, the shipped defaults for them, and a pure function that computes a household's tax for a year from them. It depends on the standard library only.
//
// The tables are plain data so that they can be stored and edited: a Federal table is one year of federal rules, a State table one year of one state's income tax in a single generic shape (brackets on taxable income, a standard deduction by filing status, exemptions per filer and per dependent, and an optional 529 deduction cap). What does not fit that shape is not modelled.
package tax

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// FilingStatus is how a household files.
type FilingStatus string

// The filing statuses.
const (
	MarriedJoint    FilingStatus = "mfj"
	Single          FilingStatus = "single"
	HeadOfHousehold FilingStatus = "hoh"
	MarriedSeparate FilingStatus = "mfs"
)

// FilingStatuses lists the filing statuses, the default first.
var FilingStatuses = []FilingStatus{MarriedJoint, Single, HeadOfHousehold, MarriedSeparate}

// Valid reports whether s is one of the filing statuses.
func (s FilingStatus) Valid() bool {
	for _, v := range FilingStatuses {
		if s == v {
			return true
		}
	}
	return false
}

// Filers is the number of people on the return who each get an exemption: two for a joint return, one otherwise.
func (s FilingStatus) Filers() int {
	if s == MarriedJoint {
		return 2
	}
	return 1
}

// Bracket is the rate on the part of taxable income above Over, up to the next bracket's Over. The first bracket starts at 0 and the last has no top.
type Bracket struct {
	Over float64 `json:"over"`
	Rate float64 `json:"rate"`
}

// FedStatus is the federal rules that depend on the filing status.
type FedStatus struct {
	Brackets          []Bracket `json:"brackets"`
	StandardDeduction float64   `json:"standard_deduction"`
	// ChildCreditPhaseOut is the income above which the child and dependent credits shrink by 50 for each 1,000 (or part) over. It is set by statute and not indexed.
	ChildCreditPhaseOut float64 `json:"child_credit_phase_out"`
	// AdditionalMedicareThreshold and NIITThreshold are the wages and the income above which the additional Medicare tax and the net investment income tax apply. Both are set by statute and not indexed.
	AdditionalMedicareThreshold float64 `json:"additional_medicare_threshold"`
	NIITThreshold               float64 `json:"niit_threshold"`
	// LongTermGains15Over and LongTermGains20Over are the taxable income above which long-term capital gains are taxed at 15% and at 20%; below the first they are taxed at 0%. They are indexed like the brackets. A table that sets neither borrows the shipped values.
	LongTermGains15Over float64 `json:"long_term_gains_15_over"`
	LongTermGains20Over float64 `json:"long_term_gains_20_over"`
	// BenefitBase and BenefitAdjusted are the two provisional income thresholds of the taxation of Social Security benefits (26 U.S.C. 86): up to half of the benefits above the base and 85% above the adjusted threshold are taxable. Both are set by statute and not indexed. A table that sets neither borrows the shipped values.
	BenefitBase     float64 `json:"benefit_base"`
	BenefitAdjusted float64 `json:"benefit_adjusted"`
}

// Federal is one year of federal income tax and payroll tax rules.
type Federal struct {
	Year   int                        `json:"year"`
	Status map[FilingStatus]FedStatus `json:"status"`

	// ChildCredit is the credit per child under 17; OtherDependentCredit the credit per other dependent. Both are subject to the phase-out.
	ChildCredit          float64 `json:"child_credit"`
	OtherDependentCredit float64 `json:"other_dependent_credit"`
	// PenaltyRate is the additional tax on a withdrawal before age 59 and a half.
	PenaltyRate float64 `json:"early_withdrawal_penalty_rate"`

	SocialSecurityRate     float64 `json:"social_security_rate"`
	SocialSecurityWageBase float64 `json:"social_security_wage_base"`
	MedicareRate           float64 `json:"medicare_rate"`
	AdditionalMedicareRate float64 `json:"additional_medicare_rate"`
	NIITRate               float64 `json:"niit_rate"`

	// The limits on pre-tax contributions: the 401(k) and TSP elective deferral, an IRA, and an HSA with family coverage.
	Limit401k float64 `json:"limit_401k"`
	LimitIRA  float64 `json:"limit_ira"`
	LimitHSA  float64 `json:"limit_hsa"`
}

// State is one year of one state's income tax.
type State struct {
	// Name is the key households name, such as "VA".
	Name string `json:"name"`
	Year int    `json:"year"`
	// Brackets are on the state's taxable income: federal adjusted gross income less the standard deduction, the exemptions and the 529 deduction.
	Brackets          []Bracket                `json:"brackets"`
	StandardDeduction map[FilingStatus]float64 `json:"standard_deduction"`
	// Exemption is allowed for each filer and DependentExemption for each dependent.
	Exemption          float64 `json:"exemption"`
	DependentExemption float64 `json:"dependent_exemption"`
	// Cap529 is the most of one 529 account's contributions deducted in a year; 0 allows no deduction.
	Cap529 float64 `json:"cap_529"`
}

// Tables is every table in the record: a federal table for some years and any number of state tables.
type Tables struct {
	Federal []Federal `json:"federal"`
	State   []State   `json:"state"`
}

// Clone returns a deep copy.
func (f Federal) Clone() Federal {
	f.Status = maps.Clone(f.Status)
	for k, s := range f.Status {
		s.Brackets = slices.Clone(s.Brackets)
		f.Status[k] = s
	}
	return f
}

// Clone returns a deep copy.
func (s State) Clone() State {
	s.Brackets = slices.Clone(s.Brackets)
	s.StandardDeduction = maps.Clone(s.StandardDeduction)
	return s
}

// Clone returns a deep copy.
func (t Tables) Clone() Tables {
	var c Tables
	for _, f := range t.Federal {
		c.Federal = append(c.Federal, f.Clone())
	}
	for _, s := range t.State {
		c.State = append(c.State, s.Clone())
	}
	return c
}

// FederalYear returns the table of exactly that year.
func (t Tables) FederalYear(year int) (Federal, bool) {
	for _, f := range t.Federal {
		if f.Year == year {
			return f, true
		}
	}
	return Federal{}, false
}

// StateYear returns the table of exactly that state and year.
func (t Tables) StateYear(name string, year int) (State, bool) {
	for _, s := range t.State {
		if s.Name == name && s.Year == year {
			return s, true
		}
	}
	return State{}, false
}

// HasState reports whether there is a table for the state in any year.
func (t Tables) HasState(name string) bool {
	for _, s := range t.State {
		if s.Name == name {
			return true
		}
	}
	return false
}

// SetFederal replaces the table of the same year, or adds it, keeping the tables in year order.
func (t *Tables) SetFederal(f Federal) {
	for i := range t.Federal {
		if t.Federal[i].Year == f.Year {
			t.Federal[i] = f
			return
		}
	}
	t.Federal = append(t.Federal, f)
	for i := len(t.Federal) - 1; i > 0 && t.Federal[i].Year < t.Federal[i-1].Year; i-- {
		t.Federal[i], t.Federal[i-1] = t.Federal[i-1], t.Federal[i]
	}
}

// SetState replaces the table of the same state and year, or adds it among that state's tables in year order, or last for a new state.
func (t *Tables) SetState(s State) {
	pos, first := len(t.State), -1
	for i, e := range t.State {
		if e.Name != s.Name {
			continue
		}
		if e.Year == s.Year {
			t.State[i] = s
			return
		}
		if first < 0 {
			first, pos = i, i
		}
		if e.Year < s.Year {
			pos = i + 1
		}
	}
	t.State = slices.Insert(t.State, pos, s)
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func checkMoney(name string, v float64) error {
	if !finite(v) || v < 0 || v > 1e12 {
		return fmt.Errorf("%s %v must be from 0 to 1e12", name, v)
	}
	return nil
}

func checkRate(name string, v float64) error {
	if !finite(v) || v < 0 || v > 1 {
		return fmt.Errorf("%s %v must be a fraction from 0 to 1", name, v)
	}
	return nil
}

func checkBrackets(bs []Bracket) error {
	if len(bs) == 0 {
		return fmt.Errorf("there must be at least one bracket")
	}
	for i, b := range bs {
		if err := checkMoney(fmt.Sprintf("bracket %d: over", i), b.Over); err != nil {
			return err
		}
		if err := checkRate(fmt.Sprintf("bracket %d: rate", i), b.Rate); err != nil {
			return err
		}
		switch {
		case i == 0 && b.Over != 0:
			return fmt.Errorf("the first bracket must start at 0, not %v", b.Over)
		case i > 0 && b.Over <= bs[i-1].Over:
			return fmt.Errorf("bracket %d: over %v must be above the previous bracket's %v", i, b.Over, bs[i-1].Over)
		}
	}
	return nil
}

func checkYear(y int) error {
	if y < 1900 || y > 2200 {
		return fmt.Errorf("the year %d must be from 1900 to 2200", y)
	}
	return nil
}

// Validate reports why the table cannot be used. Every filing status must have its rules.
func (f Federal) Validate() error {
	if err := checkYear(f.Year); err != nil {
		return err
	}
	for k := range f.Status {
		if !k.Valid() {
			return fmt.Errorf("unknown filing status %q", k)
		}
	}
	for _, k := range FilingStatuses {
		s, ok := f.Status[k]
		if !ok {
			return fmt.Errorf("there are no rules for filing status %q", k)
		}
		if err := checkBrackets(s.Brackets); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		if s.BenefitAdjusted < s.BenefitBase {
			return fmt.Errorf("%s: the adjusted benefit threshold %v must not be below the base threshold %v", k, s.BenefitAdjusted, s.BenefitBase)
		}
		if s.LongTermGains20Over < s.LongTermGains15Over {
			return fmt.Errorf("%s: the 20%% long-term gains threshold %v must not be below the 15%% threshold %v", k, s.LongTermGains20Over, s.LongTermGains15Over)
		}
		for _, v := range []struct {
			name string
			v    float64
		}{{"standard deduction", s.StandardDeduction}, {"child credit phase-out", s.ChildCreditPhaseOut},
			{"additional Medicare threshold", s.AdditionalMedicareThreshold}, {"NIIT threshold", s.NIITThreshold},
			{"long-term gains 15% threshold", s.LongTermGains15Over}, {"long-term gains 20% threshold", s.LongTermGains20Over},
			{"benefit base threshold", s.BenefitBase}, {"benefit adjusted threshold", s.BenefitAdjusted}} {
			if err := checkMoney(v.name, v.v); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
	}
	for _, v := range []struct {
		name string
		v    float64
	}{{"child credit", f.ChildCredit}, {"other dependent credit", f.OtherDependentCredit},
		{"Social Security wage base", f.SocialSecurityWageBase}, {"401(k) limit", f.Limit401k}, {"IRA limit", f.LimitIRA}, {"HSA limit", f.LimitHSA}} {
		if err := checkMoney(v.name, v.v); err != nil {
			return err
		}
	}
	for _, v := range []struct {
		name string
		v    float64
	}{{"early withdrawal penalty rate", f.PenaltyRate}, {"Social Security rate", f.SocialSecurityRate}, {"Medicare rate", f.MedicareRate},
		{"additional Medicare rate", f.AdditionalMedicareRate}, {"NIIT rate", f.NIITRate}} {
		if err := checkRate(v.name, v.v); err != nil {
			return err
		}
	}
	return nil
}

// Validate reports why the table cannot be used.
func (s State) Validate() error {
	if s.Name == "" || s.Name != strings.TrimSpace(s.Name) {
		return fmt.Errorf("a state table needs a name without surrounding spaces")
	}
	if err := checkYear(s.Year); err != nil {
		return err
	}
	if err := checkBrackets(s.Brackets); err != nil {
		return err
	}
	for k, v := range s.StandardDeduction {
		if !k.Valid() {
			return fmt.Errorf("unknown filing status %q", k)
		}
		if err := checkMoney("standard deduction", v); err != nil {
			return err
		}
	}
	for _, v := range []struct {
		name string
		v    float64
	}{{"exemption", s.Exemption}, {"dependent exemption", s.DependentExemption}, {"529 cap", s.Cap529}} {
		if err := checkMoney(v.name, v.v); err != nil {
			return err
		}
	}
	return nil
}

// BracketTop returns the taxable income at the top of the federal bracket with the rate in the year for the filing status, with the year's table indexed by inflation: the Over of the next bracket, or +Inf for the last. It also returns the year's standard deduction, so that top+deduction is the adjusted gross income where the bracket ends. It is an error if the table has no bracket at the rate.
func (t Tables) BracketTop(year int, status FilingStatus, inflation, rate float64) (top, deduction float64, err error) {
	f, err := t.federalFor(year, inflation)
	if err != nil {
		return 0, 0, err
	}
	rules := f.Status[status]
	for i, b := range rules.Brackets {
		if math.Abs(b.Rate-rate) < 1e-9 {
			if i+1 < len(rules.Brackets) {
				return rules.Brackets[i+1].Over, rules.StandardDeduction, nil
			}
			return math.Inf(1), rules.StandardDeduction, nil
		}
	}
	return 0, 0, fmt.Errorf("tax: there is no %g%% federal bracket for %s in %d", rate*100, status, year)
}

// HasBracketRate reports whether any federal table has a bracket at the rate for any filing status.
func (t Tables) HasBracketRate(rate float64) bool {
	for _, f := range t.Federal {
		for _, s := range f.Status {
			for _, b := range s.Brackets {
				if math.Abs(b.Rate-rate) < 1e-9 {
					return true
				}
			}
		}
	}
	return false
}
