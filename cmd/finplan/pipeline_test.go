// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// pipelineWait is the longest a test waits for the background work of a demo record.
const pipelineWait = 2 * time.Minute

// waitUntil polls cond until it holds, failing the test after pipelineWait.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(pipelineWait); !cond(); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// waitIdle waits until the Graph has no background work.
func waitIdle(t *testing.T, g *graphTab) {
	t.Helper()
	waitUntil(t, "the Graph's background work", func() bool { return !g.busy() })
}

// samplesOn asks for the series and waits for it.
func samplesOn(t *testing.T, c *dataCache, asset model.AssetID, proj string, grid graph.Grid) []engine.Sample {
	t.Helper()
	c.want(asset, proj, grid)
	var out []engine.Sample
	waitUntil(t, "a series", func() bool {
		var ok bool
		out, ok = c.cached(asset, proj, grid)
		return ok
	})
	return out
}

// blockWorkers occupies every worker of the runner until the returned function is called, so that what is asked for meanwhile stays queued.
func blockWorkers(t *testing.T, r *bgRunner) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	started := make(chan struct{}, backgroundWorkers)
	for i := range backgroundWorkers {
		r.submit(i, func() {
			started <- struct{}{}
			<-gate
		})
	}
	for range backgroundWorkers {
		<-started
	}
	return func() { close(gate) }
}

// A view that moved on before its series was made does not get it: the ask is replaced while it is queued.
func TestPipelineSupersededAskIsNotRun(t *testing.T) {
	rec := demodata.Family()
	c := newDataCache(rec)
	release := blockWorkers(t, c.run)
	a := graph.SampleGrid(rec.Today, rec.Today.Add(365), 1000)
	b := graph.SampleGrid(rec.Today.Add(2000), rec.Today.Add(2365), 1000)
	c.want(demodata.NetWorth, "", a)
	c.want(demodata.NetWorth, "", b)
	if !c.busy() {
		t.Fatal("the asks are not pending")
	}
	release()
	waitUntil(t, "the cache to be idle", func() bool { return !c.busy() })
	if c.has(demodata.NetWorth, "", a) {
		t.Error("the superseded ask was run")
	}
	if !c.has(demodata.NetWorth, "", b) {
		t.Error("the latest ask was not run")
	}
}

// A series for dates the view has moved away from is not drawn, though it is cached.
func TestPipelineSeriesOfAnotherViewIsNotDrawn(t *testing.T) {
	rec := demodata.Family()
	c := newDataCache(rec)
	a := graph.SampleGrid(rec.Today, rec.Today.Add(365), 1000)
	samplesOn(t, c, demodata.NetWorth, "", a)
	if s := c.nearest(demodata.NetWorth, "", rec.Today.Add(3000), rec.Today.Add(3365)); s != nil {
		t.Errorf("a view 8 years away is given %d samples of another", len(s))
	}
	if s := c.nearest(demodata.NetWorth, "", rec.Today.Add(100), rec.Today.Add(200)); len(s) == 0 {
		t.Error("a view inside the series has no samples")
	}
}

// nearest prefers the finest series that covers the view, then the one that covers most of it.
func TestPipelineNearestPrefersFinerCoveringSeries(t *testing.T) {
	rec := demodata.Family()
	c := newDataCache(rec)
	from, to := rec.Today.Add(100), rec.Today.Add(200)
	coarse := graph.Grid{Step: 30, First: rec.Today, Last: rec.Today.Add(365)}
	fine := graph.Grid{Step: 7, First: rec.Today.Add(50), Last: rec.Today.Add(250)}
	part := graph.Grid{Step: 1, First: rec.Today.Add(150), Last: rec.Today.Add(400)}
	samplesOn(t, c, demodata.NetWorth, "", coarse)
	if s := c.nearest(demodata.NetWorth, "", from, to); len(s) != len(coarse.Times()) {
		t.Errorf("%d samples, want the coarse series' %d", len(s), len(coarse.Times()))
	}
	samplesOn(t, c, demodata.NetWorth, "", part)
	if s := c.nearest(demodata.NetWorth, "", from, to); len(s) != len(coarse.Times()) {
		t.Errorf("a series that covers part of the view won over one that covers it: %d samples", len(s))
	}
	samplesOn(t, c, demodata.NetWorth, "", fine)
	if s := c.nearest(demodata.NetWorth, "", from, to); len(s) != len(fine.Times()) {
		t.Errorf("%d samples, want the fine series' %d", len(s), len(fine.Times()))
	}
}

