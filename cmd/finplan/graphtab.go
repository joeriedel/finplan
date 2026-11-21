// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

const secondsPerDay = 86400

// lineView is one visible line with the samples drawn this frame.
type lineView struct {
	line    graph.Line
	name    string
	proj    string
	samples []engine.Sample
	// mc is the line's Monte Carlo state when the line asks for it, and mcSeries its bands once ready.
	mc       mcState
	mcSeries *mcSeries
}

// graphTab is the Graph tab: the line list on the left, the toolbar and plot on the right.
type graphTab struct {
	rec       *model.Record
	lines     *graph.Lines
	cache     *dataCache
	mc        *mcCache
	dataStart model.Date
	// demo is whether the database is the throwaway demo, which decides the lines a new Graph draws.
	demo bool

	// The visible limits, in unix seconds and dollars, which the app owns and sets on the plot every frame: the x range is changed by the wheel and right drag (pin), the range buttons and presets, and y is fitted to the lines in the x range before the plot is built. fitY asks for y to be fitted again, as it is whenever the x range or the sample grid changes.
	xMin, xMax, yMin, yMax float64
	fitY                   bool
	pin                    timeInput
	ticks                  moneyTicks

	// fitX0, fitX1 and fitGrid are the x range and the sample grid the y range was last fitted for.
	fitX0, fitX1 float64
	fitGrid      graph.Grid
	// grid is the sample grid of the last frame, and moving the number of frames left of the time that the view counts as moving after a wheel or drag, in which a grid that is not cached is not made but one already sampled, or a coarse one that leaves room to move, is used.
	grid   graph.Grid
	moving int

	// The pixel geometry of the plot's lines and bands, and the slice of them made each frame.
	px    pixelCache
	polys [][]graph.Pt

	// The plot area in screen pixels as of last frame, shared with the data lanes.
	plotPos, plotSize imgui.Vec2

	// The cursor tags queued while the plot is built, drawn after it.
	tags tagQueue

	// The breakdown panel: its cache, the interval the user set (unset, it covers the 12 months ending at cursor), the cursor, the header fields, and the Shift-drag gesture that paints the interval, anchored at bandAnchor.
	bkCache    *breakdownCache
	iv         bkInterval
	cur        cursorState
	bk         bkUI
	bandDrag   bool
	bandAnchor model.Date

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
	// Presets are written through store; notify tells the user about a failure.
	store  *store.Store
	notify func(string)
}

// bandView is a drawn band with the indexes in the frame's lineViews of its upper and lower lines.
type bandView struct {
	band         graph.Band
	name         string
	upper, lower int
}

// demoLines are the lines of the demo's Graph.
var demoLines = []model.AssetID{
	demodata.NetWorth, demodata.AllCash, demodata.Retirement,
	model.DefaultAssetID(demodata.Brokerage), model.DefaultAssetID(demodata.TSP), model.DefaultAssetID(demodata.Mortgage),
	model.DefaultAssetID(demodata.College1), model.DefaultAssetID(demodata.College2), model.DefaultAssetID(demodata.College3), model.DefaultAssetID(demodata.College4),
}

// defaultLines returns the assets a new Graph draws. The demo draws its own ten: the aggregates, three accounts and the four 529 accounts, whose college years overlap. Any other record draws every aggregate asset (a user asset that has an asset or a rule among its members) in record order, or, with none, every account's default asset in record order.
func defaultLines(rec *model.Record, demo bool) []model.AssetID {
	var out []model.AssetID
	if demo {
		for _, id := range demoLines {
			if rec.Asset(id) != nil {
				out = append(out, id)
			}
		}
		return out
	}
	isDefault := map[model.AssetID]bool{}
	for _, a := range rec.Accounts {
		isDefault[model.DefaultAssetID(a.ID)] = true
	}
	for _, a := range rec.Assets {
		if !isDefault[a.ID] && slices.ContainsFunc(a.Members, func(m model.Member) bool { return m.Asset != "" || m.Rule != nil }) {
			out = append(out, a.ID)
		}
	}
	if len(out) == 0 {
		for _, a := range rec.Accounts {
			out = append(out, model.DefaultAssetID(a.ID))
		}
	}
	return out
}

