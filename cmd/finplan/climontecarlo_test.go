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
