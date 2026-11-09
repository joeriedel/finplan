// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"unsafe"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/AllenDang/cimgui-go/implot"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// Pixel sizes at a display scale of 1.
const (
	lineWidth     = 2
	selectedWidth = 3.5
	haloWidth     = 9
	dashLen       = 7
	dashGap       = 5
	hitRadius     = 6
)

func lineColor(c graph.Color, alpha float32) imgui.Vec4 {
	r, g, b := c.Floats()
	return imgui.NewVec4(r, g, b, alpha)
}

// vecs reinterprets points as ImGui vectors; graph.Pt has the same two-float32 layout as imgui.Vec2.
func vecs(pts []graph.Pt) *imgui.Vec2 { return (*imgui.Vec2)(unsafe.Pointer(&pts[0])) }

func vec(p graph.Pt) imgui.Vec2 { return imgui.NewVec2(p.X, p.Y) }

// style classes of a line segment, by the status of the sample it starts at.
const (
	solid = iota
	faded
	dashed
)

func segmentClass(s engine.Status) int {
	switch s {
	case engine.StatusActual:
		return solid
	case engine.StatusProjected:
		return dashed
	}
	return faded // partly projected, or default balances only
}

// drawPolyline strokes a polyline in the style of its status class.
func drawPolyline(dl *imgui.DrawList, pts []graph.Pt, class int, c graph.Color, width float32) {
	if len(pts) < 2 {
		return
	}
	switch class {
	case solid:
		dl.AddPolyline(vecs(pts), int32(len(pts)), u32(lineColor(c, 1)), width)
	case faded:
		dl.AddPolyline(vecs(pts), int32(len(pts)), u32(lineColor(c, 0.45)), width)
	default:
		col := u32(lineColor(c, 0.9))
		for _, s := range graph.Dash(pts, dashLen*uiScale, dashGap*uiScale) {
			dl.AddLineArgs(vec(s.A), vec(s.B), col, width)
		}
	}
}

// drawLine strokes a whole line, with a halo and extra weight when selected.
func drawLine(dl *imgui.DrawList, v lineView, pts []graph.Pt, selected bool) {
	width := lineWidth * uiScale
	if selected {
		width = selectedWidth * uiScale
		dl.AddPolyline(vecs(pts), int32(len(pts)), u32(lineColor(v.line.Color, 0.18)), haloWidth*uiScale)
	}
	for i := 0; i < len(v.samples)-1; {
		class := segmentClass(v.samples[i].Status)
		j := i + 1
		for j < len(v.samples)-1 && segmentClass(v.samples[j].Status) == class {
			j++
		}
		drawPolyline(dl, pts[i:j+1], class, v.line.Color, width)
		i = j
	}
}

func (g *graphTab) toPx(x, y float64) graph.Pt {
	return graph.Pt{
		X: g.plotPos.X + float32((x-g.xMin)/(g.xMax-g.xMin))*g.plotSize.X,
		Y: g.plotPos.Y + float32((g.yMax-y)/(g.yMax-g.yMin))*g.plotSize.Y,
	}
}

