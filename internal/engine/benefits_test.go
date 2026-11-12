// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// benefitRecord is a household of the members with no inflation, so benefits are the amounts the rules give.
func benefitRecord(t *testing.T, inflation float64, members ...model.HouseholdMember) *model.Record {
	t.Helper()
	rec := contribRecord(t)
	rec.Globals[model.PropInflation] = model.Constant(inflation)
	rec.Household = model.DefaultHousehold()
	rec.Household.Members = members
	return rec
}

func benefitsOf(t *testing.T, rec *model.Record, year int, name string) MemberBenefit {
	t.Helper()
	got, _, err := BenefitsOf(rec, "", year)
	must(t, err)
	for _, b := range got {
		if b.Member == name {
			return b
		}
	}
	return MemberBenefit{Member: name}
}

func member(name string, role model.Role, born int) model.HouseholdMember {
	return model.HouseholdMember{Name: name, Role: role, Birth: d(born, time.June, 15)}
}

func TestClaimingAt62OrAt70ChangesTheBenefitByTheSSAFactors(t *testing.T) {
	early := member("A", model.RoleEarner, 1975)
	early.SSBenefit, early.SSClaimAge = 2000, 62
	late := early
	late.SSClaimAge = 70
	full := early
	full.SSClaimAge = 67
	if got := benefitsOf(t, benefitRecord(t, 0, early), 2040, "A").SSOwn; !near(got, 12*2000*0.70) {
		t.Errorf("claiming at 62 pays %v a year, want 70%% of the full benefit", got)
	}
	if got := benefitsOf(t, benefitRecord(t, 0, full), 2045, "A").SSOwn; !near(got, 12*2000) {
		t.Errorf("claiming at 67 pays %v a year", got)
	}
	if got := benefitsOf(t, benefitRecord(t, 0, late), 2050, "A").SSOwn; !near(got, 12*2000*1.24) {
		t.Errorf("claiming at 70 pays %v a year, want 124%% of the full benefit", got)
	}
	// Nothing is paid before the claim: the first payment is the first of the month after the 62nd birthday.
	if got := benefitsOf(t, benefitRecord(t, 0, early), 2037, "A").SSOwn; !near(got, 6*1400) {
		t.Errorf("the year of the claim pays %v, want six months", got)
	}
}

func TestSocialSecurityIsIndexedByInflation(t *testing.T) {
	m := member("A", model.RoleEarner, 1975)
	m.SSBenefit, m.SSClaimAge = 2000, 67
	rec := benefitRecord(t, 0.03, m)
	got := benefitsOf(t, rec, 2045, "A").SSOwn
	if got < 12*2000*1.03*1.03 {
		t.Errorf("a benefit 19 years out pays %v a year, not indexed", got)
	}
}

func TestSpouseReceivesTheSpousalBenefit(t *testing.T) {
	a := member("A", model.RoleEarner, 1975)
	a.SSBenefit, a.SSClaimAge = 3000, 67
	b := member("B", model.RoleSpouse, 1976)
	b.SSBenefit, b.SSClaimAge = 1000, 67
	rec := benefitRecord(t, 0, a, b)
	// B's claim in July 2043 is the later of the two: half of A's 3,000 is more than B's own 1,000.
	if got := benefitsOf(t, rec, 2050, "B"); !near(got.SSSpousal, 12*1500) || got.SSOwn != 0 {
		t.Errorf("spouse gets %+v, want only the spousal benefit", got)
	}
	if got := benefitsOf(t, rec, 2043, "B"); !near(got.SSSpousal, 6*1500) || got.SSOwn != 0 {
		t.Errorf("spouse gets %+v in the year of the claim, want six months of the spousal benefit", got)
	}
	if got := benefitsOf(t, rec, 2050, "A"); got.SSSpousal != 0 || !near(got.SSOwn, 12*3000) {
		t.Errorf("earner gets %+v", got)
	}

	// A spouse whose own benefit is higher keeps it.
	b.SSBenefit = 2000
	if got := benefitsOf(t, benefitRecord(t, 0, a, b), 2050, "B"); got.SSSpousal != 0 || !near(got.SSOwn, 12*2000) {
		t.Errorf("spouse with the higher own benefit gets %+v", got)
	}
}

func TestSpousalBenefitStartsWhenTheEarnerClaims(t *testing.T) {
	a := member("A", model.RoleEarner, 1975)
	a.SSBenefit, a.SSClaimAge = 3000, 70
	b := member("B", model.RoleSpouse, 1976)
	b.SSBenefit, b.SSClaimAge = 1000, 62
	rec := benefitRecord(t, 0, a, b)
	// B's own benefit is 70% of 1,000; the spousal benefit starts in July 2045, when A claims and B is past her full retirement age, so it is all of 1,500.
	if got := benefitsOf(t, rec, 2040, "B"); !near(got.SSOwn, 12*700) || got.SSSpousal != 0 {
		t.Errorf("before A claims B gets %+v", got)
	}
	if got := benefitsOf(t, rec, 2045, "B"); !near(got.SSOwn, 6*700) || !near(got.SSSpousal, 6*1500) {
		t.Errorf("in the year A claims B gets %+v, want six months of each", got)
	}
	if got := benefitsOf(t, rec, 2046, "B"); got.SSOwn != 0 || !near(got.SSSpousal, 12*1500) {
		t.Errorf("after A claims B gets %+v", got)
	}
}