// The plot draws what is cached while the series for the new grid is made, and refits y when it lands.
func TestPipelineDrawsCachedSamplesWhilePending(t *testing.T) {
	g := demoApp(t).graph
	g.setRange(graph.RangeButtons[1])
	g.prepareView(g.xMin, g.xMax, false, 1000)
	waitIdle(t, g)
	first := g.prepareView(g.xMin, g.xMax, false, 1000)
	if len(first) == 0 || len(first[0].samples) == 0 {
		t.Fatal("the settled view has no samples")
	}
	release := blockWorkers(t, g.cache.run)
	views := g.prepareView(g.xMin, g.xMax, false, 1500)
	if !g.cache.busy() {
		t.Fatal("the wider plot's series is not pending")
	}
	for i, v := range views {
		if len(v.samples) == 0 || len(v.samples) != len(first[i].samples) {
			t.Errorf("line %d has %d samples while the new grid is pending, want the %d it had", i, len(v.samples), len(first[i].samples))
		}
	}
	release()
	waitIdle(t, g)
	views = g.prepareView(g.xMin, g.xMax, false, 1500)
	if want := graph.SampleGrid(dateOf(g.xMin), dateOf(g.xMax), 1500); g.grid != want || len(views[0].samples) != len(want.Times()) {
		t.Errorf("grid %v with %d samples, want %v with %d", g.grid, len(views[0].samples), want, len(want.Times()))
	}
}

// Landing a series asks for a y fit, as Monte Carlo does.
func TestPipelineLandingRefitsY(t *testing.T) {
	g := demoApp(t).graph
	g.setRange(graph.RangeButtons[1])
	g.prepareView(g.xMin, g.xMax, false, 1000)
	if g.yMin != 0 || g.yMax != 1 {
		t.Fatalf("y %v..%v with nothing cached, want the default 0..1", g.yMin, g.yMax)
	}
	waitIdle(t, g)
	g.prepareView(g.xMin, g.xMax, false, 1000)
	if g.yMax <= g.yMin || (g.yMin == 0 && g.yMax == 1) {
		t.Errorf("y %v..%v was not fitted to the series that landed", g.yMin, g.yMax)
	}
}

// Work in flight when the data changes never lands in the cache that replaces it, nor in the old one.
func TestPipelineDataChangeInvalidatesWork(t *testing.T) {
	g := demoApp(t).graph
	old := g.cache
	rec := g.rec
	grid := graph.SampleGrid(rec.Today, rec.Today.Add(365), 1000)
	release := blockWorkers(t, old.run)
	old.want(demodata.NetWorth, "", grid)
	old.lastActual(demodata.NetWorth, "")
	g.setRecord(rec.Clone())
	if g.cache == old {
		t.Fatal("setRecord kept the cache")
	}
	release()
	waitUntil(t, "the old cache to be idle", func() bool { return !old.busy() })
	if old.has(demodata.NetWorth, "", grid) || old.landCount() != 0 {
		t.Error("a series landed in the replaced cache")
	}
	if g.cache.has(demodata.NetWorth, "", grid) || g.cache.landCount() != 0 {
		t.Error("work of the old record reached the new cache")
	}
}

