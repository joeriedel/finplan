// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"math"
	"math/rand"
	"slices"
	"testing"
)

// referenceHatch clips every stripe against every visible strip and joins the pieces of a stripe that touch. It
// does work proportional to strips times the band's height over the spacing, and defines what HatchStripes returns.
func referenceHatch(a, b []Pt, spacing float32, lo, hi Pt) []Seg {
	if !(spacing > 0) {
		return nil
	}
	sp := float64(spacing)
	var out []Seg
	minX, maxX, minY, maxY := float64(lo.X), float64(hi.X), float64(lo.Y), float64(hi.Y)
	base := int(math.Ceil((minX + minY) / sp))
	n := int(math.Floor((maxX+maxY)/sp)) - base + 1
	if n <= 0 {
		return nil
	}
	// open[i] is the index in out of the latest segment of stripe base+i.
	open := make([]int, n)
	for i := range open {
		open[i] = -1
	}
	for _, s := range bandStrips(a, b) {
		if s.x1 < minX || s.x0 > maxX {
			continue
		}
		// A stripe is x + y = c; its y is c - x. Within the strip's visible x span, only c in
		// [vx0+minY, vx1+maxY] can reach the rectangle.
		vx0, vx1 := math.Max(s.x0, minX), math.Min(s.x1, maxX)
		cLo := math.Max(math.Min(s.x0+s.t0, s.x1+s.t1), vx0+minY)
		cHi := math.Min(math.Max(s.x0+s.b0, s.x1+s.b1), vx1+maxY)
		for c := math.Ceil(cLo/sp) * sp; c <= cHi; c += sp {
			dx := s.x1 - s.x0
			tLo, tHi := clipGE(c-s.x0-s.t0, c-s.x1-s.t1, 0, 1)
			if tLo > tHi {
				continue
			}
			tLo, tHi = clipGE(s.b0-(c-s.x0), s.b1-(c-s.x1), tLo, tHi)
			if tLo > tHi {
				continue
			}
			xa, xb := s.x0+dx*tLo, s.x0+dx*tHi
			xa, xb = math.Max(xa, math.Max(minX, c-maxY)), math.Min(xb, math.Min(maxX, c-minY))
			if xb-xa < 1e-4 {
				continue
			}
			k := int(math.Round(c/sp)) - base
			if j := open[k]; j >= 0 && float64(out[j].B.X) >= xa-1e-3 {
				out[j].B = Pt{float32(xb), float32(c - xb)}
				continue
			}
			open[k] = len(out)
			out = append(out, Seg{Pt{float32(xa), float32(c - xa)}, Pt{float32(xb), float32(c - xb)}})
		}
	}
	return out
}

func sortedSegs(s []Seg) []Seg {
	s = slices.Clone(s)
	slices.SortFunc(s, func(p, q Seg) int {
		if p.A.X != q.A.X {
			return int(math.Copysign(1, float64(p.A.X-q.A.X)))
		}
		return int(math.Copysign(1, float64(p.A.Y-q.A.Y)))
	})
	return s
}

func sameSegs(t *testing.T, name string, got, want []Seg) {
	t.Helper()
	got, want = sortedSegs(got), sortedSegs(want)
	if len(got) != len(want) {
		t.Errorf("%s: %d segments, want %d", name, len(got), len(want))
		return
	}
	near := func(p, q Pt) bool { return math.Abs(float64(p.X-q.X)) < 1e-2 && math.Abs(float64(p.Y-q.Y)) < 1e-2 }
	for i := range got {
		if !near(got[i].A, want[i].A) || !near(got[i].B, want[i].B) {
			t.Errorf("%s: segment %d is %v, want %v", name, i, got[i], want[i])
			return
		}
	}
}

func TestHatchMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	curve := func(n int, x0, step, amp, y0 float64) []Pt {
		out := make([]Pt, n)
		y := y0
		for i := range out {
			y += (rng.Float64() - 0.5) * amp
			out[i] = Pt{float32(x0 + float64(i)*step), float32(y)}
		}
		return out
	}
	lo, hi := Pt{0, 0}, Pt{300, 200}
	cases := []struct {
		name string
		a, b []Pt
	}{
		{"parallel", curve(80, -20, 4, 0, 50), curve(80, -20, 4, 0, 120)},
		{"crossing", curve(80, -20, 4, 30, 100), curve(80, -20, 4, 30, 100)},
		{"gaps", curve(120, 0, 3, 40, 100), curve(120, 0, 3, 40, 100)},
		{"partly off rect", curve(80, 200, 4, 10, 60), curve(80, 200, 4, 10, 220)},
		{"left of rect", curve(40, -300, 4, 10, 60), curve(40, -300, 4, 10, 120)},
		{"narrow", curve(80, -20, 4, 2, 100), curve(80, -20, 4, 2, 103)},
		{"tall", curve(80, -20, 4, 5, -500), curve(80, -20, 4, 5, 900)},
		{"steep", curve(80, -20, 4, 300, 100), curve(80, -20, 4, 300, 100)},
	}
	for _, c := range cases {
		for _, sp := range []float32{2, 7, 13} {
			got, _ := hatchStripes(c.a, c.b, sp, lo, hi)
			sameSegs(t, c.name, got, referenceHatch(c.a, c.b, sp, lo, hi))
		}
	}
}

// TestHatchStripeLeavingNearSample covers stripes whose run ends a hair before a sample, where a stripe must not
// be left open past the strip it ends in.
func TestHatchStripeLeavingNearSample(t *testing.T) {
	lo, hi := Pt{0, 0}, Pt{100, 100}
	for _, off := range []float32{5, 5.0001, 5.0005, 5.0015, 5.003, 5.01, 4.9995, 4.9985, 4.99} {
		a := []Pt{{0, off}, {10, off + 20}, {20, off + 40}}
		b := []Pt{{0, 90}, {10, 90}, {20, 90}}
		for _, sp := range []float32{7, 5, 3} {
			got, _ := hatchStripes(a, b, sp, lo, hi)
			sameSegs(t, "near-sample exit", got, referenceHatch(a, b, sp, lo, hi))
		}
	}
	// The same with the slope steeper, the run ending within the tolerance of the next sample from the other side.
	for _, off := range []float32{0.0005, 0.0015, 0.003} {
		a := []Pt{{0, 10}, {10, 10}, {20, 10}, {30, 10}}
		b := []Pt{{0, 20}, {10, 20 + off}, {20, 20 - off}, {30, 20}}
		got, _ := hatchStripes(a, b, 7, lo, hi)
		sameSegs(t, "wobbling bottom", got, referenceHatch(a, b, 7, lo, hi))
	}
}

// TestHatchWorkBoundedByPlot checks that the work does not grow with the strips a band is cut into or with how far
// its edges reach beyond the rectangle: the reference's work would be about 70 times the bound here.
func TestHatchWorkBoundedByPlot(t *testing.T) {
	const w, h, sp = 1600, 700, 7
	var up, low []Pt
	for x := float32(-4000); x <= w+4000; x += 4 {
		up = append(up, Pt{x, -2000})
		low = append(low, Pt{x, h + 2000})
	}
	segs, work := hatchStripes(up, low, sp, Pt{}, Pt{w, h})
	stripes := (w + h) / sp
	if bound := 2*w/4 + 6*stripes; work > bound {
		t.Errorf("work %d for a tall band, want at most %d", work, bound)
	}
	if len(segs) > stripes+1 {
		t.Errorf("%d segments, want at most %d", len(segs), stripes+1)
	}
	t.Logf("tall band: %d samples, work %d, %d segments", len(up), work, len(segs))
}
