// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

type periodRow struct {
	Item        string   `json:"item"`
	Start       string   `json:"period_start"`
	End         string   `json:"period_end"`
	Timing      string   `json:"timing"`
	Budgeted    float64  `json:"budgeted"`
	Expected    *float64 `json:"expected"`
	Posted      float64  `json:"posted"`
	PostedAuto  bool     `json:"posted_auto"`
	Status      string   `json:"status"`
	Remaining   float64  `json:"remaining"`
	Variance    float64  `json:"variance"`
	OccurrenceI *int64   `json:"occurrence_id"`
	History     []struct {
		Start    string  `json:"period_start"`
		Variance float64 `json:"variance"`
	} `json:"history"`
}

func periodRows(t *testing.T, path string, args ...string) map[string]periodRow {
	t.Helper()
	var rows []periodRow
	cliJSON(t, &rows, append([]string{"--db", path, "budget", "period", "list"}, args...)...)
	out := map[string]periodRow{}
	for _, r := range rows {
		out[r.Item] = r
	}
	return out
}

type periodWrite struct {
	DryRun  bool      `json:"dry_run"`
	Changed bool      `json:"changed"`
	Period  periodRow `json:"period"`
}

func periodWritten(t *testing.T, path string, args ...string) periodWrite {
	t.Helper()
	code, out := wcli(t, "", append([]string{"--db", path, "budget", "period", "set"}, args...)...)
	if code != 0 {
		t.Fatalf("budget period set %v exited %d: %s", args, code, out)
	}
	var w periodWrite
	if err := json.Unmarshal(out, &w); err != nil {
		t.Fatalf("budget period set %v: %v\n%s", args, err, out)
	}
	return w
}

