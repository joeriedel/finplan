// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

func TestZoomTimeSpanKeepsTheAnchorInPlace(t *testing.T) {
	lo, hi := 1.0e9, 1.0e9+1000*secondsPerDay
	anchor := lo + 0.25*(hi-lo)
	nlo, nhi := zoomTimeSpan(lo, hi, anchor, 2)
	if want := (hi - lo) * zoomPerNotch * zoomPerNotch; !closeTo(nhi-nlo, want) {
		t.Errorf("span %v, want %v", nhi-nlo, want)
	}
	if frac := (anchor - nlo) / (nhi - nlo); !closeTo(frac, 0.25) {
		t.Errorf("the anchor moved to %v of the span", frac)
	}
	// Zooming out by the same notches restores the span.
	olo, ohi := zoomTimeSpan(nlo, nhi, anchor, -2)
	if !closeTo(olo, lo) || !closeTo(ohi, hi) {
		t.Errorf("out again: %v..%v, want %v..%v", olo, ohi, lo, hi)
	}
}

func TestZoomTimeSpanConstraints(t *testing.T) {
	today := float64(model.D(2026, 11, 8)) * secondsPerDay
	lo, hi := today-365*secondsPerDay, today+365*secondsPerDay
	for range 100 {
		lo, hi = zoomTimeSpan(lo, hi, today, 1)
	}
	if !closeTo(hi-lo, minTimeSpan) {
		t.Errorf("zoomed in to %v days, want 7", (hi-lo)/secondsPerDay)
	}
	for range 100 {
		lo, hi = zoomTimeSpan(lo, hi, today, -1)
	}
	if !closeTo(hi-lo, maxTimeSpan) {
		t.Errorf("zoomed out to %v days, want two centuries", (hi-lo)/secondsPerDay)
	}
	if lo < timeAxisMin-1 || hi > timeAxisMax+1 {
		t.Errorf("view %v..%v leaves the time axis", lo, hi)
	}
}

func TestPanTimeSpan(t *testing.T) {
	lo, hi := 1.0e9, 1.0e9+100*secondsPerDay
	nlo, nhi := panTimeSpan(lo, hi, 10*secondsPerDay)
	if !closeTo(nlo, lo+10*secondsPerDay) || !closeTo(nhi, hi+10*secondsPerDay) {
		t.Errorf("panned to %v..%v", nlo, nhi)
	}
	nlo, nhi = panTimeSpan(lo, hi, -1e12)
	if nlo != timeAxisMin || !closeTo(nhi-nlo, 100*secondsPerDay) {
		t.Errorf("panned past the start: %v..%v", nlo, nhi)
	}
	nlo, nhi = panTimeSpan(lo, hi, 1e12)
	if nhi != timeAxisMax || !closeTo(nhi-nlo, 100*secondsPerDay) {
		t.Errorf("panned past the end: %v..%v", nlo, nhi)
	}
}

func TestFitTimeSpanRepairsBadSpans(t *testing.T) {
	lo, hi := fitTimeSpan(1.0e9, 1.0e9)
	if !closeTo(hi-lo, minTimeSpan) {
		t.Errorf("empty span became %v days", (hi-lo)/secondsPerDay)
	}
	lo, hi = fitTimeSpan(0, 1e12)
	if lo < timeAxisMin || hi > timeAxisMax || !closeTo(hi-lo, maxTimeSpan) {
		t.Errorf("huge span became %v..%v", lo, hi)
	}
}

// The y range is fitted in the frame in which the x range changes, not the next one.
func TestPrepareViewFitsYInTheFrameOfTheChange(t *testing.T) {
	g := demoApp(t).graph
	g.setRange(graph.RangeButtons[1])
	g.prepareView(g.xMin, g.xMax, false, 1000)
	waitIdle(t, g)
	g.prepareView(g.xMin, g.xMax, false, 1000)
	lo5, hi5 := g.yMin, g.yMax
	// Zoom in on a year a long way ahead. Its series is made in the background and fitted when it lands.
	today := float64(g.rec.Today) * secondsPerDay
	g.prepareView(today+3000*secondsPerDay, today+3365*secondsPerDay, true, 1000)
	waitIdle(t, g)
	views := g.prepareView(g.xMin, g.xMax, false, 1000)
	series := make([][]engine.Sample, len(views))
	for i, v := range views {
		series[i] = v.samples
	}
	wlo, whi, ok := fitYRange(series, dateOf(g.xMin), dateOf(g.xMax))
	if !ok || g.yMin != wlo || g.yMax != whi {
		t.Errorf("y %v..%v after the zoom, want the fit %v..%v of its lines", g.yMin, g.yMax, wlo, whi)
	}
	if g.yMin == lo5 && g.yMax == hi5 {
		t.Error("y did not change with the view")
	}
	// A pan refits again at once.
	before := g.yMax
	g.prepareView(g.xMin+1500*secondsPerDay, g.xMax+1500*secondsPerDay, true, 1000)
	waitIdle(t, g)
	g.prepareView(g.xMin, g.xMax, false, 1000)
	if g.yMax == before {
		t.Error("y did not follow the pan")
	}
}

