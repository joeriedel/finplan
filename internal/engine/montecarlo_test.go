// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

// yearlyTimes returns the first of January of each of the next years years after the record's today, then the day itself.
func yearlyTimes(rec *model.Record, years int) []model.Date {
	out := []model.Date{rec.Today}
	y, _, _ := rec.Today.YMD()
	for i := 1; i <= years; i++ {
		out = append(out, model.D(y+i, time.January, 1))
	}
	return out
}

func TestMonteCarloSameSeedSameBands(t *testing.T) {
	rec := demodata.Family()
	times := yearlyTimes(rec, 8)
	opts := MonteCarloOptions{Runs: 40, Seed: 7}
	a, err := MonteCarlo(rec, demodata.ProjBase, demodata.NetWorth, times, opts)
	must(t, err)
	b, err := MonteCarlo(rec, demodata.ProjBase, demodata.NetWorth, times, opts)
	must(t, err)
	if !reflect.DeepEqual(a, b) {
		t.Error("the same seed gave different results")
	}
	opts.Seed = 8
	c, err := MonteCarlo(rec, demodata.ProjBase, demodata.NetWorth, times, opts)
	must(t, err)
	if reflect.DeepEqual(a.Bands, c.Bands) {
		t.Error("a different seed gave the same bands")
	}
	if a.Runs != 40 || a.Seed != 7 || !reflect.DeepEqual(a.Percentiles, DefaultPercentiles) || len(a.Bands) != 5 || len(a.Bands[0]) != len(times) || len(a.Values) != 40 {
		t.Errorf("result shape: runs %d seed %d percentiles %v bands %d", a.Runs, a.Seed, a.Percentiles, len(a.Bands))
	}
	last := len(times) - 1
	for i := 1; i < len(a.Bands); i++ {
		if a.Bands[i][last] < a.Bands[i-1][last] {
			t.Errorf("percentile %v below percentile %v at the end", a.Percentiles[i], a.Percentiles[i-1])
		}
	}
	if a.Bands[0][last] == a.Bands[4][last] {
		t.Error("the bands did not spread")
	}
}

func TestMonteCarloZeroVolatilityIsTheDeterministicLine(t *testing.T) {
	rec := demodata.Family()
	for i := range rec.Classes {
		rec.Classes[i].Volatility = 0
	}
	for _, s := range rec.Symbols() {
		rec.StockVolatility[s] = 0
	}
	times := yearlyTimes(rec, 12)
	want, err := Balances(rec, demodata.ProjBase, demodata.NetWorth, times)
	must(t, err)
	got, err := MonteCarlo(rec, demodata.ProjBase, demodata.NetWorth, times, MonteCarloOptions{Runs: 10, Seed: 3, Percentiles: []float64{0, 50, 100}})
	must(t, err)
	for i := range got.Bands {
		for k, s := range want.Samples {
			if !near(got.Bands[i][k], s.Amount) {
				t.Errorf("percentile %v at %v: %v, want %v", got.Percentiles[i], s.Date, got.Bands[i][k], s.Amount)
			}
		}
	}
}

func TestMonteCarloStockVolatilityMovesEquity(t *testing.T) {
	rec := demodata.Family()
	for i := range rec.Classes {
		rec.Classes[i].Volatility = 0
	}
	times := yearlyTimes(rec, 6)
	res, err := MonteCarlo(rec, demodata.ProjBase, demodata.NetWorth, times, MonteCarloOptions{Runs: 30, Seed: 1, Percentiles: []float64{10, 90}})
	must(t, err)
	if last := len(times) - 1; res.Bands[0][last] >= res.Bands[1][last] {
		t.Error("stock volatility alone did not spread the bands")
	}
}

func TestMonteCarloRejectsInconsistentCorrelations(t *testing.T) {
	rec := demodata.Family()
	rec.Correlations = []model.Correlation{
		{A: model.ClassLargeCap, B: model.ClassSmallCap, Value: 0.95},
		{A: model.ClassLargeCap, B: model.ClassInternational, Value: 0.95},
		{A: model.ClassInternational, B: model.ClassSmallCap, Value: -0.95},
	}
	_, err := MonteCarlo(rec, "", demodata.NetWorth, yearlyTimes(rec, 2), MonteCarloOptions{Runs: 2})
	if err == nil || !strings.Contains(err.Error(), "not positive definite") {
		t.Errorf("error %v", err)
	}
}

