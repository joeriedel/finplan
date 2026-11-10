// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package timeline holds the edits of the timeline strip that shows a schedule as blocks on a time axis. It knows
// nothing of windows: every edit takes a schedule and returns one that model.NewSchedule accepted, or an error
// and no change. Segments are never deleted except by Delete; an edge dragged into a neighbour pushes it along.
package timeline

import (
	"errors"
	"fmt"
	"math"

	"github.com/joeriedel/finplan/internal/model"
)

var (
	// ErrFixedStart is returned for a segment that starts at the beginning of time, which has no start to move.
	ErrFixedStart = errors.New("timeline: a segment from the beginning of time has no start to move")
	// ErrLastSegment is returned when deleting the only segment, which would leave an empty schedule.
	ErrLastSegment = errors.New("timeline: the only segment cannot be deleted")
	// ErrNoSegment is returned for a segment index the schedule does not have.
	ErrNoSegment = errors.New("timeline: no such segment")
)

// MoveStart moves the start of segment i to the date. Where the segment touches the one before, this is the shared
// anchor: the previous segment's end follows it, it stops one day after the previous segment's From, and moving it past
// the segment's own last day pushes the segment along (see push). Where there is a gap before the segment, only its
// start moves, stopping at the day after the previous segment's End (where it becomes an anchor) and at its own last day.
func MoveStart(s model.Schedule, i int, to model.Date) (model.Schedule, error) {
	segs, err := load(s, i)
	if err != nil {
		return s, err
	}
	if segs[i].From == model.MinDate {
		return s, ErrFixedStart
	}
	switch {
	case i > 0 && segs[i-1].End == nil:
		push(segs, i, max(to, segs[i-1].From+1))
	default:
		to = max(to, model.MinDate+1)
		if i > 0 {
			to = max(to, *segs[i-1].End+1)
		}
		if end, bounded := LastDay(segs, i); bounded {
			to = min(to, end)
		}
		segs[i].From = to
	}
	return build(s, segs)
}

// MoveEnd moves the last day of segment i to the date, which is never before the segment's From. A segment that
// touches the next one gets an End of its own, which opens a gap; ending it on the day before the next From
// touches again, and further moves the next segment (see push). On the last segment the End is set.
func MoveEnd(s model.Schedule, i int, to model.Date) (model.Schedule, error) {
	segs, err := load(s, i)
	if err != nil {
		return s, err
	}
	g := &segs[i]
	to = max(to, g.From)
	switch {
	case i+1 == len(segs):
		g.End = &to
	case to >= segs[i+1].From-1:
		g.End = nil
		if to >= segs[i+1].From {
			push(segs, i+1, to+1)
		}
	default:
		g.End = &to
	}
	return build(s, segs)
}

// DropEnd moves the end of segment i as MoveEnd does for an edge dropped on the boundary date (the day after the last
// day), except that dropping the end of the last segment at or beyond hi, the right edge of the axis, clears it.
func DropEnd(s model.Schedule, i int, boundary, hi model.Date) (model.Schedule, error) {
	if i >= 0 && i == len(s.Segments())-1 && boundary >= hi {
		return ClearEnd(s)
	}
	return MoveEnd(s, i, boundary-1)
}

// ClearEnd removes the End of the last segment, so it runs on without end.
func ClearEnd(s model.Schedule) (model.Schedule, error) {
	segs, err := load(s, len(s.Segments())-1)
	if err != nil {
		return s, err
	}
	segs[len(segs)-1].End = nil
	return build(s, segs)
}

// Delete removes segment i. What it covered goes to the segment before if that has no End, else it becomes a gap.
func Delete(s model.Schedule, i int) (model.Schedule, error) {
	segs, err := load(s, i)
	if err != nil {
		return s, err
	}
	if len(segs) == 1 {
		return s, ErrLastSegment
	}
	return build(s, append(segs[:i], segs[i+1:]...))
}

// SetValue sets the value of segment i.
func SetValue(s model.Schedule, i int, v float64) (model.Schedule, error) {
	segs, err := load(s, i)
	if err != nil {
		return s, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return s, fmt.Errorf("timeline: %v is not a value", v)
	}
	segs[i].Value = v
	return build(s, segs)
}