// While the view moves, the grid is not changed to the exact one; that comes when it settles.
func TestPrepareViewKeepsTheGridWhileMoving(t *testing.T) {
	g := demoApp(t).graph
	g.setRange(graph.RangeButtons[1])
	g.prepareView(g.xMin, g.xMax, false, 1000)
	settled := g.grid
	mid := (g.xMin + g.xMax) / 2
	lo, hi := zoomTimeSpan(g.xMin, g.xMax, mid, 4)
	want := graph.SampleGrid(dateOf(lo), dateOf(hi), 1000)
	if want == settled {
		t.Fatal("the test's zoom does not change the grid")
	}
	g.prepareView(lo, hi, true, 1000)
	if g.grid != settled || g.cache.has(g.lines.All()[0].Asset, g.lines.Switcher(), want) {
		t.Errorf("a moving view changed the grid from %v to %v", settled, g.grid)
	}
	for range viewSettleFrames + 1 {
		g.prepareView(lo, hi, false, 1000)
	}
	if g.grid != want {
		t.Errorf("settled on %v, want %v", g.grid, want)
	}
}

func TestMoneyTicksAreMadeForTheDrawnRangeOnce(t *testing.T) {
	var m moneyTicks
	made := 0
	gutter := func() string { made++; return "$000.00M" }
	m.fill(1000, 9000, 400, gutter)
	if len(m.ticks) < 3 || len(m.ticks) != len(m.labels) {
		t.Fatalf("%d ticks, %d labels", len(m.ticks), len(m.labels))
	}
	for _, v := range m.ticks[:len(m.ticks)-1] {
		if v < 1000 || v > 9000 {
			t.Errorf("tick %v outside the range 1000..9000", v)
		}
	}
	if last := m.ticks[len(m.ticks)-1]; last >= 1000 {
		t.Errorf("the hidden tick %v is within the range", last)
	}
	m.fill(1000, 9000, 400, gutter)
	if made != 1 {
		t.Errorf("an unchanged range made the ticks %d times", made)
	}
	m.fill(2000, 12000, 400, gutter)
	if made != 2 || m.ticks[0] < 2000 {
		t.Errorf("a new range kept the old ticks: %v", m.ticks)
	}
}

// Double-clicking the plot fits x to the All range of the drawn lines, within the time axis limits.
func TestGraphFitSpanIsTheDataRange(t *testing.T) {
	g := demoApp(t).graph
	lo, hi, ok := g.fitSpan()
	if !ok {
		t.Fatal("no fit span with drawn lines")
	}
	from, to := graph.RangeButtons[len(graph.RangeButtons)-1].Span(g.rec.Today, g.dataStart)
	if lo != float64(from)*secondsPerDay || hi != float64(to)*secondsPerDay || lo < timeAxisMin || hi > timeAxisMax {
		t.Errorf("fit span %v..%v, want %v..%v", dateOf(lo), dateOf(hi), from, to)
	}
	for _, ln := range g.lines.All() {
		g.lines.Remove(ln.ID)
	}
	if _, _, ok := g.fitSpan(); ok {
		t.Error("fit span with no lines")
	}
}

// A view narrower than the sample grid has no sample in it, but y still fits the lines' values there.
func TestPrepareViewFitsYInAViewNarrowerThanTheGrid(t *testing.T) {
	g := demoApp(t).graph
	g.setRange(graph.RangeButtons[3])
	g.prepareView(g.xMin, g.xMax, false, 1000)
	waitIdle(t, g)
	g.prepareView(g.xMin, g.xMax, false, 1000)
	if g.grid.Step < 14 {
		t.Fatalf("grid step %d is not wider than the view", g.grid.Step)
	}
	stale := g.yMax - g.yMin
	mid := float64(g.rec.Today)*secondsPerDay + 2000*secondsPerDay
	g.prepareView(mid, mid+minTimeSpan, true, 1000)
	if g.grid.Step < 14 {
		t.Fatalf("grid step %d after the zoom, want the coarse grid kept", g.grid.Step)
	}
	if !(g.yMax > g.yMin) || g.yMax-g.yMin == stale {
		t.Errorf("y %v..%v was not fitted to the week", g.yMin, g.yMax)
	}
	series := [][]engine.Sample{}
	for _, v := range g.visible(g.grid) {
		series = append(series, withEnds(v.samples, dateOf(g.xMin), dateOf(g.xMax)))
	}
	if lo, hi, ok := fitYRange(series, dateOf(g.xMin), dateOf(g.xMax)); !ok || lo != g.yMin || hi != g.yMax {
		t.Errorf("y %v..%v, want %v..%v", g.yMin, g.yMax, lo, hi)
	}
}

// Setting a record refits y even though x is unchanged.
func TestSetRecordAsksForAYFit(t *testing.T) {
	g := demoApp(t).graph
	g.fitY = false
	g.setRecord(g.rec)
	if !g.fitY {
		t.Error("setRecord did not ask for a y fit")
	}
}
