// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/timeline"
)

func TestIntervalProblemsAndTexts(t *testing.T) {
	a := demoApp(t)
	id := demodata.Retirement
	if why := addIntervalProblem(a.st, a.rec, id, "2016-01-01", "2021-06-30", "200000"); why != "" {
		t.Errorf("a good interval: %q", why)
	}
	for _, c := range [][3]string{{"nope", "", "1"}, {"2020-01-01", "2019-01-01", "1"}, {"", "", "x"}} {
		if why := addIntervalProblem(a.st, a.rec, id, c[0], c[1], c[2]); why == "" {
			t.Errorf("interval %v: no problem", c)
		}
	}
	if !a.edit("add default balance", func() (*ops.Result, error) {
		return ops.AddDefault(a.st, a.rec, id, model.Interval{From: model.D(2016, 1, 1), To: model.Ptr(model.D(2021, 6, 30)), Value: 200000}, ops.Options{})
	}) {
		t.Fatal("add failed")
	}
	iv := a.rec.Asset(id).Default.Intervals[0]
	from, to, value := intervalTexts(iv)
	if from != "2016-01-01" || to != "2021-06-30" || value != "200000" {
		t.Errorf("texts %q %q %q", from, to, value)
	}
	if why := editIntervalProblem(a.st, a.rec, id, 0, from, to, value); why != "" {
		t.Errorf("an unchanged interval: %q", why)
	}
	if why := editIntervalProblem(a.st, a.rec, id, 3, from, to, value); why == "" {
		t.Error("an index out of range is no problem")
	}
	if why := editIntervalProblem(a.st, a.rec, id, 0, from, "2015-01-01", value); why == "" {
		t.Error("an end before the start is no problem")
	}
	if f, tt, v := intervalTexts(model.Interval{From: model.MinDate, Value: -5.5}); f != "" || tt != "" || v != "-5.5" {
		t.Errorf("open interval texts %q %q %q", f, tt, v)
	}
}

func TestGrowthKindAndConstant(t *testing.T) {
	a := demoApp(t)
	id := demodata.Retirement
	asset := func() *model.Asset { return a.rec.Asset(id) }
	if k, _ := growthKind(asset()); k != growthNone {
		t.Fatalf("kind %s, want none", k)
	}
	if why := growthConstantProblem(a.st, a.rec, id, "x"); why == "" {
		t.Error("a word is no growth rate")
	}
	if why := growthConstantProblem(a.st, a.rec, id, "4"); why != "" {
		t.Errorf("4: %q", why)
	}
	if !a.edit("set growth", func() (*ops.Result, error) { return setGrowthConstant(a.st, a.rec, id, "4", ops.Options{}) }) {
		t.Fatal("set failed")
	}
	if k, r := growthKind(asset()); k != growthConstant || r != 0.04 {
		t.Errorf("kind %s rate %v", k, r)
	}
	if why := growthConstantProblem(a.st, a.rec, id, "4%"); why != "" {
		t.Errorf("the rate it has: %q", why)
	}

	// The timeline strip's commit writes the setting as one undoable edit, and the rate becomes a schedule.
	s := &timelineStrip{a: a, commit: (&assetsTab{a: a}).growthCommit(id)}
	s.edit("split segment", asset().Params[model.PropGrowth], func(x model.Schedule) (model.Schedule, error) {
		return timeline.Split(x, 0, model.D(2028, 1, 1))
	})
	if k, _ := growthKind(asset()); k != growthSchedule || len(asset().Params[model.PropGrowth].Segments()) != 2 {
		t.Errorf("kind %s, segments %v", k, asset().Params[model.PropGrowth].Segments())
	}
	if step, ok := a.history.PeekUndo(); !ok || step.Label != "split segment" {
		t.Errorf("last step %+v", step)
	}
	// A refused edit changes nothing and is told.
	n := len(a.notices)
	s.edit("delete segment", asset().Params[model.PropGrowth], func(x model.Schedule) (model.Schedule, error) { return timeline.Delete(x, 9) })
	if len(a.notices) != n+1 {
		t.Errorf("notices %v", a.notices)
	}
}

func TestAssetGrowthLayers(t *testing.T) {
	a := demoApp(t)
	for _, id := range []model.AssetID{demodata.Retirement, demodata.NetWorth} {
		g := model.Constant(0.03)
		if !a.edit("set growth", func() (*ops.Result, error) {
			return ops.SetParam(a.st, a.rec, id, model.PropGrowth, &g, ops.Options{})
		}) {
			t.Fatal("set failed")
		}
	}
	rows, err := assetGrowth(a.rec, demodata.NetWorth)
	if err != nil || len(rows) == 0 {
		t.Fatalf("rows %v, %v", rows, err)
	}
	for _, r := range rows {
		if !r.HasToday || len(r.Layers) < 2 || r.Layers[0].Asset != "" {
			t.Errorf("%s: %+v", r.Name, r)
		}
		applying := 0
		for _, l := range r.Layers {
			if l.Applies {
				applying++
			}
		}
		if applying != 1 || !r.Layers[len(r.Layers)-1].Applies || r.Today != 0.03 {
			t.Errorf("%s: %d layers apply, today %v: %+v", r.Name, applying, r.Today, r.Layers)
		}
	}
}

func TestGrowthKindEmptySchedule(t *testing.T) {
	a := &model.Asset{Params: map[string]model.Schedule{model.PropGrowth: {}}}
	if k, _ := growthKind(a); k != growthNone {
		t.Fatalf("kind %s, want none", k)
	}
}
