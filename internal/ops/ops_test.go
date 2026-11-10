// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
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

func TestSetInflationKeepsScaleMods(t *testing.T) {
	st, rec := seed(t)
	scale := model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Mode: model.ModeScale, Values: model.Constant(1.2)}
	res, err := AddMod(st, rec, demodata.ProjWithdraw, scale, Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err = SetInflation(st, res.Record, demodata.ProjWithdraw, model.Ptr(0.03), Options{})
	if err != nil {
		t.Fatal(err)
	}
	mods := res.Record.Projection(demodata.ProjWithdraw).Mods
	var got []model.Modification
	for _, m := range mods {
		if m.Target.Kind == model.KindGlobal && m.Property == model.PropInflation {
			got = append(got, m)
		}
	}
	if len(got) != 2 || got[0].IsScale() || got[0].Values.Segments()[0].Value != 0.03 || !got[1].IsScale() {
		t.Fatalf("got %+v", got)
	}
	res, err = SetInflation(st, res.Record, demodata.ProjWithdraw, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Record.Projection(demodata.ProjWithdraw).Mods {
		if m.Target.Kind == model.KindGlobal && m.Property == model.PropInflation && !m.IsScale() {
			t.Fatalf("set modification kept: %+v", m)
		}
	}
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
	if _, err := InsertMod(st, rec, "Child", 0, amountMod("nope", model.D(2030, 1, 1), 1), Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("unknown item under child: %v", err)
	}
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
	item := model.BudgetItem{Name: "Gift", Kind: model.ItemIncome, Amount: model.Constant(100), Account: demodata.Checking, Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: model.D(2028, 1, 1)}}
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
	res, err := AddItem(st, rec, "Child", model.BudgetItem{Name: "Gift", Kind: model.ItemIncome, Amount: model.Constant(1), Account: demodata.Checking, Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: model.D(2028, 1, 1)}}, Options{})
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

// sameSchedule reports whether the schedules have the same segments.
func sameSchedule(a, b model.Schedule) bool { return reflect.DeepEqual(a.Segments(), b.Segments()) }

func goodItem() model.BudgetItem {
	return model.BudgetItem{
		Name: "Side Gig #1!", Kind: model.ItemIncome, Amount: model.Constant(800), Account: demodata.Checking,
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
	if m.Property != model.PropAddItem || m.Target != (model.ObjectRef{Kind: model.KindBudgetItem, ID: "side-gig-1"}) || m.Item == nil || m.Item.ID != "side-gig-1" || !sameSchedule(m.Item.Amount, model.Constant(800)) {
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
	tr := model.BudgetItem{ID: "to-friend", Name: "Gift", Kind: model.ItemTransfer, Amount: model.Constant(50), From: demodata.Checking, To: "outside", Recurrence: model.Recurrence{Kind: model.Annual, Month: 12, Day: 25}, Start: model.Ptr(model.D(2027, 1, 1))}
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
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Amount = model.Constant(0) })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Amount = model.Constant(1e16) })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Kind = "gift" })
	refuseItem(ErrUnknownTarget, func(it *model.BudgetItem) { it.Account = "nope" })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Recurrence.Day = 32 })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.Recurrence = model.Recurrence{Kind: model.Annual, Month: 13, Day: 1} })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) {
		it.Recurrence = model.Recurrence{Kind: model.Semimonthly, Day: 1, Day2: 1}
	})
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) { it.End = model.Ptr(model.D(2026, 1, 1)) })
	refuseItem(ErrNameTaken, func(it *model.BudgetItem) { it.ID = "salary-a" })
	refuseItem(ErrNameTaken, func(it *model.BudgetItem) { it.ID = "early-401k-withdrawal" })
	refuseItem(ErrInvalidValue, func(it *model.BudgetItem) {
		*it = model.BudgetItem{Name: "x", Kind: model.ItemTransfer, Amount: model.Constant(1), From: demodata.Checking, To: demodata.Checking, Recurrence: it.Recurrence, Start: it.Start}
	})
	refuseItem(ErrUnknownTarget, func(it *model.BudgetItem) {
		*it = model.BudgetItem{Name: "x", Kind: model.ItemTransfer, Amount: model.Constant(1), From: "a", To: "b", Recurrence: it.Recurrence, Start: it.Start}
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return AddItem(st, rec, "Nowhere", goodItem(), Options{})
	})
}