func fersMember() model.HouseholdMember {
	m := member("A", model.RoleEarner, 1975)
	m.SSBenefit, m.SSClaimAge = 2400, 67
	m.FERSHigh3, m.FERSYears, m.FERSSeparationAge, m.FERSStartAge = 100000, 30, 57, 57
	return m
}

func TestFERSPaysTheSupplementUntil62ThenTheAnnuityWithCOLA(t *testing.T) {
	m := fersMember()
	rec := benefitRecord(t, 0, m)
	// Born June 1975: 57 in June 2032, so pay starts in July 2032, and the supplement ends with the payment of June 2037.
	if got := benefitsOf(t, rec, 2031, "A"); got.FERSAnnuity != 0 || got.FERSSupplement != 0 {
		t.Errorf("before the start %+v", got)
	}
	if got := benefitsOf(t, rec, 2034, "A"); !near(got.FERSAnnuity, 30000) || !near(got.FERSSupplement, 12*2400*30/40) {
		t.Errorf("at 59 %+v, want 30,000 and a supplement of 1,800 a month", got)
	}
	if got := benefitsOf(t, rec, 2037, "A"); !near(got.FERSSupplement, 6*1800) {
		t.Errorf("in the year of 62 the supplement is %v, want six months", got.FERSSupplement)
	}
	if got := benefitsOf(t, rec, 2038, "A"); got.FERSSupplement != 0 || !near(got.FERSAnnuity, 30000) {
		t.Errorf("at 63 with no inflation %+v", got)
	}

	// The annuity is flat until the year after 62 and then rises by the FERS COLA.
	for _, c := range []struct{ inflation, cola float64 }{{0.015, 0.015}, {0.025, 0.02}, {0.04, 0.03}} {
		rec := benefitRecord(t, c.inflation, m)
		before, after, later := benefitsOf(t, rec, 2036, "A").FERSAnnuity, benefitsOf(t, rec, 2038, "A").FERSAnnuity, benefitsOf(t, rec, 2039, "A").FERSAnnuity
		sep := d(2032, time.June, 15)
		high3 := 100000 * model.Constant(c.inflation).Compound(rec.Today, sep)
		if !near(before, high3*30*0.01) {
			t.Errorf("inflation %v: annuity at 61 is %v, want the high-3 indexed to separation, %v", c.inflation, before, high3*30*0.01)
		}
		if math.Abs(after/before-(1+c.cola)) > 1e-9 || math.Abs(later/after-(1+c.cola)) > 1e-9 {
			t.Errorf("inflation %v: annuity %v, %v, %v; want a rise of %v a year from 63", c.inflation, before, after, later, c.cola)
		}
	}
}

func TestSurvivorOptionReducesTheAnnuity(t *testing.T) {
	m := fersMember()
	for survivor, want := range map[model.Survivor]float64{model.SurvivorNone: 30000, model.SurvivorPartial: 28500, model.SurvivorFull: 27000} {
		m.FERSSurvivor = survivor
		if got := benefitsOf(t, benefitRecord(t, 0, m), 2034, "A").FERSAnnuity; !near(got, want) {
			t.Errorf("survivor option %q: annuity %v, want %v", survivor, got, want)
		}
	}
}

func TestReducedFERSRetirementHasNoSupplement(t *testing.T) {
	m := fersMember()
	m.FERSYears = 12
	got := benefitsOf(t, benefitRecord(t, 0, m), 2034, "A")
	// At 57, five years before 62, the annuity is 5% less for each year.
	if !near(got.FERSAnnuity, 100000*12*0.01*0.75) || got.FERSSupplement != 0 {
		t.Errorf("reduced retirement pays %+v", got)
	}
	m.FERSYears = 5
	rec := benefitRecord(t, 0, m)
	if got := benefitsOf(t, rec, 2034, "A"); got.FERSAnnuity != 0 {
		t.Errorf("an annuity the service does not allow pays %v", got.FERSAnnuity)
	}
	if len(noteOf(t, rec)) == 0 {
		t.Error("there is no note saying the annuity is not modelled")
	}
}

func noteOf(t *testing.T, rec *model.Record) []string {
	t.Helper()
	ty, err := TaxYearOf(rec, "", 2034)
	must(t, err)
	return ty.Notes
}

