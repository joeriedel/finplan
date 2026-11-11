// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"math"
	"slices"
	"testing"
)

type tCalendar struct {
	Name      string
	Rules     []string
	Overrides []struct{ From, To string }
	Grants    int
}

type tGrant struct {
	ID           int64
	Account      string
	Symbol       string
	Date         string
	Shares       int
	Value        *float64
	Price        float64
	FirstVest    string    `json:"first_vest"`
	VestPercents []float64 `json:"vest_percents"`
	Vested       int
	Remaining    int
	Brokerage    string
}

type tVest struct {
	ID       int64
	Grant    int64
	Planned  int
	Date     string
	Shares   int
	Price    float64
	Gross    float64
	Withheld float64
	Net      float64
	TxEntry  *int64 `json:"tx_entry"`
}

type tVestWrite struct {
	DryRun  bool `json:"dry_run"`
	Changed bool
	Vest    tVest
	Price   *struct {
		ID     int64
		Symbol string
		Date   string
		Price  float64
	}
	Entry *struct {
		ID      *int64
		Kind    string
		Date    string
		Amount  float64
		TxKind  string `json:"tx_kind"`
		Counter string
	}
}

type tSchedule struct {
	Grant tGrant
	Vests []struct {
		Planned   int
		Date      string
		Shares    int
		Percent   float64
		Status    string
		Vest      *tVest
		DateDiff  *int `json:"date_diff"`
		ShareDiff *int `json:"share_diff"`
	}
}

func TestCLICalendars(t *testing.T) {
	path := seededFile(t)
	var cals []tCalendar
	cliJSON(t, &cals, "--db", path, "calendar", "list")
	if len(cals) != 1 || cals[0].Name != "Acme Corp" || !slices.Equal(cals[0].Rules, []string{"mar=3mon", "jun=3mon", "sep=3mon", "dec=2mon"}) || cals[0].Grants != 2 {
		t.Fatalf("calendars = %+v", cals)
	}
	var dates struct{ Dates []string }
	cliJSON(t, &dates, "--db", path, "calendar", "dates", "Acme Corp", "--from", "2026-01", "--to", "2027-12")
	want := []string{"2026-03-16", "2026-06-15", "2026-09-21", "2026-12-14", "2027-03-15", "2027-06-21", "2027-09-20", "2027-12-13"}
	if !slices.Equal(dates.Dates, want) {
		t.Errorf("dates = %v", dates.Dates)
	}
	var w struct {
		DryRun   bool `json:"dry_run"`
		Calendar tCalendar
	}
	cliJSON(t, &w, "--db", path, "calendar", "new", "Beta", "--rules", "feb=lastthu,may=15", "--overrides", "2027-05-15=2027-05-17", "--dry-run")
	if !w.DryRun || w.Calendar.Name != "Beta" {
		t.Errorf("dry run: %+v", w)
	}
	cliJSON(t, &cals, "--db", path, "calendar", "list")
	if len(cals) != 1 {
		t.Fatal("a dry run saved the calendar")
	}
	cliJSON(t, &w, "--db", path, "calendar", "new", "Beta", "--rules", "feb=lastthu,may=15", "--overrides", "2027-05-15=2027-05-17")
	cliJSON(t, &dates, "--db", path, "calendar", "dates", "Beta", "--from", "2027-01", "--to", "2027-12")
	if !slices.Equal(dates.Dates, []string{"2027-02-25", "2027-05-17"}) {
		t.Errorf("Beta dates = %v", dates.Dates)
	}
	if code, _ := cliFail(t, 1, "--db", path, "calendar", "new", "Gamma", "--rules", "feb=30"); code != "invalid" {
		t.Errorf("30 Feb: %s", code)
	}
	if code, _ := cliFail(t, 2, "--db", path, "calendar", "new", "Gamma", "--rules", "feb=5thmon"); code != "usage" {
		t.Errorf("5thmon: %s", code)
	}
	if code, _ := cliFail(t, 1, "--db", path, "calendar", "new", "Beta", "--rules", "may=1"); code != "name_taken" {
		t.Errorf("name taken: %s", code)
	}
	if code, _ := cliFail(t, 1, "--db", path, "calendar", "delete", "Acme Corp"); code != "in_use" {
		t.Errorf("delete in use: %s", code)
	}
	cliJSON(t, &w, "--db", path, "calendar", "edit", "Beta", "--overrides", "")
	if len(w.Calendar.Overrides) != 0 {
		t.Errorf("overrides after edit: %+v", w.Calendar)
	}
	cliJSON(t, new(any), "--db", path, "calendar", "delete", "Beta")
}

