// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

const presetName = "Base view"

// seed returns a store holding the Family record and a view preset that pins a line under demodata.ProjBase, and the record loaded from it.
func seed(t *testing.T) (*store.Store, *model.Record) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Save(demodata.Family()); err != nil {
		t.Fatal(err)
	}
	l := graph.NewLines(demodata.ProjBase)
	l.Add(demodata.NetWorth, true, demodata.ProjBase, graph.RGB(1))
	putPreset(t, st, graph.Capture(presetName, l, 0, 1))
	rec, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	return st, rec
}

func putPreset(t *testing.T, st *store.Store, p graph.Preset) {
	t.Helper()
	body, err := graph.EncodePreset(p)
	if err == nil {
		err = st.PutPreset(p.Name, graph.PresetFormat, body)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func storedPreset(t *testing.T, st *store.Store) graph.Preset {
	t.Helper()
	row, err := st.Preset(presetName)
	if err != nil {
		t.Fatal(err)
	}
	p, err := graph.DecodePreset(row.Name, row.Format, row.Body)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func reload(t *testing.T, st *store.Store) *model.Record {
	t.Helper()
	rec, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// unchanged checks that the store still holds the projections of before, and that before is the record it was.
func unchanged(t *testing.T, st *store.Store, before *model.Record, snapshot []*model.Projection) {
	t.Helper()
	if !reflect.DeepEqual(before.Projections, snapshot) {
		t.Error("the input record was changed")
	}
	if got := reload(t, st).Projections; !reflect.DeepEqual(got, snapshot) {
		t.Error("the store was changed")
	}
}

func snapshot(rec *model.Record) []*model.Projection {
	return rec.WithProjections(rec.Projections).Projections
}

// dryRunMatches calls op as a dry run, checks that it saved nothing, then as a real run, and checks that the two results agree and the real one was saved.
func dryRunMatches(t *testing.T, op func(st *store.Store, rec *model.Record, o Options) (*Result, error)) {
	t.Helper()
	st, rec := seed(t)
	snap := snapshot(rec)
	preset := storedPreset(t, st)
	dry, err := op(st, rec, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	unchanged(t, st, rec, snap)
	if !reflect.DeepEqual(storedPreset(t, st), preset) {
		t.Error("a dry run wrote a preset")
	}
	real, err := op(st, rec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dry.Record.Projections, real.Record.Projections) || dry.Removed != real.Removed || !reflect.DeepEqual(dry.Rewritten, real.Rewritten) {
		t.Error("the dry run differs from the real run")
	}
	if got := reload(t, st).Projections; !reflect.DeepEqual(got, real.Record.Projections) {
		t.Error("the real run was not saved")
	}
}

// refused checks that op fails with want, saving nothing.
func refused(t *testing.T, want error, op func(st *store.Store, rec *model.Record) (*Result, error)) {
	t.Helper()
	st, rec := seed(t)
	snap := snapshot(rec)
	res, err := op(st, rec)
	if !errors.Is(err, want) || res != nil {
		t.Fatalf("got %v, %v; want %v", res, err, want)
	}
	unchanged(t, st, rec, snap)
}

func TestNewProjection(t *testing.T) {
	op := func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return NewProjection(st, rec, "  Fresh ", demodata.ProjBase, o)
	}
	dryRunMatches(t, op)
	st, rec := seed(t)
	res, err := op(st, rec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := reload(t, st).Projection("Fresh")
	if p == nil || p.Parent != demodata.ProjBase || res.Projection.Name != "Fresh" || rec.Projection("Fresh") != nil {
		t.Errorf("saved %+v", p)
	}
	refused(t, ErrEmptyName, func(st *store.Store, rec *model.Record) (*Result, error) {
		return NewProjection(st, rec, "  ", "", Options{})
	})
	refused(t, ErrNameTaken, func(st *store.Store, rec *model.Record) (*Result, error) {
		return NewProjection(st, rec, demodata.ProjBase, "", Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return NewProjection(st, rec, "Fresh", "Nowhere", Options{})
	})
}

func TestCloneProjection(t *testing.T) {
	op := func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return CloneProjection(st, rec, demodata.ProjNewJob, "Job copy", o)
	}
	dryRunMatches(t, op)
	st, rec := seed(t)
	if _, err := op(st, rec, Options{}); err != nil {
		t.Fatal(err)
	}
	got := reload(t, st)
	if c := got.Projection("Job copy"); c == nil || !reflect.DeepEqual(c.Mods, got.Projection(demodata.ProjNewJob).Mods) {
		t.Errorf("clone %+v", c)
	}
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return CloneProjection(st, rec, "Nowhere", "x", Options{})
	})
	refused(t, ErrNameTaken, func(st *store.Store, rec *model.Record) (*Result, error) {
		return CloneProjection(st, rec, demodata.ProjNewJob, demodata.ProjBase, Options{})
	})
	refused(t, ErrEmptyName, func(st *store.Store, rec *model.Record) (*Result, error) {
		return CloneProjection(st, rec, demodata.ProjNewJob, " ", Options{})
	})
}

func TestRenameProjection(t *testing.T) {
	op := func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return RenameProjection(st, rec, demodata.ProjBase, " Renamed ", o)
	}
	dryRunMatches(t, op)
	st, rec := seed(t)
	res, err := op(st, rec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if *res.Rename != (Rename{demodata.ProjBase, "Renamed"}) || len(res.Warnings) != 0 || !reflect.DeepEqual(res.Rewritten, []string{presetName}) {
		t.Errorf("result %+v", res)
	}
	got := reload(t, st)
	if got.Projection("Renamed") == nil || got.Projection(demodata.ProjBase) != nil || got.Projection(demodata.ProjNewJob).Parent != "Renamed" {
		t.Error("rename not saved")
	}
	p := storedPreset(t, st)
	if p.Switcher != "Renamed" || p.Lines[0].Projection != "Renamed" {
		t.Errorf("stored preset %+v", p)
	}
	back, err := RestoreProjections(st, res.Record, rec.Projections, res.Rename.Reverse(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p := storedPreset(t, st); p.Switcher != demodata.ProjBase || back.Record.Projection(demodata.ProjBase) == nil {
		t.Errorf("restore did not undo the rename: %+v", p)
	}

	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return RenameProjection(st, rec, demodata.ProjBase, " "+demodata.ProjBase+" ", Options{})
	})
	refused(t, ErrEmptyName, func(st *store.Store, rec *model.Record) (*Result, error) {
		return RenameProjection(st, rec, demodata.ProjBase, "", Options{})
	})
	refused(t, ErrNameTaken, func(st *store.Store, rec *model.Record) (*Result, error) {
		return RenameProjection(st, rec, demodata.ProjBase, demodata.ProjWithdraw, Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return RenameProjection(st, rec, "Nowhere", "x", Options{})
	})
}

func TestRenameSkipsUndecodablePresets(t *testing.T) {
	st, rec := seed(t)
	if err := st.PutPreset("junk", graph.PresetFormat, []byte("not a preset")); err != nil {
		t.Fatal(err)
	}
	res, err := RenameProjection(st, rec, demodata.ProjBase, "Renamed", Options{})
	if err != nil || len(res.Warnings) != 0 || !reflect.DeepEqual(res.Rewritten, []string{presetName}) {
		t.Errorf("%+v %v", res, err)
	}
}

func TestDeleteProjection(t *testing.T) {
	op := func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return DeleteProjection(st, rec, demodata.ProjWithdraw, o)
	}
	dryRunMatches(t, op)
	st, rec := seed(t)
	res, err := op(st, rec, Options{})
	if err != nil || res.Removed != demodata.ProjWithdraw || res.Projection != nil {
		t.Fatalf("%+v %v", res, err)
	}
	if reload(t, st).Projection(demodata.ProjWithdraw) != nil {
		t.Error("not deleted")
	}
	refused(t, ErrHasChildren, func(st *store.Store, rec *model.Record) (*Result, error) {
		return DeleteProjection(st, rec, demodata.ProjBase, Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return DeleteProjection(st, rec, "Nowhere", Options{})
	})
}

func TestSetParent(t *testing.T) {
	op := func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetParent(st, rec, demodata.ProjNewJob, "", o)
	}
	dryRunMatches(t, op)
	st, rec := seed(t)
	if _, err := op(st, rec, Options{}); err != nil {
		t.Fatal(err)
	}
	if reload(t, st).Projection(demodata.ProjNewJob).Parent != "" {
		t.Error("parent not cleared")
	}
	refused(t, model.ErrCycle, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetParent(st, rec, demodata.ProjBase, demodata.ProjNewJob, Options{})
	})
	refused(t, model.ErrCycle, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetParent(st, rec, demodata.ProjBase, demodata.ProjBase, Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetParent(st, rec, demodata.ProjBase, "Nowhere", Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetParent(st, rec, "Nowhere", "", Options{})
	})
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetParent(st, rec, demodata.ProjNewJob, demodata.ProjBase, Options{})
	})
}

func TestSetStartAndOverrideHistory(t *testing.T) {
	d := model.Ptr(model.D(2031, 2, 3))
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetStart(st, rec, demodata.ProjBase, d, o)
	})
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetOverrideHistory(st, rec, demodata.ProjBase, model.Ptr(true), o)
	})
	st, rec := seed(t)
	res, err := SetStart(st, rec, demodata.ProjNewJob, d, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Start; got == nil || *got != *d {
		t.Errorf("start %v", got)
	}
	res, err = SetOverrideHistory(st, res.Record, demodata.ProjNewJob, model.Ptr(false), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).OverrideHistory; got == nil || *got {
		t.Errorf("override %v", got)
	}
	if res, err = SetStart(st, res.Record, demodata.ProjNewJob, nil, Options{}); err != nil || res.Projection.Start != nil || reload(t, st).Projection(demodata.ProjNewJob).Start != nil {
		t.Errorf("start not removed: %v", err)
	}

	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetStart(st, rec, demodata.ProjBase, rec.Projection(demodata.ProjBase).Start, Options{})
	})
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetOverrideHistory(st, rec, demodata.ProjBase, rec.Projection(demodata.ProjBase).OverrideHistory, Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetStart(st, rec, "Nowhere", d, Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetOverrideHistory(st, rec, "Nowhere", nil, Options{})
	})
}

