// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// Class IDs of the shipped market classes.
const (
	ClassLargeCap      = "us-large-cap"
	ClassSmallCap      = "us-small-cap"
	ClassInternational = "international"
	ClassBonds         = "us-bonds"
	ClassTreasuries    = "treasuries"
	ClassCash          = "cash"
)

// MarketClass is a source of returns (treasuries, large cap, ...). Return is the annual growth of the class over time; Volatility is the annual standard deviation of its return, which the Monte Carlo runs draw yearly returns with.
type MarketClass struct {
	ID         string
	Name       string
	Return     Schedule
	Volatility float64
}

// Correlation is the correlation of the returns of two classes, A before B in ID order. A missing pair is uncorrelated and a class is always fully correlated with itself.
type Correlation struct {
	A, B  string
	Value float64
}

// DefaultClasses returns the shipped market classes: the session defaults a new record starts with, in their order.
func DefaultClasses() []MarketClass {
	mk := func(id, name string, ret, vol float64) MarketClass {
		return MarketClass{ID: id, Name: name, Return: Constant(ret), Volatility: vol}
	}
	return []MarketClass{
		mk(ClassLargeCap, "US large cap", 0.10, 0.16),
		mk(ClassSmallCap, "US small cap", 0.11, 0.20),
		mk(ClassInternational, "International", 0.08, 0.18),
		mk(ClassBonds, "US bonds", 0.045, 0.06),
		mk(ClassTreasuries, "Treasuries", 0.04, 0.05),
		mk(ClassCash, "Cash", 0.03, 0.01),
	}
}

// DefaultCorrelations returns the shipped correlations; cash is uncorrelated with every class and so has none.
func DefaultCorrelations() []Correlation {
	cs := []Correlation{
		{ClassLargeCap, ClassSmallCap, 0.85},
		{ClassInternational, ClassLargeCap, 0.75},
		{ClassInternational, ClassSmallCap, 0.70},
		{ClassBonds, ClassLargeCap, 0.10},
		{ClassBonds, ClassSmallCap, 0.05},
		{ClassBonds, ClassInternational, 0.10},
		{ClassLargeCap, ClassTreasuries, -0.10},
		{ClassSmallCap, ClassTreasuries, -0.15},
		{ClassInternational, ClassTreasuries, -0.05},
		{ClassBonds, ClassTreasuries, 0.90},
	}
	return CanonicalCorrelations(cs)
}

// CanonicalCorrelations returns the correlations with each pair's classes and the pairs in ID order, so equal sets compare equal.
func CanonicalCorrelations(cs []Correlation) []Correlation {
	out := slices.Clone(cs)
	for i, c := range out {
		if c.A > c.B {
			out[i].A, out[i].B = c.B, c.A
		}
	}
	slices.SortFunc(out, func(x, y Correlation) int {
		if x.A != y.A {
			return cmp.Compare(x.A, y.A)
		}
		return cmp.Compare(x.B, y.B)
	})
	return out
}

// SetCorrelation returns the correlations with the pair set to v, or removed if v is 0, in canonical order.
func SetCorrelation(cs []Correlation, a, b string, v float64) []Correlation {
	if a > b {
		a, b = b, a
	}
	out := slices.DeleteFunc(slices.Clone(cs), func(c Correlation) bool { return c.A == a && c.B == b })
	if v != 0 {
		out = append(out, Correlation{a, b, v})
	}
	return CanonicalCorrelations(out)
}

// CorrelationOf returns the correlation of two classes: 1 for a class with itself, 0 for a pair that has none.
func (r *Record) CorrelationOf(a, b string) float64 {
	if a == b {
		return 1
	}
	if a > b {
		a, b = b, a
	}
	for _, c := range r.Correlations {
		if c.A == a && c.B == b {
			return c.Value
		}
	}
	return 0
}

// Class returns the market class, or nil.
func (r *Record) Class(id string) *MarketClass {
	for i := range r.Classes {
		if r.Classes[i].ID == id {
			return &r.Classes[i]
		}
	}
	return nil
}

// Holding is one class's share of an account's allocation, in percent: 60 is 60%.
type Holding struct {
	Class   string
	Percent float64
}

// AllocationTolerance is how far from 100 the percents of an allocation may sum.
const AllocationTolerance = 0.01

// Investable reports whether an account of the type can hold an allocation.
func Investable(t AccountType) bool {
	switch t {
	case TypeBrokerage, Type401k, TypeTSP, TypeIRA, TypeRothIRA, TypeHSA, Type529:
		return true
	}
	return false
}

// DefaultAllocation is the allocation suggested for an account of the type, nil if the type holds none. It is never applied automatically.
func DefaultAllocation(t AccountType) []Holding {
	switch t {
	case Type401k, TypeTSP, TypeIRA, Type529:
		return []Holding{{ClassLargeCap, 60}, {ClassBonds, 40}}
	case TypeBrokerage, TypeRothIRA, TypeHSA:
		return []Holding{{ClassLargeCap, 100}}
	}
	return nil
}

// CheckAllocation reports why the allocation does not suit an account of the type in the record: a type that holds none, a percent that is not above 0, a class twice or unknown, or percents that do not sum to 100 within AllocationTolerance. An empty allocation is always valid.
func (r *Record) CheckAllocation(t AccountType, alloc []Holding) error {
	if len(alloc) == 0 {
		return nil
	}
	if !Investable(t) {
		return fmt.Errorf("an account of type %q cannot hold an allocation", t)
	}
	sum := 0.0
	seen := map[string]bool{}
	for _, h := range alloc {
		switch {
		case math.IsNaN(h.Percent) || h.Percent <= 0:
			return fmt.Errorf("the percent of class %q must be above 0", h.Class)
		case seen[h.Class]:
			return fmt.Errorf("class %q is in the allocation twice", h.Class)
		case r.Class(h.Class) == nil:
			return fmt.Errorf("there is no market class %q", h.Class)
		}
		seen[h.Class] = true
		sum += h.Percent
	}
	if math.Abs(sum-100) > AllocationTolerance {
		return fmt.Errorf("the allocation sums to %v%%, not 100%%", sum)
	}
	return nil
}

// Shock is a one-off change to a class on a date: Change is a fraction, -0.3 for a 30% drop.
type Shock struct {
	Class  string
	Date   Date
	Change float64
}

// Weight is one class's share of an allocation.
type Weight struct {
	Class string
	Share float64
}

// Rebalance is a dated change of an account's allocation: on the date its holdings are traded to the Shares of Allocation, which sum to 1, and money paid in after it is split that way.
type Rebalance struct {
	Account    AccountID
	Date       Date
	Allocation []Weight
}