func TestMonteCarloRejectsBadOptions(t *testing.T) {
	rec := demodata.Family()
	times := yearlyTimes(rec, 1)
	for _, opts := range []MonteCarloOptions{{Runs: -1}, {Percentiles: []float64{101}}, {Percentiles: []float64{-1}}} {
		if _, err := MonteCarlo(rec, "", demodata.NetWorth, times, opts); err == nil {
			t.Errorf("%+v accepted", opts)
		}
	}
}

func TestPercentileInterpolates(t *testing.T) {
	v := []float64{10, 20, 30, 40}
	for p, want := range map[float64]float64{0: 10, 50: 25, 100: 40, 25: 17.5, 90: 37} {
		if got := percentile(v, p); !near(got, want) {
			t.Errorf("percentile %v = %v, want %v", p, got, want)
		}
	}
	if got := percentile([]float64{5}, 90); got != 5 {
		t.Errorf("one value: %v", got)
	}
}

func TestSamplerReproducesCorrelationsAndVolatilities(t *testing.T) {
	rec := model.NewRecord(d(2026, time.January, 1))
	rec.Classes = model.DefaultClasses()
	rec.Correlations = model.DefaultCorrelations()
	s, err := NewSampler(rec)
	must(t, err)
	n := len(s.Classes())
	const draws = 200000
	rng := rand.New(rand.NewPCG(5, 6))
	z := make([]float64, n)
	sum := make([]float64, n)
	cross := make([][]float64, n)
	for i := range cross {
		cross[i] = make([]float64, n)
	}
	logSum, logSq, factorSum := 0.0, 0.0, 0.0
	for range draws {
		s.Draw(rng, z)
		for i := range n {
			sum[i] += z[i]
			for j := range n {
				cross[i][j] += z[i] * z[j]
			}
		}
		l := math.Log(lognormalFactor(0.2, z[0]))
		logSum += l
		logSq += l * l
		factorSum += lognormalFactor(0.2, z[0])
	}
	for i := range n {
		if m := sum[i] / draws; math.Abs(m) > 0.01 {
			t.Errorf("mean of %s = %v", s.Classes()[i], m)
		}
		for j := range n {
			got := cross[i][j]/draws - sum[i]/draws*sum[j]/draws
			if want := rec.CorrelationOf(s.Classes()[i], s.Classes()[j]); math.Abs(got-want) > 0.01 {
				t.Errorf("correlation of %s and %s = %v, want %v", s.Classes()[i], s.Classes()[j], got, want)
			}
		}
	}
	// The log of the factor has the volatility, and the factor has a mean of 1.
	if sd := math.Sqrt(logSq/draws - math.Pow(logSum/draws, 2)); math.Abs(sd-0.2) > 0.003 {
		t.Errorf("volatility of the log factor %v, want 0.2", sd)
	}
	if m := factorSum / draws; math.Abs(m-1) > 0.005 {
		t.Errorf("mean factor %v, want 1", m)
	}
}

func TestYearlyReplacesFromStartAndKeepsBefore(t *testing.T) {
	base := model.Constant(0.10)
	start := d(2024, time.February, 29)
	got := yearly(base, start, []float64{1.1, 0.9})
	for date, want := range map[model.Date]float64{
		d(2024, time.February, 28): 0.10,
		start:                      1.1*1.1 - 1,
		d(2025, time.February, 27): 1.1*1.1 - 1,
		d(2025, time.February, 28): 1.1*0.9 - 1,
		d(2026, time.February, 27): 1.1*0.9 - 1,
		d(2026, time.February, 28): 0.10,
	} {
		if v, ok := got.At(date); !ok || !near(v, want) {
			t.Errorf("rate on %v = %v, want %v", date, v, want)
		}
	}
}

// netWorthWorkload is the workload of the speed target: 1,000 runs over 60 years of the demo family's net worth.
func netWorthWorkload(tb testing.TB) {
	rec := demodata.Family()
	times := yearlyTimes(rec, 60)
	if _, err := MonteCarlo(rec, demodata.ProjBase, demodata.NetWorth, times, MonteCarloOptions{Runs: 1000, Seed: 1}); err != nil {
		tb.Fatal(err)
	}
}

