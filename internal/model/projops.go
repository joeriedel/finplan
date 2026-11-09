// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
)

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func (s Schedule) clone() Schedule {
	if s.segs == nil {
		return s
	}
	return Schedule{segs: s.Segments()}
}

// Clone returns a deep copy of the projection: nothing in it is shared with the original.
func (p *Projection) Clone() *Projection {
	c := *p
	c.Start = clonePtr(p.Start)
	c.OverrideHistory = clonePtr(p.OverrideHistory)
	c.AssetSettings = slices.Clone(p.AssetSettings)
	for i, s := range c.AssetSettings {
		c.AssetSettings[i].Start = clonePtr(s.Start)
		c.AssetSettings[i].OverrideHistory = clonePtr(s.OverrideHistory)
	}
	c.Mods = slices.Clone(p.Mods)
	for i, m := range c.Mods {
		c.Mods[i].Values = m.Values.clone()
		if m.Item != nil {
			it := *m.Item
			it.Start, it.End = clonePtr(it.Start), clonePtr(it.End)
			c.Mods[i].Item = &it
		}
	}
	c.Shocks = slices.Clone(p.Shocks)
	c.Rebalances = slices.Clone(p.Rebalances)
	for i, r := range c.Rebalances {
		c.Rebalances[i].Allocation = slices.Clone(r.Allocation)
	}
	return &c
}

// EditableCopy returns a record that shares everything with r except the projections, which are deep copies. It is
// what an edit of projections changes: the original stays as it was until the edit is accepted. An edit of accounts,
// assets or the budget changes a Clone instead.
func (r *Record) EditableCopy() *Record { return r.WithProjections(r.Projections) }

// WithProjections returns a record that shares everything with r except the projections, which are deep copies of ps.
func (r *Record) WithProjections(ps []*Projection) *Record {
	c := *r
	c.Projections = make([]*Projection, len(ps))
	for i, p := range ps {
		c.Projections[i] = p.Clone()
	}
	return &c
}

// UniqueProjectionName returns base if no projection has that name, else base with " 2", " 3", and so on added.
func (r *Record) UniqueProjectionName(base string) string {
	if r.Projection(base) == nil {
		return base
	}
	for n := 2; ; n++ {
		if name := base + " " + strconv.Itoa(n); r.Projection(name) == nil {
			return name
		}
	}
}

// Children returns the projections whose parent is the named one, in order.
func (r *Record) Children(name string) []*Projection {
	var out []*Projection
	for _, p := range r.Projections {
		if p.Parent == name {
			out = append(out, p)
		}
	}
	return out
}

// RemoveProjection deletes the projection. It refuses, changing nothing, while another projection builds on it.
func (r *Record) RemoveProjection(name string) error {
	i := slices.IndexFunc(r.Projections, func(p *Projection) bool { return p.Name == name })
	if i < 0 {
		return fmt.Errorf("model: unknown projection %q", name)
	}
	if kids := r.Children(name); len(kids) > 0 {
		return fmt.Errorf("model: projection %q cannot be removed: %q builds on it", name, kids[0].Name)
	}
	r.Projections = slices.Delete(r.Projections, i, i+1)
	return nil
}

// RenameProjection renames the projection and points its children at the new name. It refuses an empty or used name.
func (r *Record) RenameProjection(name, to string) error {
	p := r.Projection(name)
	if p == nil {
		return fmt.Errorf("model: unknown projection %q", name)
	}
	if to == "" {
		return errors.New("model: a projection needs a name")
	}
	if to == name {
		return nil
	}
	if r.Projection(to) != nil {
		return fmt.Errorf("model: projection name %q is already used", to)
	}
	for _, c := range r.Children(name) {
		c.Parent = to
	}
	p.Name = to
	return nil
}

// SetParent makes parent (empty for none) the projection's parent. It refuses, changing nothing, an unknown parent
// and a parent that would make a cycle; that error matches ErrCycle.
func (r *Record) SetParent(name, parent string) error {
	p := r.Projection(name)
	if p == nil {
		return fmt.Errorf("model: unknown projection %q", name)
	}
	if parent != "" && r.Projection(parent) == nil {
		return fmt.Errorf("model: unknown projection %q", parent)
	}
	old := p.Parent
	p.Parent = parent
	if _, err := r.Chain(name); err != nil {
		p.Parent = old
		return err
	}
	return nil
}

// CloneProjection adds a deep copy of the projection under the new name, which must be unused and not empty.
func (r *Record) CloneProjection(name, as string) (*Projection, error) {
	src := r.Projection(name)
	if src == nil {
		return nil, fmt.Errorf("model: unknown projection %q", name)
	}
	c := src.Clone()
	c.Name = as
	if err := r.AddProjection(c); err != nil {
		return nil, err
	}
	return c, nil
}

// InsertMod inserts m so that it is at index i of the projection's modifications.
func (p *Projection) InsertMod(i int, m Modification) error {
	if i < 0 || i > len(p.Mods) {
		return fmt.Errorf("model: projection %q: modification index %d out of range", p.Name, i)
	}
	p.Mods = slices.Insert(p.Mods, i, m)
	return nil
}

// RemoveMod deletes the modification at index i.
func (p *Projection) RemoveMod(i int) error {
	if i < 0 || i >= len(p.Mods) {
		return fmt.Errorf("model: projection %q: modification index %d out of range", p.Name, i)
	}
	p.Mods = slices.Delete(p.Mods, i, i+1)
	return nil
}

// MoveMod moves the modification at index from so that it ends up at index to.
func (p *Projection) MoveMod(from, to int) error {
	if from < 0 || from >= len(p.Mods) || to < 0 || to >= len(p.Mods) {
		return fmt.Errorf("model: projection %q: cannot move modification %d to %d", p.Name, from, to)
	}
	m := p.Mods[from]
	p.Mods = slices.Delete(p.Mods, from, from+1)
	p.Mods = slices.Insert(p.Mods, to, m)
	return nil
}
