// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

func tspStart() model.AssetSetting {
	return model.AssetSetting{Asset: model.DefaultAssetID(demodata.TSP), Start: model.Ptr(model.D(2027, 3, 1))}
}

func TestAssetSettingOps(t *testing.T) {
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return AddAssetSetting(st, rec, demodata.ProjBase, tspStart(), o)
	})
	st, rec := seed(t)
	override := model.AssetSetting{Asset: model.DefaultAssetID(demodata.Brokerage), OverrideHistory: model.Ptr(true)}
	res, err := AddAssetSetting(st, rec, demodata.ProjBase, tspStart(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res, err = InsertAssetSetting(st, res.Record, demodata.ProjBase, 0, override, Options{}); err != nil {
		t.Fatal(err)
	}
	got := reload(t, st).Projection(demodata.ProjBase).AssetSettings
	if len(got) != 2 || !reflect.DeepEqual(got[0], override) || !reflect.DeepEqual(got[1], tspStart()) {
		t.Fatalf("after add and insert: %+v", got)
	}
	if res, err = MoveAssetSetting(st, res.Record, demodata.ProjBase, 0, 1, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjBase).AssetSettings; !reflect.DeepEqual(got[0], tspStart()) || !reflect.DeepEqual(got[1], override) {
		t.Errorf("after move: %+v", got)
	}
	later := tspStart()
	later.Start = model.Ptr(model.D(2028, 1, 1))
	if res, err = SetAssetSetting(st, res.Record, demodata.ProjBase, 0, later, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjBase).AssetSettings; !reflect.DeepEqual(got[0], later) {
		t.Errorf("after set: %+v", got)
	}
	if _, err = RemoveAssetSetting(st, res.Record, demodata.ProjBase, 0, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjBase).AssetSettings; len(got) != 1 || !reflect.DeepEqual(got[0], override) {
		t.Errorf("after remove: %+v", got)
	}
}

func TestAssetSettingRefusals(t *testing.T) {
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return AddAssetSetting(st, rec, demodata.ProjBase, model.AssetSetting{Asset: model.DefaultAssetID(demodata.TSP)}, Options{})
	})
	refused(t, ErrUnknownTarget, func(st *store.Store, rec *model.Record) (*Result, error) {
		s := tspStart()
		s.Asset = "nowhere"
		return AddAssetSetting(st, rec, demodata.ProjBase, s, Options{})
	})
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return AddAssetSetting(st, rec, "Nowhere", tspStart(), Options{})
	})
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		res, err := AddAssetSetting(st, rec, demodata.ProjBase, tspStart(), Options{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		return SetAssetSetting(st, res.Record, demodata.ProjBase, 0, tspStart(), Options{DryRun: true})
	})
	for _, i := range []int{-1, 0, 1000} {
		st, rec := seed(t)
		if _, err := RemoveAssetSetting(st, rec, demodata.ProjBase, i, Options{}); err == nil {
			t.Errorf("remove %d accepted", i)
		}
		if _, err := SetAssetSetting(st, rec, demodata.ProjBase, i, tspStart(), Options{}); err == nil {
			t.Errorf("set %d accepted", i)
		}
	}
	st, rec := seed(t)
	if _, err := InsertAssetSetting(st, rec, demodata.ProjBase, 1, tspStart(), Options{}); err == nil {
		t.Error("insert past the end accepted")
	}
}

// A hypothetical account of the projection or one it builds on can be given a setting.
func TestAssetSettingOnProjectionAccount(t *testing.T) {
	st, rec := seed(t)
	acct := model.ProjAccount{ID: "house", Name: "House", Type: model.TypeProperty, Tax: model.Taxable, Opening: 1000, From: model.D(2027, 1, 1)}
	res, err := AddProjAccount(st, rec, demodata.ProjBase, acct, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := model.AssetSetting{Asset: model.DefaultAssetID("house"), Start: model.Ptr(model.D(2027, 6, 1))}
	if _, err := AddAssetSetting(st, res.Record, demodata.ProjNewJob, s, Options{}); err != nil {
		t.Errorf("a child projection's setting for an inherited account: %v", err)
	}
	if _, err := AddAssetSetting(st, res.Record, demodata.ProjBase, s, Options{}); err != nil {
		t.Errorf("setting for the projection's own account: %v", err)
	}
}

func TestDeleteBudgetItem(t *testing.T) {
	dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return DeleteBudgetItem(st, rec, "salary-a", o)
	})
	st, rec := seed(t)
	mods := len(rec.Projection(demodata.ProjNewJob).Mods)
	res, err := DeleteBudgetItem(st, rec, "salary-a", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != "salary-a" || res.Item == nil || res.Item.ID != "salary-a" {
		t.Errorf("result: %+v", res)
	}
	got := reload(t, st)
	if _, ok := got.Budget.Item("salary-a"); ok || len(got.Budget.Items) != len(rec.Budget.Items)-1 {
		t.Error("the item is still in the budget")
	}
	// The modification of the item is kept, pointing at nothing.
	if len(got.Projection(demodata.ProjNewJob).Mods) != mods {
		t.Error("a modification was removed")
	}
	missing, err := got.MissingTargets(demodata.ProjNewJob)
	if err != nil || missing[0] != "budget item salary-a" {
		t.Errorf("missing = %q, %v", missing, err)
	}
	refused(t, ErrUnknownObject, func(st *store.Store, rec *model.Record) (*Result, error) {
		return DeleteBudgetItem(st, rec, "nowhere", Options{})
	})
}
