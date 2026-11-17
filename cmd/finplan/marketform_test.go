// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
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

func TestShockAndRebalanceFieldEdits(t *testing.T) {
	s := model.Shock{Class: model.ClassLargeCap, Date: model.D(2027, 3, 2), Change: -0.4}
	got, err := shockWith(s, "percent", "-25")
	if err != nil || got.Change != -0.25 || got.Date != s.Date {
		t.Errorf("percent: %+v, %v", got, err)
	}
	if got, err = shockWith(s, "date", "2028-01-05"); err != nil || got.Date != model.D(2028, 1, 5) || got.Change != s.Change {
		t.Errorf("date: %+v, %v", got, err)
	}
	if _, err = shockWith(s, "percent", "x"); err == nil {
		t.Error("a change that is not a number was read")
	}
	if _, err = shockWith(s, "date", "soon"); err == nil {
		t.Error("a date that is not one was read")
	}

	r := model.Rebalance{Account: demodata.Brokerage, Date: model.D(2027, 9, 1), Allocation: []model.Weight{{Class: model.ClassLargeCap, Share: 0.6}, {Class: model.ClassBonds, Share: 0.4}}}
	if got, err = shockWith(s, "class", "x"); err != nil || got != s {
		t.Errorf("an unknown field changed the shock: %+v, %v", got, err)
	}
	rec := demodata.Family()
	if got := targetText(rec, r); got != "60% US large cap, 40% US bonds" {
		t.Errorf("allocation text %q", got)
	}
	rb, err := rebalanceWith(rec, r, "allocation", "70% US large cap, 30 us bonds")
	if err != nil || rb.Allocation[0].Share != 0.7 || rb.Allocation[1] != (model.Weight{Class: model.ClassBonds, Share: 0.3}) || r.Allocation[0].Share != 0.6 {
		t.Errorf("allocation: %+v (original %+v), %v", rb.Allocation, r.Allocation, err)
	}
	if rb, err = rebalanceWith(rec, r, "allocation", targetText(rec, r)); err != nil || !reflect.DeepEqual(rb, r) {
		t.Errorf("the text read back is %+v, %v", rb, err)
	}
	for _, bad := range []string{"", "60% nothing", "60%", "x% US bonds", "50% US bonds, 50% US bonds"} {
		if _, err := rebalanceWith(rec, r, "allocation", bad); err == nil {
			t.Errorf("%q was read", bad)
		}
	}
	if rb, err = rebalanceWith(rec, r, "date", "2028-02-01"); err != nil || rb.Date != model.D(2028, 2, 1) {
		t.Errorf("date: %+v, %v", rb, err)
	}
}

func TestEditShockAndRebalanceInPlace(t *testing.T) {
	a := demoApp(t)
	pt := a.proj
	name := newChild(t, a, demodata.ProjBase)
	pt.addShock = shockForm{class: model.ClassLargeCap, date: "2027-03-02", percent: "-40"}
	pt.addRebalance = goodRebalanceForm()
	if !pt.addShockTo(name) || !pt.addRebalanceTo(name) {
		t.Fatalf("not added: %v", a.notices)
	}
	pt.editShock(name, 0, "percent", "-10")
	pt.editShock(name, 0, "date", "2027-04-02")
	if got := a.rec.Projection(name).Shocks[0]; got.Change != -0.1 || got.Date != model.D(2027, 4, 2) || got.Class != model.ClassLargeCap {
		t.Fatalf("shock %+v; notices %v", got, a.notices)
	}
	pt.editShock(name, 0, "percent", "x")
	if pt.errs["shockpercent0"] == "" {
		t.Error("a change that is not a number was taken")
	}
	pt.editRebalance(name, 0, "allocation", "50% US large cap, 50% US bonds")
	if got := a.rec.Projection(name).Rebalances[0]; got.Allocation[0].Share != 0.5 || got.Allocation[1].Share != 0.5 {
		t.Fatalf("rebalance %+v; notices %v", got, a.notices)
	}
	a.notices = nil
	pt.editRebalance(name, 0, "allocation", "50% US large cap, 10% US bonds")
	if len(a.notices) == 0 || a.rec.Projection(name).Rebalances[0].Allocation[1].Share != 0.5 {
		t.Error("a target that does not add up to 100% was saved")
	}
	a.undo()
	if got := a.rec.Projection(name).Rebalances[0]; got.Allocation[0].Share != 0.6 || got.Allocation[1].Share != 0.4 {
		t.Errorf("undo left %+v", got)
	}
	if !pt.setShock(name, 0, model.Shock{Class: model.ClassBonds, Date: model.D(2027, 4, 2), Change: -0.1}) || a.rec.Projection(name).Shocks[0].Class != model.ClassBonds {
		t.Errorf("the class was not changed: %v", a.notices)
	}
}
