// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"fmt"
	"slices"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// checkSchedule refuses a schedule with a value that is not a number or is out of range.
func checkSchedule(s model.Schedule) error {
	for _, seg := range s.Segments() {
		if v := seg.Value; v != v || v > maxValue || v < -maxValue {
			return refuse(ErrInvalidValue, "%v is not a number within range", v)
		}
	}
	return nil
}

// checkMod refuses a modification that needs an object and names none, or whose values are out of range.
func checkMod(m model.Modification) error {
	if m.Target.Kind != model.KindGlobal && m.Target.ID == "" {
		return refuse(ErrNoObject, "a modification of kind %q needs an object", m.Target.Kind)
	}
	return checkSchedule(m.Values)
}

// SetInflation sets the projection's one global inflation modification to a constant v, adding the modification if
// there is none; nil removes it. If there are several, the first is the one set or removed.
func SetInflation(st *store.Store, rec *model.Record, name string, v *float64, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		idx := slices.IndexFunc(p.Mods, func(m model.Modification) bool {
			return m.Target.Kind == model.KindGlobal && m.Property == model.PropInflation
		})
		switch {
		case v == nil && idx >= 0:
			return p.RemoveMod(idx)
		case v == nil:
			return nil
		}
		s := model.Constant(*v)
		if err := checkSchedule(s); err != nil {
			return err
		}
		if idx >= 0 {
			p.Mods[idx].Values = s
		} else {
			p.Mods = append(p.Mods, model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Values: s})
		}
		return nil
	})
}

// AddMod appends the modification to the projection's. It refuses a non-global modification that names no object and a
// value that is not a number within range.
func AddMod(st *store.Store, rec *model.Record, name string, m model.Modification, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if err := checkMod(m); err != nil {
			return err
		}
		p.Mods = append(p.Mods, m)
		return nil
	})
}

// SetModValues replaces the values of the projection's modification at index, which is how its value and start change.
func SetModValues(st *store.Store, rec *model.Record, name string, index int, values model.Schedule, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if index < 0 || index >= len(p.Mods) {
			return fmt.Errorf("projection %q: modification index %d out of range", name, index)
		}
		if err := checkSchedule(values); err != nil {
			return err
		}
		p.Mods[index].Values = values
		return nil
	})
}

// MoveMod moves the projection's modification at index from so that it ends up at index to.
func MoveMod(st *store.Store, rec *model.Record, name string, from, to int, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return p.MoveMod(from, to) })
}

// RemoveMod deletes the projection's modification at index.
func RemoveMod(st *store.Store, rec *model.Record, name string, index int, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return p.RemoveMod(index) })
}
