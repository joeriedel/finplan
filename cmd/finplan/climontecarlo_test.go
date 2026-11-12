// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"
)

type monteCarloOut struct {
	Target      string
	Projection  *string
	Runs        int
	Seed        uint64
	Percentiles []float64
	Dates       []string
	Bands       []struct {
		Percentile float64
		Values     []float64
	}
	SuccessAge int      `json:"success_age"`
	Through    *string  `json:"success_through"`
	Success    *float64 `json:"success_percent"`
}

func TestCLIMonteCarlo(t *testing.T) {
	args := []string{"--demo", "montecarlo", "net-worth", "--from", "2027-01", "--to", "2031-01", "--step", "year", "--projection", "Base", "--runs", "30"}
	var m monteCarloOut
	cliJSON(t, &m, args...)
	if m.Target != "net-worth" || *m.Projection != "Base" || m.Runs != 30 || m.Seed != 1 || !reflect.DeepEqual(m.Percentiles, []float64{10, 25, 50, 75, 90}) {
		t.Errorf("montecarlo = %+v", m)
	}
	if len(m.Dates) != 5 || m.Dates[0] != "2027-01-01" || len(m.Bands) != 5 || m.Bands[2].Percentile != 50 || len(m.Bands[0].Values) != 5 {
		t.Errorf("shape: dates %v, bands %v", m.Dates, m.Bands)
	}
	if m.Bands[0].Values[4] >= m.Bands[4].Values[4] {
		t.Errorf("bands do not spread: %v, %v", m.Bands[0].Values, m.Bands[4].Values)
	}

	var same, other monteCarloOut
	cliJSON(t, &same, args...)
	cliJSON(t, &other, append(args, "--seed", "2")...)
	if !reflect.DeepEqual(m, same) || reflect.DeepEqual(m.Bands, other.Bands) || other.Seed != 2 {
		t.Error("the seed does not decide the bands")
	}

	var custom monteCarloOut
	cliJSON(t, &custom, append(args, "--percentiles", "0, 50,100")...)
	if !reflect.DeepEqual(custom.Percentiles, []float64{0, 50, 100}) || len(custom.Bands) != 3 || custom.Bands[0].Values[4] > custom.Bands[2].Values[4] {
		t.Errorf("custom percentiles: %+v", custom)
	}
}

func TestCLIMonteCarloRefusesBadFlags(t *testing.T) {
	base := []string{"--demo", "montecarlo", "net-worth", "--from", "2027-01", "--to", "2027-12"}
	for name, extra := range map[string][]string{
		"no runs":              {"--runs", "0"},
		"negative runs":        {"--runs", "-3"},
		"fractional runs":      {"--runs", "2.5"},
		"negative seed":        {"--seed", "-1"},
		"percentile too big":   {"--percentiles", "50,101"},
		"percentile negative":  {"--percentiles", "-1"},
		"percentile not a num": {"--percentiles", "median"},
		"empty percentile":     {"--percentiles", "10,,90"},
	} {
		if code, _ := cliFail(t, 2, append(append([]string{}, base...), extra...)...); code != "usage" {
			t.Errorf("%s: code %s", name, code)
		}
	}
}

// TestCLIMonteCarloSuccess is the success figure of net worth under Base: bands and a percent from 0 to 100.
func TestCLIMonteCarloSuccess(t *testing.T) {
	args := []string{"--demo", "montecarlo", "net-worth", "--from", "2026-12", "--to", "2080-12", "--step", "year", "--projection", "Base", "--runs", "40"}
	var m monteCarloOut
	cliJSON(t, &m, args...)
	if len(m.Bands) != 5 || m.SuccessAge != 95 || m.Through == nil || *m.Through != "2080-03-12" || m.Success == nil || *m.Success < 0 || *m.Success > 100 {
		t.Errorf("success: age %d through %v percent %v, %d bands", m.SuccessAge, m.Through, m.Success, len(m.Bands))
	}
	var early monteCarloOut
	cliJSON(t, &early, append(args, "--success-age", "50")...)
	if early.Success == nil || *early.Success < *m.Success || early.SuccessAge != 50 || *early.Through != "2035-03-12" {
		t.Errorf("an earlier success age should not lower the success: %v against %v", early.Success, m.Success)
	}
	var past monteCarloOut
	cliJSON(t, &past, append(args, "--success-age", "20")...)
	if past.Success != nil {
		t.Errorf("an age before today has success %v", *past.Success)
	}
	if code, _ := cliFail(t, 2, append(args, "--success-age", "0")...); code != "usage" {
		t.Errorf("success age 0: code %s", code)
	}
}

type earliestOut struct {
	Target     string  `json:"target"`
	Projection string  `json:"projection"`
	SuccessAge int     `json:"success_age"`
	Want       float64 `json:"success_target"`
	Runs       int     `json:"runs"`
	Seed       uint64  `json:"seed"`
	Found      bool    `json:"found"`
	Date       *string `json:"retirement_date"`
	Success    float64 `json:"success_percent"`
	Latest     string  `json:"latest_date"`
	Tried      int     `json:"dates_tried"`
}

// TestCLIGoalEarliestRetirement finds a date, sets it with the write command and sees the success it promised.
func TestCLIGoalEarliestRetirement(t *testing.T) {
	path := seededFile(t)
	var g earliestOut
	cliJSON(t, &g, "--db", path, "goal", "earliest-retirement", "net-worth", "--projection", "Retire at 60", "--runs", "100")
	if !g.Found || g.Date == nil || g.Success < 90 || g.Want != 90 || g.SuccessAge != 95 || g.Runs != 100 || g.Seed != 1 || g.Projection != "Retire at 60" || g.Target != "net-worth" || g.Tried < 2 {
		t.Fatalf("goal = %+v", g)
	}
	write(t, path, "", "projection", "set-retirement", "Retire at 60", *g.Date)
	var m monteCarloOut
	cliJSON(t, &m, "--db", path, "montecarlo", "net-worth", "--from", "2026-12", "--to", "2080-12", "--step", "month", "--projection", "Retire at 60", "--runs", "100", "--success-age", "95")
	if m.Success == nil || *m.Success != g.Success {
		t.Errorf("after setting %s the success is %v, the goal said %v", *g.Date, m.Success, g.Success)
	}
}

func TestCLIGoalEarliestRetirementNotFoundAndRefusals(t *testing.T) {
	var g earliestOut
	cliJSON(t, &g, "--demo", "goal", "earliest-retirement", "mortgage", "--projection", "Base", "--runs", "10")
	if g.Found || g.Date != nil || g.Tried != 1 {
		t.Errorf("a mortgage is never above zero: %+v", g)
	}
	base := []string{"--demo", "goal", "earliest-retirement", "net-worth", "--projection", "Base", "--runs", "10"}
	for name, extra := range map[string][]string{
		"success over 100": {"--success", "101"},
		"success negative": {"--success", "-1"},
		"no runs":          {"--runs", "0"},
		"negative seed":    {"--seed", "-1"},
		"age zero":         {"--success-age", "0"},
	} {
		if code, _ := cliFail(t, 2, append(append([]string{}, base...), extra...)...); code != "usage" {
			t.Errorf("%s: code %s", name, code)
		}
	}
	if code, _ := cliFail(t, 2, "--demo", "goal", "earliest-retirement", "net-worth"); code != "usage" {
		t.Errorf("no projection: code %s", code)
	}
	if code, _ := cliFail(t, 1, "--demo", "goal", "earliest-retirement", "net-worth", "--projection", "Nope"); code != "not_found" {
		t.Errorf("unknown projection: code %s", code)
	}
}