func TestSetInflation(t *testing.T) {
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetInflation(st, rec, demodata.ProjWithdraw, model.Ptr(0.03), o)
	})
	st, rec := seed(t)
	infl := func(r *model.Record) (out []model.Modification) {
		for _, m := range r.Projection(demodata.ProjWithdraw).Mods {
			if m.Target.Kind == model.KindGlobal && m.Property == model.PropInflation {
				out = append(out, m)
			}
		}
		return out
	}
	res, err := SetInflation(st, rec, demodata.ProjWithdraw, model.Ptr(0.03), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := infl(reload(t, st)); len(got) != 1 || got[0].Values.Segments()[0].Value != 0.03 {
		t.Fatalf("added %+v", got)
	}
	res, err = SetInflation(st, res.Record, demodata.ProjWithdraw, model.Ptr(0.025), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := infl(reload(t, st)); len(got) != 1 || got[0].Values.Segments()[0].Value != 0.025 {
		t.Fatalf("changed %+v", got)
	}
	if _, err := SetInflation(st, res.Record, demodata.ProjWithdraw, model.Ptr(0.025), Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same value: %v", err)
	}
	res, err = SetInflation(st, res.Record, demodata.ProjWithdraw, nil, Options{})
	if err != nil || len(infl(reload(t, st))) != 0 {
		t.Fatalf("removed: %v", err)
	}
	if _, err := SetInflation(st, res.Record, demodata.ProjWithdraw, nil, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("remove none: %v", err)
	}
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetInflation(st, rec, demodata.ProjWithdraw, model.Ptr(2e15), Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetInflation(st, rec, "Nowhere", nil, Options{})
	})
}