func TestEditItem(t *testing.T) {
	op := func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		it := *rec.Projection(demodata.ProjWithdraw).Mods[0].Item
		it.Amount = model.Constant(90000)
		return EditItem(st, rec, demodata.ProjWithdraw, "early-401k-withdrawal", it, o)
	}
	dryRunMatches(t, op)
	st, rec := seed(t)
	if _, err := op(st, rec, Options{}); err != nil {
		t.Fatal(err)
	}
	m := reload(t, st).Projection(demodata.ProjWithdraw).Mods[0]
	if !sameSchedule(m.Item.Amount, model.Constant(90000)) || m.Item.ID != "early-401k-withdrawal" || m.Target.ID != "early-401k-withdrawal" {
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
		it.Amount = model.Constant(-1)
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
	base := model.BudgetItem{Name: "Gift", Kind: model.ItemIncome, Amount: model.Constant(1), Account: demodata.Checking, Start: model.Ptr(model.D(2027, 1, 1))}
	for _, r := range []model.Recurrence{
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
	// An item with no start and no anchor is refused.
	for _, r := range []model.Recurrence{{Kind: model.OneTime}, {Kind: model.Biweekly}, {Kind: model.Monthly, Day: 1}} {
		it := base
		it.Start, it.Recurrence = nil, r
		if _, err := AddItem(st, rec, demodata.ProjBase, it, Options{DryRun: true}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("no start, recurrence %+v: %v", r, err)
		}
	}
}

func TestSetMod(t *testing.T) {
	// New job: mod 0 is an amount of salary-a, mod 1 the growth of the brokerage.
	to := model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "utilities"}, Property: model.PropAmount, Values: model.Constant(500)}
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetMod(st, rec, demodata.ProjNewJob, 0, to, o)
	})
	st, rec := seed(t)
	if _, err := SetMod(st, rec, demodata.ProjNewJob, 0, to, Options{}); err != nil {
		t.Fatal(err)
	}
	mods := reload(t, st).Projection(demodata.ProjNewJob).Mods
	if !reflect.DeepEqual(mods[0], to) || len(mods) != len(rec.Projection(demodata.ProjNewJob).Mods) {
		t.Errorf("mods %+v", mods)
	}
	// The kind and object of a modification can change with the property.
	acct := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Checking)}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	if _, err := SetMod(st, rec, demodata.ProjNewJob, 1, acct, Options{DryRun: true}); err != nil {
		t.Errorf("account to account: %v", err)
	}
	global := model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Values: model.Constant(0.02)}
	if _, err := SetMod(st, rec, demodata.ProjNewJob, 1, global, Options{DryRun: true}); err != nil {
		t.Errorf("account to global: %v", err)
	}

	refuseSet := func(want error, proj string, index int, m model.Modification) {
		t.Helper()
		refused(t, want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return SetMod(st, rec, proj, index, m, Options{})
		})
	}
	noObject := to
	noObject.Target.ID = ""
	refuseSet(ErrNoObject, demodata.ProjNewJob, 0, noObject)
	wrongProp := to
	wrongProp.Property = model.PropInflation
	refuseSet(ErrInvalidValue, demodata.ProjNewJob, 0, wrongProp)
	unknown := to
	unknown.Target.ID = "nope"
	refuseSet(ErrUnknownTarget, demodata.ProjNewJob, 0, unknown)
	big := to
	big.Values = model.Constant(1e16)
	refuseSet(ErrInvalidValue, demodata.ProjNewJob, 0, big)
	refuseSet(ErrUnknownProjection, "Nowhere", 0, to)

	// An item added by this projection's later modification does not exist yet at an earlier one.
	own := model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "side-gig-1"}, Property: model.PropAmount, Values: model.Constant(900)}
	res, err := AddItem(st, rec, demodata.ProjNewJob, goodItem(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	n := len(res.Projection.Mods)
	if _, err := SetMod(st, res.Record, demodata.ProjNewJob, 0, own, Options{DryRun: true}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("item before its addition: %v", err)
	}
	// The item exists after its addition, so a modification there may point at it.
	later, err := AddMod(st, res.Record, demodata.ProjNewJob, to, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetMod(st, later.Record, demodata.ProjNewJob, n, own, Options{DryRun: true}); err != nil {
		t.Errorf("item after its addition: %v", err)
	}

	// An add_item modification is not replaced, and nothing becomes one.
	add := res.Projection.Mods[n-1]
	if _, err := SetMod(st, res.Record, demodata.ProjNewJob, n-1, to, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("replacing add_item: %v", err)
	}
	if _, err := SetMod(st, res.Record, demodata.ProjNewJob, 0, add, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("replacing with add_item: %v", err)
	}
	if _, err := SetMod(st, res.Record, demodata.ProjNewJob, 99, to, Options{}); err == nil {
		t.Error("index out of range accepted")
	}
}

func TestCheckModAndCheckItem(t *testing.T) {
	_, rec := seed(t)
	good := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Savings)}, Property: model.PropGrowth, Values: model.Constant(0.04)}
	if err := CheckMod(rec, demodata.ProjNewJob, good); err != nil {
		t.Errorf("good: %v", err)
	}
	noObject := good
	noObject.Target.ID = ""
	unknown := good
	unknown.Target.ID = "nope"
	wrongProp := good
	wrongProp.Property = model.PropAmount
	huge := good
	huge.Values = model.Constant(1e16)
	// A parent's added item exists in a child, not in a sibling.
	ofParent := model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "early-401k-withdrawal"}, Property: model.PropAmount, Values: model.Constant(1)}
	for _, c := range []struct {
		what string
		proj string
		m    model.Modification
		want error
	}{
		{"no object", demodata.ProjNewJob, noObject, ErrNoObject},
		{"unknown account", demodata.ProjNewJob, unknown, ErrUnknownTarget},
		{"wrong property", demodata.ProjNewJob, wrongProp, ErrInvalidValue},
		{"huge value", demodata.ProjNewJob, huge, ErrInvalidValue},
		{"item of a sibling", demodata.ProjNewJob, ofParent, ErrUnknownTarget},
		{"item of its own", demodata.ProjWithdraw, ofParent, nil},
		{"unknown projection", "Nowhere", good, ErrUnknownProjection},
	} {
		if err := CheckMod(rec, c.proj, c.m); !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Errorf("%s: %v, want %v", c.what, err, c.want)
		}
	}
	if err := CheckItem(rec, goodItem()); err != nil {
		t.Errorf("good item: %v", err)
	}
	it := goodItem()
	it.Name = ""
	if err := CheckItem(rec, it); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("nameless item: %v", err)
	}
	it = goodItem()
	it.Account = "nope"
	if err := CheckItem(rec, it); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("unknown account: %v", err)
	}
}

