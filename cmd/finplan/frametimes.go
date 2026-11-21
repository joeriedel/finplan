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
	// frameTimesLimit bounds the whole frame-time run, including the wait for Monte Carlo.
	frameTimesLimit = 90 * time.Second
	// frameBudget is the time a frame may take before it counts as a hitch.
	frameBudget = 8 * time.Millisecond
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
}

// frame draws one frame after input moves the mouse, and records its time if timed.
func (r *frameRig) frame(input func(io *imgui.IO), timed bool) {
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

// frameGestures are the gestures timed. The pan is a drag with the button that pans.
func frameGestures() []gesture {
	away := func(io *imgui.IO) { io.AddMousePosEvent(-math.MaxFloat32, -math.MaxFloat32) }
	return []gesture{
		{"idle", 60, func(io *imgui.IO, n int, p plotGeom) { away(io) }},
		{"hover", 90, func(io *imgui.IO, n int, p plotGeom) {
			io.AddMousePosEvent(p.pos.X+20+(p.size.X-40)*float32(n)/89, p.pos.Y+p.size.Y/2)
		}},
		{"pan", 90, func(io *imgui.IO, n int, p plotGeom) {
			// Drag 150 px to the left and back, 5 px a frame.
			step := float32(n % 30)
			if n/30%2 == 1 {
				step = 30 - step
			}
			io.AddMousePosEvent(p.pos.X+p.size.X*0.75-5*step, p.pos.Y+p.size.Y/2)
			if n == 0 {
				io.AddMouseButtonEvent(int32(panButton), true)
			}
			if n == 89 {
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

// frameStats summarises times: the mean, the 95th percentile, the maximum and the number of frames over budget.
func frameStats(times []time.Duration) (mean, p95, maxT time.Duration, over int) {
	if len(times) == 0 {
		return
	}
	sorted := slices.Clone(times)
	slices.Sort(sorted)
	var sum time.Duration
	for _, t := range times {
		sum += t
		if t > frameBudget {
			over++
		}
	}
	return sum / time.Duration(len(times)), sorted[min(len(sorted)-1, len(sorted)*95/100)], sorted[len(sorted)-1], over
}

// frameTimes draws the Graph under scripted pan, zoom and hover, with and without Monte Carlo bands on the Net worth line, and writes a table of the CPU time per frame of each gesture to w.
func frameTimes(a *app, w io.Writer) error {
	deadline := time.Now().Add(frameTimesLimit)
	r := &frameRig{a: a, io: imgui.CurrentIO()}
	r.size = r.io.DisplaySize()
	fmt.Fprintf(w, "display %.0fx%.0f, budget %v, times are imgui.NewFrame to imgui.Render on one thread\n", r.size.X, r.size.Y, frameBudget)
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
			for a.graph.mc.busy() {
				if time.Now().After(deadline) {
					return fmt.Errorf("the Monte Carlo jobs were still running after %v", frameTimesLimit)
				}
				time.Sleep(10 * time.Millisecond)
			}
			for range frameTimesSettle {
				r.frame(nil, false)
			}
		}
		fmt.Fprintf(w, "\nmonte carlo %v, %d lines\n%-8s %6s %9s %9s %9s %12s\n", map[bool]string{false: "off", true: "on"}[mc], len(a.graph.lines.All()), "gesture", "frames", "mean", "p95", "max", "over "+frameBudget.String())
		for _, g := range frameGestures() {
			a.graph.setRange(graph.RangeButtons[1])
			for range frameTimesSettle {
				r.frame(func(io *imgui.IO) { io.AddMousePosEvent(-math.MaxFloat32, -math.MaxFloat32) }, false)
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
				io.AddMousePosEvent(-math.MaxFloat32, -math.MaxFloat32)
			}, false)
			mean, p95, maxT, over := frameStats(r.times)
			fmt.Fprintf(w, "%-8s %6d %9s %9s %9s %12d\n", g.name, len(r.times), mean.Round(time.Microsecond), p95.Round(time.Microsecond), maxT.Round(time.Microsecond), over)
		}
	}
	return nil
}
