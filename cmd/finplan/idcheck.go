// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/joeriedel/finplan/internal/backend"
)

// idScanStep is the distance in pixels between the points the mouse visits; no widget is smaller than that in both directions.
const idScanStep = 10

// idConflict is one widget ID that more than one visible item used.
type idConflict struct {
	id     imgui.ID
	items  int
	at     imgui.Vec2
	window string
}

// idScan finds widget ID conflicts without anyone hovering: ImGui counts items sharing the ID of the item hovered in the previous frame, so the scan moves the mouse over a grid, one point per frame, and reads that count after each frame is drawn. Every item that can be hovered is covered, whatever draws it.
type idScan struct {
	cols, rows int
	frame      int
	// at and window say where the mouse was, and what it was over, in the previous frame.
	at     imgui.Vec2
	window string
	found  map[imgui.ID]idConflict
}

// newIDScan returns a scan of a display of width by height pixels.
func newIDScan(width, height float32) *idScan {
	return &idScan{
		cols:  int(width)/idScanStep + 1,
		rows:  int(height)/idScanStep + 1,
		found: map[imgui.ID]idConflict{},
	}
}

// done reports whether every point has been visited and its frame read.
func (s *idScan) done() bool { return s.frame > s.cols*s.rows }

// before moves the mouse to the next point; call it after the backend's new frame and before imgui.NewFrame.
func (s *idScan) before(io *imgui.IO) {
	if s.frame < s.cols*s.rows {
		io.AddMousePosEvent(float32(s.frame%s.cols*idScanStep), float32(s.frame/s.cols*idScanStep))
	}
}

// after reads the frame just drawn, before imgui.Render.
func (s *idScan) after(ctx *imgui.Context) {
	if s.frame > 0 {
		s.record(ctx.HoveredIdPreviousFrame(), int(ctx.HoveredIdPreviousFrameItemCount()), s.at, s.window)
	}
	s.at = imgui.NewVec2(float32(s.frame%s.cols*idScanStep), float32(s.frame/s.cols*idScanStep))
	s.window = ""
	if w := ctx.HoveredWindow(); w.CData != nil {
		s.window = w.Name()
	}
	s.frame++
}

// record notes that items visible items had id, which the mouse hovered at in the window, when there is more than one.
func (s *idScan) record(id imgui.ID, items int, at imgui.Vec2, window string) {
	if id != 0 && items > 1 {
		s.found[id] = idConflict{id: id, items: items, at: at, window: window}
	}
}

// err describes the conflicts found, or is nil.
func (s *idScan) err() error {
	if len(s.found) == 0 {
		return nil
	}
	list := make([]idConflict, 0, len(s.found))
	for _, c := range s.found {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].id < list[j].id })
	lines := make([]string, len(list))
	for i, c := range list {
		lines[i] = fmt.Sprintf("widget ID conflict: %d visible items share ID %#08x, hovered at (%.0f, %.0f) in window %q", c.items, uint32(c.id), c.at.X, c.at.Y, c.window)
	}
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}

// scanIDs draws the app again under a mouse that visits every point of the window, and reports the widget ID conflicts that shows. The frames are not shown.
func scanIDs(a *app) error {
	ctx := imgui.CurrentContext()
	io := imgui.CurrentIO()
	s := newIDScan(io.DisplaySize().X, io.DisplaySize().Y)
	for !s.done() {
		backend.NewFrame()
		s.before(io)
		imgui.NewFrame()
		a.draw()
		s.after(ctx)
		imgui.Render()
	}
	return s.err()
}
