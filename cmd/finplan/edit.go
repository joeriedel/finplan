// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// renameOp is the rename of a projection, which also rewrites the graph lines and the saved presets that name it.
type renameOp struct{ from, to string }

func (r *renameOp) reverse() *renameOp {
	if r == nil {
		return nil
	}
	return &renameOp{r.to, r.from}
}

// errNoChange is returned by a change that left what it edits as it was; edit then saves nothing and keeps no step.
var errNoChange = errors.New("no change")

// editStep is what the history keeps of one edit: the projections before and after it, its rename if it had one, and
// the Graph switcher before and after, which the edit moves when it deletes the projection the switcher names.
// The lists are never changed once kept: edits change copies, and undo and redo install copies.
type editStep struct {
	before, after []*model.Projection
	rename        *renameOp
	switchBefore  string
	switchAfter   string
}

// edit is the one way an editor tab changes the record. It runs change on a copy of the editable documents and saves
// the copy; only if that works does the copy replace the live record, the Graph tab see it and the history get a step
// named label. If change or the save fails the live record stays as it was and the user is told why. A rename is the
// projection rename change makes; it is carried out on the graph lines and saved presets as part of the edit. Record
// entries (past history) are not edited this way. It reports whether the edit was made.
func (a *app) edit(label string, rename *renameOp, change func(rec *model.Record) error) bool {
	next := a.rec.EditableCopy()
	a.unchanged = false
	if err := change(next); err != nil {
		if errors.Is(err, errNoChange) {
			a.unchanged = true
			return false
		}
		a.notify(fmt.Sprintf("Could not %s: %v", label, err))
		return false
	}
	before, switchBefore := a.rec.Projections, a.graph.lines.Switcher()
	if !a.install(label, next, rename) {
		return false
	}
	a.history.Push(label, editStep{before, next.Projections, rename, switchBefore, a.graph.lines.Switcher()})
	return true
}

// install saves next and, if that works, makes it the live record and brings the graph up to date.
func (a *app) install(label string, next *model.Record, rename *renameOp) bool {
	if err := a.st.Save(next); err != nil {
		a.notify(fmt.Sprintf("Could not save the change (%s): %v\nNothing was changed.", label, err))
		return false
	}
	a.rec = next
	if rename != nil {
		a.graph.renameProjection(rename.from, rename.to)
	}
	a.graph.setRecord(next)
	return true
}

// undo reverts the latest step through the same save as an edit; if that fails the history stays where it is.
func (a *app) undo() {
	s, ok := a.history.PeekUndo()
	if ok && a.install("undo "+s.Label, a.rec.WithProjections(s.Data.before), s.Data.rename.reverse()) {
		if s.Data.rename == nil && s.Data.switchBefore != s.Data.switchAfter {
			a.graph.lines.SetSwitcher(s.Data.switchBefore)
		}
		a.history.Undo()
	}
}

// redo repeats the latest undone step; if the save fails the history stays where it is.
func (a *app) redo() {
	s, ok := a.history.PeekRedo()
	if ok && a.install("redo "+s.Label, a.rec.WithProjections(s.Data.after), s.Data.rename) {
		if s.Data.rename == nil && s.Data.switchBefore != s.Data.switchAfter {
			a.graph.lines.SetSwitcher(s.Data.switchAfter)
		}
		a.history.Redo()
	}
}

// editProjection is edit for a change to one projection that is not a rename. A change that leaves the projection as it
// was makes no edit.
func (a *app) editProjection(name, label string, change func(p *model.Projection) error) bool {
	return a.edit(label, nil, func(rec *model.Record) error {
		p := rec.Projection(name)
		if p == nil {
			return fmt.Errorf("projection %q does not exist", name)
		}
		was := p.Clone()
		if err := change(p); err != nil {
			return err
		}
		if reflect.DeepEqual(was, p) {
			return errNoChange
		}
		return nil
	})
}

// setRecord switches the Graph tab to a record that differs from the current one in its editable documents, keeping
// the lines, the preset and the visible range. A switcher naming a projection that is gone moves to the first one.
func (g *graphTab) setRecord(rec *model.Record) {
	g.rec, g.cache, g.dataStart = rec, newDataCache(rec), graph.DataStart(rec)
	if sw := g.lines.Switcher(); sw != "" && rec.Projection(sw) == nil && len(rec.Projections) > 0 {
		g.lines.SetSwitcher(rec.Projections[0].Name)
	}
}

// renameProjection renames the projection on the graph lines and in the saved presets.
func (g *graphTab) renameProjection(from, to string) {
	g.lines.RenameProjection(from, to)
	for i, p := range g.presets {
		if np, changed := p.RenameProjection(from, to); changed && g.persist(np) {
			g.presets[i] = np
		}
	}
}
