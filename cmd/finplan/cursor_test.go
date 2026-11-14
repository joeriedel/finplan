// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

func TestCursorHoverLockFallback(t *testing.T) {
	today := model.D(2026, time.November, 8)
	c := cursorState{lock: today}
	if c.effective() != today {
		t.Errorf("default = %v, want today", c.effective())
	}
	day := model.D(2027, time.March, 1)
	c.track(true, day)
	if c.effective() != day {
		t.Errorf("hovering = %v, want %v", c.effective(), day)
	}
	if c.lock != today {
		t.Errorf("hover moved the lock to %v", c.lock)
	}
	c.lock = day
	c.track(true, model.D(2028, time.January, 1))
	c.track(false, model.D(2029, time.January, 1))
	if c.effective() != day {
		t.Errorf("after leaving = %v, want the lock %v", c.effective(), day)
	}
}

func TestIsClick(t *testing.T) {
	for _, c := range []struct {
		released, dragged, band, want bool
	}{
		{true, false, false, true},
		{true, true, false, false},
		{true, false, true, false},
		{false, false, false, false},
	} {
		if got := isClick(c.released, c.dragged, c.band); got != c.want {
			t.Errorf("isClick(%v, %v, %v) = %v", c.released, c.dragged, c.band, got)
		}
	}
}

func TestBreakdownWindowFollowsEffectiveCursor(t *testing.T) {
	today := model.D(2026, time.November, 8)
	c := cursorState{lock: today}
	var iv bkInterval
	c.track(true, today.Add(100))
	if _, to := iv.window(c.effective()); to != today.Add(100) {
		t.Errorf("window ends %v, want the hover date", to)
	}
	c.track(false, 0)
	if _, to := iv.window(c.effective()); to != today {
		t.Errorf("window ends %v, want the lock", to)
	}
}
