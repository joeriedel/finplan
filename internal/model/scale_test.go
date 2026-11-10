// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"
	"testing"
	"time"
)

func TestScheduleScale(t *testing.T) {
	feb, mar, apr, jun := day(time.February, 1), day(time.March, 1), day(time.April, 1), day(time.June, 1)
	s := Constant(10).Scale(MustSchedule(Segment{From: jun, Value: 0.5}))
	if v, ok := s.At(apr); !ok || v != 10 {
		t.Errorf("before the scale: %v %v", v, ok)
	}
	if v, ok := s.At(jun); !ok || v != 5 {
		t.Errorf("from the scale: %v %v", v, ok)
	}

	// Where nothing lies beneath, the scale leaves the date unset.
	bounded := MustSchedule(Segment{From: feb, End: Ptr(apr), Value: 10})
	s = bounded.Scale(Constant(2))
	if v, ok := s.At(mar); !ok || v != 20 {
		t.Errorf("inside: %v %v", v, ok)
	}
	for _, d := range []Date{feb - 1, apr + 1, jun} {
		if _, ok := s.At(d); ok {
			t.Errorf("%v has a value", d)
		}
	}

	// A scale that ends gives way to what is beneath, and equal neighbours are one segment.
	s = Constant(4).Scale(MustSchedule(Segment{From: mar, End: Ptr(apr), Value: 1}))
	if want := []Segment{{From: MinDate, Value: 4}}; !reflect.DeepEqual(s.Segments(), want) {
		t.Errorf("identity scale: %+v", s.Segments())
	}
	s = Constant(4).Scale(MustSchedule(Segment{From: mar, End: Ptr(apr), Value: 2}))
	if want := []Segment{{From: MinDate, Value: 4}, {From: mar, Value: 8}, {From: apr + 1, Value: 4}}; !reflect.DeepEqual(s.Segments(), want) {
		t.Errorf("ended scale: %+v", s.Segments())
	}

	// A schedule that is empty, on either side, changes nothing.
	if got := bounded.Scale(Schedule{}); !reflect.DeepEqual(got.Segments(), bounded.Segments()) {
		t.Errorf("scaled by nothing: %+v", got.Segments())
	}
	if got := (Schedule{}).Scale(Constant(2)); len(got.Segments()) != 0 {
		t.Errorf("nothing scaled: %+v", got.Segments())
	}
}

func TestModificationOver(t *testing.T) {
	base := Constant(10)
	v := MustSchedule(Segment{From: day(time.June, 1), Value: 3})
	set := Modification{Values: v}
	scale := Modification{Mode: ModeScale, Values: v}
	if got, _ := set.Over(base).At(day(time.July, 1)); got != 3 {
		t.Errorf("set: %v", got)
	}
	if got, _ := (Modification{Mode: ModeSet, Values: v}).Over(base).At(day(time.July, 1)); got != 3 {
		t.Errorf("explicit set: %v", got)
	}
	if got, _ := scale.Over(base).At(day(time.July, 1)); got != 30 {
		t.Errorf("scale: %v", got)
	}
}

func TestScaleNeverOverwrites(t *testing.T) {
	r := overwriteRecord(t)
	scaled := func(m Modification) Modification { m.Mode = ModeScale; return m }
	rent := func(m Mode, v float64) Modification {
		return Modification{Target: ObjectRef{Kind: KindBudgetItem, ID: "rent"}, Property: PropAmount, Mode: m, Values: Constant(v)}
	}
	if err := r.AddProjection(&Projection{Name: "S", Mods: []Modification{
		growth(KindAccount, "chk", Constant(0.02)),         // 0: the set at 2 covers it
		scaled(growth(KindAccount, "chk", Constant(0.5))),  // 1: the set at 2 covers it
		growth(KindAccount, "chk", Constant(0.04)),         // 2
		scaled(growth(KindAccount, "chk", Constant(0.25))), // 3: a scale overwrites nothing
		rent(ModeSet, 100),   // 4
		rent(ModeScale, 0.7), // 5: leaves 4 in effect
		rent(ModeScale, 0.6), // 6
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := r.OverwrittenMods("S")
	if err != nil {
		t.Fatal(err)
	}
	want := []*Overwrite{{Projection: "S", Index: 2}, {Projection: "S", Index: 2}, nil, nil, nil, nil, nil}
	if !reflect.DeepEqual(got, want) {
		for i := range got {
			t.Logf("%d: %+v", i, got[i])
		}
		t.Fatal("wrong overwrites")
	}

	// A set after a scale overwrites it.
	if err := r.AddProjection(&Projection{Name: "T", Mods: []Modification{rent(ModeScale, 0.7), rent(ModeSet, 100)}}); err != nil {
		t.Fatal(err)
	}
	got, err = r.OverwrittenMods("T")
	if err != nil || got[0] == nil || got[0].Index != 1 || got[1] != nil {
		t.Errorf("set after scale: %+v %v", got, err)
	}
}

func TestCloneKeepsModeAndGrowth(t *testing.T) {
	p := &Projection{Name: "P", Mods: []Modification{
		{Target: ObjectRef{Kind: KindBudgetItem, ID: "x"}, Property: PropAmount, Mode: ModeScale, Values: Constant(0.7)},
		{Target: ObjectRef{Kind: KindBudgetItem, ID: "y"}, Property: PropAddItem, Item: &BudgetItem{ID: "y", Growth: Constant(0.03)}},
	}}
	c := p.Clone()
	if c.Mods[0].Mode != ModeScale || c.Mods[1].Item.Growth.Segments()[0].Value != 0.03 {
		t.Errorf("clone: %+v", c.Mods)
	}
}
