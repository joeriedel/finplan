// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/backend"
	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
)

const (
	// frameTimesLimit bounds the whole frame-time run, including the waits for Monte Carlo and for the background work to settle.
	frameTimesLimit = 600 * time.Second
	// frameBudget is the time a frame may take before it counts as a hitch: a frame of a 60 Hz display.
	frameBudget = 16 * time.Millisecond
	// frameInterval is the time between the starts of frames, as on a 60 Hz display, so that background work gets the time between frames that it has when the app is used.
	frameInterval = 16 * time.Millisecond
	// frameTimesSettle is how many frames are drawn, untimed, before each gesture so that the view and the caches have settled.
	frameTimesSettle = 6
)

// panButton is the mouse button whose drag pans the plots.
const panButton = imgui.MouseButtonRight

// frameRig draws frames of the app under synthetic input and times the CPU work of each, from imgui.NewFrame to imgui.Render, which leaves out the window system and the GL submit that an offscreen display makes unrepresentative.
type frameRig struct {
	a     *app
	io    *imgui.IO
	size  imgui.Vec2
	times []time.Duration
	last  time.Time
}

// frame draws one frame after input moves the mouse, and records its time if timed. Frames start frameInterval apart.
func (r *frameRig) frame(input func(io *imgui.IO), timed bool) {
	time.Sleep(time.Until(r.last.Add(frameInterval)))
	r.last = time.Now()
	backend.NewFrame()
	r.io.SetDisplaySize(r.size)
	if input != nil {
		input(r.io)
	}
	start := time.Now()
	imgui.NewFrame()
	r.a.draw()
	imgui.Render()
	if timed {
		r.times = append(r.times, time.Since(start))
	}
}

// gesture is a scripted input: frame n of count frames moves the mouse and presses buttons, with the plot at plot.
type gesture struct {
	name  string
	count int
	input func(io *imgui.IO, n int, plot plotGeom)
}

const (
	// panSweepFrames is the frames of one drag of the pan: the button goes down on the second, the pointer moves panStepPx a frame on the next panDragFrames, and the button goes up before the pointer returns to the start.
	panSweepFrames = 12
	panDragFrames  = 8
	panStepPx      = 70
	// panSweeps is how many drags the pan makes in one direction before it turns; together they travel several widths of the view.
	panSweeps = 10
)

// frameGestures are the gestures timed. The pan drags with the button that pans, again and again in one direction and then the other, far enough to leave every grid the view had cached.
func frameGestures() []gesture {
	away := func(io *imgui.IO) { io.AddMousePosEvent(-math.MaxFloat32, -math.MaxFloat32) }
	return []gesture{
		{"idle", 60, func(io *imgui.IO, n int, p plotGeom) { away(io) }},
		{"hover", 90, func(io *imgui.IO, n int, p plotGeom) {
			io.AddMousePosEvent(p.pos.X+20+(p.size.X-40)*float32(n)/89, p.pos.Y+p.size.Y/2)
		}},
		{"pan", 2 * panSweeps * panSweepFrames, func(io *imgui.IO, n int, p plotGeom) {
			sweep, k := n/panSweepFrames, n%panSweepFrames
			// Dragging left moves the view later; the second half of the sweeps drags right.
			left := sweep < panSweeps
			start, dir := p.size.X*0.2, float32(1)
			if left {
				start, dir = p.size.X*0.8, -1
			}
			step := float32(min(max(k-1, 0), panDragFrames))
			io.AddMousePosEvent(p.pos.X+start+dir*panStepPx*step, p.pos.Y+p.size.Y/2)
			if k == 1 {
				io.AddMouseButtonEvent(int32(panButton), true)
			}
			if k == panDragFrames+1 {
				io.AddMouseButtonEvent(int32(panButton), false)
			}
		}},
		{"zoom", 60, func(io *imgui.IO, n int, p plotGeom) {
			io.AddMousePosEvent(p.pos.X+p.size.X/2, p.pos.Y+p.size.Y/2)
			wheel := float32(1)
			if n/15%2 == 1 {
				wheel = -1
			}
			io.AddMouseWheelEvent(0, wheel)
		}},
	}
}

// frameViews are the views each gesture starts from: the range buttons that show the most data at once, and the one the app opens on.
var frameViews = []struct {
	name string
	set  func(g *graphTab) bool
}{
	{"All", func(g *graphTab) bool { g.setRange(graph.RangeButtons[len(graph.RangeButtons)-1]); return true }},
	{"Age95", func(g *graphTab) bool {
		from, to, _, ok := graph.LifespanSpan(g.rec.Household, g.rec.Today)
		if ok {
			g.setSpan(from, to)
		}
		return ok
	}},
	{"30Y", func(g *graphTab) bool { g.setRange(graph.RangeButtons[3]); return true }},
	{"5Y", func(g *graphTab) bool { g.setRange(graph.RangeButtons[1]); return true }},
}

