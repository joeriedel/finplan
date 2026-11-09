// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"
	"testing"
	"time"
)

func TestScheduleCovers(t *testing.T) {
	a, b, c := day(time.February, 1), day(time.April, 1), day(time.June, 1)
	from := func(d Date) Schedule { return MustSchedule(Segment{From: d, Value: 1}) }
	between := MustSchedule(Segment{From: a, End: Ptr(b), Value: 1})
	touching := MustSchedule(Segment{From: a, Value: 1}, Segment{From: b, Value: 2})
	gap := MustSchedule(Segment{From: a, End: Ptr(b), Value: 1}, Segment{From: c, Value: 2})
	for _, tc := range []struct {
		name string
		s, o Schedule
		want bool
	}{
		{"constant covers all", Constant(1), gap, true},
		{"constant covers constant", Constant(1), Constant(2), true},
		{"later start does not cover constant", from(a), Constant(1), false},
		{"open covers later open", from(a), from(b), true},
		{"open does not cover earlier open", from(b), from(a), false},
		{"bounded does not cover open", between, from(a), false},
		{"open covers bounded", from(a), between, true},
		{"touching segments are one range", touching, MustSchedule(Segment{From: a.Add(10), End: Ptr(c), Value: 3}), true},
		{"gap is not covered", gap, MustSchedule(Segment{From: a, End: Ptr(c), Value: 3}), false},
		{"covers a piece after the gap", gap, from(c.Add(5)), true},
		{"empty covered by anything", between, Schedule{}, true},
		{"empty covers nothing", Schedule{}, between, false},
	} {
		if got := tc.s.Covers(tc.o); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func overwriteRecord(t *testing.T) *Record {
	t.Helper()
	d := day(time.January, 1)
	r := newRecord(t, acct("chk", TypeChecking, d, 1), acct("sav", TypeSavings, d, 1), acct("brk", TypeBrokerage, d, 1))
	addAsset(t, r, "cash", AccountMember("chk"), AccountMember("sav"))
	return r
}

func growth(kind ObjectKind, id string, v Schedule) Modification {
	return Modification{Target: ObjectRef{Kind: kind, ID: id}, Property: PropGrowth, Values: v}
}

func TestOverwrittenMods(t *testing.T) {
	r := overwriteRecord(t)
	later := MustSchedule(Segment{From: day(time.June, 1), Value: 0.05})
	amount := func(v Schedule) Modification {
		return Modification{Target: ObjectRef{Kind: KindBudgetItem, ID: "rent"}, Property: PropAmount, Values: v}
	}
	infl := func(v Schedule) Modification {
		return Modification{Target: ObjectRef{Kind: KindGlobal}, Property: PropInflation, Values: v}
	}
	must := func(p *Projection) {
		t.Helper()
		if err := r.AddProjection(p); err != nil {
			t.Fatal(err)
		}
	}
	must(&Projection{Name: "Base", Mods: []Modification{growth(KindAccount, "chk", Constant(0.01))}})
	must(&Projection{Name: "P", Parent: "Base", Mods: []Modification{
		growth(KindAccount, "chk", Constant(0.02)), // 0: the last later line covering chk is 2
		growth(KindAsset, "cash", Constant(0.03)),  // 1: chk is overwritten by 2 and sav by 5
		growth(KindAccount, "chk", Constant(0.04)), // 2
		growth(KindAccount, "brk", later),          // 3: later constant covers it
		growth(KindAccount, "brk", Constant(0.06)), // 4
		growth(KindAccount, "sav", Constant(0.07)), // 5: nothing after covers sav
		amount(later),         // 6
		amount(Constant(100)), // 7
		infl(Constant(0.02)),  // 8: a different property later is no overwrite
		{Target: ObjectRef{Kind: KindBudgetItem, ID: "rent"}, Property: PropAddItem, Item: &BudgetItem{ID: "rent"}}, // 9
		growth(KindAccount, "gone", Constant(0.1)), // 10: target does not exist
		growth(KindAccount, "gone", Constant(0.2)), // 11
	}})
	got, err := r.OverwrittenMods("P")
	if err != nil {
		t.Fatal(err)
	}
	want := []*Overwrite{
		{Projection: "P", Index: 2},
		{Projection: "P", Index: 2},
		nil,
		{Projection: "P", Index: 4},
		nil,
		nil,
		{Projection: "P", Index: 7},
		nil,
		nil,
		nil,
		nil,
		nil,
	}
	if !reflect.DeepEqual(got, want) {
		for i := range got {
			t.Logf("%d: %+v", i, got[i])
		}
		t.Fatal("wrong overwrites")
	}

	must(&Projection{Name: "Q", Mods: []Modification{
		growth(KindAsset, "cash", Constant(0.03)),
		growth(KindAccount, "chk", Constant(0.04)),
	}})
	partial, err := r.OverwrittenMods("Q")
	if err != nil || !reflect.DeepEqual(partial, []*Overwrite{{Projection: "Q", Index: 1, Partial: true}, nil}) {
		t.Errorf("partial: %+v %v", partial, err)
	}

	// Ancestor modifications are overwritten by the projection's own, but only the projection's own are reported.
	baseGot, err := r.OverwrittenMods("Base")
	if err != nil || len(baseGot) != 1 || baseGot[0] != nil {
		t.Errorf("base: %v %v", baseGot, err)
	}
}

func TestOverwrittenModsAcrossChain(t *testing.T) {
	r := overwriteRecord(t)
	for _, p := range []*Projection{
		{Name: "Base", Mods: []Modification{growth(KindAccount, "chk", Constant(0.01))}},
		{Name: "Child", Parent: "Base", Mods: []Modification{growth(KindAccount, "chk", Constant(0.02))}},
	} {
		if err := r.AddProjection(p); err != nil {
			t.Fatal(err)
		}
	}
	// The parent's modification is applied first, so the child's own is never overwritten by it.
	got, err := r.OverwrittenMods("Child")
	if err != nil || len(got) != 1 || got[0] != nil {
		t.Errorf("child: %v %v", got, err)
	}
	if _, err := r.OverwrittenMods("nope"); err == nil {
		t.Error("unknown projection accepted")
	}
}
