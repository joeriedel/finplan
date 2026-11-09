// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"
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

// Segment is one piece of a schedule: Value holds on [From, End). End is exclusive, unlike the inclusive
// BudgetItem.End and Interval.To. Without an End the segment runs until the next segment's From, or
// indefinitely if it is the last.
type Segment struct {
	From  Date
	End   *Date
	Value float64
}

// Schedule is a piecewise-constant value over time. A constant is one segment starting at MinDate.
// The schedule has no value before its first segment, in a gap between an ended segment and the next, or
// from the last segment's End on. Segments are sorted by From, never overlap, and each End is after its From.
// The zero Schedule is valid and empty.
type Schedule struct {
	segs []Segment
}

// Constant returns a schedule that holds v on every date.
func Constant(v float64) Schedule {
	return Schedule{segs: []Segment{{From: MinDate, Value: v}}}
}

// NewSchedule returns a schedule from segments, which must be sorted by strictly increasing From, each
// End after its From and not after the next segment's From.
func NewSchedule(segs ...Segment) (Schedule, error) {
	for i, g := range segs {
		if g.End != nil && *g.End <= g.From {
			return Schedule{}, fmt.Errorf("model: schedule segment %d: end must be after from", i)
		}
		if i == 0 {
			continue
		}
		prev := segs[i-1]
		if g.From <= prev.From {
			return Schedule{}, fmt.Errorf("model: schedule segment %d: from must be after the previous segment's from", i)
		}
		if prev.End != nil && *prev.End > g.From {
			return Schedule{}, fmt.Errorf("model: schedule segment %d overlaps the previous segment", i)
		}
	}
	s := make([]Segment, len(segs))
	for i, g := range segs {
		if g.End != nil {
			g.End = Ptr(*g.End)
		}
		s[i] = g
	}
	return Schedule{segs: s}, nil
}

// MustSchedule is NewSchedule for literal data: it panics on an invalid schedule.
func MustSchedule(segs ...Segment) Schedule {
	s, err := NewSchedule(segs...)
	if err != nil {
		panic(err)
	}
	return s
}

// Segments returns a copy of the schedule's segments, in order.
func (s Schedule) Segments() []Segment {
	out := make([]Segment, len(s.segs))
	for i, g := range s.segs {
		if g.End != nil {
			g.End = Ptr(*g.End)
		}
		out[i] = g
	}
	return out
}

// find returns the index of the segment covering d.
func (s Schedule) find(d Date) (int, bool) {
	i := sort.Search(len(s.segs), func(i int) bool { return s.segs[i].From > d }) - 1
	if i < 0 || (s.segs[i].End != nil && d >= *s.segs[i].End) {
		return 0, false
	}
	return i, true
}

// At returns the value in effect on d, and false if the schedule has none there.
func (s Schedule) At(d Date) (float64, bool) {
	i, ok := s.find(d)
	if !ok {
		return 0, false
	}
	return s.segs[i].Value, true
}

// Overlay returns the schedule with o written over it: on dates o covers the result has o's value, and
// everywhere else it has s's, including s's own gaps. A schedule with no segments changes nothing.
func (s Schedule) Overlay(o Schedule) Schedule {
	if len(o.segs) == 0 {
		return s
	}
	var pts []Date
	for _, sc := range []Schedule{s, o} {
		for _, g := range sc.segs {
			pts = append(pts, g.From)
			if g.End != nil {
				pts = append(pts, *g.End)
			}
		}
	}
	slices.Sort(pts)
	pts = slices.Compact(pts)

	// Pieces between breakpoints that come from the same source segment are one segment again.
	type piece struct {
		seg    Segment
		fromO  bool
		source int
		end    *Date
	}
	var ps []piece
	for i, a := range pts {
		var end *Date
		if i+1 < len(pts) {
			end = Ptr(pts[i+1])
		}
		fromO := true
		idx, ok := o.find(a)
		if !ok {
			fromO = false
			if idx, ok = s.find(a); !ok {
				continue
			}
		}
		v := o.segs
		if !fromO {
			v = s.segs
		}
		if n := len(ps); n > 0 && ps[n-1].fromO == fromO && ps[n-1].source == idx && ps[n-1].end != nil && *ps[n-1].end == a {
			ps[n-1].end = end
			continue
		}
		ps = append(ps, piece{seg: Segment{From: a, Value: v[idx].Value}, fromO: fromO, source: idx, end: end})
	}
	segs := make([]Segment, len(ps))
	for i, p := range ps {
		segs[i] = p.seg
		if p.end != nil && (i+1 == len(ps) || ps[i+1].seg.From != *p.end) {
			segs[i].End = p.end
		}
	}
	return Schedule{segs: segs}
}

// Compound returns the growth factor over the days from..to when the schedule is an annual rate,
// compounded over exact elapsed time and split at each segment boundary. Dates with no value contribute a factor of 1.
func (s Schedule) Compound(from, to Date) float64 {
	f := 1.0
	for cur := from; cur < to; {
		i, covered := s.find(cur)
		end := to
		if covered && s.segs[i].End != nil {
			end = min(end, *s.segs[i].End)
		}
		if next := sort.Search(len(s.segs), func(j int) bool { return s.segs[j].From > cur }); next < len(s.segs) {
			end = min(end, s.segs[next].From)
		}
		if covered {
			f *= math.Pow(1+s.segs[i].Value, (float64(end)-float64(cur))/DaysPerYear)
		}
		cur = end
	}
	return f
}
