// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func affectedKinds(plan DeletePlan) map[string]int {
	out := map[string]int{}
	for _, a := range plan.Affected {
		out[a.Kind]++
	}
	return out
}

func TestDeleteAssetCascade(t *testing.T) {
	st, rec := seed(t)
	res, err := NewAsset(st, rec, "Emergency fund", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Asset.ID
	mustOp(t, func() (*Result, error) {
		return AddMember(st, reload(t, st), demodata.Liquid, model.AssetMember(id), Options{})
	})
	mod := model.Modification{Target: model.ObjectRef{Kind: model.KindAsset, ID: string(id)}, Property: model.PropGrowth, Values: model.Constant(0.01)}
	mustOp(t, func() (*Result, error) { return AddMod(st, reload(t, st), demodata.ProjBase, mod, Options{}) })
	set := model.AssetSetting{Asset: id, Start: model.Ptr(model.D(2027, time.January, 1))}
	mustOp(t, func() (*Result, error) { return AddAssetSetting(st, reload(t, st), demodata.ProjBase, set, Options{}) })
	rec = reload(t, st)

	plan, err := AssetDeletePlan(rec, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := affectedKinds(plan); got[AffectedMember] != 1 || got[AffectedAssetSetting] != 1 || got[AffectedModification] != 1 || len(plan.Affected) != 3 {
		t.Fatalf("plan %+v", plan)
	}
	if rec.Asset(id) == nil || len(rec.Asset(demodata.Liquid).Members) != 4 {
		t.Fatal("the plan changed the record")
	}

	if _, err := DeleteAsset(st, rec, id, Options{}); err != nil {
		t.Fatal(err)
	}
	after := reload(t, st)
	if after.Asset(id) != nil || len(after.Asset(demodata.Liquid).Members) != 3 {
		t.Errorf("asset %v, Liquid members %+v", after.Asset(id), after.Asset(demodata.Liquid).Members)
	}
	consecutive(t, after)
	p := after.Projection(demodata.ProjBase)
	if slices.ContainsFunc(p.AssetSettings, func(s model.AssetSetting) bool { return s.Asset == id }) ||
		slices.ContainsFunc(p.Mods, func(m model.Modification) bool { return m.Target == mod.Target }) {
		t.Error("a setting or modification of the deleted asset is left")
	}
	if _, err := after.Resolve(demodata.NetWorth); err != nil {
		t.Error(err)
	}
}

func TestAssetDeletePlanOfAnUnusedAssetIsEmpty(t *testing.T) {
	st, rec := seed(t)
	res, err := NewAsset(st, rec, "Spare", "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := AssetDeletePlan(reload(t, st), res.Asset.ID)
	if err != nil || len(plan.Affected) != 0 || plan.Blocked != "" {
		t.Errorf("plan %+v, %v", plan, err)
	}
	if _, err := AssetDeletePlan(rec, "acct:checking"); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("default asset: %v", err)
	}
	if _, err := AssetDeletePlan(rec, "nope"); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown asset: %v", err)
	}
}

func TestDeleteAccountCascade(t *testing.T) {
	st, rec := seed(t)
	rec = withEmptyAccount(t, st, rec)
	scratch := model.AccountID("scratch")
	base := goodItem()
	base.Account = scratch
	res, err := AddBudgetItem(st, rec, base, Options{})
	if err != nil {
		t.Fatal(err)
	}
	baseID := res.Item.ID
	transfer := model.BudgetItem{
		Name: "Sweep", Kind: model.ItemTransfer, Amount: model.Constant(50), From: demodata.Checking, To: scratch,
		Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Start: model.Ptr(model.D(2027, 1, 1)),
	}
	mustOp(t, func() (*Result, error) { return AddBudgetItem(st, reload(t, st), transfer, Options{}) })
	added := goodItem()
	added.Account = scratch
	added.Name = "Added"
	mustOp(t, func() (*Result, error) { return AddItem(st, reload(t, st), demodata.ProjBase, added, Options{}) })
	rec = reload(t, st)
	for _, m := range []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: baseID}, Property: model.PropAmount, Values: model.Constant(900)},
		{Target: model.ObjectRef{Kind: model.KindAccount, ID: "scratch"}, Property: model.PropGrowth, Values: model.Constant(0.01)},
		{Target: model.ObjectRef{Kind: model.KindAsset, ID: "acct:scratch"}, Property: model.PropGrowth, Values: model.Constant(0.01)},
	} {
		mustOp(t, func() (*Result, error) { return AddMod(st, reload(t, st), demodata.ProjBase, m, Options{}) })
	}
	mustOp(t, func() (*Result, error) {
		return AddAssetSetting(st, reload(t, st), demodata.ProjBase, model.AssetSetting{Asset: "acct:scratch", Start: model.Ptr(model.D(2027, 1, 1))}, Options{})
	})
	mustOp(t, func() (*Result, error) {
		return AddMember(st, reload(t, st), demodata.Liquid, model.AccountMember(scratch), Options{})
	})
	rec = reload(t, st)
	items, mods := len(rec.Budget.Items), len(rec.Projection(demodata.ProjBase).Mods)

	plan, err := AccountDeletePlan(st, rec, scratch)
	if err != nil {
		t.Fatal(err)
	}
	got := affectedKinds(plan)
	// Items: two base and one added; modifications: of the base item, the account and its default asset.
	if plan.Blocked != "" || got[AffectedBudgetItem] != 3 || got[AffectedModification] != 3 || got[AffectedAssetSetting] != 1 || got[AffectedMember] != 1 {
		t.Fatalf("plan %+v", plan)
	}
	if len(rec.Budget.Items) != items {
		t.Fatal("the plan changed the record")
	}

	if _, err := DeleteAccount(st, rec, scratch, Options{}); err != nil {
		t.Fatal(err)
	}
	after := reload(t, st)
	if after.Account(scratch) != nil || after.Asset("acct:scratch") != nil {
		t.Error("the account or its default asset is still there")
	}
	if len(after.Budget.Items) != items-2 {
		t.Errorf("base items %d, want %d", len(after.Budget.Items), items-2)
	}
	p := after.Projection(demodata.ProjBase)
	if len(p.Mods) != mods-4 || len(p.AssetSettings) != len(rec.Projection(demodata.ProjBase).AssetSettings)-1 {
		t.Errorf("mods %d of %d, settings %d", len(p.Mods), mods, len(p.AssetSettings))
	}
	if len(after.Asset(demodata.Liquid).Members) != 3 {
		t.Errorf("Liquid members %+v", after.Asset(demodata.Liquid).Members)
	}
	consecutive(t, after)
	if _, err := after.Resolve(demodata.NetWorth); err != nil {
		t.Error(err)
	}
}