// Closing a cache while a job may be running keeps the job from landing, whichever side of the start it is on.
func TestPipelineCloseKeepsRunningWorkFromLanding(t *testing.T) {
	rec := demodata.Family()
	for range 20 {
		c := newDataCache(rec)
		grid := graph.SampleGrid(rec.Today, rec.Today.Add(365), 1000)
		c.want(demodata.NetWorth, "", grid)
		c.close()
		waitUntil(t, "the cache to be idle", func() bool { return !c.busy() })
		if c.has(demodata.NetWorth, "", grid) {
			t.Fatal("a series landed after close")
		}
	}
}

// The last actual date is not known on the first ask, which does not run the engine, and is later.
func TestPipelineLastActualArrives(t *testing.T) {
	rec := demodata.Family()
	c := newDataCache(rec)
	if _, ok := c.lastActual(demodata.NetWorth, ""); ok {
		t.Fatal("the last date was known before it was asked for")
	}
	waitUntil(t, "the last date", func() bool { _, ok := c.lastActual(demodata.NetWorth, ""); return ok })
}

// The breakdown panel shows nothing at first, then the breakdown once it is made, and the previous one for the same line while another window is.
func TestPipelineBreakdownArrivesAndPreviousStays(t *testing.T) {
	rec := demodata.Family()
	c := newBreakdownCache(rec)
	k := bkKey{"", demodata.NetWorth, model.D(2025, time.June, 30), model.D(2025, time.December, 31)}
	if e, exact := c.get(k); e != nil || exact {
		t.Fatalf("an entry %v before the engine ran", e)
	}
	first := breakdownOn(t, c, k)
	release := blockWorkers(t, c.run)
	next := bkKey{"", demodata.NetWorth, model.D(2025, time.July, 31), model.D(2026, time.January, 31)}
	if e, exact := c.get(next); e != first || exact {
		t.Errorf("while the next window is made the panel has %p, exact %v, want the previous %p", e, exact, first)
	}
	other := bkKey{"", model.DefaultAssetID(demodata.Brokerage), next.from, next.to}
	if e, _ := c.get(other); e != nil {
		t.Error("another line was given the previous line's breakdown")
	}
	release()
	waitUntil(t, "the breakdown", func() bool { _, exact := c.get(other); return exact })
}

// Work for a window the cursor has left is not run, and closing the cache keeps the rest from landing.
func TestPipelineBreakdownSupersededAndClosed(t *testing.T) {
	rec := demodata.Family()
	c := newBreakdownCache(rec)
	release := blockWorkers(t, c.run)
	a := bkKey{"", demodata.NetWorth, model.D(2025, time.June, 30), model.D(2025, time.December, 31)}
	b := bkKey{"", demodata.NetWorth, model.D(2025, time.July, 31), model.D(2026, time.January, 31)}
	c.get(a)
	c.get(b)
	release()
	waitUntil(t, "the breakdown", func() bool { _, exact := c.get(b); return exact })
	waitUntil(t, "the cache to be idle", func() bool { return !c.busy() })
	if _, exact := c.get(a); exact {
		t.Error("the superseded window was made")
	}
	k := bkKey{"", demodata.NetWorth, model.D(2024, time.June, 30), model.D(2024, time.December, 31)}
	c.get(k)
	c.close()
	waitUntil(t, "the cache to be idle", func() bool { return !c.busy() })
	if _, exact := c.get(k); exact {
		t.Error("a breakdown landed in a closed cache")
	}
}

// The chart of the Assets tab is built in the background and replaced by the next asset's.
func TestPipelineChartIsBuiltInTheBackground(t *testing.T) {
	rec := demodata.Family()
	c := newChartCache(rec)
	if c.get(demodata.AllCash) != nil {
		t.Fatal("the chart was there on the first ask")
	}
	var ch *assetChart
	waitUntil(t, "the chart", func() bool { ch = c.get(demodata.AllCash); return ch != nil })
	if ch.Err != "" || len(ch.Samples) == 0 {
		t.Errorf("chart %+v", ch)
	}
	if c.get(demodata.Liquid) != nil {
		t.Error("the chart of another asset was the previous one")
	}
	c.close()
}