// drawPlot draws everything inside the plot: the today marker, the lines, handoff markers and labels,
// the crosshair, and handles selecting a line by clicking it. Limits and plot area are read from ImPlot.
func (g *graphTab) drawPlot(views []lineView) {
	lim := implot.GetPlotLimitsV(implot.AxisX1, implot.AxisY1)
	lo, hi := lim.Min(), lim.Max()
	g.xMin, g.xMax, g.yMin, g.yMax = lo.X, hi.X, lo.Y, hi.Y
	g.plotPos, g.plotSize = implot.GetPlotPos(), implot.GetPlotSize()
	if g.plotSize.X <= 0 || g.plotSize.Y <= 0 || !(g.xMax > g.xMin) || !(g.yMax > g.yMin) {
		return
	}
	s := uiScale
	left, right := g.plotPos.X, g.plotPos.X+g.plotSize.X
	top, bottom := g.plotPos.Y, g.plotPos.Y+g.plotSize.Y
	selID, hasSel := g.lines.Selected()

	dl := implot.GetPlotDrawList()
	implot.PushPlotClipRect()

	// Today: tint everything after it and mark it.
	todayX := g.toPx(float64(g.rec.Today)*secondsPerDay, 0).X
	if todayX < right {
		dl.AddRectFilled(imgui.NewVec2(max(todayX, left), top), imgui.NewVec2(right, bottom), u32(rgba(colAccent, 0.05)))
	}
	if todayX >= left && todayX <= right {
		dl.AddLineArgs(imgui.NewVec2(todayX, top), imgui.NewVec2(todayX, bottom), u32(rgba(colAccent, 0.9)), 1.5*s)
		dl.AddTextVec2(imgui.NewVec2(todayX+6*s, top+4*s), u32(rgba(colAccent, 1)), "Today · projected →")
	}

	// Lines, the selected one last so that it lies on top.
	polys := make([][]graph.Pt, len(views))
	for i, v := range views {
		polys[i] = make([]graph.Pt, len(v.samples))
		for k, sm := range v.samples {
			polys[i][k] = g.toPx(float64(sm.Date)*secondsPerDay, sm.Amount)
		}
	}
	for pass := 0; pass < 2; pass++ {
		for i, v := range views {
			if (hasSel && v.line.ID == selID) == (pass == 1) && len(polys[i]) > 1 {
				drawLine(dl, v, polys[i], pass == 1)
			}
		}
	}

	// Handoff markers and stale labels.
	for _, v := range views {
		bg := u32(rgba(colInput, 1))
		col := u32(lineColor(v.line.Color, 1))
		if h, ok := graph.Handoff(v.samples); ok {
			p := vec(g.toPx(float64(h.Date)*secondsPerDay, h.Amount))
			dl.AddCircleFilledV(p, 4.5*s, bg, 0)
			dl.AddCircleV(p, 4.5*s, col, 0, 2*s)
		}
		if last, ok := g.cache.lastActual(v.line.Asset, v.proj); ok {
			if label, stale := graph.StaleLabel(v.name, last, g.rec.Today); stale {
				if amount, _, ok := graph.At(v.samples, last); ok {
					p := g.toPx(float64(last)*secondsPerDay, amount)
					dl.AddTextVec2(imgui.NewVec2(p.X+8*s, p.Y-18*s), u32(lineColor(v.line.Color, 0.9)), label)
				}
			}
		}
	}

	hovered := implot.IsPlotHovered()
	mouse := imgui.MousePos()
	cursor := implot.GetPlotMousePos()
	day := dateOf(cursor.X)
	if hovered {
		dashRGBA := u32(rgba(colMuted, 0.6))
		for _, sg := range graph.Dash([]graph.Pt{{X: mouse.X, Y: top}, {X: mouse.X, Y: bottom}}, 4*s, 4*s) {
			dl.AddLineArgs(vec(sg.A), vec(sg.B), dashRGBA, s)
		}
		for _, v := range views {
			amount, _, ok := graph.At(v.samples, day)
			if !ok {
				continue
			}
			p := g.toPx(cursor.X, amount)
			p.X = mouse.X
			col := lineColor(v.line.Color, 0.75)
			for _, sg := range graph.Dash([]graph.Pt{p, {X: left, Y: p.Y}}, 4*s, 4*s) {
				dl.AddLineArgs(vec(sg.A), vec(sg.B), u32(col), s)
			}
			dl.AddCircleFilled(vec(p), 3.5*s, u32(lineColor(v.line.Color, 1)))
		}
	}
	implot.PopPlotClipRect()

	// Click selects the line nearest the pointer, within a few pixels; a click elsewhere clears it.
	// A drag is panning, not a click.
	if hovered && imgui.IsMouseReleased(imgui.MouseButtonLeft) {
		if d := imgui.MouseDragDelta(); d.X == 0 && d.Y == 0 {
			best, bestDist := graph.LineID(0), float32(hitRadius*s)
			for i, v := range views {
				if dist := graph.Distance(graph.Pt{X: mouse.X, Y: mouse.Y}, polys[i]); dist <= bestDist {
					best, bestDist = v.line.ID, dist
				}
			}
			g.lines.Select(best)
		}
	}

	if hovered {
		g.crosshairTags(views, day)
	}
}

// pendingTag is a crosshair tag with its top-left corner, queued while the plot is being built, to be drawn once
// ImPlot has finished.
type pendingTag struct {
	at   graph.Pt
	bg   imgui.Vec4
	text string
}

