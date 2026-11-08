// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/AllenDang/cimgui-go/implot"

	"github.com/joeriedel/finplan/internal/demodata"
)

type plotSeries struct {
	label string
	ys    []float64
	spec  *implot.Spec
}

// balancePlot draws the demo balances as one full-window ImPlot plot.
type balancePlot struct {
	xs     []float64
	series []plotSeries
	// Initial axis limits, applied once.
	xMin, xMax, yMin, yMax float64
}

func newBalancePlot(d *demodata.Data) *balancePlot {
	p := &balancePlot{xs: make([]float64, len(d.Times))}
	for i, t := range d.Times {
		p.xs[i] = float64(t)
	}

	add := func(label string, ys []float64, color imgui.Vec4) {
		spec := implot.NewSpec()
		spec.SetPropVec4(implot.PropLineColor, color)
		spec.SetPropFloat(implot.PropLineWeight, 2)
		p.series = append(p.series, plotSeries{label: label, ys: ys, spec: spec})
	}
	add("Debt", d.Debt, imgui.NewVec4(0.90, 0.20, 0.20, 1))
	add("Cash", d.Cash, imgui.NewVec4(0.20, 0.75, 0.30, 1))
	add("Brokerage", d.Brokerage, imgui.NewVec4(0.25, 0.50, 0.95, 1))
	add("401k", d.K401, imgui.NewVec4(0.95, 0.65, 0.15, 1))
	add("Savings", d.Savings, imgui.NewVec4(0.20, 0.80, 0.80, 1))
	add("Total", d.Total, imgui.NewVec4(0.95, 0.95, 0.95, 1))

	p.xMin, p.xMax = p.xs[0], p.xs[len(p.xs)-1]
	p.yMin, p.yMax = d.Debt[0], d.Total[0]
	for _, s := range p.series {
		for _, y := range s.ys {
			p.yMin = min(p.yMin, y)
			p.yMax = max(p.yMax, y)
		}
	}
	margin := (p.yMax - p.yMin) * 0.05
	p.yMin -= margin
	p.yMax += margin
	return p
}

// draw fills the main viewport with the plot. Axis limits are free to pan and zoom.
func (p *balancePlot) draw() {
	vp := imgui.MainViewport()
	imgui.SetNextWindowPos(vp.Pos())
	imgui.SetNextWindowSize(vp.Size())
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.NewVec2(0, 0))
	flags := imgui.WindowFlagsNoDecoration | imgui.WindowFlagsNoMove | imgui.WindowFlagsNoSavedSettings |
		imgui.WindowFlagsNoBringToFrontOnFocus | imgui.WindowFlagsNoScrollWithMouse
	if imgui.BeginV("##main", nil, flags) {
		if implot.BeginPlotV("Balances", imgui.NewVec2(-1, -1), 0) {
			implot.SetupAxisV(implot.AxisX1, "Date", implot.AxisFlagsNone)
			implot.SetupAxisScalePlotScale(implot.AxisX1, implot.ScaleTime)
			implot.SetupAxisV(implot.AxisY1, "Balance", implot.AxisFlagsNone)
			implot.SetupAxisFormatStr(implot.AxisY1, "$%.0f")
			implot.SetupAxisLimitsV(implot.AxisX1, p.xMin, p.xMax, implot.CondOnce)
			implot.SetupAxisLimitsV(implot.AxisY1, p.yMin, p.yMax, implot.CondOnce)
			for _, s := range p.series {
				implot.PlotLinedoublePtrdoublePtrV(s.label, &p.xs[0], &s.ys[0], int32(len(p.xs)), *s.spec)
			}
			implot.EndPlot()
		}
	}
	imgui.End()
	imgui.PopStyleVar()
}