func TestItemsBefore(t *testing.T) {
	st, rec := seed(t)
	base := len(rec.Budget.Items)
	res, err := NewProjection(st, rec, "Child", demodata.ProjWithdraw, Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err = AddItem(st, res.Record, "Child", goodItem(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	ids := func(index int) []string {
		t.Helper()
		items, err := ItemsBefore(rec, "Child", index)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, a := range items {
			if a.AddedBy != "" {
				out = append(out, a.Item.ID+" by "+a.AddedBy)
			}
		}
		if len(items) < base {
			t.Fatalf("lost record items: %d", len(items))
		}
		return out
	}
	if got := ids(0); !reflect.DeepEqual(got, []string{"early-401k-withdrawal by " + demodata.ProjWithdraw}) {
		t.Errorf("start of the list: %v", got)
	}
	if got := ids(1); !reflect.DeepEqual(got, []string{"early-401k-withdrawal by " + demodata.ProjWithdraw, "side-gig-1 by Child"}) {
		t.Errorf("end of the list: %v", got)
	}
	if _, err := ItemsBefore(rec, "Child", 2); err == nil {
		t.Error("index past the end accepted")
	}
	if _, err := ItemsBefore(rec, "Nowhere", 0); !errors.Is(err, ErrUnknownProjection) {
		t.Errorf("unknown projection: %v", err)
	}
	// The siblings of the parent see none of it.
	if items, _ := ItemsBefore(rec, demodata.ProjNewJob, 0); len(items) != base {
		t.Errorf("sibling sees %d items, want %d", len(items), base)
	}
}

func TestEndModAndEndedOn(t *testing.T) {
	from := model.D(2028, 1, 1)
	m := EndMod("salary-a", from)
	if d, ok := EndedOn(m); !ok || d != from {
		t.Errorf("EndedOn(EndMod) = %v, %v", d, ok)
	}
	for what, change := range map[string]func(m *model.Modification){
		"a value": func(m *model.Modification) { m.Values = model.MustSchedule(model.Segment{From: from, Value: 5}) },
		"always":  func(m *model.Modification) { m.Values = model.Constant(0) },
		"an end": func(m *model.Modification) {
			m.Values = model.MustSchedule(model.Segment{From: from, End: model.Ptr(from.Add(9)), Value: 0})
		},
		"a property": func(m *model.Modification) { m.Property = model.PropGrowth },
		"an account": func(m *model.Modification) { m.Target.Kind = model.KindAccount },
	} {
		x := EndMod("salary-a", from)
		change(&x)
		if _, ok := EndedOn(x); ok {
			t.Errorf("%s counts as an end", what)
		}
	}
}

func TestScaleAndGrowthMods(t *testing.T) {
	scale := amountMod("salary-a", model.D(2030, 1, 1), 0.7)
	scale.Mode = model.ModeScale
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return AddMod(st, rec, demodata.ProjNewJob, scale, o)
	})
	st, rec := seed(t)
	n := len(rec.Projection(demodata.ProjNewJob).Mods)
	if _, err := AddMod(st, rec, demodata.ProjNewJob, scale, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Mods[n]; !reflect.DeepEqual(got, scale) {
		t.Errorf("stored %+v", got)
	}
	// The default mode is stored as the empty mode, however it is written.
	set := amountMod("salary-a", model.D(2030, 1, 1), 7000)
	set.Mode = model.ModeSet
	res, err := AddMod(st, rec, demodata.ProjNewJob, set, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Projection.Mods[n].Mode; got != "" {
		t.Errorf("set stored as %q", got)
	}

	growth := model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "salary-a"}, Property: model.PropGrowth, Values: model.Constant(0.03)}
	if _, err := AddMod(st, rec, demodata.ProjNewJob, growth, Options{DryRun: true}); err != nil {
		t.Errorf("growth of an item: %v", err)
	}
	growth.Mode, growth.Values = model.ModeScale, model.Constant(1.5)
	if _, err := AddMod(st, rec, demodata.ProjNewJob, growth, Options{DryRun: true}); err != nil {
		t.Errorf("scaled growth of an item: %v", err)
	}
	for name, tc := range map[string]struct {
		m    model.Modification
		want error
	}{
		"negative scale":     {func() model.Modification { m := scale; m.Values = model.Constant(-0.1); return m }(), ErrInvalidValue},
		"scale not a number": {func() model.Modification { m := scale; m.Values = model.Constant(1e16); return m }(), ErrInvalidValue},
		"unknown mode":       {func() model.Modification { m := scale; m.Mode = "mix"; return m }(), ErrInvalidValue},
		"raise of -100%":     {model.Modification{Target: growth.Target, Property: model.PropGrowth, Values: model.Constant(-1)}, ErrInvalidValue},
		"scale of add_item":  {model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "x"}, Property: model.PropAddItem, Mode: model.ModeScale}, ErrInvalidValue},
		"item inflation":     {model.Modification{Target: growth.Target, Property: model.PropInflation, Values: model.Constant(0.1)}, ErrInvalidValue},
	} {
		t.Run(name, func(t *testing.T) {
			refused(t, tc.want, func(st *store.Store, rec *model.Record) (*Result, error) {
				return AddMod(st, rec, demodata.ProjNewJob, tc.m, Options{})
			})
		})
	}
}