func TestAddMod(t *testing.T) {
	m := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Savings)}, Property: model.PropGrowth, Values: model.Constant(0.045)}
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return AddMod(st, rec, demodata.ProjNewJob, m, o)
	})
	st, rec := seed(t)
	n := len(rec.Projection(demodata.ProjNewJob).Mods)
	if _, err := AddMod(st, rec, demodata.ProjNewJob, m, Options{}); err != nil {
		t.Fatal(err)
	}
	mods := reload(t, st).Projection(demodata.ProjNewJob).Mods
	if len(mods) != n+1 || !reflect.DeepEqual(mods[n], m) {
		t.Errorf("mods %+v", mods)
	}
	global := model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Values: model.Constant(0.02)}
	if _, err := AddMod(st, rec, demodata.ProjNewJob, global, Options{DryRun: true}); err != nil {
		t.Errorf("global without object: %v", err)
	}
	noObject := m
	noObject.Target.ID = ""
	refused(t, ErrNoObject, func(st *store.Store, rec *model.Record) (*Result, error) {
		return AddMod(st, rec, demodata.ProjNewJob, noObject, Options{})
	})
	bad := m
	bad.Values = model.Constant(1e16)
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return AddMod(st, rec, demodata.ProjNewJob, bad, Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return AddMod(st, rec, "Nowhere", m, Options{})
	})
}

