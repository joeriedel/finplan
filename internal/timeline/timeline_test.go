// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package timeline

import (
	"errors"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/model"
)

const none = model.Date(math.MinInt32 + 1)

// day is the date n days after a fixed origin.
func day(n int) model.Date { return model.D(2030, 1, 1).Add(n) }

// seg is a segment from day from, with an end on day end unless that is none, and the value v.
func seg(from int, end model.Date, v float64) model.Segment {
	g := model.Segment{From: day(from), Value: v}
	if end != none {
		g.End = model.Ptr(day(int(end)))
	}
	return g
}

func sched(t *testing.T, segs ...model.Segment) model.Schedule {
	t.Helper()
	s, err := model.NewSchedule(segs...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// three is 1 from the start of time, 2 from day 10, 3 from day 20, all touching.
func three(t *testing.T) model.Schedule {
	return sched(t, model.Segment{From: model.MinDate, Value: 1}, seg(10, none, 2), seg(20, none, 3))
}

func equal(t *testing.T, got model.Schedule, want ...model.Segment) {
	t.Helper()
	if w := sched(t, want...); !reflect.DeepEqual(got.Segments(), w.Segments()) {
		t.Errorf("got %v, want %v", got.Segments(), w.Segments())
	}
}

func TestMoveStart(t *testing.T) {
	begin := model.Segment{From: model.MinDate, Value: 1}
	four := sched(t, begin, seg(10, none, 2), seg(20, 22, 3), seg(25, none, 4))
	tests := []struct {
		name string
		in   model.Schedule
		i    int
		to   model.Date
		want []model.Segment
	}{
		{"anchor right inside the next segment", three(t), 1, day(15), []model.Segment{begin, seg(15, none, 2), seg(20, none, 3)}},
		{"anchor left", three(t), 1, day(5), []model.Segment{begin, seg(5, none, 2), seg(20, none, 3)}},
		{"anchor left stops a day after the previous from", sched(t, seg(0, none, 1), seg(10, none, 2)), 1, day(-5), []model.Segment{seg(0, none, 1), seg(1, none, 2)}},
		{"anchor right past the implied end pushes the segment along", three(t), 1, day(25), []model.Segment{begin, seg(25, none, 2), seg(35, none, 3)}},
		{"anchor right pushes a cascade", four, 1, day(21), []model.Segment{begin, seg(21, none, 2), seg(31, 33, 3), seg(34, none, 4)}},
		{"anchor right of a segment with an end moves it along", sched(t, begin, seg(10, 12, 2), seg(20, none, 3)), 1, day(14), []model.Segment{begin, seg(14, 16, 2), seg(20, none, 3)}},
		{"anchor right of an ended segment into the next one", sched(t, begin, seg(10, 12, 2), seg(20, none, 3)), 1, day(19), []model.Segment{begin, seg(19, 21, 2), seg(22, none, 3)}},
		{"last segment pushed along", sched(t, begin, seg(10, none, 2)), 1, day(50), []model.Segment{begin, seg(50, none, 2)}},
		{"gap start left stops where it touches", sched(t, begin, seg(10, 12, 2), seg(20, none, 3)), 2, day(2), []model.Segment{begin, seg(10, none, 2), seg(13, none, 3)}},
		{"gap start left inside the gap", sched(t, begin, seg(10, 12, 2), seg(20, none, 3)), 2, day(16), []model.Segment{begin, seg(10, 12, 2), seg(16, none, 3)}},
		{"gap start right stops at the segment's own end", sched(t, begin, seg(10, 12, 2), seg(20, 25, 3)), 2, day(40), []model.Segment{begin, seg(10, 12, 2), seg(25, 25, 3)}},
		{"gap start right of an implied end stops before the next", sched(t, begin, seg(10, 12, 2), seg(20, none, 3), seg(30, none, 4)), 2, day(60), []model.Segment{begin, seg(10, 12, 2), seg(29, none, 3), seg(30, none, 4)}},
		{"first segment's start has nothing to stop it", sched(t, seg(5, none, 1)), 0, day(-3), []model.Segment{seg(-3, none, 1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MoveStart(tc.in, tc.i, tc.to)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, tc.want...)
		})
	}

	in := three(t)
	if got, err := MoveStart(in, 0, day(3)); !errors.Is(err, ErrFixedStart) || !reflect.DeepEqual(got, in) {
		t.Errorf("start of time: %v, %v", got, err)
	}
	if _, err := MoveStart(in, 3, day(3)); !errors.Is(err, ErrNoSegment) {
		t.Errorf("no such segment: %v", err)
	}
}

func TestMoveEnd(t *testing.T) {
	begin := model.Segment{From: model.MinDate, Value: 1}
	gap := sched(t, begin, seg(10, 15, 2), seg(30, none, 3))
	tests := []struct {
		name string
		in   model.Schedule
		i    int
		to   model.Date
		want []model.Segment
	}{
		{"last segment gets an end", three(t), 2, day(40), []model.Segment{begin, seg(10, none, 2), seg(20, 40, 3)}},
		{"last segment's end stops at its from", three(t), 2, day(3), []model.Segment{begin, seg(10, none, 2), seg(20, 20, 3)}},
		{"last segment's end moves", sched(t, begin, seg(10, 15, 2)), 1, day(80), []model.Segment{begin, seg(10, 80, 2)}},
		{"last segment's end at the end of time clears it", sched(t, begin, seg(10, 15, 2)), 1, model.MaxDate, []model.Segment{begin, seg(10, none, 2)}},
		{"anchor's earlier side opens a gap", three(t), 1, day(14), []model.Segment{begin, seg(10, 14, 2), seg(20, none, 3)}},
		{"ending the day before the next from touches", gap, 1, day(29), []model.Segment{begin, seg(10, none, 2), seg(30, none, 3)}},
		{"gap edge moves inside the gap", gap, 1, day(20), []model.Segment{begin, seg(10, 20, 2), seg(30, none, 3)}},
		{"gap edge left stops at the segment's from", gap, 1, day(-4), []model.Segment{begin, seg(10, 10, 2), seg(30, none, 3)}},
		{"end into the next segment pushes its start", three(t), 1, day(22), []model.Segment{begin, seg(10, none, 2), seg(23, none, 3)}},
		{"end far into the next segment moves it along", sched(t, begin, seg(10, none, 2), seg(20, none, 3), seg(25, none, 4)), 0, day(30), []model.Segment{begin, seg(31, none, 2), seg(41, none, 3), seg(46, none, 4)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MoveEnd(tc.in, tc.i, tc.to)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, tc.want...)
		})
	}
	if _, err := MoveEnd(three(t), -1, day(3)); !errors.Is(err, ErrNoSegment) {
		t.Errorf("no such segment: %v", err)
	}
}

func TestDropEnd(t *testing.T) {
	s := sched(t, seg(0, none, 1), seg(10, 20, 2))
	hi := day(100)
	got, err := DropEnd(s, 1, hi, hi)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, seg(0, none, 1), seg(10, none, 2))
	got, err = DropEnd(s, 1, day(50), hi)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, seg(0, none, 1), seg(10, 49, 2))
	got, err = DropEnd(s, 0, hi, hi)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, seg(0, none, 1), seg(100, 110, 2))
}

