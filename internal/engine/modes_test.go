// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

func rentMod(mode model.Mode, from model.Date, v float64) model.Modification {
	return model.Modification{
		Target:   model.ObjectRef{Kind: model.KindBudgetItem, ID: "rent"},
		Property: model.PropAmount,
		Mode:     mode,
		Values:   model.MustSchedule(model.Segment{From: from, Value: v}),
	}
}

// rentDiff is the checking balance on 2026-04-30 under a projection with the mods, minus the base's. Rent is 200 on the 20th of each month.
func rentDiff(t *testing.T, rec *model.Record, mods ...model.Modification) float64 {
	t.Helper()
	must(t, rec.AddProjection(&model.Projection{Name: "p", Mods: mods}))
	chk := model.DefaultAssetID("chk")
	day := d(2026, time.April, 30)
	return sample(t, rec, "p", chk, day).Amount - sample(t, rec, "", chk, day).Amount
}

func TestScaleModificationsOfAnItemAmount(t *testing.T) {
	feb, mar := d(2026, time.February, 1), d(2026, time.March, 1)
	for _, tc := range []struct {
		name string
		mods []model.Modification
		want float64
	}{
		// Rent on Mar 20 and Apr 20 is halved: 100 each less is paid.
		{"scale over the base", []model.Modification{rentMod(model.ModeScale, mar, 0.5)}, 200},
		// Feb 20 is 400 (200 more paid); Mar 20 and Apr 20 are 400 x 0.5 = 200, as the base.
		{"scale over an earlier set", []model.Modification{rentMod(model.ModeSet, feb, 400), rentMod(model.ModeScale, mar, 0.5)}, -200},
		// Feb 20 is 100 (100 less paid); Mar 20 and Apr 20 are 300 (100 more paid each).
		{"set after a scale", []model.Modification{rentMod(model.ModeScale, feb, 0.5), rentMod(model.ModeSet, mar, 300)}, -100},
		{"a mode left empty is set", []model.Modification{rentMod("", mar, 100)}, 200},
		{"scale to zero ends it", []model.Modification{rentMod(model.ModeScale, mar, 0)}, 400},
	} {
		if got := rentDiff(t, fixture(t), tc.mods...); !near(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestScaleFollowsTheBase(t *testing.T) {
	rec := fixture(t)
	rec.Budget.Items[1].Amount = model.Constant(400) // rent
	// Halved from Mar 1, 400 becomes 200 on Mar 20 and Apr 20.
	if got := rentDiff(t, rec, rentMod(model.ModeScale, d(2026, time.March, 1), 0.5)); !near(got, 400) {
		t.Errorf("scale of the changed base: got %v, want 400", got)
	}
}

func TestScaleInflationAndAccountGrowth(t *testing.T) {
	rec := fixture(t)
	rec.Globals[model.PropInflation] = model.Constant(0.10)
	rec.Budget.Items[1].Indexed = true
	inf := model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Mode: model.ModeScale, Values: model.Constant(0)}
	must(t, rec.AddProjection(&model.Projection{Name: "none", Mods: []model.Modification{inf}}))
	bd, err := Breakdown(rec, "none", model.DefaultAssetID("chk"), d(2026, time.January, 1), d(2027, time.January, 31))
	must(t, err)
	if l := line(bd, CauseExpense); !near(l.Projected, -13*200.0) {
		t.Errorf("inflation scaled to zero: got %v", l.Projected)
	}

	// The 401k grows 6%; halved, it grows 3%.
	inv := model.DefaultAssetID("inv")
	day := d(2027, time.January, 1)
	half := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: "inv"}, Property: model.PropGrowth, Mode: model.ModeScale, Values: model.Constant(0.5)}
	must(t, rec.AddProjection(&model.Projection{Name: "half", Mods: []model.Modification{half}}))
	set := half
	set.Mode, set.Values = model.ModeSet, model.Constant(0.03)
	must(t, rec.AddProjection(&model.Projection{Name: "set", Mods: []model.Modification{set}}))
	if a, b := sample(t, rec, "half", inv, day).Amount, sample(t, rec, "set", inv, day).Amount; !near(a, b) {
		t.Errorf("scaled growth %v differs from set growth %v", a, b)
	}
	if a, b := sample(t, rec, "half", inv, day).Amount, sample(t, rec, "", inv, day).Amount; !(a < b) {
		t.Errorf("halved growth %v is not below the base %v", a, b)
	}
}