func TestSetModModeValues(t *testing.T) {
	st, rec := seed(t)
	before := rec.Projection(demodata.ProjNewJob).Mods[0].Values
	if _, err := SetModMode(st, rec, demodata.ProjNewJob, 0, model.ModeScale, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	got := rec.Projection(demodata.ProjNewJob).Mods[0]
	if got.Mode != model.ModeScale || !reflect.DeepEqual(got.Values, before) {
		t.Errorf("nil values: mode %q values %v, want scale and %v", got.Mode, got.Values, before)
	}
	v := model.Constant(1.5)
	if _, err := SetModMode(st, rec, demodata.ProjNewJob, 0, model.ModeSet, &v, Options{}); err != nil {
		t.Fatal(err)
	}
	got = reload(t, st).Projection(demodata.ProjNewJob).Mods[0]
	if got.Mode != "" || !reflect.DeepEqual(got.Values, v) {
		t.Errorf("values: mode %q values %v, want set and %v", got.Mode, got.Values, v)
	}
}

func TestSetModMode(t *testing.T) {
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetModMode(st, rec, demodata.ProjNewJob, 0, model.ModeScale, nil, o)
	})
	st, rec := seed(t)
	if _, err := SetModMode(st, rec, demodata.ProjNewJob, 0, model.ModeScale, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if got := rec.Projection(demodata.ProjNewJob).Mods[0]; got.Mode != model.ModeScale {
		t.Errorf("mode %q", got.Mode)
	}
	if _, err := SetModMode(st, rec, demodata.ProjNewJob, 0, model.ModeSet, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjNewJob).Mods[0].Mode; got != "" {
		t.Errorf("set stored as %q", got)
	}
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetModMode(st, rec, demodata.ProjNewJob, 0, model.ModeSet, nil, Options{})
	})
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetModMode(st, rec, demodata.ProjNewJob, 0, "mix", nil, Options{})
	})
	// A replacement that scales by less than zero is refused.
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetMod(st, rec, demodata.ProjNewJob, 0, model.Modification{
			Target: rec.Projection(demodata.ProjNewJob).Mods[0].Target, Property: rec.Projection(demodata.ProjNewJob).Mods[0].Property,
			Mode: model.ModeScale, Values: model.Constant(-1),
		}, Options{})
	})
	for _, p := range rec.Projections {
		for i, m := range p.Mods {
			if m.Property == model.PropAddItem {
				refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
					return SetModMode(st, rec, p.Name, i, model.ModeScale, nil, Options{})
				})
				return
			}
		}
	}
	t.Error("no add_item modification to refuse")
}

