// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import "math"

// Pt is a point in pixels.
type Pt struct{ X, Y float32 }

// Seg is a line segment in pixels.
type Seg struct{ A, B Pt }

// Dash cuts the polyline into dashes of the given pixel length separated by gaps, with the pattern running
// on across the polyline's corners.
func Dash(pts []Pt, dash, gap float32) []Seg {
	return dashPolyline(pts, dash, gap, nil)
}

// DashClipped is Dash keeping only the parts of the dashes inside the rectangle from lo to hi. The pattern runs
// on through the parts outside as if they were drawn, so the dashes inside are where Dash puts them, and the
// work is bounded by the rectangle's size rather than the polyline's length.
func DashClipped(pts []Pt, dash, gap float32, lo, hi Pt) []Seg {
	return dashPolyline(pts, dash, gap, &[2]Pt{lo, hi})
}

// clipParam returns the part [t0, t1] of the segment a to b, as fractions of its length, that lies inside the
// rectangle; t0 > t1 if none does.
func clipParam(a, b, lo, hi Pt) (float64, float64) {
	t0, t1 := 0.0, 1.0
	for _, e := range [4][2]float64{
		{float64(a.X) - float64(lo.X), float64(b.X) - float64(a.X)},
		{float64(hi.X) - float64(a.X), float64(a.X) - float64(b.X)},
		{float64(a.Y) - float64(lo.Y), float64(b.Y) - float64(a.Y)},
		{float64(hi.Y) - float64(a.Y), float64(a.Y) - float64(b.Y)},
	} {
		// Inside where e[0] + e[1]*t >= 0.
		switch {
		case e[1] == 0:
			if e[0] < 0 {
				return 1, 0
			}
		case e[1] > 0:
			t0 = math.Max(t0, -e[0]/e[1])
		default:
			t1 = math.Min(t1, -e[0]/e[1])
		}
	}
	return t0, t1
}

// dashPolyline walks the pattern as a phase in [0, dash+gap): a dash while below dash, else a gap. Positions are
// float64 so that they keep advancing along very long segments.
func dashPolyline(pts []Pt, dash, gap float32, clip *[2]Pt) []Seg {
	if dash <= 0 || gap < 0 {
		return nil
	}
	d, period := float64(dash), float64(dash)+float64(gap)
	var out []Seg
	phase := 0.0
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		length := math.Hypot(float64(b.X-a.X), float64(b.Y-a.Y))
		if length == 0 {
			continue
		}
		t0, t1 := 0.0, 1.0
		if clip != nil {
			t0, t1 = clipParam(a, b, clip[0], clip[1])
		}
		if t0 > t1 {
			phase = math.Mod(phase+length, period)
			continue
		}
		pos, end := t0*length, t1*length
		phase = math.Mod(phase+pos, period)
		at := func(pos float64) Pt {
			t := pos / length
			return Pt{float32(float64(a.X) + float64(b.X-a.X)*t), float32(float64(a.Y) + float64(b.Y-a.Y)*t)}
		}
		for pos < end {
			boundary := period
			if phase < d {
				boundary = d
			}
			step := math.Min(boundary-phase, end-pos)
			if phase < d {
				out = append(out, Seg{at(pos), at(pos + step)})
			}
			pos += step
			if step == boundary-phase {
				phase = boundary
			} else {
				phase += step
			}
			if phase >= period {
				phase = 0
			}
		}
		phase = math.Mod(phase+length-end, period)
	}
	return out
}

// Distance returns the distance from p to the polyline; it is +Inf for an empty polyline.
func Distance(p Pt, poly []Pt) float32 {
	best := math.Inf(1)
	if len(poly) == 1 {
		return float32(math.Hypot(float64(p.X-poly[0].X), float64(p.Y-poly[0].Y)))
	}
	for i := 1; i < len(poly); i++ {
		a, b := poly[i-1], poly[i]
		abx, aby := float64(b.X-a.X), float64(b.Y-a.Y)
		t := 0.0
		if l2 := abx*abx + aby*aby; l2 > 0 {
			t = max(0, min(1, (float64(p.X-a.X)*abx+float64(p.Y-a.Y)*aby)/l2))
		}
		best = min(best, math.Hypot(float64(p.X-a.X)-t*abx, float64(p.Y-a.Y)-t*aby))
	}
	return float32(best)
}