func newGraphTab(rec *model.Record, presets []graph.Preset, st *store.Store, notify func(string), demo bool) *graphTab {
	switcher := ""
	if len(rec.Projections) > 0 {
		switcher = rec.Projections[0].Name
	}
	g := &graphTab{rec: rec, lines: graph.NewLines(switcher), cache: newDataCache(rec), mc: newMCCache(rec), dataStart: graph.DataStart(rec), preset: -1,
		bkCache: newBreakdownCache(rec), cur: cursorState{lock: rec.Today}, presets: presets, store: st, notify: notify, demo: demo}
	for _, id := range defaultLines(rec, demo) {
		g.lines.Add(id, false, "", graph.AssetColor(rec, id))
	}
	g.setRange(graph.RangeButtons[1])
	return g
}

// reload switches to a freshly loaded record and preset list, keeping the view: the lines whose assets and
// projections still exist, the x range and the current preset if its name still exists.
func (g *graphTab) reload(rec *model.Record, presets []graph.Preset) {
	if len(g.rec.Accounts) == 0 {
		*g = *newGraphTab(rec, presets, g.store, g.notify, g.demo)
		return
	}
	name := ""
	if g.preset >= 0 {
		name = g.presets[g.preset].Name
	}
	old := g.lines.All()
	oldSel, selected := g.lines.Selected()
	keep := graph.Capture("", g.lines, g.xMin, g.xMax)
	has := recordHas(rec)
	g.rec, g.cache, g.mc, g.dataStart, g.bkCache = rec, newDataCache(rec), newMCCache(rec), graph.DataStart(rec), newBreakdownCache(rec)
	g.lines = keep.Restore(has, projectionNames(rec))
	// Restore keeps the order of the lines it keeps, so the kept old lines pair up with the new ones.
	newIDs := map[graph.LineID]graph.LineID{}
	kept := g.lines.All()
	for _, ln := range old {
		if has(ln.Asset, ln.Projection) {
			newIDs[ln.ID] = kept[len(newIDs)].ID
		}
	}
	if selected {
		g.lines.Select(newIDs[oldSel])
	}
	g.bandUpper, g.bandLower = newIDs[g.bandUpper], newIDs[g.bandLower]
	g.presets, g.preset = presets, -1
	for i, p := range presets {
		if p.Name == name {
			g.preset = i
		}
	}
}

// applyPreset replaces the lines, bands and x range with the preset's and makes it current. Selection is cleared.
func (g *graphTab) applyPreset(i int) {
	p := g.presets[i]
	g.lines = p.Restore(recordHas(g.rec), projectionNames(g.rec))
	g.xMin, g.xMax = p.From, p.To
	g.fitY = true
	g.preset = i
}

// persist writes the preset to the database, telling the user and returning false if that fails.
func (g *graphTab) persist(p graph.Preset) bool {
	body, err := graph.EncodePreset(p)
	if err == nil {
		err = g.store.PutPreset(p.Name, graph.PresetFormat, body)
	}
	if err != nil {
		g.notify(fmt.Sprintf("Could not save view preset %q: %v", p.Name, err))
		return false
	}
	return true
}

// presetModified reports whether the view cur differs from the stored preset: any line, band or switcher, or an x range off by more than a day, which a read-back from ImPlot after applying the preset never is.
func presetModified(stored, cur graph.Preset) bool {
	const tolerance = secondsPerDay
	return stored.Switcher != cur.Switcher || !slices.Equal(stored.Lines, cur.Lines) || !slices.Equal(stored.Bands, cur.Bands) ||
		math.Abs(stored.From-cur.From) > tolerance || math.Abs(stored.To-cur.To) > tolerance
}