func TestSetModValues(t *testing.T) {
	from := model.D(2029, 1, 1)
	vals := model.MustSchedule(model.Segment{From: from, Value: 0.07})
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetModValues(st, rec, demodata.ProjNewJob, 0, vals, o)
	})
	st, rec := seed(t)
	if _, err := SetModValues(st, rec, demodata.ProjNewJob, 0, vals, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Mods[0].Values; !reflect.DeepEqual(got.Segments(), vals.Segments()) {
		t.Errorf("values %v", got.Segments())
	}
	same := rec.Projection(demodata.ProjNewJob).Mods[0].Values
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetModValues(st, rec, demodata.ProjNewJob, 0, same, Options{})
	})
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetModValues(st, rec, demodata.ProjNewJob, 0, model.Constant(-1e16), Options{})
	})
	for _, i := range []int{-1, 1000} {
		st, rec := seed(t)
		if _, err := SetModValues(st, rec, demodata.ProjNewJob, i, vals, Options{}); err == nil {
			t.Errorf("index %d accepted", i)
		}
	}
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetModValues(st, rec, "Nowhere", 0, vals, Options{})
	})
}

func TestMoveAndRemoveMod(t *testing.T) {
	st, rec := seed(t)
	mods := rec.Projection(demodata.ProjNewJob).Mods
	if len(mods) < 2 {
		t.Fatal("the demo projection needs two modifications")
	}
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return MoveMod(st, rec, demodata.ProjNewJob, 0, 1, o)
	})
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return RemoveMod(st, rec, demodata.ProjNewJob, 0, o)
	})
	res, err := MoveMod(st, rec, demodata.ProjNewJob, 0, 1, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Mods; !reflect.DeepEqual(got[1], mods[0]) || !reflect.DeepEqual(got[0], mods[1]) {
		t.Error("not moved")
	}
	if _, err := RemoveMod(st, res.Record, demodata.ProjNewJob, 0, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Mods; len(got) != len(mods)-1 || !reflect.DeepEqual(got[0], mods[0]) {
		t.Error("not removed")
	}
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return MoveMod(st, rec, demodata.ProjNewJob, 0, 0, Options{})
	})
	for _, c := range [][2]int{{-1, 0}, {0, 1000}, {1000, 0}} {
		st, rec := seed(t)
		if _, err := MoveMod(st, rec, demodata.ProjNewJob, c[0], c[1], Options{}); err == nil {
			t.Errorf("move %v accepted", c)
		}
	}
	for _, i := range []int{-1, 1000} {
		st, rec := seed(t)
		if _, err := RemoveMod(st, rec, demodata.ProjNewJob, i, Options{}); err == nil {
			t.Errorf("remove %d accepted", i)
		}
	}
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return RemoveMod(st, rec, "Nowhere", 0, Options{})
	})
}

