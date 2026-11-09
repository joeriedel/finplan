// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"math"
	"sort"

	"github.com/joeriedel/finplan/internal/model"
)

// BandID identifies a band for as long as the list lives. Ids are never reused.
type BandID int

// Band is the area between two lines. It follows the lines' effective projections, so none is stored.
type Band struct {
	ID           BandID
	Upper, Lower LineID
	Color        Color
	Hatch        bool
	TodayDollars bool
	Visible      bool
}

func (l *Lines) bandIndex(id BandID) int {
	for i, b := range l.bands {
		if b.ID == id {
			return i
		}
	}
	return -1
}

// AddBand appends a visible band between two lines, unhatched and in nominal dollars, and returns its id.
// It returns 0 and adds nothing if either line is unknown or both are the same line.
func (l *Lines) AddBand(upper, lower LineID, c Color) BandID {
	if upper == lower || l.index(upper) < 0 || l.index(lower) < 0 {
		return 0
	}
	b := Band{ID: l.nextBand, Upper: upper, Lower: lower, Color: c, Visible: true}
	l.nextBand++
	l.bands = append(l.bands, b)
	return b.ID
}

// RemoveBand deletes the band.
func (l *Lines) RemoveBand(id BandID) {
	if i := l.bandIndex(id); i >= 0 {
		l.bands = append(l.bands[:i], l.bands[i+1:]...)
	}
}

// Bands returns a copy of the bands in the order they were added.
func (l *Lines) Bands() []Band { return append([]Band(nil), l.bands...) }

// GetBand returns the band with the id.
func (l *Lines) GetBand(id BandID) (Band, bool) {
	if i := l.bandIndex(id); i >= 0 {
		return l.bands[i], true
	}
	return Band{}, false
}

// SetBandColor recolours the band.
func (l *Lines) SetBandColor(id BandID, c Color) {
	if i := l.bandIndex(id); i >= 0 {
		l.bands[i].Color = c
	}
}

// SetBandVisible shows or hides the band.
func (l *Lines) SetBandVisible(id BandID, v bool) {
	if i := l.bandIndex(id); i >= 0 {
		l.bands[i].Visible = v
	}
}

// SetBandHatch turns the band's diagonal stripes on or off.
func (l *Lines) SetBandHatch(id BandID, v bool) {
	if i := l.bandIndex(id); i >= 0 {
		l.bands[i].Hatch = v
	}
}

// SetBandTodayDollars turns the band's gap in today's dollars on or off.
func (l *Lines) SetBandTodayDollars(id BandID, v bool) {
	if i := l.bandIndex(id); i >= 0 {
		l.bands[i].TodayDollars = v
	}
}

// BandsUsing returns the ids of the bands that have the line as either edge.
func (l *Lines) BandsUsing(id LineID) []BandID {
	var out []BandID
	for _, b := range l.bands {
		if b.Upper == id || b.Lower == id {
			out = append(out, b.ID)
		}
	}
	return out
}

// BandDrawn reports whether the band is drawn: it is visible and both its lines are drawn, so solo and line
// visibility apply to it.
func (l *Lines) BandDrawn(id BandID) bool {
	i := l.bandIndex(id)
	if i < 0 {
		return false
	}
	b := l.bands[i]
	return b.Visible && l.Drawn(b.Upper) && l.Drawn(b.Lower)
}

// BandName names a band from its edges' asset names and effective projections: "Asset: Upper − Lower" when both
// edges are the same asset, else "Asset (Upper) − Asset (Lower)".
func BandName(upperAsset, upperProj, lowerAsset, lowerProj string) string {
	if upperAsset == lowerAsset {
		return upperAsset + ": " + upperProj + " − " + lowerProj
	}
	return upperAsset + " (" + upperProj + ") − " + lowerAsset + " (" + lowerProj + ")"
}

// Gap returns how far the upper value is above the lower.
func Gap(upper, lower float64) float64 { return upper - lower }

// ToTodayDollars converts an amount in the dollars of date d to today's dollars, deflating by the inflation
// schedule for a later date and inflating for an earlier one.
func ToTodayDollars(v float64, d, today model.Date, inflation model.Schedule) float64 {
	if d < today {
		return v * inflation.Compound(d, today)
	}
	return v / inflation.Compound(today, d)
}