// Split cuts segment i in two at the date, which must be after its From and on a day it covers. Both halves keep
// its value; the first touches the second.
func Split(s model.Schedule, i int, at model.Date) (model.Schedule, error) {
	segs, err := load(s, i)
	if err != nil {
		return s, err
	}
	end, bounded := LastDay(segs, i)
	if at <= segs[i].From || (bounded && at > end) {
		return s, fmt.Errorf("timeline: cannot split the segment at %v: it covers %v to %v", at, segs[i].From, end)
	}
	second := segs[i]
	second.From = at
	segs[i].End = nil
	return build(s, append(segs[:i+1], append([]model.Segment{second}, segs[i+1:]...)...))
}

// Append adds a segment starting at the date after the last, with the last one's value. The date must be after the
// last segment's From and End; it touches the last segment if the last has no End or ends the day before.
func Append(s model.Schedule, at model.Date) (model.Schedule, error) {
	segs := s.Segments()
	if len(segs) == 0 {
		return s, ErrNoSegment
	}
	last := segs[len(segs)-1]
	if at <= last.From || (last.End != nil && at <= *last.End) {
		return s, fmt.Errorf("timeline: a segment cannot start at %v: it must come after the last segment's start and end", at)
	}
	return build(s, append(segs, model.Segment{From: at, Value: last.Value}))
}

// NearestMonth returns the first day of the month nearest the date.
func NearestMonth(d model.Date) model.Date {
	y, m, _ := d.YMD()
	a, b := model.D(y, m, 1), model.D(y, m+1, 1)
	if d-a < b-d {
		return a
	}
	return b
}

// Span returns the dates a strip for the schedule should show: from before the earliest finite date (or the start,
// for a schedule that begins at the beginning of time) to well after the latest, leaving room to drag either way.
func Span(s model.Schedule, start model.Date) (lo, hi model.Date) {
	lo, hi = model.MaxDate, model.MinDate
	note := func(d model.Date) { lo, hi = min(lo, d), max(hi, d) }
	segs := s.Segments()
	for _, g := range segs {
		if g.From != model.MinDate {
			note(g.From)
		}
		if g.End != nil {
			note(*g.End + 1)
		}
	}
	if hi < lo {
		return start, start + 3*365
	}
	if segs[0].From == model.MinDate && start < lo {
		lo = start
	}
	pad := max(365, (hi-lo)/4)
	return lo - pad, hi + max(2*365, (hi-lo)/4)
}

// Anchor reports whether the start of segment i is an anchor it shares with the end of the one before.
func Anchor(s model.Schedule, i int) bool {
	segs := s.Segments()
	return i > 0 && i < len(segs) && segs[i-1].End == nil
}

// push sets the From of segment i to t. A segment whose last day is before t would be empty, so it moves along instead,
// keeping its length, and pushes the later segments it runs into the same way.
func push(segs []model.Segment, i int, t model.Date) {
	end, bounded := LastDay(segs, i)
	g := &segs[i]
	if !bounded || t <= end {
		g.From = t
		return
	}
	length := end - g.From
	g.From = t
	need := t + length + 1
	if g.End != nil {
		g.End = model.Ptr(need - 1)
	}
	if i+1 < len(segs) && segs[i+1].From < need {
		push(segs, i+1, need)
	}
}

// LastDay returns the last day segment i covers, and false if it has none because it is the last and has no End.
func LastDay(segs []model.Segment, i int) (model.Date, bool) {
	switch {
	case segs[i].End != nil:
		return *segs[i].End, true
	case i+1 < len(segs):
		return segs[i+1].From - 1, true
	}
	return model.MaxDate, false
}

func load(s model.Schedule, i int) ([]model.Segment, error) {
	segs := s.Segments()
	if i < 0 || i >= len(segs) {
		return nil, ErrNoSegment
	}
	return segs, nil
}

// build returns the schedule of the segments, or s and the reason if NewSchedule refuses them.
func build(s model.Schedule, segs []model.Segment) (model.Schedule, error) {
	out, err := model.NewSchedule(segs...)
	if err != nil {
		return s, err
	}
	return out, nil
}