// frameStats summarises times: the mean, the 95th percentile, the maximum, the number of frames over budget, and the longest stall, the most time that consecutive frames over budget took together.
func frameStats(times []time.Duration) (mean, p95, maxT time.Duration, over int, stall time.Duration) {
	if len(times) == 0 {
		return
	}
	sorted := slices.Clone(times)
	slices.Sort(sorted)
	var sum, run time.Duration
	for _, t := range times {
		sum += t
		if t > frameBudget {
			over++
			run += t
			stall = max(stall, run)
		} else {
			run = 0
		}
	}
	return sum / time.Duration(len(times)), sorted[min(len(sorted)-1, len(sorted)*95/100)], sorted[len(sorted)-1], over, stall
}

// settle draws frames, untimed, until the background work is done and the view and the caches have settled.
func (r *frameRig) settle(deadline time.Time, input func(io *imgui.IO)) error {
	for range frameTimesSettle {
		r.frame(input, false)
	}
	for r.a.busy() {
		if time.Now().After(deadline) {
			return fmt.Errorf("the background work was still running after %v", frameTimesLimit)
		}
		r.frame(input, false)
	}
	for range frameTimesSettle {
		r.frame(input, false)
	}
	return nil
}

// frameTimes draws the Graph under scripted pan, zoom and hover from each of several views, with and without Monte Carlo bands on the Net worth line, and writes a table of the CPU time per frame of each gesture to w.
func frameTimes(a *app, w io.Writer) error {
	deadline := time.Now().Add(frameTimesLimit)
	r := &frameRig{a: a, io: imgui.CurrentIO()}
	r.size = r.io.DisplaySize()
	away := func(io *imgui.IO) { io.AddMousePosEvent(-math.MaxFloat32, -math.MaxFloat32) }
	fmt.Fprintf(w, "display %.0fx%.0f, budget %v, frames start %v apart, times are imgui.NewFrame to imgui.Render on one thread\n", r.size.X, r.size.Y, frameBudget, frameInterval)
	for _, mc := range []bool{false, true} {
		if mc {
			var found bool
			for _, ln := range a.graph.lines.All() {
				if ln.Asset == demodata.NetWorth {
					a.graph.lines.SetMonteCarlo(ln.ID, true)
					found = true
				}
			}
			if !found {
				return fmt.Errorf("the Graph has no Net worth line")
			}
			// The jobs start when the lines are drawn with Monte Carlo on, so draw a frame before asking whether they are busy.
			r.frame(nil, false)
			if err := r.settle(deadline, nil); err != nil {
				return err
			}
		}
		fmt.Fprintf(w, "\nmonte carlo %v, %d lines\n%-6s %-6s %6s %9s %9s %9s %12s %9s\n", map[bool]string{false: "off", true: "on"}[mc], len(a.graph.lines.All()), "view", "gest", "frames", "mean", "p95", "max", "over "+frameBudget.String(), "stall")
		for _, v := range frameViews {
			for _, g := range frameGestures() {
				if !v.set(a.graph) {
					return fmt.Errorf("the view %s is not available", v.name)
				}
				// Every gesture starts with nothing cached, as the first view of a user's session does.
				a.graph.cache.close()
				a.graph.bkCache.close()
				a.graph.cache, a.graph.bkCache = newDataCache(a.graph.rec), newBreakdownCache(a.graph.rec)
				if err := r.settle(deadline, away); err != nil {
					return err
				}
				plot := a.graph.geom()
				r.times = r.times[:0]
				for n := 0; n < g.count; n++ {
					if time.Now().After(deadline) {
						return fmt.Errorf("the frame-time run exceeded %v", frameTimesLimit)
					}
					r.frame(func(io *imgui.IO) { g.input(io, n, plot) }, true)
				}
				r.frame(func(io *imgui.IO) {
					io.AddMouseButtonEvent(int32(panButton), false)
					away(io)
				}, false)
				mean, p95, maxT, over, stall := frameStats(r.times)
				fmt.Fprintf(w, "%-6s %-6s %6d %9s %9s %9s %12d %9s\n", v.name, g.name, len(r.times), mean.Round(time.Microsecond), p95.Round(time.Microsecond), maxT.Round(time.Microsecond), over, stall.Round(time.Microsecond))
			}
		}
	}
	return nil
}
