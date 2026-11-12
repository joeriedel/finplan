// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestSuccessCountsRunsAboveZeroInTheWindow(t *testing.T) {
	d := func(y int) model.Date { return model.D(y, time.January, 1) }
	res := &MonteCarloResult{
		Times: []model.Date{d(2020), d(2030), d(2040), d(2050)},
		Values: [][]float64{
			{-5, 1, 1, 1}, // below zero only in the past
			{1, 1, 1, 1},
			{1, 1, 0, 1}, // zero is not above zero
			{1, 1, 1, -1},
		},
	}
	for _, c := range []struct {
		from, through model.Date
		want          float64
	}{
		{d(2030), d(2050), 50},
		{d(2030), d(2040), 75},
		{d(2030), d(2030), 100},
		{d(2020), d(2050), 25},
		{d(2050), d(2050), 75},
	} {
		got, err := Success(res, c.from, c.through)
		if err != nil || got != c.want {
			t.Errorf("Success(%v, %v) = %v, %v; want %v", c.from, c.through, got, err, c.want)
		}
	}
}

func TestSuccessRefusesEmptyInput(t *testing.T) {
	if _, err := Success(&MonteCarloResult{Times: []model.Date{1}}, 0, 10); err == nil {
		t.Error("a result without runs was accepted")
	}
	res := &MonteCarloResult{Times: []model.Date{100}, Values: [][]float64{{1}}}
	if _, err := Success(res, 0, 10); err == nil {
		t.Error("a window without samples was accepted")
	}
}

func TestAgeDateUsesTheFirstEarner(t *testing.T) {
	rec := demodata.Family()
	got, ok := AgeDate(rec, 95)
	if want := model.D(2080, time.March, 12); !ok || got != want {
		t.Errorf("AgeDate = %v, %v; want %v", got, ok, want)
	}
	rec.Household.Members = nil
	if _, ok := AgeDate(rec, 95); ok {
		t.Error("a household without an earner has an age date")
	}
}

func TestMonthStarts(t *testing.T) {
	got := MonthStarts(model.D(2026, time.November, 12), model.D(2027, time.February, 1))
	want := []model.Date{model.D(2026, time.November, 1), model.D(2026, time.December, 1), model.D(2027, time.January, 1), model.D(2027, time.February, 1)}
	if len(got) != len(want) {
		t.Fatalf("MonthStarts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("MonthStarts[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestFindEarliestRetirement(t *testing.T) {
	rec := demodata.Family()
	opts := SuccessOptions{SuccessAge: 95, MonteCarloOptions: MonteCarloOptions{Runs: 100, Seed: 1}}
	before := *rec.Projection(demodata.ProjRetire60).RetireOn
	got, err := FindEarliestRetirement(context.Background(), rec, demodata.ProjRetire60, demodata.NetWorth, 90, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Success < 90 {
		t.Fatalf("result %+v: want a date with success of at least 90", got)
	}
	if *rec.Projection(demodata.ProjRetire60).RetireOn != before {
		t.Error("the search changed the caller's record")
	}
	success := func(d model.Date) float64 {
		c := rec.EditableCopy()
		c.Projection(demodata.ProjRetire60).RetireOn = model.Ptr(d)
		_, pct, err := MonteCarloSuccess(c, demodata.ProjRetire60, demodata.NetWorth, opts)
		if err != nil {
			t.Fatal(err)
		}
		return pct
	}
	if pct := success(got.Date); pct != got.Success || pct < 90 {
		t.Errorf("success on %v is %v, the search said %v", got.Date, pct, got.Success)
	}
	if y, m, _ := got.Date.YMD(); got.Date != model.D(y, m, 1) {
		t.Errorf("the date %v is not the first of a month", got.Date)
	}
	if first := MonthStarts(rec.Today, got.Latest)[0]; got.Date != first {
		y, m, _ := got.Date.YMD()
		if pct := success(model.D(y, m-1, 1)); pct >= 90 {
			t.Errorf("one month earlier also succeeds with %v", pct)
		}
	}
}

func TestFindEarliestRetirementNotFound(t *testing.T) {
	rec := demodata.Family()
	opts := SuccessOptions{MonteCarloOptions: MonteCarloOptions{Runs: 20, Seed: 1}}
	got, err := FindEarliestRetirement(context.Background(), rec, demodata.ProjRetire60, model.DefaultAssetID(demodata.Mortgage), 90, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got.Found || got.Evaluated != 1 {
		t.Errorf("result %+v: want not found after trying only the latest date", got)
	}
}

func TestFindEarliestRetirementRefusals(t *testing.T) {
	rec := demodata.Family()
	opts := SuccessOptions{MonteCarloOptions: MonteCarloOptions{Runs: 5}}
	if _, err := FindEarliestRetirement(context.Background(), rec, "", demodata.NetWorth, 90, opts); err == nil {
		t.Error("the base was accepted")
	}
	if _, err := FindEarliestRetirement(context.Background(), rec, demodata.ProjRetire60, demodata.NetWorth, 101, opts); err == nil {
		t.Error("a target above 100 was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FindEarliestRetirement(ctx, rec, demodata.ProjRetire60, demodata.NetWorth, 90, opts); err == nil {
		t.Error("a cancelled search finished")
	}
}
