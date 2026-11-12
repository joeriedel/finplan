// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func goodShockForm() shockForm {
	return shockForm{class: model.ClassLargeCap, date: "2027-03-02", percent: "-40"}
}

func goodRebalanceForm() rebalanceForm {
	return rebalanceForm{account: string(demodata.Brokerage), date: "2027-09-01", percents: map[string]string{model.ClassLargeCap: "60", model.ClassBonds: "40"}}
}

func TestShockFormBuildsShock(t *testing.T) {
	s, err := goodShockForm().shock()
	if err != nil {
		t.Fatal(err)
	}
	if s.Class != model.ClassLargeCap || s.Date != model.D(2027, 3, 2) || s.Change != -0.4 {
		t.Errorf("shock %+v", s)
	}
	f := goodShockForm()
	f.percent = "12.5%"
	if s, err = f.shock(); err != nil || s.Change != 0.125 {
		t.Errorf("12.5%%: %+v, %v", s, err)
	}
}

func TestShockFormProblems(t *testing.T) {
	rec := demodata.Family()
	if got := shockProblem(rec, goodShockForm()); got != "" {
		t.Errorf("a good form: %q", got)
	}
	for name, c := range map[string]struct {
		change func(*shockForm)
		want   string
	}{
		"no class":     {func(f *shockForm) { f.class = "" }, "class"},
		"no date":      {func(f *shockForm) { f.date = "" }, "date"},
		"bad date":     {func(f *shockForm) { f.date = "soon" }, "date"},
		"no percent":   {func(f *shockForm) { f.percent = " " }, "percent"},
		"not a number": {func(f *shockForm) { f.percent = "a lot" }, "not a number"},
		"minus 100":    {func(f *shockForm) { f.percent = "-100" }, "-100%"},
		"unknown":      {func(f *shockForm) { f.class = "gold" }, "gold"},
	} {
		f := goodShockForm()
		c.change(&f)
		if got := shockProblem(rec, f); !strings.Contains(got, c.want) {
			t.Errorf("%s: problem %q, want one with %q", name, got, c.want)
		}
	}
}

func TestRebalanceFormBuildsRebalance(t *testing.T) {
	rec := demodata.Family()
	r, err := goodRebalanceForm().rebalance(rec)
	if err != nil {
		t.Fatal(err)
	}
	if r.Account != demodata.Brokerage || r.Date != model.D(2027, 9, 1) || len(r.Allocation) != 2 ||
		r.Allocation[0] != (model.Weight{Class: model.ClassLargeCap, Share: 0.6}) || r.Allocation[1] != (model.Weight{Class: model.ClassBonds, Share: 0.4}) {
		t.Errorf("rebalance %+v", r)
	}
}

func TestRebalanceFormProblems(t *testing.T) {
	rec := demodata.Family()
	if got := rebalanceProblem(rec, goodRebalanceForm()); got != "" {
		t.Errorf("a good form: %q", got)
	}
	for name, c := range map[string]struct {
		change func(*rebalanceForm)
		want   string
	}{
		"no account":   {func(f *rebalanceForm) { f.account = "" }, "account"},
		"no date":      {func(f *rebalanceForm) { f.date = "" }, "date"},
		"no percents":  {func(f *rebalanceForm) { f.percents = nil }, "percent"},
		"bad percent":  {func(f *rebalanceForm) { f.percents[model.ClassBonds] = "x" }, "not a number"},
		"sum":          {func(f *rebalanceForm) { f.percents[model.ClassBonds] = "30" }, "90"},
		"negative":     {func(f *rebalanceForm) { f.percents[model.ClassLargeCap], f.percents[model.ClassBonds] = "120", "-20" }, "negative"},
		"checking":     {func(f *rebalanceForm) { f.account = string(demodata.Checking) }, "cannot"},
		"unknown acct": {func(f *rebalanceForm) { f.account = "nope" }, "nope"},
	} {
		f := goodRebalanceForm()
		f.percents = map[string]string{model.ClassLargeCap: "60", model.ClassBonds: "40"}
		c.change(&f)
		if got := rebalanceProblem(rec, f); !strings.Contains(got, c.want) {
			t.Errorf("%s: problem %q, want one with %q", name, got, c.want)
		}
	}
}

func TestMarketFormsUntouched(t *testing.T) {
	if !(shockForm{}).untouched() || !(rebalanceForm{}).untouched() || !(rebalanceForm{percents: map[string]string{model.ClassBonds: " "}}).untouched() {
		t.Error("an empty form is touched")
	}
	for name, f := range map[string]shockForm{"class": {class: "x"}, "date": {date: "2027"}, "percent": {percent: "1"}} {
		if f.untouched() {
			t.Errorf("a shock form with %s is untouched", name)
		}
	}
	for name, f := range map[string]rebalanceForm{"account": {account: "x"}, "date": {date: "2027"}, "percent": {percents: map[string]string{model.ClassBonds: "40"}}} {
		if f.untouched() {
			t.Errorf("a rebalance form with %s is untouched", name)
		}
	}
}

func TestMarketSummaries(t *testing.T) {
	rec := demodata.Family()
	if got := shockSummary(rec, model.Shock{Class: model.ClassLargeCap, Date: model.D(2027, 3, 2), Change: -0.4}); got != "-40% to US large cap on 2027-03-02" {
		t.Errorf("shock: %q", got)
	}
	if got := shockSummary(rec, model.Shock{Class: model.ClassBonds, Date: model.D(2027, 3, 2), Change: 0.055}); got != "+5.5% to US bonds on 2027-03-02" {
		t.Errorf("rise: %q", got)
	}
	r := model.Rebalance{Account: demodata.Brokerage, Date: model.D(2027, 9, 1), Allocation: []model.Weight{{Class: model.ClassLargeCap, Share: 0.6}, {Class: model.ClassBonds, Share: 0.4}}}
	if got := rebalanceSummary(rec, r); !strings.Contains(got, "60% US large cap, 40% US bonds on 2027-09-01") {
		t.Errorf("rebalance: %q", got)
	}
}

func TestAddShockAndRebalanceThroughTheForms(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	name := newChild(t, a, demodata.ProjBase)
	before := balanceOn(t, a, demodata.Brokerage, name, endOfYear)

	pt.addShock = shockForm{class: model.ClassLargeCap, date: "2027-03-02"}
	if pt.addShockTo(name) {
		t.Fatal("a shock without a change was added")
	}
	pt.addShock.percent = "-40"
	if !pt.addShockTo(name) {
		t.Fatalf("not added: %v", a.notices)
	}
	if got := a.rec.Projection(name).Shocks; len(got) != 1 || got[0].Change != -0.4 || pt.addShock.date != "" {
		t.Fatalf("shocks %+v, form %+v", got, pt.addShock)
	}
	if after := balanceOn(t, a, demodata.Brokerage, name, endOfYear); !(after < before) {
		t.Errorf("the brokerage is %v after a 40%% drop in its only class, was %v", after, before)
	}

	pt.addRebalance = goodRebalanceForm()
	if !pt.addRebalanceTo(name) {
		t.Fatalf("not added: %v", a.notices)
	}
	if got := a.rec.Projection(name).Rebalances; len(got) != 1 || got[0].Allocation[1].Share != 0.4 || pt.addRebalance.date != "" {
		t.Fatalf("rebalances %+v, form %+v", got, pt.addRebalance)
	}
	a.undo()
	if len(a.rec.Projection(name).Rebalances) != 0 || len(a.rec.Projection(name).Shocks) != 1 {
		t.Error("undo did not remove the rebalance alone")
	}
}
