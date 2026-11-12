// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// editStep is what the history keeps of one edit: the projections before and after it, its rename if it had one, and
// the Graph switcher before and after, which the edit moves when it deletes the projection the switcher names.
// A delete that removed lines from saved view presets also keeps those presets as stored before and after it (presetsBefore, presetsAfter), which undo and redo put back.
// An edit of the accounts, assets, base budget or vest calendars also keeps the records before and after it (docsBefore, docsAfter; nil otherwise), which undo and redo restore, with their projections, through ops.RestoreDocuments.
// The lists and records are never changed once kept: edits change copies, and undo and redo install copies.
type editStep struct {
	before, after               []*model.Projection
	rename                      *ops.Rename
	switchBefore                string
	switchAfter                 string
	docsBefore, docsAfter       *model.Record
	presetsBefore, presetsAfter []store.Preset
	// facts is set for the step of a recorded vest: undo voids what it stored and redo restores it, instead of restoring documents or projections.
	facts *vestFacts
}

// vestFacts are the ids of the facts recording a vest stored: the vest and, when the recording created them, its deposit entry and price observation; 0 for none. voided is which of them (in that order) undo voided and redo has not restored yet: redo restores only those, so a fact the user voided on their own stays void.
type vestFacts struct {
	vest, entry, price int64
	voided             [3]bool
}

// edit is the one way an editor tab changes the record. do makes the edit through internal/ops against the live record, which validates and saves it; only if that works does the new record replace the live one, the Graph tab see it and the history get a step named label. If do fails the live record stays as it was and the user is told why; an edit that changed nothing says nothing and sets a.unchanged. Record entries and equity facts (past history) are not edited this way: they are append-only, and an added, corrected or voided one is made with addEntry, which leaves no undo step; recordVest is the one exception. It reports whether the edit was made.
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
	old, switchBefore := a.rec, a.graph.lines.Switcher()
	a.install(res)
	step := editStep{before: old.Projections, after: res.Record.Projections, rename: res.Rename, switchBefore: switchBefore, switchAfter: a.graph.lines.Switcher()}
	if !reflect.DeepEqual(old.Accounts, res.Record.Accounts) || !reflect.DeepEqual(old.Assets, res.Record.Assets) || !reflect.DeepEqual(old.Budget, res.Record.Budget) || !reflect.DeepEqual(old.Calendars, res.Record.Calendars) {
		step.docsBefore, step.docsAfter = old, res.Record
	}
	for _, c := range res.Pruned {
		step.presetsBefore, step.presetsAfter = append(step.presetsBefore, c.Before), append(step.presetsAfter, c.After)
	}
	a.history.Push(label, step)
	return true
}

// addEntry makes an edit that adds, corrects or voids a record entry or an equity fact (grant, vest, price) through internal/ops and installs the result. Facts are append-only, so it pushes no undo step; a fact is corrected or voided, never undone. It reports whether the edit was made.
func (a *app) addEntry(label string, do func() (*ops.Result, error)) bool {
	res, err := do()
	if err != nil {
		a.notifyFailed(label, err)
		return false
	}
	a.install(res)
	return true
}

// recordVest records a vest through internal/ops and installs the result like addEntry, but pushes an undo step: undo voids the vest and, if this recording created them, its deposit and price observation, and redo restores them. It reports whether the vest was recorded.
func (a *app) recordVest(label string, do func() (*ops.Result, error)) bool {
	res, err := do()
	if err != nil {
		a.notifyFailed(label, err)
		return false
	}
	a.install(res)
	facts := &vestFacts{vest: res.Vest.ID}
	if res.Entry != nil {
		facts.entry = res.Entry.ID
	}
	if res.Price != nil {
		facts.price = res.Price.ID
	}
	sw := a.graph.lines.Switcher()
	a.history.Push(label, editStep{before: res.Record.Projections, after: res.Record.Projections, switchBefore: sw, switchAfter: sw, facts: facts})
	return true
}

// restoreFacts voids the facts of a recorded vest, or restores those it voided with restore, through the same saves as any edit. Undo skips a fact already void, which stays the user's; redo restores the vest first and stops at the first fact it cannot restore, so a deposit is never restored without its vest. A fact already as asked is skipped, so a step that failed half way can be tried again. It reports whether all of it was done.
func (a *app) restoreFacts(label string, f *vestFacts, restore bool) bool {
	o := ops.Options{}
	type fact struct {
		id     int64
		do     func() (*ops.Result, error)
		voided *bool
	}
	var facts []fact
	if restore {
		facts = []fact{
			{f.vest, func() (*ops.Result, error) { return ops.CorrectVest(a.st, a.rec, f.vest, ops.VestChange{}, label, o) }, &f.voided[0]},
			{f.entry, func() (*ops.Result, error) {
				return ops.CorrectEntry(a.st, a.rec, f.entry, ops.EntryChange{}, label, o)
			}, &f.voided[1]},
			{f.price, func() (*ops.Result, error) {
				return ops.CorrectPrice(a.st, a.rec, f.price, ops.PriceChange{}, label, o)
			}, &f.voided[2]},
		}
	} else {
		facts = []fact{
			{f.vest, func() (*ops.Result, error) { return ops.VoidVest(a.st, a.rec, f.vest, label, o) }, &f.voided[0]},
			{f.entry, func() (*ops.Result, error) { return ops.VoidEntry(a.st, a.rec, f.entry, label, o) }, &f.voided[1]},
			{f.price, func() (*ops.Result, error) { return ops.VoidPrice(a.st, a.rec, f.price, label, o) }, &f.voided[2]},
		}
	}
	for _, x := range facts {
		if x.id == 0 || *x.voided == !restore {
			continue
		}
		res, err := x.do()
		if errors.Is(err, ops.ErrNoChange) {
			*x.voided = false
			continue
		}
		if err != nil {
			a.notifyFailed(label, err)
			return false
		}
		*x.voided = !restore
		a.install(res)
	}
	a.generation++
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
	for _, c := range res.Pruned {
		a.graph.setPreset(c.After)
	}
}

