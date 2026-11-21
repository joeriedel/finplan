// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"sync"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// maxCached bounds the series cache; panning across many chunks drops the series used least recently rather than growing.
const maxCached = 512

type seriesKey struct {
	asset model.AssetID
	proj  string
	grid  graph.Grid
}

type startKey struct {
	asset model.AssetID
	proj  string
}

type lastActual struct {
	date model.Date
	ok   bool
}

// startsSlot is the bgRunner slot of a line's first dates of real data; the line's series are queued under the line itself.
type startsSlot startKey

// dataCache memoizes engine answers so the engine does not run every frame, and runs the engine in the background: a series or a last date that is not cached is asked for, and shows in a later frame. A cache serves one record: the Graph makes a new one when the record changes and closes the old one, whose running jobs then finish unseen.
type dataCache struct {
	// rec is the record the cache serves; jobs read snap's copy of it.
	rec  *model.Record
	snap recCopy
	run  *bgRunner

	mu     sync.Mutex
	series map[seriesKey]*cachedSeries
	last   map[startKey]lastActual
	// asked is the grid queued for each line and running the grid being computed, so that the same ask is not queued again; askedLast is the lines whose last date is queued or computed.
	asked     map[startKey]graph.Grid
	running   map[startKey]graph.Grid
	askedLast map[startKey]bool
	// tick counts the lookups; a series is used at the tick of its last.
	tick uint64
	// fresh is set when a job lands, until the Graph takes it to refit.
	fresh bool
	// lands counts the jobs that landed, for the loop to tell whether it has redrawn for the latest.
	lands  uint64
	closed bool
}

// cachedSeries is a memoized series and when it was last used.
type cachedSeries struct {
	samples []engine.Sample
	used    uint64
}

func newDataCache(rec *model.Record) *dataCache {
	return &dataCache{rec: rec, snap: recCopy{src: rec}, run: newBGRunner(), series: map[seriesKey]*cachedSeries{}, last: map[startKey]lastActual{},
		asked: map[startKey]graph.Grid{}, running: map[startKey]graph.Grid{}, askedLast: map[startKey]bool{}}
}

// has reports whether the asset's balances under the projection at the grid's dates are cached.
func (c *dataCache) has(asset model.AssetID, proj string, grid graph.Grid) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.series[seriesKey{asset, proj, grid}]
	return ok
}

// cached returns the asset's balances under the projection at the grid's dates if they are cached.
func (c *dataCache) cached(asset model.AssetID, proj string, grid graph.Grid) ([]engine.Sample, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tick++
	e, ok := c.series[seriesKey{asset, proj, grid}]
	if !ok {
		return nil, false
	}
	e.used = c.tick
	return e.samples, true
}

// nearest returns the cached series of the asset under the projection that best serves the dates from..to, nil if there is none: the one with the finest grid among those that cover them, else the one that covers most of them. Samples carry their dates, so a series from another grid is drawn stretched or clipped to the view.
func (c *dataCache) nearest(asset model.AssetID, proj string, from, to model.Date) []engine.Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tick++
	var best *cachedSeries
	var bestKey seriesKey
	var bestCover bool
	var bestOverlap model.Date
	for k, e := range c.series {
		if k.asset != asset || k.proj != proj || len(e.samples) == 0 {
			continue
		}
		cover := k.grid.First <= from && k.grid.Last >= to
		overlap := min(k.grid.Last, to) - max(k.grid.First, from)
		if overlap < 0 {
			continue
		}
		better := best == nil
		switch {
		case better:
		case cover != bestCover:
			better = cover
		case cover:
			better = k.grid.Step < bestKey.grid.Step
		default:
			better = overlap > bestOverlap || (overlap == bestOverlap && k.grid.Step < bestKey.grid.Step)
		}
		if better {
			best, bestKey, bestCover, bestOverlap = e, k, cover, overlap
		}
	}
	if best == nil {
		return nil
	}
	best.used = c.tick
	return best.samples
}

// want asks for the asset's balances under the projection at the grid's dates, unless they are cached or already asked for. The ask replaces the line's earlier one that has not started, so a view that moves faster than the engine answers queues one job per line; the series is cached when it is done, and taken by nearest or cached in a later frame.
func (c *dataCache) want(asset model.AssetID, proj string, grid graph.Grid) {
	line := startKey{asset, proj}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if _, ok := c.series[seriesKey{asset, proj, grid}]; ok {
		return
	}
	if g, ok := c.asked[line]; ok && g == grid {
		return
	}
	if g, ok := c.running[line]; ok && g == grid {
		return
	}
	c.asked[line] = grid
	rec := c.snap.get()
	c.run.submit(line, func() { c.balances(rec, line, grid) })
}

// balances is the job of want.
func (c *dataCache) balances(rec *model.Record, line startKey, grid graph.Grid) {
	c.mu.Lock()
	if c.asked[line] == grid {
		delete(c.asked, line)
	}
	_, have := c.series[seriesKey{line.asset, line.proj, grid}]
	if c.closed || have {
		c.mu.Unlock()
		return
	}
	c.running[line] = grid
	c.mu.Unlock()

	e := &cachedSeries{}
	if res, err := engine.Balances(rec, line.proj, line.asset, grid.Times()); err == nil {
		e.samples = res.Samples
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running[line] == grid {
		delete(c.running, line)
	}
	if c.closed {
		return
	}
	if len(c.series) >= maxCached {
		c.dropOldest()
	}
	e.used = c.tick
	c.series[seriesKey{line.asset, line.proj, grid}] = e
	c.fresh, c.lands = true, c.lands+1
}

// dropOldest drops the half of the series used least recently, which the view panning on does not need, and keeps the rest, so that a long pan does not start over.
func (c *dataCache) dropOldest() {
	used := make([]uint64, 0, len(c.series))
	for _, e := range c.series {
		used = append(used, e.used)
	}
	slices.Sort(used)
	cut := used[len(used)/2]
	for k, e := range c.series {
		if e.used < cut {
			delete(c.series, k)
		}
	}
}

// lastActual returns the latest date any account under the asset has real data for, and false while it is not known: it is computed in the background the first time it is asked for.
func (c *dataCache) lastActual(asset model.AssetID, proj string) (model.Date, bool) {
	k := startKey{asset, proj}
	c.mu.Lock()
	defer c.mu.Unlock()
	if l, ok := c.last[k]; ok {
		return l.date, l.ok
	}
	if !c.askedLast[k] && !c.closed {
		c.askedLast[k] = true
		rec := c.snap.get()
		c.run.submit(startsSlot(k), func() { c.starts(rec, k) })
	}
	return 0, false
}

// starts is the job of lastActual.
func (c *dataCache) starts(rec *model.Record, k startKey) {
	var l lastActual
	if starts, err := engine.Starts(rec, k.proj, k.asset); err == nil {
		l.date, l.ok = graph.LastActual(starts)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.last[k] = l
		c.fresh, c.lands = true, c.lands+1
	}
}

// takeFresh reports whether a job landed since the last call.
func (c *dataCache) takeFresh() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.fresh
	c.fresh = false
	return f
}

// landCount returns the number of jobs that have landed.
func (c *dataCache) landCount() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lands
}

// busy reports whether a job is queued or running.
func (c *dataCache) busy() bool { return c.run.busy() }

// close drops the jobs that have not started and keeps the running ones from landing: the cache is being replaced.
func (c *dataCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}