func TestClearEnd(t *testing.T) {
	got, err := ClearEnd(sched(t, seg(0, none, 1), seg(10, 20, 2)))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, seg(0, none, 1), seg(10, none, 2))
	got, err = ClearEnd(got)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, seg(0, none, 1), seg(10, none, 2))
	if _, err := ClearEnd(model.Schedule{}); !errors.Is(err, ErrNoSegment) {
		t.Errorf("empty: %v", err)
	}
}

func TestDelete(t *testing.T) {
	begin := model.Segment{From: model.MinDate, Value: 1}
	tests := []struct {
		name string
		in   model.Schedule
		i    int
		want []model.Segment
	}{
		{"touching middle segment is covered by the one before", three(t), 1, []model.Segment{begin, seg(20, none, 3)}},
		{"after an ended segment it leaves a gap", sched(t, begin, seg(5, 8, 2), seg(10, none, 3), seg(20, none, 4)), 2, []model.Segment{begin, seg(5, 8, 2), seg(20, none, 4)}},
		{"first", three(t), 0, []model.Segment{seg(10, none, 2), seg(20, none, 3)}},
		{"last", three(t), 2, []model.Segment{begin, seg(10, none, 2)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Delete(tc.in, tc.i)
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, tc.want...)
		})
	}
	one := sched(t, seg(0, none, 1))
	if got, err := Delete(one, 0); !errors.Is(err, ErrLastSegment) || !reflect.DeepEqual(got, one) {
		t.Errorf("only segment: %v, %v", got, err)
	}
	if _, err := Delete(one, 1); !errors.Is(err, ErrNoSegment) {
		t.Errorf("no such segment: %v", err)
	}
}

