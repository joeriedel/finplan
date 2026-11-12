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

// HouseholdMember is a person on the household's tax return.
type HouseholdMember struct {
	Name  string
	Birth Date
	Role  Role
}

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
