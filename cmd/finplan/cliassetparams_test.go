// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"
)

type paramsOut struct {
	Asset   string
	Default []winterval
	Growth  []wsegment
	Today   string
	// Accounts hold the layers of each account's growth in the order they apply.
	Accounts []struct {
		Account string
		Name    string
		Layers  []struct {
			Source   string
			Asset    *string
			Schedule []wsegment
			Today    *float64
			Applies  bool
		}
		Effective []wsegment
		Today     *float64
	}
}

func assetParams(t *testing.T, path, asset string) paramsOut {
	t.Helper()
	var p paramsOut
	cliJSON(t, &p, "--db", path, "asset", "params", asset)
	return p
}

// cliSample is the value and status of the target on one date, from the balance command.
func cliSample(t *testing.T, path, target, date string) (float64, string) {
	t.Helper()
	var b balanceOut
	cliJSON(t, &b, "--db", path, "balance", target, "--from", date, "--to", date)
	if len(b.Samples) != 1 {
		t.Fatalf("balance of %s on %s: %+v", target, date, b)
	}
	return b.Samples[0].Value, b.Samples[0].Status
}

func TestCLIAggregateDefaultIntervalsScenario(t *testing.T) {
	path := seededFile(t)
	// The seeded Roth carries its own default before its data begins, so that default is all the aggregate has there.
	if v, s := cliSample(t, path, "retirement", "2019-03-31"); v != 70000 || s != "default" {
		t.Fatalf("before the interval: %v %s", v, s)
	}
	w := data(t, path, "", "asset", "add-default", "retirement", "--value", "200000", "--from", "2016-01-01", "--to", "2021-06")
	if d := w.Asset.Default; len(d) != 1 || d[0].Value != 200000 || *d[0].To != "2021-06-30" {
		t.Fatalf("add-default: %+v", d)
	}
	if v, s := cliSample(t, path, "retirement", "2019-03-31"); v != 200000 || s != "default" {
		t.Errorf("inside the interval: %v %s, want 200000 default", v, s)
	}
	if v, _ := cliSample(t, path, "retirement", "2021-07-31"); v != 70000 {
		t.Errorf("after the interval: %v", v)
	}
	// Dates with actual data keep the members' balances.
	if _, s := cliSample(t, path, "retirement", "2026-01-31"); s != "actual" {
		t.Errorf("status with data: %s", s)
	}

	w = data(t, path, "", "asset", "edit-default", "retirement", "0", "--value", "250000", "--from", "2016-01-01", "--to", "2022-12")
	if d := w.Asset.Default; len(d) != 1 || d[0].Value != 250000 || *d[0].To != "2022-12-31" {
		t.Fatalf("edit-default: %+v", d)
	}
	if v, s := cliSample(t, path, "retirement", "2022-03-31"); v != 250000 || s != "default" {
		t.Errorf("after the edit: %v %s", v, s)
	}
	if w := data(t, path, "", "asset", "edit-default", "retirement", "0", "--value", "250000", "--from", "2016-01-01", "--to", "2022-12"); w.Changed {
		t.Error("an edit to the same interval changed")
	}

	// Where intervals overlap the later one in the list wins, and a move changes the list.
	data(t, path, "", "asset", "add-default", "retirement", "--value", "10", "--from", "2018-01-01", "--to", "2018-12-31")
	if v, _ := cliSample(t, path, "retirement", "2018-06-30"); v != 10 {
		t.Errorf("later interval: %v, want 10", v)
	}
	w = data(t, path, "", "asset", "move-default", "retirement", "1", "0")
	if d := w.Asset.Default; len(d) != 2 || d[0].Value != 10 || d[1].Value != 250000 {
		t.Fatalf("move-default: %+v", d)
	}
	if v, _ := cliSample(t, path, "retirement", "2018-06-30"); v != 250000 {
		t.Errorf("after the move: %v, want 250000", v)
	}
	if a := assetParams(t, path, "retirement"); len(a.Default) != 2 || a.Default[0].Value != 10 {
		t.Errorf("asset params default: %+v", a.Default)
	}

	refusedWrite(t, path, "", 1, "invalid", "asset", "edit-default", "retirement", "0", "--value", "1", "--from", "2020-01-01", "--to", "2019-01-01")
	if a := assetParams(t, path, "retirement"); len(a.Default) != 2 || a.Default[0].Value != 10 || a.Default[1].Value != 250000 {
		t.Errorf("after a refused edit: %+v", a.Default)
	}

	data(t, path, "", "asset", "remove-default", "retirement", "1")
	data(t, path, "", "asset", "remove-default", "retirement", "0")
	if v, s := cliSample(t, path, "retirement", "2019-03-31"); v != 70000 || s != "default" {
		t.Errorf("after removing: %v %s", v, s)
	}
	refusedWrite(t, path, "", 1, "invalid", "asset", "edit-default", "retirement", "0", "--value", "1")
	refusedWrite(t, path, "", 1, "invalid", "asset", "move-default", "retirement", "0", "1")
	refusedWrite(t, path, "", 2, "usage", "asset", "edit-default", "retirement", "x", "--value", "1")
}