// bandStrip is the part of a band between two consecutive samples: x from x0 to x1, with the band's upper
// (smaller y) edge at t0, t1 and lower edge at b0, b1. Crossings split strips so that t <= b always holds.
type bandStrip struct{ x0, x1, t0, t1, b0, b1 float64 }

// bandStrips cuts the region between two pixel polylines sampled at the same x positions into strips, splitting
// at the points where the edges cross. The x positions are taken from a.
func bandStrips(a, b []Pt) []bandStrip {
	var out []bandStrip
	for i := 1; i < min(len(a), len(b)); i++ {
		x0, x1 := float64(a[i-1].X), float64(a[i].X)
		a0, a1, b0, b1 := float64(a[i-1].Y), float64(a[i].Y), float64(b[i-1].Y), float64(b[i].Y)
		if !(x1 > x0) {
			continue
		}
		d0, d1 := a0-b0, a1-b1
		if d0*d1 < 0 {
			t := d0 / (d0 - d1)
			xc, yc := x0+(x1-x0)*t, a0+(a1-a0)*t
			out = append(out,
				bandStrip{x0, xc, min(a0, b0), yc, max(a0, b0), yc},
				bandStrip{xc, x1, yc, min(a1, b1), yc, max(a1, b1)})
			continue
		}
		out = append(out, bandStrip{x0, x1, min(a0, b0), min(a1, b1), max(a0, b0), max(a1, b1)})
	}
	return out
}

// BandPolys returns convex polygons that together fill the region between two pixel polylines sampled at the
// same x positions, split where the edges cross so that none self-intersects.
func BandPolys(a, b []Pt) [][]Pt {
	var out [][]Pt
	for _, s := range bandStrips(a, b) {
		var poly []Pt
		for _, p := range []Pt{{float32(s.x0), float32(s.t0)}, {float32(s.x1), float32(s.t1)}, {float32(s.x1), float32(s.b1)}, {float32(s.x0), float32(s.b0)}} {
			if len(poly) == 0 || poly[len(poly)-1] != p {
				poly = append(poly, p)
			}
		}
		if len(poly) > 1 && poly[0] == poly[len(poly)-1] {
			poly = poly[:len(poly)-1]
		}
		if len(poly) >= 3 {
			out = append(out, poly)
		}
	}
	return out
}

// clipGE narrows [lo, hi] to where the line g0 + (g1-g0)t is not negative.
func clipGE(g0, g1, lo, hi float64) (float64, float64) {
	switch {
	case g0 >= 0 && g1 >= 0:
	case g0 < 0 && g1 < 0:
		return 1, 0
	case g0 >= 0:
		hi = min(hi, g0/(g0-g1))
	default:
		lo = max(lo, g0/(g0-g1))
	}
	return lo, hi
}

// HatchStripes returns the 45 degree stripes (running up to the right, one every spacing pixels along each axis)
// that lie between two pixel polylines sampled at the same x positions. Stripes are aligned to a fixed grid, so
// they stay put as the band's edges change. Only the parts inside the rectangle from lo to hi are returned, and
// only the samples that can reach it are read. A stripe's pieces in neighbouring strips are joined, so the result
// has one segment per stripe and contiguous run inside the band, however many samples the edges have.
func HatchStripes(a, b []Pt, spacing float32, lo, hi Pt) []Seg {
	segs, _ := hatchStripes(a, b, spacing, lo, hi)
	return segs
}