func TestFailedSaveIsASaveError(t *testing.T) {
	st, rec := seed(t)
	st.Close()
	res, err := SetStart(st, rec, demodata.ProjBase, model.Ptr(model.D(2031, 1, 1)), Options{})
	var se *SaveError
	if res != nil || !errors.As(err, &se) {
		t.Errorf("%v %v", res, err)
	}
}

func amountMod(id string, from model.Date, v float64) model.Modification {
	return model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: id}, Property: model.PropAmount, Values: model.MustSchedule(model.Segment{From: from, Value: v})}
}

func TestInsertMod(t *testing.T) {
	m := amountMod("salary-a", model.D(2030, 1, 1), 7000)
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return InsertMod(st, rec, demodata.ProjNewJob, 1, m, o)
	})
	st, rec := seed(t)
	n := len(rec.Projection(demodata.ProjNewJob).Mods)
	if _, err := InsertMod(st, rec, demodata.ProjNewJob, 1, m, Options{}); err != nil {
		t.Fatal(err)
	}
	mods := reload(t, st).Projection(demodata.ProjNewJob).Mods
	if len(mods) != n+1 || !reflect.DeepEqual(mods[1], m) {
		t.Errorf("mods %+v", mods)
	}
	if _, err := InsertMod(st, rec, demodata.ProjNewJob, n, m, Options{DryRun: true}); err != nil {
		t.Errorf("insert at the end: %v", err)
	}
	for _, i := range []int{-1, n + 1} {
		st, rec := seed(t)
		if _, err := InsertMod(st, rec, demodata.ProjNewJob, i, m, Options{}); err == nil {
			t.Errorf("index %d accepted", i)
		}
	}
}

func TestModTargets(t *testing.T) {
	add := func(proj string, i int, m model.Modification, want error) {
		t.Helper()
		refused(t, want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return InsertMod(st, rec, proj, i, m, Options{})
		})
	}
	// Existing items, accounts and assets resolve.
	st, rec := seed(t)
	for _, m := range []model.Modification{
		amountMod("salary-a", model.D(2030, 1, 1), 1),
		{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Checking)}, Property: model.PropGrowth, Values: model.Constant(0.01)},
		{Target: model.ObjectRef{Kind: model.KindAsset, ID: string(demodata.NetWorth)}, Property: model.PropGrowth, Values: model.Constant(0.01)},
	} {
		if _, err := AddMod(st, rec, demodata.ProjNewJob, m, Options{DryRun: true}); err != nil {
			t.Errorf("%+v: %v", m.Target, err)
		}
	}
	// A parent's added item resolves from a child; a sibling's does not.
	child := model.Projection{Name: "Child", Parent: demodata.ProjWithdraw}
	rec.Projections = append(rec.Projections, &child)
	if err := st.Save(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := AddMod(st, rec, "Child", amountMod("early-401k-withdrawal", model.D(2027, 6, 1), 5000), Options{DryRun: true}); err != nil {
		t.Errorf("parent's item: %v", err)
	}
	add("Child", 0, amountMod("nope", model.D(2030, 1, 1), 1), ErrUnknownTarget)
	add(demodata.ProjNewJob, 0, amountMod("early-401k-withdrawal", model.D(2027, 6, 1), 5000), ErrUnknownTarget)
	add(demodata.ProjNewJob, 0, model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: "nope"}, Property: model.PropGrowth, Values: model.Constant(0.01)}, ErrUnknownTarget)
	add(demodata.ProjNewJob, 0, model.Modification{Target: model.ObjectRef{Kind: model.KindAsset, ID: "nope"}, Property: model.PropGrowth, Values: model.Constant(0.01)}, ErrUnknownTarget)
	// The property must suit the object.
	add(demodata.ProjNewJob, 0, model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Checking)}, Property: model.PropAmount, Values: model.Constant(1)}, ErrInvalidValue)
	add(demodata.ProjNewJob, 0, model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropGrowth, Values: model.Constant(1)}, ErrInvalidValue)
	bare := amountMod("salary-a", 0, 1)
	bare.Property = model.PropAddItem
	add(demodata.ProjNewJob, 0, bare, ErrInvalidValue)
}

