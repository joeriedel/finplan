// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"
)

type tProjectionShow struct {
	Modifications []struct {
		Target         struct{ Kind, ID string }
		Property, Mode string
		Schedule       []struct {
			From  string
			End   *string
			Value float64
		}
		Grant *struct {
			Symbol          string
			Value           float64
			Date            string
			Yearly          bool
			Last            *string
			Calendar        string
			YearPercents    []float64 `json:"year_percents"`
			CliffMonths     int       `json:"cliff_months"`
			FirstVestMonths int       `json:"first_vest_months"`
			Federal, State  float64
			FICA            float64
			Deposit         string
		}
	}
}

func TestCLIStockModAndGrowth(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Acme up", "--parent", "Base")
	base := lastValue(t, path, "brokerage", "Acme up", "2028-01", "2028-12")
	write(t, path, "", "mod", "add", "Acme up", "--target", "stock:ACME", "--property", "growth", "--value", "0.15")
	var show tProjectionShow
	cliJSON(t, &show, "--db", path, "projection", "show", "Acme up")
	if m := show.Modifications[0]; m.Target.Kind != "stock" || m.Target.ID != "ACME" || m.Property != "growth" || m.Schedule[0].Value != 0.15 {
		t.Errorf("stored modification: %+v", m)
	}
	var b balanceOut
	cliJSON(t, &b, "--db", path, "balance", "equity", "--from", "2028-01", "--to", "2028-01", "--projection", "Acme up")
	if len(b.Diagnostics.Skipped) != 0 {
		t.Errorf("skipped %v", b.Diagnostics.Skipped)
	}
	if up := lastValue(t, path, "brokerage", "Acme up", "2028-01", "2028-12"); !(up > base) {
		t.Errorf("brokerage under +15%%: %v, base %v", up, base)
	}
	write(t, path, "", "mod", "add", "Acme up", "--target", "stock:ACME", "--property", "growth", "--mode", "scale", "--value", "0.5")
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "add", "Acme up", "--target", "stock:NOPE", "--property", "growth", "--value", "0.1")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Acme up", "--target", "stock:", "--property", "growth", "--value", "0.1")

	// The base growth of the stock is a setter of its own.
	before := lastValue(t, path, "equity", "Base", "2028-01", "2028-01")
	w := write(t, path, "", "stock", "growth", "ACME", "--value", "0.2")
	if !w.Changed {
		t.Errorf("stock growth: %+v", w)
	}
	if after := lastValue(t, path, "equity", "Base", "2028-01", "2028-01"); !(after > before) {
		t.Errorf("equity under 20%% base growth: %v, before %v", after, before)
	}
	write(t, path, "", "stock", "growth", "ACME", "--clear")
	if again := lastValue(t, path, "equity", "Base", "2028-01", "2028-01"); again != before {
		t.Errorf("equity after clearing: %v, want %v", again, before)
	}
	write(t, path, `[{"from":"2027-01-01","end":"2027-12-31","value":-0.4}]`, "stock", "growth", "ACME")
	refusedWrite(t, path, "", 1, "unknown_target", "stock", "growth", "NOPE", "--value", "0.1")
	refusedWrite(t, path, "", 1, "invalid", "stock", "growth", "ACME", "--value", "-1")
	refusedWrite(t, path, "", 2, "usage", "stock", "growth", "ACME", "--clear", "--value", "0.1")
}

func TestCLIFocalGrantMod(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Yearly", "--parent", "Base")
	base := lastValue(t, path, "equity", "Base", "2027-03", "2027-03")
	write(t, path, "", "mod", "add", "Yearly", "--target", "account:equity", "--property", "add_grant", "--symbol", "ACME", "--value", "80000",
		"--from", "2027-03-01", "--yearly", "--calendar", "Acme Corp", "--year-percents", "40,30,20,10", "--first-vest-months", "1", "--deposit", "brokerage")
	var show tProjectionShow
	cliJSON(t, &show, "--db", path, "projection", "show", "Yearly")
	g := show.Modifications[0].Grant
	if g == nil || g.Symbol != "ACME" || g.Value != 80000 || !g.Yearly || g.Date != "2027-03-01" || g.Calendar != "Acme Corp" ||
		len(g.YearPercents) != 4 || g.FirstVestMonths != 1 || g.Deposit != "brokerage" || g.Federal != 0.22 || g.Last != nil {
		t.Errorf("stored grant: %+v", g)
	}
	if got := lastValue(t, path, "equity", "Yearly", "2027-03", "2027-03"); !(got > base) {
		t.Errorf("equity after the first focal grant: %v, base %v", got, base)
	}
	var b balanceOut
	cliJSON(t, &b, "--db", path, "balance", "equity", "--from", "2027-03", "--to", "2027-03", "--projection", "Yearly")
	if len(b.Diagnostics.Skipped) != 0 {
		t.Errorf("skipped %v", b.Diagnostics.Skipped)
	}
	write(t, path, "", "mod", "add", "Yearly", "--target", "account:equity", "--property", "add_grant", "--value", "10000", "--symbol", "ACME",
		"--from", "2030-03-01", "--calendar", "Acme Corp", "--year-percents", "100", "--deposit", "Brokerage", "--federal", "0.24", "--state", "0.05", "--fica", "0.0765")
	cliJSON(t, &show, "--db", path, "projection", "show", "Yearly")
	if g := show.Modifications[1].Grant; g == nil || g.Yearly || g.Federal != 0.24 || g.State != 0.05 {
		t.Errorf("second grant: %+v", g)
	}
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Yearly", "--target", "account:equity", "--property", "add_grant", "--symbol", "ACME", "--value", "1000", "--from", "2027-03-01", "--calendar", "Acme Corp", "--deposit", "brokerage")
	refusedWrite(t, path, "", 1, "not_found", "mod", "add", "Yearly", "--target", "account:equity", "--property", "add_grant", "--symbol", "ACME", "--value", "1000", "--from", "2027-03-01", "--calendar", "Nope", "--year-percents", "100", "--deposit", "brokerage")
	refusedWrite(t, path, "", 1, "invalid", "mod", "add", "Yearly", "--target", "account:equity", "--property", "add_grant", "--symbol", "ACME", "--value", "1000", "--from", "2027-03-01", "--calendar", "Acme Corp", "--year-percents", "60,30", "--deposit", "brokerage")
	refusedWrite(t, path, "", 1, "invalid", "mod", "edit", "Yearly", "1", "--value", "0.1")
}

