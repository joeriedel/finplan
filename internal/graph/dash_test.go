// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph_test

import (
	"math"
	"testing"

	"github.com/joeriedel/finplan/internal/graph"
)

// restrict cuts the segments to the rectangle, dropping what lies outside.
func restrict(segs []graph.Seg, lo, hi graph.Pt) []graph.Seg {
	var out []graph.Seg
	for _, s := range segs {
		t0, t1 := 0.0, 1.0
		d := [2]float64{float64(s.B.X - s.A.X), float64(s.B.Y - s.A.Y)}
		a := [2]float64{float64(s.A.X), float64(s.A.Y)}
		l := [2]float64{float64(lo.X), float64(lo.Y)}
		h := [2]float64{float64(hi.X), float64(hi.Y)}
		ok := true
		for k := 0; k < 2; k++ {
			if d[k] == 0 {
				ok = ok && a[k] >= l[k] && a[k] <= h[k]
				continue
			}
			u, v := (l[k]-a[k])/d[k], (h[k]-a[k])/d[k]
			t0, t1 = math.Max(t0, math.Min(u, v)), math.Min(t1, math.Max(u, v))
		}
		if !ok || t1-t0 < 1e-9 {
			continue
		}
		at := func(t float64) graph.Pt {
			return graph.Pt{X: float32(a[0] + d[0]*t), Y: float32(a[1] + d[1]*t)}
		}
		out = append(out, graph.Seg{A: at(t0), B: at(t1)})
	}
	return out
}

// farLine is a polyline that crosses the plot and runs 3e5 pixels above it, with a corner inside the plot.
func farLine() []graph.Pt {
	return []graph.Pt{{X: -50, Y: 900}, {X: 400, Y: 300}, {X: 900, Y: -3e5}, {X: 1700, Y: 200}, {X: 1900, Y: 3e5}}
}

func TestDashClippedMatchesDash(t *testing.T) {
	lo, hi := graph.Pt{X: 0, Y: 0}, graph.Pt{X: plotW, Y: plotH}
	pts := farLine()[:2]
	pts = append(pts, graph.Pt{X: 500, Y: 100}, graph.Pt{X: 1500, Y: 650}, graph.Pt{X: 1700, Y: -20})
	want := restrict(graph.Dash(pts, 7, 5), lo, hi)
	got := restrict(graph.DashClipped(pts, 7, 5, lo, hi), lo, hi)
	if len(got) != len(want) {
		t.Fatalf("%d dashes, want %d", len(got), len(want))
	}
	for i := range got {
		for _, d := range []float32{got[i].A.X - want[i].A.X, got[i].A.Y - want[i].A.Y, got[i].B.X - want[i].B.X, got[i].B.Y - want[i].B.Y} {
			if math.Abs(float64(d)) > 1e-2 {
				t.Fatalf("dash %d is %v, want %v", i, got[i], want[i])
			}
		}
	}
	if len(got) < 100 {
		t.Errorf("only %d dashes compared", len(got))
	}
}

func TestDashClippedBoundedByRect(t *testing.T) {
	lo, hi := graph.Pt{X: 0, Y: 0}, graph.Pt{X: plotW, Y: plotH}
	got := graph.DashClipped(farLine(), 7, 5, lo, hi)
	// The line's length inside the rectangle is under its perimeter's worth per crossing; allow a few.
	const visible = 4 * (plotW + plotH)
	if limit := visible/(7+5) + 100; len(got) > limit {
		t.Errorf("%d dashes, want at most %d", len(got), limit)
	}
	if all := len(graph.Dash(farLine(), 7, 5)); all < 50000 {
		t.Errorf("the unclipped line has only %d dashes; the test line should run far off the plot", all)
	}
}

func TestDashClippedFarViews(t *testing.T) {
	r := newRunner(t)
	lo, hi := graph.Pt{X: 0, Y: 0}, graph.Pt{X: plotW, Y: plotH}
	for _, v := range farViews() {
		f := r.frame(t, v[0], v[1])
		for _, line := range [][]graph.Pt{f.upper, f.lower} {
			n := len(graph.DashClipped(line, 7, 5, lo, hi))
			// A polyline visible in the plot is at most a few plot-diagonals long there.
			if limit := 4 * (plotW + plotH) / 12; n > limit {
				t.Errorf("view %v..%v: %d dashes, want at most %d", v[0], v[1], n, limit)
			}
		}
	}
}

func TestDashLongSegmentTerminates(t *testing.T) {
	got := graph.DashClipped([]graph.Pt{{X: 0, Y: 100}, {X: 1e9, Y: 100}}, 7, 5, graph.Pt{}, graph.Pt{X: 1600, Y: 700})
	if want := 1600/12 + 1; len(got) < want-1 || len(got) > want+1 {
		t.Errorf("%d dashes, want about %d", len(got), want)
	}
}
