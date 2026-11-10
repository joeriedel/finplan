// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// payments returns each date from..to on which the gym item paid, with the amount.
func payments(t *testing.T, rec *model.Record, proj string, from, to model.Date) map[model.Date]float64 {
	t.Helper()
	chk := model.DefaultAssetID("chk")
	out := map[model.Date]float64{}
	for day := from; day <= to; day++ {
		if got := sample(t, rec, proj, chk, day-1).Amount - sample(t, rec, proj, chk, day).Amount; !near(got, 0) {
			out[day] = got
		}
	}
	return out
}

func TestItemRecurrencesPayOnTheirDates(t *testing.T) {
	start := d(2027, time.January, 15)
	for _, c := range []struct {
		name string
		r    model.Recurrence
		want []model.Date
	}{
		{"quarterly", model.Recurrence{Kind: model.Quarterly}, []model.Date{d(2027, 1, 15), d(2027, 4, 15), d(2027, 7, 15), d(2027, 10, 15)}},
		{"every 3 weeks", model.Recurrence{Kind: model.Custom, Count: 3, Unit: model.UnitWeeks}, []model.Date{start, start.Add(21), start.Add(42), start.Add(63), start.Add(84), start.Add(105), start.Add(126), start.Add(147)}},
		{"weekly", model.Recurrence{Kind: model.Weekly}, []model.Date{start, start.Add(7), start.Add(14), start.Add(21), start.Add(28), start.Add(35), start.Add(42), start.Add(49), start.Add(56), start.Add(63)}},
	} {
		rec := gymRecord(t, start, 0, false)
		rec.Budget.Items[0].Recurrence = c.r
		got := payments(t, rec, "", d(2027, 1, 1), d(2027, 3, 20))
		if c.name == "quarterly" {
			got = payments(t, rec, "", d(2027, 1, 1), d(2027, 12, 31))
		}
		for _, day := range c.want {
			if day <= d(2027, 3, 20) || c.name == "quarterly" {
				if !near(got[day], 100) {
					t.Errorf("%s: paid %v on %v, want 100 (all: %v)", c.name, got[day], day, got)
				}
			}
		}
		n := 0
		for day := range got {
			if day >= start {
				n++
			}
		}
		want := 0
		for _, day := range c.want {
			if day <= d(2027, 3, 20) || c.name == "quarterly" {
				want++
			}
		}
		if n != want {
			t.Errorf("%s: paid %d times, want %d (%v)", c.name, n, want, got)
		}
	}
}

func TestMonthEndItemPaysOnTheLastDay(t *testing.T) {
	rec := gymRecord(t, d(2027, time.January, 1), 0, false)
	rec.Budget.Items[0].Recurrence = model.Recurrence{Kind: model.Monthly, Day: 31}
	got := payments(t, rec, "", d(2027, 1, 1), d(2027, 6, 30))
	for _, day := range []model.Date{d(2027, 1, 31), d(2027, 2, 28), d(2027, 3, 31), d(2027, 4, 30), d(2027, 5, 31), d(2027, 6, 30)} {
		if !near(got[day], 100) {
			t.Errorf("paid %v on %v, want 100", got[day], day)
		}
	}
	if len(got) != 6 {
		t.Errorf("paid on %d days, want 6: %v", len(got), got)
	}
}

func TestAmountScheduleIsPaidAtEachAmount(t *testing.T) {
	rec := gymRecord(t, d(2027, time.January, 1), 0, false)
	rec.Budget.Items[0].Amount = model.MustSchedule(
		model.Segment{From: model.MinDate, End: model.Ptr(d(2029, time.July, 31)), Value: 1400},
		model.Segment{From: d(2029, time.August, 1), Value: 600},
	)
	for day, want := range map[model.Date]float64{d(2027, 3, 10): 1400, d(2029, 7, 10): 1400, d(2029, 8, 10): 600, d(2030, 1, 10): 600} {
		if got := paid(t, rec, day); !near(got, want) {
			t.Errorf("paid on %v: got %v, want %v", day, got, want)
		}
	}
	// An amount modification overlays the item's own schedule: the later segment stays where the mod does not reach.
	must(t, rec.AddProjection(&model.Projection{Name: "p", Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "gym"}, Property: model.PropAmount, Values: model.MustSchedule(model.Segment{From: d(2028, time.January, 1), End: model.Ptr(d(2028, time.December, 31)), Value: 1000})},
	}}))
	chk := model.DefaultAssetID("chk")
	for day, want := range map[model.Date]float64{d(2027, 3, 10): 1400, d(2028, 3, 10): 1000, d(2029, 3, 10): 1400, d(2029, 9, 10): 600} {
		if got := sample(t, rec, "p", chk, day-1).Amount - sample(t, rec, "p", chk, day).Amount; !near(got, want) {
			t.Errorf("with the mod, paid on %v: got %v, want %v", day, got, want)
		}
	}
}

func TestGrowthScheduleStepsAtTheRateInEffect(t *testing.T) {
	rec := gymRecord(t, d(2026, time.March, 1), 0, false)
	rec.Budget.Items[0].Growth = model.MustSchedule(
		model.Segment{From: model.MinDate, End: model.Ptr(d(2027, time.December, 31)), Value: 0.10},
		model.Segment{From: d(2028, time.January, 1), Value: 0.05},
	)
	for day, want := range map[model.Date]float64{d(2027, 3, 10): 110, d(2028, 3, 10): 115.5} {
		if got := paid(t, rec, day); !near(got, want) {
			t.Errorf("paid on %v: got %v, want %v", day, got, want)
		}
	}
}