func TestSetValue(t *testing.T) {
	got, err := SetValue(three(t), 1, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, got, model.Segment{From: model.MinDate, Value: 1}, seg(10, none, 0.5), seg(20, none, 3))
	if _, err := SetValue(three(t), 1, math.NaN()); err == nil {
		t.Error("NaN accepted")
	}
	if _, err := SetValue(three(t), 5, 1); !errors.Is(err, ErrNoSegment) {
		t.Errorf("no such segment: %v", err)
	}
}

func TestSplit(t *testing.T) {
	begin := model.Segment{From: model.MinDate, Value: 1}
	tests := []struct {
		name string
		in   model.Schedule
		i    int
		at   model.Date
		want []model.Segment // nil: refused
	}{
		{"unbounded last", three(t), 2, day(30), []model.Segment{begin, seg(10, none, 2), seg(20, none, 3), seg(30, none, 3)}},
		{"keeps the end on the second half", sched(t, seg(0, 50, 1)), 0, day(10), []model.Segment{seg(0, none, 1), seg(10, 50, 1)}},
		{"touching middle", three(t), 1, day(15), []model.Segment{begin, seg(10, none, 2), seg(15, none, 2), seg(20, none, 3)}},
		{"from the start of time", three(t), 0, day(-100), []model.Segment{begin, seg(-100, none, 1), seg(10, none, 2), seg(20, none, 3)}},
		{"at the from", three(t), 1, day(10), nil},
		{"before the from", three(t), 1, day(5), nil},
		{"after the last day", three(t), 1, day(20), nil},
		{"after an explicit end", sched(t, seg(0, 5, 1)), 0, day(6), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Split(tc.in, tc.i, tc.at)
			if tc.want == nil {
				if err == nil || !reflect.DeepEqual(got, tc.in) {
					t.Errorf("got %v, %v; want a refusal and no change", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, tc.want...)
		})
	}
}

func TestAppend(t *testing.T) {
	begin := model.Segment{From: model.MinDate, Value: 1}
	tests := []struct {
		name string
		in   model.Schedule
		at   model.Date
		want []model.Segment // nil: refused
	}{
		{"after an open last segment", three(t), day(40), []model.Segment{begin, seg(10, none, 2), seg(20, none, 3), seg(40, none, 3)}},
		{"the day after the end touches", sched(t, begin, seg(10, 30, 2)), day(31), []model.Segment{begin, seg(10, none, 2), seg(31, none, 2)}},
		{"later than the end leaves a gap", sched(t, begin, seg(10, 30, 2)), day(35), []model.Segment{begin, seg(10, 30, 2), seg(35, none, 2)}},
		{"inside the last segment's end", sched(t, begin, seg(10, 30, 2)), day(30), nil},
		{"at the last from", three(t), day(20), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Append(tc.in, tc.at)
			if tc.want == nil {
				if err == nil || !reflect.DeepEqual(got, tc.in) {
					t.Errorf("got %v, %v; want a refusal and no change", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, tc.want...)
		})
	}
	if _, err := Append(model.Schedule{}, day(1)); !errors.Is(err, ErrNoSegment) {
		t.Errorf("empty: %v", err)
	}
}

func TestNearestMonth(t *testing.T) {
	tests := []struct{ in, want model.Date }{
		{model.D(2030, 3, 1), model.D(2030, 3, 1)},
		{model.D(2030, 3, 10), model.D(2030, 3, 1)},
		{model.D(2030, 3, 20), model.D(2030, 4, 1)},
		{model.D(2030, 12, 25), model.D(2031, 1, 1)},
	}
	for _, tc := range tests {
		if got := NearestMonth(tc.in); got != tc.want {
			t.Errorf("NearestMonth(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSpan(t *testing.T) {
	start := model.D(2026, 1, 1)
	s := sched(t, model.Segment{From: model.MinDate, Value: 0.04}, model.Segment{From: model.D(2029, 1, 1), Value: 0.06}, model.Segment{From: model.D(2035, 1, 1), Value: 0.02})
	lo, hi := Span(s, start)
	if lo >= start || hi <= model.D(2035, 1, 1) {
		t.Errorf("span %v to %v does not hold %v to 2035-01-01 with room", lo, hi, start)
	}
	lo, hi = Span(sched(t, seg(0, 100, 1)), start)
	if lo >= day(0) || hi <= day(101) {
		t.Errorf("span %v to %v has no room around %v to %v", lo, hi, day(0), day(101))
	}
	// Starting later than the first dated segment does not narrow it.
	if lo, _ := Span(s, model.D(2040, 1, 1)); lo >= model.D(2029, 1, 1) {
		t.Errorf("span starts at %v", lo)
	}
	if lo, hi := Span(sched(t, model.Segment{From: model.MinDate, Value: 1}), start); lo != start || hi <= lo {
		t.Errorf("constant: %v to %v", lo, hi)
	}
}

func TestAnchor(t *testing.T) {
	s := sched(t, seg(0, none, 1), seg(10, 12, 2), seg(20, none, 3), seg(30, none, 4))
	for i, want := range []bool{false, true, false, true} {
		if got := Anchor(s, i); got != want {
			t.Errorf("Anchor(%d) = %v, want %v", i, got, want)
		}
	}
}

func TestSetStartAndSetEnd(t *testing.T) {
	begin := model.Segment{From: model.MinDate, Value: 1}
	four := sched(t, begin, seg(10, none, 2), seg(20, 22, 3), seg(25, none, 4))
	tests := []struct {
		name  string
		in    model.Schedule
		i     int
		to    model.Date
		end   bool
		want  []model.Segment
		wantE error
	}{
		{name: "start far later pushes the segment and its follower", in: three(t), i: 1, to: day(500), want: []model.Segment{begin, seg(500, none, 2), seg(510, none, 3)}},
		{name: "start earlier than the previous start plus one is refused", in: three(t), i: 2, to: day(10)},
		{name: "start inside the segment before", in: three(t), i: 2, to: day(15), want: []model.Segment{begin, seg(10, none, 2), seg(15, none, 3)}},
		{name: "start of the beginning segment", in: three(t), i: 0, to: day(5), wantE: ErrFixedStart},
		{name: "start of a missing segment", in: three(t), i: 5, to: day(5), wantE: ErrNoSegment},
		{name: "start inside a bounded segment before is refused", in: four, i: 3, to: day(22)},
		{name: "start right after a bounded segment before", in: four, i: 3, to: day(23), want: []model.Segment{begin, seg(10, none, 2), seg(20, 22, 3), seg(23, none, 4)}},
		{name: "end far later pushes the follower", in: three(t), i: 1, to: day(500), end: true, want: []model.Segment{begin, seg(10, none, 2), seg(501, none, 3)}},
		{name: "end before the start is refused", in: three(t), i: 1, to: day(5), end: true},
		{name: "end of the last segment", in: three(t), i: 2, to: day(30), end: true, want: []model.Segment{begin, seg(10, none, 2), seg(20, 30, 3)}},
		{name: "end of a missing segment", in: three(t), i: -1, to: day(30), end: true, wantE: ErrNoSegment},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			var got model.Schedule
			var err error
			if c.end {
				got, err = SetEnd(c.in, c.i, c.to)
			} else {
				got, err = SetStart(c.in, c.i, c.to)
			}
			if c.want == nil {
				if err == nil || (c.wantE != nil && !errors.Is(err, c.wantE)) {
					t.Fatalf("err = %v, want a refusal", err)
				}
				if !reflect.DeepEqual(got, c.in) {
					t.Fatal("a refused edit changed the schedule")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			equal(t, got, c.want...)
		})
	}
}

// TestEditsKeepEverySegment applies random edits and checks that each is a schedule NewSchedule accepts, that none
// but Delete drops a segment, and that no edit changes the segments' order or values.
func TestEditsKeepEverySegment(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for run := 0; run < 200; run++ {
		s := sched(t, model.Segment{From: model.MinDate, Value: 0}, seg(10, none, 1), seg(20, 25, 2), seg(30, none, 3), seg(40, 45, 4))
		for step := 0; step < 30; step++ {
			before := s.Segments()
			i, to := rng.Intn(len(before)), day(rng.Intn(120)-20)
			var got model.Schedule
			var err error
			switch rng.Intn(5) {
			case 0:
				got, err = MoveStart(s, i, to)
			case 1:
				got, err = MoveEnd(s, i, to)
			case 2:
				got, err = SetStart(s, i, to)
			case 3:
				got, err = SetEnd(s, i, to)
			default:
				got, err = ClearEnd(s)
			}
			if err != nil {
				if !reflect.DeepEqual(got, s) {
					t.Fatal("a refused edit changed the schedule")
				}
				continue
			}
			after := got.Segments()
			if len(after) != len(before) {
				t.Fatalf("%d segments became %d", len(before), len(after))
			}
			if _, err := model.NewSchedule(after...); err != nil {
				t.Fatalf("NewSchedule refused the result: %v", err)
			}
			for k := range after {
				if after[k].Value != before[k].Value {
					t.Fatalf("segment %d changed value", k)
				}
			}
			s = got
		}
	}
}