func TestItemGrowth(t *testing.T) {
	st, rec := seed(t)
	it := goodItem()
	it.Growth = model.Constant(0.03)
	res, err := AddItem(st, rec, demodata.ProjNewJob, it, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mods := reload(t, st).Projection(demodata.ProjNewJob).Mods
	if got := mods[len(mods)-1].Item.Growth.Segments(); len(got) != 1 || got[0].Value != 0.03 {
		t.Errorf("growth %v", got)
	}
	edited := *res.Projection.Mods[len(res.Projection.Mods)-1].Item
	edited.Growth = model.Constant(0.04)
	if _, err := EditItem(st, res.Record, demodata.ProjNewJob, edited.ID, edited, Options{}); err != nil {
		t.Fatal(err)
	}
	mods = reload(t, st).Projection(demodata.ProjNewJob).Mods
	if got := mods[len(mods)-1].Item.Growth.Segments(); len(got) != 1 || got[0].Value != 0.04 {
		t.Errorf("edited growth %v", got)
	}
	for _, g := range []float64{-1, -2, 1e16} {
		bad := goodItem()
		bad.Growth = model.Constant(g)
		refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddItem(st, rec, demodata.ProjNewJob, bad, Options{})
		})
	}
	base := rec.Budget.Items[0]
	base.Growth = model.Constant(0.02)
	if _, err := EditBudgetItem(st, rec, base.ID, base, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Budget.Items[0].Growth.Segments(); len(got) != 1 || got[0].Value != 0.02 {
		t.Errorf("base growth %v", got)
	}
}

