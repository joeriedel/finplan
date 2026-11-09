// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

// axisLabelWorstCases are the widest tick labels FormatMoneyCompact can plausibly produce: negative, eight or nine figures.
var axisLabelWorstCases = []string{"-$888.88M", "-$88.88B", "-$8,888"}

// AxisLabelWorstCase returns the widest of the worst-case y tick labels under the measure function; giving the
// axis a tick with this label fixes its gutter width, whatever ticks are shown.
func AxisLabelWorstCase(measure func(string) float32) string {
	best, w := "", float32(-1)
	for _, l := range axisLabelWorstCases {
		if lw := measure(l); lw > w {
			best, w = l, lw
		}
	}
	return best
}

// ClampTag returns the top-left corner for a tag of the given size whose preferred top-left corner is at,
// moved so that the tag lies inside the rectangle from lo to hi. A tag larger than the rectangle sits at lo.
func ClampTag(at, size, lo, hi Pt) Pt {
	at.X = min(at.X, hi.X-size.X)
	at.X = max(at.X, lo.X)
	at.Y = min(at.Y, hi.Y-size.Y)
	at.Y = max(at.Y, lo.Y)
	return at
}

// TagTextBlack reports whether text on the given background (components 0..1) should be black rather than white.
func TagTextBlack(r, g, b float32) bool { return r*0.299+g*0.587+b*0.114 > 0.5 }

// InRange reports whether v lies within the visible range from lo to hi, inclusive; a tag for a value outside
// it is not drawn.
func InRange(v, lo, hi float64) bool { return v >= lo && v <= hi }
