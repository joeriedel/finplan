// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

func demoApp(t *testing.T) *app {
	t.Helper()
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	a, err := newApp(st, true)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func setStart(a *app, proj string, d *model.Date) bool {
	return a.edit("set start date", func() (*ops.Result, error) { return ops.SetStart(a.st, a.rec, proj, d, ops.Options{}) })
}

func TestEditSavesSwapsAndPushesOneStep(t *testing.T) {
	a := demoApp(t)
	g := a.graph
	old := a.rec
	lines, xMin := len(g.lines.All()), g.xMin
	d := model.Ptr(model.D(2030, 1, 1))
	if !setStart(a, demodata.ProjBase, d) {
		t.Fatalf("edit refused: %v", a.notices)
	}
	if a.rec == old || *a.rec.Projection(demodata.ProjBase).Start != *d {
		t.Error("the edit was not swapped in")
	}
	if old.Projection(demodata.ProjBase).Start == nil || *old.Projection(demodata.ProjBase).Start == *d {
		t.Error("the edit changed the old record")
	}
	if g.rec != a.rec || g.cache.rec != a.rec {
		t.Error("the Graph tab does not see the edit")
	}
	if len(g.lines.All()) != lines || g.xMin != xMin {
		t.Error("the edit disturbed the Graph view")
	}
	saved, err := a.st.Load()
	if err != nil || *saved.Projection(demodata.ProjBase).Start != *d {
		t.Errorf("the edit was not saved: %v", err)
	}
	if !a.history.CanUndo() || a.history.CanRedo() {
		t.Error("history after one edit")
	}
	if s, _ := a.history.PeekUndo(); s.Label != "set start date" {
		t.Errorf("label %q", s.Label)
	}
}

func TestUndoRedoAreSaved(t *testing.T) {
	a := demoApp(t)
	orig := a.rec.Projection(demodata.ProjBase).Start
	d := model.Ptr(model.D(2030, 1, 1))
	setStart(a, demodata.ProjBase, d)
	a.undo()
	if got := a.rec.Projection(demodata.ProjBase).Start; *got != *orig {
		t.Errorf("undo: start %v", got)
	}
	saved, _ := a.st.Load()
	if *saved.Projection(demodata.ProjBase).Start != *orig {
		t.Error("undo was not saved")
	}
	if a.history.CanUndo() || !a.history.CanRedo() || a.graph.rec != a.rec {
		t.Error("history or graph after undo")
	}
	a.redo()
	saved, _ = a.st.Load()
	if *a.rec.Projection(demodata.ProjBase).Start != *d || *saved.Projection(demodata.ProjBase).Start != *d {
		t.Error("redo did not restore the edit")
	}
	setStart(a, demodata.ProjBase, orig)
	a.undo()
	setStart(a, demodata.ProjBase, nil)
	if a.history.CanRedo() {
		t.Error("an edit left the redo side")
	}
}

func TestFailedSaveChangesNothing(t *testing.T) {
	a := demoApp(t)
	setStart(a, demodata.ProjBase, model.Ptr(model.D(2030, 1, 1)))
	a.notices = nil
	a.st.Close()

	if setStart(a, demodata.ProjBase, nil) {
		t.Error("an edit succeeded without a database")
	}
	if len(a.notices) != 1 {
		t.Errorf("the user was told %d times", len(a.notices))
	}
	if a.graph.rec != a.rec || a.rec.Projection(demodata.ProjBase).Start == nil {
		t.Error("a failed edit changed the model")
	}
	live := a.rec
	a.undo()
	if a.rec != live || !a.history.CanUndo() || a.history.CanRedo() {
		t.Error("a failed undo moved the history")
	}
	a.redo()
	if a.rec != live {
		t.Error("redo ran with nothing to redo")
	}
}

func TestEditRefusedByChange(t *testing.T) {
	a := demoApp(t)
	old := a.rec
	if a.edit("break it", func() (*ops.Result, error) { return nil, errors.New("no") }) {
		t.Error("a refused change was accepted")
	}
	if a.rec != old || a.history.CanUndo() || len(a.notices) != 1 {
		t.Error("a refused change had an effect")
	}
}

func TestReloadClearsHistory(t *testing.T) {
	a := demoApp(t)
	setStart(a, demodata.ProjBase, nil)
	a.undo()
	setStart(a, demodata.ProjBase, nil)
	a.reload()
	if a.history.CanUndo() || a.history.CanRedo() {
		t.Error("reload kept the history")
	}
}

func TestOwnSavesDoNotLookForeign(t *testing.T) {
	a := demoApp(t)
	v0, err := a.st.DataVersion()
	if err != nil {
		t.Fatal(err)
	}
	setStart(a, demodata.ProjBase, nil)
	a.undo()
	a.graph.persist(a.graph.presets[0])
	if v1, _ := a.st.DataVersion(); v1 != v0 {
		t.Errorf("data version moved from %d to %d on our own writes", v0, v1)
	}
}

func TestRenameRewritesLinesPresetsAndUndoes(t *testing.T) {
	a := demoApp(t)
	g := a.graph
	const to = "Renamed"
	g.lines.Add(demodata.NetWorth, true, demodata.ProjBase, graph.RGB(1))
	g.lines.SetSwitcher(demodata.ProjBase)
	idx := -1
	for i, p := range g.presets {
		for _, l := range p.Lines {
			if l.Projection == demodata.ProjBase {
				idx = i
			}
		}
	}
	if idx < 0 {
		t.Fatal("the demo has no preset naming Base")
	}
	name := g.presets[idx].Name

	ok := a.edit("rename projection", func() (*ops.Result, error) {
		return ops.RenameProjection(a.st, a.rec, demodata.ProjBase, to, ops.Options{})
	})
	if !ok {
		t.Fatal(a.notices)
	}
	check := func(proj string) {
		t.Helper()
		if g.lines.Switcher() != proj {
			t.Errorf("switcher %q", g.lines.Switcher())
		}
		pinned := 0
		for _, ln := range g.lines.All() {
			if ln.Pinned && ln.Projection == proj {
				pinned++
			}
		}
		if pinned == 0 {
			t.Errorf("no pinned line under %q", proj)
		}
		row, err := a.st.Preset(name)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := graph.DecodePreset(name, row.Format, row.Body)
		if err != nil || !reflect.DeepEqual(stored, g.presets[idx]) {
			t.Errorf("stored preset differs from the loaded one: %v", err)
		}
		uses := false
		for _, l := range stored.Lines {
			uses = uses || l.Projection == proj
		}
		if !uses {
			t.Errorf("stored preset does not name %q", proj)
		}
		if a.rec.Projection(proj) == nil || a.rec.Projection(demodata.ProjNewJob).Parent != proj {
			t.Errorf("record does not use %q", proj)
		}
	}
	check(to)
	a.undo()
	check(demodata.ProjBase)
	a.redo()
	check(to)
}

func TestDeleteRefusedWhileOthersBuildOnIt(t *testing.T) {
	a := demoApp(t)
	a.proj.sel = demodata.ProjBase
	a.proj.requestDelete()
	if len(a.notices) != 1 || a.proj.openDelete {
		t.Errorf("delete of a parent: notices %v", a.notices)
	}
	if a.edit("delete projection", func() (*ops.Result, error) {
		return ops.DeleteProjection(a.st, a.rec, demodata.ProjBase, ops.Options{})
	}) {
		t.Error("ops removed a projection with children")
	}
}

func TestProjectionTabActions(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	pt.newProjection()
	if pt.sel != "New projection" || a.rec.Projection("New projection").Parent != "" {
		t.Errorf("new from Reality: %q", pt.sel)
	}
	pt.newProjection()
	if pt.sel != "New projection 2" || a.rec.Projection(pt.sel).Parent != "New projection" {
		t.Errorf("new child: %q", pt.sel)
	}
	pt.selectProjection(demodata.ProjNewJob)
	pt.cloneProjection()
	if pt.sel != demodata.ProjNewJob+" copy" {
		t.Fatalf("clone: %q", pt.sel)
	}
	if !reflect.DeepEqual(a.rec.Projection(pt.sel).Mods, a.rec.Projection(demodata.ProjNewJob).Mods) {
		t.Error("clone lost the modifications")
	}
	pt.cloneProjection()
	if pt.sel != demodata.ProjNewJob+" copy copy" {
		t.Errorf("clone of clone: %q", pt.sel)
	}
	pt.selectProjection(demodata.ProjNewJob + " copy")
	pt.cloneProjection()
	pt.selectProjection(demodata.ProjNewJob)
	pt.cloneProjection()
	if pt.sel != demodata.ProjNewJob+" copy 2" {
		t.Errorf("second clone of one projection: %q", pt.sel)
	}

	pt.setParent(demodata.ProjBase, demodata.ProjNewJob)
	if a.rec.Projection(demodata.ProjBase).Parent != "" || len(pt.errs) == 0 {
		t.Error("a cycle was accepted")
	}
	pt.selectProjection(demodata.ProjNewJob)
	pt.rename(demodata.ProjWithdraw)
	if len(pt.errs) == 0 || pt.sel != demodata.ProjNewJob {
		t.Error("a duplicate name was accepted")
	}
	pt.rename("")
	if a.rec.Projection(demodata.ProjNewJob) == nil {
		t.Error("an empty name was accepted")
	}
	pt.rename("Job")
	if pt.sel != "Job" || a.rec.Projection("Job") == nil {
		t.Error("rename failed")
	}
}

func TestModificationEditing(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	pt.selectProjection(demodata.ProjNewJob)
	n := len(a.rec.Projection(demodata.ProjNewJob).Mods)

	pt.addKind, pt.addObj, pt.addProp, pt.addValue, pt.addStart = model.KindAccount, string(demodata.Savings), model.PropGrowth, "4.5", "2027-01-01"
	pt.addMod(demodata.ProjNewJob)
	p := a.rec.Projection(demodata.ProjNewJob)
	if len(p.Mods) != n+1 {
		t.Fatalf("mods %d, notices %v", len(p.Mods), a.notices)
	}
	v, from, ok := constantOf(p.Mods[n].Values)
	if !ok || v != 0.045 || from == nil || *from != model.D(2027, 1, 1) {
		t.Errorf("added value %v from %v", v, from)
	}

	pt.setModValue(demodata.ProjNewJob, n, "5%")
	pt.setModStart(demodata.ProjNewJob, n, "")
	v, from, _ = constantOf(a.rec.Projection(demodata.ProjNewJob).Mods[n].Values)
	if v != 0.05 || from != nil {
		t.Errorf("edited value %v from %v", v, from)
	}
	before := a.rec
	pt.setModValue(demodata.ProjNewJob, n, "lots")
	pt.setModStart(demodata.ProjNewJob, n, "tomorrow")
	if a.rec != before || len(pt.errs) == 0 {
		t.Error("bad input was accepted")
	}

	first := a.rec.Projection(demodata.ProjNewJob).Mods[0]
	a.edit("reorder modifications", func() (*ops.Result, error) {
		return ops.MoveMod(a.st, a.rec, demodata.ProjNewJob, 0, n, ops.Options{})
	})
	if !reflect.DeepEqual(a.rec.Projection(demodata.ProjNewJob).Mods[n], first) {
		t.Error("move failed")
	}
	for range 4 {
		a.undo()
	}
	if got := len(a.rec.Projection(demodata.ProjNewJob).Mods); got != n {
		t.Errorf("after undo %d mods, want %d", got, n)
	}
}

func TestSetInflation(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	infl := func() []model.Modification {
		var out []model.Modification
		for _, m := range a.rec.Projection(demodata.ProjBase).Mods {
			if isInflation(m) {
				out = append(out, m)
			}
		}
		return out
	}
	pt.setInflation(demodata.ProjBase, "3")
	if got := infl(); len(got) != 1 || got[0].Values.Segments()[0].Value != 0.03 {
		t.Fatalf("added: %+v", got)
	}
	pt.setInflation(demodata.ProjBase, "2.5%")
	if got := infl(); len(got) != 1 || got[0].Values.Segments()[0].Value != 0.025 {
		t.Fatalf("changed: %+v", got)
	}
	pt.setInflation(demodata.ProjBase, "")
	if len(infl()) != 0 {
		t.Error("clearing left the modification")
	}
	before := a.rec
	pt.setInflation(demodata.ProjBase, "lots")
	if a.rec != before || len(pt.errs) == 0 {
		t.Error("bad input was accepted")
	}
}

// netWorthSamples returns the Graph's sampled values of the net worth line over the next three years.
func netWorthSamples(t *testing.T, a *app) []float64 {
	t.Helper()
	today := a.rec.Today
	grid := graph.Grid{Step: 30, First: today, Last: today.Add(30 * 36)}
	for _, v := range a.graph.visible(grid) {
		if v.line.Asset == demodata.NetWorth {
			out := make([]float64, len(v.samples))
			for i, s := range v.samples {
				out[i] = s.Amount
			}
			return out
		}
	}
	t.Fatal("no net worth line")
	return nil
}

func TestGraphSamplesFollowEditUndoRedoWithoutReload(t *testing.T) {
	a := demoApp(t)
	before := netWorthSamples(t, a)
	var item model.BudgetItem
	for _, it := range a.rec.Budget.Items {
		if it.ID == "salary-a" {
			item = it
		}
	}
	amount, _ := item.Amount.At(a.rec.Today)
	item.Amount = model.Constant(amount * 2)
	if !a.edit("edit budget item", func() (*ops.Result, error) {
		return ops.EditBudgetItem(a.st, a.rec, item.ID, item, ops.Options{})
	}) {
		t.Fatalf("edit refused: %v", a.notices)
	}
	edited := netWorthSamples(t, a)
	if reflect.DeepEqual(before, edited) {
		t.Fatal("the Graph still shows the values from before the edit")
	}
	a.undo()
	if got := netWorthSamples(t, a); !reflect.DeepEqual(got, before) {
		t.Error("undo did not bring back the original values")
	}
	a.redo()
	if got := netWorthSamples(t, a); !reflect.DeepEqual(got, edited) {
		t.Error("redo did not re-apply the edited values")
	}
}

// periodRowOf is the current period of the budget item, as the This period table gives it to setPeriod.
func periodRowOf(t *testing.T, a *app, item string) engine.ItemPeriods {
	t.Helper()
	it, ok := a.rec.Budget.Item(item)
	if !ok {
		t.Fatalf("no item %s", item)
	}
	p, ok := it.PeriodAt(a.rec.Today)
	if !ok {
		t.Fatalf("%s has no period today", item)
	}
	return engine.ItemPeriods{Item: it, Periods: []model.PeriodView{{Period: p}}}
}

// A period override is undone by voiding it or correcting it back, and redone by correcting it, always as fact corrections that keep the fact's history.
func TestPeriodOverrideUndoRedoKeepsTheFactHistory(t *testing.T) {
	a := demoApp(t)
	row := periodRowOf(t, a, "dining")
	start := row.Current().Period.Start
	if _, ok := a.rec.Occurrences.Get("dining", start); ok {
		t.Fatal("the demo already overrides the period")
	}
	posted := func(v float64) ops.OccurrenceChange { return ops.OccurrenceChange{Posted: &v} }
	got := func() *float64 {
		o, ok := a.rec.Occurrences.Get("dining", start)
		if !ok {
			return nil
		}
		return o.Posted
	}

	if !a.budget.setPeriod(row, posted(100)) || got() == nil || *got() != 100 {
		t.Fatalf("set: %v", a.notices)
	}
	o, _ := a.rec.Occurrences.Get("dining", start)
	if !a.budget.setPeriod(row, posted(250)) || *got() != 250 {
		t.Fatalf("correct: %v", a.notices)
	}
	if a.budget.setPeriod(row, posted(250)) || !a.unchanged {
		t.Error("the same values were not left unchanged")
	}

	a.undo()
	if got() == nil || *got() != 100 {
		t.Fatalf("undo of a correction: %v", got())
	}
	a.undo()
	if got() != nil {
		t.Fatalf("undo of a new override left %v", *got())
	}
	if saved, err := a.st.Load(); err != nil {
		t.Fatal(err)
	} else if _, ok := saved.Occurrences.Get("dining", start); ok {
		t.Error("undo was not saved")
	}
	a.redo()
	a.redo()
	if got() == nil || *got() != 250 {
		t.Fatalf("redo: %v", got())
	}
	if !a.budget.clearPeriod(row) || got() != nil {
		t.Fatalf("clear: %v", a.notices)
	}
	a.undo()
	if got() == nil || *got() != 250 {
		t.Fatalf("undo of a clear: %v", got())
	}
	a.redo()
	if got() != nil {
		t.Fatal("redo of a clear left the override")
	}
	revs, err := a.st.FactHistory(o.ID)
	if err != nil || len(revs) != 9 {
		t.Errorf("the fact has %d revisions, %v", len(revs), err)
	}
}
