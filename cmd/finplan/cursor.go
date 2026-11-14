// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import "github.com/joeriedel/finplan/internal/model"

// cursorState is the Graph's cursor: it follows the pointer while it is over the plot and otherwise rests at the locked date, which a click on the plot sets.
type cursorState struct {
	lock     model.Date
	hover    model.Date
	hovering bool
}

// effective is the date under the pointer while hovering, else the locked date.
func (c *cursorState) effective() model.Date {
	if c.hovering {
		return c.hover
	}
	return c.lock
}

// track records whether the pointer is over the plot and the day under it.
func (c *cursorState) track(hovering bool, day model.Date) {
	c.hovering, c.hover = hovering, day
}

// isClick reports whether a left button release over the plot ends a click: a press and release without dragging, and not the end of a Shift band drag.
func isClick(released, dragged, bandDrag bool) bool {
	return released && !dragged && !bandDrag
}
