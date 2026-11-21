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

func TestBreakdownWindowFollowsSettledCursor(t *testing.T) {
	today := model.D(2026, time.November, 8)
	c := cursorState{lock: today}
	var iv bkInterval
	hover := today.Add(100)
	// The pointer comes onto the plot and crosses days: the panel stays on the lock until it rests.
	for i := range restFrames {
		c.track(true, hover.Add(i))
		if _, to := iv.window(c.settled()); to != today {
			t.Errorf("frame %d of moving: window ends %v, want the lock", i, to)
		}
	}
	for range restFrames + 1 {
		c.track(true, hover)
	}
	if _, to := iv.window(c.settled()); to != hover {
		t.Errorf("at rest: window ends %v, want the hover date", to)
	}
	// Moving on keeps the day it last rested on.
	c.track(true, hover.Add(50))
	if _, to := iv.window(c.settled()); to != hover {
		t.Errorf("moving after rest: window ends %v, want %v", to, hover)
	}
	if _, to := iv.window(c.effective()); to != hover.Add(50) {
		t.Errorf("the effective cursor ends %v, want the day under the pointer", to)
	}
	c.track(false, 0)
	if _, to := iv.window(c.settled()); to != today {
		t.Errorf("away: window ends %v, want the lock", to)
	}
}

func TestSetLockWhileHoveringMovesThePanelAtOnce(t *testing.T) {
	today := model.D(2026, time.November, 8)
	c := cursorState{lock: today}
	day := today.Add(40)
	c.track(true, day)
	c.setLock(day)
	if c.lock != day || c.settled() != day {
		t.Errorf("lock %v, settled %v, want %v", c.lock, c.settled(), day)
	}
}

func TestCursorMarks(t *testing.T) {
	c := cursorState{lock: 5}
	for _, tc := range []struct {
		hovering, lockInView, wantLock, wantHover bool
	}{
		{false, true, true, false},   // the locked marker alone
		{true, true, true, true},     // both
		{true, false, false, true},   // the pointer alone, the lock out of view
		{false, false, false, false}, // none
	} {
		c.track(tc.hovering, 9)
		if lock, hover := c.marks(tc.lockInView); lock != tc.wantLock || hover != tc.wantHover {
			t.Errorf("hovering %v, lock in view %v: marks %v, %v", tc.hovering, tc.lockInView, lock, hover)
		}
	}
}
