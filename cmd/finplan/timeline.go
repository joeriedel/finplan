// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/timeline"
)

// timelineState is the timeline editor under the modifications table: which modification it edits, which of its segments is selected (-1 for none) and the drag in progress.
type timelineState struct {
	open bool
	proj string
	mod  int
	// target, property and mode identify the modification that was opened, so the editor closes when the index comes to hold another.
	target   model.ObjectRef
	property string
	mode     model.Mode
	sel      int
	drag     *timelineDrag
	// generation is app.generation when the editor opened; an undo, a redo or a reload since then closes it.
	generation int
}

// modAfterMove is the index of the modification that was at mod after the one at from moves to index to.
func modAfterMove(mod, from, to int) int {
	switch {
	case mod == from:
		return to
	case from < mod && mod <= to:
		return mod - 1
	case to <= mod && mod < from:
		return mod + 1
	}
	return mod
}

// modAfterRemove is the index of the modification that was at mod after the one at removed is deleted, and false if it is the one deleted.
func modAfterRemove(mod, removed int) (int, bool) {
	switch {
	case mod == removed:
		return 0, false
	case mod > removed:
		return mod - 1, true
	}
	return mod, true
}

// modAfterInsert is the index of the modification that was at mod after one is inserted at index at.
func modAfterInsert(mod, at int) int {
	if mod >= at {
		return mod + 1
	}
	return mod
}

// inserted follows the open editor's modification of projection proj when a modification is inserted at index at.
func (t *projTab) inserted(proj string, at int) {
	if t.tl.open && t.tl.proj == proj {
		t.tl.mod = modAfterInsert(t.tl.mod, at)
	}
}

// moved follows the open editor's modification of projection proj when the modification at from moves to to.
func (t *projTab) moved(proj string, from, to int) {
	if t.tl.open && t.tl.proj == proj {
		t.tl.mod = modAfterMove(t.tl.mod, from, to)
	}
}

// removed follows the open editor's modification of projection proj when the modification at index is removed, and closes the editor if it was that one.
func (t *projTab) removed(proj string, index int) {
	if t.tl.open && t.tl.proj == proj {
		var ok bool
		if t.tl.mod, ok = modAfterRemove(t.tl.mod, index); !ok {
			t.tl = timelineState{}
		}
	}
}

// edits reports whether the editor, opened on modification index mod, still shows m, which is now at that index.
func (st *timelineState) edits(m model.Modification) bool {
	return st.target == m.Target && st.property == m.Property && st.mode == m.Mode && len(m.Values.Segments()) > 0
}

// timelineDrag is one drag of an edge. The strip shows cur, the edit of orig for the pointer's present place, and only a completed drag is written to the record, as one undo step. The axis stays as it was when the drag began, so the strip does not stretch under the pointer.
type timelineDrag struct {
	id        string
	seg       int
	end       bool
	grab      float32
	pressX    float32
	moved     bool
	orig, cur model.Schedule
	lo, hi    model.Date
}

// timelineHandle is a draggable edge: the start of segment seg, or the last day of it if end.
type timelineHandle struct {
	id  string
	seg int
	end bool
	x   float32
}

// openTimeline shows the timeline editor for modification mod of the projection, which is m.
func (t *projTab) openTimeline(proj string, mod int, m model.Modification) {
	t.tl = timelineState{open: true, proj: proj, mod: mod, generation: t.a.generation, target: m.Target, property: m.Property, mode: m.Mode, sel: -1}
}

