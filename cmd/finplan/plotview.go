// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
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
	hatchSpacing  = 7
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

// drawPolyline strokes a polyline in the style of its status class. Dashes are only made within the plot
// rectangle from lo to hi, so a line running far off it costs no more than one that does not.
func drawPolyline(dl *imgui.DrawList, pts []graph.Pt, class int, c graph.Color, width float32, lo, hi graph.Pt) {
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
		lo, hi = graph.Pt{X: lo.X - width, Y: lo.Y - width}, graph.Pt{X: hi.X + width, Y: hi.Y + width}
		for _, s := range graph.DashClipped(pts, dashLen*uiScale, dashGap*uiScale, lo, hi) {
			dl.AddLineArgs(vec(s.A), vec(s.B), col, width)
		}
	}
}

// drawLine strokes a whole line, with a halo and extra weight when selected. The plot rectangle runs from lo to hi.
func drawLine(dl *imgui.DrawList, v lineView, pts []graph.Pt, selected bool, lo, hi graph.Pt) {
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
		drawPolyline(dl, pts[i:j+1], class, v.line.Color, width, lo, hi)
		i = j
	}
}

// geom is the plot's geometry as drawPlot last read it.
func (g *graphTab) geom() plotGeom {
	return plotGeom{g.plotPos, g.plotSize, g.xMin, g.xMax, g.yMin, g.yMax}
}

func (g *graphTab) toPx(x, y float64) graph.Pt { return g.geom().toPx(x, y) }

// drawPlot draws everything inside the plot: the today marker, the bands, the lines, handoff markers and labels,
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

	hovered := implot.IsPlotHovered()
	mouse := imgui.MousePos()
	day := dateOf(implot.GetPlotMousePos().X)
	if hovered {
		g.cursor = day
	}
	g.intervalBand(dl, hovered, day)

	// Bands lie beneath the lines. The selected line is drawn last so that it lies on top.
	polys := make([][]graph.Pt, len(views))
	for i, v := range views {
		polys[i] = make([]graph.Pt, len(v.samples))
		for k, sm := range v.samples {
			polys[i][k] = g.toPx(float64(sm.Date)*secondsPerDay, sm.Amount)
		}
	}
	bands := g.bandViews(views)
	for _, bv := range bands {
		g.drawBand(dl, bv, polys[bv.upper], polys[bv.lower])
	}
	g.drawMonteCarlo(dl, views)
	for pass := 0; pass < 2; pass++ {
		for i, v := range views {
			if (hasSel && v.line.ID == selID) == (pass == 1) && len(polys[i]) > 1 {
				drawLine(dl, v, polys[i], pass == 1, graph.Pt{X: left, Y: top}, graph.Pt{X: right, Y: bottom})
			}
		}
	}

	g.drawMonteCarloLabels(dl, views)

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

	var rows []crosshairRow
	var pts []crosshairPoint
	if hovered {
		rows, pts = crosshairAt(views, day)
		drawCrosshair(dl, g.geom(), mouse.X, pts)
	}
	implot.PopPlotClipRect()

	// Click selects the line nearest the pointer, within a few pixels; a click elsewhere clears it.
	// A drag is panning, not a click, and a Shift gesture paints the interval.
	if hovered && imgui.IsMouseReleased(imgui.MouseButtonLeft) && !g.bandDrag {
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
		g.crosshairTags(views, rows, pts, bands, day)
	}
	if !imgui.IsMouseDown(imgui.MouseButtonLeft) {
		g.bandDrag = false
	}
}

// intervalBand handles the breakdown interval on the plot and draws it as a tinted band with a draggable handle on each edge. Shift and a left drag paint a new interval from the day where the drag starts to the day under the pointer, in either direction. day is the day under the pointer, valid when hovered.
func (g *graphTab) intervalBand(dl *imgui.DrawList, hovered bool, day model.Date) {
	handleHovered := false
	if g.iv.set {
		left, right := g.plotPos.X, g.plotPos.X+g.plotSize.X
		x0, x1 := g.toPx(float64(g.iv.from)*secondsPerDay, 0).X, g.toPx(float64(g.iv.to)*secondsPerDay, 0).X
		if x1 > left && x0 < right {
			dl.AddRectFilled(imgui.NewVec2(max(x0, left), g.plotPos.Y), imgui.NewVec2(min(x1, right), g.plotPos.Y+g.plotSize.Y), u32(rgba(colAccent, 0.16)))
		}
		for i, d := range []model.Date{g.iv.from, g.iv.to} {
			x := float64(d) * secondsPerDay
			var clicked, hov, held bool
			if implot.DragLineXV(int32(i), &x, rgba(colAccentLit, 0.9), 1.5*uiScale, implot.DragToolFlagsNoFit, &clicked, &hov, &held) {
				g.iv.setEdge(g.cursor, i == 1, model.Date(math.Round(x/secondsPerDay)))
			}
			handleHovered = handleHovered || hov || held
		}
	}
	shift := imgui.CurrentIO().KeyMods() == imgui.KeyChord(imgui.ModShift)
	if hovered && shift && !handleHovered && imgui.IsMouseClickedBool(imgui.MouseButtonLeft) {
		g.bandDrag, g.bandAnchor = true, day
	}
	if g.bandDrag && imgui.IsMouseDown(imgui.MouseButtonLeft) {
		g.iv.paint(g.bandAnchor, dateOf(implot.GetPlotMousePos().X))
	}
}

