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
	if dash <= 0 || gap < 0 {
		return nil
	}
	var out []Seg
	on, left := true, dash
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
		length := float32(math.Hypot(dx, dy))
		if length == 0 {
			continue
		}
		at := func(pos float32) Pt {
			t := pos / length
			return Pt{a.X + (b.X-a.X)*t, a.Y + (b.Y-a.Y)*t}
		}
		for pos := float32(0); pos < length; {
			step := min(left, length-pos)
			if on {
				out = append(out, Seg{at(pos), at(pos + step)})
			}
			pos += step
			left -= step
			if left <= 0 {
				on = !on
				left = gap
				if on {
					left = dash
				}
			}
		}
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
