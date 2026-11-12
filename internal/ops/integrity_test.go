// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

func TestSetParamRefusesAnEmptyGrowthSchedule(t *testing.T) {
	empty := model.Schedule{}
	dataRefused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetParam(st, rec, demodata.Retirement, model.PropGrowth, &empty, Options{})
	})
	st, rec := seed(t)
	_, err := SetParam(st, rec, demodata.Retirement, model.PropGrowth, &empty, Options{})
	if err == nil || !strings.Contains(err.Error(), "at least one segment") {
		t.Errorf("reason %v", err)
	}
}

func TestAssetNamesAvoidProjectionAccounts(t *testing.T) {
	st, rec := seed(t)
	acct := model.ProjAccount{ID: "nw", Name: "Rainy day", Type: model.TypeSavings, Tax: model.Taxable, Opening: 10, From: model.D(2027, 1, 1)}
	mustOp(t, func() (*Result, error) { return AddProjAccount(st, rec, demodata.ProjNewJob, acct, Options{}) })
	rec = reload(t, st)
	if _, err := NewAsset(st, rec, " rainy DAY ", "", Options{}); !errors.Is(err, ErrNameTaken) || !strings.Contains(err.Error(), demodata.ProjNewJob) {
		t.Errorf("new asset: %v", err)
	}
	if _, err := RenameAsset(st, rec, demodata.Liquid, "Rainy day", Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("rename: %v", err)
	}
	if _, err := NewAsset(st, rec, "Checking", "", Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("an account's name: %v", err)
	}
}

func TestProjAccountNamesAvoidAssets(t *testing.T) {
	st, rec := seed(t)
	clash := model.ProjAccount{ID: "nw", Name: " net WORTH ", Type: model.TypeSavings, Tax: model.Taxable, Opening: 10, From: model.D(2027, 1, 1)}
	if _, err := AddProjAccount(st, rec, demodata.ProjBase, clash, Options{}); !errors.Is(err, ErrNameTaken) || !strings.Contains(err.Error(), demodata.ProjBase) {
		t.Errorf("add: %v", err)
	}
	clash.Name = "Fine"
	mustOp(t, func() (*Result, error) { return AddProjAccount(st, rec, demodata.ProjBase, clash, Options{}) })
	rec = reload(t, st)
	name := "Liquid assets"
	if _, err := EditProjAccount(st, rec, demodata.ProjBase, "nw", ProjAccountChange{Name: &name}, Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("rename: %v", err)
	}
	opening := 20.0
	mustOp(t, func() (*Result, error) {
		return EditProjAccount(st, rec, demodata.ProjBase, "nw", ProjAccountChange{Opening: &opening}, Options{})
	})
	if _, err := NewAccount(st, rec, model.Account{ID: "nw2", Name: "net worth", Type: model.TypeSavings, Tax: model.Taxable}, Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("a record account named as an asset: %v", err)
	}
}

func TestProjAccountWithAnAssetsNameStillEdits(t *testing.T) {
	st, rec := seed(t)
	rec.Projection(demodata.ProjBase).Accounts = []model.ProjAccount{{ID: "nw", Name: "Net worth", Type: model.TypeSavings, Tax: model.Taxable, Opening: 10, From: model.D(2027, 1, 1)}}
	if err := st.Save(rec); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	opening := 30.0
	mustOp(t, func() (*Result, error) {
		return EditProjAccount(st, rec, demodata.ProjBase, "nw", ProjAccountChange{Opening: &opening}, Options{})
	})
}

// presetWith stores a preset named name with a line for each asset, a band from the first line to the last, and one from the first to the second.
func presetWith(t *testing.T, st *store.Store, name string, assets ...model.AssetID) {
	t.Helper()
	l := graph.NewLines(demodata.ProjBase)
	var ids []graph.LineID
	for i, a := range assets {
		ids = append(ids, l.Add(a, false, "", graph.RGB(uint32(i+1))))
	}
	l.AddBand(ids[0], ids[len(ids)-1], graph.RGB(9))
	l.AddBand(ids[0], ids[1], graph.RGB(8))
	putPreset(t, st, graph.Capture(name, l, 0, 1))
}

