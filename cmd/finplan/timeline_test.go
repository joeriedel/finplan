// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
	"github.com/joeriedel/finplan/internal/timeline"
)

type shownSchedule []struct {
	From  *string
	End   *string
	Value float64
}

// classGrowth reads the New job us-large-cap class growth modification through the CLI: its index and its schedule.
func classGrowth(t *testing.T, path string) (int, shownSchedule) {
	t.Helper()
	var show struct {
		Modifications []struct {
			Index    int
			Target   struct{ Kind, ID string }
			Property string
			Schedule shownSchedule
		}
	}
	cliJSON(t, &show, "--db", path, "projection", "show", "New job")
	for _, m := range show.Modifications {
		if m.Target.Kind == string(model.KindClass) && m.Target.ID == "us-large-cap" && m.Property == model.PropGrowth {
			return m.Index, m.Schedule
		}
	}
	t.Fatal("New job has no us-large-cap class growth modification")
	return 0, nil
}

// segmentsOf reads a schedule shown by the CLI; a null from is the beginning of time.
func segmentsOf(t *testing.T, sh shownSchedule) model.Schedule {
	t.Helper()
	var segs []model.Segment
	for _, g := range sh {
		seg := model.Segment{From: model.MinDate, Value: g.Value}
		if g.From != nil {
			d, err := parseCLIDate(*g.From, false)
			if err != nil {
				t.Fatal(err)
			}
			seg.From = d
		}
		if g.End != nil {
			d, err := parseCLIDate(*g.End, false)
			if err != nil {
				t.Fatal(err)
			}
			seg.End = &d
		}
		segs = append(segs, seg)
	}
	s, err := model.NewSchedule(segs...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// dragGrowth plays the timeline edits of the scenario on the schedule, checking each step: drag the anchors, open a gap, push a segment along, and delete one.
func dragGrowth(t *testing.T, s model.Schedule) model.Schedule {
	t.Helper()
	d := model.D
	step := func(name string, next model.Schedule, err error, want ...model.Segment) model.Schedule {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if w := model.MustSchedule(want...); !reflect.DeepEqual(next.Segments(), w.Segments()) {
			t.Fatalf("%s: got %v, want %v", name, next.Segments(), w.Segments())
		}
		return next
	}
	begin := model.Segment{From: model.MinDate, Value: 0.04}
	seg := func(from model.Date, v float64) model.Segment { return model.Segment{From: from, Value: v} }

	// 4% to 2028, 6% to 2034, 2% after.
	next, err := timeline.MoveStart(s, 1, d(2030, 1, 1))
	s = step("drag the 2029 anchor", next, err, begin, seg(d(2030, 1, 1), 0.06), seg(d(2035, 1, 1), 0.02))
	next, err = timeline.MoveStart(s, 2, d(2033, 1, 1))
	s = step("drag the 2035 anchor", next, err, begin, seg(d(2030, 1, 1), 0.06), seg(d(2033, 1, 1), 0.02))

	// Ending the first segment before the next one starts opens a gap.
	next, err = timeline.MoveEnd(s, 0, d(2027, 12, 31))
	gapped := model.Segment{From: model.MinDate, End: model.Ptr(d(2027, 12, 31)), Value: 0.04}
	s = step("insert a gap", next, err, gapped, seg(d(2030, 1, 1), 0.06), seg(d(2033, 1, 1), 0.02))

	// The 6% end dragged past the 2% start takes the 2% with it, keeping its length.
	next, err = timeline.MoveEnd(s, 1, d(2034, 12, 31))
	pushed := d(2035, 1, 1)
	s = step("push", next, err, gapped, seg(d(2030, 1, 1), 0.06), seg(pushed, 0.02))

	// Deleting the 6% leaves its place a gap, since the segment before ends.
	next, err = timeline.Delete(s, 1)
	return step("delete", next, err, gapped, seg(pushed, 0.02))
}

func TestTimelineScenarioThroughCLI(t *testing.T) {
	path := seededFile(t)
	idx, shown := classGrowth(t, path)
	s := segmentsOf(t, shown)
	want := []model.Segment{
		{From: model.MinDate, Value: 0.04},
		{From: model.D(2029, 1, 1), Value: 0.06},
		{From: model.D(2035, 1, 1), Value: 0.02},
	}
	if !reflect.DeepEqual(s.Segments(), want) {
		t.Fatalf("the demo schedule is %v", s.Segments())
	}
	edited := dragGrowth(t, s)

	// The mod edit command takes no segment that starts at the beginning of time in JSON, so the first one starts long before the record.
	type jseg struct {
		From  string  `json:"from"`
		End   *string `json:"end"`
		Value float64 `json:"value"`
	}
	var in []jseg
	for _, g := range edited.Segments() {
		j := jseg{From: g.From.String(), Value: g.Value}
		if g.From == model.MinDate {
			j.From = "1900-01-01"
		}
		if g.End != nil {
			j.End = model.Ptr(g.End.String())
		}
		in = append(in, j)
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(body), "mod", "edit", "New job", itoa(idx))

	_, got := classGrowth(t, path)
	wantShown := shownSchedule{
		{From: model.Ptr("1900-01-01"), End: model.Ptr("2027-12-31"), Value: 0.04},
		{From: model.Ptr(pushedFrom(edited)), Value: 0.02},
	}
	if !reflect.DeepEqual(got, wantShown) {
		t.Errorf("after mod edit: %+v, want %+v", got, wantShown)
	}
}

// TestTimelineScenarioKeepsBeginning writes the same result the way the editor does, through ops, which can say "from the beginning".
func TestTimelineScenarioKeepsBeginning(t *testing.T) {
	path := seededFile(t)
	idx, shown := classGrowth(t, path)
	edited := dragGrowth(t, segmentsOf(t, shown))

	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.SetModValues(st, rec, "New job", idx, edited, ops.Options{}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	_, got := classGrowth(t, path)
	if len(got) != 2 || got[0].From != nil || got[0].End == nil || *got[0].End != "2027-12-31" || got[1].From == nil || *got[1].From != pushedFrom(edited) {
		t.Errorf("after SetModValues: %+v", got)
	}
}

// TestTimelineLastSegmentRefusedThroughCLI checks that the one schedule the editor refuses to make, an empty one, is also refused by the CLI.
func TestTimelineLastSegmentRefusedThroughCLI(t *testing.T) {
	path := seededFile(t)
	idx, _ := classGrowth(t, path)
	refusedWrite(t, path, "[]", 2, "usage", "mod", "edit", "New job", itoa(idx))
}

// pushedFrom is the From of the last segment of s, as the CLI writes it.
func pushedFrom(s model.Schedule) string {
	segs := s.Segments()
	return segs[len(segs)-1].From.String()
}

func TestTimelineStateEdits(t *testing.T) {
	sched := model.MustSchedule(model.Segment{From: model.MinDate, Value: 0.04})
	m := model.Modification{Target: model.ObjectRef{ID: "brokerage"}, Property: model.PropGrowth, Mode: model.ModeScale, Values: sched}
	st := timelineState{target: m.Target, property: m.Property, mode: m.Mode}
	if !st.edits(m) {
		t.Error("the opened modification is not recognised")
	}
	other := m
	other.Target.ID = "cash"
	if st.edits(other) {
		t.Error("another target is taken for the opened modification")
	}
	other = m
	other.Property = model.PropInflation
	if st.edits(other) {
		t.Error("another property is taken for the opened modification")
	}
	other = m
	other.Mode = model.ModeSet
	if st.edits(other) {
		t.Error("another mode is taken for the opened modification")
	}
	other = m
	other.Values = model.Schedule{}
	if st.edits(other) {
		t.Error("a modification with no segments is edited")
	}
}

func TestModAfterMoveAndRemove(t *testing.T) {
	for _, c := range []struct{ mod, from, to, want int }{
		{1, 1, 3, 3}, {2, 1, 3, 1}, {3, 1, 3, 2}, {4, 1, 3, 4}, {0, 1, 3, 0},
		{3, 3, 1, 1}, {1, 3, 1, 2}, {2, 3, 1, 3}, {0, 3, 1, 0}, {4, 3, 1, 4},
	} {
		if got := modAfterMove(c.mod, c.from, c.to); got != c.want {
			t.Errorf("modAfterMove(%d, %d, %d) = %d, want %d", c.mod, c.from, c.to, got, c.want)
		}
	}
	for _, c := range []struct {
		mod, removed, want int
		ok                 bool
	}{{1, 0, 0, true}, {1, 1, 0, false}, {1, 2, 1, true}, {0, 2, 0, true}} {
		if got, ok := modAfterRemove(c.mod, c.removed); got != c.want || ok != c.ok {
			t.Errorf("modAfterRemove(%d, %d) = %d, %v, want %d, %v", c.mod, c.removed, got, ok, c.want, c.ok)
		}
	}
}

func TestModAfterInsert(t *testing.T) {
	for _, c := range []struct{ mod, at, want int }{{0, 1, 0}, {1, 1, 2}, {2, 1, 3}, {0, 0, 1}} {
		if got := modAfterInsert(c.mod, c.at); got != c.want {
			t.Errorf("modAfterInsert(%d, %d) = %d, want %d", c.mod, c.at, got, c.want)
		}
	}
}

func TestTimelineFollowsMods(t *testing.T) {
	tab := &projTab{a: &app{}}
	tab.tl = timelineState{open: true, proj: "p", mod: 1}
	tab.removed("q", 0)
	tab.removed("p", 0)
	if !tab.tl.open || tab.tl.mod != 0 {
		t.Fatalf("after removing mod 0: %+v", tab.tl)
	}
	tab.moved("p", 0, 2)
	if tab.tl.mod != 2 {
		t.Fatalf("after move: mod %d, want 2", tab.tl.mod)
	}
	tab.inserted("q", 0)
	tab.inserted("p", 1)
	if tab.tl.mod != 3 {
		t.Fatalf("after insert: mod %d, want 3", tab.tl.mod)
	}
	tab.removed("p", 3)
	if tab.tl.open {
		t.Fatal("editor still open after its modification was removed")
	}
}

func mustDate(t *testing.T, s string) model.Date {
	t.Helper()
	d, err := parseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func coversDate(s model.Schedule, d model.Date) bool {
	lo, hi := timeline.Span(s, model.D(2026, 1, 1))
	return lo < d && d < hi
}

// TestTypedDateReaches2045ThroughCLI types dates in 2045 into the demo schedule, writes the result as the editor does and reads it back through the CLI.
func TestTypedDateReaches2045ThroughCLI(t *testing.T) {
	path := seededFile(t)
	idx, shown := classGrowth(t, path)
	s := segmentsOf(t, shown)
	if coversDate(s, mustDate(t, "2045-03-01")) {
		t.Fatal("the demo schedule already covers 2045")
	}
	from, err := typedDate(s, 1, false, " 2045-03-01 ")
	if err != nil {
		t.Fatal(err)
	}
	if got := from.Segments()[1].From; got != mustDate(t, "2045-03-01") {
		t.Errorf("segment 2 starts %v", got)
	}
	if !coversDate(from, mustDate(t, "2045-03-01")) || len(from.Segments()) != 3 {
		t.Errorf("span does not cover 2045 or a segment was lost: %v", from.Segments())
	}
	to, err := typedDate(s, 2, true, "2045-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if last := to.Segments()[2]; last.End == nil || *last.End != mustDate(t, "2045-12-31") || !coversDate(to, *last.End) {
		t.Errorf("last segment is %+v", last)
	}

	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.SetModValues(st, rec, "New job", idx, from, ops.Options{}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	_, got := classGrowth(t, path)
	if len(got) != 3 || got[1].From == nil || *got[1].From != "2045-03-01" {
		t.Errorf("after SetModValues: %+v", got)
	}
}

// TestTypedDateOnAssetGrowthUndoesInOneStep edits an asset's growth schedule by typed dates as the editor does, through app.edit, and undoes it in one step.
func TestTypedDateOnAssetGrowthUndoesInOneStep(t *testing.T) {
	a := demoApp(t)
	id := demodata.NetWorth
	orig := model.MustSchedule(
		model.Segment{From: model.MinDate, Value: 0.04},
		model.Segment{From: model.D(2029, 1, 1), Value: 0.06},
		model.Segment{From: model.D(2035, 1, 1), Value: 0.02},
	)
	if !a.edit("set growth", func() (*ops.Result, error) {
		return ops.SetParam(a.st, a.rec, id, model.PropGrowth, &orig, ops.Options{})
	}) {
		t.Fatalf("setup refused: %v", a.notices)
	}
	tab := &assetsTab{a: a}
	commit := tab.growthCommit(id)
	cur := a.rec.Asset(id).Params[model.PropGrowth]
	next, err := typedDate(cur, 1, false, "2045-03-01")
	if err != nil {
		t.Fatal(err)
	}
	commit("set segment start", next)
	got := a.rec.Asset(id).Params[model.PropGrowth]
	if !reflect.DeepEqual(got, next) || !coversDate(got, mustDate(t, "2045-03-01")) {
		t.Fatalf("the growth is %v", got.Segments())
	}
	if s, _ := a.history.PeekUndo(); s.Label != "set segment start" {
		t.Errorf("undo label %q", s.Label)
	}
	a.undo()
	if back := a.rec.Asset(id).Params[model.PropGrowth]; !reflect.DeepEqual(back, orig) {
		t.Errorf("one undo left %v", back.Segments())
	}
}

func TestTypedDatePushesAndRefuses(t *testing.T) {
	d := model.D
	s := model.MustSchedule(
		model.Segment{From: model.MinDate, Value: 0.04},
		model.Segment{From: d(2029, 1, 1), Value: 0.06},
		model.Segment{From: d(2035, 1, 1), Value: 0.02},
	)
	pushed, err := typedDate(s, 1, true, "2036-06-30")
	if err != nil {
		t.Fatal(err)
	}
	if want := d(2036, 7, 1); pushed.Segments()[2].From != want {
		t.Errorf("the next segment starts %v, want %v", pushed.Segments()[2].From, want)
	}
	for _, c := range []struct {
		name, text string
		i          int
		end        bool
	}{
		{"to before from", "2028-12-31", 1, true},
		{"from of the beginning segment", "2020-01-01", 0, false},
		{"from that empties the previous segment", "2029-01-01", 2, false},
		{"unparseable from", "next year", 1, false},
		{"unparseable to", "2045", 1, true},
		{"empty from", "", 1, false},
		{"empty to of a segment that is not last", "", 1, true},
		{"no such segment", "2040-01-01", 7, false},
	} {
		got, err := typedDate(s, c.i, c.end, c.text)
		if err == nil || err.Error() == "" {
			t.Errorf("%s: no refusal", c.name)
		}
		if !reflect.DeepEqual(got, s) {
			t.Errorf("%s: the schedule changed", c.name)
		}
	}
	var bad badDateText
	if _, err := typedDate(s, 1, false, "x"); !errors.As(err, &bad) {
		t.Errorf("unparseable text is %v, not a badDateText", err)
	}
	if _, err := typedDate(s, 1, true, "2028-12-31"); errors.As(err, &bad) {
		t.Error("a refused date is reported as unparseable text")
	}
	open, err := typedDate(model.MustSchedule(model.Segment{From: d(2030, 1, 1), End: model.Ptr(d(2031, 1, 1)), Value: 1}), 0, true, "")
	if err != nil || open.Segments()[0].End != nil {
		t.Errorf("an empty end of the last segment: %v, %v", open.Segments(), err)
	}
}
