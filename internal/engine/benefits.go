// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"time"

	"github.com/joeriedel/finplan/internal/benefits"
	"github.com/joeriedel/finplan/internal/model"
)

// Social Security and FERS income is synthesized from the household members: monthly income items paid into the reconciliation account, on the first of each month from the month after the member reaches the age the benefit starts at. They exist for the query only and are never written to the record. Social Security is indexed by inflation from the record's today; the FERS supplement is indexed to its start and then fixed, the FERS annuity is indexed to the separation date, then fixed until the year after the member is 62 and annuitant, and rises by the FERS cost-of-living adjustment from then on (see the benefits package).

// benefitKind says which benefit an item pays.
type benefitKind string

// Benefit kinds.
const (
	benefitSSOwn        benefitKind = "ss-own"
	benefitSSSpousal    benefitKind = "ss-spousal"
	benefitAnnuity      benefitKind = "fers-annuity"
	benefitSupplement   benefitKind = "fers-supplement"
	benefitItemIDPrefix             = "benefit:"
)

func (k benefitKind) socialSecurity() bool { return k == benefitSSOwn || k == benefitSSSpousal }

func (k benefitKind) cause() Cause {
	if k.socialSecurity() {
		return CauseSocialSecurity
	}
	return CauseFERS
}

// benefit is one synthesized income item of a member.
type benefit struct {
	member string
	kind   benefitKind
	item   model.BudgetItem
}

// memberBenefits returns the benefit items of the household's members under the inflation schedule, noting in diag those it cannot model. Items are generated to the horizon.
func memberBenefits(rec *model.Record, infl model.Schedule, horizon model.Date, diag *Diagnostics) []benefit {
	var out []benefit
	var earner *model.HouseholdMember
	for i, m := range rec.Household.Members {
		if m.Role == model.RoleEarner && m.SSBenefit > 0 && m.SSClaimAge > 0 && (earner == nil || m.SSBenefit > earner.SSBenefit) {
			earner = &rec.Household.Members[i]
		}
	}
	add := func(m model.HouseholdMember, kind benefitKind, from model.Date, end *model.Date, amount model.Schedule, indexed bool) {
		acct := ReconciliationAccount(rec, max(from, rec.Today))
		if acct == "" {
			diag.Notes = append(diag.Notes, fmt.Sprintf("there is no open checking account on %s: the %s of %s is not paid", from, kind, m.Name))
			return
		}
		out = append(out, benefit{m.Name, kind, model.BudgetItem{
			ID: benefitItemIDPrefix + m.Name + ":" + string(kind), Name: m.Name + " " + string(kind), Kind: model.ItemIncome,
			Amount: amount, Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Start: &from, End: end, Indexed: indexed,
			Account: acct, IncomeType: model.IncomeOrdinary,
		}})
	}
	for _, m := range rec.Household.Members {
		if m.Role == model.RoleDependent {
			continue
		}
		by, _, _ := m.Birth.YMD()
		if m.SSClaimAge < benefits.MinClaimAge || m.SSClaimAge > benefits.MaxClaimAge {
			// No claim age, or one the record should not hold: no Social Security.
			if m.SSBenefit > 0 || m.SSClaimAge != 0 {
				diag.Notes = append(diag.Notes, fmt.Sprintf("%s: the Social Security claim age %d must be from 62 to 70", m.Name, m.SSClaimAge))
			}
		} else {
			own := m.SSBenefit * benefits.SSOwnFactor(by, m.SSClaimAge)
			ownStart := benefits.PayStart(m.Birth, m.SSClaimAge)
			var ownEnd *model.Date
			if m.Role == model.RoleSpouse && earner != nil && earner.Name != m.Name {
				spousalStart := max(ownStart, benefits.PayStart(earner.Birth, earner.SSClaimAge))
				// A claim at an exact age is reduced as at that age; a spousal benefit that waits for the earner is reduced by the age it starts at.
				ageMonths := m.SSClaimAge * 12
				if spousalStart > ownStart {
					ageMonths = benefits.AgeMonths(m.Birth, spousalStart)
				}
				spousal := earner.SSBenefit / 2 * benefits.SSSpousalFactor(by, ageMonths)
				if spousal > own {
					add(m, benefitSSSpousal, spousalStart, nil, model.Constant(spousal), true)
					if spousalStart > ownStart {
						ownEnd = model.Ptr(spousalStart.Add(-1))
					} else {
						own = 0
					}
				}
			}
			if own > 0 {
				add(m, benefitSSOwn, ownStart, ownEnd, model.Constant(own), true)
			}
		}
		if !m.HasFERS() {
			continue
		}
		ground, err := benefits.Eligibility(by, m.FERSYears, m.FERSStartAge)
		if err != nil {
			diag.Notes = append(diag.Notes, fmt.Sprintf("%s: %v", m.Name, err))
			continue
		}
		start := benefits.PayStart(m.Birth, m.FERSStartAge)
		high3 := m.FERSHigh3 * infl.Compound(rec.Today, benefits.AgeDate(m.Birth, m.FERSSeparationAge))
		monthly := benefits.Annuity(high3, m.FERSYears, m.FERSStartAge, ground, m.FERSSurvivor) / 12
		sixtyTwo := benefits.AgeDate(m.Birth, 62)
		startYear, _, _ := start.YMD()
		cola, _, _ := sixtyTwo.YMD()
		cola = max(cola, startYear)
		segs := []model.Segment{{From: start, Value: monthly}}
		last, _, _ := horizon.YMD()
		for y := cola + 1; y <= last; y++ {
			jan := model.D(y, time.January, 1)
			rate, _ := infl.At(jan)
			monthly *= 1 + benefits.COLA(rate)
			segs = append(segs, model.Segment{From: jan, Value: monthly})
		}
		add(m, benefitAnnuity, start, nil, model.MustSchedule(segs...), false)
		if ground == benefits.Unreduced && m.FERSStartAge < 62 && m.FERSSeparationAge == m.FERSStartAge && m.SSBenefit > 0 {
			y, mo, _ := sixtyTwo.YMD()
			supplement := benefits.Supplement(m.SSBenefit, m.FERSYears) * infl.Compound(rec.Today, start)
			add(m, benefitSupplement, start, model.Ptr(model.D(y, mo, 1)), model.Constant(supplement), false)
		}
	}
	return out
}

