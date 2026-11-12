// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

func TestGrowthCommitRefusesAnEmptySchedule(t *testing.T) {
	a := demoApp(t)
	before := a.rec.Clone()
	a.assets.growthCommit(demodata.Retirement)("edit growth", model.Schedule{})
	if len(a.notices) != 1 || !strings.Contains(a.notices[0], "at least one segment") {
		t.Errorf("notices %q", a.notices)
	}
	if !reflect.DeepEqual(a.rec.Assets, before.Assets) || a.history.CanUndo() {
		t.Error("the refused commit changed the record or the history")
	}
}

func TestNewNamesAvoidProjectionAccountsAndAssets(t *testing.T) {
	a := demoApp(t)
	if got := newAssetProblem(a.st, a.rec, "Checking"); !strings.Contains(got, "account") {
		t.Errorf("asset named as an account: %q", got)
	}
	var f acctForm
	fillAccount(&f, "name", "Net worth", "type", "property", "tax", "taxable", "from", "2027-06-01")
	if got := acctProblem(a.st, a.rec, demodata.ProjNewJob, f); !strings.Contains(got, "asset named") {
		t.Errorf("projection account named as an asset: %q", got)
	}
	addHouse(t, a, demodata.ProjNewJob)
	if got := newAssetProblem(a.st, a.rec, "House"); !strings.Contains(got, demodata.ProjNewJob) {
		t.Errorf("asset named as a projection account: %q", got)
	}
	if got := renameAssetProblem(a.st, a.rec, demodata.Liquid, "house"); got == "" {
		t.Error("an asset can be renamed to a projection account's name")
	}
}

