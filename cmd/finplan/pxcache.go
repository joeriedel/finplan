// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
)

// pixelCache holds the pixel geometry of the Graph's lines, bands and Monte Carlo bands, made again only when the plot's geometry or the data it was made from changes, so that a frame with the view still (the pointer moving over the plot, say) does none of that work, and one with it moving reuses the buffers.
type pixelCache struct {
	lines map[graph.LineID]*linePx
	mc    map[graph.LineID]*mcPx
	bands map[graph.BandID]*bandPx
}

// linePx is a line's samples in pixels.
type linePx struct {
	geom plotGeom
	src  []engine.Sample
	pts  []graph.Pt
}

// mcPx is a line's Monte Carlo bands: the polygons of the 10th to 90th and the 25th to 75th percentiles, and the median, in pixels. The buffers are those the polygons were cut from.
type mcPx struct {
	geom                    plotGeom
	sr                      *mcSeries
	outer, inner            [][]graph.Pt
	median                  []graph.Pt
	p90, p10, p75, p25, p50 []graph.Pt
}

// bandPx is a band's polygons within the plot and, if it is hatched, its stripes.
type bandPx struct {
	geom         plotGeom
	upper, lower []engine.Sample
	hatch        bool
	polys        [][]graph.Pt
	stripes      []graph.Seg
}

// sameSamples reports whether a and b are the same slice of the data cache.
func sameSamples(a, b []engine.Sample) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// toPixels fills dst with the points of the dates and amounts, as pixels of the plot.
func toPixels(dst []graph.Pt, geom plotGeom, n int, at func(i int) (x, y float64)) []graph.Pt {
	dst = dst[:0]
	for i := 0; i < n; i++ {
		x, y := at(i)
		dst = append(dst, geom.toPx(x, y))
	}
	return dst
}

// line returns the line's samples in pixels in the plot geom.
func (c *pixelCache) line(id graph.LineID, geom plotGeom, samples []engine.Sample) []graph.Pt {
	if c.lines == nil {
		c.lines = map[graph.LineID]*linePx{}
	}
	e := c.lines[id]
	if e == nil {
		e = &linePx{}
		c.lines[id] = e
	}
	if e.geom != geom || !sameSamples(e.src, samples) {
		e.pts = toPixels(e.pts, geom, len(samples), func(i int) (float64, float64) {
			return float64(samples[i].Date) * secondsPerDay, samples[i].Amount
		})
		e.geom, e.src = geom, samples
	}
	return e.pts
}

// polysWithin returns the polygons between two pixel polylines that reach into the plot's horizontal extent.
func polysWithin(upper, lower []graph.Pt, geom plotGeom) [][]graph.Pt {
	left, right := geom.pos.X, geom.pos.X+geom.size.X
	var out [][]graph.Pt
	for _, poly := range graph.BandPolys(upper, lower) {
		minX, maxX := poly[0].X, poly[0].X
		for _, p := range poly[1:] {
			minX, maxX = min(minX, p.X), max(maxX, p.X)
		}
		if minX > right || maxX < left {
			continue
		}
		out = append(out, poly)
	}
	return out
}

// band returns the polygons of the band between the pixel polylines upper and lower, which were made from the samples upperSrc and lowerSrc, and its hatch stripes if hatch.
func (c *pixelCache) band(id graph.BandID, geom plotGeom, upper, lower []graph.Pt, upperSrc, lowerSrc []engine.Sample, hatch bool) *bandPx {
	if c.bands == nil {
		c.bands = map[graph.BandID]*bandPx{}
	}
	e := c.bands[id]
	if e == nil {
		e = &bandPx{}
		c.bands[id] = e
	}
	if e.geom != geom || e.hatch != hatch || !sameSamples(e.upper, upperSrc) || !sameSamples(e.lower, lowerSrc) {
		e.polys = polysWithin(upper, lower, geom)
		e.stripes = nil
		if hatch {
			e.stripes = graph.HatchStripes(upper, lower, hatchSpacing*uiScale,
				graph.Pt{X: geom.pos.X, Y: geom.pos.Y}, graph.Pt{X: geom.pos.X + geom.size.X, Y: geom.pos.Y + geom.size.Y})
		}
		e.geom, e.hatch, e.upper, e.lower = geom, hatch, upperSrc, lowerSrc
	}
	return e
}

// mcBands returns the line's Monte Carlo bands in pixels in the plot geom.
func (c *pixelCache) mcBands(id graph.LineID, geom plotGeom, sr *mcSeries) *mcPx {
	if c.mc == nil {
		c.mc = map[graph.LineID]*mcPx{}
	}
	e := c.mc[id]
	if e == nil {
		e = &mcPx{}
		c.mc[id] = e
	}
	if e.geom != geom || e.sr != sr {
		px := func(dst []graph.Pt, values []float64) []graph.Pt {
			return toPixels(dst, geom, len(values), func(i int) (float64, float64) { return float64(sr.dates[i]) * secondsPerDay, values[i] })
		}
		e.p90, e.p10, e.p75, e.p25, e.p50 = px(e.p90, sr.p90), px(e.p10, sr.p10), px(e.p75, sr.p75), px(e.p25, sr.p25), px(e.p50, sr.p50)
		e.outer, e.inner, e.median = polysWithin(e.p90, e.p10, geom), polysWithin(e.p75, e.p25, geom), e.p50
		e.geom, e.sr = geom, sr
	}
	return e
}