// presetDirty reports whether the current preset is set and the view differs from it.
func (g *graphTab) presetDirty() bool {
	return g.preset >= 0 && presetModified(g.presets[g.preset], graph.Capture("", g.lines, g.xMin, g.xMax))
}

// savePreset stores the current view over the current preset.
func (g *graphTab) savePreset() {
	p := graph.Capture(g.presets[g.preset].Name, g.lines, g.xMin, g.xMax)
	if g.persist(p) {
		g.presets[g.preset] = p
	}
}

// savePresetAs stores the current view as a new preset and makes it current.
func (g *graphTab) savePresetAs(name string) {
	p := graph.Capture(name, g.lines, g.xMin, g.xMax)
	if g.persist(p) {
		g.presets = append(g.presets, p)
		g.preset = len(g.presets) - 1
	}
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
	g.setSpan(from, to)
}

// setSpan shows the dates from..to.
func (g *graphTab) setSpan(from, to model.Date) {
	g.xMin, g.xMax = float64(from)*secondsPerDay, float64(to)*secondsPerDay
	g.fitY = true
}

func dateOf(x float64) model.Date { return model.Date(math.Floor(x / secondsPerDay)) }

func (g *graphTab) assetName(id model.AssetID) string {
	if a := g.rec.Asset(id); a != nil {
		return a.Name
	}
	for _, p := range g.rec.Projections {
		for _, a := range p.Accounts {
			if model.DefaultAssetID(a.ID) == id {
				return a.Name
			}
		}
	}
	return string(id)
}

func (g *graphTab) draw() {
	if len(g.rec.Accounts) == 0 {
		wrappedText("There are no accounts yet. Adding them comes with the editor tabs and SimpleFIN. Run finplan with --demo to explore an invented family.")
		return
	}
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
	g.escapeInterval()
}

// escapeInterval clears the breakdown interval on Esc, unless a text field has the keyboard or a popup is open, where Esc belongs to them.
func (g *graphTab) escapeInterval() {
	if g.iv.set && imgui.IsKeyPressedBoolV(imgui.KeyEscape, false) && !imgui.CurrentIO().WantTextInput() &&
		!imgui.IsPopupOpenStrV("", imgui.PopupFlagsAnyPopupId|imgui.PopupFlagsAnyPopupLevel) {
		g.iv.clear()
	}
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
	if from, to, name, ok := graph.LifespanSpan(g.rec.Household, g.rec.Today); ok {
		imgui.SameLine()
		if imgui.Button("Age " + strconv.Itoa(graph.LifespanAge)) {
			g.setSpan(from, to)
		}
		tooltipOn("Show the range up to " + name + " turning " + strconv.Itoa(graph.LifespanAge) + ", on " + to.String())
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
		v := lineView{line: ln, name: g.assetName(ln.Asset), proj: proj, samples: g.cache.samples(ln.Asset, proj, grid)}
		if ln.MonteCarlo {
			v.mc, v.mcSeries = g.mc.get(ln.Asset, proj)
		}
		out = append(out, v)
	}
	return out
}

// fitToData sets the y limits to the drawn lines within the visible dates, with a margin.
func (g *graphTab) fitToData(views []lineView) {
	series := make([][]engine.Sample, len(views))
	for i, v := range views {
		series[i] = v.samples
		if v.mcSeries != nil {
			series = append(series, v.mcSeries.samples(v.mcSeries.p10), v.mcSeries.samples(v.mcSeries.p90))
		}
	}
	if lo, hi, ok := fitYRange(series, dateOf(g.xMin), dateOf(g.xMax)); ok {
		g.yMin, g.yMax = lo, hi
	}
}

// fitYRange returns the y range that shows the samples dated from..to with a margin: the span of their amounts, widened by 1 on each side if under 1, plus 8% on each side. ok is false when no sample is in range.
func fitYRange(series [][]engine.Sample, from, to model.Date) (lo, hi float64, ok bool) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, samples := range series {
		for _, s := range samples {
			if s.Date >= from && s.Date <= to {
				lo, hi = min(lo, s.Amount), max(hi, s.Amount)
			}
		}
	}
	if lo > hi {
		return 0, 0, false
	}
	if hi-lo < 1 {
		lo, hi = lo-1, hi+1
	}
	margin := (hi - lo) * 0.08
	return lo - margin, hi + margin, true
}

