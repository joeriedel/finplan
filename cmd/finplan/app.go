// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
)

// app is the application shell: menu bar, tab bar and the tabs.
type app struct {
	rec   *model.Record
	graph *graphTab
	quit  bool
}

func newApp(rec *model.Record) *app {
	return &app{rec: rec, graph: newGraphTab(rec)}
}

// tabs lists the tab bar in order; only Graph is built.
var tabs = []string{"Graph", "Projections", "Assets", "Accounts", "Budget", "Markets"}

func (a *app) draw() {
	a.menuBar()

	vp := imgui.MainViewport()
	imgui.SetNextWindowPos(vp.WorkPos())
	imgui.SetNextWindowSize(vp.WorkSize())
	pad := imgui.CurrentStyle().WindowPadding()
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.NewVec2(0, 0))
	imgui.PushStyleVarFloat(imgui.StyleVarWindowRounding, 0)
	imgui.PushStyleVarFloat(imgui.StyleVarWindowBorderSize, 0)
	flags := imgui.WindowFlagsNoDecoration | imgui.WindowFlagsNoMove | imgui.WindowFlagsNoSavedSettings |
		imgui.WindowFlagsNoBringToFrontOnFocus | imgui.WindowFlagsNoScrollWithMouse
	if imgui.BeginV("##main", nil, flags) {
		p := imgui.CursorScreenPos()
		stripH := imgui.FrameHeight() + 2*uiScale
		imgui.WindowDrawList().AddRectFilled(p, imgui.NewVec2(p.X+imgui.WindowSize().X, p.Y+stripH), u32(rgba(colStrip, 1)))
		if imgui.BeginTabBarV("##tabs", imgui.TabBarFlagsNone) {
			for i, name := range tabs {
				if !imgui.BeginTabItemV(name, nil, imgui.TabItemFlagsNone) {
					continue
				}
				imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, pad)
				imgui.PushStyleColorVec4(imgui.ColChildBg, rgba(colWindow, 1))
				if imgui.BeginChildStrV("##content", imgui.NewVec2(0, 0), imgui.ChildFlagsAlwaysUseWindowPadding, imgui.WindowFlagsNoScrollbar|imgui.WindowFlagsNoScrollWithMouse) {
					if i == 0 {
						a.graph.draw()
					} else {
						imgui.TextDisabled("The " + name + " tab is not built yet.")
					}
				}
				imgui.EndChild()
				imgui.PopStyleColor()
				imgui.PopStyleVar()
				imgui.EndTabItem()
			}
			imgui.EndTabBar()
		}
	}
	imgui.End()
	imgui.PopStyleVarV(3)
}

// menuBar draws the main menu. Only File > Quit does anything yet.
func (a *app) menuBar() {
	if !imgui.BeginMainMenuBar() {
		return
	}
	if imgui.BeginMenuV("File", true) {
		imgui.MenuItemBoolV("New", "", false, false)
		imgui.MenuItemBoolV("Open...", "", false, false)
		imgui.MenuItemBoolV("Save", "", false, false)
		imgui.Separator()
		if imgui.MenuItemBoolV("Quit", "", false, true) {
			a.quit = true
		}
		imgui.EndMenu()
	}
	if imgui.BeginMenuV("Edit", true) {
		imgui.MenuItemBoolV("Undo", "", false, false)
		imgui.MenuItemBoolV("Redo", "", false, false)
		imgui.EndMenu()
	}
	if imgui.BeginMenuV("View", true) {
		imgui.MenuItemBoolV("Reset layout", "", false, false)
		imgui.EndMenu()
	}
	if imgui.BeginMenuV("Help", true) {
		imgui.MenuItemBoolV("About finplan", "", false, false)
		imgui.EndMenu()
	}
	imgui.EndMainMenuBar()
}
