// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
)

type tMarkets struct {
	Shocks []struct {
		Index   int
		Class   string
		Date    string
		Percent float64
	}
	Rebalances []struct {
		Index      int
		Account    string
		Date       string
		Allocation []tHolding
	}
}

func marketsOf(t *testing.T, path, projection string) tMarkets {
	t.Helper()
	var out tMarkets
	cliJSON(t, &out, "--db", path, "projection", "show", projection)
	return out
}

func TestCLIShocks(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "shock", "add", "Base", "--class", "US large cap", "--date", "2027-03-02", "--percent", "-40")
	write(t, path, "", "shock", "add", "Base", "--class", "us-bonds", "--date", "2028-01", "--percent", "5.5")
	got := marketsOf(t, path, "Base")
	if len(got.Shocks) != 2 || got.Shocks[0].Class != "us-large-cap" || got.Shocks[0].Date != "2027-03-02" || got.Shocks[0].Percent != -40 || got.Shocks[1].Index != 1 || got.Shocks[1].Percent != 5.5 || got.Shocks[1].Date != "2028-01-01" {
		t.Fatalf("shocks: %+v", got.Shocks)
	}
	write(t, path, "", "shock", "edit", "Base", "0", "--percent", "-25")
	write(t, path, "", "shock", "edit", "Base", "0", "--class", "us-bonds", "--date", "2027-04-01")
	if got = marketsOf(t, path, "Base"); got.Shocks[0].Percent != -25 || got.Shocks[0].Class != "us-bonds" || got.Shocks[0].Date != "2027-04-01" {
		t.Errorf("edited shock: %+v", got.Shocks[0])
	}
	if w := write(t, path, "", "shock", "remove", "Base", "0"); !w.Changed {
		t.Errorf("remove: %+v", w)
	}
	if got = marketsOf(t, path, "Base"); len(got.Shocks) != 1 || got.Shocks[0].Percent != 5.5 {
		t.Errorf("after the remove: %+v", got.Shocks)
	}
	refusedWrite(t, path, "", 1, "invalid", "shock", "add", "Base", "--class", "us-bonds", "--date", "2027-03-02", "--percent", "-100")
	refusedWrite(t, path, "", 1, "unknown_target", "shock", "add", "Base", "--class", "gold", "--date", "2027-03-02", "--percent", "-10")
	refusedWrite(t, path, "", 1, "invalid", "shock", "remove", "Base", "3")
	refusedWrite(t, path, "", 1, "not_found", "shock", "add", "Nope", "--class", "us-bonds", "--date", "2027-03-02", "--percent", "-10")
}

func TestCLIRebalances(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "rebalance", "add", "Base", "--account", "brokerage", "--date", "2027-09-01", "--set", "us-large-cap=60,US bonds=40")
	got := marketsOf(t, path, "Base")
	if len(got.Rebalances) != 1 || got.Rebalances[0].Account != "brokerage" || got.Rebalances[0].Date != "2027-09-01" ||
		len(got.Rebalances[0].Allocation) != 2 || got.Rebalances[0].Allocation[0] != (tHolding{"us-large-cap", 60}) || got.Rebalances[0].Allocation[1] != (tHolding{"us-bonds", 40}) {
		t.Fatalf("rebalances: %+v", got.Rebalances)
	}
	write(t, path, "", "rebalance", "edit", "Base", "0", "--set", "us-large-cap=70,us-bonds=30", "--date", "2027-10-01")
	if got = marketsOf(t, path, "Base"); got.Rebalances[0].Allocation[0].Percent != 70 || got.Rebalances[0].Date != "2027-10-01" || got.Rebalances[0].Account != "brokerage" {
		t.Errorf("edited rebalance: %+v", got.Rebalances[0])
	}
	refusedWrite(t, path, "", 1, "invalid", "rebalance", "add", "Base", "--account", "brokerage", "--date", "2027-09-01", "--set", "us-large-cap=60,us-bonds=30")
	refusedWrite(t, path, "", 1, "invalid", "rebalance", "add", "Base", "--account", "checking", "--date", "2027-09-01", "--set", "us-large-cap=100")
	refusedWrite(t, path, "", 1, "invalid", "rebalance", "add", "Base", "--account", "brokerage", "--date", "2027-09-01", "--set", "us-large-cap=50,us-large-cap=50")
	refusedWrite(t, path, "", 1, "unknown_target", "rebalance", "add", "Base", "--account", "brokerage", "--date", "2027-09-01", "--set", "gold=100")
	write(t, path, "", "rebalance", "remove", "Base", "0")
	if got = marketsOf(t, path, "Base"); len(got.Rebalances) != 0 {
		t.Errorf("after the remove: %+v", got.Rebalances)
	}
}

func TestCLIYearlyRebalance(t *testing.T) {
	path := seededFile(t)
	yearly := func() bool {
		var accts []struct {
			ID              string
			RebalanceYearly bool `json:"rebalance_yearly"`
		}
		cliJSON(t, &accts, "--db", path, "account", "list")
		for _, a := range accts {
			if a.ID == "brokerage" {
				return a.RebalanceYearly
			}
		}
		t.Fatal("no brokerage")
		return false
	}
	if yearly() {
		t.Fatal("the yearly rebalance starts on")
	}
	write(t, path, "", "account", "rebalance", "brokerage", "--yearly", "on")
	if !yearly() {
		t.Error("yearly on did not stick")
	}
	if w := write(t, path, "", "account", "rebalance", "brokerage", "--yearly", "on"); w.Changed {
		t.Errorf("on again changed: %+v", w)
	}
	write(t, path, "", "account", "rebalance", "brokerage", "--yearly", "off")
	if yearly() {
		t.Error("yearly off did not stick")
	}
	refusedWrite(t, path, "", 1, "invalid", "account", "rebalance", "checking", "--yearly", "on")
}

func TestCLITaxYearShowsRealizedGains(t *testing.T) {
	path := seededFile(t)
	var out struct {
		Inputs struct {
			Short float64 `json:"short_term_gains"`
			Long  float64 `json:"long_term_gains"`
		}
		Tax struct {
			GainsTax float64 `json:"gains_tax"`
		}
	}
	cliJSON(t, &out, "--db", path, "tax", "year", "2027", "--projection", "Crash and rebalance")
	if out.Inputs.Short == 0 && out.Inputs.Long == 0 || out.Tax.GainsTax == 0 {
		t.Errorf("the demo's rebalance realized %+v, gains tax %v", out.Inputs, out.Tax.GainsTax)
	}
	cliJSON(t, &out, "--db", path, "tax", "year", "2027", "--projection", "Base")
	if out.Inputs.Short != 0 || out.Inputs.Long != 0 || out.Tax.GainsTax != 0 {
		t.Errorf("the base projection realized %+v, gains tax %v", out.Inputs, out.Tax.GainsTax)
	}
}