func TestAccountDeletePlanOfAnAccountWithRecordsIsBlocked(t *testing.T) {
	st, rec := seed(t)
	plan, err := AccountDeletePlan(st, rec, demodata.Checking)
	if err != nil || plan.Blocked == "" || len(plan.Affected) != 0 {
		t.Errorf("plan %+v, %v", plan, err)
	}
	if _, err := AccountDeletePlan(st, rec, "nope"); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown account: %v", err)
	}
}

func TestBudgetItemDeletePlanListsTheModsThatWouldTargetNothing(t *testing.T) {
	st, rec := seed(t)
	plan, err := BudgetItemDeletePlan(rec, "salary-a")
	if err != nil {
		t.Fatal(err)
	}
	want := []Affected{{AffectedOrphan, demodata.ProjNewJob, string(model.PropAmount), EffectOrphaned}}
	if !slices.Equal(plan.Affected, want) || plan.Blocked != "" {
		t.Fatalf("plan %+v, want %+v", plan, want)
	}
	if plan, err = BudgetItemDeletePlan(rec, "groceries"); err != nil || len(plan.Affected) != 0 {
		t.Errorf("an item without modifications: %+v, %v", plan, err)
	}
	if _, err = BudgetItemDeletePlan(rec, "nope"); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown item: %v", err)
	}
	if _, err := DeleteBudgetItem(st, rec, "salary-a", Options{}); err != nil {
		t.Fatal(err)
	}
	if after := reload(t, st); len(after.Projection(demodata.ProjNewJob).Mods) != 2 {
		t.Error("the delete removed the modification the plan listed")
	}
}
