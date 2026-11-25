// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"sync"

	"github.com/joeriedel/finplan/internal/model"
)

// chartCache builds the Assets tab's chart of the selected asset in the background, on a copy of the record it was made for. A cache serves one record: the tab makes a new one when the record changes and closes the old one.
type chartCache struct {
	snap recCopy
	run  *bgRunner

	mu       sync.Mutex
	want     model.AssetID
	wanted   bool
	chart    *assetChart
	chartFor model.AssetID
	closed   bool
	lands    uint64
}

func newChartCache(rec *model.Record) *chartCache {
	return &chartCache{snap: recCopy{src: rec}, run: newBGRunner()}
}

// get returns the chart of the asset, or nil while it is built, which it is asked to be if it is not built or being built already. The ask replaces the one before it that has not started.
func (c *chartCache) get(id model.AssetID) *assetChart {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.chart != nil && c.chartFor == id {
		return c.chart
	}
	if !c.closed && !(c.wanted && c.want == id) {
		c.want, c.wanted = id, true
		rec := c.snap.get()
		c.run.submit(struct{}{}, func() { c.build(rec, id) })
	}
	return nil
}

// build is the job of get.
func (c *chartCache) build(rec *model.Record, id model.AssetID) {
	c.mu.Lock()
	skip := c.closed || c.want != id
	c.mu.Unlock()
	if skip {
		return
	}
	chart := buildChart(rec, id)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed && c.want == id {
		c.chart, c.chartFor = chart, id
		c.lands++
	}
}

// landCount returns the number of charts that have landed.
func (c *chartCache) landCount() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lands
}

// busy reports whether a job is queued or running.
func (c *chartCache) busy() bool { return c.run.busy() }

// close keeps the running job from landing: the cache is being replaced.
func (c *chartCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

// balanceCache works out in the background the balance today of every asset the tree shows a balance for and of the default asset of every account, which the Accounts tab shows, on a copy of the record it was made for, closed when the tab replaces it.
type balanceCache struct {
	snap recCopy
	run  *bgRunner

	mu     sync.Mutex
	vals   map[model.AssetID]float64
	done   bool
	closed bool
	lands  uint64
}

func newBalanceCache(rec *model.Record) *balanceCache {
	c := &balanceCache{snap: recCopy{src: rec}, run: newBGRunner()}
	snap := c.snap.get()
	c.run.submit(struct{}{}, func() { c.compute(snap) })
	return c
}

// compute is the job of newBalanceCache.
func (c *balanceCache) compute(rec *model.Record) {
	vals := map[model.AssetID]float64{}
	for _, a := range rec.Assets {
		vals[a.ID] = todayBalance(rec, a.ID)
	}
	for _, a := range rec.Accounts {
		id := model.DefaultAssetID(a.ID)
		vals[id] = todayBalance(rec, id)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.vals, c.done = vals, true
		c.lands++
	}
}

// balance returns the balance of the asset, 0 if it is not known.
func (c *balanceCache) balance(id model.AssetID) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vals[id]
}

// ready reports whether the balances are worked out.
func (c *balanceCache) ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}

// landCount returns the number of times the balances landed.
func (c *balanceCache) landCount() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lands
}

// busy reports whether the job is queued or running.
func (c *balanceCache) busy() bool { return c.run.busy() }

// close keeps the running job from landing: the cache is being replaced.
func (c *balanceCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}
