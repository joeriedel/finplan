// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package tax

import (
	"fmt"
	"math"
)

// Earner is one person's year. The Social Security wage base and the 401(k) and IRA limits apply to each person.
type Earner struct {
	Wages float64
	// K401 and IRA are the pre-tax 401(k) or TSP deferrals and the traditional IRA contributions, each limited to the year's limit with the excess post-tax, so it deducts nothing. Roth contributions are not deductible and are not passed.
	K401, IRA float64
	// HSA is the contribution through payroll. The year's family limit applies to the household's total.
	HSA float64
}

// Input is a household's year.
type Input struct {
	Year   int
	Status FilingStatus
	// State is the name of the state table to use; empty for no state tax.
	State string

	// Earners are the household's people with wages or pre-tax contributions.
	Earners []Earner
	// OtherIncome is ordinary income that is not wages and bears no payroll tax.
	OtherIncome float64
	// NetInvestmentIncome is the income the net investment income tax applies to; it is not added to the taxable income here.
	NetInvestmentIncome float64
	// DependentAges are the ages at the end of the year of the dependents; those under 17 earn the child tax credit and the others the credit for other dependents.
	DependentAges []int
	// Contrib529 are the year's contributions to each 529 account, deducted from state income up to the state's cap for each account.
	Contrib529 []float64
	// EarlyWithdrawal is a taxable withdrawal before age 59 and a half: ordinary income and subject to the penalty.
	EarlyWithdrawal float64
	// ShortTermGains and LongTermGains are the year's net realized capital gains on assets held a year or less and more than a year; a loss is negative. Short-term gains are ordinary income and long-term gains are taxed at the preferential rates on top of the ordinary taxable income. A net loss deducts up to 3,000 (1,500 married filing separately) from ordinary income, and what is left is not carried forward. Both kinds of gains count in the adjusted gross income and the net investment income.
	ShortTermGains, LongTermGains float64
	// Inflation is the annual rate that indexes a year past the last federal table.
	Inflation float64
}

// Capital gains rates and the most a net capital loss deducts a year, set by statute (26 U.S.C. 1(h), 1211(b)).
const (
	longTermRate15     = 0.15
	longTermRate20     = 0.20
	capitalLossLimit   = 3000
	capitalLossLimitMS = 1500
)

// Result is the tax of a year.
type Result struct {
	// AGI is the adjusted gross income: wages, other income, the early withdrawal and the net capital gains, less the net capital loss deduction and the pre-tax deductions.
	AGI                float64
	FederalTaxable     float64
	FederalTax         float64 // on the taxable income, before credits
	Credits            float64 // child and other dependent credits used, which cannot take the tax below zero
	NIIT               float64
	Penalty            float64
	StateTaxable       float64
	StateTax           float64
	SocialSecurity     float64
	Medicare           float64
	AdditionalMedicare float64
	// Total is the federal tax after credits, the NIIT, the penalty, the state tax and the payroll taxes.
	Total float64
}

// index scales the table's indexed amounts from its own year to year by (1+inflation) a year. Brackets, the long-term gains thresholds and the standard deduction round to 50, the Social Security wage base to 300, the 401(k) and IRA limits to 500, the HSA limit to 50 and the child credit to 100. The credit for other dependents and the statutory thresholds are not indexed. A year at or before the table's is left as it is.
func (f Federal) index(year int, inflation float64) Federal {
	if year <= f.Year {
		return f
	}
	factor := math.Pow(1+inflation, float64(year-f.Year))
	scale := func(v, unit float64) float64 { return math.Round(v*factor/unit) * unit }
	f = f.Clone()
	f.Year = year
	for k, s := range f.Status {
		for i := range s.Brackets {
			s.Brackets[i].Over = scale(s.Brackets[i].Over, 50)
		}
		s.StandardDeduction = scale(s.StandardDeduction, 50)
		s.LongTermGains15Over = scale(s.LongTermGains15Over, 50)
		s.LongTermGains20Over = scale(s.LongTermGains20Over, 50)
		f.Status[k] = s
	}
	f.ChildCredit = scale(f.ChildCredit, 100)
	f.SocialSecurityWageBase = scale(f.SocialSecurityWageBase, 300)
	f.Limit401k = scale(f.Limit401k, 500)
	f.LimitIRA = scale(f.LimitIRA, 500)
	f.LimitHSA = scale(f.LimitHSA, 50)
	return f
}

