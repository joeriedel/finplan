// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
)

type tMember struct {
	Name              string
	SSBenefit         float64 `json:"ss_benefit"`
	SSClaimAge        int     `json:"ss_claim_age"`
	FERSHigh3         float64 `json:"fers_high3"`
	FERSYears         int     `json:"fers_years"`
	FERSSeparationAge int     `json:"fers_separation_age"`
	FERSStartAge      int     `json:"fers_start_age"`
	FERSSurvivor      string  `json:"fers_survivor"`
}

type tBenefits struct {
	Year    int
	Members []struct {
		Name           string
		SSOwn          float64 `json:"ss_own"`
		SSSpousal      float64 `json:"ss_spousal"`
		FERSAnnuity    float64 `json:"fers_annuity"`
		FERSSupplement float64 `json:"fers_supplement"`
	}
	Received float64 `json:"social_security_received"`
	Taxable  float64 `json:"taxable_social_security"`
}

func benefitsIn(t *testing.T, path string, year string) map[string]struct {
	own, spousal, annuity, supplement float64
} {
	t.Helper()
	var b tBenefits
	cliJSON(t, &b, "--db", path, "household", "benefits", year)
	out := map[string]struct{ own, spousal, annuity, supplement float64 }{}
	for _, m := range b.Members {
		out[m.Name] = struct{ own, spousal, annuity, supplement float64 }{m.SSOwn, m.SSSpousal, m.FERSAnnuity, m.FERSSupplement}
	}
	return out
}

func TestCLIHouseholdShowsTheBenefitInputs(t *testing.T) {
	path := seededFile(t)
	var h struct{ Members []tMember }
	cliJSON(t, &h, "--db", path, "household", "show")
	a, b, child := h.Members[0], h.Members[1], h.Members[2]
	if a.SSBenefit != 3100 || a.SSClaimAge != 67 || a.FERSHigh3 != 0 || a.FERSSurvivor != "none" {
		t.Errorf("Parent A %+v", a)
	}
	if b.SSBenefit != 1200 || b.SSClaimAge != 62 || b.FERSHigh3 != 105000 || b.FERSYears != 30 || b.FERSSeparationAge != 57 || b.FERSStartAge != 57 || b.FERSSurvivor != "partial" {
		t.Errorf("Parent B %+v", b)
	}
	if child.SSBenefit != 0 || child.FERSHigh3 != 0 {
		t.Errorf("a child has benefits: %+v", child)
	}
}

func TestCLIMemberBenefitFlagsAndRefusals(t *testing.T) {
	path := seededFile(t)
	var w struct{ Member tMember }
	cliJSON(t, &w, "--db", path, "household", "member", "add", "Grandpa", "--birth", "1960-05-05", "--role", "earner", "--ss-benefit", "2500", "--ss-claim-age", "68",
		"--fers-high3", "90000", "--fers-years", "35", "--fers-separation-age", "62", "--fers-start-age", "62", "--fers-survivor", "full")
	if m := w.Member; m.SSBenefit != 2500 || m.SSClaimAge != 68 || m.FERSHigh3 != 90000 || m.FERSYears != 35 || m.FERSStartAge != 62 || m.FERSSurvivor != "full" {
		t.Errorf("added %+v", m)
	}
	write(t, path, "", "household", "member", "edit", "Grandpa", "--ss-claim-age", "70", "--fers-survivor", "none")
	cliJSON(t, &w, "--db", path, "household", "member", "edit", "Grandpa", "--ss-benefit", "2600")
	if m := w.Member; m.SSBenefit != 2600 || m.SSClaimAge != 70 || m.FERSSurvivor != "none" || m.FERSHigh3 != 90000 {
		t.Errorf("edited %+v", m)
	}
	refusedWrite(t, path, "", exitFailure, "invalid", "household", "member", "edit", "Child 1", "--ss-benefit", "100", "--ss-claim-age", "67")
	refusedWrite(t, path, "", exitFailure, "invalid", "household", "member", "edit", "Parent A", "--ss-claim-age", "61")
	refusedWrite(t, path, "", exitFailure, "invalid", "household", "member", "edit", "Parent A", "--fers-high3", "80000", "--fers-years", "10", "--fers-separation-age", "40", "--fers-start-age", "40")
	refusedWrite(t, path, "", exitFailure, "invalid", "household", "member", "edit", "Parent A", "--fers-years", "10")
}