// incomeCause is the cause an income item is booked under: that of the benefit it pays, else income.
func (h *householdTax) incomeCause(id string) Cause {
	if k, ok := h.benefits[id]; ok {
		return k.cause()
	}
	return CauseIncome
}

// MemberBenefit is what a member receives in a year from each benefit.
type MemberBenefit struct {
	Member                                        string
	SSOwn, SSSpousal, FERSAnnuity, FERSSupplement float64
}

// BenefitsOf returns, per household member with any benefit, what each benefit pays in the calendar year under the projection ("" for reality), and the household's tax for the year.
func BenefitsOf(rec *model.Record, projection string, year int) ([]MemberBenefit, *TaxYear, error) {
	r, err := taxRun(rec, projection, year)
	if err != nil {
		return nil, nil, err
	}
	var out []MemberBenefit
	for _, b := range r.benefits {
		i := len(out) - 1
		if i < 0 || out[i].Member != b.member {
			out = append(out, MemberBenefit{Member: b.member})
			i++
		}
		sum := 0.0
		for _, d := range r.tax.dates(b.item, year) {
			if amt, ok := amountOn(r.rec, b.item, d, r.tax.amounts, r.tax.growths, r.tax.infl); ok {
				sum += amt
			}
		}
		switch b.kind {
		case benefitSSOwn:
			out[i].SSOwn = sum
		case benefitSSSpousal:
			out[i].SSSpousal = sum
		case benefitAnnuity:
			out[i].FERSAnnuity = sum
		case benefitSupplement:
			out[i].FERSSupplement = sum
		}
	}
	t := r.tax.result(year)
	t.Diagnostics = r.diag
	return out, t, nil
}