func addGrant(t *testing.T, path string) tGrant {
	t.Helper()
	var w struct{ Grant tGrant }
	cliJSON(t, &w, "--db", path, "grant", "add", "equity", "--symbol", "acme", "--date", "2027-02-01", "--value", "100000", "--price", "187.50",
		"--calendar", "Acme Corp", "--first-vest", "2027-03-15", "--year-percents", "25,25,25,25", "--brokerage", "brokerage")
	return w.Grant
}

func TestCLIGrantVestAndSchedule(t *testing.T) {
	path := seededFile(t)
	var grants []tGrant
	cliJSON(t, &grants, "--db", path, "account", "grants", "equity")
	if len(grants) != 2 || grants[0].Shares != 533 || grants[0].Vested != 7*33 || grants[1].Shares != 292 {
		t.Fatalf("demo grants = %+v", grants)
	}
	g := addGrant(t, path)
	if g.ID == 0 || g.Shares != 533 || g.Symbol != "ACME" || len(g.VestPercents) != 16 || g.Remaining != 533 {
		t.Fatalf("new grant = %+v", g)
	}
	var before []map[string]any
	cliJSON(t, &before, "--db", path, "account", "entries", "brokerage")
	var dry tVestWrite
	cliJSON(t, &dry, "--db", path, "vest", "record", fmt.Sprint(g.ID), "--planned", "1", "--price", "210", "--dry-run")
	if !dry.DryRun || dry.Vest.ID != 0 || dry.Entry == nil || dry.Entry.ID != nil {
		t.Errorf("dry run = %+v", dry)
	}
	var w tVestWrite
	cliJSON(t, &w, "--db", path, "vest", "record", fmt.Sprint(g.ID), "--planned", "1", "--price", "210")
	v := w.Vest
	if v.Shares != 33 || v.Date != "2027-03-15" || v.Gross != 6930 || v.Withheld != 2453.22 || v.Net != 4476.78 || v.TxEntry == nil {
		t.Fatalf("vest = %+v", v)
	}
	if w.Price == nil || w.Price.Price != 210 || w.Price.Date != "2027-03-15" || w.Entry == nil || w.Entry.Amount != 4476.78 || w.Entry.TxKind != "transfer" || w.Entry.Counter != "equity" || *w.Entry.ID != *v.TxEntry {
		t.Fatalf("price %+v entry %+v", w.Price, w.Entry)
	}
	var after []map[string]any
	cliJSON(t, &after, "--db", path, "account", "entries", "brokerage")
	if len(after) != len(before)+1 {
		t.Errorf("brokerage entries %d -> %d", len(before), len(after))
	}
	var prices []struct {
		ID    int64
		Date  string
		Price float64
	}
	cliJSON(t, &prices, "--db", path, "price", "history", "acme")
	if i := slices.IndexFunc(prices, func(p struct {
		ID    int64
		Date  string
		Price float64
	}) bool {
		return p.Date == "2027-03-15"
	}); i < 0 || prices[i].Price != 210 {
		t.Errorf("price history lacks the vest's price: %+v", prices)
	}
	var s tSchedule
	cliJSON(t, &s, "--db", path, "grant", "schedule", fmt.Sprint(g.ID))
	if len(s.Vests) != 16 || s.Vests[0].Status != "recorded" || s.Vests[0].Vest == nil || s.Vests[0].Vest.ID != v.ID || *s.Vests[0].DateDiff != 0 || s.Vests[1].Status != "planned" || s.Vests[1].Vest != nil || s.Grant.Vested != 33 {
		t.Errorf("schedule = %+v", s)
	}
	// A second vest tied to an existing deposit records no deposit.
	var dep struct{ Entry struct{ ID int64 } }
	cliJSON(t, &dep, "--db", path, "entry", "add-transaction", "brokerage", "--date", "2027-06-23", "--amount", "4000", "--kind", "transfer", "--counter", "equity")
	var tied tVestWrite
	cliJSON(t, &tied, "--db", path, "vest", "record", fmt.Sprint(g.ID), "--planned", "2", "--price", "211", "--date", "2027-06-22", "--entry", fmt.Sprint(dep.Entry.ID))
	if tied.Entry != nil || tied.Vest.TxEntry == nil || *tied.Vest.TxEntry != dep.Entry.ID {
		t.Errorf("tied vest = %+v", tied)
	}
	cliJSON(t, &after, "--db", path, "account", "entries", "brokerage")
	if len(after) != len(before)+2 {
		t.Errorf("brokerage entries after the tied vest: %d, want %d", len(after), len(before)+2)
	}
	cliJSON(t, &s, "--db", path, "grant", "schedule", fmt.Sprint(g.ID))
	if *s.Vests[1].DateDiff != 1 || *s.Vests[1].ShareDiff != 0 {
		t.Errorf("row 2 = %+v", s.Vests[1])
	}
	for _, args := range [][]string{
		{"vest", "record", fmt.Sprint(g.ID), "--planned", "1", "--price", "210"},
		{"vest", "record", fmt.Sprint(g.ID), "--planned", "3", "--price", "210", "--date", "2027-01-01"},
		{"vest", "record", fmt.Sprint(g.ID), "--planned", "3", "--price", "210", "--shares", "999"},
	} {
		if code, _ := cliFail(t, 1, append([]string{"--db", path}, args...)...); code != "invalid" {
			t.Errorf("%v: %s", args, code)
		}
	}
	if code, _ := cliFail(t, 1, "--db", path, "vest", "record", "99999", "--planned", "1", "--price", "1"); code != "not_found" {
		t.Errorf("unknown grant: %s", code)
	}
	if code, _ := cliFail(t, 1, "--db", path, "entry", "add-snapshot", "equity", "--date", "2027-01-01", "--balance", "5"); code != "invalid" {
		t.Errorf("snapshot on the equity account: %s", code)
	}
}