func TestAddModDefaultBalanceTargets(t *testing.T) {
	end := model.D(2024, 6, 30)
	ranged := model.MustSchedule(model.Segment{From: model.D(2024, 1, 1), End: &end, Value: 250000})
	mod := func(kind model.ObjectKind, id, prop string, mode model.Mode, vals model.Schedule) model.Modification {
		return model.Modification{Target: model.ObjectRef{Kind: kind, ID: id}, Property: prop, Mode: mode, Values: vals}
	}
	for _, m := range []model.Modification{
		mod(model.KindAccount, string(demodata.Home), model.PropDefault, "", ranged),
		mod(model.KindAsset, "retirement", model.PropDefault, model.ModeSet, ranged),
		mod(model.KindAccount, string(demodata.Home), model.PropDefault, model.ModeScale, model.Constant(0.8)),
		mod(model.KindAccount, string(demodata.Card), model.PropDefault, "", model.Constant(-4000)),
	} {
		st, rec := seed(t)
		if _, err := AddMod(st, rec, demodata.ProjNewJob, m, Options{}); err != nil {
			t.Errorf("%+v: %v", m, err)
		}
	}
	for _, m := range []model.Modification{
		mod(model.KindBudgetItem, "salary-a", model.PropDefault, "", ranged),
		mod(model.KindGlobal, "", model.PropDefault, "", ranged),
		mod(model.KindAccount, string(demodata.Home), model.PropAmount, "", ranged),
		mod(model.KindAccount, string(demodata.Home), model.PropDefault, model.ModeScale, model.Constant(-1)),
	} {
		refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddMod(st, rec, demodata.ProjNewJob, m, Options{})
		})
	}
	refused(t, ErrUnknownTarget, func(st *store.Store, rec *model.Record) (*Result, error) {
		return AddMod(st, rec, demodata.ProjNewJob, mod(model.KindAccount, "nope", model.PropDefault, "", ranged), Options{})
	})
}

func TestItemRecurrenceAndScheduleRules(t *testing.T) {
	st, rec := seed(t)
	check := func(name string, change func(it *model.BudgetItem), want error) {
		t.Helper()
		it := goodItem()
		change(&it)
		_, err := AddItem(st, rec, demodata.ProjNewJob, it, Options{DryRun: true})
		if want == nil && err != nil || want != nil && !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", name, err, want)
		}
	}
	rule := func(r model.Recurrence) func(it *model.BudgetItem) {
		return func(it *model.BudgetItem) { it.Recurrence = r }
	}
	check("weekly", rule(model.Recurrence{Kind: model.Weekly}), nil)
	check("quarterly", rule(model.Recurrence{Kind: model.Quarterly}), nil)
	check("semimonthly", rule(model.Recurrence{Kind: model.Semimonthly, Day: 1, Day2: 15}), nil)
	check("semimonthly same days", rule(model.Recurrence{Kind: model.Semimonthly, Day: 15, Day2: 15}), ErrInvalidValue)
	check("semimonthly day 0", rule(model.Recurrence{Kind: model.Semimonthly, Day: 0, Day2: 15}), ErrInvalidValue)
	check("semimonthly day2 32", rule(model.Recurrence{Kind: model.Semimonthly, Day: 1, Day2: 32}), ErrInvalidValue)
	check("custom", rule(model.Recurrence{Kind: model.Custom, Count: 3, Unit: model.UnitWeeks}), nil)
	check("custom count 0", rule(model.Recurrence{Kind: model.Custom, Count: 0, Unit: model.UnitWeeks}), ErrInvalidValue)
	check("custom no unit", rule(model.Recurrence{Kind: model.Custom, Count: 2}), ErrInvalidValue)
	check("custom bad unit", rule(model.Recurrence{Kind: model.Custom, Count: 2, Unit: "fortnights"}), ErrInvalidValue)
	check("unknown kind", rule(model.Recurrence{Kind: "bimonthly"}), ErrInvalidValue)
	check("no start", func(it *model.BudgetItem) { it.Start = nil }, ErrInvalidValue)
	check("no amount", func(it *model.BudgetItem) { it.Amount = model.Schedule{} }, ErrInvalidValue)
	check("amount segment zero", func(it *model.BudgetItem) {
		it.Amount = model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(model.D(2028, 1, 31)), Value: 5}, model.Segment{From: model.D(2028, 2, 1), Value: 0})
	}, ErrInvalidValue)
	check("amount segments", func(it *model.BudgetItem) {
		it.Amount = model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(model.D(2028, 1, 31)), Value: 1400}, model.Segment{From: model.D(2028, 2, 1), Value: 600})
	}, nil)
	check("growth segment too low", func(it *model.BudgetItem) {
		it.Growth = model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(model.D(2028, 1, 31)), Value: 0.03}, model.Segment{From: model.D(2028, 2, 1), Value: -1})
	}, ErrInvalidValue)
	check("growth segments", func(it *model.BudgetItem) {
		it.Growth = model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(model.D(2028, 1, 31)), Value: 0.03}, model.Segment{From: model.D(2028, 2, 1), Value: -0.5})
	}, nil)
	check("end before start", func(it *model.BudgetItem) { it.End = model.Ptr(model.D(2026, 12, 31)) }, ErrInvalidValue)
	check("end on start", func(it *model.BudgetItem) { it.End = model.Ptr(*it.Start) }, nil)
}

