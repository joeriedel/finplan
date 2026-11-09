// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/AllenDang/cimgui-go/implot"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

const secondsPerDay = 86400

// lineView is one visible line with the samples drawn this frame.
type lineView struct {
	line    graph.Line
	name    string
	proj    string
	samples []engine.Sample
}

// graphTab is the Graph tab: the line list on the left, the toolbar and plot on the right.
type graphTab struct {
	rec       *model.Record
	lines     *graph.Lines
	cache     *dataCache
	dataStart model.Date

	// The visible limits as ImPlot reported them last frame, in unix seconds and dollars. applyX and
	// applyY push the limits to ImPlot next frame (a range button, or fitting to the data); otherwise
	// the user's panning and zooming are left alone.
	xMin, xMax, yMin, yMax float64
	applyX, applyY, fitY   bool
	// The plot area in screen pixels as of last frame, shared with the data lanes.
	plotPos, plotSize imgui.Vec2

	// The crosshair tags queued by crosshairTags, drawn after the plot by drawPendingTags.
	tags []pendingTag

	// The Add popup.
	search     string
	addAsset   model.AssetID
	addProj    string
	addColor   [3]float32
	addDefault [3]float32
	// The recolour popup of a row or band.
	editColor [3]float32

	// The Add band popup; the colour follows the upper line's until the user edits it.
	bandUpper, bandLower   graph.LineID
	bandColor, bandDefault [3]float32

	// The saved view presets, the index of the current one (-1 for none) and the Save as name.
	presets    []graph.Preset
	preset     int
	presetName string
}

// bandView is a drawn band with the indexes in the frame's lineViews of its upper and lower lines.
type bandView struct {
	band         graph.Band
	name         string
	upper, lower int
}

func newGraphTab(rec *model.Record) *graphTab {
	switcher := ""
	if len(rec.Projections) > 0 {
		switcher = rec.Projections[0].Name
	}
	g := &graphTab{rec: rec, lines: graph.NewLines(switcher), cache: newDataCache(rec), dataStart: graph.DataStart(rec), preset: -1}
	for _, id := range []model.AssetID{
		demodata.NetWorth, demodata.AllCash, demodata.Retirement,
		model.DefaultAssetID(demodata.Brokerage), model.DefaultAssetID(demodata.TSP), model.DefaultAssetID(demodata.Mortgage),
	} {
		if rec.Asset(id) != nil {
			g.lines.Add(id, false, "", graph.AssetColor(rec, id))
		}
	}
	g.setRange(graph.RangeButtons[1])
	g.presets = familyPresets(rec)
	return g
}

// applyPreset replaces the lines, bands and x range with the preset's and makes it current. Selection is cleared.
func (g *graphTab) applyPreset(i int) {
	p := g.presets[i]
	g.lines = p.Restore()
	g.xMin, g.xMax = p.From, p.To
	g.applyX, g.fitY = true, true
	g.preset = i
}

// savePreset stores the current view over the current preset.
func (g *graphTab) savePreset() {
	g.presets[g.preset] = graph.Capture(g.presets[g.preset].Name, g.lines, g.xMin, g.xMax)
}

// savePresetAs stores the current view as a new preset and makes it current.
func (g *graphTab) savePresetAs(name string) {
	g.presets = append(g.presets, graph.Capture(name, g.lines, g.xMin, g.xMax))
	g.preset = len(g.presets) - 1
}

// presetNameTaken reports whether a preset already has the name.
func (g *graphTab) presetNameTaken(name string) bool {
	for _, p := range g.presets {
		if p.Name == name {
			return true
		}
	}
	return false
}

// lineName returns the line's asset name and effective projection.
func (g *graphTab) lineName(id graph.LineID) (name, proj string) {
	if ln, ok := g.lines.Get(id); ok {
		proj, _ = g.lines.Effective(id)
		return g.assetName(ln.Asset), proj
	}
	return "", ""
}

// bandName returns the band's name from its lines' assets and effective projections.
func (g *graphTab) bandName(b graph.Band) string {
	un, up := g.lineName(b.Upper)
	ln, lp := g.lineName(b.Lower)
	return graph.BandName(un, up, ln, lp)
}

// bandViews returns the drawn bands whose two lines were sampled in views to the same number of samples.
func (g *graphTab) bandViews(views []lineView) []bandView {
	at := map[graph.LineID]int{}
	for i, v := range views {
		at[v.line.ID] = i
	}
	var out []bandView
	for _, b := range g.lines.Bands() {
		u, uok := at[b.Upper]
		l, lok := at[b.Lower]
		if !g.lines.BandDrawn(b.ID) || !uok || !lok || len(views[u].samples) != len(views[l].samples) {
			continue
		}
		out = append(out, bandView{b, g.bandName(b), u, l})
	}
	return out
}

func (g *graphTab) setRange(b graph.RangeButton) {
	from, to := b.Span(g.rec.Today, g.dataStart)
	g.xMin, g.xMax = float64(from)*secondsPerDay, float64(to)*secondsPerDay
	g.applyX, g.fitY = true, true
}

func dateOf(x float64) model.Date { return model.Date(math.Floor(x / secondsPerDay)) }

func (g *graphTab) assetName(id model.AssetID) string {
	if a := g.rec.Asset(id); a != nil {
		return a.Name
	}
	return string(id)
}

