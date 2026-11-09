// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"math"
	"strconv"
	"strings"
)

// axisLabelWorstCases are the widest tick labels FormatMoneyCompact can plausibly produce: negative, eight or nine figures.
var axisLabelWorstCases = []string{"-$888.88M", "-$88.88B", "-$8,888"}

// AxisLabelWorstCase returns the widest of the worst-case y tick labels under the measure function; GutterLabel
// builds on it the label of the tick that fixes the gutter width, whatever ticks are shown.
func AxisLabelWorstCase(measure func(string) float32) string {
	best, w := "", float32(-1)
	for _, l := range axisLabelWorstCases {
		if lw := measure(l); lw > w {
			best, w = l, lw
		}
	}
	return best
}

// clampSpan returns v limited to [lo, hi]; if hi < lo the span is too small and lo wins.
func clampSpan(v, lo, hi float32) float32 { return max(min(v, hi), lo) }

// ValueTagPos returns the top-left corner of a value tag of the given size for a value whose pixel row is y: in
// the y-axis gutter with its right edge on the plot's left edge, centred on y, kept within the plot's vertical
// extent.
func ValueTagPos(y float32, size, plotPos, plotSize Pt) Pt {
	return Pt{X: plotPos.X - size.X, Y: clampSpan(y-size.Y/2, plotPos.Y, plotPos.Y+plotSize.Y-size.Y)}
}

// DateTagPos returns the top-left corner of a date tag of the given size for a cursor at pixel column x: just
// below the plot, centred on x, kept within the plot's horizontal extent.
func DateTagPos(x float32, size, plotPos, plotSize Pt) Pt {
	return Pt{X: clampSpan(x-size.X/2, plotPos.X, plotPos.X+plotSize.X-size.X), Y: plotPos.Y + plotSize.Y}
}

// maxTagWholeDollars is the largest rounded whole-dollar amount FormatTagMoney writes out in full.
const maxTagWholeDollars = 9999999

// FormatTagMoney formats a value for a crosshair tag. While the amount rounded to whole dollars is at most
// 9,999,999 in magnitude it is FormatMoney's output. Beyond that it is three significant digits with a suffix M, B
// or T ("$10.0M", "$124M", "$1.24B", "-$3.50T"), rounding carrying into the next suffix. From 1000T up it is
// ">$999T" or "<-$999T", and NaN is "$?". The result is never longer than "<-$999T" or "-$9,999,999".
func FormatTagMoney(v float64) string {
	if math.IsNaN(v) {
		return "$?"
	}
	r := math.Round(math.Abs(v))
	if r <= maxTagWholeDollars {
		return FormatMoney(v)
	}
	neg := v < 0
	x := r / 1e6
	for _, unit := range []string{"M", "B", "T"} {
		dec := 2
		rx := math.Round(x*100) / 100
		if rx >= 10 {
			dec, rx = 1, math.Round(x*10)/10
		}
		if rx >= 100 {
			dec, rx = 0, math.Round(x)
		}
		if rx < 1000 {
			sign := ""
			if neg {
				sign = "-"
			}
			return sign + "$" + strconv.FormatFloat(rx, 'f', dec, 64) + unit
		}
		x /= 1000
	}
	if neg {
		return "<-$999T"
	}
	return ">$999T"
}

// tagWidthTemplates are the widest value tag texts FormatTagMoney can produce, with 9 standing for the widest
// digit.
var tagWidthTemplates = []string{"-$9,999,999", "-$999M", "-$9.99B", "<-$999T"}

// TagWidthWorstCase returns the width of the widest value tag: the widest of tagWidthTemplates under the measure
// function, with each 9 replaced by whichever digit measures widest, plus padX on both sides.
func TagWidthWorstCase(measure func(string) float32, padX float32) float32 {
	digit, dw := "0", float32(-1)
	for d := '0'; d <= '9'; d++ {
		if w := measure(string(d)); w > dw {
			digit, dw = string(d), w
		}
	}
	widest := float32(0)
	for _, t := range tagWidthTemplates {
		if w := measure(strings.ReplaceAll(t, "9", digit)); w > widest {
			widest = w
		}
	}
	return widest + 2*padX
}

// GutterLabel returns the label for the hidden y tick that fixes the gutter width: AxisLabelWorstCase, preceded by
// spaces if need be so that it measures at least as wide as the widest value tag with padding padX.
func GutterLabel(measure func(string) float32, padX float32) string {
	label := AxisLabelWorstCase(measure)
	need := TagWidthWorstCase(measure, padX)
	if measure(" ") <= 0 {
		return label
	}
	for measure(label) < need {
		label = " " + label
	}
	return label
}

// TagTextBlack reports whether text on the given background (components 0..1) should be black rather than white.
func TagTextBlack(r, g, b float32) bool { return r*0.299+g*0.587+b*0.114 > 0.5 }

// InRange reports whether v lies within the visible range from lo to hi, inclusive; a tag for a value outside
// it is not drawn.
func InRange(v, lo, hi float64) bool { return v >= lo && v <= hi }
