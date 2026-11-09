// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func projRecord(t *testing.T) *Record {
	t.Helper()
	r := newRecord(t)
	for _, p := range []*Projection{
		{Name: "Base", Start: Ptr(day(time.March, 1))},
		{Name: "Kid", Parent: "Base", OverrideHistory: Ptr(true), Mods: []Modification{
			{Target: ObjectRef{Kind: KindGlobal}, Property: PropInflation, Values: Constant(0.03)},
			{Target: ObjectRef{Kind: KindBudgetItem, ID: "x"}, Property: PropAddItem, Item: &BudgetItem{ID: "x", End: Ptr(day(time.May, 1))}},
		}},
		{Name: "Other"},
	} {
		if err := r.AddProjection(p); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestEditableCopyIsDeep(t *testing.T) {
	r := projRecord(t)
	c := r.EditableCopy()
	if !reflect.DeepEqual(r, c) {
		t.Fatal("copy differs from the original")
	}
	kid := c.Projection("Kid")
	kid.Name = "Changed"
	*kid.OverrideHistory = false
	kid.Mods[0].Values = Constant(0.5)
	kid.Mods[1].Item.ID = "y"
	*kid.Mods[1].Item.End = day(time.June, 1)
	*c.Projection("Base").Start = day(time.April, 1)
	c.Projections[2] = nil
	want := projRecord(t)
	if !reflect.DeepEqual(r, want) {
		t.Error("changing the copy changed the original")
	}
}

func TestWithProjections(t *testing.T) {
	r := projRecord(t)
	other := []*Projection{{Name: "Solo", Mods: []Modification{{Property: PropGrowth, Values: Constant(1)}}}}
	c := r.WithProjections(other)
	if len(c.Projections) != 1 || !reflect.DeepEqual(c.Projections[0], other[0]) || c.Projections[0] == other[0] {
		t.Error("projections are not a deep copy of the argument")
	}
	if len(r.Projections) != 3 {
		t.Error("the original changed")
	}
}

func TestRemoveProjection(t *testing.T) {
	r := projRecord(t)
	if err := r.RemoveProjection("Base"); err == nil {
		t.Error("removed a projection others build on")
	}
	if err := r.RemoveProjection("nope"); err == nil {
		t.Error("removed an unknown projection")
	}
	if err := r.RemoveProjection("Kid"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveProjection("Base"); err != nil || len(r.Projections) != 1 || r.Projections[0].Name != "Other" {
		t.Errorf("after removing: %v %v", err, r.Projections)
	}
}

func TestRenameProjection(t *testing.T) {
	r := projRecord(t)
	if err := r.RenameProjection("Base", "Other"); err == nil {
		t.Error("renamed to a used name")
	}
	if err := r.RenameProjection("Base", ""); err == nil {
		t.Error("renamed to an empty name")
	}
	if err := r.RenameProjection("Base", "Root"); err != nil {
		t.Fatal(err)
	}
	if r.Projection("Base") != nil || r.Projection("Root") == nil || r.Projection("Kid").Parent != "Root" {
		t.Error("rename did not update the name and the children")
	}
	if err := r.RenameProjection("Root", "Root"); err != nil {
		t.Errorf("renaming to the same name: %v", err)
	}
}

func TestSetParent(t *testing.T) {
	r := projRecord(t)
	if err := r.SetParent("Base", "Kid"); !errors.Is(err, ErrCycle) {
		t.Errorf("cycle: %v", err)
	}
	if err := r.SetParent("Base", "Base"); !errors.Is(err, ErrCycle) {
		t.Errorf("self parent: %v", err)
	}
	if r.Projection("Base").Parent != "" {
		t.Error("a refused parent was kept")
	}
	if err := r.SetParent("Kid", "nope"); err == nil {
		t.Error("unknown parent accepted")
	}
	if err := r.SetParent("Kid", "Other"); err != nil || r.Projection("Kid").Parent != "Other" {
		t.Errorf("set parent: %v", err)
	}
	if err := r.SetParent("Kid", ""); err != nil || r.Projection("Kid").Parent != "" {
		t.Errorf("clear parent: %v", err)
	}
}

func TestCloneProjection(t *testing.T) {
	r := projRecord(t)
	c, err := r.CloneProjection("Kid", "Kid copy")
	if err != nil {
		t.Fatal(err)
	}
	want := r.Projection("Kid").Clone()
	want.Name = "Kid copy"
	if !reflect.DeepEqual(c, want) || r.Projection("Kid copy") != c {
		t.Error("clone differs from the source")
	}
	c.Mods[1].Item.ID = "y"
	if r.Projection("Kid").Mods[1].Item.ID != "x" {
		t.Error("clone shares an item with the source")
	}
	if _, err := r.CloneProjection("Kid", "Base"); err == nil {
		t.Error("cloned under a used name")
	}
	if _, err := r.CloneProjection("nope", "Z"); err == nil {
		t.Error("cloned an unknown projection")
	}
}

func TestUniqueProjectionName(t *testing.T) {
	r := projRecord(t)
	if got := r.UniqueProjectionName("Base copy"); got != "Base copy" {
		t.Errorf("free name: %q", got)
	}
	for _, want := range []string{"Base copy", "Base copy 2", "Base copy 3"} {
		got := r.UniqueProjectionName("Base copy")
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if err := r.AddProjection(&Projection{Name: got}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestModOps(t *testing.T) {
	mod := func(id string) Modification {
		return Modification{Target: ObjectRef{Kind: KindBudgetItem, ID: id}, Property: PropAmount, Values: Constant(1)}
	}
	ids := func(p *Projection) string {
		s := ""
		for _, m := range p.Mods {
			s += m.Target.ID
		}
		return s
	}
	p := &Projection{Name: "P"}
	for i, id := range []string{"a", "b", "c"} {
		if err := p.InsertMod(i, mod(id)); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.InsertMod(0, mod("z")); err != nil || ids(p) != "zabc" {
		t.Errorf("insert first: %v %s", err, ids(p))
	}
	if err := p.InsertMod(9, mod("q")); err == nil {
		t.Error("inserted out of range")
	}
	if err := p.MoveMod(0, 3); err != nil || ids(p) != "abcz" {
		t.Errorf("move down: %v %s", err, ids(p))
	}
	if err := p.MoveMod(3, 1); err != nil || ids(p) != "azbc" {
		t.Errorf("move up: %v %s", err, ids(p))
	}
	if err := p.MoveMod(1, 4); err == nil {
		t.Error("moved out of range")
	}
	if err := p.RemoveMod(1); err != nil || ids(p) != "abc" {
		t.Errorf("remove: %v %s", err, ids(p))
	}
	if err := p.RemoveMod(3); err == nil {
		t.Error("removed out of range")
	}
}