func storedByName(t *testing.T, st *store.Store, name string) graph.Preset {
	t.Helper()
	row, err := st.Preset(name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := graph.DecodePreset(row.Name, row.Format, row.Body)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDeleteAccountPrunesPresets(t *testing.T) {
	st, rec := seed(t)
	rec = withEmptyAccount(t, st, rec)
	presetWith(t, st, "Mine", demodata.NetWorth, "acct:scratch", demodata.AllCash)
	presetWith(t, st, "Other", demodata.NetWorth, demodata.AllCash)
	before, err := st.Presets()
	if err != nil {
		t.Fatal(err)
	}

	plan, err := AccountDeletePlan(st, rec, "scratch")
	if err != nil {
		t.Fatal(err)
	}
	var listed []Affected
	for _, a := range plan.Affected {
		if a.Kind == AffectedPreset {
			listed = append(listed, a)
		}
	}
	if !reflect.DeepEqual(listed, []Affected{{AffectedPreset, "Mine", "1 line, 1 band", EffectRemoved}}) {
		t.Fatalf("plan %+v", plan.Affected)
	}

	dry, err := DeleteAccount(st, rec, "scratch", Options{DryRun: true})
	if err != nil || len(dry.Pruned) != 1 {
		t.Fatalf("dry run %+v, %v", dry, err)
	}
	if after, _ := st.Presets(); !reflect.DeepEqual(after, before) {
		t.Fatal("a dry run wrote presets")
	}

	res, err := DeleteAccount(st, rec, "scratch", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pruned) != 1 || res.Pruned[0].Before.Name != "Mine" {
		t.Fatalf("pruned %+v", res.Pruned)
	}
	p := storedByName(t, st, "Mine")
	if len(p.Lines) != 2 || p.Lines[0].Asset != demodata.NetWorth || p.Lines[1].Asset != demodata.AllCash || len(p.Bands) != 1 || p.Bands[0].Upper != 0 || p.Bands[0].Lower != 1 {
		t.Errorf("Mine %+v", p)
	}
	if got, _ := st.Preset("Other"); !reflect.DeepEqual(got, before[indexOf(before, "Other")]) {
		t.Error("another preset changed")
	}
	if err := PutPresets(st, []store.Preset{res.Pruned[0].Before}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Preset("Mine"); !reflect.DeepEqual(got, before[indexOf(before, "Mine")]) {
		t.Error("the original bytes were not restored")
	}
}

func indexOf(rows []store.Preset, name string) int {
	for i, r := range rows {
		if r.Name == name {
			return i
		}
	}
	return -1
}

func TestDeleteAssetPrunesPresetsAndReindexesBands(t *testing.T) {
	st, rec := seed(t)
	mustOp(t, func() (*Result, error) { return NewAsset(st, rec, "Emergency fund", "", Options{}) })
	rec = reload(t, st)
	presetWith(t, st, "Three", demodata.NetWorth, "emergency-fund", demodata.AllCash)
	l := graph.NewLines(demodata.ProjBase)
	a := l.Add("emergency-fund", false, "", graph.RGB(1))
	b := l.Add(demodata.NetWorth, false, "", graph.RGB(2))
	c := l.Add(demodata.AllCash, false, "", graph.RGB(3))
	l.AddBand(b, c, graph.RGB(4))
	l.AddBand(a, c, graph.RGB(5))
	putPreset(t, st, graph.Capture("Bands", l, 0, 1))
	mustOp(t, func() (*Result, error) { return DeleteAsset(st, rec, "emergency-fund", Options{}) })
	p := storedByName(t, st, "Bands")
	if len(p.Lines) != 2 || len(p.Bands) != 1 || p.Bands[0].Upper != 0 || p.Bands[0].Lower != 1 {
		t.Errorf("Bands %+v", p)
	}
	if three := storedByName(t, st, "Three"); len(three.Lines) != 2 || len(three.Bands) != 1 {
		t.Errorf("Three %+v", three)
	}
}

func TestDeleteAssetKeepsAPresetLeftWithNoLines(t *testing.T) {
	st, rec := seed(t)
	mustOp(t, func() (*Result, error) { return NewAsset(st, rec, "Emergency fund", "", Options{}) })
	rec = reload(t, st)
	l := graph.NewLines(demodata.ProjBase)
	l.Add("emergency-fund", false, "", graph.RGB(1))
	putPreset(t, st, graph.Capture("Solo", l, 0, 1))
	mustOp(t, func() (*Result, error) { return DeleteAsset(st, rec, "emergency-fund", Options{}) })
	if p := storedByName(t, st, "Solo"); len(p.Lines) != 0 || len(p.Bands) != 0 {
		t.Errorf("Solo %+v", p)
	}
}
