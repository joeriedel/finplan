// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"math"
	"strings"

	"github.com/joeriedel/finplan/internal/benefits"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
	"github.com/joeriedel/finplan/internal/tax"
)

// HouseholdChange is the settings of the household to change; nil leaves one as it is.
type HouseholdChange struct {
	Filing *tax.FilingStatus
	// State is the name of a state table the record has.
	State *string
	// Checking is the id of a checking account to reconcile against; empty goes back to the first checking account.
	Checking *model.AccountID
}

// SetHousehold changes the filing status, the state table the household is taxed by, or the checking account its tax is reconciled against. The state must have a table, and the account must exist and be a checking account.
func SetHousehold(st *store.Store, rec *model.Record, c HouseholdChange, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		h := &next.Household
		if c.Filing != nil {
			if !c.Filing.Valid() {
				return Result{}, refuse(ErrInvalidValue, "unknown filing status %q", *c.Filing)
			}
			h.Filing = *c.Filing
		}
		if c.State != nil {
			if !next.Tax.HasState(*c.State) {
				return Result{}, refuse(ErrUnknownObject, "there is no state tax table %q", *c.State)
			}
			h.State = *c.State
		}
		if c.Checking != nil {
			if *c.Checking != "" {
				a := next.Account(*c.Checking)
				if a == nil {
					return Result{}, refuse(ErrUnknownObject, "there is no account %q", *c.Checking)
				}
				if a.Type != model.TypeChecking {
					return Result{}, refuse(ErrInvalidValue, "account %q is not a checking account", a.ID)
				}
			}
			h.Checking = *c.Checking
		}
		return Result{Household: h}, nil
	})
}

// checkHouseholdMember trims the member's name and refuses a member that is incomplete or whose name another member (other than the one at skip, -1 for none) has, ignoring case.
func checkHouseholdMember(rec *model.Record, m model.HouseholdMember, skip int) (model.HouseholdMember, error) {
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		return m, refuse(ErrInvalidValue, "a household member needs a name")
	}
	for i, other := range rec.Household.Members {
		if i != skip && strings.EqualFold(other.Name, m.Name) {
			return m, refuse(ErrNameTaken, "there is already a household member %q", other.Name)
		}
	}
	if !m.Role.Valid() {
		return m, refuse(ErrInvalidValue, "the role %q must be earner, spouse or dependent", m.Role)
	}
	if y, _, _ := m.Birth.YMD(); m.Birth == 0 || y < 1900 || m.Birth > rec.Today {
		return m, refuse(ErrInvalidValue, "the birth date %v must be from 1900 to today", m.Birth)
	}
	return m, checkBenefits(m)
}

// checkBenefits refuses Social Security and FERS inputs that are out of range, set on a dependent, or a FERS annuity the service does not allow.
func checkBenefits(m model.HouseholdMember) error {
	hasSS := m.SSBenefit != 0 || m.SSClaimAge != 0
	if m.Role == model.RoleDependent && (hasSS || m != (model.HouseholdMember{Name: m.Name, Birth: m.Birth, Role: m.Role})) {
		return refuse(ErrInvalidValue, "a dependent has no Social Security or FERS benefits")
	}
	if m.SSBenefit < 0 || math.IsNaN(m.SSBenefit) || math.IsInf(m.SSBenefit, 0) {
		return refuse(ErrInvalidValue, "the Social Security benefit %v must be zero or more", m.SSBenefit)
	}
	if (m.SSBenefit > 0 || m.SSClaimAge != 0) && (m.SSClaimAge < benefits.MinClaimAge || m.SSClaimAge > benefits.MaxClaimAge) {
		return refuse(ErrInvalidValue, "the Social Security claim age %d must be from %d to %d", m.SSClaimAge, benefits.MinClaimAge, benefits.MaxClaimAge)
	}
	if !m.FERSSurvivor.Valid() {
		return refuse(ErrInvalidValue, "the survivor option %q must be none, partial or full", m.FERSSurvivor)
	}
	if !m.HasFERS() {
		if m.FERSHigh3 != 0 || m.FERSYears != 0 || m.FERSSeparationAge != 0 || m.FERSStartAge != 0 || (m.FERSSurvivor != "" && m.FERSSurvivor != model.SurvivorNone) {
			return refuse(ErrInvalidValue, "the FERS inputs need a high-3 salary above zero")
		}
		return nil
	}
	switch {
	case math.IsNaN(m.FERSHigh3) || math.IsInf(m.FERSHigh3, 0):
		return refuse(ErrInvalidValue, "the FERS high-3 salary %v must be a number", m.FERSHigh3)
	case m.FERSYears < 1 || m.FERSYears > 60:
		return refuse(ErrInvalidValue, "the years of FERS service %d must be from 1 to 60", m.FERSYears)
	case m.FERSSeparationAge < 18 || m.FERSSeparationAge > 90:
		return refuse(ErrInvalidValue, "the FERS separation age %d must be from 18 to 90", m.FERSSeparationAge)
	case m.FERSStartAge < m.FERSSeparationAge || m.FERSStartAge > 90:
		return refuse(ErrInvalidValue, "the FERS annuity start age %d must be from the separation age %d to 90", m.FERSStartAge, m.FERSSeparationAge)
	}
	by, _, _ := m.Birth.YMD()
	if _, err := benefits.Eligibility(by, m.FERSYears, m.FERSStartAge); err != nil {
		return refuse(ErrInvalidValue, "%v", err)
	}
	return nil
}

