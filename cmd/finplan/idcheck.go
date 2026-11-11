// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/joeriedel/finplan/internal/backend"
)

const (
	// idScanMaxFrames bounds the frames a scan draws, one per mouse point; a window that needs more is an error, not a long wait.
	idScanMaxFrames = 16000
	// idScanMaxTime bounds the wall-clock time a scan takes, so that a slow tab ends in its own error before the smoke test's case timeout kills the process.
	idScanMaxTime = 20 * time.Second
	// idScanSettle is how many frames are drawn after the display changes size, with the mouse away, before the layout is read.
	idScanSettle = 4
	// idScanGrowTries is how many times the display is made taller or wider to show content that overflowed its window.
	idScanGrowTries = 4
	// idScanMaxHeight is the tallest display the scan makes, in pixels.
	idScanMaxHeight = 3000
)

// idConflict is one widget ID that more than one visible item used.
type idConflict struct {
	id     imgui.ID
	items  int
	at     imgui.Vec2
	window string
}

// idScan finds widget ID conflicts without anyone hovering: ImGui counts items sharing the ID of the item hovered in the previous frame, so the scan moves the mouse over a grid, one point per frame, and reads that count after each frame is drawn. An item is covered when the mouse can reach it: it is drawn on screen in the state scanned, it is not behind an open popup (a popup blocks hover in the windows outside its stack, so the forms behind a picker are scanned in a separate state with the picker closed), and the grid steps are no larger than the item, which holds for text and buttons but not for the 10 px timeline drag handles, which no scanned state shows.
type idScan struct {
	cols, rows int
	// stepX and stepY are the distances in pixels between the points the mouse visits.
	stepX, stepY float32
	frame        int
	// at and window say where the mouse was, and what it was over, in the previous frame.
	at     imgui.Vec2
	window string
	found  map[imgui.ID]idConflict
}

// newIDScan returns a scan of the width by height pixels at the top left of the display, with points stepX and stepY apart.
func newIDScan(width, height, stepX, stepY float32) *idScan {
	return &idScan{
		cols:  int(width/stepX) + 1,
		rows:  int(height/stepY) + 1,
		stepX: stepX,
		stepY: stepY,
		found: map[imgui.ID]idConflict{},
	}
}

// points is the number of points the scan visits.
func (s *idScan) points() int { return s.cols * s.rows }

// tooLarge is the error for a grid that needs more than idScanMaxFrames frames, or nil.
func (s *idScan) tooLarge() error {
	if s.points() > idScanMaxFrames {
		return fmt.Errorf("the ID scan needs %d frames (%dx%d points), more than the %d it may draw", s.points(), s.cols, s.rows, idScanMaxFrames)
	}
	return nil
}

// checkTime is the error for a scan begun at start that has run longer than idScanMaxTime at now, or nil.
func checkTime(start, now time.Time) error {
	if now.Sub(start) > idScanMaxTime {
		return fmt.Errorf("id scan: exceeded %v of wall-clock time", idScanMaxTime)
	}
	return nil
}

// done reports whether every point has been visited and its frame read, or the frame bound is reached.
func (s *idScan) done() bool { return s.frame > s.points() || s.frame > idScanMaxFrames }

// point is where the mouse is for frame n.
func (s *idScan) point(n int) imgui.Vec2 {
	return imgui.NewVec2(float32(n%s.cols)*s.stepX, float32(n/s.cols)*s.stepY)
}

// before moves the mouse to the next point; call it after the backend's new frame and before imgui.NewFrame.
func (s *idScan) before(io *imgui.IO) {
	if s.frame < s.points() {
		p := s.point(s.frame)
		io.AddMousePosEvent(p.X, p.Y)
	}
}

