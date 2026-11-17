// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

func TestRaiseFactorMemoMatchesTheWalk(t *testing.T) {
	today := d(2026, time.January, 1)
	g := model.MustSchedule(
		model.Segment{From: model.MinDate, End: model.Ptr(d(2030, time.December, 31)), Value: 0.03},
		model.Segment{From: d(2032, time.January, 1), Value: -2},
	)
	for _, c := range []struct {
		name string
		item model.BudgetItem
	}{
		{"anchored on today", model.BudgetItem{ID: "a"}},
		{"started before today", model.BudgetItem{ID: "b", Start: model.Ptr(d(2023, time.May, 10))}},
		{"started after today", model.BudgetItem{ID: "c", Start: model.Ptr(d(2027, time.March, 1))}},
		{"leap day", model.BudgetItem{ID: "e", Start: model.Ptr(d(2024, time.February, 29))}},
	} {
		t.Run(c.name, func(t *testing.T) {
			memo := raiseMemo{}
			// The dates go forward and back, so the series is extended and then read from the middle.
			for _, on := range []model.Date{d(2026, time.June, 1), d(2040, time.February, 28), d(2027, time.February, 28), d(2028, time.February, 29), d(2029, time.March, 1), d(2035, time.December, 31), d(2025, time.January, 1), d(2033, time.January, 1)} {
				want := raiseFactor(c.item, today, on, g, nil)
				if got := raiseFactor(c.item, today, on, g, memo); got != want {
					t.Errorf("%v: memoized %v, walked %v", on, got, want)
				}
			}
		})
	}
	// The raises of the 2027 and 2028 anniversaries of a start today at 3%, and none before the first anniversary.
	it := model.BudgetItem{ID: "a"}
	if got := raiseFactor(it, today, d(2026, time.December, 31), g, raiseMemo{}); got != 1 {
		t.Errorf("before the first anniversary: %v", got)
	}
	if got, want := raiseFactor(it, today, d(2028, time.January, 1), g, raiseMemo{}), 1.03*1.03; got != want {
		t.Errorf("two anniversaries: %v, want %v", got, want)
	}
}
