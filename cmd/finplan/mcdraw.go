// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/AllenDang/cimgui-go/imgui"
)

// drawMonteCarlo fills the 10th to 90th and the 25th to 75th percentile bands of each line that has them, lighter outside, and strokes the median. It runs before the lines so that they lie on top.
func (g *graphTab) drawMonteCarlo(dl *imgui.DrawList, views []lineView) {
	geom := g.geom()
	for _, v := range views {
		sr := v.mcSeries
		if sr == nil || len(sr.dates) < 2 {
			continue
		}
		px := g.px.mcBands(v.line.ID, geom, sr)
		fillPolys(dl, px.outer, u32(lineColor(v.line.Color, 0.12)))
		fillPolys(dl, px.inner, u32(lineColor(v.line.Color, 0.25)))
		dl.AddPolyline(vecs(px.median), int32(len(px.median)), u32(lineColor(v.line.Color, 0.75)), 1.5*uiScale)
	}
}

// drawMonteCarloLabels writes at the bottom left of the plot, for each line that asks for Monte Carlo, that it is computing or the share of runs that stay above zero.
func (g *graphTab) drawMonteCarloLabels(dl *imgui.DrawList, views []lineView) {
	var texts []string
	var lines []lineView
	for _, v := range views {
		if !v.line.MonteCarlo {
			continue
		}
		var text string
		switch {
		case v.mc == mcFailed:
			text = v.name + ": Monte Carlo failed"
		case v.mc == mcComputing:
			text = v.name + ": computing Monte Carlo…"
		case v.mcSeries.hasSuccess:
			text = fmt.Sprintf("%s: %.0f%% of runs stay above zero to age %d", v.name, v.mcSeries.success, mcSuccessAge)
		default:
			text = v.name + ": Monte Carlo bands"
		}
		texts, lines = append(texts, text), append(lines, v)
	}
	s := uiScale
	lh := imgui.TextLineHeight() + 2*s
	y := g.plotPos.Y + g.plotSize.Y - 6*s - lh*float32(len(texts))
	for i, text := range texts {
		dl.AddTextVec2(imgui.NewVec2(g.plotPos.X+8*s, y+lh*float32(i)), u32(lineColor(lines[i].line.Color, 1)), text)
	}
}