// The balances of the Assets tree are worked out in the background, and are those of buildAssets.
func TestPipelineAssetBalancesArrive(t *testing.T) {
	rec := demodata.Family()
	c := newBalanceCache(rec)
	waitUntil(t, "the balances", c.ready)
	want := assetRowOf(buildAssets(rec), demodata.AllCash)
	got := assetRowOf(buildAssetsWith(rec, c.balance), demodata.AllCash)
	if want == nil || got == nil || got.Balance != want.Balance || want.Balance == 0 {
		t.Fatalf("all cash rows %v and %v", got, want)
	}
}

// breakdownOn asks for the breakdown and waits for it.
func breakdownOn(t *testing.T, c *breakdownCache, k bkKey) *bkEntry {
	t.Helper()
	var e *bkEntry
	waitUntil(t, "a breakdown", func() bool {
		var exact bool
		e, exact = c.get(k)
		return exact
	})
	return e
}

// membersOf asks for the member breakdowns of the entry and waits for them.
func membersOf(t *testing.T, c *breakdownCache, e *bkEntry) []bkMember {
	t.Helper()
	var out []bkMember
	waitUntil(t, "the member breakdowns", func() bool {
		var ok bool
		out, ok = c.members(e)
		return ok
	})
	return out
}

// A job for an older window landing in a full cache after the wanted window's does not evict it: the wanted window is not asked for again, so the panel would show another window's breakdown under its header for good.
func TestPipelineBreakdownEvictionKeepsWanted(t *testing.T) {
	c := newBreakdownCache(demodata.Family())
	k := bkKey{"", demodata.NetWorth, model.D(2025, time.June, 30), model.D(2025, time.December, 31)}
	c.mu.Lock()
	c.want, c.wanted = k, true
	for i := range maxBreakdowns - 1 {
		other := bkKey{"", demodata.NetWorth, model.D(2000, time.January, 1) + model.Date(i), model.D(2001, time.January, 1)}
		c.entries[other] = &bkEntry{key: other, done: true}
	}
	c.storeLocked(&bkEntry{key: k})
	old := bkKey{"", demodata.NetWorth, model.D(1999, time.January, 1), model.D(1999, time.December, 31)}
	c.storeLocked(&bkEntry{key: old})
	c.mu.Unlock()
	if e, exact := c.get(k); !exact || e == nil || e.key != k {
		t.Errorf("the wanted window was evicted: got %v, exact %v", e, exact)
	}
}

// After a landing the loop redraws once for it and then goes idle, whichever tab is shown, and the Graph still refits when it is next drawn.
func TestPipelineLandingDoesNotKeepIdleLoopHot(t *testing.T) {
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	a, err := newApp(st, true)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a.graph)
	a.keepRedrawing(false)
	a.graph.cache.lastActual(demodata.NetWorth, "")
	waitIdle(t, a.graph)
	if !a.keepRedrawing(false) {
		t.Error("no redraw for a landing")
	}
	if a.keepRedrawing(false) {
		t.Error("the loop stays hot after the landing was redrawn for, with the Graph hidden")
	}
	if !a.graph.cache.takeFresh() {
		t.Error("the Graph will not refit when drawn after a landing while hidden")
	}
}

// A breakdown that lands while no input follows is redrawn for once, then the loop goes idle.
func TestPipelineBreakdownLandingRedrawsOnce(t *testing.T) {
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	a, err := newApp(st, true)
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, a.graph)
	a.keepRedrawing(false)
	c := a.graph.bkCache
	k := bkKey{"", demodata.NetWorth, model.D(2025, time.June, 30), model.D(2025, time.December, 31)}
	c.get(k)
	waitUntil(t, "the breakdown to land", func() bool { return c.landCount() > 0 && !c.busy() })
	if !a.keepRedrawing(false) {
		t.Error("no redraw for a breakdown landing")
	}
	if a.keepRedrawing(false) {
		t.Error("the loop stays hot after the breakdown was redrawn for")
	}
}