// after reads the frame just drawn, before imgui.Render.
func (s *idScan) after(ctx *imgui.Context) {
	if s.frame > 0 {
		s.record(ctx.HoveredIdPreviousFrame(), int(ctx.HoveredIdPreviousFrameItemCount()), s.at, s.window)
	}
	s.at = s.point(s.frame)
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

// layout is what a settled frame says about the windows drawn.
type layout struct {
	// overflow is how far, at most, a window's content reaches past its scroll range, and window names the one it is for.
	overflow imgui.Vec2
	window   string
	// bounded names the windows that hide content by design, such as the lists in popups and the clipped entries table; the scan cannot reach what they hide.
	bounded []string
	// bottom is the lowest y at which any window with no child windows has content.
	bottom float32
}

// windows lists the windows of the context. cimgui-go wraps the context's window vector, which holds pointers to windows, as if it held windows, so the vector's data pointer is read as the array it is.
func windows(ctx *imgui.Context) []*imgui.Window {
	v := ctx.Windows()
	ptrs := unsafe.Slice((*unsafe.Pointer)(unsafe.Pointer(v.Data.CData)), v.Size)
	wins := make([]*imgui.Window, len(ptrs))
	for i, p := range ptrs {
		wins[i] = imgui.NewWindowFromC(p)
	}
	return wins
}

// inPopup reports whether w is a popup or a child of one, whose height ImGui caps, so that its list scrolls. It and scrollsByDesign decide which windows readLayout treats as bounded.
func inPopup(w *imgui.Window) bool {
	for ; w != nil && w.CData != nil; w = w.ParentWindow() {
		f := w.Flags()
		if f&imgui.WindowFlagsPopup != 0 {
			return true
		}
		if f&imgui.WindowFlagsChildWindow == 0 {
			return false
		}
	}
	return false
}

// scrollsByDesign reports whether w is the scrolling window of the entries table, which draws only the visible rows of a ledger that is many screens tall. ImGui names that window "<parent>/<table ID>_<hex ID>".
func scrollsByDesign(w *imgui.Window) bool {
	return strings.Contains(w.Name(), "/"+entriesTableID+"_")
}

// readLayout measures the active windows of the frame just drawn.
func readLayout(ctx *imgui.Context) layout {
	var l layout
	wins := windows(ctx)
	parents := map[imgui.Window]bool{}
	for _, w := range wins {
		if w.Active() && w.Flags()&imgui.WindowFlagsChildWindow != 0 {
			parents[*w.ParentWindow()] = true
		}
	}
	pad := imgui.CurrentStyle().WindowPadding().Y
	// What hides less than a scan step cannot hide a whole item, and the part of an item that shows is hovered.
	step := fontSize * uiScale
	for _, w := range wins {
		if !w.Active() || w.Hidden() {
			continue
		}
		if m := w.ScrollMax(); (m.X >= step || m.Y >= step) && w.Flags()&imgui.WindowFlagsMenuBar == 0 {
			if inPopup(w) || scrollsByDesign(w) {
				l.bounded = append(l.bounded, fmt.Sprintf("%s (%.0f px)", w.Name(), m.Y))
			} else {
				if l.window == "" || m.Y > l.overflow.Y {
					l.window = w.Name()
				}
				l.overflow = imgui.NewVec2(max(l.overflow.X, m.X), max(l.overflow.Y, m.Y))
			}
		}
		if parents[*w] {
			continue
		}
		// The title and menu bars are not in ContentSize.
		b := w.Pos().Y + w.ContentSize().Y + 2*pad + 2*imgui.FrameHeight()
		l.bottom = max(l.bottom, min(b, w.Pos().Y+w.Size().Y))
	}
	return l
}

// scanIDs draws the app again under a mouse that visits every point of its windows, and reports the widget ID conflicts that shows, in the items the mouse can reach (see idScan). The display is made as large as it takes for no window to hide content below or beside its scroll range, so nothing escapes the mouse without a scroll; a window that still hides content is an error. The frames are not shown.
func scanIDs(a *app) error {
	start := time.Now()
	ctx := imgui.CurrentContext()
	io := imgui.CurrentIO()
	size := io.DisplaySize()
	away := imgui.NewVec2(-math.MaxFloat32, -math.MaxFloat32)
	frame := func(s *idScan) {
		backend.NewFrame()
		io.SetDisplaySize(size)
		if s == nil {
			io.AddMousePosEvent(away.X, away.Y)
		} else {
			s.before(io)
		}
		imgui.NewFrame()
		a.draw()
		if s != nil {
			s.after(ctx)
		}
		imgui.Render()
	}

	var l layout
	for try := 0; ; try++ {
		for range idScanSettle {
			frame(nil)
		}
		l = readLayout(ctx)
		if l.window == "" {
			break
		}
		if try == idScanGrowTries || size.Y+l.overflow.Y > idScanMaxHeight {
			return fmt.Errorf("window %q hides content beyond its scroll range (%.0f px down, %.0f px across) in a %.0fx%.0f display (the scan makes it at most %d px tall), so it cannot reach that content", l.window, l.overflow.Y, l.overflow.X, size.X, size.Y, idScanMaxHeight)
		}
		size = imgui.NewVec2(size.X+l.overflow.X, size.Y+l.overflow.Y)
	}

	if len(l.bounded) > 0 {
		log.Printf("id scan: these windows scroll by design, so what is below their scroll range is not scanned: %s", strings.Join(l.bounded, ", "))
	}
	// A line of text is the smallest item, so a point every font size covers each one; the 10 px timeline drag handles (timeline.go) are the exception.
	step := fontSize * uiScale
	s := newIDScan(size.X, min(l.bottom, size.Y), step, step)
	if err := s.tooLarge(); err != nil {
		return err
	}
	log.Printf("id scan: %d frames, %dx%d points %.0f px apart, display %.0fx%.0f", s.points(), s.cols, s.rows, step, size.X, size.Y)
	for !s.done() {
		if err := checkTime(start, time.Now()); err != nil {
			return fmt.Errorf("%w after %d of %d frames", err, s.frame, s.points())
		}
		frame(s)
	}
	return s.err()
}