// timelineEditor draws the editor for the modification openTimeline chose: the strip, the controls for the selected segment, and the Delete key.
func (t *projTab) timelineEditor(p *model.Projection) {
	st := &t.tl
	if st.open && (st.generation != t.a.generation || st.proj != p.Name || st.mod >= len(p.Mods) || !st.edits(p.Mods[st.mod])) {
		*st = timelineState{}
	}
	if !st.open {
		return
	}
	rec := t.a.rec
	m := p.Mods[st.mod]
	segs := m.Values.Segments()
	if len(segs) == 0 {
		return
	}
	if st.sel >= len(segs) {
		st.sel = len(segs) - 1
	}

	imgui.Separator()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(fmt.Sprintf("Timeline of modification %d: %s %s", st.mod+1, objectText(effective(rec, p.Name), m.Target), m.Property))
	imgui.SameLine()
	if imgui.SmallButton("Close##timeline") {
		*st = timelineState{}
		return
	}

	start := rec.Today
	if p.Start != nil {
		start = *p.Start
	}
	cur := m.Values
	var lo, hi model.Date
	if d := st.drag; d != nil {
		cur, lo, hi = d.cur, d.lo, d.hi
	} else {
		lo, hi = timeline.Span(cur, start)
	}
	t.strip(m, cur, lo, hi)
	t.timelineControls(m, segs, lo)

	if st.sel >= 0 && !imgui.IsAnyItemActive() && imgui.IsKeyPressedBoolV(imgui.KeyDelete, false) {
		t.timelineEdit("delete segment", m.Values, func(s model.Schedule) (model.Schedule, error) { return timeline.Delete(s, st.sel) })
	}
}