// crosshairRow is a line's value at the date under the pointer, for the tooltip.
type crosshairRow struct {
	v      lineView
	amount float64
	status engine.Status
}

// crosshairAt returns the value of each line that has one at the day, in the order of views, as tooltip rows and as the points of the crosshair.
func crosshairAt(views []lineView, day model.Date) (rows []crosshairRow, pts []crosshairPoint) {
	for _, v := range views {
		if amount, st, ok := graph.At(v.samples, day); ok {
			rows = append(rows, crosshairRow{v, amount, st})
			pts = append(pts, crosshairPoint{amount, v.line.Color})
		}
	}
	return rows, pts
}

// crosshairTags queues the value and date tags for the day under the pointer and shows the tooltip.
func (g *graphTab) crosshairTags(views []lineView, rows []crosshairRow, pts []crosshairPoint, bands []bandView, day model.Date) {
	if len(rows) == 0 {
		return
	}
	g.tags.crosshair(g.geom(), day, pts)
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
			g.bandTipRows(views, bands, day)
			imgui.EndTable()
		}
		imgui.EndTooltip()
	}
}

// drawBand fills the area between the band's edges, and stripes it if hatched. The edges are the pixel polylines of
// its upper and lower lines.
func (g *graphTab) drawBand(dl *imgui.DrawList, bv bandView, upper, lower []graph.Pt) {
	g.fillBetween(dl, upper, lower, u32(lineColor(bv.band.Color, 0.25)))
	left, right := g.plotPos.X, g.plotPos.X+g.plotSize.X
	if bv.band.Hatch {
		col := u32(lineColor(bv.band.Color, 0.6))
		for _, sg := range graph.HatchStripes(upper, lower, hatchSpacing*uiScale,
			graph.Pt{X: left, Y: g.plotPos.Y}, graph.Pt{X: right, Y: g.plotPos.Y + g.plotSize.Y}) {
			dl.AddLineArgs(vec(sg.A), vec(sg.B), col, uiScale)
		}
	}
}

// fillBetween fills the area between two pixel polylines sampled at the same x positions, skipping the pieces outside the plot.
func (g *graphTab) fillBetween(dl *imgui.DrawList, upper, lower []graph.Pt, fill uint32) {
	left, right := g.plotPos.X, g.plotPos.X+g.plotSize.X
	for _, poly := range graph.BandPolys(upper, lower) {
		minX, maxX := poly[0].X, poly[0].X
		for _, p := range poly[1:] {
			minX, maxX = min(minX, p.X), max(maxX, p.X)
		}
		if minX > right || maxX < left {
			continue
		}
		dl.AddConvexPolyFilled(vecs(poly), int32(len(poly)), fill)
	}
}

// bandTipRows adds a tooltip row per band with the gap between its lines at the day, and the gap in today's dollars
// if the band asks for it. A band whose lines have no value at the day is left out.
func (g *graphTab) bandTipRows(views []lineView, bands []bandView, day model.Date) {
	for _, bv := range bands {
		up, _, uok := graph.At(views[bv.upper].samples, day)
		lo, _, lok := graph.At(views[bv.lower].samples, day)
		if !uok || !lok {
			continue
		}
		gap := graph.Gap(up, lo)
		imgui.TableNextRow()
		imgui.TableNextColumn()
		imgui.PushStyleColorVec4(imgui.ColText, lineColor(bv.band.Color, 1))
		imgui.TextUnformatted(bv.name)
		imgui.PopStyleColor()
		imgui.TableNextColumn()
		imgui.TextUnformatted(graph.FormatMoney(gap))
		imgui.TableNextColumn()
		imgui.PushStyleColorVec4(imgui.ColText, rgba(colMuted, 1))
		imgui.TextUnformatted("gap")
		imgui.PopStyleColor()
		if bv.band.TodayDollars {
			imgui.TableNextRow()
			imgui.TableNextColumn()
			imgui.PushStyleColorVec4(imgui.ColText, rgba(colMuted, 1))
			imgui.TextUnformatted("  gap in today's $")
			imgui.TableNextColumn()
			imgui.TextUnformatted(graph.FormatMoney(graph.ToTodayDollars(gap, day, g.rec.Today, g.rec.Globals[model.PropInflation])))
			imgui.PopStyleColor()
		}
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
