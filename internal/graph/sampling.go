// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// Sampling constants.
const (
	// PixelsPerSample is the least horizontal distance between neighbouring samples.
	PixelsPerSample = 4
	// MaxSamples bounds the number of samples of one grid.
	MaxSamples = 3000
	// chunkSteps is how many steps the grid's ends move at a time, so that panning does not change the grid.
	chunkSteps = 32
)

// sampleSteps are the spacings, in days, that a grid may use. All grids lie on multiples of their step
// counted from 1970-01-01, so a date is sampled or not regardless of the view.
var sampleSteps = []int{1, 2, 3, 7, 14, 30, 61, 122, 365, 730}

// Grid is a set of sample dates: First, First+Step, ... up to Last. It is comparable, for use as a cache key.
type Grid struct {
	Step        int
	First, Last model.Date
}

// Times returns the grid's dates in order.
func (g Grid) Times() []model.Date {
	out := make([]model.Date, 0, g.Count())
	for d := g.First; d <= g.Last; d = d.Add(g.Step) {
		out = append(out, d)
	}
	return out
}

// Count returns the number of dates in the grid.
func (g Grid) Count() int { return int(g.Last-g.First)/g.Step + 1 }

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// SampleGrid returns the sample grid for the visible dates from..to on a plot widthPx pixels wide: the
// smallest step that keeps samples PixelsPerSample apart, over the view padded by half its length on each side and
// rounded outward to whole chunks, so that the grid is the same while the view pans within a chunk. The
// grid has at most MaxSamples dates.
func SampleGrid(from, to model.Date, widthPx float32) Grid {
	if to < from {
		from, to = to, from
	}
	days := max(int(to-from), 1)
	want := float64(days) * PixelsPerSample / float64(max(widthPx, 1))
	step := sampleSteps[len(sampleSteps)-1]
	for _, s := range sampleSteps {
		if float64(s) >= want {
			step = s
			break
		}
	}
	for {
		chunk := step * chunkSteps
		g := Grid{
			Step:  step,
			First: model.Date(floorDiv(int(from)-days/2, chunk) * chunk),
			Last:  model.Date(-floorDiv(-(int(to)+days/2), chunk) * chunk),
		}
		if g.Count() <= MaxSamples {
			return g
		}
		step *= 2
	}
}

// RangeButton is a preset visible range: Years ahead of today, or the whole data when Years is 0.
type RangeButton struct {
	Label string
	Years int
}

// RangeButtons are the range presets, in display order.
var RangeButtons = []RangeButton{{"1Y", 1}, {"5Y", 5}, {"10Y", 10}, {"30Y", 30}, {"All", 0}}

// HorizonYears is how far ahead of today the All range reaches.
const HorizonYears = 30

// Span returns the visible dates for the preset. A numbered preset starts Years months (a twelfth of its length)
// before today and ends Years after it. All starts at the first data date, or a month
// before today if there is none or it lies after today, and ends HorizonYears after today.
func (b RangeButton) Span(today, dataStart model.Date) (from, to model.Date) {
	t := today.Time()
	add := func(months int) model.Date {
		return model.D(t.Year(), t.Month()+time.Month(months), t.Day())
	}
	if b.Years == 0 {
		from = dataStart
		if dataStart >= today {
			from = add(-1)
		}
		return from, add(12 * HorizonYears)
	}
	return add(-max(b.Years, 1)), add(12 * b.Years)
}
