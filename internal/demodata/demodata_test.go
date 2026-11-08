// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package demodata

import (
	"math"
	"testing"
	"time"
)

func TestGenerateShape(t *testing.T) {
	d := Generate(1)
	series := map[string][]float64{
		"debt": d.Debt, "cash": d.Cash, "brokerage": d.Brokerage,
		"401k": d.K401, "savings": d.Savings, "total": d.Total,
	}
	if len(d.Times) != Months {
		t.Fatalf("times: got %d samples, want %d", len(d.Times), Months)
	}
	for name, s := range series {
		if len(s) != Months {
			t.Errorf("%s: got %d samples, want %d", name, len(s), Months)
		}
	}
}

func TestGenerateMonthlyTimes(t *testing.T) {
	d := Generate(1)
	for i := 1; i < len(d.Times); i++ {
		if d.Times[i] <= d.Times[i-1] {
			t.Fatalf("times not increasing at %d", i)
		}
		cur := time.Unix(d.Times[i], 0).UTC()
		prev := time.Unix(d.Times[i-1], 0).UTC()
		if cur.Day() != 1 || cur.AddDate(0, -1, 0) != prev {
			t.Fatalf("sample %d is %v, not the month after %v", i, cur, prev)
		}
	}
}

func TestTotalIsSumOfComponents(t *testing.T) {
	d := Generate(1)
	for i := range d.Total {
		want := d.Debt[i] + d.Cash[i] + d.Brokerage[i] + d.K401[i] + d.Savings[i]
		if math.Abs(d.Total[i]-want) > 1e-6 {
			t.Fatalf("month %d: total %v, components sum to %v", i, d.Total[i], want)
		}
	}
}

func TestDebtIsNegativeAndAssetsPositive(t *testing.T) {
	d := Generate(1)
	for i := range d.Debt {
		if d.Debt[i] > 0 {
			t.Fatalf("month %d: debt %v is positive", i, d.Debt[i])
		}
		if d.Cash[i] <= 0 || d.Brokerage[i] <= 0 || d.K401[i] <= 0 || d.Savings[i] <= 0 {
			t.Fatalf("month %d: an asset balance is not positive", i)
		}
	}
}

func TestGenerateDeterministic(t *testing.T) {
	a, b := Generate(7), Generate(7)
	for i := range a.Total {
		if a.Total[i] != b.Total[i] {
			t.Fatalf("month %d differs between runs with the same seed", i)
		}
	}
}
