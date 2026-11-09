// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// freeName trims name and checks that it is not empty and that no projection of rec has it.
func freeName(rec *model.Record, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrEmptyName
	}
	if rec.Projection(name) != nil {
		return "", refuse(ErrNameTaken, "there is already a projection named %q", name)
	}
	return name, nil
}

// NewProjection adds an empty projection building on parent (empty for none).
func NewProjection(st *store.Store, rec *model.Record, name, parent string, o Options) (*Result, error) {
	return edit(st, rec, o, func(next *model.Record) (Result, error) {
		trimmed, err := freeName(next, name)
		if err != nil {
			return Result{}, err
		}
		if parent != "" && next.Projection(parent) == nil {
			return Result{}, errUnknown(parent)
		}
		p := &model.Projection{Name: trimmed, Parent: parent}
		if err := next.AddProjection(p); err != nil {
			return Result{}, err
		}
		return Result{Projection: p}, nil
	})
}

// CloneProjection adds a deep copy of the projection from under the name as.
func CloneProjection(st *store.Store, rec *model.Record, from, as string, o Options) (*Result, error) {
	return edit(st, rec, o, func(next *model.Record) (Result, error) {
		if next.Projection(from) == nil {
			return Result{}, errUnknown(from)
		}
		trimmed, err := freeName(next, as)
		if err != nil {
			return Result{}, err
		}
		p, err := next.CloneProjection(from, trimmed)
		if err != nil {
			return Result{}, err
		}
		return Result{Projection: p}, nil
	})
}

// RenameProjection renames the projection, points the projections that build on it at the new name and rewrites the
// saved view presets that name it. A preset that cannot be rewritten is a warning in the result; the rename is saved
// regardless. Renaming to the name it has is ErrNoChange.
func RenameProjection(st *store.Store, rec *model.Record, from, to string, o Options) (*Result, error) {
	return edit(st, rec, o, func(next *model.Record) (Result, error) {
		if next.Projection(from) == nil {
			return Result{}, errUnknown(from)
		}
		if strings.TrimSpace(to) == from {
			return Result{}, ErrNoChange
		}
		trimmed, err := freeName(next, to)
		if err != nil {
			return Result{}, err
		}
		if err := next.RenameProjection(from, trimmed); err != nil {
			return Result{}, err
		}
		return Result{Projection: next.Projection(trimmed), Rename: &Rename{from, trimmed}}, nil
	})
}

// DeleteProjection removes the projection. It refuses while another projection builds on it.
func DeleteProjection(st *store.Store, rec *model.Record, name string, o Options) (*Result, error) {
	return edit(st, rec, o, func(next *model.Record) (Result, error) {
		if next.Projection(name) == nil {
			return Result{}, errUnknown(name)
		}
		if kids := next.Children(name); len(kids) > 0 {
			return Result{}, refuse(ErrHasChildren, "%q cannot be deleted while %q builds on it", name, kids[0].Name)
		}
		if err := next.RemoveProjection(name); err != nil {
			return Result{}, err
		}
		return Result{Removed: name}, nil
	})
}

// SetParent makes the projection build on parent, or on nothing if parent is empty. It refuses an unknown parent and one
// that would make a cycle; that error matches model.ErrCycle.
func SetParent(st *store.Store, rec *model.Record, name, parent string, o Options) (*Result, error) {
	return edit(st, rec, o, func(next *model.Record) (Result, error) {
		p := next.Projection(name)
		if p == nil {
			return Result{}, errUnknown(name)
		}
		if parent != "" && next.Projection(parent) == nil {
			return Result{}, errUnknown(parent)
		}
		if err := next.SetParent(name, parent); err != nil {
			if errors.Is(err, model.ErrCycle) {
				return Result{}, refuse(model.ErrCycle, "%q cannot build on %q: that would make a cycle", name, parent)
			}
			return Result{}, err
		}
		return Result{Projection: p}, nil
	})
}

// SetStart sets the projection's start date; nil removes it, so the projection inherits.
func SetStart(st *store.Store, rec *model.Record, name string, start *model.Date, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		p.Start = copyPtr(start)
		return nil
	})
}

// SetOverrideHistory sets the projection's override_history; nil removes it, so the projection inherits.
func SetOverrideHistory(st *store.Store, rec *model.Record, name string, override *bool, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		p.OverrideHistory = copyPtr(override)
		return nil
	})
}

func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// RestoreProjections replaces the record's projections with copies of ps, which is how an editor undoes and redoes an
// edit. rename, if not nil, is the rename that edit made; the presets that name its From are rewritten to its To. Nothing
// is checked beyond what saving checks: ps comes from an earlier state of this record.
func RestoreProjections(st *store.Store, rec *model.Record, ps []*model.Projection, rename *Rename, o Options) (*Result, error) {
	next := rec.WithProjections(ps)
	return commit(st, next, Result{Rename: rename}, o)
}