// strip draws the schedule cur as blocks on a time axis from lo to hi, with a handle on each edge, and handles clicks and drags on it.
func (t *projTab) strip(m model.Modification, cur model.Schedule, lo, hi model.Date) {
	st := &t.tl
	s := uiScale
	dl := imgui.WindowDrawList()
	origin := imgui.CursorScreenPos()
	pad := 8 * s
	w := max(imgui.ContentRegionAvail().X, 240*s)
	x0, x1 := origin.X+pad, origin.X+w-pad
	bh := imgui.FrameHeight() * 1.6
	y0, y1 := origin.Y, origin.Y+bh
	ppd := (x1 - x0) / float32(hi-lo)
	xOf := func(d model.Date) float32 {
		switch {
		case d <= lo:
			return x0
		case d >= hi:
			return x1
		}
		return x0 + float32(d-lo)*ppd
	}
	dateAt := func(x float32) model.Date {
		return min(max(lo+model.Date(math.Round(float64((x-x0)/ppd))), lo), hi)
	}
	segs := cur.Segments()
	posOf := func(seg int, end bool) float32 {
		if !end {
			return xOf(segs[seg].From)
		}
		if last, bounded := timeline.LastDay(segs, seg); bounded {
			return xOf(last + 1)
		}
		return x1
	}

	dl.AddRectFilledV(imgui.NewVec2(x0, y0), imgui.NewVec2(x1, y1), u32(rgba(colInput, 1)), 3*s, 0)
	for i, g := range segs {
		xa, xb := posOf(i, false), posOf(i, true)
		xb = max(xb, xa+1)
		fill := uint32(colButton)
		if i == st.sel {
			fill = colSelect
		}
		a, b := imgui.NewVec2(xa, y0), imgui.NewVec2(xb, y1)
		dl.AddRectFilledV(a, b, u32(rgba(fill, 1)), 2*s, 0)
		dl.AddRectV(a, b, u32(rgba(colBorder, 1)), 2*s, s, 0)
		text := formatValue(m.Property, m.Mode, g.Value)
		ts := imgui.CalcTextSize(text)
		dl.PushClipRect(a, b)
		dl.AddTextVec2(imgui.NewVec2(max(xa+(xb-xa-ts.X)/2, xa+4*s), y0+(bh-ts.Y)/2), u32(rgba(colText, 1)), text)
		dl.PopClipRect()
		if st.drag != nil {
			continue
		}
		imgui.SetCursorScreenPos(a)
		imgui.SetNextItemAllowOverlap()
		if imgui.InvisibleButton("##seg"+strconv.Itoa(i), imgui.NewVec2(xb-xa, bh)) {
			st.sel = i
		}
		if imgui.IsItemHovered() {
			last, bounded := timeline.LastDay(segs, i)
			tooltipText(segmentRange(g.From, last, bounded) + "\nClick to select; Delete removes it.")
		}
	}

	var hs []timelineHandle
	if d := st.drag; d != nil {
		hs = append(hs, timelineHandle{d.id, d.seg, d.end, posOf(d.seg, d.end)})
	} else {
		for i, g := range segs {
			if g.From != model.MinDate {
				hs = append(hs, timelineHandle{"##start" + strconv.Itoa(i), i, false, posOf(i, false)})
			}
			if g.End != nil || i == len(segs)-1 {
				hs = append(hs, timelineHandle{"##end" + strconv.Itoa(i), i, true, posOf(i, true)})
			}
		}
	}
	hw := 10 * s
	for _, h := range hs {
		imgui.SetCursorScreenPos(imgui.NewVec2(h.x-hw/2, y0))
		imgui.InvisibleButton(h.id, imgui.NewVec2(hw, bh))
		hot, active := imgui.IsItemHovered(), imgui.IsItemActive()
		if hot || active {
			imgui.SetMouseCursor(imgui.MouseCursorResizeEW)
		}
		if hot && !active && st.drag == nil {
			tooltipText("Drag to move this edge.\nShift-drag an anchor to move only the earlier segment's end, which opens a gap.\nCtrl-drag moves by single days instead of months.")
		}
		if imgui.IsItemActivated() && st.drag == nil {
			mx := imgui.MousePos().X
			d := &timelineDrag{id: h.id, seg: h.seg, end: h.end, grab: mx - h.x, pressX: mx, orig: cur, cur: cur, lo: lo, hi: hi}
			if !h.end && timeline.Anchor(cur, h.seg) && imgui.IsKeyDown(imgui.ModShift) {
				d.seg, d.end = h.seg-1, true
			}
			st.drag, st.sel = d, h.seg
		}
		if d := st.drag; d != nil && d.id == h.id {
			if active {
				if mx := imgui.MousePos().X; d.moved || math.Abs(float64(mx-d.pressX)) >= 3*float64(s) {
					d.moved = true
					d.cur = d.preview(dateAt(mx-d.grab), !imgui.IsKeyDown(imgui.ModCtrl))
				}
			}
			if imgui.IsItemDeactivated() {
				st.drag = nil
				if d.moved && !reflect.DeepEqual(d.cur, d.orig) {
					t.timelineEdit("move timeline edge", d.orig, func(model.Schedule) (model.Schedule, error) { return d.cur, nil })
				}
			}
		}
		col := uint32(colAccent)
		if hot || active {
			col = colAccentLit
		}
		dl.AddRectFilledV(imgui.NewVec2(h.x-1.5*s, y0-2*s), imgui.NewVec2(h.x+1.5*s, y1+2*s), u32(rgba(col, 1)), 1.5*s, 0)
	}

	tickCol := u32(rgba(colMuted, 1))
	yearPx := 365.25 * ppd
	step := 100
	for _, n := range []int{1, 2, 5, 10, 20, 50} {
		if float32(n)*yearPx >= 56*s {
			step = n
			break
		}
	}
	ylo, _, _ := lo.YMD()
	yhi, _, _ := hi.YMD()
	for yr := ylo - ((ylo%step)+step)%step; yr <= yhi; yr += step {
		d := model.D(yr, 1, 1)
		if d < lo || d > hi {
			continue
		}
		x := xOf(d)
		dl.AddRectFilled(imgui.NewVec2(x, y1+2*s), imgui.NewVec2(x+s, y1+6*s), tickCol)
		dl.AddTextVec2(imgui.NewVec2(x+3*s, y1+3*s), tickCol, strconv.Itoa(yr))
	}

	imgui.SetCursorScreenPos(imgui.NewVec2(origin.X, y1+imgui.TextLineHeight()+10*s))
	imgui.Dummy(imgui.NewVec2(w, 0))
}

// preview returns the schedule that dragging this drag's edge to the date gives. The date is where the edge's boundary lies, so the last day of an end is the day before. Snapping puts it on the nearest first of a month, except at the right edge of the axis, where an end is cleared.
func (d *timelineDrag) preview(boundary model.Date, snap bool) model.Schedule {
	if snap && boundary < d.hi {
		boundary = timeline.NearestMonth(boundary)
	}
	var next model.Schedule
	var err error
	if d.end {
		next, err = timeline.DropEnd(d.orig, d.seg, boundary, d.hi)
	} else {
		next, err = timeline.MoveStart(d.orig, d.seg, boundary)
	}
	if err != nil {
		return d.orig
	}
	return next
}

