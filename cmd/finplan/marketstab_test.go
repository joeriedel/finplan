// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

func marketsApp(t *testing.T) *app {
	t.Helper()
	a := demoApp(t)
	a.markets.rec = a.rec
	a.markets.reset()
	return a
}

func brokerageLater(t *testing.T, a *app) float64 {
	t.Helper()
	res, err := engine.Balances(a.rec, demodata.ProjBase, model.DefaultAssetID(demodata.Brokerage), []model.Date{model.D(2035, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	return res.Samples[0].Amount
}

// Scenario: editing large cap's return changes the Graph, and undo restores the class and the Graph.
func TestMarketsEditLargeCapReturn(t *testing.T) {
	a := marketsApp(t)
	before := brokerageLater(t, a)
	graphBefore := a.graph.cache
	a.markets.editClass(model.ClassLargeCap, "ret", "2")
	if r, ok := classRate(*a.rec.Class(model.ClassLargeCap)); !ok || r != 0.02 {
		t.Fatalf("return %v, %v; notices %v", r, ok, a.notices)
	}
	if a.graph.rec != a.rec || a.graph.cache == graphBefore {
		t.Error("the Graph was not given the edited record")
	}
	if after := brokerageLater(t, a); !(after < before) {
		t.Errorf("brokerage in 2035: %v, before %v", after, before)
	}
	a.markets.editClass(model.ClassLargeCap, "vol", "x")
	if a.markets.errs["vol"+model.ClassLargeCap] == "" {
		t.Error("a volatility that is not a number was taken")
	}

	a.undo()
	if r, _ := classRate(*a.rec.Class(model.ClassLargeCap)); r != 0.10 {
		t.Errorf("undo left the return at %v", r)
	}
	a.redo()
	if r, _ := classRate(*a.rec.Class(model.ClassLargeCap)); r != 0.02 {
		t.Errorf("redo left the return at %v", r)
	}
}

func TestMarketsCorrelationUndo(t *testing.T) {
	a := marketsApp(t)
	before := a.rec.CorrelationOf(model.ClassLargeCap, model.ClassSmallCap)
	a.markets.editCorrelation("c", model.ClassLargeCap, model.ClassSmallCap, "0.5")
	if got := a.rec.CorrelationOf(model.ClassSmallCap, model.ClassLargeCap); got != 0.5 {
		t.Fatalf("correlation %v; notices %v", got, a.notices)
	}
	a.markets.editCorrelation("c", model.ClassLargeCap, model.ClassSmallCap, "2")
	if got := a.rec.CorrelationOf(model.ClassLargeCap, model.ClassSmallCap); got != 0.5 || len(a.notices) == 0 {
		t.Errorf("a correlation of 2 was taken (%v) or not reported", got)
	}
	a.undo()
	if got := a.rec.CorrelationOf(model.ClassLargeCap, model.ClassSmallCap); got != before {
		t.Errorf("undo left the correlation at %v, want %v", got, before)
	}
}

func TestClassOverrides(t *testing.T) {
	rec := demodata.Family()
	var scale, shock bool
	for _, o := range classOverrides(rec, model.ClassLargeCap) {
		scale = scale || o.Projection == demodata.ProjNewJob && strings.HasPrefix(o.Text, "growth ")
		shock = shock || o.Projection == demodata.ProjCrash && strings.HasPrefix(o.Text, "shock: -40%")
	}
	if !scale || !shock {
		t.Errorf("overrides of large cap: %+v", classOverrides(rec, model.ClassLargeCap))
	}
	if got := classOverrides(rec, model.ClassCash); len(got) != 0 {
		t.Errorf("overrides of cash: %+v", got)
	}
}

func TestAllocationEditorText(t *testing.T) {
	a := demoApp(t)
	acct := a.rec.Account(demodata.Brokerage)
	texts := allocTexts(acct)
	if !allocUntouched(a.rec, acct, texts) || allocProblem(a.rec, acct, texts) != "" {
		t.Errorf("the account's own allocation: untouched %v, problem %q", allocUntouched(a.rec, acct, texts), allocProblem(a.rec, acct, texts))
	}
	texts[model.ClassLargeCap], texts[model.ClassBonds] = "60", "30"
	if allocUntouched(a.rec, acct, texts) || !strings.Contains(allocProblem(a.rec, acct, texts), "90") || allocSum(a.rec, texts) != 90 {
		t.Errorf("a sum of 90: problem %q", allocProblem(a.rec, acct, texts))
	}
	texts[model.ClassBonds] = "x"
	if allocProblem(a.rec, acct, texts) == "" {
		t.Error("a percent that is not a number was accepted")
	}
	texts[model.ClassBonds] = "40%"
	alloc, err := allocFromTexts(a.rec, texts)
	if err != nil || len(alloc) != 2 || alloc[1] != (model.Holding{Class: model.ClassBonds, Percent: 40}) {
		t.Fatalf("alloc %+v, %v", alloc, err)
	}
	if _, err := ops.SetAllocation(a.st, a.rec, acct.ID, alloc, false, ops.Options{}); err != nil {
		t.Fatal(err)
	}
	if p := allocProblem(a.rec, a.rec.Account(demodata.Checking), texts); p == "" {
		t.Error("an allocation for a checking account was accepted")
	}
	if allocProblem(a.rec, acct, map[string]string{}) == "" {
		t.Error("an empty allocation was accepted")
	}
}

func TestAllocationUndo(t *testing.T) {
	a := demoApp(t)
	alloc := []model.Holding{{Class: model.ClassLargeCap, Percent: 60}, {Class: model.ClassBonds, Percent: 40}}
	if !a.edit("set allocation", func() (*ops.Result, error) {
		return ops.SetAllocation(a.st, a.rec, demodata.Brokerage, alloc, false, ops.Options{})
	}) {
		t.Fatalf("notices %v", a.notices)
	}
	a.undo()
	if got := a.rec.Account(demodata.Brokerage).Allocation; len(got) != 1 || got[0].Percent != 100 {
		t.Errorf("undo left the allocation %+v", got)
	}
}

// Scenario: the empty-exposure text differs between a class no allocation holds and one held only by accounts that are closed or empty today.
func TestNoExposureText(t *testing.T) {
	a := demoApp(t)
	held := noExposureText(a.rec, model.ClassLargeCap)
	if !strings.Contains(held, "No open account") {
		t.Errorf("a class an allocation holds: %q", held)
	}
	if unheld := noExposureText(a.rec, "no-such-class"); !strings.Contains(unheld, "allocation holds") {
		t.Errorf("a class no allocation holds: %q", unheld)
	}
}
