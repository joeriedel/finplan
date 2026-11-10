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
	"github.com/joeriedel/finplan/internal/undo"
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
	// history holds the undoable edits (see edit); a reload from another program's changes clears it.
	history undo.History[editStep]
	proj    *projTab
	assets  *assetsTab
	accts   *acctTab
	budget  *budgetTab
	// unchanged is whether the latest edit call ended as a no-change; field commits use it to drop their typed text.
	unchanged bool
	// generation counts the undos, redos and reloads done, so an editor opened before one closes and drops its drag.
	generation int
	// showGraph selects the Graph tab for one frame.
	showGraph bool
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
	a.proj = &projTab{a: a}
	a.assets = &assetsTab{a: a}
	a.accts = &acctTab{a: a}
	a.budget = newBudgetTab(a)
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
	a.generation++
	a.history.Clear()
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
		imgui.TextUnformatted(a.notices[0])
		imgui.PopTextWrapPos()
		if imgui.Button("OK") {
			a.notices = a.notices[1:]
			imgui.CloseCurrentPopup()
		}
		imgui.EndPopup()
	}
}

// tabs lists the tab bar in order; only Markets is not built.
var tabs = []string{"Graph", "Projections", "Assets", "Accounts", "Budget", "Markets"}

func (a *app) draw() {
	if r, ok := backend.PollSaveDialog(); ok {
		a.backupOpen = false
		a.backup(r)
	}
	a.shortcuts()
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
				tabFlags := imgui.TabItemFlagsNone
				if i == 0 && a.showGraph {
					tabFlags, a.showGraph = imgui.TabItemFlagsSetSelected, false
				}
				if !imgui.BeginTabItemV(name, nil, tabFlags) {
					continue
				}
				imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, pad)
				imgui.PushStyleColorVec4(imgui.ColChildBg, rgba(colWindow, 1))
				if imgui.BeginChildStrV("##content", imgui.NewVec2(0, 0), imgui.ChildFlagsAlwaysUseWindowPadding, imgui.WindowFlagsNoScrollbar|imgui.WindowFlagsNoScrollWithMouse) {
					switch i {
					case 0:
						a.graph.draw()
					case 1:
						a.proj.draw()
					case 2:
						a.assets.draw()
					case 3:
						a.accts.draw()
					case 4:
						a.budget.draw()
					default:
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

// shortcuts handles Ctrl+Z (undo) and Ctrl+Shift+Z or Ctrl+Y (redo), except while a text field is being edited, which has
// its own undo, or a message is waiting.
func (a *app) shortcuts() {
	if len(a.notices) > 0 || imgui.CurrentIO().WantTextInput() {
		return
	}
	switch {
	case imgui.IsKeyChordPressed(imgui.KeyChord(imgui.ModCtrl | imgui.KeyZ)):
		a.undo()
	case imgui.IsKeyChordPressed(imgui.KeyChord(imgui.ModCtrl | imgui.ModShift | imgui.KeyZ)), imgui.IsKeyChordPressed(imgui.KeyChord(imgui.ModCtrl | imgui.KeyY)):
		a.redo()
	}
}

// menuBar draws the main menu. File > Backup, Quit and Edit > Undo and Redo are the menu items that do anything yet.
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
		undoLabel, redoLabel := "Undo", "Redo"
		if s, ok := a.history.PeekUndo(); ok {
			undoLabel += " " + s.Label
		}
		if s, ok := a.history.PeekRedo(); ok {
			redoLabel += " " + s.Label
		}
		if imgui.MenuItemBoolV(undoLabel, "Ctrl+Z", false, a.history.CanUndo()) {
			a.undo()
		}
		if imgui.MenuItemBoolV(redoLabel, "Ctrl+Shift+Z", false, a.history.CanRedo()) {
			a.redo()
		}
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
