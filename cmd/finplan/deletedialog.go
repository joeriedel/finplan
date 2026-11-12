// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/ops"
)

// deleteChoice is what the user chose in the delete dialog.
type deleteChoice int

const (
	deleteNone deleteChoice = iota
	deleteGo
	deleteClose
)

// deleteDialog is the modal that asks before an asset, account or budget item is deleted. It lists what the delete changes besides the object, or says why an account cannot be deleted and offers to close it.
type deleteDialog struct {
	what    string
	lines   []string
	blocked string
	opening bool
	// orphans is set when every line is a modification that stays behind and targets nothing, rather than something the delete removes.
	orphans bool
}

// ask opens the dialog for the object named what (for example `asset "Emergency fund"`) and its plan.
func (d *deleteDialog) ask(what string, plan ops.DeletePlan) {
	d.what, d.lines, d.blocked, d.opening = what, deleteLines(plan), plan.Blocked, true
	d.orphans = len(plan.Affected) > 0 && !slices.ContainsFunc(plan.Affected, func(a ops.Affected) bool { return a.Kind != ops.AffectedOrphan })
}

// deleteLines is the plan as the lines the dialog lists, one per affected object.
func deleteLines(plan ops.DeletePlan) []string {
	var out []string
	for _, a := range plan.Affected {
		switch {
		case a.Kind == ops.AffectedMember:
			out = append(out, fmt.Sprintf("Asset %q: remove its member %s", a.In, a.Detail))
		case a.Kind == ops.AffectedAssetSetting:
			out = append(out, fmt.Sprintf("Projection %q: remove the setting for %s", a.In, a.Detail))
		case a.Kind == ops.AffectedOrphan:
			out = append(out, fmt.Sprintf("Projection %q: its %s change will target nothing", a.In, a.Detail))
		case a.Kind == ops.AffectedPreset:
			out = append(out, fmt.Sprintf("View preset %q: remove what shows it (%s)", a.In, a.Detail))
		case a.Kind == ops.AffectedModification:
			out = append(out, fmt.Sprintf("Projection %q: remove the modification %s", a.In, a.Detail))
		case a.In == "budget":
			out = append(out, "Base budget: delete the item "+a.Detail)
		default:
			out = append(out, fmt.Sprintf("Projection %q: delete the item it adds, %s", a.In, a.Detail))
		}
	}
	return out
}

// draw draws the modal, when open, and returns the user's choice. canClose says whether a blocked account can be closed instead; closeBlock, if not empty, is why it cannot be closed on the date the form holds.
func (d *deleteDialog) draw(canClose bool, closeBlock string) deleteChoice {
	const name = "Delete?"
	if d.opening {
		imgui.OpenPopupStrV(name, 0)
		d.opening = false
	}
	if !imgui.BeginPopupModalV(name, nil, imgui.WindowFlagsAlwaysAutoResize) {
		return deleteNone
	}
	choice := deleteNone
	imgui.PushTextWrapPos()
	switch {
	case d.blocked != "":
		imgui.TextUnformatted(fmt.Sprintf("The %s cannot be deleted: %s. Closing keeps its history and reads 0 after the close date.", d.what, d.blocked))
	case len(d.lines) == 0:
		imgui.TextUnformatted(fmt.Sprintf("Delete the %s? Undo brings it back.", d.what))
	case d.orphans:
		imgui.TextUnformatted(fmt.Sprintf("Delete the %s? The projection changes below stay, but have nothing left to change. Undo brings it all back.", d.what))
		imgui.Spacing()
		for _, l := range d.lines {
			bulletText(l)
		}
	default:
		imgui.TextUnformatted(fmt.Sprintf("Delete the %s? It is removed from everything below, and those changes go with it. Undo brings it all back.", d.what))
		imgui.Spacing()
		for _, l := range d.lines {
			bulletText(l)
		}
	}
	imgui.PopTextWrapPos()
	imgui.Spacing()
	switch {
	case d.blocked == "":
		if imgui.Button("Delete") {
			choice = deleteGo
		}
	case canClose:
		actionButton("Close account", closeBlock, func() { choice = deleteClose })
	}
	if d.blocked == "" || canClose {
		imgui.SameLine()
	}
	if imgui.Button("Cancel") || choice != deleteNone {
		imgui.CloseCurrentPopup()
	}
	imgui.EndPopup()
	return choice
}
