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

// addClass adds the form's class as the Add class button does and reports whether it was added.
func addClass(a *app, f newClassForm) bool {
	c, _ := f.class()
	return a.edit("add market class", func() (*ops.Result, error) {
		return ops.AddClass(a.st, a.rec, c, ops.Options{})
	})
}

// Scenario: adding Gold at 5% and 15% puts it among the classes, where the correlation matrix and the allocation lists read them, and undo removes it.
func TestMarketsAddClass(t *testing.T) {
	a := marketsApp(t)
	n := len(a.rec.Classes)
	f := newClassForm{name: "Gold", ret: "5", vol: "15%"}
	if p := newClassProblem(a.st, a.rec, f); p != "" {
		t.Fatalf("problem %q", p)
	}
	if !addClass(a, f) {
		t.Fatalf("not added; notices %v", a.notices)
	}
	gold := a.rec.Class("gold")
	if gold == nil || len(a.rec.Classes) != n+1 {
		t.Fatalf("classes %v", a.rec.Classes)
	}
	if r, ok := classRate(*gold); !ok || r != 0.05 || gold.Volatility != 0.15 {
		t.Errorf("gold return %v, %v, volatility %v", r, ok, gold.Volatility)
	}
	if got := a.rec.CorrelationOf("gold", model.ClassLargeCap); got != 0 {
		t.Errorf("a new class is correlated %v with large cap", got)
	}
	a.undo()
	if a.rec.Class("gold") != nil || len(a.rec.Classes) != n {
		t.Errorf("undo left %v", a.rec.Classes)
	}
}

// Scenario: an unused class can be removed and undo restores it; a class in an allocation cannot, and the reason names what holds it.
func TestMarketsRemoveClass(t *testing.T) {
	a := marketsApp(t)
	addClass(a, newClassForm{name: "Gold", ret: "5", vol: "15"})
	if p := removeClassProblem(a.st, a.rec, "gold"); p != "" {
		t.Fatalf("problem %q", p)
	}
	if !a.edit("remove market class", func() (*ops.Result, error) {
		return ops.RemoveClass(a.st, a.rec, "gold", ops.Options{})
	}) || a.rec.Class("gold") != nil {
		t.Fatalf("not removed; notices %v", a.notices)
	}
	a.undo()
	if a.rec.Class("gold") == nil {
		t.Error("undo did not restore the class")
	}
	p := removeClassProblem(a.st, a.rec, model.ClassLargeCap)
	if p == "" || !strings.Contains(p, model.ClassLargeCap) {
		t.Errorf("problem for large cap %q", p)
	}
}

func TestNewClassFormUntouched(t *testing.T) {
	if !(newClassForm{name: " ", vol: " "}).untouched() {
		t.Error("blank form is touched")
	}
	for _, f := range []newClassForm{{name: "a"}, {ret: "1"}, {vol: "1"}} {
		if f.untouched() {
			t.Errorf("%+v is untouched", f)
		}
	}
}

func TestNewClassProblem(t *testing.T) {
	a := marketsApp(t)
	for _, c := range []struct {
		f    newClassForm
		want string
	}{
		{newClassForm{}, "enter a name"},
		{newClassForm{name: "US large cap", ret: "5", vol: "15"}, "US large cap"},
		{newClassForm{name: "Gold", ret: "x", vol: "15"}, "expected return"},
		{newClassForm{name: "Gold", ret: "5", vol: ""}, "volatility"},
		{newClassForm{name: "Gold", ret: "5", vol: "-3"}, ""},
	} {
		p := newClassProblem(a.st, a.rec, c.f)
		if p == "" || !strings.Contains(p, c.want) {
			t.Errorf("%+v: problem %q, want %q", c.f, p, c.want)
		}
	}
}

// A stock's volatility is typed in percent, blank unsets it, undo restores it, and a bad one is not taken.
func TestMarketsEditStockVolatility(t *testing.T) {
	a := marketsApp(t)
	symbols := a.rec.Symbols()
	if len(symbols) == 0 {
		t.Fatal("the demo has no stocks")
	}
	sym := symbols[0]
	was, wasSet := a.rec.StockVolatility[sym]
	a.markets.editStockVolatility(sym, "50")
	if got := a.rec.StockVolatilityOf(sym); got != 0.5 {
		t.Fatalf("volatility %v; notices %v", got, a.notices)
	}
	if stockVolatilityText(a.rec, sym) != "50" {
		t.Errorf("shown as %q", stockVolatilityText(a.rec, sym))
	}
	a.markets.editStockVolatility(sym, "x")
	if a.markets.errs["stockvol"+sym] == "" {
		t.Error("a volatility that is not a number was taken")
	}
	a.markets.editStockVolatility(sym, "")
	if _, set := a.rec.StockVolatility[sym]; set || a.rec.StockVolatilityOf(sym) != model.DefaultStockVolatility {
		t.Errorf("a blank field left %v", a.rec.StockVolatility)
	}
	a.undo()
	if got := a.rec.StockVolatilityOf(sym); got != 0.5 {
		t.Errorf("undo of the clear left %v", got)
	}
	a.undo()
	if got, set := a.rec.StockVolatility[sym]; set != wasSet || set && got != was {
		t.Errorf("undo left %v, %v; was %v, %v", got, set, was, wasSet)
	}
	a.redo()
	if got := a.rec.StockVolatilityOf(sym); got != 0.5 {
		t.Errorf("redo left %v", got)
	}
	saved, err := a.st.Load()
	if err != nil || saved.StockVolatilityOf(sym) != 0.5 {
		t.Errorf("not saved: %v", err)
	}
	a.notices = nil
	a.markets.editStockVolatility(sym, "100000")
	if len(a.notices) == 0 {
		t.Error("a volatility out of range was taken")
	}
}