// timelineControls draws the value field and buttons for the selected segment, and the button that adds one after the last. lo is the axis start, standing in for a segment that begins at the beginning of time.
func (t *projTab) timelineControls(m model.Modification, segs []model.Segment, lo model.Date) {
	st := &t.tl
	if st.sel >= 0 {
		g := segs[st.sel]
		last, bounded := timeline.LastDay(segs, st.sel)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(fmt.Sprintf("Segment %d, %s. Value", st.sel+1, segmentRange(g.From, last, bounded)))
		imgui.SameLine()
		id := "tlvalue" + strconv.Itoa(st.sel)
		if text, done := t.field(id, "", formatValue(m.Property, m.Mode, g.Value), imgui.FontSize()*8); done {
			t.commit(id, func() {
				v, err := parseValue(m.Property, m.Mode, text)
				if err != nil {
					t.setErr(id, err.Error())
					return
				}
				t.timelineEdit("change segment value", m.Values, func(s model.Schedule) (model.Schedule, error) { return timeline.SetValue(s, st.sel, v) })
			})
		}
		imgui.SameLine()
		if imgui.Button("Split") {
			from := g.From
			if from == model.MinDate {
				from = lo
			}
			at := from + 365
			if bounded {
				at = from + (last-from+1)/2
			}
			if mo := timeline.NearestMonth(at); mo > from && (!bounded || mo <= last) {
				at = mo
			}
			t.timelineEdit("split segment", m.Values, func(s model.Schedule) (model.Schedule, error) { return timeline.Split(s, st.sel, at) })
		}
		tooltipOn("Cut the segment in two, half way along; both halves keep its value.")
		imgui.SameLine()
		if imgui.Button("Delete") {
			t.timelineEdit("delete segment", m.Values, func(s model.Schedule) (model.Schedule, error) { return timeline.Delete(s, st.sel) })
		}
		tooltipOn("Remove the segment (or press Delete). What it covered goes to the segment before, unless that has an end.")
		if st.sel == len(segs)-1 && g.End != nil {
			imgui.SameLine()
			if imgui.Button("No end") {
				t.timelineEdit("clear end", m.Values, timeline.ClearEnd)
			}
			tooltipOn("Let the last segment run on without end.")
		}
	} else {
		imgui.AlignTextToFramePadding()
		imgui.TextDisabled("Click a segment to select it.")
	}
	imgui.SameLine()
	if imgui.Button("Add after last") {
		l := segs[len(segs)-1]
		at := l.From + 365
		if l.From == model.MinDate {
			at = lo + 365
		}
		if l.End != nil {
			at = *l.End + 1
		}
		t.timelineEdit("add segment", m.Values, func(s model.Schedule) (model.Schedule, error) { return timeline.Append(s, at) })
	}
	tooltipOn("Add a segment after the last, with the last one's value.")
}

// timelineEdit applies do to the schedule the strip showed and writes the result to the modification, as one edit. A refusal is told to the user and changes nothing.
func (t *projTab) timelineEdit(label string, from model.Schedule, do func(model.Schedule) (model.Schedule, error)) {
	st := &t.tl
	next, err := do(from)
	if err != nil {
		t.a.notifyFailed(label, err)
		return
	}
	t.a.edit(label, func() (*ops.Result, error) {
		return ops.SetModValues(t.a.st, t.a.rec, st.proj, st.mod, next, ops.Options{})
	})
}

// segmentRange says when a segment holds, for labels and tooltips.
func segmentRange(from, last model.Date, bounded bool) string {
	a, b := "from the beginning", "no end"
	if from != model.MinDate {
		a = "from " + from.String()
	}
	if bounded {
		b = "to " + last.String()
	}
	return a + ", " + b
}

// tooltipOn shows msg as a tooltip while the last item is hovered.
func tooltipOn(msg string) {
	if imgui.IsItemHovered() {
		tooltipText(msg)
	}
}
