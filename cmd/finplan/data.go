// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// maxCached bounds the series cache; panning across many chunks starts it over rather than growing.
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
	series map[seriesKey][]engine.Sample
	last   map[startKey]lastActual
}

func newDataCache(rec *model.Record) *dataCache {
	return &dataCache{rec: rec, series: map[seriesKey][]engine.Sample{}, last: map[startKey]lastActual{}}
}

// samples returns the asset's balances under the projection at the grid's dates, or nil if the engine fails.
func (c *dataCache) samples(asset model.AssetID, proj string, grid graph.Grid) []engine.Sample {
	k := seriesKey{asset, proj, grid}
	if s, ok := c.series[k]; ok {
		return s
	}
	if len(c.series) >= maxCached {
		clear(c.series)
	}
	var out []engine.Sample
	if res, err := engine.Balances(c.rec, proj, asset, grid.Times()); err == nil {
		out = res.Samples
	}
	c.series[k] = out
	return out
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
