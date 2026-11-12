// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"sync"

	"github.com/joeriedel/finplan/internal/model"
)

// DefaultRuns is the number of runs MonteCarlo makes when asked for none.
const DefaultRuns = 1000

// DefaultPercentiles are the percentiles MonteCarlo returns when asked for none.
var DefaultPercentiles = []float64{10, 25, 50, 75, 90}

// MonteCarloOptions say how many runs to make, with which seed, and which percentiles to report. Zero Runs and empty Percentiles mean the defaults; any Seed is valid.
type MonteCarloOptions struct {
	Runs        int
	Seed        uint64
	Percentiles []float64 // each from 0 to 100
}

// MonteCarloResult answers MonteCarlo.
type MonteCarloResult struct {
	Target      model.AssetID
	Times       []model.Date
	Runs        int
	Seed        uint64
	Percentiles []float64
	// Bands[i][t] is the Percentiles[i] percentile of the runs' values at Times[t].
	Bands [][]float64
	// Values[r][t] is the value of run r at Times[t], for what the bands do not say, such as the share of runs that stay above a level up to a date.
	Values [][]float64
}

// MonteCarlo runs the projection many times with the market classes' and the stocks' yearly returns drawn at random and returns the percentile bands of the target's value at the times. Each run draws one correlated lognormal return per class and year, and one independent lognormal price return per stock and year, from where the projection starts: realized rate = (1+r)·exp(σz − σ²/2) − 1, with r the expected rate the deterministic projection uses, σ the volatility and z a standard normal. A year is the stretch between anniversaries of the projection's start. Run i draws from a stream derived from the seed and i alone, so the result does not depend on how the runs are scheduled.
//
// A percentile p of n sorted values is read at rank (n−1)·p/100, linearly interpolated between the two values around it.
func MonteCarlo(rec *model.Record, projection string, target model.AssetID, times []model.Date, opts MonteCarloOptions) (*MonteCarloResult, error) {
	if opts.Runs < 0 {
		return nil, fmt.Errorf("the number of runs %d must not be negative", opts.Runs)
	}
	if opts.Runs == 0 {
		opts.Runs = DefaultRuns
	}
	if len(opts.Percentiles) == 0 {
		opts.Percentiles = DefaultPercentiles
	}
	for _, p := range opts.Percentiles {
		if math.IsNaN(p) || p < 0 || p > 100 {
			return nil, fmt.Errorf("the percentile %v must be between 0 and 100", p)
		}
	}
	sampler, err := NewSampler(rec)
	if err != nil {
		return nil, err
	}
	horizon := model.MinDate
	for _, t := range times {
		horizon = max(horizon, t)
	}
	res := &MonteCarloResult{Target: target, Times: slices.Clone(times), Runs: opts.Runs, Seed: opts.Seed, Percentiles: slices.Clone(opts.Percentiles), Values: make([][]float64, opts.Runs)}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		next     int
	)
	for range min(runtime.GOMAXPROCS(0), opts.Runs) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				i := next
				next++
				failed := firstErr != nil
				mu.Unlock()
				if i >= opts.Runs || failed {
					return
				}
				r, err := prepareDrawn(rec, projection, target, horizon, newRateDraws(rec, sampler, opts.Seed, i))
				if err != nil {
					mu.Lock()
					firstErr = cmp.Or(firstErr, err)
					mu.Unlock()
					return
				}
				vals := make([]float64, len(times))
				for t, d := range times {
					vals[t] = r.eval(d).Amount
				}
				res.Values[i] = vals
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	res.Bands = make([][]float64, len(opts.Percentiles))
	for i := range res.Bands {
		res.Bands[i] = make([]float64, len(times))
	}
	col := make([]float64, opts.Runs)
	for t := range times {
		for i := range col {
			col[i] = res.Values[i][t]
		}
		slices.Sort(col)
		for i, p := range opts.Percentiles {
			res.Bands[i][t] = percentile(col, p)
		}
	}
	return res, nil
}

// percentile reads the percentile p of the sorted values at rank (n−1)·p/100, interpolating linearly.
func percentile(sorted []float64, p float64) float64 {
	rank := float64(len(sorted)-1) * p / 100
	lo := int(math.Floor(rank))
	if lo >= len(sorted)-1 {
		return sorted[len(sorted)-1]
	}
	return sorted[lo] + (rank-float64(lo))*(sorted[lo+1]-sorted[lo])
}

// rateDraws are one run's random returns. The shocks of each year are drawn when first asked for, in order, so a year's draw does not depend on how many years a query needs.
type rateDraws struct {
	sampler *Sampler
	rec     *model.Record
	seed    uint64
	run     int
	rng     *rand.Rand
	// factors[c][y] is the lognormal factor of the class at index c in year y.
	factors [][]float64
	z       []float64
	stocks  map[string]*stockDraws
	// classes and stockRates are the schedules built for a class and a stock, which every account holding them shares.
	classes    map[string]model.Schedule
	stockRates map[string]model.Schedule
}

