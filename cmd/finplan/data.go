// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"

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

// dataCache memoizes engine answers so the engine does not run every frame.
type dataCache struct {
	rec    *model.Record
	series map[seriesKey]*cachedSeries
	last   map[startKey]lastActual
	// tick counts the lookups; a series is used at the tick of its last.
	tick uint64
}

// cachedSeries is a memoized series and when it was last used.
type cachedSeries struct {
	samples []engine.Sample
	used    uint64
}

func newDataCache(rec *model.Record) *dataCache {
	return &dataCache{rec: rec, series: map[seriesKey]*cachedSeries{}, last: map[startKey]lastActual{}}
}

// has reports whether the asset's balances under the projection at the grid's dates are cached.
func (c *dataCache) has(asset model.AssetID, proj string, grid graph.Grid) bool {
	_, ok := c.series[seriesKey{asset, proj, grid}]
	return ok
}

// samples returns the asset's balances under the projection at the grid's dates, or nil if the engine fails.
func (c *dataCache) samples(asset model.AssetID, proj string, grid graph.Grid) []engine.Sample {
	c.tick++
	k := seriesKey{asset, proj, grid}
	if e, ok := c.series[k]; ok {
		e.used = c.tick
		return e.samples
	}
	if len(c.series) >= maxCached {
		c.dropOldest()
	}
	e := &cachedSeries{used: c.tick}
	if res, err := engine.Balances(c.rec, proj, asset, grid.Times()); err == nil {
		e.samples = res.Samples
	}
	c.series[k] = e
	return e.samples
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

// lastActual returns the latest date any account under the asset has real data for.
func (c *dataCache) lastActual(asset model.AssetID, proj string) (model.Date, bool) {
	k := startKey{asset, proj}
	if l, ok := c.last[k]; ok {
		return l.date, l.ok
	}
	var l lastActual
	if starts, err := engine.Starts(c.rec, proj, asset); err == nil {
		l.date, l.ok = graph.LastActual(starts)
	}
	c.last[k] = l
	return l.date, l.ok
}
