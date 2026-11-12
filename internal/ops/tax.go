// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"slices"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
	"github.com/joeriedel/finplan/internal/tax"
)

// SetFederalTable replaces the federal table of the year the table names, or adds a table for a new year. The table must be valid (see tax.Federal.Validate).
func SetFederalTable(st *store.Store, rec *model.Record, f tax.Federal, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		if err := f.Validate(); err != nil {
			return Result{}, refuse(ErrInvalidValue, "federal table %d: %v", f.Year, err)
		}
		f = f.Clone()
		next.Tax.SetFederal(f)
		return Result{Federal: &f}, nil
	})
}

// SetStateTable replaces the table of the state and year the table names, or adds one, which for a new name is a new state that households can name. The table must be valid (see tax.State.Validate).
func SetStateTable(st *store.Store, rec *model.Record, s tax.State, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		if err := s.Validate(); err != nil {
			return Result{}, refuse(ErrInvalidValue, "state table %q %d: %v", s.Name, s.Year, err)
		}
		s = s.Clone()
		next.Tax.SetState(s)
		return Result{State: &s}, nil
	})
}

// RevertFederalTable restores the shipped federal table of the year. A year the program ships no table for is ErrUnknownObject.
func RevertFederalTable(st *store.Store, rec *model.Record, year int, o Options) (*Result, error) {
	shipped, ok := tax.Defaults().FederalYear(year)
	if !ok {
		return nil, refuse(ErrUnknownObject, "there is no shipped federal table for %d", year)
	}
	return SetFederalTable(st, rec, shipped, o)
}

// RevertStateTable restores the shipped table of the state and year. A state and year the program ships no table for is ErrUnknownObject.
func RevertStateTable(st *store.Store, rec *model.Record, name string, year int, o Options) (*Result, error) {
	shipped, ok := tax.Defaults().StateYear(name, year)
	if !ok {
		return nil, refuse(ErrUnknownObject, "there is no shipped state table %q for %d", name, year)
	}
	return SetStateTable(st, rec, shipped, o)
}

// CopyStateTables adds the state name with a copy of every year of the state from, so a household can name it. An unknown source is ErrUnknownObject, a name that a state already has is ErrNameTaken, and a name that is not valid (see tax.State.Validate) is ErrInvalidValue.
func CopyStateTables(st *store.Store, rec *model.Record, from, name string, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		if !next.Tax.HasState(from) {
			return Result{}, refuse(ErrUnknownObject, "there is no state tax table %q", from)
		}
		if next.Tax.HasState(name) {
			return Result{}, refuse(ErrNameTaken, "there is already a state tax table %q", name)
		}
		var first *tax.State
		for _, s := range slices.Clone(next.Tax.State) {
			if s.Name != from {
				continue
			}
			s = s.Clone()
			s.Name = name
			if err := s.Validate(); err != nil {
				return Result{}, refuse(ErrInvalidValue, "state table %q: %v", name, err)
			}
			next.Tax.SetState(s)
			if first == nil {
				first = &s
			}
		}
		return Result{State: first}, nil
	})
}