func BenchmarkMonteCarlo(b *testing.B) {
	for range b.N {
		netWorthWorkload(b)
	}
}

// TestMonteCarloSpeed guards the speed target loosely: 1,000 runs of 60 years should take a few seconds, and fail here only if they take several times that.
func TestMonteCarloSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("speed guard")
	}
	start := time.Now()
	netWorthWorkload(t)
	if took := time.Since(start); took > 15*time.Second {
		t.Errorf("1,000 runs of 60 years took %v, want a few seconds", took)
	}
}

func TestYearlyKeepsScheduleChangesAndGaps(t *testing.T) {
	base := model.MustSchedule(
		model.Segment{From: model.MinDate, End: model.Ptr(d(2026, 12, 31)), Value: 0.05},
		model.Segment{From: d(2027, 6, 1), End: model.Ptr(d(2027, 12, 31)), Value: -0.4},
	)
	start := d(2026, 11, 8)
	f := 1.5
	got := yearly(base, start, []float64{f, f})
	for _, c := range []struct {
		on   model.Date
		rate float64
	}{
		{d(2026, 12, 31), 1.05*f - 1},
		{d(2027, 1, 1), f - 1},
		{d(2027, 5, 31), f - 1},
		{d(2027, 6, 1), 0.6*f - 1},
		{d(2027, 11, 7), 0.6*f - 1},
		{d(2027, 11, 8), 0.6*f - 1},
		{d(2027, 12, 31), 0.6*f - 1},
		{d(2028, 1, 1), f - 1},
		{d(2028, 11, 7), f - 1},
	} {
		if r, ok := got.At(c.on); !ok || math.Abs(r-c.rate) > 1e-12 {
			t.Errorf("rate on %v = %v, %v; want %v", c.on, r, ok, c.rate)
		}
	}
	// Zero volatility leaves each rate as base's, with gaps as zero.
	one := yearly(base, start, []float64{1, 1})
	for _, on := range []model.Date{d(2026, 12, 31), d(2027, 6, 1), d(2027, 11, 7)} {
		w, _ := base.At(on)
		if r, ok := one.At(on); !ok || math.Abs(r-w) > 1e-12 {
			t.Errorf("unit factor changed the rate on %v: %v, want %v", on, r, w)
		}
	}
	// Touching segments lose their Ends; each still governs only until the next begins.
	crash := model.Constant(0.08).Overlay(model.MustSchedule(
		model.Segment{From: d(2027, 6, 1), End: model.Ptr(d(2028, 5, 31)), Value: -0.4},
	))
	steps := model.MustSchedule(
		model.Segment{From: model.MinDate, End: model.Ptr(d(2028, 12, 31)), Value: 0.04},
		model.Segment{From: d(2029, 1, 1), End: model.Ptr(d(2034, 12, 31)), Value: 0.06},
		model.Segment{From: d(2035, 1, 1), Value: 0.02},
	)
	for _, b := range []struct {
		name  string
		base  model.Schedule
		cases []model.Date
	}{
		{"crash", crash, []model.Date{d(2026, 12, 31), d(2027, 5, 31), d(2027, 6, 1), d(2028, 5, 31), d(2028, 6, 1), d(2029, 3, 1)}},
		{"steps", steps, []model.Date{d(2027, 1, 1), d(2028, 12, 31), d(2029, 1, 1), d(2034, 12, 31), d(2035, 1, 1), d(2036, 1, 1)}},
	} {
		fs := make([]float64, 12)
		for i := range fs {
			fs[i] = f
		}
		ones := make([]float64, len(fs))
		for i := range ones {
			ones[i] = 1
		}
		scaled, unit := yearly(b.base, start, fs), yearly(b.base, start, ones)
		for _, on := range b.cases {
			w, _ := b.base.At(on)
			if r, ok := scaled.At(on); !ok || math.Abs(r-((1+w)*f-1)) > 1e-12 {
				t.Errorf("%s: rate on %v = %v, %v; want %v", b.name, on, r, ok, (1+w)*f-1)
			}
			if r, ok := unit.At(on); !ok || math.Abs(r-w) > 1e-12 {
				t.Errorf("%s: unit factor changed the rate on %v: %v, want %v", b.name, on, r, w)
			}
		}
	}
}