func TestBenefitsAreTaxedAndBookedUnderTheirOwnCauses(t *testing.T) {
	a := member("A", model.RoleEarner, 1975)
	a.SSBenefit, a.SSClaimAge = 3000, 67
	a.FERSHigh3, a.FERSYears, a.FERSSeparationAge, a.FERSStartAge = 100000, 30, 57, 57
	rec := benefitRecord(t, 0, a)
	ty, err := TaxYearOf(rec, "", 2045)
	must(t, err)
	if !near(ty.Benefits, 12*3000) || !near(ty.Ordinary, 30000) {
		t.Errorf("benefits %v and ordinary income %v; want 36,000 and the 30,000 annuity", ty.Benefits, ty.Ordinary)
	}
	// Provisional income 30,000 + 18,000 is 48,000 for a joint return: 85% of the excess over 44,000 and half of the 12,000 between the thresholds.
	if want := 6000 + 0.85*4000; !near(ty.Result.TaxableBenefits, want) {
		t.Errorf("taxable benefits %v, want %v", ty.Result.TaxableBenefits, want)
	}
	if want := 30000 + ty.Result.TaxableBenefits; !near(ty.Result.AGI, want) {
		t.Errorf("AGI %v, want %v", ty.Result.AGI, want)
	}
	if ty.Input.OtherIncome != ty.Ordinary || ty.Input.BenefitIncome != ty.Benefits {
		t.Errorf("tax input %+v", ty.Input)
	}

	bd, err := Breakdown(rec, "", model.DefaultAssetID("chk"), d(2044, time.December, 31), d(2045, time.December, 31))
	must(t, err)
	if l := line(bd, CauseSocialSecurity); !near(l.Projected, 36000) {
		t.Errorf("Social Security line %+v", l)
	}
	if l := line(bd, CauseFERS); !near(l.Projected, 30000) {
		t.Errorf("FERS line %+v", l)
	}
	if l := line(bd, CauseIncome); l.Projected != 0 {
		t.Errorf("income line %+v: the benefits are not plain income", l)
	}
}

func TestSpousalReductionUsesTheAgeWhenItStarts(t *testing.T) {
	a := model.HouseholdMember{Name: "A", Role: model.RoleEarner, Birth: d(1985, time.March, 12), SSBenefit: 3100, SSClaimAge: 67}
	b := model.HouseholdMember{Name: "B", Role: model.RoleSpouse, Birth: d(1986, time.July, 4), SSBenefit: 1200, SSClaimAge: 62}
	// The spousal benefit starts in April 2052, when B is 65 years 9 months old, 15 months before her full retirement age.
	rec := benefitRecord(t, 0, a, b)
	if got := benefitsOf(t, rec, 2060, "B"); !near(got.SSSpousal, 12*1550*(1-15*25.0/36/100)) || got.SSOwn != 0 {
		t.Errorf("spouse gets %+v, want the spousal benefit reduced for 15 months", got)
	}
}

func TestSupplementIsPaidOnlyOnImmediateRetirement(t *testing.T) {
	m := fersMember()
	m.FERSYears, m.FERSSeparationAge, m.FERSStartAge = 20, 50, 60
	got := benefitsOf(t, benefitRecord(t, 0, m), 2036, "A")
	if got.FERSAnnuity <= 0 || got.FERSSupplement != 0 {
		t.Errorf("a deferred retirement pays %+v, want no supplement", got)
	}
}

func TestSupplementIsIndexedOnceToItsStart(t *testing.T) {
	m := fersMember()
	rec := benefitRecord(t, 0.03, m)
	// Pay starts in July 2032; the supplement is the same in every year it is paid.
	want := 12 * 2400 * 30 / 40 * model.Constant(0.03).Compound(rec.Today, d(2032, time.July, 1))
	for _, year := range []int{2033, 2036} {
		if got := benefitsOf(t, rec, year, "A").FERSSupplement; !near(got, want) {
			t.Errorf("supplement in %d is %v, want %v", year, got, want)
		}
	}
}

func TestBenefitFollowsTheCheckingAccountOpenToday(t *testing.T) {
	m := member("A", model.RoleEarner, 1950)
	m.SSBenefit, m.SSClaimAge = 2000, 67
	rec := benefitRecord(t, 0, m)
	other := &model.Account{ID: "other", Type: model.TypeChecking, Tax: model.Taxable}
	other.AddSnapshot(d(2026, time.January, 1), 0)
	must(t, rec.AddAccount(other))
	// The benefit started in July 2017, while the first checking account was open; it follows the account open today.
	closed := d(2020, time.June, 30)
	rec.Account("chk").Closed = &closed
	rec.Household.Checking = ""
	items := memberBenefits(rec, model.Constant(0), d(2030, time.January, 1), &Diagnostics{})
	if len(items) != 1 || items[0].item.Account != "other" {
		t.Errorf("benefit items %+v, want one paid into the open account", items)
	}
}