func TestCLIBreakdownOfEquityAccount(t *testing.T) {
	var bd struct {
		Total float64
		Lines []struct {
			Cause     string
			Actual    float64
			Projected float64
		}
	}
	cliJSON(t, &bd, "--demo", "breakdown", "equity", "--from", "2026-01", "--to", "2027-12", "--projection", "Acme yearly grants")
	got := map[string]struct{ Actual, Projected float64 }{}
	for _, l := range bd.Lines {
		got[l.Cause] = struct{ Actual, Projected float64 }{l.Actual, l.Projected}
	}
	for _, c := range []string{"grant", "vest", "growth", "tax"} {
		if _, ok := got[c]; !ok {
			t.Errorf("no %s line in %+v", c, bd.Lines)
		}
	}
	// The January 2026 grant and the recorded vests are actual; the planned vests after today and the focal grant of 2027 are projected.
	if got["vest"].Actual >= 0 || got["vest"].Projected >= 0 || got["grant"].Actual <= 0 || got["grant"].Projected <= 0 || got["tax"].Actual >= 0 || got["tax"].Projected >= 0 {
		t.Errorf("lines: %+v", got)
	}
	if got["growth"].Actual == 0 {
		t.Errorf("actual growth of the stock price: %+v", got["growth"])
	}
	if _, ok := got["unexplained"]; ok {
		t.Errorf("unexplained %+v", got["unexplained"])
	}
	sum := 0.0
	for _, l := range bd.Lines {
		sum += l.Actual + l.Projected
	}
	if sum != bd.Total {
		t.Errorf("lines add up to %v, total %v", sum, bd.Total)
	}
}

func TestCLIStockVolatility(t *testing.T) {
	path := seededFile(t)
	var w struct {
		Changed    bool
		Volatility *float64
		Growth     any
	}
	cliWrite := func(args ...string) {
		t.Helper()
		w.Volatility, w.Growth = nil, nil
		code, out := wcli(t, "", append([]string{"--db", path, "stock", "growth", "ACME"}, args...)...)
		if code != 0 {
			t.Fatalf("stock growth %v exited %d: %s", args, code, out)
		}
		if err := json.Unmarshal(out, &w); err != nil {
			t.Fatal(err)
		}
	}
	before := lastValue(t, path, "equity", "Base", "2028-01", "2028-01")
	cliWrite("--volatility", "0.5")
	if !w.Changed || w.Volatility == nil || *w.Volatility != 0.5 || w.Growth != nil {
		t.Errorf("set volatility: %+v", w)
	}
	if lastValue(t, path, "equity", "Base", "2028-01", "2028-01") != before {
		t.Error("the volatility changed the deterministic line")
	}
	cliWrite("--volatility", "0.5")
	if w.Changed {
		t.Error("setting the volatility again changed")
	}
	cliWrite("--volatility", "0.4", "--value", "0.1")
	if !w.Changed || *w.Volatility != 0.4 || w.Growth == nil {
		t.Errorf("growth and volatility: %+v", w)
	}
	cliWrite("--clear-volatility")
	if !w.Changed || *w.Volatility != 0.35 {
		t.Errorf("clear volatility: %+v", w)
	}
	refusedWrite(t, path, "", 1, "unknown_target", "stock", "growth", "NOPE", "--volatility", "0.2")
	refusedWrite(t, path, "", 1, "invalid", "stock", "growth", "ACME", "--volatility", "-0.1")
	refusedWrite(t, path, "", 2, "usage", "stock", "growth", "ACME", "--volatility", "0.2", "--clear-volatility")
	refusedWrite(t, path, "", 1, "invalid", "stock", "growth", "ACME", "--value", "0.08", "--volatility", "11")
}