// federalFor returns the table in effect in the year: that of the latest table year not after it, or the first table for an earlier year, indexed by inflation for the years between.
func (t Tables) federalFor(year int, inflation float64) (Federal, error) {
	var best *Federal
	for i := range t.Federal {
		f := &t.Federal[i]
		if f.Year <= year && (best == nil || f.Year > best.Year) {
			best = f
		}
	}
	if best == nil {
		for i := range t.Federal {
			if best == nil || t.Federal[i].Year < best.Year {
				best = &t.Federal[i]
			}
		}
	}
	if best == nil {
		return Federal{}, fmt.Errorf("tax: there are no federal tables")
	}
	return best.index(year, inflation), nil
}

// stateFor returns the state's table of the latest year not after the year, or its first table for an earlier year. State amounts are not indexed.
func (t Tables) stateFor(name string, year int) (State, error) {
	var best *State
	for i := range t.State {
		s := &t.State[i]
		switch {
		case s.Name != name:
		case best == nil:
			best = s
		case s.Year <= year && (best.Year > year || s.Year > best.Year):
			best = s
		case s.Year > year && best.Year > year && s.Year < best.Year:
			best = s
		}
	}
	if best == nil {
		return State{}, fmt.Errorf("tax: there is no state table %q", name)
	}
	return *best, nil
}

func bracketTax(bs []Bracket, income float64) float64 {
	var tax float64
	for i, b := range bs {
		if income <= b.Over {
			break
		}
		top := income
		if i+1 < len(bs) && bs[i+1].Over < top {
			top = bs[i+1].Over
		}
		tax += (top - b.Over) * b.Rate
	}
	return tax
}

// netGains nets the year's short-term and long-term gains against each other. It returns the part taxed as ordinary income, the part taxed at the long-term rates and the deduction a net loss takes against other income.
func netGains(short, long float64, status FilingStatus) (ordinary, preferred, deduction float64) {
	switch net := short + long; {
	case net < 0:
		limit := float64(capitalLossLimit)
		if status == MarriedSeparate {
			limit = capitalLossLimitMS
		}
		return 0, 0, math.Min(-net, limit)
	case short < 0:
		return 0, net, 0
	case long < 0:
		return net, 0, 0
	}
	return short, long, 0
}

// GainsInAGI is what the year's short-term and long-term gains add to the adjusted gross income: their net, or for a net loss the part that deducts.
func GainsInAGI(short, long float64, status FilingStatus) float64 {
	ordinary, preferred, deduction := netGains(short, long, status)
	return ordinary + preferred - deduction
}

// longTermTax is the tax on gains stacked on top of the ordinary taxable income base: 0% below over15, 15% up to over20 and 20% above.
func longTermTax(base, gains, over15, over20 float64) float64 {
	top := base + gains
	at15 := math.Max(0, math.Min(top, over20)-math.Max(base, over15))
	at20 := math.Max(0, top-math.Max(base, over20))
	return longTermRate15*at15 + longTermRate20*at20
}

// capped returns v limited to 0..limit.
func capped(v, limit float64) float64 { return math.Max(0, math.Min(v, limit)) }

