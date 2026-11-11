// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func goodFocalForm() focalForm {
	return focalForm{account: string(demodata.Equity), symbol: demodata.Symbol, calendar: demodata.CalendarName, deposit: string(demodata.Brokerage),
		value: "60000", date: "2027-03-01", years: "40, 30, 20, 10", firstVest: "1", yearly: true}
}

func TestFocalFormBuildsModification(t *testing.T) {
	m, err := goodFocalForm().mod()
	if err != nil {
		t.Fatal(err)
	}
	g := m.Grant
	if m.Property != model.PropAddGrant || m.Target != (model.ObjectRef{Kind: model.KindAccount, ID: "equity"}) || g.Value != 60000 || !g.Yearly ||
		g.Date != model.D(2027, 3, 1) || len(g.YearPercents) != 4 || g.YearPercents[0] != 40 || g.FirstVestMonths != 1 || g.Rates != nil || g.Last != nil {
		t.Errorf("modification %+v grant %+v", m, g)
	}
	f := goodFocalForm()
	f.last, f.federal, f.state, f.fica, f.cliff = "2030-03-01", "24", "5", "7.65", "3"
	m, err = f.mod()
	if err != nil {
		t.Fatal(err)
	}
	if g := m.Grant; *g.Last != model.D(2030, 3, 1) || g.Rates.Federal != 0.24 || g.Rates.State != 0.05 || g.Rates.FICA != 0.0765 || g.CliffMonths != 3 {
		t.Errorf("grant %+v rates %+v", g, g.Rates)
	}
}

func TestFocalFormProblems(t *testing.T) {
	rec := demodata.Family()
	if got := focalProblem(rec, demodata.ProjBase, goodFocalForm()); got != "" {
		t.Errorf("a good form: %q", got)
	}
	for name, c := range map[string]struct {
		change func(*focalForm)
		want   string
	}{
		"no account":     {func(f *focalForm) { f.account = "" }, "equity account"},
		"no stock":       {func(f *focalForm) { f.symbol = "" }, "stock"},
		"no calendar":    {func(f *focalForm) { f.calendar = "" }, "calendar"},
		"no deposit":     {func(f *focalForm) { f.deposit = "" }, "deposit"},
		"no value":       {func(f *focalForm) { f.value = "" }, "value"},
		"bad date":       {func(f *focalForm) { f.date = "soon" }, "date"},
		"no percents":    {func(f *focalForm) { f.years = "" }, "percent"},
		"percents sum":   {func(f *focalForm) { f.years = "40,30" }, "100"},
		"bad cliff":      {func(f *focalForm) { f.cliff = "x" }, "whole number"},
		"checking":       {func(f *focalForm) { f.account = string(demodata.Checking) }, "equity"},
		"unknown stock":  {func(f *focalForm) { f.symbol = "NOPE" }, "NOPE"},
		"too high rates": {func(f *focalForm) { f.federal, f.state = "70", "40" }, "withholding"},
	} {
		f := goodFocalForm()
		c.change(&f)
		if got := focalProblem(rec, demodata.ProjBase, f); !strings.Contains(got, c.want) {
			t.Errorf("%s: problem %q, want one with %q", name, got, c.want)
		}
	}
}

func TestFocalFormUntouched(t *testing.T) {
	if !(focalForm{}).untouched() {
		t.Error("an empty form is touched")
	}
	for name, f := range map[string]focalForm{"account": {account: "equity"}, "value": {value: "1"}} {
		if f.untouched() {
			t.Errorf("a form with %s is untouched", name)
		}
	}
}

func TestFocalSummary(t *testing.T) {
	m, _ := goodFocalForm().mod()
	if got, want := focalSummary(*m.Grant), "$60000 of ACME every year, vesting 40/30/20/10 on Acme Corp"; got != want {
		t.Errorf("summary %q, want %q", got, want)
	}
}