func TestCLIGrowthScheduleScenario(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "asset", "new", "Pot")
	write(t, path, "", "asset", "add-member", "pot", "--account", "savings")
	const a, b = "2027-11-08", "2029-11-08"
	base1, _ := cliSample(t, path, "pot", a)
	base2, _ := cliSample(t, path, "pot", b)

	// Flat 0 until the end of 2027, then 10%: the two dates straddle the boundary.
	flat := `[{"from":"2020-01-01","value":0}]`
	step := `[{"from":"2020-01-01","end":"2027-12-31","value":0},{"from":"2028-01-01","value":0.1}]`
	w := data(t, path, flat, "asset", "set-param", "pot")
	if g := w.Asset.Params["growth"]; len(g) != 1 || g[0].Value != 0 {
		t.Fatalf("set-param: %+v", w.Asset.Params)
	}
	flat1, _ := cliSample(t, path, "pot", a)
	flat2, _ := cliSample(t, path, "pot", b)
	if flat1 >= base1 || flat2 >= base2 {
		t.Errorf("a 0%% schedule did not replace the account's rate: %v %v against %v %v", flat1, flat2, base1, base2)
	}
	w = data(t, path, step, "asset", "set-param", "pot")
	if g := w.Asset.Params["growth"]; len(g) != 2 || g[1].Value != 0.1 {
		t.Fatalf("set-param schedule: %+v", w.Asset.Params)
	}
	step1, _ := cliSample(t, path, "pot", a)
	step2, _ := cliSample(t, path, "pot", b)
	if step1 != flat1 {
		t.Errorf("before the boundary: %v, want %v", step1, flat1)
	}
	if step2 <= flat2 {
		t.Errorf("after the boundary: %v, want more than %v", step2, flat2)
	}
	if p := assetParams(t, path, "pot"); len(p.Growth) != 2 || len(p.Accounts) != 1 || len(p.Accounts[0].Effective) != 3 {
		t.Errorf("asset params: %+v", p)
	}

	write(t, path, "", "asset", "set-param", "pot", "--clear")
	if v, _ := cliSample(t, path, "pot", b); v != base2 {
		t.Errorf("after clearing: %v, want %v", v, base2)
	}
}

func TestCLIParameterSettingsApplyInListOrderScenario(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "asset", "new", "Inner")
	write(t, path, "", "asset", "add-member", "inner", "--account", "savings")
	write(t, path, "", "asset", "new", "Outer")
	write(t, path, "", "asset", "add-member", "outer", "--asset", "inner")
	write(t, path, "", "asset", "set-param", "inner", "--value", "0.05")
	// The outer asset applies after the inner one, but only for 2027.
	write(t, path, `[{"from":"2027-01-01","end":"2027-12-31","value":0.08}]`, "asset", "set-param", "outer")

	p := assetParams(t, path, "outer")
	if len(p.Accounts) != 1 || p.Accounts[0].Account != "savings" || p.Today != "2026-11-08" {
		t.Fatalf("params: %+v", p)
	}
	acct := p.Accounts[0]
	var sources []string
	for _, l := range acct.Layers {
		if l.Asset == nil {
			sources = append(sources, l.Source)
		} else {
			sources = append(sources, *l.Asset)
		}
	}
	if want := []string{"stated_rate", "inner", "outer"}; !reflect.DeepEqual(sources, want) {
		t.Fatalf("layers %v, want %v", sources, want)
	}
	// Today the outer schedule has no value, so the inner constant shows through and the stated rate is overwritten.
	if acct.Today == nil || *acct.Today != 0.05 || acct.Layers[0].Applies || !acct.Layers[1].Applies || acct.Layers[2].Applies || acct.Layers[2].Today != nil {
		t.Errorf("today: %+v", acct)
	}
	// Where the outer schedule covers, it wins; outside, the inner shows through.
	want := []wsegment{{Value: 0.05}, {From: ptr("2027-01-01"), Value: 0.08}, {From: ptr("2028-01-01"), Value: 0.05}}
	if len(acct.Effective) != 3 {
		t.Fatalf("effective %+v", acct.Effective)
	}
	for i, s := range acct.Effective {
		if s.Value != want[i].Value {
			t.Errorf("effective %d: %+v, want value %v", i, s, want[i].Value)
		}
	}
	// A segment runs to the start of the next, so the 2027 segment has no end of its own.
	if e := acct.Effective; e[0].From != nil || e[1].From == nil || *e[1].From != "2027-01-01" || e[2].From == nil || *e[2].From != "2028-01-01" {
		t.Errorf("effective bounds %+v", e)
	}

	// With the outer setting covering today, it is the one that applies and the inner is overwritten.
	write(t, path, "", "asset", "set-param", "outer", "--value", "0.08")
	p = assetParams(t, path, "outer")
	acct = p.Accounts[0]
	if acct.Today == nil || *acct.Today != 0.08 || acct.Layers[1].Applies || !acct.Layers[2].Applies {
		t.Errorf("outer covering today: %+v", acct)
	}

	// The engine layers the same way: the aggregate grows like one that carries the outer setting alone.
	write(t, path, "", "asset", "new", "Ref")
	write(t, path, "", "asset", "add-member", "ref", "--account", "savings")
	write(t, path, "", "asset", "set-param", "ref", "--value", "0.08")
	got, _ := cliSample(t, path, "outer", "2029-11-08")
	ref, _ := cliSample(t, path, "ref", "2029-11-08")
	if got != ref {
		t.Errorf("outer %v, reference %v", got, ref)
	}
}

func ptr(s string) *string { return &s }
