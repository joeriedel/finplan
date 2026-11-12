// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
)

func taxRowValue(t *testing.T, v taxView, label string) string {
	t.Helper()
	for _, r := range v.Rows {
		if r.Label == label {
			return r.Value
		}
	}
	t.Fatalf("no row %q in %+v", label, v.Rows)
	return ""
}

// Scenario 1: the demo's 2026 and 2027 under Base show wages, the taxes and what was withheld.
func TestTaxViewShowsDemoYears(t *testing.T) {
	a := demoApp(t)
	for _, year := range []int{2026, 2027} {
		v := taxViewOf(a.rec, demodata.ProjBase, year)
		if v.Err != "" {
			t.Fatalf("%d: %s", year, v.Err)
		}
		for _, label := range []string{"Wages", "Federal tax", "State tax (VA)", "FICA", "Withheld", "Federal taxable income"} {
			if got := taxRowValue(t, v, label); got == "$0" {
				t.Errorf("%d %s is $0", year, label)
			}
		}
	}
}

// Scenario 2: a projection with higher 401k contributions deducts more and so taxes less income.
func TestTaxViewFollowsTheProjection(t *testing.T) {
	a := demoApp(t)
	base := taxViewOf(a.rec, demodata.ProjBase, 2027)
	more := taxViewOf(a.rec, demodata.ProjMax401k, 2027)
	if base.Err != "" || more.Err != "" {
		t.Fatalf("errors %q, %q", base.Err, more.Err)
	}
	b, err := engine.TaxYearOf(a.rec, demodata.ProjBase, 2027)
	if err != nil {
		t.Fatal(err)
	}
	m, err := engine.TaxYearOf(a.rec, demodata.ProjMax401k, 2027)
	if err != nil {
		t.Fatal(err)
	}
	if m.K401 <= b.K401 || m.Result.FederalTaxable >= b.Result.FederalTaxable {
		t.Errorf("401k %v -> %v, federal taxable %v -> %v", b.K401, m.K401, b.Result.FederalTaxable, m.Result.FederalTaxable)
	}
	if taxRowValue(t, base, "Pre-tax deductions") == taxRowValue(t, more, "Pre-tax deductions") {
		t.Error("pre-tax deductions did not change")
	}
	if taxRowValue(t, base, "Federal taxable income") == taxRowValue(t, more, "Federal taxable income") {
		t.Error("federal taxable income did not change")
	}
}

// The view defaults to the first projection and this year, and keeps its answers until the record is replaced.
func TestTaxStateDefaultsAndMemo(t *testing.T) {
	a := demoApp(t)
	var s taxState
	s.view(a.rec)
	if s.projection != demodata.ProjBase || s.year != 2026 {
		t.Errorf("defaults %q, %d", s.projection, s.year)
	}
	if len(s.views) != 1 {
		t.Errorf("%d memoized views", len(s.views))
	}
	s.view(a.rec)
	if len(s.views) != 1 {
		t.Errorf("%d memoized views after a second draw", len(s.views))
	}
	s.year = 2027
	s.view(a.rec)
	if len(s.views) != 2 {
		t.Errorf("%d memoized views for two years", len(s.views))
	}
	s.view(demoApp(t).rec)
	if len(s.views) != 1 {
		t.Errorf("%d memoized views for a new record", len(s.views))
	}
}

// A year the rules cannot compute is reported as text.
func TestTaxViewReportsProblems(t *testing.T) {
	a := demoApp(t)
	v := taxViewOf(a.rec, "no such projection", 2026)
	if v.Err == "" || strings.Contains(v.Err, "panic") {
		t.Errorf("error %q", v.Err)
	}
}
