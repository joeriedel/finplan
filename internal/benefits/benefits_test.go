// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package benefits

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestFullRetirementAge(t *testing.T) {
	for year, want := range map[int]int{1943: 792, 1954: 792, 1955: 794, 1959: 802, 1960: 804, 1985: 804} {
		if got := FRAMonths(year); got != want {
			t.Errorf("born %d: full retirement age %d months, want %d", year, got, want)
		}
	}
}

func TestOwnBenefitFactors(t *testing.T) {
	cases := []struct {
		birth, claim int
		want         float64
	}{
		{1985, 67, 1},
		{1985, 62, 0.70},
		{1985, 66, 1 - 12*5.0/9/100},
		{1985, 70, 1.24},
		{1950, 62, 0.75},
		{1950, 70, 1.32},
		{1957, 62, 1 - (36*5.0/9+(798-744-36)*5.0/12)/100},
	}
	for _, c := range cases {
		if got := SSOwnFactor(c.birth, c.claim); !near(got, c.want) {
			t.Errorf("born %d claiming at %d: factor %v, want %v", c.birth, c.claim, got, c.want)
		}
	}
}

func TestSpousalFactors(t *testing.T) {
	if got := SSSpousalFactor(1985, 67); got != 1 {
		t.Errorf("spousal factor at full retirement age %v", got)
	}
	if got := SSSpousalFactor(1985, 62); !near(got, 0.65) {
		t.Errorf("spousal factor at 62 %v, want 0.65", got)
	}
	if got := SSSpousalFactor(1985, 70); got != 1 {
		t.Errorf("spousal factor at 70 %v: there are no delayed credits", got)
	}
}

func TestMinimumRetirementAge(t *testing.T) {
	for year, want := range map[int]int{1947: 660, 1948: 662, 1952: 670, 1953: 672, 1964: 672, 1965: 674, 1969: 682, 1970: 684, 1990: 684} {
		if got := MRAMonths(year); got != want {
			t.Errorf("born %d: minimum retirement age %d months, want %d", year, got, want)
		}
	}
}

func TestEligibilityAndReduction(t *testing.T) {
	cases := []struct {
		birth, years, start int
		want                Retirement
		fail                bool
	}{
		{1985, 30, 57, Unreduced, false},
		{1985, 30, 56, 0, true},
		{1985, 20, 60, Unreduced, false},
		{1985, 5, 62, Deferred62, false},
		{1985, 4, 62, 0, true},
		{1985, 15, 57, Reduced, false},
		{1985, 9, 57, 0, true},
		{1966, 30, 56, 0, true},
		{1966, 30, 57, Unreduced, false},
	}
	for _, c := range cases {
		got, err := Eligibility(c.birth, c.years, c.start)
		if got != c.want || (err != nil) != c.fail {
			t.Errorf("born %d, %d years, start %d: %v, %v", c.birth, c.years, c.start, got, err)
		}
	}
	// 15 years of service retiring at the minimum retirement age of 57 is 5 years early: a 25% reduction.
	if got := Annuity(100000, 15, 57, Reduced, model.SurvivorNone); !near(got, 100000*15*0.01*0.75) {
		t.Errorf("reduced annuity %v", got)
	}
	if got := Annuity(100000, 25, 62, Deferred62, model.SurvivorNone); !near(got, 100000*25*0.011) {
		t.Errorf("annuity at 62 with 25 years %v, want 1.1%% a year", got)
	}
	if got := Annuity(100000, 19, 62, Deferred62, model.SurvivorNone); !near(got, 100000*19*0.01) {
		t.Errorf("annuity at 62 with 19 years %v, want 1%% a year", got)
	}
}

func TestSurvivorOptionAndCOLA(t *testing.T) {
	for s, want := range map[model.Survivor]float64{"": 0, model.SurvivorNone: 0, model.SurvivorPartial: 0.05, model.SurvivorFull: 0.10} {
		if got := SurvivorCut(s); got != want {
			t.Errorf("survivor %q cuts %v, want %v", s, got, want)
		}
	}
	for infl, want := range map[float64]float64{-0.01: 0, 0.01: 0.01, 0.02: 0.02, 0.025: 0.02, 0.03: 0.02, 0.04: 0.03} {
		if got := COLA(infl); !near(got, want) {
			t.Errorf("inflation %v: COLA %v, want %v", infl, got, want)
		}
	}
}

func TestPayStartIsTheFirstOfTheMonthAfterTheBirthday(t *testing.T) {
	birth := model.D(1986, time.July, 4)
	if got := PayStart(birth, 62); got != model.D(2048, time.August, 1) {
		t.Errorf("pay starts %v", got)
	}
	if got := PayStart(model.D(1986, time.December, 31), 62); got != model.D(2049, time.January, 1) {
		t.Errorf("pay starts %v", got)
	}
}
