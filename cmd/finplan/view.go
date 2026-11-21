// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/AllenDang/cimgui-go/implot"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// The time axis of the plots stays within these years and zoomed between a week and two centuries, in unix seconds.
var (
	timeAxisMin = float64(model.D(1971, 1, 1)) * secondsPerDay
	timeAxisMax = float64(model.D(2200, 1, 1)) * secondsPerDay
)

const (
	minTimeSpan = 7 * secondsPerDay
	maxTimeSpan = 200 * 365 * secondsPerDay
	// zoomPerNotch is the factor the visible time span shrinks by for one notch of the mouse wheel.
	zoomPerNotch = 0.8
)

// fitTimeSpan returns the span lo..hi kept within the years the plots handle, the span's length clamped between a week and two centuries about its middle.
func fitTimeSpan(lo, hi float64) (float64, float64) {
	if !(hi > lo) {
		hi = lo + minTimeSpan
	}
	span := hi - lo
	if clamped := min(max(span, minTimeSpan), maxTimeSpan); clamped != span {
		mid := (lo + hi) / 2
		lo, span = mid-clamped/2, clamped
	}
	lo = min(max(lo, timeAxisMin), timeAxisMax-span)
	return lo, lo + span
}

// zoomTimeSpan zooms the span lo..hi by notches of the mouse wheel, positive in, keeping the time anchor at the same place in the span.
func zoomTimeSpan(lo, hi, anchor, notches float64) (float64, float64) {
	span := min(max((hi-lo)*math.Pow(zoomPerNotch, notches), minTimeSpan), maxTimeSpan)
	frac := min(max((anchor-lo)/(hi-lo), 0), 1)
	return fitTimeSpan(anchor-frac*span, anchor-frac*span+span)
}

// panTimeSpan moves the span lo..hi by dx seconds, stopping at the ends of the time axis.
func panTimeSpan(lo, hi, dx float64) (float64, float64) {
	return fitTimeSpan(lo+dx, hi+dx)
}

// timeInput turns the mouse over a time plot into changes of its x range: the wheel zooms the time axis around the pointer and a right drag pans it. The plot area is that of the last frame, so the range is changed before the plot is built, which lets the y range be fitted to it in the same frame.
type timeInput struct {
	panning bool
}

// overPlot reports whether the pointer is over the plot area at pos with size, in the window being drawn and not behind a popup.
func overPlot(pos, size imgui.Vec2) bool {
	return size.X > 0 && size.Y > 0 && imgui.IsWindowHovered() &&
		imgui.IsMouseHoveringRectV(pos, imgui.NewVec2(pos.X+size.X, pos.Y+size.Y), false)
}

// apply returns the range lo..hi changed by this frame's mouse input over the plot area at pos with size, and whether it was.
func (ti *timeInput) apply(lo, hi float64, pos, size imgui.Vec2) (float64, float64, bool) {
	over := overPlot(pos, size)
	if over && imgui.IsMouseClickedBool(imgui.MouseButtonRight) {
		ti.panning = true
	}
	if !imgui.IsMouseDown(imgui.MouseButtonRight) {
		ti.panning = false
	}
	io := imgui.CurrentIO()
	nlo, nhi := lo, hi
	if ti.panning && size.X > 0 {
		if dx := io.MouseDelta().X; dx != 0 {
			nlo, nhi = panTimeSpan(nlo, nhi, -float64(dx)/float64(size.X)*(nhi-nlo))
			imgui.SetMouseCursor(imgui.MouseCursorResizeEW)
		}
	}
	if w := io.MouseWheel(); over && w != 0 {
		anchor := nlo + float64(imgui.MousePos().X-pos.X)/float64(size.X)*(nhi-nlo)
		nlo, nhi = zoomTimeSpan(nlo, nhi, anchor, float64(w))
	}
	return nlo, nhi, nlo != lo || nhi != hi
}