func TestSkippedScaleOfMissingItem(t *testing.T) {
	rec := fixture(t)
	gone := rentMod(model.ModeScale, d(2026, time.March, 1), 0.5)
	gone.Target.ID = "gone"
	growth := gone
	growth.Property = model.PropGrowth
	must(t, rec.AddProjection(&model.Projection{Name: "p", Mods: []model.Modification{gone, growth}}))
	res, err := Balances(rec, "p", model.DefaultAssetID("chk"), []model.Date{d(2026, time.March, 31)})
	must(t, err)
	if len(res.Skipped) != 2 || res.Skipped[0].Reason != "budget item not found" || res.Skipped[1].Reason != "budget item not found" {
		t.Errorf("skipped: got %+v", res.Skipped)
	}
}

// gymRecord has one budget item, an expense of 100 on the 10th of each month from the start date, and the checking balance 1000 on 2026-01-01.
func gymRecord(t *testing.T, start model.Date, growth float64, indexed bool) *model.Record {
	t.Helper()
	rec := fixture(t)
	if indexed {
		rec.Globals[model.PropInflation] = model.Constant(0.02)
	}
	rec.Budget.Items = []model.BudgetItem{{
		ID: "gym", Kind: model.ItemExpense, Amount: model.Constant(100), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 10},
		Start: &start, Account: "chk", Indexed: indexed,
	}}
	if growth != 0 {
		rec.Budget.Items[0].Growth = model.Constant(growth)
	}
	return rec
}

// gymBalance is the checking balance on upTo that raises on the anniversaries, each stepping the amount by rate(anniversary), should leave.
func gymBalance(rec *model.Record, annivs []model.Date, rate func(a model.Date) float64, upTo model.Date) float64 {
	it := rec.Budget.Items[0]
	bal := 1000.0
	for y, m := 2026, time.January; ; {
		occ := d(y, m, 10)
		if occ > upTo {
			return bal
		}
		if occ >= *it.Start {
			amt, _ := it.Amount.At(occ)
			for _, a := range annivs {
				if a <= occ {
					amt *= 1 + rate(a)
				}
			}
			if it.Indexed {
				amt *= math.Pow(1.02, float64(occ-rec.Today)/model.DaysPerYear)
			}
			bal -= amt
		}
		if m++; m > time.December {
			y, m = y+1, time.January
		}
	}
}

// paid is what the gym item takes from checking on day, a day it happens on.
func paid(t *testing.T, rec *model.Record, day model.Date) float64 {
	t.Helper()
	chk := model.DefaultAssetID("chk")
	return sample(t, rec, "", chk, day-1).Amount - sample(t, rec, "", chk, day).Amount
}

func gymMod(prop string, mode model.Mode, from model.Date, v float64) model.Modification {
	return model.Modification{
		Target:   model.ObjectRef{Kind: model.KindBudgetItem, ID: "gym"},
		Property: prop,
		Mode:     mode,
		Values:   model.MustSchedule(model.Segment{From: from, Value: v}),
	}
}

