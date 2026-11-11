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

// plotGeom is where an ImPlot plot is: its area in screen pixels and the limits of its axes, in unix seconds and dollars.
type plotGeom struct {
	pos, size              imgui.Vec2
	xMin, xMax, yMin, yMax float64
}

// readPlotGeom reads the geometry of the plot being built, which must have had its setup finished.
func readPlotGeom() plotGeom {
	lim := implot.GetPlotLimitsV(implot.AxisX1, implot.AxisY1)
	lo, hi := lim.Min(), lim.Max()
	return plotGeom{implot.GetPlotPos(), implot.GetPlotSize(), lo.X, hi.X, lo.Y, hi.Y}
}

// usable reports whether the plot has an area and ranges to draw in.
func (p plotGeom) usable() bool {
	return p.size.X > 0 && p.size.Y > 0 && p.xMax > p.xMin && p.yMax > p.yMin
}

// toPx converts a point of the plot, in unix seconds and dollars, to screen pixels.
func (p plotGeom) toPx(x, y float64) graph.Pt {
	return graph.Pt{
		X: p.pos.X + float32((x-p.xMin)/(p.xMax-p.xMin))*p.size.X,
		Y: p.pos.Y + float32((p.yMax-y)/(p.yMax-p.yMin))*p.size.Y,
	}
}

// constrainTimeAxis keeps the time axis of the plot being set up within the years the app handles and zoomed between a week and two centuries.
func constrainTimeAxis() {
	implot.SetupAxisLimitsConstraints(implot.AxisX1, float64(model.D(1971, 1, 1))*secondsPerDay, float64(model.D(2200, 1, 1))*secondsPerDay)
	implot.SetupAxisZoomConstraints(implot.AxisX1, 7*secondsPerDay, 200*365*secondsPerDay)
}

// setupMoneyTicks sets the ticks of the y axis, a dollar axis, of the plot being set up for the range yMin to yMax in a plot plotH pixels tall. ImPlot sizes the y gutter to the widest tick label. A last tick far outside the visible range, never drawn, is labelled (space-padded if need be) to be as wide as the widest tick label or value tag, which keeps the gutter the same width however the view moves and wide enough for the tags drawn in it.
func setupMoneyTicks(yMin, yMax float64, plotH float32) {
	ticks := graph.NiceTicks(yMin, yMax, max(3, int(plotH/(48*uiScale))))
	labels := make([]string, len(ticks), len(ticks)+1)
	for i, t := range ticks {
		labels[i] = graph.FormatMoneyCompact(t)
	}
	ticks = append(ticks, yMin-1000*max(yMax-yMin, math.Abs(yMin), 1))
	labels = append(labels, graph.GutterLabel(func(s string) float32 { return imgui.CalcTextSize(s).X }, implot.GetStyle().AnnotationPadding().X))
	implot.SetupAxisTicksdoublePtrV(implot.AxisY1, &ticks[0], int32(len(ticks)), labels, false)
}

// crosshairPoint is a series' value at the date under the pointer.
type crosshairPoint struct {
	amount float64
	color  graph.Color
}

// drawCrosshair draws, inside the plot's clip rect, the dashed vertical line through the pointer at screen column mouseX and, for each point, a dashed line from it to the plot's left edge and a dot on the series.
func drawCrosshair(dl *imgui.DrawList, p plotGeom, mouseX float32, pts []crosshairPoint) {
	s := uiScale
	left, top, bottom := p.pos.X, p.pos.Y, p.pos.Y+p.size.Y
	dashRGBA := u32(rgba(colMuted, 0.6))
	for _, sg := range graph.Dash([]graph.Pt{{X: mouseX, Y: top}, {X: mouseX, Y: bottom}}, 4*s, 4*s) {
		dl.AddLineArgs(vec(sg.A), vec(sg.B), dashRGBA, s)
	}
	for _, pt := range pts {
		at := graph.Pt{X: mouseX, Y: p.toPx(0, pt.amount).Y}
		col := lineColor(pt.color, 0.75)
		for _, sg := range graph.Dash([]graph.Pt{at, {X: left, Y: at.Y}}, 4*s, 4*s) {
			dl.AddLineArgs(vec(sg.A), vec(sg.B), u32(col), s)
		}
		dl.AddCircleFilled(vec(at), 3.5*s, u32(lineColor(pt.color, 1)))
	}
}

// pendingTag is a crosshair tag with its top-left corner, queued while the plot is being built, to be drawn once
// ImPlot has finished.
type pendingTag struct {
	at   graph.Pt
	bg   imgui.Vec4
	text string
}

// tagQueue holds the crosshair tags of a plot while it is built.
type tagQueue struct {
	tags []pendingTag
}

// crosshair queues the value tags (in the y-axis gutter, against the plot's left edge) of the points that lie in the plot's y range, and the date tag (in the x-axis area, below the plot) of day, which is queued only if some series has a value there, that is, if pts is not empty. The tags are our own rather than ImPlot's, which would resize the axis gutters as the pointer moves.
func (q *tagQueue) crosshair(p plotGeom, day model.Date, pts []crosshairPoint) {
	if len(pts) == 0 {
		return
	}
	plotPos := graph.Pt{X: p.pos.X, Y: p.pos.Y}
	plotSize := graph.Pt{X: p.size.X, Y: p.size.Y}
	for _, pt := range pts {
		if !graph.InRange(pt.amount, p.yMin, p.yMax) {
			continue
		}
		text := graph.FormatTagMoney(pt.amount)
		q.tags = append(q.tags, pendingTag{graph.ValueTagPos(p.toPx(0, pt.amount).Y, tagSize(text), plotPos, plotSize), lineColor(pt.color, 1), text})
	}
	dateText := graph.FormatDate(day)
	dx := p.toPx(float64(day)*secondsPerDay, 0).X
	if dayStart := float64(day) * secondsPerDay; dayStart+secondsPerDay > p.xMin && dayStart <= p.xMax {
		q.tags = append(q.tags, pendingTag{graph.DateTagPos(dx, tagSize(dateText), plotPos, plotSize), rgba(colSelect, 1), dateText})
	}
}

// draw draws the queued tags on the window, after implot.EndPlot, so that ImPlot's tick marks and border do not paint over them, and empties the queue.
func (q *tagQueue) draw() {
	dl := imgui.WindowDrawList()
	for _, t := range q.tags {
		drawTag(dl, t.at, t.bg, t.text)
	}
	q.tags = q.tags[:0]
}

// tagSize returns the size of a tag around the text, with ImPlot's annotation padding.
func tagSize(text string) graph.Pt {
	pad := implot.GetStyle().AnnotationPadding()
	ts := imgui.CalcTextSize(text)
	return graph.Pt{X: ts.X + 2*pad.X, Y: ts.Y + 2*pad.Y}
}

// drawTag draws a filled tag with its top-left corner at p, with text black or white by the background's
// brightness, as ImPlot does for its own axis tags.
func drawTag(dl *imgui.DrawList, p graph.Pt, bg imgui.Vec4, text string) {
	pad := implot.GetStyle().AnnotationPadding()
	size := tagSize(text)
	fg := uint32(0xffffffff)
	if graph.TagTextBlack(bg.X, bg.Y, bg.Z) {
		fg = 0xff000000
	}
	dl.AddRectFilled(vec(p), vec(graph.Pt{X: p.X + size.X, Y: p.Y + size.Y}), u32(bg))
	dl.AddTextVec2(imgui.NewVec2(p.X+pad.X, p.Y+pad.Y), fg, text)
}
