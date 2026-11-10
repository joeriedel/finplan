// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"fmt"
	"slices"
	"strings"

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

// checkMod refuses a modification that needs an object and names none, whose mode is unknown or does not suit its property, or whose values are out of range. It returns the modification with the default mode, set, written as the empty mode.
func checkMod(m model.Modification) (model.Modification, error) {
	if m.Target.Kind != model.KindGlobal && m.Target.ID == "" {
		return m, refuse(ErrNoObject, "a modification of kind %q needs an object", m.Target.Kind)
	}
	switch m.Mode {
	case model.ModeSet:
		m.Mode = ""
	case "":
	case model.ModeScale:
		if m.Property == model.PropAddItem {
			return m, refuse(ErrInvalidValue, "a modification that adds a budget item cannot scale")
		}
	default:
		return m, refuse(ErrInvalidValue, "the mode %q must be set or scale", m.Mode)
	}
	if err := checkSchedule(m.Values); err != nil {
		return m, err
	}
	for _, seg := range m.Values.Segments() {
		switch {
		case m.IsScale() && seg.Value < 0:
			return m, refuse(ErrInvalidValue, "a scale of %v must not be negative", seg.Value)
		case !m.IsScale() && m.Target.Kind == model.KindBudgetItem && m.Property == model.PropGrowth && seg.Value <= -1:
			return m, refuse(ErrInvalidValue, "a raise of %v must be above -1", seg.Value)
		}
	}
	return m, nil
}

// SetInflation sets the projection's global set-mode inflation modification to a constant v, adding it if there is none; nil removes it. If there are several, the first is the one set or removed. Scale modifications are kept and still apply on top: an added modification goes before the projection's first scale inflation modification.
func SetInflation(st *store.Store, rec *model.Record, name string, v *float64, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		idx := slices.IndexFunc(p.Mods, func(m model.Modification) bool {
			return m.Target.Kind == model.KindGlobal && m.Property == model.PropInflation && !m.IsScale()
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
			return nil
		}
		at := slices.IndexFunc(p.Mods, func(m model.Modification) bool {
			return m.Target.Kind == model.KindGlobal && m.Property == model.PropInflation && m.IsScale()
		})
		if at < 0 {
			at = len(p.Mods)
		}
		p.Mods = slices.Insert(p.Mods, at, model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Values: s})
		return nil
	})
}

// AddMod appends the modification to the projection's. It refuses a non-global modification that names no object, a value that is not a number within range (a scale also not below zero, a raise not at or below -1), a mode that is not set or scale, and a target that does not resolve at the end of the list (ErrUnknownTarget). A budget item is added with AddItem, not here.
func AddMod(st *store.Store, rec *model.Record, name string, m model.Modification, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return insertMod(rec, p, len(p.Mods), m) })
}

// InsertMod is AddMod for a modification that ends up at index; index len adds it at the end.
func InsertMod(st *store.Store, rec *model.Record, name string, index int, m model.Modification, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return insertMod(rec, p, index, m) })
}

func insertMod(rec *model.Record, p *model.Projection, index int, m model.Modification) error {
	m, err := checkMod(m)
	if err != nil {
		return err
	}
	if err := p.InsertMod(index, m); err != nil {
		return err
	}
	return checkTarget(rec, p, index)
}

// checkTarget checks that the property of the modification of p at index suits its object and that the object resolves there: an account or asset the record has or the projection's chain defines, or a budget item of the record or one added earlier in the chain.
func checkTarget(rec *model.Record, p *model.Projection, index int) error {
	m := p.Mods[index]
	var want []string
	switch m.Target.Kind {
	case model.KindBudgetItem:
		want = []string{model.PropAmount, model.PropGrowth}
	case model.KindAccount, model.KindAsset:
		want = []string{model.PropGrowth, model.PropDefault}
	case model.KindGlobal:
		want = []string{model.PropInflation}
	default:
		return refuse(ErrInvalidValue, "unknown object kind %q", m.Target.Kind)
	}
	if !slices.Contains(want, m.Property) {
		return refuse(ErrInvalidValue, "a modification of a %s changes its %s, not %q", m.Target.Kind, strings.Join(want, " or "), m.Property)
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
		view, err := viewOf(rec, p)
		if err != nil {
			return err
		}
		if view.Account(model.AccountID(m.Target.ID)) == nil {
			return refuse(ErrUnknownTarget, "there is no account %q", m.Target.ID)
		}
	case model.KindAsset:
		view, err := viewOf(rec, p)
		if err != nil {
			return err
		}
		if view.Asset(model.AssetID(m.Target.ID)) == nil {
			return refuse(ErrUnknownTarget, "there is no asset %q", m.Target.ID)
		}
	}
	return nil
}

// AvailableItem is a budget item that exists at some position of a projection's modifications, and the projection that adds it; AddedBy is empty for an item of the record's budget.
type AvailableItem struct {
	Item    model.BudgetItem
	AddedBy string
}

// ItemsBefore lists the budget items that exist at index of the projection's modifications (len(Mods) is the end of the list): the record's, those added by the projection's ancestors, root first, and those added by its own modifications before index. A modification of an item must come at a position the item exists at.
func ItemsBefore(rec *model.Record, name string, index int) ([]AvailableItem, error) {
	p := rec.Projection(name)
	if p == nil {
		return nil, errUnknown(name)
	}
	if index < 0 || index > len(p.Mods) {
		return nil, fmt.Errorf("projection %q: modification index %d out of range", name, index)
	}
	return itemsAt(rec, p, index)
}

