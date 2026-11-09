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

// SetInflation sets the projection's one global inflation modification to a constant v, adding the modification if there is none; nil removes it. If there are several, the first is the one set or removed.
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

// AddMod appends the modification to the projection's. It refuses a non-global modification that names no object, a value that is not a number within range, and a target that does not resolve at the end of the list (ErrUnknownTarget). A budget item is added with AddItem, not here.
func AddMod(st *store.Store, rec *model.Record, name string, m model.Modification, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return insertMod(rec, p, len(p.Mods), m) })
}

// InsertMod is AddMod for a modification that ends up at index; index len adds it at the end.
func InsertMod(st *store.Store, rec *model.Record, name string, index int, m model.Modification, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return insertMod(rec, p, index, m) })
}

func insertMod(rec *model.Record, p *model.Projection, index int, m model.Modification) error {
	if err := checkMod(m); err != nil {
		return err
	}
	if err := p.InsertMod(index, m); err != nil {
		return err
	}
	return checkTarget(rec, p, index)
}

// checkTarget checks that the property of the modification of p at index suits its object and that the object resolves there: an account or asset the record has, or a budget item of the record or one added earlier in the chain.
func checkTarget(rec *model.Record, p *model.Projection, index int) error {
	m := p.Mods[index]
	want := ""
	switch m.Target.Kind {
	case model.KindBudgetItem:
		want = model.PropAmount
	case model.KindAccount, model.KindAsset:
		want = model.PropGrowth
	case model.KindGlobal:
		want = model.PropInflation
	default:
		return refuse(ErrInvalidValue, "unknown object kind %q", m.Target.Kind)
	}
	if m.Property != want {
		return refuse(ErrInvalidValue, "a modification of a %s changes its %s, not %q", m.Target.Kind, want, m.Property)
	}
	switch m.Target.Kind {
	case model.KindBudgetItem:
		ids, err := itemsBefore(rec, p, index)
		if err != nil {
			return err
		}
		if !ids[m.Target.ID] {
			return refuse(ErrUnknownTarget, "there is no budget item %q before this modification", m.Target.ID)
		}
	case model.KindAccount:
		if rec.Account(model.AccountID(m.Target.ID)) == nil {
			return refuse(ErrUnknownTarget, "there is no account %q", m.Target.ID)
		}
	case model.KindAsset:
		if rec.Asset(model.AssetID(m.Target.ID)) == nil {
			return refuse(ErrUnknownTarget, "there is no asset %q", m.Target.ID)
		}
	}
	return nil
}

// itemsBefore returns the ids of the budget items that exist at index of p's modifications: the record's, those added by p's ancestors and those added by p's own modifications before index. p may be an edited copy; its ancestors are taken from rec.
func itemsBefore(rec *model.Record, p *model.Projection, index int) (map[string]bool, error) {
	chain, err := rec.Chain(p.Name)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, it := range rec.Budget.Items {
		ids[it.ID] = true
	}
	for _, q := range chain[:len(chain)-1] {
		addedItems(ids, q.Mods)
	}
	addedItems(ids, p.Mods[:index])
	return ids, nil
}

func addedItems(ids map[string]bool, mods []model.Modification) {
	for _, m := range mods {
		if m.Property == model.PropAddItem && m.Item != nil {
			ids[m.Item.ID] = true
		}
	}
}

// danglingMods counts, per projection, target and property, the amount modifications of budget items that do not resolve.
func danglingMods(rec *model.Record) map[[3]string]int {
	out := map[[3]string]int{}
	for _, p := range rec.Projections {
		chain, err := rec.Chain(p.Name)
		if err != nil {
			continue
		}
		ids := map[string]bool{}
		for _, it := range rec.Budget.Items {
			ids[it.ID] = true
		}
		for _, q := range chain[:len(chain)-1] {
			addedItems(ids, q.Mods)
		}
		for i, m := range p.Mods {
			if m.Target.Kind == model.KindBudgetItem && m.Property == model.PropAmount && !ids[m.Target.ID] {
				out[[3]string{p.Name, m.Target.ID, m.Property}]++
			}
			addedItems(ids, p.Mods[i:i+1])
		}
	}
	return out
}

// checkNoNewDangling refuses an edit after which a modification, in the edited projection or one building on it, points at a budget item that no longer exists where it is, though it did before. Modifications that already pointed at nothing do not block an edit.
func checkNoNewDangling(before, after *model.Record) error {
	was := danglingMods(before)
	for k, n := range danglingMods(after) {
		if n > was[k] {
			return refuse(ErrUnknownTarget, "projection %q would have a modification of budget item %q that does not resolve", k[0], k[1])
		}
	}
	return nil
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