// AddHouseholdMember adds a member to the household. The name must be unique among the members, ignoring case, the role earner, spouse or dependent, and the birth date a date from 1900 to the record's today. The Social Security and FERS inputs are checked as EditHouseholdMember does.
func AddHouseholdMember(st *store.Store, rec *model.Record, m model.HouseholdMember, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		m, err := checkHouseholdMember(next, m, -1)
		if err != nil {
			return Result{}, err
		}
		next.Household.Members = append(next.Household.Members, m)
		return Result{Household: &next.Household, Member: &m}, nil
	})
}

// MemberChange is the attributes of a household member to change; nil leaves one as it is.
type MemberChange struct {
	Name  *string
	Birth *model.Date
	Role  *model.Role

	SSBenefit  *float64
	SSClaimAge *int

	FERSHigh3         *float64
	FERSYears         *int
	FERSSeparationAge *int
	FERSStartAge      *int
	FERSSurvivor      *model.Survivor
}

// EditHouseholdMember changes the member of the name. The same checks as AddHouseholdMember apply, and the Social Security and FERS inputs are checked together: a claim age from 62 to 70 when there is a benefit (a spouse's claim age alone asks for the spousal benefit), a FERS annuity only with a high-3 salary, 1 to 60 years of service, a separation age from 18 and a start age not before it that the years of service allow, and none of them on a dependent.
func EditHouseholdMember(st *store.Store, rec *model.Record, name string, c MemberChange, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		i := next.Household.Member(name)
		if i < 0 {
			return Result{}, refuse(ErrUnknownObject, "there is no household member %q", name)
		}
		m := next.Household.Members[i]
		if c.Name != nil {
			m.Name = *c.Name
		}
		if c.Birth != nil {
			m.Birth = *c.Birth
		}
		if c.Role != nil {
			m.Role = *c.Role
		}
		if c.SSBenefit != nil {
			m.SSBenefit = *c.SSBenefit
		}
		if c.SSClaimAge != nil {
			m.SSClaimAge = *c.SSClaimAge
		}
		if c.FERSHigh3 != nil {
			m.FERSHigh3 = *c.FERSHigh3
		}
		if c.FERSYears != nil {
			m.FERSYears = *c.FERSYears
		}
		if c.FERSSeparationAge != nil {
			m.FERSSeparationAge = *c.FERSSeparationAge
		}
		if c.FERSStartAge != nil {
			m.FERSStartAge = *c.FERSStartAge
		}
		if c.FERSSurvivor != nil {
			m.FERSSurvivor = *c.FERSSurvivor
		}
		m, err := checkHouseholdMember(next, m, i)
		if err != nil {
			return Result{}, err
		}
		if m.Role != model.RoleDependent {
			for _, a := range next.Accounts {
				if a.Beneficiary == name {
					return Result{}, refuse(ErrInUse, "%q is the beneficiary of account %q: only a dependent can be one", name, a.ID)
				}
			}
		}
		if m.Name != name {
			for _, a := range next.Accounts {
				if a.Beneficiary == name {
					a.Beneficiary = m.Name
				}
			}
		}
		next.Household.Members[i] = m
		return Result{Household: &next.Household, Member: &m}, nil
	})
}

// RemoveHouseholdMember removes the member of the name. The result's Member is the member as it was.
func RemoveHouseholdMember(st *store.Store, rec *model.Record, name string, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		i := next.Household.Member(name)
		if i < 0 {
			return Result{}, refuse(ErrUnknownObject, "there is no household member %q", name)
		}
		m := next.Household.Members[i]
		next.Household.Members = append(next.Household.Members[:i], next.Household.Members[i+1:]...)
		var affected []Affected
		for _, a := range next.Accounts {
			if a.Beneficiary == name {
				a.Beneficiary = ""
				affected = append(affected, Affected{AffectedBeneficiary, string(a.ID), "beneficiary " + name, EffectRemoved})
			}
		}
		return Result{Household: &next.Household, Member: &m, Removed: m.Name, Affected: affected}, nil
	})
}
