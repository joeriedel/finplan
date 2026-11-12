// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// grow is the factor an annual rate compounds to over days.
func grow(rate float64, days model.Date) float64 {
	return math.Pow(1+rate, float64(days)/model.DaysPerYear)
}

// allocFixture is a checking account and a brokerage account "a" with 1000 on today, held 60% large cap and 40% bonds, and the shipped classes.
func allocFixture(t *testing.T) *model.Record {
	t.Helper()
	today := d(2026, time.January, 1)
	rec := model.NewRecord(today)
	rec.Classes = model.DefaultClasses()
	chk := &model.Account{ID: "chk", Type: model.TypeChecking, Tax: model.Taxable}
	chk.AddSnapshot(today, 5000)
	a := &model.Account{ID: "a", Type: model.TypeBrokerage, Tax: model.Taxable, StatedRate: 0.02,
		Allocation: []model.Holding{{Class: model.ClassLargeCap, Percent: 60}, {Class: model.ClassBonds, Percent: 40}}}
	a.AddSnapshot(today, 1000)
	must(t, rec.AddAccount(chk))
	must(t, rec.AddAccount(a))
	return rec
}

func TestAllocatedAccountGrowsAtTheBlend(t *testing.T) {
	rec := allocFixture(t)
	end := d(2027, time.January, 1)
	days := end - rec.Today
	want := 600*grow(0.10, days) + 400*grow(0.045, days)
	if got := sample(t, rec, "", model.DefaultAssetID("a"), end); !near(got.Amount, want) {
		t.Errorf("60/40 balance = %v, want %v", got.Amount, want)
	}
}

func TestClassGrowthModLowersAllocatedAccount(t *testing.T) {
	rec := allocFixture(t)
	addProjection(t, rec, "bearish", "", growthMod(model.KindClass, model.ClassLargeCap, 0.05))
	end := d(2027, time.January, 1)
	days := end - rec.Today
	base := sample(t, rec, "", model.DefaultAssetID("a"), end).Amount
	got := sample(t, rec, "bearish", model.DefaultAssetID("a"), end).Amount
	if want := 600*grow(0.05, days) + 400*grow(0.045, days); !near(got, want) || got >= base {
		t.Errorf("with large cap at 5%%: %v, want %v below %v", got, want, base)
	}
	addProjection(t, rec, "halved", "", model.Modification{
		Target: model.ObjectRef{Kind: model.KindClass, ID: model.ClassLargeCap}, Property: model.PropGrowth, Mode: model.ModeScale, Values: model.Constant(0.5),
	})
	if got, want := sample(t, rec, "halved", model.DefaultAssetID("a"), end).Amount, 600*grow(0.05, days)+400*grow(0.045, days); !near(got, want) {
		t.Errorf("with large cap scaled to half: %v, want %v", got, want)
	}
}

func TestBreakdownSplitsGrowthByClass(t *testing.T) {
	rec := allocFixture(t)
	end := d(2027, time.January, 1)
	days := end - rec.Today
	bd, err := Breakdown(rec, "", model.DefaultAssetID("a"), rec.Today, end)
	must(t, err)
	if len(bd.Lines) != 2 {
		t.Fatalf("lines: %+v", bd.Lines)
	}
	for i, w := range []struct {
		class string
		gain  float64
	}{{model.ClassLargeCap, 600 * (grow(0.10, days) - 1)}, {model.ClassBonds, 400 * (grow(0.045, days) - 1)}} {
		if l := bd.Lines[i]; l.Cause != CauseGrowth || l.Class != w.class || !near(l.Projected, w.gain) || l.Actual != 0 {
			t.Errorf("line %d = %+v, want growth of %v for %s", i, l, w.gain, w.class)
		}
	}
	if !near(bd.Total(), bd.End-bd.Start) {
		t.Errorf("lines sum to %v, the change is %v", bd.Total(), bd.End-bd.Start)
	}
}

func TestAccountWithoutAllocationIsUnchanged(t *testing.T) {
	plain := fixture(t)
	withClasses := fixture(t)
	withClasses.Classes = model.DefaultClasses()
	addProjection(t, withClasses, "mod", "", growthMod(model.KindClass, model.ClassLargeCap, 0.01))
	end := d(2027, time.January, 1)
	inv := model.DefaultAssetID("inv")
	want := sample(t, plain, "", inv, end).Amount
	for _, proj := range []string{"", "mod"} {
		if got := sample(t, withClasses, proj, inv, end).Amount; got != want {
			t.Errorf("projection %q: unallocated account = %v, want %v", proj, got, want)
		}
	}
	// 10500 on 2026-01-01 compounding at the stated 6% with the monthly 300 contributions: at least the plain growth of what it started with.
	if got := sample(t, plain, "", inv, end).Amount; got < 10500*grow(0.06, end-d(2026, time.January, 1)) {
		t.Errorf("unallocated account = %v, below its stated-rate growth", got)
	}
	bd, err := Breakdown(withClasses, "", inv, d(2026, time.January, 1), end)
	must(t, err)
	for _, l := range bd.Lines {
		if l.Class != "" {
			t.Errorf("a line of an unallocated account names class %q", l.Class)
		}
	}
}

func TestAllocatedFlowsSplitInAndComeOutProRata(t *testing.T) {
	rec := allocFixture(t)
	in, out := d(2026, time.July, 1), d(2026, time.October, 1)
	rec.Budget.Items = []model.BudgetItem{
		{ID: "in", Kind: model.ItemTransfer, Amount: model.Constant(500), Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: in}, From: "chk", To: "a"},
		{ID: "out", Kind: model.ItemTransfer, Amount: model.Constant(300), Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: out}, From: "a", To: "chk"},
	}
	lc := 600*grow(0.10, in-rec.Today) + 300
	bonds := 400*grow(0.045, in-rec.Today) + 200
	lc, bonds = lc*grow(0.10, out-in), bonds*grow(0.045, out-in)
	total := lc + bonds
	lc, bonds = lc-300*lc/total, bonds-300*bonds/total
	end := d(2027, time.January, 1)
	want := lc*grow(0.10, end-out) + bonds*grow(0.045, end-out)
	if got := sample(t, rec, "", model.DefaultAssetID("a"), end); !near(got.Amount, want) {
		t.Errorf("balance = %v, want %v", got.Amount, want)
	}
}
