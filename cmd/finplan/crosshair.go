// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"

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

// constrainTimeAxis keeps the time axis of the plot being set up within the years the app handles and zoomed between a week and two centuries, as the app's own view does (see fitTimeSpan).
func constrainTimeAxis() {
	implot.SetupAxisLimitsConstraints(implot.AxisX1, timeAxisMin, timeAxisMax)
	implot.SetupAxisZoomConstraints(implot.AxisX1, minTimeSpan, maxTimeSpan)
}

// crosshairPoint is a series' value at the date under the pointer.
type crosshairPoint struct {
	amount float64
	color  graph.Color
}

// drawCrosshair draws, inside the plot's clip rect, the vertical line through screen column x and, for each point, a line from it to the plot's left edge and a dot on the series. The locked cursor's is solid and bold, the pointer's dashed and faint.
func drawCrosshair(dl *imgui.DrawList, p plotGeom, x float32, pts []crosshairPoint, locked bool) {
	s := uiScale
	left, top, bottom := p.pos.X, p.pos.Y, p.pos.Y+p.size.Y
	if locked {
		dl.AddLineArgs(imgui.NewVec2(x, top), imgui.NewVec2(x, bottom), u32(rgba(colSelect, 0.95)), 2*s)
	} else {
		dashRGBA := u32(rgba(colMuted, 0.6))
		for _, sg := range graph.Dash([]graph.Pt{{X: x, Y: top}, {X: x, Y: bottom}}, 4*s, 4*s) {
			dl.AddLineArgs(vec(sg.A), vec(sg.B), dashRGBA, s)
		}
	}
	for _, pt := range pts {
		at := graph.Pt{X: x, Y: p.toPx(0, pt.amount).Y}
		if locked {
			dl.AddLineArgs(vec(at), imgui.NewVec2(left, at.Y), u32(lineColor(pt.color, 0.6)), s)
			dl.AddCircleFilled(vec(at), 4.5*s, u32(lineColor(pt.color, 1)))
			dl.AddCircleV(vec(at), 4.5*s, u32(rgba(colSelect, 1)), 0, 1.5*s)
			continue
		}
		col := lineColor(pt.color, 0.75)
		for _, sg := range graph.Dash([]graph.Pt{at, {X: left, Y: at.Y}}, 4*s, 4*s) {
			dl.AddLineArgs(vec(sg.A), vec(sg.B), u32(col), s)
		}
		dl.AddCircleFilled(vec(at), 3.5*s, u32(lineColor(pt.color, 1)))
	}
}

// pendingTag is a crosshair tag with its top-left corner, queued while the plot is being built, to be drawn once ImPlot has finished.
type pendingTag struct {
	at   graph.Pt
	bg   imgui.Vec4
	text string
}

// tagQueue holds the crosshair tags of a plot while it is built.
type tagQueue struct {
	tags []pendingTag
}

// tagSet is the points of a cursor at a day, whose tags the queue takes. The day is queued only if some series has a value there, that is, if pts is not empty.
type tagSet struct {
	day model.Date
	pts []crosshairPoint
}

// crosshair queues the value tags of the points that lie in the plot's y range, stacked so that none overlaps another: the first set's in the y-axis gutter, against the plot's left edge, and the others', which would overlap them there, against the plot's right edge, and the date tag (in the x-axis area, below the plot) of each set's day. A date tag that would overlap an earlier set's is left out. The tags are our own rather than ImPlot's, which would resize the axis gutters as the pointer moves.
func (q *tagQueue) crosshair(p plotGeom, sets ...tagSet) {
	plotPos := graph.Pt{X: p.pos.X, Y: p.pos.Y}
	plotSize := graph.Pt{X: p.size.X, Y: p.size.Y}
	for k, set := range sets {
		var ys []float32
		var sizes []graph.Pt
		var texts []string
		var cols []imgui.Vec4
		for _, pt := range set.pts {
			if !graph.InRange(pt.amount, p.yMin, p.yMax) {
				continue
			}
			text := graph.FormatTagMoney(pt.amount)
			ys, sizes, texts, cols = append(ys, p.toPx(0, pt.amount).Y), append(sizes, tagSize(text)), append(texts, text), append(cols, lineColor(pt.color, 1))
		}
		for i, at := range graph.StackValueTags(ys, sizes, uiScale, plotPos, plotSize) {
			if k > 0 {
				at.X = plotPos.X + plotSize.X - sizes[i].X
			}
			q.tags = append(q.tags, pendingTag{at, cols[i], texts[i]})
		}
	}
	type span struct{ lo, hi float32 }
	var taken []span
	for _, set := range sets {
		dayStart := float64(set.day) * secondsPerDay
		if len(set.pts) == 0 || dayStart+secondsPerDay <= p.xMin || dayStart > p.xMax {
			continue
		}
		text := graph.FormatDate(set.day)
		size := tagSize(text)
		at := graph.DateTagPos(p.toPx(dayStart, 0).X, size, plotPos, plotSize)
		if slices.ContainsFunc(taken, func(t span) bool { return at.X < t.hi && at.X+size.X > t.lo }) {
			continue
		}
		taken = append(taken, span{at.X, at.X + size.X})
		q.tags = append(q.tags, pendingTag{at, rgba(colSelect, 1), text})
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

// drawTag draws a filled tag with its top-left corner at p, with text black or white by the background's brightness, as ImPlot does for its own axis tags.
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
