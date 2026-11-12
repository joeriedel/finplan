// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package benefits holds the Social Security and FERS rules as pure functions of ages and amounts. It knows nothing of the record or the engine.
package benefits

import (
	"fmt"

	"github.com/joeriedel/finplan/internal/model"
)

// Social Security claiming ages and the months of reduction and credit, set by statute (42 U.S.C. 402, 415).
const (
	MinClaimAge = 62
	MaxClaimAge = 70
)

// FRAMonths is the full retirement age in months of someone born in the year: 66 for 1954 and earlier, 2 more months for each year from 1955 to 1959 and 67 from 1960.
func FRAMonths(birthYear int) int {
	switch {
	case birthYear <= 1954:
		return 66 * 12
	case birthYear >= 1960:
		return 67 * 12
	}
	return 66*12 + 2*(birthYear-1954)
}

// SSOwnFactor is the share of the full-retirement-age benefit paid on a claim at the age: 5/9 of 1% less for each month before the full retirement age up to 36 and 5/12 of 1% for each month beyond, and 2/3 of 1% more for each month after it up to age 70.
func SSOwnFactor(birthYear, claimAge int) float64 {
	diff := claimAge*12 - FRAMonths(birthYear)
	if diff >= 0 {
		return 1 + float64(min(diff, MaxClaimAge*12-FRAMonths(birthYear)))*2.0/3/100
	}
	early := float64(-diff)
	return 1 - (min(early, 36)*5.0/9+max(early-36, 0)*5.0/12)/100
}

// SSSpousalFactor is the share of the spousal benefit (half the other member's full-retirement-age benefit) paid to someone born in the year who is ageMonths old when it starts: 25/36 of 1% less for each month before the full retirement age up to 36 and 5/12 of 1% for each month beyond. A spousal benefit earns no delayed credits.
func SSSpousalFactor(birthYear, ageMonths int) float64 {
	early := float64(max(FRAMonths(birthYear)-ageMonths, 0))
	return 1 - (min(early, 36)*25.0/36+max(early-36, 0)*5.0/12)/100
}

// AgeMonths is the number of calendar months from the month of birth to the month of the date.
func AgeMonths(birth, on model.Date) int {
	by, bm, _ := birth.YMD()
	y, m, _ := on.YMD()
	return (y-by)*12 + int(m) - int(bm)
}

// AgeDate is the date the person born on birth reaches the age.
func AgeDate(birth model.Date, age int) model.Date {
	y, m, d := birth.YMD()
	return model.D(y+age, m, d)
}

// PayStart is the first of the month after the person born on birth reaches the age: where a benefit for that age begins.
func PayStart(birth model.Date, age int) model.Date {
	y, m, _ := AgeDate(birth, age).YMD()
	return model.D(y, m+1, 1)
}

// MRAMonths is the minimum retirement age in months of someone born in the year (OPM): 55 for 1947 and earlier, 2 more months for each year to 1952, 56 from 1953 to 1964, 2 more months for each year to 1969 and 57 from 1970.
func MRAMonths(birthYear int) int {
	switch {
	case birthYear <= 1947:
		return 55 * 12
	case birthYear <= 1952:
		return 55*12 + 2*(birthYear-1947)
	case birthYear <= 1964:
		return 56 * 12
	case birthYear <= 1969:
		return 56*12 + 2*(birthYear-1964)
	}
	return 57 * 12
}

// Retirement says on what ground a FERS annuity starts.
type Retirement int

// Retirement grounds. Unreduced is the minimum retirement age with 30 years or age 60 with 20, which also pay the supplement; Deferred62 is age 62 with 5 years, unreduced without a supplement since the member is 62; Reduced is the minimum retirement age with 10 to 29 years starting before 62.
const (
	Unreduced Retirement = iota + 1
	Deferred62
	Reduced
)

// Eligibility returns the ground on which a member born in the year with the years of service can start the annuity at the age, or why the member cannot.
func Eligibility(birthYear, years, startAge int) (Retirement, error) {
	mra := MRAMonths(birthYear)
	atMRA := startAge*12 >= mra
	switch {
	case atMRA && years >= 30, startAge >= 60 && years >= 20:
		return Unreduced, nil
	case startAge >= 62 && years >= 5:
		return Deferred62, nil
	case atMRA && years >= 10:
		return Reduced, nil
	}
	return 0, fmt.Errorf("a FERS annuity starting at %d with %d years of service needs the minimum retirement age of %d years %d months with 10 years, 60 with 20 years, or 62 with 5 years",
		startAge, years, mra/12, mra%12)
}

// Rate is the share of the high-3 salary paid for each year of service: 1.1% for a start at 62 or later with 20 years or more, else 1%.
func Rate(startAge, years int) float64 {
	if startAge >= 62 && years >= 20 {
		return 0.011
	}
	return 0.01
}

// Reduction is the share of the annuity a reduced retirement takes off: 5% for each year the start is before 62.
func Reduction(startAge int) float64 { return 0.05 * float64(max(62-startAge, 0)) }

// SurvivorCut is the share of the annuity the survivor option takes.
func SurvivorCut(s model.Survivor) float64 {
	switch s {
	case model.SurvivorPartial:
		return 0.05
	case model.SurvivorFull:
		return 0.10
	}
	return 0
}

// COLA is the FERS cost-of-living adjustment for a year of the inflation: all of it up to 2%, 2% from 2% to 3% and 1 point less above 3%. It is never negative.
func COLA(inflation float64) float64 {
	switch {
	case inflation <= 0:
		return 0
	case inflation <= 0.02:
		return inflation
	case inflation <= 0.03:
		return 0.02
	}
	return inflation - 0.01
}

// Annuity is the annual FERS annuity at the start, before cost-of-living adjustments, on the high-3 salary at separation.
func Annuity(high3 float64, years, startAge int, ground Retirement, survivor model.Survivor) float64 {
	a := high3 * float64(years) * Rate(startAge, years)
	if ground == Reduced {
		a *= 1 - Reduction(startAge)
	}
	return a * (1 - SurvivorCut(survivor))
}

// Supplement is the monthly FERS supplement of a member whose full-retirement-age Social Security benefit is ssBenefit: that benefit times the years of service over 40.
func Supplement(ssBenefit float64, years int) float64 {
	return ssBenefit * float64(years) / 40
}