func TestCLIBudgetPeriodListAndSet(t *testing.T) {
	path := seededFile(t)
	rows := periodRows(t, path)
	g := rows["groceries"]
	if g.Start != "2026-11-03" || g.End != "2026-12-02" || g.Timing != "spread" || g.Posted != 2050 || g.PostedAuto || g.Status != "auto" ||
		g.Remaining != 0 || g.Variance != 2050-g.Budgeted || g.Variance <= 0 || g.OccurrenceI == nil || len(g.History) == 0 || len(g.History) > 6 {
		t.Errorf("groceries %+v", g)
	}
	d := rows["dining"]
	if d.Timing != "spread" || !d.PostedAuto || d.OccurrenceI != nil || d.Status != "auto" || d.Remaining <= 0 || d.Remaining > d.Budgeted {
		t.Errorf("dining %+v", d)
	}
	if rows["salary-a"].Timing != "lump" || rows["insurance"].Timing != "lump" {
		t.Errorf("timings %q %q", rows["salary-a"].Timing, rows["insurance"].Timing)
	}

	// A dry run shows the result and stores nothing.
	w := periodWritten(t, path, "dining", "--status", "pending", "--posted", "100", "--dry-run")
	if !w.DryRun || !w.Changed || w.Period.Status != "pending" || w.Period.Posted != 100 || w.Period.PostedAuto || w.Period.OccurrenceI != nil {
		t.Errorf("dry run %+v", w)
	}
	if got := periodRows(t, path)["dining"]; !reflect.DeepEqual(got, d) {
		t.Errorf("a dry run changed dining: %+v", got)
	}
	w = periodWritten(t, path, "dining", "--status", "pending", "--posted", "100", "--expected", "950")
	if w.DryRun || !w.Changed || w.Period.Status != "pending" || w.Period.Posted != 100 || w.Period.Expected == nil || *w.Period.Expected != 950 || w.Period.Remaining != 850 || w.Period.OccurrenceI == nil {
		t.Errorf("set %+v", w)
	}
	if got := periodRows(t, path)["dining"]; !reflect.DeepEqual(got, w.Period) {
		t.Errorf("list after set %+v, want %+v", got, w.Period)
	}
	if w := periodWritten(t, path, "dining", "--posted", "100"); w.Changed {
		t.Errorf("same values: %+v", w)
	}
	// Only what is given changes.
	if w := periodWritten(t, path, "Dining out", "--status", "posted"); w.Period.Status != "posted" || w.Period.Posted != 100 || w.Period.Remaining != 0 {
		t.Errorf("status only %+v", w.Period)
	}
	id := periodRows(t, path)["dining"].OccurrenceI
	var dining struct {
		Revisions []struct{ Status string }
	}
	cliJSON(t, &dining, "--db", path, "budget", "period", "revisions", itoa64(*id))
	if len(dining.Revisions) != 2 {
		t.Errorf("dining revisions %+v", dining)
	}

	w = periodWritten(t, path, "dining", "--clear")
	if !w.Changed || w.Period.Status != "auto" || !w.Period.PostedAuto || w.Period.OccurrenceI != nil {
		t.Errorf("clear %+v", w)
	}
	if w := periodWritten(t, path, "dining", "--clear"); w.Changed {
		t.Errorf("clear again %+v", w)
	}

	// The previous period can be set by a date in it; two periods back cannot.
	if w := periodWritten(t, path, "dining", "--period-start", "2026-10-20", "--status", "pending"); !w.Changed || w.Period.End >= d.Start || w.Period.Start > "2026-10-20" || w.Period.End < "2026-10-20" || w.Period.Status != "pending" || w.Period.OccurrenceI == nil {
		t.Errorf("previous period %+v, the current one starts %s", w, d.Start)
	}
	refusedWrite(t, path, "", 1, "invalid", "budget", "period", "set", "dining", "--period-start", "2026-09-20", "--status", "pending")
	refusedWrite(t, path, "", 1, "invalid", "budget", "period", "set", "dining", "--posted", "-5")
	refusedWrite(t, path, "", 1, "not_found", "budget", "period", "set", "nothing", "--posted", "5")
	refusedWrite(t, path, "", 2, "usage", "budget", "period", "set", "dining")
	refusedWrite(t, path, "", 2, "usage", "budget", "period", "set", "dining", "--clear", "--posted", "5")
	refusedWrite(t, path, "", 2, "usage", "budget", "period", "set", "dining", "--status", "done")
}

func TestCLIBudgetTimingFlag(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "budget", "edit", "groceries", "--timing", "lump")
	if got := periodRows(t, path)["groceries"]; got.Timing != "lump" || got.Remaining != 0 {
		t.Errorf("groceries as a lump %+v", got)
	}
	var list []struct{ ID, Timing string }
	cliJSON(t, &list, "--db", path, "budget", "list")
	timings := map[string]string{}
	for _, it := range list {
		timings[it.ID] = it.Timing
	}
	if timings["groceries"] != "lump" || timings["dining"] != "spread" || timings["salary-a"] != "lump" {
		t.Errorf("budget list timings %v", timings)
	}
	write(t, path, "", "budget", "add", "--kind", "expense", "--name", "Fuel", "--amount", "120", "--every", "weekly", "--on", "2027-01-04", "--account", "checking", "--timing", "lump")
	write(t, path, "", "budget", "add", "--kind", "expense", "--name", "Snacks", "--amount", "20", "--every", "weekly", "--on", "2027-01-04", "--account", "checking")
	cliJSON(t, &list, "--db", path, "budget", "list")
	for _, it := range list {
		timings[it.ID] = it.Timing
	}
	if timings["fuel"] != "lump" || timings["snacks"] != "spread" {
		t.Errorf("added timings %v", timings)
	}
	refusedWrite(t, path, "", 2, "usage", "budget", "edit", "groceries", "--timing", "daily")
	refusedWrite(t, path, `{"name":"X","kind":"expense","amount":5,"account":"checking","timing":"daily","start":"2027-01-01","recurrence":{"kind":"monthly","day":1}}`, 1, "invalid", "budget", "add")
}
