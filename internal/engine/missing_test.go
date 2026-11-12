// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// MissingTargets must name exactly the modifications the engine skips because their target is not found.
func TestMissingTargetsAgreeWithSkipped(t *testing.T) {
	rec := fixture(t)
	chk := model.DefaultAssetID("chk")
	extra := model.BudgetItem{ID: "extra", Kind: model.ItemExpense, Amount: model.Constant(5), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 3}, Account: "chk", Timing: model.Lump}
	amount := func(kind model.ObjectKind, id string) model.Modification {
		return model.Modification{Target: model.ObjectRef{Kind: kind, ID: id}, Property: model.PropAmount, Values: model.Constant(1)}
	}
	growth := func(kind model.ObjectKind, id string) model.Modification {
		m := amount(kind, id)
		m.Property = model.PropGrowth
		return m
	}
	must(t, rec.AddProjection(&model.Projection{Name: "parent", Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "extra"}, Property: model.PropAddItem, Item: &extra},
	}}))
	must(t, rec.AddProjection(&model.Projection{Name: "child", Parent: "parent", Mods: []model.Modification{
		amount(model.KindBudgetItem, "salary"),
		amount(model.KindBudgetItem, "extra"),
		amount(model.KindBudgetItem, "later"),
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "later"}, Property: model.PropAddItem, Item: &model.BudgetItem{ID: "later", Kind: model.ItemExpense, Amount: model.Constant(1), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 3}, Account: "chk", Timing: model.Lump}},
		amount(model.KindBudgetItem, "later"),
		amount(model.KindBudgetItem, "gone"),
		growth(model.KindAccount, "chk"),
		growth(model.KindAccount, "gone"),
		growth(model.KindAsset, "gone"),
		{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Values: model.Constant(0.02)},
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "rent"}, Property: "colour"},
	}}))

	missing, err := rec.MissingTargets("child")
	must(t, err)
	want := []string{"", "", "budget item later", "", "", "budget item gone", "", "account gone", "asset gone", "", ""}
	if !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %q, want %q", missing, want)
	}
	res, err := Balances(rec, "child", chk, []model.Date{d(2026, time.March, 31)})
	must(t, err)
	skipped := make([]string, len(want))
	for _, s := range res.Skipped {
		if s.Projection == "child" && (s.Reason == "budget item not found" || s.Reason == "target not found") {
			skipped[s.Index] = "x"
		}
	}
	for i := range want {
		if (want[i] != "") != (skipped[i] != "") {
			t.Errorf("modification %d: missing %q, engine skipped as not found: %v", i, want[i], skipped[i] != "")
		}
	}
	if _, err := rec.MissingTargets("nowhere"); err == nil {
		t.Error("an unknown projection was accepted")
	}
}

func TestAllStartsUsesAssetSettings(t *testing.T) {
	rec := fixture(t)
	start := d(2027, time.January, 1)
	must(t, rec.AddProjection(&model.Projection{Name: "p", Start: model.Ptr(d(2026, time.June, 1)), AssetSettings: []model.AssetSetting{
		{Asset: model.DefaultAssetID("chk"), Start: &start},
	}}))
	all, err := AllStarts(rec, "p")
	must(t, err)
	if len(all) != len(rec.Accounts) {
		t.Fatalf("got %d starts for %d accounts", len(all), len(rec.Accounts))
	}
	for _, s := range all {
		want := d(2026, time.June, 1)
		if s.Account == "chk" {
			want = start
		}
		if s.ProjectionStart != want {
			t.Errorf("%s projection start = %v, want %v", s.Account, s.ProjectionStart, want)
		}
	}
	must(t, rec.AddProjection(&model.Projection{Name: "bad", AssetSettings: []model.AssetSetting{{Asset: "nowhere", Start: &start}}}))
	if _, err := AllStarts(rec, "bad"); err == nil {
		t.Error("a setting naming an unknown asset was accepted")
	}
}
