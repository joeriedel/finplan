// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"slices"

	"github.com/joeriedel/finplan/internal/tax"
)

// Role is what a household member is to the tax return.
type Role string

// Household roles.
const (
	RoleEarner    Role = "earner"
	RoleSpouse    Role = "spouse"
	RoleDependent Role = "dependent"
)

// Roles lists the household roles.
var Roles = []Role{RoleEarner, RoleSpouse, RoleDependent}

// Valid reports whether r is one of the roles.
func (r Role) Valid() bool { return slices.Contains(Roles, r) }

// Survivor is the survivor option of a FERS annuity, which reduces the annuity to pay for the benefit a surviving spouse would get. The zero value is none.
type Survivor string

// Survivor options.
const (
	SurvivorNone    Survivor = "none"
	SurvivorPartial Survivor = "partial"
	SurvivorFull    Survivor = "full"
)

// Survivors lists the survivor options.
var Survivors = []Survivor{SurvivorNone, SurvivorPartial, SurvivorFull}

// Valid reports whether s is one of the options or empty.
func (s Survivor) Valid() bool { return s == "" || slices.Contains(Survivors, s) }

// HouseholdMember is a person on the household's tax return. The benefit fields are optional and zero means none: SSBenefit is the monthly Social Security benefit at full retirement age in today's dollars and SSClaimAge the whole age from 62 to 70 it is claimed at (a spouse who sets only the claim age gets the spousal benefit alone); FERSHigh3 is the high-3 average salary in today's dollars, FERSYears the years of service, FERSSeparationAge and FERSStartAge the ages the member leaves service and starts the annuity, and FERSSurvivor the survivor option.
type HouseholdMember struct {
	Name  string
	Birth Date
	Role  Role

	SSBenefit  float64
	SSClaimAge int

	FERSHigh3         float64
	FERSYears         int
	FERSSeparationAge int
	FERSStartAge      int
	FERSSurvivor      Survivor
}

// HasFERS reports whether the member has a FERS annuity.
func (m HouseholdMember) HasFERS() bool { return m.FERSHigh3 > 0 }

// AgeIn is the member's age at the end of the calendar year.
func (m HouseholdMember) AgeIn(year int) int {
	y, _, _ := m.Birth.YMD()
	return year - y
}

// Household is who files the tax return and how: its members, the filing status, the name of the state table that taxes it and, if it is not the first checking account, the account tax is reconciled against.
type Household struct {
	Members []HouseholdMember
	Filing  tax.FilingStatus
	State   string
	// Checking is the account id of the reconciliation account; empty means the first checking account.
	Checking AccountID
}

// DefaultHousehold is a household without members that files jointly in Virginia.
func DefaultHousehold() Household {
	return Household{Filing: tax.MarriedJoint, State: tax.VA}
}

// Clone returns a deep copy.
func (h Household) Clone() Household {
	h.Members = slices.Clone(h.Members)
	return h
}

// Member returns the index of the member with the name, or -1.
func (h Household) Member(name string) int {
	return slices.IndexFunc(h.Members, func(m HouseholdMember) bool { return m.Name == name })
}

// DependentAges returns the ages at the end of the year of the dependents.
func (h Household) DependentAges(year int) []int {
	var ages []int
	for _, m := range h.Members {
		if m.Role == RoleDependent {
			ages = append(ages, m.AgeIn(year))
		}
	}
	return ages
}
