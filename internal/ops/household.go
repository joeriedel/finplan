// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"strings"

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
	return m, nil
}

// AddHouseholdMember adds a member to the household. The name must be unique among the members, ignoring case, the role earner, spouse or dependent, and the birth date a date from 1900 to the record's today.
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
}

// EditHouseholdMember changes the member of the name. The same checks as AddHouseholdMember apply.
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
		m, err := checkHouseholdMember(next, m, i)
		if err != nil {
			return Result{}, err
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
		return Result{Household: &next.Household, Member: &m, Removed: m.Name}, nil
	})
}