func TestModTargetAddedLaterIsRefused(t *testing.T) {
	st, rec := seed(t)
	item := model.BudgetItem{Name: "Gift", Kind: model.ItemIncome, Amount: 100, Account: demodata.Checking, Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: model.D(2028, 1, 1)}}
	res, err := AddItem(st, rec, demodata.ProjNewJob, item, Options{})
	if err != nil {
		t.Fatal(err)
	}
	n := len(res.Projection.Mods)
	if _, err := InsertMod(st, res.Record, demodata.ProjNewJob, n-1, amountMod("gift", model.D(2029, 1, 1), 1), Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("before the add_item: %v", err)
	}
	if _, err := AddMod(st, res.Record, demodata.ProjNewJob, amountMod("gift", model.D(2029, 1, 1), 1), Options{DryRun: true}); err != nil {
		t.Errorf("after the add_item: %v", err)
	}
}

func TestNoNewDangling(t *testing.T) {
	// "Withdraw $100k" adds early-401k-withdrawal; a child ends it.
	setup := func(t *testing.T) (*store.Store, *model.Record) {
		st, rec := seed(t)
		res, err := NewProjection(st, rec, "Child", demodata.ProjWithdraw, Options{})
		if err != nil {
			t.Fatal(err)
		}
		res, err = AddMod(st, res.Record, "Child", amountMod("early-401k-withdrawal", model.D(2027, 6, 1), 5000), Options{})
		if err != nil {
			t.Fatal(err)
		}
		return st, res.Record
	}
	st, rec := setup(t)
	if _, err := RemoveMod(st, rec, demodata.ProjWithdraw, 0, Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("remove the add_item: %v", err)
	}
	if _, err := SetParent(st, rec, "Child", demodata.ProjBase, Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("reparent: %v", err)
	}
	if _, err := SetParent(st, rec, demodata.ProjWithdraw, "Child", Options{}); !errors.Is(err, model.ErrCycle) {
		t.Errorf("cycle: %v", err)
	}
	// Moving the amount mod is fine; removing it is fine.
	if _, err := RemoveMod(st, rec, "Child", 0, Options{DryRun: true}); err != nil {
		t.Errorf("remove the amount mod: %v", err)
	}
	// A mod that dangled before does not block an edit.
	rec.Projection("Child").Mods = append(rec.Projection("Child").Mods, amountMod("ghost", model.D(2030, 1, 1), 1))
	if _, err := SetStart(st, rec, "Child", model.Ptr(model.D(2031, 1, 1)), Options{DryRun: true}); err != nil {
		t.Errorf("dangling before: %v", err)
	}
	if _, err := RemoveMod(st, rec, "Child", 1, Options{DryRun: true}); err != nil {
		t.Errorf("remove dangling: %v", err)
	}
	// Renaming or cloning a projection with a mod that dangled before is fine, and so is doing so to its child.
	if res, err := NewProjection(st, rec, "Grandchild", "Child", Options{}); err != nil {
		t.Errorf("new child: %v", err)
	} else if _, err := RenameProjection(st, res.Record, "Child", "Kid", Options{DryRun: true}); err != nil {
		t.Errorf("rename with a dangling mod: %v", err)
	} else if _, err := CloneProjection(st, res.Record, "Child", "Copy", Options{DryRun: true}); err != nil {
		t.Errorf("clone with a dangling mod: %v", err)
	} else if _, err := CloneProjection(st, res.Record, "Grandchild", "Copy", Options{DryRun: true}); err != nil {
		t.Errorf("clone a child of a dangling projection: %v", err)
	}
	// Moving an amount mod before its add_item in the same projection is refused.
	res, err := AddItem(st, rec, "Child", model.BudgetItem{Name: "Gift", Kind: model.ItemIncome, Amount: 1, Account: demodata.Checking, Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: model.D(2028, 1, 1)}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err = EndItem(st, res.Record, "Child", "gift", model.D(2029, 1, 1), Options{})
	if err != nil {
		t.Fatal(err)
	}
	n := len(res.Projection.Mods)
	if _, err := MoveMod(st, res.Record, "Child", n-1, n-2, Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("move before the add_item: %v", err)
	}
}

func goodItem() model.BudgetItem {
	return model.BudgetItem{
		Name: "Side Gig #1!", Kind: model.ItemIncome, Amount: 800, Account: demodata.Checking,
		Recurrence: model.Recurrence{Kind: model.Monthly, Day: 15}, Start: model.Ptr(model.D(2027, 1, 1)),
	}
}

func TestAddItem(t *testing.T) {
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return AddItem(st, rec, demodata.ProjNewJob, goodItem(), o)
	})
	st, rec := seed(t)
	res, err := AddItem(st, rec, demodata.ProjNewJob, goodItem(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	mods := reload(t, st).Projection(demodata.ProjNewJob).Mods
	m := mods[len(mods)-1]
	if m.Property != model.PropAddItem || m.Target != (model.ObjectRef{Kind: model.KindBudgetItem, ID: "side-gig-1"}) || m.Item == nil || m.Item.ID != "side-gig-1" || m.Item.Amount != 800 {
		t.Errorf("added %+v", m)
	}
	// The same name gets the next free id.
	res, err = AddItem(st, res.Record, demodata.ProjWithdraw, goodItem(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Projection.Mods[len(res.Projection.Mods)-1].Item.ID; got != "side-gig-1-2" {
		t.Errorf("id %q", got)
	}
	// A transfer with one outside account.
	tr := model.BudgetItem{ID: "to-friend", Name: "Gift", Kind: model.ItemTransfer, Amount: 50, From: demodata.Checking, To: "outside", Recurrence: model.Recurrence{Kind: model.Annual, Month: 12, Day: 25}}
	if _, err := AddItem(st, rec, demodata.ProjNewJob, tr, Options{DryRun: true}); err != nil {
		t.Errorf("transfer to outside: %v", err)
	}

	refuseItem := func(want error, change func(it *model.BudgetItem)) {
		t.Helper()
		it := goodItem()
		change(&it)
		refused(t, want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddItem(st, rec, demodata.ProjNewJob, it, Options{})
		})
	}
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Name = " " })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Amount = 0 })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Amount = 1e16 })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Kind = "gift" })
	refuseItem(ErrUnknownTarget, func(it *model.BudgetItem) { it.Account = "nope" })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Recurrence.Day = 32 })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Recurrence = model.Recurrence{Kind: model.Annual, Month: 13, Day: 1} })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) {
		it.Recurrence = model.Recurrence{Kind: model.Semimonthly, Day: 1, Day2: 15}
	})
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.End = model.Ptr(model.D(2026, 1, 1)) })
	refuseItem(ErrNameTaken, func(it *model.BudgetItem) { it.ID = "salary-a" })
	refuseItem(ErrNameTaken, func(it *model.BudgetItem) { it.ID = "early-401k-withdrawal" })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) {
		*it = model.BudgetItem{Name: "x", Kind: model.ItemTransfer, Amount: 1, From: demodata.Checking, To: demodata.Checking, Recurrence: it.Recurrence}
	})
	refuseItem(ErrUnknownTarget, func(it *model.BudgetItem) {
		*it = model.BudgetItem{Name: "x", Kind: model.ItemTransfer, Amount: 1, From: "a", To: "b", Recurrence: it.Recurrence}
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return AddItem(st, rec, "Nowhere", goodItem(), Options{})
	})
}