type stockDraws struct {
	rng     *rand.Rand
	sigma   float64
	factors []float64
}

func newRateDraws(rec *model.Record, s *Sampler, seed uint64, run int) *rateDraws {
	return &rateDraws{
		sampler: s, rec: rec, seed: seed, run: run,
		rng:        rand.New(rand.NewPCG(seed, uint64(run))),
		factors:    make([][]float64, len(s.ids)),
		z:          make([]float64, len(s.ids)),
		stocks:     map[string]*stockDraws{},
		classes:    map[string]model.Schedule{},
		stockRates: map[string]model.Schedule{},
	}
}

// classRate returns the expected rate schedule base of the class with the years from start up to horizon replaced by the drawn rates, or base itself if the class has no volatility or no draws.
func (d *rateDraws) classRate(class string, base model.Schedule, start, horizon model.Date) model.Schedule {
	if s, ok := d.classes[class]; ok {
		return s
	}
	out := base
	if c := slices.Index(d.sampler.ids, class); c >= 0 && d.sampler.vol[c] != 0 {
		years := yearsBetween(start, horizon)
		for len(d.factors[c]) < years {
			d.sampler.Draw(d.rng, d.z)
			for i := range d.factors {
				d.factors[i] = append(d.factors[i], lognormalFactor(d.sampler.vol[i], d.z[i]))
			}
		}
		out = yearly(base, start, d.factors[c][:years])
	}
	d.classes[class] = out
	return out
}

// stockRate is classRate for a stock's growth, drawn with the stock's volatility independently of the classes and of the other stocks.
func (d *rateDraws) stockRate(symbol string, base model.Schedule, start, horizon model.Date) model.Schedule {
	if s, ok := d.stockRates[symbol]; ok {
		return s
	}
	out := base
	if sigma := d.rec.StockVolatilityOf(symbol); sigma != 0 {
		sd := d.stocks[symbol]
		if sd == nil {
			h := fnv.New64a()
			h.Write([]byte(symbol))
			sd = &stockDraws{rng: rand.New(rand.NewPCG(d.seed^h.Sum64(), uint64(d.run))), sigma: sigma}
			d.stocks[symbol] = sd
		}
		years := yearsBetween(start, horizon)
		for len(sd.factors) < years {
			sd.factors = append(sd.factors, lognormalFactor(sd.sigma, sd.rng.NormFloat64()))
		}
		out = yearly(base, start, sd.factors[:years])
	}
	d.stockRates[symbol] = out
	return out
}

// yearsBetween is the number of years that cover start through horizon, counting the calendar years of the two dates inclusively.
func yearsBetween(start, horizon model.Date) int {
	sy, _, _ := start.YMD()
	hy, _, _ := horizon.YMD()
	return max(0, hy-sy+1)
}

// yearly returns base with each year from start, ending on the day before the next anniversary of start, grown at (1+r)·factor − 1 on each stretch of the year where base's rate is r; a stretch where base has no rate counts as rate 0. A rate that changes within a year keeps its change date. A 29 February anniversary falls on 28 February in other years.
func yearly(base model.Schedule, start model.Date, factors []float64) model.Schedule {
	sy, month, day := start.YMD()
	anniversary := func(k int) model.Date {
		a := model.D(sy+k, month, day)
		if _, m, _ := a.YMD(); m != month {
			a = model.D(sy+k, month, 28)
		}
		return a
	}
	baseSegs := base.Segments()
	var segs []model.Segment
	for k, f := range factors {
		from, last := anniversary(k), anniversary(k+1).Add(-1)
		add := func(a, b model.Date, r float64) {
			segs = append(segs, model.Segment{From: a, End: model.Ptr(b), Value: (1+r)*f - 1})
		}
		cur := from
		for i, bs := range baseSegs {
			if cur > last {
				break
			}
			bsEnd := model.MaxDate
			if bs.End != nil {
				bsEnd = *bs.End
			} else if i+1 < len(baseSegs) {
				bsEnd = baseSegs[i+1].From.Add(-1)
			}
			if bsEnd < cur {
				continue
			}
			if bs.From > last {
				break
			}
			if bs.From > cur {
				add(cur, bs.From.Add(-1), 0)
				cur = bs.From
			}
			end := min(last, bsEnd)
			add(cur, end, bs.Value)
			cur = end.Add(1)
		}
		if cur <= last {
			add(cur, last, 0)
		}
	}
	return base.Overlay(model.MustSchedule(segs...))
}
