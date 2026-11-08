// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Command finplan is a personal financial planner. For now it shows an interactive plot of
// invented balance data.
package main

import (
	"log"
	"runtime"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/AllenDang/cimgui-go/implot"

	"github.com/joeriedel/finplan/internal/backend"
	"github.com/joeriedel/finplan/internal/demodata"
)

const (
	// idleWaitMs bounds how long the loop sleeps waiting for an event when nothing needs redrawing.
	idleWaitMs = 500
	// settleFrames is how many frames are drawn after input so ImGui's one-frame-late state settles.
	settleFrames = 3
)

func init() {
	// SDL and OpenGL must stay on the thread that created the window.
	runtime.LockOSThread()
}

func main() {
	imgui.CreateContext()
	defer imgui.DestroyContext()
	implot.CreateContext()
	defer implot.DestroyContext()

	if err := backend.Init("finplan", 1280, 800); err != nil {
		log.Fatalf("backend init: %v", err)
	}
	defer backend.Shutdown()

	imgui.StyleColorsDark()
	scale := float32(1)
	plot := newBalancePlot(demodata.Generate(1))

	frames := settleFrames
	for {
		timeout := idleWaitMs
		if frames > 0 {
			timeout = 0
		}
		events := backend.WaitEvents(timeout)
		if events&backend.EventQuit != 0 {
			return
		}
		if events&backend.EventInput != 0 {
			frames = settleFrames
		}
		if frames == 0 {
			continue
		}
		frames--

		if s := backend.UIScale(); s != scale {
			// Scaling is not repeatable on a live style (sizes are truncated), so rebuild it from defaults.
			cur := imgui.CurrentStyle()
			base := imgui.NewStyle()
			imgui.StyleColorsDarkV(base)
			base.ScaleAllSizes(s)
			base.SetFontSizeBase(cur.FontSizeBase())
			base.SetFontScaleMain(cur.FontScaleMain())
			base.SetFontScaleDpi(s)
			*cur.CData = *base.CData
			base.Destroy()
			scale = s
		}

		backend.NewFrame()
		imgui.NewFrame()
		plot.draw()
		imgui.Render()
		backend.Render()
	}
}