func TestEditItemKeepsAbsentStart(t *testing.T) {
	st, rec := seed(t)
	it := *rec.Projection(demodata.ProjWithdraw).Mods[0].Item
	it.Start = nil
	it.Amount = model.Constant(1)
	if _, err := EditItem(st, rec, demodata.ProjWithdraw, "early-401k-withdrawal", it, Options{DryRun: true}); err != nil {
		t.Errorf("legacy item without a start: %v", err)
	}
	// A base item with a start may not lose it.
	salary, _ := rec.Budget.Item("salary-a")
	if _, err := EditBudgetItem(st, rec, "salary-a", salary, Options{DryRun: true}); !errors.Is(err, ErrNoChange) {
		t.Errorf("unchanged legacy item: %v", err)
	}
	// Switching a legacy item to a recurrence that needs a phase needs a date.
	for _, c := range []struct {
		name string
		r    model.Recurrence
		want error
	}{
		{"one-time", model.Recurrence{Kind: model.OneTime}, ErrInvalidValue},
		{"quarterly", model.Recurrence{Kind: model.Quarterly}, ErrInvalidValue},
		{"biweekly with anchor", model.Recurrence{Kind: model.Biweekly, Anchor: model.D(2027, 1, 8)}, nil},
	} {
		sal := salary
		sal.Recurrence = c.r
		if _, err := EditBudgetItem(st, rec, "salary-a", sal, Options{DryRun: true}); !errors.Is(err, c.want) {
			t.Errorf("legacy base item to %s: %v", c.name, err)
		}
		pit := it
		pit.Recurrence = c.r
		if _, err := EditItem(st, rec, demodata.ProjWithdraw, "early-401k-withdrawal", pit, Options{DryRun: true}); !errors.Is(err, c.want) {
			t.Errorf("legacy projection item to %s: %v", c.name, err)
		}
	}
	groceries, _ := rec.Budget.Item("groceries")
	groceries.Start = model.Ptr(model.D(2027, 1, 1))
	res, err := EditBudgetItem(st, rec, "groceries", groceries, Options{})
	if err != nil {
		t.Fatal(err)
	}
	next := res.Record
	groceries.Start = nil
	if _, err := EditBudgetItem(st, next, "groceries", groceries, Options{DryRun: true}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("dropping the start: %v", err)
	}
}