// hatchStripes is HatchStripes, also returning a count of the loop iterations it ran, for tests of its cost.
//
// The sweep goes strip by strip. A stripe can only enter or leave the band where its line x + y = c meets an
// edge, and within a strip that is only for c between the edge's values of x + y at the strip's two ends. Only
// those stripes are examined; every other stripe keeps the state it had, so a stripe running through many strips
// costs nothing until it ends. The work is the visible strips, plus the stripes, plus how far the edges move
// across the stripes inside the rectangle.
func hatchStripes(a, b []Pt, spacing float32, lo, hi Pt) ([]Seg, int) {
	if !(spacing > 0) {
		return nil, 0
	}
	sp := float64(spacing)
	minX, maxX, minY, maxY := float64(lo.X), float64(hi.X), float64(lo.Y), float64(hi.Y)
	base := int(math.Ceil((minX + minY) / sp))
	n := int(math.Floor((maxX+maxY)/sp)) - base + 1
	if n <= 0 {
		return nil, 0
	}
	// The samples from the last one left of the rectangle to the first one right of it.
	m := min(len(a), len(b))
	i0 := sort.Search(m, func(i int) bool { return float64(a[i].X) >= minX })
	i0 = max(i0-1, 0)
	i1 := min(sort.Search(m, func(i int) bool { return float64(a[i].X) > maxX })+1, m)
	if i1-i0 < 2 {
		return nil, 0
	}
	strips := bandStrips(a[i0:i1], b[i0:i1])
	if len(strips) == 0 {
		return nil, 0
	}
	work := len(strips) + 2*n

	// open[k] says stripe base+k is inside the band, and from[k] is where its current run began.
	open := make([]bool, n)
	from := make([]float64, n)
	var out []Seg
	emit := func(k int, xa, xb float64) {
		c := float64(base+k) * sp
		xa, xb = math.Max(xa, math.Max(minX, c-maxY)), math.Min(xb, math.Min(maxX, c-minY))
		if xb-xa >= 1e-4 {
			out = append(out, Seg{Pt{float32(xa), float32(c - xa)}, Pt{float32(xb), float32(c - xb)}})
		}
	}
	// span is the stripes with c in [cLo, cHi], clamped to the rectangle's stripes.
	span := func(cLo, cHi float64) (int, int) {
		return max(int(math.Ceil((cLo-1e-6)/sp))-base, 0), min(int(math.Floor((cHi+1e-6)/sp))-base, n-1)
	}
	visit := func(s bandStrip, k int) {
		c := float64(base+k) * sp
		tLo, tHi := clipGE(c-s.x0-s.t0, c-s.x1-s.t1, 0, 1)
		if tLo <= tHi {
			tLo, tHi = clipGE(s.b0-(c-s.x0), s.b1-(c-s.x1), tLo, tHi)
		}
		if tLo > tHi {
			if open[k] {
				emit(k, from[k], s.x0)
				open[k] = false
			}
			return
		}
		dx := s.x1 - s.x0
		xa, xb := s.x0+dx*tLo, s.x0+dx*tHi
		if open[k] && xa > s.x0+1e-3 {
			emit(k, from[k], s.x0)
			open[k] = false
		}
		if !open[k] {
			open[k], from[k] = true, xa
		}
		// The stripe stays open past the strip if it is still between the edges at x1, to within the slack that
		// span uses, so that the next strip's span is certain to visit it.
		if c < s.x1+s.t1-1e-6 || c > s.x1+s.b1+1e-6 {
			emit(k, from[k], xb)
			open[k] = false
		}
	}
	for i, s := range strips {
		if i == 0 {
			for k := range n {
				visit(s, k)
			}
			continue
		}
		t0, t1 := span(math.Min(s.x0+s.t0, s.x1+s.t1), math.Max(s.x0+s.t0, s.x1+s.t1))
		b0, b1 := span(math.Min(s.x0+s.b0, s.x1+s.b1), math.Max(s.x0+s.b0, s.x1+s.b1))
		switch {
		case t0 > t1:
			t0, t1, b0, b1 = b0, b1, 1, 0
		case b0 > b1:
			b0, b1 = 1, 0
		case b0 < t0:
			t0, t1, b0, b1 = b0, b1, t0, t1
		}
		if t0 <= t1 && b0 <= t1+1 {
			t1, b0, b1 = max(t1, b1), 1, 0
		}
		for k := t0; k <= t1; k++ {
			visit(s, k)
			work++
		}
		for k := b0; k <= b1; k++ {
			visit(s, k)
			work++
		}
	}
	end := strips[len(strips)-1].x1
	for k := range n {
		if open[k] {
			emit(k, from[k], end)
		}
	}
	return out, work
}