func TestCLIEquityBalance(t *testing.T) {
	path := seededFile(t)
	// The demo's grants leave 302 and 234 shares unvested; the price on 2026-11-06 is 233.10 and on 2026-11-05 the latest before it is 232.40.
	var bal struct {
		Samples []struct {
			Date   string
			Value  float64
			Status string
		}
	}
	for date, want := range map[string]float64{"2026-11-06": 536 * 233.10, "2026-11-05": 536 * 232.40} {
		cliJSON(t, &bal, "--db", path, "balance", "equity", "--from", date, "--to", date)
		if len(bal.Samples) != 1 || bal.Samples[0].Status != "actual" || math.Abs(bal.Samples[0].Value-want) > 1e-6 {
			t.Errorf("balance on %s = %+v, want %v", date, bal.Samples, want)
		}
	}
	// A new grant adds its shares at the latest price, through the same path.
	var g struct{ Grant tGrant }
	cliJSON(t, &g, "--db", path, "grant", "add", "equity", "--symbol", "ACME", "--date", "2026-11-01", "--shares", "100", "--price", "200",
		"--calendar", "Acme Corp", "--first-vest", "2026-12-14", "--year-percents", "100", "--brokerage", "brokerage")
	cliJSON(t, &bal, "--db", path, "balance", "equity", "--from", "2026-11-06", "--to", "2026-11-06")
	if math.Abs(bal.Samples[0].Value-636*233.10) > 1e-6 {
		t.Errorf("balance with a new grant = %+v", bal.Samples)
	}
}