// drawPendingTags draws the queued tags on the window, after implot.EndPlot, so that ImPlot's tick marks and
// border do not paint over them.
func (g *graphTab) drawPendingTags() {
	dl := imgui.WindowDrawList()
	for _, t := range g.tags {
		g.drawTag(dl, t.at, t.bg, t.text)
	}
	g.tags = g.tags[:0]
}

// tagSize returns the size of a tag around the text, with ImPlot's annotation padding.
func tagSize(text string) graph.Pt {
	pad := implot.GetStyle().AnnotationPadding()
	ts := imgui.CalcTextSize(text)
	return graph.Pt{X: ts.X + 2*pad.X, Y: ts.Y + 2*pad.Y}
}

// drawTag draws a filled tag with its top-left corner at p, with text black or white by the background's
// brightness, as ImPlot does for its own axis tags.
func (g *graphTab) drawTag(dl *imgui.DrawList, p graph.Pt, bg imgui.Vec4, text string) {
	pad := implot.GetStyle().AnnotationPadding()
	size := tagSize(text)
	fg := uint32(0xffffffff)
	if graph.TagTextBlack(bg.X, bg.Y, bg.Z) {
		fg = 0xff000000
	}
	dl.AddRectFilled(vec(p), vec(graph.Pt{X: p.X + size.X, Y: p.Y + size.Y}), u32(bg))
	dl.AddTextVec2(imgui.NewVec2(p.X+pad.X, p.Y+pad.Y), fg, text)
}

// crosshairTags queues the value tags (in the y-axis gutter, against the plot's left edge) and the date tag (in
// the x-axis area, below the plot), and shows the tooltip, for the date under the pointer. The tags are our own
// rather than ImPlot's, which would resize the axis gutters as the pointer moves.
func (g *graphTab) crosshairTags(views []lineView, day model.Date) {
	type row struct {
		v      lineView
		amount float64
		status engine.Status
	}
	var rows []row
	plotPos := graph.Pt{X: g.plotPos.X, Y: g.plotPos.Y}
	plotSize := graph.Pt{X: g.plotSize.X, Y: g.plotSize.Y}
	for _, v := range views {
		if amount, st, ok := graph.At(v.samples, day); ok {
			rows = append(rows, row{v, amount, st})
			if !graph.InRange(amount, g.yMin, g.yMax) {
				continue
			}
			y := g.toPx(0, amount).Y
			text := graph.FormatTagMoney(amount)
			g.tags = append(g.tags, pendingTag{graph.ValueTagPos(y, tagSize(text), plotPos, plotSize), lineColor(v.line.Color, 1), text})
		}
	}
	if len(rows) == 0 {
		return
	}
	dateText := graph.FormatDate(day)
	dx := g.toPx(float64(day)*secondsPerDay, 0).X
	if dayStart := float64(day) * secondsPerDay; dayStart+secondsPerDay > g.xMin && dayStart <= g.xMax {
		g.tags = append(g.tags, pendingTag{graph.DateTagPos(dx, tagSize(dateText), plotPos, plotSize), rgba(colSelect, 1), dateText})
	}
	if imgui.BeginTooltip() {
		imgui.PushStyleColorVec4(imgui.ColText, rgba(colMuted, 1))
		imgui.TextUnformatted(graph.FormatDate(day))
		imgui.PopStyleColor()
		if imgui.BeginTableV("##tip", 3, imgui.TableFlagsNone, imgui.NewVec2(0, 0), 0) {
			for _, r := range rows {
				imgui.TableNextRow()
				imgui.TableNextColumn()
				imgui.PushStyleColorVec4(imgui.ColText, lineColor(r.v.line.Color, 1))
				imgui.TextUnformatted(r.v.name + " (" + r.v.proj + ")")
				imgui.PopStyleColor()
				imgui.TableNextColumn()
				imgui.TextUnformatted(graph.FormatMoney(r.amount))
				imgui.TableNextColumn()
				imgui.PushStyleColorVec4(imgui.ColText, rgba(colMuted, 1))
				imgui.TextUnformatted(r.status.String())
				imgui.PopStyleColor()
			}
			imgui.EndTable()
		}
		imgui.EndTooltip()
	}
}

// lanesHeight returns the height of the data lanes strip for n lines and the pitch between lanes.
func lanesHeight(n int) (height, pitch float32) {
	if n == 0 {
		return 0, 0
	}
	pitch = min(16*uiScale, 120*uiScale/float32(n))
	return float32(n)*pitch + 8*uiScale, pitch
}