func TestEditItem(t *testing.T) {
	op := func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		it := *rec.Projection(demodata.ProjWithdraw).Mods[0].Item
		it.Amount = 90000
		return EditItem(st, rec, demodata.ProjWithdraw, "early-401k-withdrawal", it, o)
	}
	dryRunMatches(t, op)
	st, rec := seed(t)
	if _, err := op(st, rec, Options{}); err != nil {
		t.Fatal(err)
	}
	m := reload(t, st).Projection(demodata.ProjWithdraw).Mods[0]
	if m.Item.Amount != 90000 || m.Item.ID != "early-401k-withdrawal" || m.Target.ID != "early-401k-withdrawal" {
		t.Errorf("edited %+v", m.Item)
	}
	refused(t, ErrUnknownTarget, func(st *store.Store, rec *model.Record) (*Result, error) {
		return EditItem(st, rec, demodata.ProjNewJob, "early-401k-withdrawal", goodItem(), Options{})
	})
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		it := goodItem()
		it.ID = "other"
		return EditItem(st, rec, demodata.ProjWithdraw, "early-401k-withdrawal", it, Options{})
	})
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		it := goodItem()
		it.Amount = -1
		return EditItem(st, rec, demodata.ProjWithdraw, "early-401k-withdrawal", it, Options{})
	})
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return EditItem(st, rec, demodata.ProjWithdraw, "early-401k-withdrawal", *rec.Projection(demodata.ProjWithdraw).Mods[0].Item, Options{})
	})
}