func TestCLICorrectVoidHistory(t *testing.T) {
	path := seededFile(t)
	g := addGrant(t, path)
	var w tVestWrite
	cliJSON(t, &w, "--db", path, "vest", "record", fmt.Sprint(g.ID), "--planned", "1", "--price", "210")
	vid := fmt.Sprint(w.Vest.ID)
	var c struct {
		Changed bool
		Vest    tVest
	}
	cliJSON(t, &c, "--db", path, "vest", "correct", vid, "--price", "212", "--note", "typo")
	if !c.Changed || c.Vest.Price != 212 {
		t.Errorf("corrected = %+v", c)
	}
	cliJSON(t, &c, "--db", path, "vest", "correct", vid, "--price", "212")
	if c.Changed {
		t.Error("an identical correction changed")
	}
	cliFail(t, 2, "--db", path, "vest", "correct", vid)
	var hist struct {
		ID        int64
		Revisions []struct {
			Price float64
			Note  *string
			Void  bool
		}
	}
	cliJSON(t, &hist, "--db", path, "vest", "history", vid)
	if len(hist.Revisions) != 2 || hist.Revisions[0].Price != 210 || hist.Revisions[1].Price != 212 || *hist.Revisions[1].Note != "typo" {
		t.Errorf("vest history = %+v", hist)
	}
	if code, _ := cliFail(t, 1, "--db", path, "grant", "void", fmt.Sprint(g.ID)); code != "in_use" {
		t.Errorf("void grant with vest: %s", code)
	}
	cliJSON(t, new(any), "--db", path, "vest", "void", vid, "--note", "wrong")
	cliJSON(t, &hist, "--db", path, "vest", "history", vid)
	if len(hist.Revisions) != 3 || !hist.Revisions[2].Void {
		t.Errorf("history after void: %+v", hist)
	}
	var sched tSchedule
	cliJSON(t, &sched, "--db", path, "grant", "schedule", fmt.Sprint(g.ID))
	if sched.Vests[0].Status != "planned" {
		t.Error("a voided vest still shows recorded")
	}
	var gc struct{ Grant tGrant }
	cliJSON(t, &gc, "--db", path, "grant", "correct", fmt.Sprint(g.ID), "--shares", "600", "--note", "more")
	if gc.Grant.Shares != 600 {
		t.Errorf("grant correct = %+v", gc.Grant)
	}
	cliJSON(t, new(any), "--db", path, "grant", "void", fmt.Sprint(g.ID))
	var ghist struct {
		Revisions []struct {
			Shares int
			Void   bool
		}
	}
	cliJSON(t, &ghist, "--db", path, "grant", "history", fmt.Sprint(g.ID))
	if len(ghist.Revisions) != 3 || ghist.Revisions[0].Shares != 533 || ghist.Revisions[1].Shares != 600 || !ghist.Revisions[2].Void {
		t.Errorf("grant history = %+v", ghist)
	}
	// Prices.
	var p struct {
		Price struct {
			ID    int64
			Price float64
		}
	}
	cliJSON(t, &p, "--db", path, "price", "add", "ACME", "--date", "2026-11-07", "--price", "240")
	pid := fmt.Sprint(p.Price.ID)
	cliJSON(t, &p, "--db", path, "price", "correct", pid, "--price", "241", "--note", "typo")
	cliJSON(t, new(any), "--db", path, "price", "void", pid)
	var rev struct {
		Revisions []struct {
			Price float64
			Void  bool
		}
	}
	cliJSON(t, &rev, "--db", path, "price", "revisions", pid)
	if len(rev.Revisions) != 3 || rev.Revisions[0].Price != 240 || rev.Revisions[1].Price != 241 || !rev.Revisions[2].Void {
		t.Errorf("price revisions = %+v", rev)
	}
	var voided []struct {
		ID   int64
		Void bool
	}
	cliJSON(t, &voided, "--db", path, "price", "history", "ACME", "--voided")
	if i := slices.IndexFunc(voided, func(x struct {
		ID   int64
		Void bool
	}) bool {
		return x.ID == p.Price.ID
	}); i < 0 || !voided[i].Void {
		t.Errorf("price history --voided: %+v", voided)
	}
	if code, _ := cliFail(t, 1, "--db", path, "price", "revisions", "99999"); code != "not_found" {
		t.Errorf("unknown price: %s", code)
	}
}
