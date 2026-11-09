// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"math"
	"slices"
	"sort"
)

// Parameter and property names understood by the placeholder engine.
const (
	PropGrowth    = "growth"
	PropInflation = "inflation"
	PropAmount    = "amount"
	PropAddItem   = "add_item"
)

// Segment is one piece of a schedule: Value holds from From until the next segment's From.
type Segment struct {
	From  Date
	Value float64
}

// Schedule is a piecewise-constant value over time. A constant is one segment starting at MinDate.
// Before the first segment the schedule has no value.
type Schedule struct {
	Segments []Segment
}

// Constant returns a schedule that holds v on every date.
func Constant(v float64) Schedule {
	return Schedule{Segments: []Segment{{From: MinDate, Value: v}}}
}

// NewSchedule returns a schedule from segments in any order.
func NewSchedule(segs ...Segment) Schedule {
	s := slices.Clone(segs)
	sort.SliceStable(s, func(i, j int) bool { return s[i].From < s[j].From })
	return Schedule{Segments: s}
}

// At returns the value in effect on d, and false if the schedule has none yet.
func (s Schedule) At(d Date) (float64, bool) {
	i := sort.Search(len(s.Segments), func(i int) bool { return s.Segments[i].From > d })
	if i == 0 {
		return 0, false
	}
	return s.Segments[i-1].Value, true
}

// Compound returns the growth factor over the days from..to when the schedule is an annual rate,
// compounded over exact elapsed time and split at each segment boundary. Dates with no value grow by 0.
func (s Schedule) Compound(from, to Date) float64 {
	f := 1.0
	for cur := from; cur < to; {
		i := sort.Search(len(s.Segments), func(i int) bool { return s.Segments[i].From > cur })
		end := to
		if i < len(s.Segments) && s.Segments[i].From < end {
			end = s.Segments[i].From
		}
		if i > 0 {
			f *= math.Pow(1+s.Segments[i-1].Value, (float64(end)-float64(cur))/DaysPerYear)
		}
		cur = end
	}
	return f
}
