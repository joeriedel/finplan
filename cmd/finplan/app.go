// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/backend"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// app is the application shell: menu bar, tab bar and the tabs.
type app struct {
	st    *store.Store
	rec   *model.Record
	graph *graphTab
	quit  bool
	// notices are messages waiting for the user to dismiss them; backupOpen is set while the Backup save dialog is open.
	notices    []string
	backupOpen bool
	// reported holds the preset problems already queued, so that a reload does not queue them again.
	reported map[string]bool
}

// newApp loads the record and the view presets from the database.
func newApp(st *store.Store) (*app, error) {
	rec, err := st.Load()
	if err != nil {
		return nil, err
	}
	presets, problems, err := loadPresets(st)
	if err != nil {
		return nil, err
	}
	a := &app{st: st, rec: rec, reported: map[string]bool{}}
	a.notifyOnce(problems)
	a.graph = newGraphTab(rec, presets, st, a.notify)
	return a, nil
}

func (a *app) notify(msg string) { a.notices = append(a.notices, msg) }

// notifyOnce queues each message not queued by an earlier call.
func (a *app) notifyOnce(msgs []string) {
	for _, m := range msgs {
		if !a.reported[m] {
			a.reported[m] = true
			a.notify(m)
		}
	}
}

// reload re-reads the record and presets after another program changed the database, keeping the view. If it
// fails the old data stays.
func (a *app) reload() {
	rec, err := a.st.Load()
	if err != nil {
		a.notify(fmt.Sprintf("Could not reload the database: %v", err))
		return
	}
	presets, problems, err := loadPresets(a.st)
	if err != nil {
		a.notify(fmt.Sprintf("Could not reload the view presets: %v", err))
		return
	}
	a.rec = rec
	a.graph.reload(rec, presets)
	a.notifyOnce(problems)
}

// backup writes the backup the save dialog asked for and reports the outcome.
func (a *app) backup(r backend.SaveResult) {
	switch {
	case r.Err != nil:
		a.notify(fmt.Sprintf("The save dialog failed: %v", r.Err))
	case r.Cancelled:
	default:
		if err := a.st.Backup(r.Path); err != nil {
			a.notify(fmt.Sprintf("Backup failed: %v", err))
		} else {
			a.notify("Backup written to " + r.Path)
		}
	}
}

// drawNotices shows the first waiting message in a modal until it is dismissed.
func (a *app) drawNotices() {
	if len(a.notices) == 0 {
		return
	}
	imgui.OpenPopupStr("finplan##notice")
	imgui.SetNextWindowPosV(imgui.MainViewport().Center(), imgui.CondAppearing, imgui.NewVec2(0.5, 0.5))
	if imgui.BeginPopupModalV("finplan##notice", nil, imgui.WindowFlagsAlwaysAutoResize) {
		imgui.PushTextWrapPosV(imgui.FontSize() * 30)
		imgui.TextWrapped(a.notices[0])
		imgui.PopTextWrapPos()
		if imgui.Button("OK") {
			a.notices = a.notices[1:]
			imgui.CloseCurrentPopup()
		}
		imgui.EndPopup()
	}
}

// tabs lists the tab bar in order; only Graph is built.
var tabs = []string{"Graph", "Projections", "Assets", "Accounts", "Budget", "Markets"}

func (a *app) draw() {
	if r, ok := backend.PollSaveDialog(); ok {
		a.backupOpen = false
		a.backup(r)
	}
	a.menuBar()
	a.drawNotices()

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

// menuBar draws the main menu. Only File > Backup and Quit do anything yet.
func (a *app) menuBar() {
	if !imgui.BeginMainMenuBar() {
		return
	}
	if imgui.BeginMenuV("File", true) {
		imgui.MenuItemBoolV("New", "", false, false)
		imgui.MenuItemBoolV("Open...", "", false, false)
		imgui.MenuItemBoolV("Save", "", false, false)
		imgui.Separator()
		if imgui.MenuItemBoolV("Backup...", "", false, !a.backupOpen) {
			name := "finplan-backup-" + time.Now().Format("2006-01-02") + ".db"
			a.backupOpen = backend.ShowSaveDialog(name)
		}
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