func TestEndItem(t *testing.T) {
	from := model.D(2028, 1, 1)
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return EndItem(st, rec, demodata.ProjNewJob, "salary-a", from, o)
	})
	st, rec := seed(t)
	if _, err := EndItem(st, rec, demodata.ProjNewJob, "salary-a", from, Options{}); err != nil {
		t.Fatal(err)
	}
	mods := reload(t, st).Projection(demodata.ProjNewJob).Mods
	m := mods[len(mods)-1]
	if segs := m.Values.Segments(); m.Property != model.PropAmount || len(segs) != 1 || segs[0].From != from || segs[0].Value != 0 || segs[0].End != nil {
		t.Errorf("end mod %+v", m)
	}
	// An item a parent adds can be ended from a child.
	res, err := NewProjection(st, rec, "Child", demodata.ProjWithdraw, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EndItem(st, res.Record, "Child", "early-401k-withdrawal", from, Options{DryRun: true}); err != nil {
		t.Errorf("parent's item: %v", err)
	}
	refused(t, ErrUnknownTarget, func(st *store.Store, rec *model.Record) (*Result, error) {
		return EndItem(st, rec, demodata.ProjNewJob, "nope", from, Options{})
	})
}

func TestCheckItemNeedsDateAndValidDay(t *testing.T) {
	st, rec := seed(t)
	base := model.BudgetItem{Name: "Gift", Kind: model.ItemIncome, Amount: 1, Account: demodata.Checking}
	for _, r := range []model.Recurrence{
		{Kind: model.OneTime},
		{Kind: model.Biweekly},
		{Kind: model.Annual, Month: 13, Day: 1},
		{Kind: model.Annual, Month: 2, Day: 30},
		{Kind: model.Monthly, Day: 32},
	} {
		it := base
		it.Recurrence = r
		if _, err := AddItem(st, rec, demodata.ProjBase, it, Options{DryRun: true}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("recurrence %+v: %v", r, err)
		}
	}
}