func TestDeleteAccountRemovesItsPresetLine(t *testing.T) {
	a := demoApp(t)
	f := newAcctForm{name: "Brokerage 2", typ: model.TypeBrokerage, tax: model.Taxable}
	acct, _ := f.account()
	if !a.edit("add account", func() (*ops.Result, error) { return ops.NewAccount(a.st, a.rec, acct, ops.Options{}) }) {
		t.Fatal(a.notices)
	}
	id := acct.ID
	if id == "" {
		id = "brokerage-2"
	}
	line := model.DefaultAssetID(id)
	a.graph.lines.Add(demodata.NetWorth, false, "", graph.RGB(1))
	a.graph.lines.Add(line, false, "", graph.RGB(2))
	a.graph.savePresetAs("Pot view")
	if len(a.notices) != 0 {
		t.Fatal(a.notices)
	}
	stored := func() string {
		row, err := a.st.Preset("Pot view")
		if err != nil {
			t.Fatal(err)
		}
		return string(row.Body)
	}
	lineCount := func() (shown, saved int) {
		for _, p := range a.graph.presets {
			if p.Name == "Pot view" {
				shown = len(p.Lines)
			}
		}
		row, _ := a.st.Preset("Pot view")
		p, err := graph.DecodePreset(row.Name, row.Format, row.Body)
		if err != nil {
			t.Fatal(err)
		}
		return shown, len(p.Lines)
	}
	original := stored()
	_, n := lineCount()
	hasLine := func() bool { return strings.Contains(stored(), string(line)) }
	if !hasLine() {
		t.Fatal("the saved preset has no line for the account")
	}

	plan, err := ops.AccountDeletePlan(a.st, a.rec, id)
	if err != nil || plan.Blocked != "" {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	if lines := deleteLines(plan); !strings.Contains(strings.Join(lines, "\n"), `View preset "Pot view"`) {
		t.Errorf("lines %q", lines)
	}
	if !a.edit("delete account", func() (*ops.Result, error) { return ops.DeleteAccount(a.st, a.rec, id, ops.Options{}) }) {
		t.Fatal(a.notices)
	}
	if shown, saved := lineCount(); shown != saved || saved != n-1 || hasLine() {
		t.Errorf("after the delete: %d shown, %d saved", shown, saved)
	}
	a.undo()
	if shown, saved := lineCount(); shown != n || saved != n || stored() != original {
		t.Errorf("after undo: %d shown, %d saved", shown, saved)
	}
	a.redo()
	if shown, saved := lineCount(); shown != n-1 || saved != n-1 || hasLine() {
		t.Errorf("after redo: %d shown, %d saved", shown, saved)
	}
}

func TestDeleteLinesNamesAPreset(t *testing.T) {
	plan := ops.DeletePlan{Affected: []ops.Affected{{Kind: ops.AffectedPreset, In: "Retirement overview", Detail: "1 line, 1 band", Effect: ops.EffectRemoved}}}
	want := `View preset "Retirement overview": remove what shows it (1 line, 1 band)`
	if got := deleteLines(plan); len(got) != 1 || got[0] != want {
		t.Errorf("got %q", got)
	}
}

func TestDeleteLinesNamesTheHousehold(t *testing.T) {
	plan := ops.DeletePlan{Affected: []ops.Affected{{Kind: ops.AffectedHousehold, In: "household", Detail: "reconciliation account", Effect: ops.EffectRemoved}}}
	want := "Household: reconciliation account resets to the first checking account"
	if got := deleteLines(plan); len(got) != 1 || got[0] != want {
		t.Errorf("got %q", got)
	}
}

func TestOpenAccountDeleteListsAPreset(t *testing.T) {
	a := demoApp(t)
	if err := addDeletableDemoAccount(a); err != nil {
		t.Fatal(err)
	}
	plan, err := ops.AccountDeletePlan(a.st, a.rec, a.accts.sel)
	if err != nil || plan.Blocked != "" {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	if lines := strings.Join(deleteLines(plan), "\n"); !strings.Contains(lines, `View preset "Brokerage 2 view"`) {
		t.Errorf("lines %q", lines)
	}
}

func TestUndoKeepsAPresetSavedSinceTheDelete(t *testing.T) {
	for _, redo := range []bool{false, true} {
		a := demoApp(t)
		if err := addDeletableDemoAccount(a); err != nil {
			t.Fatal(err)
		}
		id := a.accts.sel
		stored := func() string {
			row, err := a.st.Preset("Brokerage 2 view")
			if err != nil {
				t.Fatal(err)
			}
			return string(row.Body)
		}
		original := stored()
		if !a.edit("delete account", func() (*ops.Result, error) { return ops.DeleteAccount(a.st, a.rec, id, ops.Options{}) }) {
			t.Fatal(a.notices)
		}
		if redo {
			a.undo()
			if stored() != original {
				t.Fatal("an unchanged preset was not restored by undo")
			}
		}
		a.graph.lines.Add(demodata.Liquid, false, "", graph.RGB(3))
		a.graph.savePreset()
		resaved := stored()
		if resaved == original {
			t.Fatal("the preset was not changed")
		}
		if redo {
			a.redo()
		} else {
			a.undo()
		}
		if stored() != resaved {
			t.Errorf("redo=%v: the re-saved preset was overwritten", redo)
		}
		if len(a.notices) != 1 || !strings.Contains(a.notices[0], `"Brokerage 2 view"`) {
			t.Errorf("redo=%v: notices %q", redo, a.notices)
		}
	}
}

func TestDeleteLinesSayTheMatchAndTheTermsAreRemoved(t *testing.T) {
	plan := ops.DeletePlan{Affected: []ops.Affected{
		{Kind: ops.AffectedBudgetItem, In: "budget", Detail: "salary employer match", Effect: ops.EffectRemoved},
		{Kind: ops.AffectedBudgetItem, In: "Raise", Detail: "bonus employer match", Effect: ops.EffectRemoved},
		{Kind: ops.AffectedTerms, In: "mortgage", Detail: "paying account checking", Effect: ops.EffectRemoved},
	}}
	want := []string{
		"Base budget: remove salary employer match",
		`Projection "Raise": remove bonus employer match`,
		`Account "mortgage": remove its mortgage terms, paying account checking`,
	}
	if got := deleteLines(plan); !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
}
