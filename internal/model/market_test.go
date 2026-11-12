// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import "testing"

func TestDefaultAllocationsAreValid(t *testing.T) {
	r := NewRecord(D(2026, 1, 1))
	r.Classes = DefaultClasses()
	for _, typ := range AccountTypes {
		alloc := DefaultAllocation(typ)
		if (alloc != nil) != Investable(typ) {
			t.Errorf("%s: default allocation %v but investable is %v", typ, alloc, Investable(typ))
		}
		if err := r.CheckAllocation(typ, alloc); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
}

func TestCheckAllocation(t *testing.T) {
	r := NewRecord(D(2026, 1, 1))
	r.Classes = DefaultClasses()
	lc, bonds := Holding{ClassLargeCap, 60}, Holding{ClassBonds, 40}
	for _, c := range []struct {
		name  string
		typ   AccountType
		alloc []Holding
		ok    bool
	}{
		{"60/40", TypeBrokerage, []Holding{lc, bonds}, true},
		{"empty", TypeChecking, nil, true},
		{"not an investment account", TypeChecking, []Holding{{ClassLargeCap, 100}}, false},
		{"sums to 99", Type401k, []Holding{lc, {ClassBonds, 39}}, false},
		{"within the tolerance", Type401k, []Holding{lc, {ClassBonds, 40.005}}, true},
		{"zero percent", Type401k, []Holding{{ClassLargeCap, 100}, {ClassBonds, 0}}, false},
		{"negative percent", Type401k, []Holding{{ClassLargeCap, 120}, {ClassBonds, -20}}, false},
		{"twice", Type401k, []Holding{lc, {ClassLargeCap, 40}}, false},
		{"unknown class", Type401k, []Holding{lc, {"gold", 40}}, false},
	} {
		if err := r.CheckAllocation(c.typ, c.alloc); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestDefaultCorrelations(t *testing.T) {
	r := NewRecord(D(2026, 1, 1))
	r.Correlations = DefaultCorrelations()
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{ClassLargeCap, ClassSmallCap, 0.85}, {ClassSmallCap, ClassLargeCap, 0.85},
		{ClassBonds, ClassTreasuries, 0.90}, {ClassTreasuries, ClassSmallCap, -0.15},
		{ClassCash, ClassLargeCap, 0}, {ClassLargeCap, ClassLargeCap, 1},
	} {
		if got := r.CorrelationOf(c.a, c.b); got != c.want {
			t.Errorf("%s/%s = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if len(r.Correlations) != 10 {
		t.Errorf("correlations: %v", r.Correlations)
	}
	for _, c := range r.Correlations {
		if c.A >= c.B {
			t.Errorf("pair %v is not in id order", c)
		}
	}
}

func TestSetCorrelationKeepsPairsCanonical(t *testing.T) {
	cs := SetCorrelation(nil, "b", "a", 0.5)
	if len(cs) != 1 || cs[0] != (Correlation{"a", "b", 0.5}) {
		t.Fatalf("%v", cs)
	}
	if cs = SetCorrelation(cs, "a", "b", 0); len(cs) != 0 {
		t.Errorf("a zero correlation is not removed: %v", cs)
	}
}