// viewSettleFrames is how many frames, counting the one with the wheel or drag, the view counts as moving. The app draws settleFrames frames after input before it waits, so the last of them has the exact grid.
const viewSettleFrames = settleFrames - 1

// cachedAll reports whether every drawn line is sampled on the grid already, so that sampling on it runs no engine.
func (g *graphTab) cachedAll(grid graph.Grid) bool {
	for _, ln := range g.lines.All() {
		if proj, _ := g.lines.Effective(ln.ID); g.lines.Drawn(ln.ID) && !g.cache.has(ln.Asset, proj, grid) {
			return false
		}
	}
	return true
}

// sampleGrid returns the grid to sample the lines on for the visible dates, plotWidth pixels wide. While the view moves, the grid is not changed to the exact one for each step, which would run the engine for every line at each one (a stall of many frames), but kept while it covers the view, or made coarse with room to move; the exact grid comes once the view settles.
func (g *graphTab) sampleGrid(plotWidth float32) graph.Grid {
	from, to := dateOf(g.xMin), dateOf(g.xMax)
	want := graph.SampleGrid(from, to, plotWidth)
	if g.moving == 0 || g.cachedAll(want) {
		return want
	}
	if g.grid.Step > 0 && g.grid.First <= from && g.grid.Last >= to && g.cachedAll(g.grid) {
		return g.grid
	}
	return graph.SampleGrid(from-(to-from), to+(to-from), plotWidth)
}

// prepareView sets the x range to lo..hi, which the frame's wheel and drag made (moved) or left as it was, and returns the lines sampled for it, with y fitted to them: all before the plot is built, so that what is drawn is the view of this frame.
func (g *graphTab) prepareView(lo, hi float64, moved bool, plotWidth float32) []lineView {
	g.xMin, g.xMax = fitTimeSpan(lo, hi)
	if moved {
		g.moving = viewSettleFrames
	} else if g.moving > 0 {
		g.moving--
	}
	if plotWidth <= 0 {
		plotWidth = 1000
	}
	grid := g.sampleGrid(plotWidth)
	g.grid = grid
	fresh := g.mc.takeFresh()
	views := g.visible(grid)
	if fresh {
		g.fitY = true
	}
	// y is fitted to the lines in the x range, so panning or zooming refits it in the frame it happens.
	if g.xMin != g.fitX0 || g.xMax != g.fitX1 || grid != g.fitGrid {
		g.fitX0, g.fitX1, g.fitGrid = g.xMin, g.xMax, grid
		g.fitY = true
	}
	if g.fitY {
		g.fitToData(views)
		g.fitY = false
	}
	if !(g.yMax > g.yMin) {
		g.yMin, g.yMax = 0, 1
	}
	return views
}

func (g *graphTab) plotArea() {
	lo, hi, moved := g.pin.apply(g.xMin, g.xMax, g.plotPos, g.plotSize)
	views := g.prepareView(lo, hi, moved, g.plotSize.X)

	laneH, pitch := lanesHeight(len(views))
	panelH := breakdownPanelHeight(imgui.ContentRegionAvail().Y)
	plotH := imgui.ContentRegionAvail().Y - laneH - panelH - 2*imgui.CurrentStyle().ItemSpacing().Y
	if plotH < 50 {
		plotH = 50
	}
	// The time axis always reserves two label rows (PadAndDatumAxesX in implot.cpp), so its height is constant.
	if beginTimePlot("##balances", imgui.NewVec2(-1, plotH), g.xMin, g.xMax, g.yMin, g.yMax, &g.ticks, g.plotSize.Y) {
		g.drawPlot(views)
		endTimePlot()
		g.tags.draw()
	}
	g.drawLanes(views, laneH, pitch)
	g.breakdownPanel(panelH)
}
