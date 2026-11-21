// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import "github.com/joeriedel/finplan/internal/model"

// restFrames is how many frames in a row the pointer must stay on one day for the panels that cost an engine run, the breakdown, to follow it there.
const restFrames = 2

// cursorState is the Graph's cursors: the locked one, which a click on the plot sets and which stays where it is, and the one that follows the pointer while it is over the plot.
type cursorState struct {
	lock     model.Date
	hover    model.Date
	hovering bool
	// rest is how many frames the pointer has stayed on hover, and shown the day the panels follow while hovering: the day the pointer last rested on, or the lock if it has not since it came onto the plot.
	rest  int
	shown model.Date
}

// effective is the date under the pointer while hovering, else the locked date.
func (c *cursorState) effective() model.Date {
	if c.hovering {
		return c.hover
	}
	return c.lock
}

// settled is the date the breakdown panel follows: the locked date, or while hovering the day the pointer last rested on, so that it does not run the engine for every day the pointer crosses.
func (c *cursorState) settled() model.Date {
	if c.hovering {
		return c.shown
	}
	return c.lock
}

// track records whether the pointer is over the plot and the day under it.
func (c *cursorState) track(hovering bool, day model.Date) {
	switch {
	case !hovering:
	case !c.hovering:
		c.rest, c.shown = 0, c.lock
	case day == c.hover:
		c.rest++
	default:
		c.rest = 0
	}
	if hovering && c.rest >= restFrames {
		c.shown = day
	}
	c.hovering, c.hover = hovering, day
}

// setLock locks the cursor at the day, a click there.
func (c *cursorState) setLock(day model.Date) {
	c.lock = day
	if c.hovering {
		c.shown = day
	}
}

// marks says which cursors to draw: the locked one whenever its day is within the view, and the pointer's while it is over the plot.
func (c *cursorState) marks(lockInView bool) (lock, hover bool) {
	return lockInView, c.hovering
}

// isClick reports whether a left button release over the plot ends a click: a press and release without dragging, and not the end of a Shift band drag.
func isClick(released, dragged, bandDrag bool) bool {
	return released && !dragged && !bandDrag
}