// Claiming at 62 pays 70% of the benefit at full retirement age and at 70 pays 124%.
func TestCLIClaimingAge(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "household", "member", "edit", "Parent A", "--ss-claim-age", "62")
	early := benefitsIn(t, path, "2060")["Parent A"].own
	write(t, path, "", "household", "member", "edit", "Parent A", "--ss-claim-age", "67")
	full := benefitsIn(t, path, "2060")["Parent A"].own
	write(t, path, "", "household", "member", "edit", "Parent A", "--ss-claim-age", "70")
	late := benefitsIn(t, path, "2060")["Parent A"].own
	if full == 0 || !closeTo(early/full, 0.70) || !closeTo(late/full, 1.24) {
		t.Errorf("benefits at 62, 67 and 70: %v, %v, %v", early, full, late)
	}
}

// Parent B claims at 62 and gets the spousal benefit, half of Parent A's once Parent A has claimed at 67, in April 2052, when she is 15 months short of her full retirement age: 25/36 of 1% less for each.
func TestCLISpousalBenefit(t *testing.T) {
	path := seededFile(t)
	before := benefitsIn(t, path, "2050")
	if b := before["Parent B"]; b.own <= 0 || b.spousal != 0 || before["Parent A"].own != 0 {
		t.Errorf("before Parent A claims: %+v, %+v", b, before["Parent A"])
	}
	after := benefitsIn(t, path, "2060")
	b, a := after["Parent B"], after["Parent A"]
	if b.own != 0 || a.own == 0 || !closeTo(b.spousal/a.own, 1550*(1-15*25.0/36/100)/3100) {
		t.Errorf("after Parent A claims: spouse %+v, earner %+v", b, a)
	}
	var tb tBenefits
	cliJSON(t, &tb, "--db", path, "household", "benefits", "2060")
	if !closeTo(tb.Received, (a.own+b.spousal)) || tb.Taxable <= 0 || tb.Taxable > 0.85*tb.Received+0.01 {
		t.Errorf("received %v and taxable %v of %v", tb.Received, tb.Taxable, a.own+b.spousal)
	}
}

// Parent B retires at 57 on an unreduced annuity: the supplement runs to 62 and the annuity then rises by the FERS COLA, 2% at the demo's 2.5% inflation.
func TestCLIFERSSupplementAndCOLA(t *testing.T) {
	path := seededFile(t)
	y := func(year string) struct{ own, spousal, annuity, supplement float64 } {
		return benefitsIn(t, path, year)["Parent B"]
	}
	if b := y("2040"); b.annuity != 0 || b.supplement != 0 {
		t.Errorf("before the start: %+v", b)
	}
	if b := y("2045"); b.annuity <= 0 || b.supplement <= 0 {
		t.Errorf("at 59: %+v", b)
	}
	if b := y("2050"); b.annuity <= 0 || b.supplement != 0 {
		t.Errorf("at 64: %+v", b)
	}
	if r := y("2048").annuity / y("2047").annuity; !closeTo(r, 1) {
		t.Errorf("the annuity rose %v in the year of 62", r)
	}
	if r := y("2049").annuity / y("2048").annuity; !closeTo(r, 1.02) {
		t.Errorf("the annuity rose %v after 62, want the 2%% COLA", r)
	}
	if r := y("2050").annuity / y("2049").annuity; !closeTo(r, 1.02) {
		t.Errorf("the annuity rose %v the next year", r)
	}
}

func TestCLISurvivorOptionReducesTheAnnuity(t *testing.T) {
	path := seededFile(t)
	annuity := func(option string) float64 {
		write(t, path, "", "household", "member", "edit", "Parent B", "--fers-survivor", option)
		return benefitsIn(t, path, "2045")["Parent B"].annuity
	}
	none, partial, full := annuity("none"), annuity("partial"), annuity("full")
	if none == 0 || !closeTo(partial/none, 0.95) || !closeTo(full/none, 0.90) {
		t.Errorf("annuity with no, partial and full survivor option: %v, %v, %v", none, partial, full)
	}
}