// restore saves the projections ps (with the rename the step they undo or redo made) through the same ops as an edit and installs the result; a step of docs restores the accounts, assets, base budget and projections of docs instead (the record entries added since are kept). It then stores the view presets to in place of from (the presets as the step left them), each only if the stored preset still has the body of from; a preset saved since is left alone and named in a notice. It reports whether the record was restored.
func (a *app) restore(label string, ps []*model.Projection, rename *ops.Rename, docs *model.Record, from, to []store.Preset) bool {
	var res *ops.Result
	var err error
	if docs != nil {
		res, err = ops.RestoreDocuments(a.st, a.rec, docs, ops.Options{})
	} else {
		res, err = ops.RestoreProjections(a.st, a.rec, ps, rename, ops.Options{})
	}
	if err != nil {
		a.notifyFailed(label, err)
		return false
	}
	a.install(res)
	var put []store.Preset
	for i, p := range to {
		if cur, err := a.st.Preset(p.Name); err != nil || !bytes.Equal(cur.Body, from[i].Body) {
			a.notify(fmt.Sprintf("View preset %q was changed since, so it was left as it is.", p.Name))
			continue
		}
		put = append(put, p)
	}
	if err := ops.PutPresets(a.st, put); err != nil {
		a.notify(fmt.Sprintf("Could not save %v", err))
	}
	for _, p := range put {
		a.graph.setPreset(p)
	}
	a.generation++
	return true
}

// undo reverts the latest step through the same save as an edit; if that fails the history stays where it is.
func (a *app) undo() {
	s, ok := a.history.PeekUndo()
	if ok && s.Data.facts != nil {
		if a.restoreFacts("undo "+s.Label, s.Data.facts, false) {
			a.history.Undo()
		}
		return
	}
	if ok && a.restore("undo "+s.Label, s.Data.before, s.Data.rename.Reverse(), s.Data.docsBefore, s.Data.presetsAfter, s.Data.presetsBefore) {
		if s.Data.rename == nil && s.Data.switchBefore != s.Data.switchAfter {
			a.graph.lines.SetSwitcher(s.Data.switchBefore)
		}
		a.history.Undo()
	}
}

// redo repeats the latest undone step; if the save fails the history stays where it is.
func (a *app) redo() {
	s, ok := a.history.PeekRedo()
	if ok && s.Data.facts != nil {
		if a.restoreFacts("redo "+s.Label, s.Data.facts, true) {
			a.history.Redo()
		}
		return
	}
	if ok && a.restore("redo "+s.Label, s.Data.after, s.Data.rename, s.Data.docsAfter, s.Data.presetsBefore, s.Data.presetsAfter) {
		if s.Data.rename == nil && s.Data.switchBefore != s.Data.switchAfter {
			a.graph.lines.SetSwitcher(s.Data.switchAfter)
		}
		a.history.Redo()
	}
}

// setRecord switches the Graph tab to a record that differs from the current one in its editable documents, keeping
// the lines, the preset and the visible range. The first accounts added to an empty record, with no lines and no preset, get the default lines. A switcher naming a projection that is gone moves to the first one.
func (g *graphTab) setRecord(rec *model.Record) {
	seed := false
	if len(g.rec.Accounts) == 0 && len(rec.Accounts) > 0 && g.preset < 0 {
		// Lines left from accounts that were undone or deleted name assets the incoming record lacks.
		for _, l := range g.lines.All() {
			if rec.Asset(l.Asset) == nil {
				g.lines.Remove(l.ID)
			}
		}
		seed = len(g.lines.All()) == 0
	}
	g.rec, g.cache, g.dataStart = rec, newDataCache(rec), graph.DataStart(rec)
	if seed {
		for _, id := range defaultLines(rec, g.demo) {
			g.lines.Add(id, false, "", graph.AssetColor(rec, id))
		}
	}
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

// setPreset replaces the loaded preset of the same name with the stored one p, a delete's rewrite or its undo. A preset the tab does not hold, or that cannot be decoded, is left out, as when presets are loaded.
func (g *graphTab) setPreset(p store.Preset) {
	np, err := graph.DecodePreset(p.Name, p.Format, p.Body)
	if err != nil {
		return
	}
	if i := slices.IndexFunc(g.presets, func(o graph.Preset) bool { return o.Name == p.Name }); i >= 0 {
		g.presets[i] = np
	}
}
