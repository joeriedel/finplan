// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"slices"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// editStep is what the history keeps of one edit: the projections before and after it, its rename if it had one, and
// the Graph switcher before and after, which the edit moves when it deletes the projection the switcher names.
// The lists are never changed once kept: edits change copies, and undo and redo install copies.
type editStep struct {
	before, after []*model.Projection
	rename        *ops.Rename
	switchBefore  string
	switchAfter   string
}

// edit is the one way an editor tab changes the record. do makes the edit through internal/ops against the live record, which validates and saves it; only if that works does the new record replace the live one, the Graph tab see it and the history get a step named label. If do fails the live record stays as it was and the user is told why; an edit that changed nothing says nothing and sets a.unchanged. Record entries (past history) are not edited this way. It reports whether the edit was made.
func (a *app) edit(label string, do func() (*ops.Result, error)) bool {
	a.unchanged = false
	res, err := do()
	if err != nil {
		if errors.Is(err, ops.ErrNoChange) {
			a.unchanged = true
		} else {
			a.notifyFailed(label, err)
		}
		return false
	}
	before, switchBefore := a.rec.Projections, a.graph.lines.Switcher()
	a.install(res)
	a.history.Push(label, editStep{before, res.Record.Projections, res.Rename, switchBefore, a.graph.lines.Switcher()})
	return true
}

// notifyFailed tells the user why the edit or step called label failed.
func (a *app) notifyFailed(label string, err error) {
	var se *ops.SaveError
	if errors.As(err, &se) {
		a.notify(fmt.Sprintf("Could not save the change (%s): %v\nNothing was changed.", label, se.Err))
		return
	}
	a.notify(fmt.Sprintf("Could not %s: %v", label, err))
}

// install makes the record of a saved result the live one and brings the graph up to date.
func (a *app) install(res *ops.Result) {
	a.rec = res.Record
	if r := res.Rename; r != nil {
		a.graph.renameProjection(r.From, r.To, res.Rewritten)
	}
	for _, w := range res.Warnings {
		a.notify(fmt.Sprintf("Could not save %v", w))
	}
	a.graph.setRecord(res.Record)
}

// restore saves the projections ps (with the rename the step they undo or redo made) through the same ops as an edit and installs the result. It reports whether that worked.
func (a *app) restore(label string, ps []*model.Projection, rename *ops.Rename) bool {
	res, err := ops.RestoreProjections(a.st, a.rec, ps, rename, ops.Options{})
	if err != nil {
		a.notifyFailed(label, err)
		return false
	}
	a.install(res)
	a.generation++
	return true
}

// undo reverts the latest step through the same save as an edit; if that fails the history stays where it is.
func (a *app) undo() {
	s, ok := a.history.PeekUndo()
	if ok && a.restore("undo "+s.Label, s.Data.before, s.Data.rename.Reverse()) {
		if s.Data.rename == nil && s.Data.switchBefore != s.Data.switchAfter {
			a.graph.lines.SetSwitcher(s.Data.switchBefore)
		}
		a.history.Undo()
	}
}

// redo repeats the latest undone step; if the save fails the history stays where it is.
func (a *app) redo() {
	s, ok := a.history.PeekRedo()
	if ok && a.restore("redo "+s.Label, s.Data.after, s.Data.rename) {
		if s.Data.rename == nil && s.Data.switchBefore != s.Data.switchAfter {
			a.graph.lines.SetSwitcher(s.Data.switchAfter)
		}
		a.history.Redo()
	}
}

// setRecord switches the Graph tab to a record that differs from the current one in its editable documents, keeping
// the lines, the preset and the visible range. A switcher naming a projection that is gone moves to the first one.
func (g *graphTab) setRecord(rec *model.Record) {
	g.rec, g.cache, g.dataStart = rec, newDataCache(rec), graph.DataStart(rec)
	if sw := g.lines.Switcher(); sw != "" && rec.Projection(sw) == nil && len(rec.Projections) > 0 {
		g.lines.SetSwitcher(rec.Projections[0].Name)
	}
}

// renameProjection renames the projection on the graph lines and in the loaded presets that ops rewrote in the store.
func (g *graphTab) renameProjection(from, to string, rewritten []string) {
	g.lines.RenameProjection(from, to)
	for i, p := range g.presets {
		if np, changed := p.RenameProjection(from, to); changed && slices.Contains(rewritten, p.Name) {
			g.presets[i] = np
		}
	}
}