func TestRaisesStepOnTheStartAnniversary(t *testing.T) {
	chk := model.DefaultAssetID("chk")
	mar := func(y int) model.Date { return d(y, time.March, 1) }
	annivs := []model.Date{mar(2027), mar(2028), mar(2029)}
	flat := func(g float64) func(model.Date) float64 { return func(model.Date) float64 { return g } }
	end := d(2029, time.June, 30)

	rec := gymRecord(t, mar(2026), 0.03, false)
	if got, want := sample(t, rec, "", chk, end).Amount, gymBalance(rec, annivs, flat(0.03), end); !near(got, want) {
		t.Errorf("3%% raises: got %v, want %v", got, want)
	}
	// The step is on the anniversary, not continuous: Feb 10 2027 pays 100, Mar 10 2027 pays 103, Mar 10 2028 pays 106.09.
	for day, want := range map[model.Date]float64{d(2027, time.February, 10): 100, d(2027, time.March, 10): 103, d(2028, time.February, 10): 103, d(2028, time.March, 10): 106.09} {
		if got := paid(t, rec, day); !near(got, want) {
			t.Errorf("paid on %v: got %v, want %v", day, got, want)
		}
	}

	// Raises multiply with inflation indexing.
	rec = gymRecord(t, mar(2026), 0.03, true)
	if got, want := sample(t, rec, "", chk, end).Amount, gymBalance(rec, annivs, flat(0.03), end); !near(got, want) {
		t.Errorf("raises and inflation: got %v, want %v", got, want)
	}

	// A growth modification sets the rate from its date; a scale one multiplies the rate beneath.
	rec = gymRecord(t, mar(2026), 0.03, false)
	must(t, rec.AddProjection(&model.Projection{Name: "set", Mods: []model.Modification{gymMod(model.PropGrowth, model.ModeSet, d(2028, time.January, 1), 0.05)}}))
	must(t, rec.AddProjection(&model.Projection{Name: "scale", Mods: []model.Modification{gymMod(model.PropGrowth, model.ModeScale, d(2028, time.January, 1), 2)}}))
	must(t, rec.AddProjection(&model.Projection{Name: "gone", Mods: []model.Modification{gymMod(model.PropGrowth, model.ModeSet, d(2028, time.January, 1), 0)}}))
	from2028 := func(g float64) func(model.Date) float64 {
		return func(a model.Date) float64 {
			if a >= d(2028, time.January, 1) {
				return g
			}
			return 0.03
		}
	}
	for name, rate := range map[string]func(model.Date) float64{"set": from2028(0.05), "scale": from2028(0.06), "gone": from2028(0)} {
		if got, want := sample(t, rec, name, chk, end).Amount, gymBalance(rec, annivs, rate, end); !near(got, want) {
			t.Errorf("%s growth modification: got %v, want %v", name, got, want)
		}
	}

	// An item without a base rate takes the raises of a modification; scaling nothing leaves it without.
	rec = gymRecord(t, mar(2026), 0, false)
	must(t, rec.AddProjection(&model.Projection{Name: "set", Mods: []model.Modification{gymMod(model.PropGrowth, model.ModeSet, model.MinDate, 0.04)}}))
	must(t, rec.AddProjection(&model.Projection{Name: "scale", Mods: []model.Modification{gymMod(model.PropGrowth, model.ModeScale, model.MinDate, 2)}}))
	if got, want := sample(t, rec, "set", chk, end).Amount, gymBalance(rec, annivs, flat(0.04), end); !near(got, want) {
		t.Errorf("growth set on an item without any: got %v, want %v", got, want)
	}
	if got, want := sample(t, rec, "scale", chk, end).Amount, sample(t, rec, "", chk, end).Amount; !near(got, want) {
		t.Errorf("growth scaled where there is none: got %v, want %v", got, want)
	}
}

func TestRaiseAnniversaries(t *testing.T) {
	chk := model.DefaultAssetID("chk")
	end := d(2029, time.June, 30)

	// A start in the past counts only the anniversaries after today (2026-01-01), and a 29 February start falls on 28 February in other years.
	rec := gymRecord(t, d(2024, time.February, 29), 0.03, false)
	annivs := []model.Date{d(2026, time.February, 28), d(2027, time.February, 28), d(2028, time.February, 29), d(2029, time.February, 28)}
	flat := func(model.Date) float64 { return 0.03 }
	if got, want := sample(t, rec, "", chk, end).Amount, gymBalance(rec, annivs, flat, end); !near(got, want) {
		t.Errorf("29 February start: got %v, want %v", got, want)
	}
	// Feb 10 2026 is before the first anniversary, Mar 10 2026 after it.
	if got := paid(t, rec, d(2026, time.March, 10)); !near(got, 103) {
		t.Errorf("paid on Mar 10 2026: got %v, want 103", got)
	}
	if got := paid(t, rec, d(2026, time.February, 10)); !near(got, 100) {
		t.Errorf("paid on Feb 10 2026: got %v, want 100", got)
	}

	// An item that starts after today raises first a year after its start.
	rec = gymRecord(t, d(2027, time.March, 1), 0.03, false)
	annivs = []model.Date{d(2028, time.March, 1), d(2029, time.March, 1)}
	if got, want := sample(t, rec, "", chk, end).Amount, gymBalance(rec, annivs, flat, end); !near(got, want) {
		t.Errorf("a start after today: got %v, want %v", got, want)
	}
}

func TestRaiseNeverTurnsAnAmountNegative(t *testing.T) {
	chk := model.DefaultAssetID("chk")
	rec := gymRecord(t, d(2026, time.March, 1), -0.5, false)
	must(t, rec.AddProjection(&model.Projection{Name: "p", Mods: []model.Modification{gymMod(model.PropGrowth, model.ModeScale, model.MinDate, 4)}}))
	// The scale makes the rate -200%; the amount drops to nothing at the anniversary and stays there.
	before := sample(t, rec, "p", chk, d(2027, time.March, 9)).Amount
	for _, day := range []model.Date{d(2027, time.March, 10), d(2027, time.June, 30), d(2029, time.June, 30)} {
		if got := sample(t, rec, "p", chk, day).Amount; !near(got, before) {
			t.Errorf("balance on %v: got %v, want %v", day, got, before)
		}
	}
}