func (g *graphTab) draw() {
	imgui.PushStyleColorVec4(imgui.ColChildBg, rgba(colPanel, 1))
	open := imgui.BeginChildStrV("##lines", imgui.NewVec2(imgui.FontSize()*21, 0), imgui.ChildFlagsBorders, 0)
	imgui.PopStyleColor()
	if open {
		g.sidebar()
	}
	imgui.EndChild()
	imgui.SameLine()
	imgui.BeginGroup()
	g.toolbar()
	g.plotArea()
	imgui.EndGroup()
}

func (g *graphTab) toolbar() {
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Projection")
	imgui.SameLine()
	imgui.SetNextItemWidth(imgui.FontSize() * 11)
	if imgui.BeginComboV("##projection", g.lines.Switcher(), 0) {
		for _, p := range g.rec.Projections {
			if imgui.SelectableBoolV(p.Name, p.Name == g.lines.Switcher(), 0, imgui.NewVec2(0, 0)) {
				g.lines.SetSwitcher(p.Name)
				g.fitY = true
			}
		}
		imgui.EndCombo()
	}
	imgui.SameLineV(0, imgui.FontSize()*2)
	for i, b := range graph.RangeButtons {
		if i > 0 {
			imgui.SameLine()
		}
		if imgui.Button(b.Label) {
			g.setRange(b)
		}
	}
}

// visible returns the drawn lines sampled on the grid, in list order.
func (g *graphTab) visible(grid graph.Grid) []lineView {
	var out []lineView
	for _, ln := range g.lines.All() {
		if !g.lines.Drawn(ln.ID) {
			continue
		}
		proj, _ := g.lines.Effective(ln.ID)
		out = append(out, lineView{line: ln, name: g.assetName(ln.Asset), proj: proj, samples: g.cache.samples(ln.Asset, proj, grid)})
	}
	return out
}

// fitToData sets the y limits to the drawn lines within the visible dates, with a margin.
func (g *graphTab) fitToData(views []lineView) {
	lo, hi := math.Inf(1), math.Inf(-1)
	from, to := dateOf(g.xMin), dateOf(g.xMax)
	for _, v := range views {
		for _, s := range v.samples {
			if s.Date >= from && s.Date <= to {
				lo, hi = min(lo, s.Amount), max(hi, s.Amount)
			}
		}
	}
	if lo > hi {
		return
	}
	if hi-lo < 1 {
		lo, hi = lo-1, hi+1
	}
	margin := (hi - lo) * 0.08
	g.yMin, g.yMax = lo-margin, hi+margin
	g.applyY = true
}

func (g *graphTab) plotArea() {
	width := g.plotSize.X
	if width <= 0 {
		width = 1000
	}
	grid := graph.SampleGrid(dateOf(g.xMin), dateOf(g.xMax), width)
	views := g.visible(grid)
	if g.fitY {
		g.fitToData(views)
		g.fitY = false
	}

	laneH, pitch := lanesHeight(len(views))
	plotH := imgui.ContentRegionAvail().Y - laneH - imgui.CurrentStyle().ItemSpacing().Y
	if plotH < 50 {
		plotH = 50
	}
	if implot.BeginPlotV("##balances", imgui.NewVec2(-1, plotH), implot.FlagsNoTitle|implot.FlagsNoLegend|implot.FlagsNoMouseText) {
		// The time axis always reserves two label rows (PadAndDatumAxesX in implot.cpp), so its height is constant.
		implot.SetupAxisV(implot.AxisX1, "", implot.AxisFlagsNoLabel)
		implot.SetupAxisScalePlotScale(implot.AxisX1, implot.ScaleTime)
		implot.SetupAxisV(implot.AxisY1, "", implot.AxisFlagsNoLabel)
		implot.SetupAxisLimitsConstraints(implot.AxisX1, float64(model.D(1971, 1, 1))*secondsPerDay, float64(model.D(2200, 1, 1))*secondsPerDay)
		implot.SetupAxisZoomConstraints(implot.AxisX1, 7*secondsPerDay, 200*365*secondsPerDay)
		// ImPlot sizes the y gutter to the widest tick label. A last tick far outside the visible range, never drawn, is
		// labelled (space-padded if need be) to be as wide as the widest tick label or value tag, which keeps the gutter
		// the same width however the view moves and wide enough for the tags drawn in it.
		ticks := graph.NiceTicks(g.yMin, g.yMax, max(3, int(g.plotSize.Y/(48*uiScale))))
		labels := make([]string, len(ticks), len(ticks)+1)
		for i, t := range ticks {
			labels[i] = graph.FormatMoneyCompact(t)
		}
		ticks = append(ticks, g.yMin-1000*max(g.yMax-g.yMin, math.Abs(g.yMin), 1))
		labels = append(labels, graph.GutterLabel(func(s string) float32 { return imgui.CalcTextSize(s).X }, implot.GetStyle().AnnotationPadding().X))
		implot.SetupAxisTicksdoublePtrV(implot.AxisY1, &ticks[0], int32(len(ticks)), labels, false)
		condX, condY := implot.CondOnce, implot.CondOnce
		if g.applyX {
			condX = implot.CondAlways
		}
		if g.applyY {
			condY = implot.CondAlways
		}
		implot.SetupAxisLimitsV(implot.AxisX1, g.xMin, g.xMax, condX)
		implot.SetupAxisLimitsV(implot.AxisY1, g.yMin, g.yMax, condY)
		implot.SetupFinish()
		g.applyX, g.applyY = false, false
		g.drawPlot(views)
		implot.EndPlot()
		g.drawPendingTags()
	}
	g.drawLanes(views, laneH, pitch)
}