// beginTimePlot begins a plot whose time axis the app owns: ImPlot's own mouse input is turned off, which leaves it the hover state, by moving its pan to a mouse button no mouse sends the plot and its zoom rate to nothing, no box select and no menus. ImPlot's input map is global, so the plot must be ended with endTimePlot. The limits are set every frame, from the app's view.
func beginTimePlot(id string, size imgui.Vec2, xMin, xMax, yMin, yMax float64, yTicks *moneyTicks, plotH float32) bool {
	m := implot.GetInputMap()
	inputPan, inputZoom = m.Pan(), m.ZoomRate()
	m.SetPan(imgui.MouseButton(4))
	m.SetZoomRate(0)
	flags := implot.FlagsNoTitle | implot.FlagsNoLegend | implot.FlagsNoMouseText | implot.FlagsNoMenus | implot.FlagsNoBoxSelect
	if !implot.BeginPlotV(id, size, flags) {
		restoreInputMap()
		return false
	}
	implot.SetupAxisV(implot.AxisX1, "", implot.AxisFlagsNoLabel|implot.AxisFlagsNoMenus)
	implot.SetupAxisScalePlotScale(implot.AxisX1, implot.ScaleTime)
	implot.SetupAxisV(implot.AxisY1, "", implot.AxisFlagsNoLabel|implot.AxisFlagsNoMenus)
	constrainTimeAxis()
	yTicks.setup(yMin, yMax, plotH)
	implot.SetupAxisLimitsV(implot.AxisX1, xMin, xMax, implot.CondAlways)
	implot.SetupAxisLimitsV(implot.AxisY1, yMin, yMax, implot.CondAlways)
	implot.SetupFinish()
	return true
}

// inputPan and inputZoom are ImPlot's pan button and zoom rate before beginTimePlot changed them.
var (
	inputPan  imgui.MouseButton
	inputZoom float32
)

// endTimePlot ends the plot begun by beginTimePlot and puts ImPlot's input map back.
func endTimePlot() {
	implot.EndPlot()
	restoreInputMap()
}

func restoreInputMap() {
	m := implot.GetInputMap()
	m.SetPan(inputPan)
	m.SetZoomRate(inputZoom)
}

// moneyTicks is the tick set of a plot's dollar axis, made again only when the range or the plot height changes.
type moneyTicks struct {
	yMin, yMax float64
	plotH      float32
	ticks      []float64
	labels     []string
}

// setup sets the ticks of the y axis, a dollar axis, of the plot being set up for the range yMin to yMax in a plot plotH pixels tall; the range is the one the plot's limits are set to in the same frame. ImPlot sizes the y gutter to the widest tick label. A last tick far outside the visible range, never drawn, is labelled (space-padded if need be) to be as wide as the widest tick label or value tag, which keeps the gutter the same width however the view moves and wide enough for the tags drawn in it.
func (m *moneyTicks) setup(yMin, yMax float64, plotH float32) {
	m.fill(yMin, yMax, plotH, func() string {
		return graph.GutterLabel(func(s string) float32 { return imgui.CalcTextSize(s).X }, implot.GetStyle().AnnotationPadding().X)
	})
	implot.SetupAxisTicksdoublePtrV(implot.AxisY1, &m.ticks[0], int32(len(m.ticks)), m.labels, false)
}

// fill makes the ticks for the range and plot height, unless they are already those of the last call; gutter returns the label of the hidden last tick.
func (m *moneyTicks) fill(yMin, yMax float64, plotH float32, gutter func() string) {
	if len(m.ticks) > 0 && m.yMin == yMin && m.yMax == yMax && m.plotH == plotH {
		return
	}
	m.yMin, m.yMax, m.plotH = yMin, yMax, plotH
	m.ticks = append(m.ticks[:0], graph.NiceTicks(yMin, yMax, max(3, int(plotH/(48*uiScale))))...)
	m.labels = m.labels[:0]
	for _, t := range m.ticks {
		m.labels = append(m.labels, graph.FormatMoneyCompact(t))
	}
	m.ticks = append(m.ticks, yMin-1000*max(yMax-yMin, math.Abs(yMin), 1))
	m.labels = append(m.labels, gutter())
}