// Compute returns the tax of the household's year under the tables. Pre-tax 401(k) and TSP deferrals and IRA contributions reduce income but not the wages FICA is computed on; HSA contributions through payroll reduce both. Social Security tax and the 401(k) and IRA limits are applied to each earner; the additional Medicare threshold applies to the household's Medicare wages and the HSA limit to its total contribution. An IRA contribution is deducted in full: the income phase-out of the deduction for a person covered at work is not modelled. Credits are nonrefundable. A state table's brackets, standard deduction and exemptions apply to the federal adjusted gross income; the early withdrawal penalty is federal only. Capital gains are in the adjusted gross income, so the state taxes them as ordinary income.
func Compute(t Tables, in Input) (Result, error) {
	var r Result
	if !in.Status.Valid() {
		return r, fmt.Errorf("tax: unknown filing status %q", in.Status)
	}
	if in.Inflation <= -1 || !finite(in.Inflation) {
		return r, fmt.Errorf("tax: inflation %v must be above -1", in.Inflation)
	}
	f, err := t.federalFor(in.Year, in.Inflation)
	if err != nil {
		return r, err
	}
	rules := f.Status[in.Status]
	var wages, deductions, hsaTotal float64
	for _, e := range in.Earners {
		wages += e.Wages
		hsaTotal += math.Max(0, e.HSA)
		deductions += capped(e.K401, f.Limit401k) + capped(e.IRA, f.LimitIRA)
	}
	hsa := capped(hsaTotal, f.LimitHSA)
	deductions += hsa

	var medicareWages float64
	for _, e := range in.Earners {
		fica := e.Wages
		if hsaTotal > 0 {
			fica -= math.Max(0, e.HSA) * hsa / hsaTotal
		}
		fica = math.Max(0, fica)
		medicareWages += fica
		r.SocialSecurity += f.SocialSecurityRate * math.Min(fica, f.SocialSecurityWageBase)
	}
	r.Medicare = f.MedicareRate * medicareWages
	r.AdditionalMedicare = f.AdditionalMedicareRate * math.Max(0, medicareWages-rules.AdditionalMedicareThreshold)

	ordinaryGains, preferred, lossDeduction := netGains(in.ShortTermGains, in.LongTermGains, in.Status)
	r.AGI = wages + in.OtherIncome + in.EarlyWithdrawal + ordinaryGains + preferred - lossDeduction - deductions
	r.FederalTaxable = math.Max(0, r.AGI-rules.StandardDeduction)
	preferredTaxable := math.Min(preferred, r.FederalTaxable)
	r.FederalTax = bracketTax(rules.Brackets, r.FederalTaxable-preferredTaxable)
	if preferredTaxable > 0 {
		over15, over20 := rules.LongTermGains15Over, rules.LongTermGains20Over
		if over15 == 0 && over20 == 0 {
			// A stored table from before these thresholds existed has none: use the shipped ones for the year.
			if d, err := Defaults().federalFor(in.Year, in.Inflation); err == nil {
				over15, over20 = d.Status[in.Status].LongTermGains15Over, d.Status[in.Status].LongTermGains20Over
			}
		}
		r.FederalTax += longTermTax(r.FederalTaxable-preferredTaxable, preferredTaxable, over15, over20)
	}

	var credit float64
	for _, age := range in.DependentAges {
		if age < 17 {
			credit += f.ChildCredit
		} else {
			credit += f.OtherDependentCredit
		}
	}
	if over := r.AGI - rules.ChildCreditPhaseOut; over > 0 {
		credit -= 50 * math.Ceil(over/1000)
	}
	r.Credits = capped(credit, r.FederalTax)

	r.NIIT = f.NIITRate * math.Max(0, math.Min(in.NetInvestmentIncome+ordinaryGains+preferred, r.AGI-rules.NIITThreshold))
	r.Penalty = f.PenaltyRate * math.Max(0, in.EarlyWithdrawal)

	if in.State != "" {
		s, err := t.stateFor(in.State, in.Year)
		if err != nil {
			return r, err
		}
		income := r.AGI - s.StandardDeduction[in.Status] -
			s.Exemption*float64(in.Status.Filers()) - s.DependentExemption*float64(len(in.DependentAges))
		for _, c := range in.Contrib529 {
			income -= capped(c, s.Cap529)
		}
		r.StateTaxable = math.Max(0, income)
		r.StateTax = bracketTax(s.Brackets, r.StateTaxable)
	}

	r.Total = r.FederalTax - r.Credits + r.NIIT + r.Penalty + r.StateTax + r.SocialSecurity + r.Medicare + r.AdditionalMedicare
	return r, nil
}

// Limit401k returns the 401(k) and TSP elective deferral limit of the year: that of the latest table year not after it, indexed by inflation for the years between.
func (t Tables) Limit401k(year int, inflation float64) (float64, error) {
	f, err := t.federalFor(year, inflation)
	return f.Limit401k, err
}