// drawLanes draws one bar per drawn line under the plot on the plot's own time axis: striped where the
// data is projected, medium where partly projected and strong where actual. A lane lights on hover, shows the
// row's selection fill when its line is selected, and selects its line when clicked.
func (g *graphTab) drawLanes(views []lineView, height, pitch float32) {
	if height == 0 {
		return
	}
	origin := imgui.CursorScreenPos()
	imgui.Dummy(imgui.NewVec2(imgui.ContentRegionAvail().X, height))
	if g.plotSize.X <= 0 || !(g.xMax > g.xMin) {
		return
	}
	s := uiScale
	dl := imgui.WindowDrawList()
	left, right := g.plotPos.X, g.plotPos.X+g.plotSize.X
	barH := pitch - 4*s
	top := origin.Y + 4*s
	xOf := func(d model.Date) float32 {
		return g.plotPos.X + float32((float64(d)*secondsPerDay-g.xMin)/(g.xMax-g.xMin))*g.plotSize.X
	}
	hover := -1
	if m := imgui.MousePos(); imgui.IsWindowHovered() && m.X >= origin.X && m.X <= right {
		hover = graph.LaneAt(m.Y, top, pitch, barH, len(views))
	}
	if hover >= 0 && imgui.IsMouseClickedBool(imgui.MouseButtonLeft) {
		g.lines.Select(views[hover].line.ID)
	}
	selID, hasSel := g.lines.Selected()
	for i, v := range views {
		y := top + float32(i)*pitch
		selected := hasSel && v.line.ID == selID
		if selected {
			dl.AddRectFilledV(imgui.NewVec2(origin.X, y-2*s), imgui.NewVec2(right, y+barH+2*s), u32(rgba(colSelect, 0.45)), 4*s, 0)
		} else if i == hover {
			dl.AddRectFilledV(imgui.NewVec2(origin.X, y-2*s), imgui.NewVec2(right, y+barH+2*s), u32(rgba(colSelect, 0.18)), 4*s, 0)
		}
		dl.PushClipRectV(imgui.NewVec2(left, y), imgui.NewVec2(right, y+barH), true)
		dl.AddRectFilled(imgui.NewVec2(left, y), imgui.NewVec2(right, y+barH), u32(rgba(colInput, 1)))
		for _, sp := range graph.StatusSpans(v.samples) {
			x0 := max(xOf(sp.From), left)
			x1 := min(max(xOf(sp.To), xOf(sp.From)+1), right)
			if x1 <= x0 {
				continue
			}
			switch sp.Status {
			case engine.StatusActual:
				dl.AddRectFilled(imgui.NewVec2(x0, y), imgui.NewVec2(x1, y+barH), u32(lineColor(v.line.Color, 0.95)))
			case engine.StatusPartlyProjected:
				dl.AddRectFilled(imgui.NewVec2(x0, y), imgui.NewVec2(x1, y+barH), u32(lineColor(v.line.Color, 0.55)))
			case engine.StatusProjected:
				dl.PushClipRectV(imgui.NewVec2(x0, y), imgui.NewVec2(x1, y+barH), true)
				col := u32(lineColor(v.line.Color, 0.7))
				for x := x0 - barH; x < x1; x += 5 * s {
					dl.AddLineArgs(imgui.NewVec2(x, y+barH), imgui.NewVec2(x+barH, y), col, s)
				}
				dl.PopClipRect()
			}
		}
		dl.PopClipRect()
		label := lineColor(v.line.Color, 0.6)
		if selected {
			label = lineColor(v.line.Color, 1)
		}
		dl.PushClipRectV(imgui.NewVec2(origin.X, y), imgui.NewVec2(left-4*s, y+barH), true)
		dl.AddTextVec2(imgui.NewVec2(origin.X+4*s, y+(barH-imgui.TextLineHeight())/2), u32(label), v.name)
		dl.PopClipRect()
	}
	if t := xOf(g.rec.Today); t >= left && t <= right {
		dl.AddLineArgs(imgui.NewVec2(t, origin.Y), imgui.NewVec2(t, origin.Y+height), u32(rgba(colAccent, 0.7)), s)
	}
}
