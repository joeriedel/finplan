// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph_test

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

const (
	plotW, plotH = 1600, 700
	spacing      = 7
)

// frame is one headless draw of the "Early withdrawal cost" band: the sampling grid for the view, the two 401k
// series on it, and the band's pixel geometry, mapped linearly to the plot the way the app does.
type frame struct {
	grid         graph.Grid
	upper, lower []graph.Pt
}

type runner struct {
	rec         *model.Record
	engineCalls int
	cache       map[graph.Grid][2][]engine.Sample
	yMax        float64
}

// newRunner fits y to the "Early withdrawal cost" preset's starting range, as loading the preset does. Panning
// and zooming then leave y alone, so the 401k balances can run far above the top of the plot.
func newRunner(t testing.TB) *runner {
	r := &runner{rec: demodata.Family(), cache: map[graph.Grid][2][]engine.Sample{}}
	from, to := demodata.FamilyToday.Add(-365), demodata.FamilyToday.Add(20*365)
	ser := r.series(t, graph.SampleGrid(from, to, plotW))
	r.yMax = 1
	for _, s := range ser[0] {
		if s.Date >= from && s.Date <= to {
			r.yMax = math.Max(r.yMax, s.Amount)
		}
	}
	r.yMax *= 1.1
	return r
}

// series returns the two 401k series on the grid, running the engine only for grids not seen before, as the
// app's cache does.
func (r *runner) series(t testing.TB, grid graph.Grid) [2][]engine.Sample {
	ser, ok := r.cache[grid]
	if !ok {
		asset := model.DefaultAssetID(demodata.K401)
		for i, proj := range []string{demodata.ProjBase, demodata.ProjWithdraw} {
			res, err := engine.Balances(r.rec, proj, asset, grid.Times())
			if err != nil {
				t.Fatal(err)
			}
			ser[i] = res.Samples
			r.engineCalls++
		}
		r.cache[grid] = ser
	}
	return ser
}

// frame draws the view from..to.
func (r *runner) frame(t testing.TB, from, to model.Date) frame {
	grid := graph.SampleGrid(from, to, plotW)
	ser := r.series(t, grid)
	px := func(ss []engine.Sample) []graph.Pt {
		out := make([]graph.Pt, len(ss))
		for i, s := range ss {
			out[i] = graph.Pt{
				X: float32(float64(s.Date-from) / float64(to-from) * plotW),
				Y: float32((r.yMax - s.Amount) / r.yMax * plotH),
			}
		}
		return out
	}
	return frame{grid, px(ser[0]), px(ser[1])}
}

func (f frame) hatch() []graph.Seg {
	return graph.HatchStripes(f.upper, f.lower, spacing, graph.Pt{}, graph.Pt{X: plotW, Y: plotH})
}

func farViews() [][2]model.Date {
	today := demodata.FamilyToday
	return [][2]model.Date{
		{model.D(1990, time.January, 1), model.D(2090, time.January, 1)},
		{model.D(2010, time.January, 1), model.D(2095, time.January, 1)},
		{model.D(2040, time.January, 1), model.D(2105, time.January, 1)},
		{today, model.D(2150, time.January, 1)},
		{model.D(2050, time.January, 1), model.D(2190, time.January, 1)},
	}
}

func TestHatchBoundedByPlot(t *testing.T) {
	r := newRunner(t)
	maxSegs := 2 * (plotW + plotH) / spacing
	// Work is the visible strips, the stripes, and the edges' travel across the stripes: a few times the
	// stripes, where strips times the plot's height over the spacing would be tens of thousands.
	maxWork := plotW/graph.PixelsPerSample + 10*(plotW+plotH)/spacing
	for _, v := range farViews() {
		f := r.frame(t, v[0], v[1])
		segs, work := graph.HatchWork(f.upper, f.lower, spacing, graph.Pt{}, graph.Pt{X: plotW, Y: plotH})
		t.Logf("view %d..%d step=%d samples=%d segments=%d work=%d polys=%d", v[0].Time().Year(), v[1].Time().Year(), f.grid.Step, f.grid.Count(), len(segs), work, len(graph.BandPolys(f.upper, f.lower)))
		if ref := graph.ReferenceHatch(f.upper, f.lower, spacing, graph.Pt{}, graph.Pt{X: plotW, Y: plotH}); len(ref) != len(segs) {
			t.Errorf("view %v..%v: %d segments, reference has %d", v[0], v[1], len(segs), len(ref))
		}
		if work > maxWork {
			t.Errorf("view %v..%v: hatch work %d, want at most %d", v[0], v[1], work, maxWork)
		}
		if len(segs) > maxSegs {
			t.Errorf("view %v..%v: %d hatch segments, want at most %d for a %dx%d plot", v[0], v[1], len(segs), maxSegs, plotW, plotH)
		}
	}
}

// TestHatchBoundedForTallBand uses a band that spans the plot's height at every sample, the worst case for work
// that grows with the band's thickness in each strip.
func TestHatchBoundedForTallBand(t *testing.T) {
	var upper, lower []graph.Pt
	for x := float32(-400); x <= plotW+400; x += graph.PixelsPerSample {
		upper = append(upper, graph.Pt{X: x, Y: -2000})
		lower = append(lower, graph.Pt{X: x, Y: plotH + 2000})
	}
	segs := graph.HatchStripes(upper, lower, spacing, graph.Pt{}, graph.Pt{X: plotW, Y: plotH})
	t.Logf("%d samples, %d segments", len(upper), len(segs))
	if maxSegs := 2 * (plotW + plotH) / spacing; len(segs) > maxSegs {
		t.Errorf("%d hatch segments, want at most %d for a %dx%d plot", len(segs), maxSegs, plotW, plotH)
	}
}

func TestPanningRarelyChangesGrid(t *testing.T) {
	r := newRunner(t)
	from, to := model.D(2020, time.January, 1), model.D(2090, time.January, 1)
	frames := 0
	grids := map[graph.Grid]bool{}
	for shift := 0; shift < 365*5; shift += 3 {
		f := r.frame(t, from.Add(shift), to.Add(shift))
		grids[f.grid] = true
		frames++
	}
	t.Logf("%d frames, %d grids, %d engine calls", frames, len(grids), r.engineCalls)
	if len(grids) > frames/20 {
		t.Errorf("%d distinct grids over %d panning frames, want at most %d", len(grids), frames, frames/20)
	}
}

func BenchmarkHatchFar(b *testing.B) {
	r := newRunner(b)
	v := farViews()[1]
	f := r.frame(b, v[0], v[1])
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.hatch()
	}
}

func BenchmarkReferenceHatchFar(b *testing.B) {
	r := newRunner(b)
	v := farViews()[1]
	f := r.frame(b, v[0], v[1])
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		graph.ReferenceHatch(f.upper, f.lower, spacing, graph.Pt{}, graph.Pt{X: plotW, Y: plotH})
	}
}

func BenchmarkHatchTallBand(b *testing.B) {
	var upper, lower []graph.Pt
	for x := float32(-400); x <= plotW+400; x += graph.PixelsPerSample {
		upper = append(upper, graph.Pt{X: x, Y: -2000})
		lower = append(lower, graph.Pt{X: x, Y: plotH + 2000})
	}
	for i := 0; i < b.N; i++ {
		graph.HatchStripes(upper, lower, spacing, graph.Pt{}, graph.Pt{X: plotW, Y: plotH})
	}
}