func TestLegacyItemEndBeforeAnchor(t *testing.T) {
	st, rec := seed(t)
	it := *rec.Projection(demodata.ProjWithdraw).Mods[0].Item
	it.Start = nil
	it.End = model.Ptr(model.D(2027, 1, 1))
	if _, err := EditItem(st, rec, demodata.ProjWithdraw, "early-401k-withdrawal", it, Options{DryRun: true}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("ending the withdrawal before its anchor: %v", err)
	}
	for _, r := range []model.Recurrence{
		{Kind: model.OneTime, Anchor: model.D(2027, 6, 1)},
		{Kind: model.Biweekly, Anchor: model.D(2027, 6, 1)},
	} {
		next := rec.Clone()
		base := model.BudgetItem{ID: "legacy", Name: "Legacy", Kind: model.ItemIncome, Amount: model.Constant(100), Account: demodata.Checking, Recurrence: r}
		next.Budget.Items = append(next.Budget.Items, base)
		if _, err := EndBudgetItem(st, next, "legacy", model.D(2027, 1, 1), Options{DryRun: true}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("ending a legacy %s item before its anchor: %v", r.Kind, err)
		}
		if _, err := EndBudgetItem(st, next, "legacy", model.D(2027, 6, 1), Options{DryRun: true}); err != nil {
			t.Errorf("ending a legacy %s item on its anchor: %v", r.Kind, err)
		}
	}
}

func TestDryRunChecksTheRecordCanBeRestored(t *testing.T) {
	st, rec := seed(t)
	next := rec.Clone()
	next.Projections = append(next.Projections, &model.Projection{Name: next.Projections[0].Name})
	for _, o := range []Options{{DryRun: true}, {}} {
		res, err := commit(st, next, Result{}, o)
		var se *SaveError
		if res != nil || !errors.As(err, &se) || !strings.Contains(err.Error(), "cannot be restored") {
			t.Errorf("dry run %v: %v %v", o.DryRun, res, err)
		}
	}
	if res, err := commit(st, rec.Clone(), Result{}, Options{DryRun: true}); err != nil || res == nil {
		t.Errorf("a record that can be restored: %v %v", res, err)
	}
}

func TestOneTimeItemStartMustMatchItsDate(t *testing.T) {
	st, rec := seed(t)
	item := model.BudgetItem{Name: "Gift", Kind: model.ItemIncome, Amount: model.Constant(100), Account: demodata.Checking,
		Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: model.D(2028, 2, 1)}, Start: model.Ptr(model.D(2028, 1, 1))}
	err := CheckItem(rec, item)
	if !errors.Is(err, ErrInvalidValue) || !strings.Contains(err.Error(), "2028-01-01") || !strings.Contains(err.Error(), "2028-02-01") {
		t.Errorf("start and date differ: %v", err)
	}
	if _, err := AddItem(st, rec, demodata.ProjBase, item, Options{DryRun: true}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("AddItem: %v", err)
	}
	item.Start = model.Ptr(model.D(2028, 2, 1))
	if err := CheckItem(rec, item); err != nil {
		t.Errorf("start equal to the date: %v", err)
	}
	item.Start = nil
	if err := CheckItem(rec, item); err != nil {
		t.Errorf("no start: %v", err)
	}
	// A biweekly item counts from its anchor, whatever its start.
	item.Recurrence.Kind, item.Start = model.Biweekly, model.Ptr(model.D(2028, 1, 1))
	if err := CheckItem(rec, item); err != nil {
		t.Errorf("biweekly: %v", err)
	}
}

func TestWrongModPropertyNamesTheKindWithASpace(t *testing.T) {
	st, rec := seed(t)
	m := model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "salary-a"}, Property: model.PropInflation, Values: model.Constant(1)}
	_, err := AddMod(st, rec, demodata.ProjNewJob, m, Options{DryRun: true})
	if !errors.Is(err, ErrInvalidValue) || !strings.Contains(err.Error(), "a modification of a budget item") || strings.Contains(err.Error(), "budget_item") {
		t.Errorf("%v", err)
	}
}
