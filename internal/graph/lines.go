// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"slices"

	"github.com/joeriedel/finplan/internal/model"
)

// LineID identifies a line for as long as the list lives. Ids are never reused.
type LineID int

// Line is one plotted series: an asset under a projection.
type Line struct {
	ID    LineID
	Asset model.AssetID
	// Pinned lines keep Projection; unpinned lines follow the switcher.
	Pinned     bool
	Projection string
	Color      Color
	Visible    bool
	// MonteCarlo draws the percentile bands and the median of many random runs beneath the line.
	MonteCarlo bool
}

// Lines is the Graph tab's line list with the projection switcher and the selection.
type Lines struct {
	lines    []Line
	nextID   LineID
	switcher string
	selected LineID
	solo     map[LineID]bool
	bands    []Band
	nextBand BandID
}

// NewLines returns an empty list whose switcher shows the projection.
func NewLines(switcher string) *Lines { return &Lines{nextID: 1, nextBand: 1, switcher: switcher} }

// Switcher returns the projection unpinned lines follow.
func (l *Lines) Switcher() string { return l.switcher }

// SetSwitcher changes the projection unpinned lines follow. Pinned lines are unaffected.
func (l *Lines) SetSwitcher(projection string) { l.switcher = projection }

// Add appends a visible line and returns its id. An unpinned line follows the switcher and ignores
// projection. The colour is base, lightened for each line already showing the asset so that duplicates
// stay apart.
func (l *Lines) Add(asset model.AssetID, pinned bool, projection string, base Color) LineID {
	dups := 0
	for _, o := range l.lines {
		if o.Asset == asset {
			dups++
		}
	}
	ln := Line{ID: l.nextID, Asset: asset, Pinned: pinned, Projection: projection, Color: base.Lighten(min(0.3*float64(dups), 0.6)), Visible: true}
	if !pinned {
		ln.Projection = ""
	}
	l.nextID++
	l.lines = append(l.lines, ln)
	return ln.ID
}

// RenameProjection points the switcher and every pinned line that names the projection from at to instead.
func (l *Lines) RenameProjection(from, to string) {
	if l.switcher == from {
		l.switcher = to
	}
	for i := range l.lines {
		if l.lines[i].Pinned && l.lines[i].Projection == from {
			l.lines[i].Projection = to
		}
	}
}

func (l *Lines) index(id LineID) int {
	for i, ln := range l.lines {
		if ln.ID == id {
			return i
		}
	}
	return -1
}

// Remove deletes the line, clears its selection and solo, and removes the bands that use it.
func (l *Lines) Remove(id LineID) {
	i := l.index(id)
	if i < 0 {
		return
	}
	l.lines = append(l.lines[:i], l.lines[i+1:]...)
	if l.selected == id {
		l.selected = 0
	}
	delete(l.solo, id)
	l.bands = slices.DeleteFunc(l.bands, func(b Band) bool { return b.Upper == id || b.Lower == id })
}

// Get returns the line with the id.
func (l *Lines) Get(id LineID) (Line, bool) {
	if i := l.index(id); i >= 0 {
		return l.lines[i], true
	}
	return Line{}, false
}

// All returns a copy of the lines in order.
func (l *Lines) All() []Line { return append([]Line(nil), l.lines...) }

// SetColor recolours the line.
func (l *Lines) SetColor(id LineID, c Color) {
	if i := l.index(id); i >= 0 {
		l.lines[i].Color = c
	}
}

// SetVisible shows or hides the line.
func (l *Lines) SetVisible(id LineID, v bool) {
	if i := l.index(id); i >= 0 {
		l.lines[i].Visible = v
	}
}

// SetMonteCarlo turns the line's Monte Carlo bands on or off.
func (l *Lines) SetMonteCarlo(id LineID, on bool) {
	if i := l.index(id); i >= 0 {
		l.lines[i].MonteCarlo = on
	}
}

// Effective returns the projection the line is drawn under: its pin, or else the switcher's.
func (l *Lines) Effective(id LineID) (string, bool) {
	i := l.index(id)
	if i < 0 {
		return "", false
	}
	if l.lines[i].Pinned {
		return l.lines[i].Projection, true
	}
	return l.switcher, true
}

// TogglePin pins an unpinned line to the projection the switcher shows now, and unpins a pinned one
// so that it follows the switcher again.
func (l *Lines) TogglePin(id LineID) {
	i := l.index(id)
	if i < 0 {
		return
	}
	ln := &l.lines[i]
	if ln.Pinned {
		ln.Pinned, ln.Projection = false, ""
		return
	}
	ln.Pinned, ln.Projection = true, l.switcher
}

// Select selects the line; an unknown id clears the selection.
func (l *Lines) Select(id LineID) {
	if l.index(id) < 0 {
		id = 0
	}
	l.selected = id
}

// Selected returns the selected line, if any.
func (l *Lines) Selected() (LineID, bool) { return l.selected, l.selected != 0 }

// ToggleSolo solos the line, or unsolos it if it is soloed. An unknown id is ignored.
func (l *Lines) ToggleSolo(id LineID) {
	if l.index(id) < 0 {
		return
	}
	if l.solo[id] {
		delete(l.solo, id)
		return
	}
	if l.solo == nil {
		l.solo = map[LineID]bool{}
	}
	l.solo[id] = true
}

// ClearSolo unsolos every line. Line.Visible is untouched.
func (l *Lines) ClearSolo() { l.solo = nil }

// Soloed reports whether the line is soloed.
func (l *Lines) Soloed(id LineID) bool { return l.solo[id] }

// SoloCount returns the number of soloed lines.
func (l *Lines) SoloCount() int { return len(l.solo) }

// Drawn reports whether the line is drawn. While any line is soloed, exactly the soloed lines are drawn,
// whatever their Visible; otherwise Visible decides.
func (l *Lines) Drawn(id LineID) bool {
	i := l.index(id)
	if i < 0 {
		return false
	}
	if len(l.solo) > 0 {
		return l.solo[id]
	}
	return l.lines[i].Visible
}
