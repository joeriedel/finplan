// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"sync"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
)

// mcHorizonYears is how far the bands of a household without an earner reach; with one they reach the date the earner is mcSuccessAge.
const (
	mcHorizonYears = 40
	mcSuccessAge   = engine.DefaultSuccessAge
	mcSeed         = 1
)

// mcSeries are the percentile bands of a line, each as dollars per entry of dates.
type mcSeries struct {
	dates []model.Date
	// p10, p25, p50, p75 and p90 are the percentiles of the runs at each date.
	p10, p25, p50, p75, p90 []float64
	// success is the percent of runs above zero from today to successThrough; hasSuccess is false for a household without an earner.
	success        float64
	successThrough model.Date
	hasSuccess     bool
}

type mcKey struct {
	asset model.AssetID
	proj  string
}

// mcState is the state of one Monte Carlo job.
type mcState int

const (
	mcComputing mcState = iota
	mcReady
	mcFailed
)

type mcEntry struct {
	state  mcState
	series *mcSeries
}

// mcCache runs the Monte Carlo of the Graph's lines in the background, once per asset and projection, on a copy of the record it was made for. A cache serves one record: the Graph makes a new one when the record changes and drops the old one, whose running jobs finish into it unseen.
type mcCache struct {
	src *model.Record
	// rec is a copy of src made when the first job starts, so that jobs never read a record the UI goes on editing.
	rec     *model.Record
	mu      sync.Mutex
	entries map[mcKey]*mcEntry
	pending int
	// fresh is set when a job finishes, until the Graph takes it to refit.
	fresh bool
}

func newMCCache(rec *model.Record) *mcCache {
	return &mcCache{src: rec, entries: map[mcKey]*mcEntry{}}
}

// get returns the state of the line's Monte Carlo and, once it is ready, its series, and starts the job if there is none.
func (c *mcCache) get(asset model.AssetID, proj string) (mcState, *mcSeries) {
	k := mcKey{asset, proj}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[k]; ok {
		return e.state, e.series
	}
	if c.rec == nil {
		c.rec = c.src.Clone()
	}
	c.entries[k] = &mcEntry{}
	c.pending++
	go c.run(k, c.rec)
	return mcComputing, nil
}

// busy reports whether any job is still running.
func (c *mcCache) busy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending > 0
}

// takeFresh reports whether a job finished since the last call.
func (c *mcCache) takeFresh() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.fresh
	c.fresh = false
	return f
}

func (c *mcCache) run(k mcKey, rec *model.Record) {
	series, err := monteCarloSeries(rec, k.proj, k.asset)
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[k]
	e.state, e.series = mcReady, series
	if err != nil {
		e.state = mcFailed
	}
	c.pending--
	c.fresh = true
}

// monteCarloSeries runs the engine's default Monte Carlo of the asset under the projection on monthly dates from today to the date the household's first earner is mcSuccessAge, or mcHorizonYears from today if there is no earner. The dates do not depend on the zoom.
func monteCarloSeries(rec *model.Record, proj string, asset model.AssetID) (*mcSeries, error) {
	end := rec.Today.Add(mcHorizonYears * 366)
	age, hasAge := engine.AgeDate(rec, mcSuccessAge)
	if hasAge && age > rec.Today {
		end = age
	}
	dates := []model.Date{rec.Today}
	for _, d := range engine.MonthStarts(rec.Today, end) {
		if d > rec.Today {
			dates = append(dates, d)
		}
	}
	res, err := engine.MonteCarlo(rec, proj, asset, dates, engine.MonteCarloOptions{Seed: mcSeed, Percentiles: engine.DefaultPercentiles})
	if err != nil {
		return nil, err
	}
	s := &mcSeries{dates: res.Times, p10: res.Bands[0], p25: res.Bands[1], p50: res.Bands[2], p75: res.Bands[3], p90: res.Bands[4]}
	if hasAge && age > rec.Today {
		if s.success, err = engine.Success(res, rec.Today, age); err == nil {
			s.hasSuccess, s.successThrough = true, age
		}
	}
	return s, nil
}

// samples returns the values as samples dated by the series, for fitting the y range.
func (s *mcSeries) samples(values []float64) []engine.Sample {
	out := make([]engine.Sample, len(values))
	for i, v := range values {
		out[i] = engine.Sample{Date: s.dates[i], Amount: v}
	}
	return out
}