// itemsAt is ItemsBefore for p, which may be an edited copy; its ancestors are taken from rec.
func itemsAt(rec *model.Record, p *model.Projection, index int) ([]AvailableItem, error) {
	chain, err := rec.Chain(p.Name)
	if err != nil {
		return nil, err
	}
	var out []AvailableItem
	for _, it := range rec.Budget.Items {
		out = append(out, AvailableItem{Item: it})
	}
	add := func(by string, mods []model.Modification) {
		for _, m := range mods {
			if m.Property == model.PropAddItem && m.Item != nil {
				out = append(out, AvailableItem{Item: *m.Item, AddedBy: by})
			}
		}
	}
	for _, q := range chain[:len(chain)-1] {
		add(q.Name, q.Mods)
	}
	add(p.Name, p.Mods[:index])
	return out, nil
}

// itemsBefore returns the ids of the items itemsAt lists.
func itemsBefore(rec *model.Record, p *model.Projection, index int) (map[string]bool, error) {
	items, err := itemsAt(rec, p, index)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, a := range items {
		ids[a.Item.ID] = true
	}
	return ids, nil
}

func addedItems(ids map[string]bool, mods []model.Modification) {
	for _, m := range mods {
		if m.Property == model.PropAddItem && m.Item != nil {
			ids[m.Item.ID] = true
		}
	}
}

// danglingMods counts, per projection, target and property, the amount and growth modifications of budget items that do not resolve.
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
			if m.Target.Kind == model.KindBudgetItem && (m.Property == model.PropAmount || m.Property == model.PropGrowth) && !ids[m.Target.ID] {
				out[[3]string{p.Name, m.Target.ID, m.Property}]++
			}
			addedItems(ids, p.Mods[i:i+1])
		}
	}
	return out
}

// checkNoNewDangling refuses an edit after which a modification, in the edited projection or one building on it, points at a budget item that no longer exists where it is, though it did before. Modifications that already pointed at nothing do not block an edit. A renamed projection is compared under its old name; a projection the edit added copies another and has nothing to compare with.
func checkNoNewDangling(before, after *model.Record, rename *Rename) error {
	was := danglingMods(before)
	for k, n := range danglingMods(after) {
		name := k[0]
		if rename != nil && name == rename.To {
			name = rename.From
		}
		if before.Projection(name) == nil {
			continue
		}
		if n > was[[3]string{name, k[1], k[2]}] {
			return refuse(ErrUnknownTarget, "projection %q would have a modification of budget item %q that does not resolve", k[0], k[1])
		}
	}
	return nil
}

// CheckMod returns the reason AddMod would refuse m at the end of the projection's modifications, nil if it would not. It is the check AddMod makes, for editors that show why an input cannot be added.
func CheckMod(rec *model.Record, name string, m model.Modification) error {
	p := rec.Projection(name)
	if p == nil {
		return errUnknown(name)
	}
	return insertMod(rec, p.Clone(), len(p.Mods), m)
}

// SetMod replaces the projection's modification at index with m, which can change its object and property as well as its values. m is checked as AddMod checks a modification added at index: its property must suit its object and a budget item must exist before index. A modification that adds a budget item is edited with EditItem, so neither the old nor the new one may be add_item. An edit after which a later modification points at an item that no longer exists is ErrUnknownTarget.
func SetMod(st *store.Store, rec *model.Record, name string, index int, m model.Modification, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if index < 0 || index >= len(p.Mods) {
			return fmt.Errorf("projection %q: modification index %d out of range", name, index)
		}
		if p.Mods[index].Property == model.PropAddItem || m.Property == model.PropAddItem {
			return refuse(ErrInvalidValue, "a modification that adds a budget item is edited with EditItem, not replaced")
		}
		m, err := checkMod(m)
		if err != nil {
			return err
		}
		p.Mods[index] = m
		return checkTarget(rec, p, index)
	})
}

// SetModMode changes how the projection's modification at index combines with what lies beneath it: model.ModeSet or model.ModeScale. Its values are kept as they are, or replaced by values if that is not nil, since the new mode may read them differently. A modification that adds a budget item has no mode.
func SetModMode(st *store.Store, rec *model.Record, name string, index int, mode model.Mode, values *model.Schedule, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if index < 0 || index >= len(p.Mods) {
			return fmt.Errorf("projection %q: modification index %d out of range", name, index)
		}
		m := p.Mods[index]
		m.Mode = mode
		if values != nil {
			m.Values = *values
		}
		m, err := checkMod(m)
		if err != nil {
			return err
		}
		p.Mods[index] = m
		return nil
	})
}

// SetModValues replaces the values of the projection's modification at index, which is how its value and start change.
func SetModValues(st *store.Store, rec *model.Record, name string, index int, values model.Schedule, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if index < 0 || index >= len(p.Mods) {
			return fmt.Errorf("projection %q: modification index %d out of range", name, index)
		}
		m := p.Mods[index]
		m.Values = values
		m, err := checkMod(m)
		if err != nil {
			return err
		}
		p.Mods[index] = m
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
