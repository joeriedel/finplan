// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

func TestIntervalUnsetFollowsCursor(t *testing.T) {
	var iv bkInterval
	from, to := iv.window(model.D(2026, time.March, 15))
	if from != model.D(2025, time.March, 15) || to != model.D(2026, time.March, 15) {
		t.Errorf("window = %v..%v, want 2025-03-15..2026-03-15", from, to)
	}
	from, to = iv.window(model.D(2026, time.July, 1))
	if from != model.D(2025, time.July, 1) || to != model.D(2026, time.July, 1) {
		t.Errorf("window after the cursor moved = %v..%v", from, to)
	}
}

func TestIntervalPaintAndEdges(t *testing.T) {
	a, b := model.D(2025, time.May, 1), model.D(2025, time.August, 1)
	var iv bkInterval
	iv.paint(a, b)
	if !iv.set || iv.from != a || iv.to != b {
		t.Fatalf("forward drag = %+v", iv)
	}
	iv.clear()
	iv.paint(b, a)
	if !iv.set || iv.from != a || iv.to != b {
		t.Fatalf("backward drag = %+v", iv)
	}
	iv.paint(a, a)
	if iv.from != a || iv.to != b {
		t.Errorf("a drag that did not move changed the interval: %+v", iv)
	}

	cursor := model.D(2030, time.January, 1)
	iv.setEdge(cursor, false, a.Add(10))
	if iv.from != a.Add(10) || iv.to != b {
		t.Errorf("moving the start: %+v", iv)
	}
	iv.setEdge(cursor, true, b.Add(10))
	if iv.from != a.Add(10) || iv.to != b.Add(10) {
		t.Errorf("moving the end: %+v", iv)
	}
	iv.setEdge(cursor, false, b.Add(100))
	if iv.from != iv.to-1 {
		t.Errorf("a start past the end is kept a day before it: %+v", iv)
	}
	iv.setEdge(cursor, true, iv.from-50)
	if iv.to != iv.from+1 {
		t.Errorf("an end before the start is kept a day after it: %+v", iv)
	}

	iv.clear()
	if iv.set {
		t.Error("clear left the interval set")
	}
	iv.setEdge(cursor, true, cursor.Add(-30))
	if from, to := iv.window(cursor.Add(500)); !iv.set || to != cursor.Add(-30) || from != model.D(2029, time.January, 1) {
		t.Errorf("editing the end of an unset interval = %v..%v", from, to)
	}
}

func TestIntervalSurvivesLineChange(t *testing.T) {
	rec := demodata.Family()
	lines := graph.NewLines("")
	a := lines.Add(demodata.NetWorth, false, "", graph.AssetColor(rec, demodata.NetWorth))
	b := lines.Add(demodata.AllCash, false, "", graph.AssetColor(rec, demodata.AllCash))
	var iv bkInterval
	iv.setWindow(yearAt(model.D(2025, time.June, 1)))
	lines.Select(a)
	lines.Select(b)
	if from, to := iv.window(rec.Today); from != model.D(2024, time.December, 31) || to != model.D(2025, time.December, 31) {
		t.Errorf("interval after changing line = %v..%v", from, to)
	}
}

func TestQuickChoices(t *testing.T) {
	cursor, today := model.D(2024, time.May, 20), model.D(2026, time.November, 8)
	if from, to := yearAt(cursor); from != model.D(2023, time.December, 31) || to != model.D(2024, time.December, 31) {
		t.Errorf("year at cursor = %v..%v", from, to)
	}
	if from, to := yearAt(today); from != model.D(2025, time.December, 31) || to != model.D(2026, time.December, 31) {
		t.Errorf("this year = %v..%v", from, to)
	}
	start := model.D(2022, time.February, 3)
	if from, to := sinceStart(start, today); firstDay(from) != start || to != today {
		t.Errorf("since start = %v..%v", from, to)
	}
	if from, to := nextTenYears(today); from != today || to != model.D(2036, time.November, 8) {
		t.Errorf("next 10 years = %v..%v", from, to)
	}
	if firstDay(model.D(2024, time.December, 31)) != model.D(2025, time.January, 1) {
		t.Error("the first day of a window is the day after from")
	}
}

func TestCauseLabels(t *testing.T) {
	for c, want := range map[engine.Cause]string{
		engine.CauseIncome:         "Income",
		engine.CauseSocialSecurity: "Social Security",
		engine.CauseFERS:           "FERS",
		engine.CauseGainsTax:       "Gains tax",
		engine.CauseUnexplained:    "Unexplained",
		"brand new cause":          "Brand new cause",
		"":                         "",
	} {
		if got := causeLabel(c); got != want {
			t.Errorf("causeLabel(%q) = %q, want %q", c, got, want)
		}
	}
	rec := demodata.Family()
	res := &engine.BreakdownResult{Lines: []engine.Line{
		{Cause: engine.CauseIncome, Actual: 1},
		{Cause: engine.CauseGrowth, Class: "no-such-class", Projected: 2},
		{Cause: "mystery", Actual: 3},
	}}
	rows := breakdownRows(rec, res)
	if len(rows) != 3 || rows[0].label != "Income" || rows[1].label != "Growth: no-such-class" || rows[2].label != "Mystery" {
		t.Errorf("rows = %+v", rows)
	}
	if rows[1].key != (rowKey{engine.CauseGrowth, "no-such-class"}) || rows[1].total() != 2 {
		t.Errorf("class row = %+v", rows[1])
	}
}

func TestBreakdownLineFollowsSelection(t *testing.T) {
	rec := demodata.Family()
	lines := graph.NewLines("")
	cash := lines.Add(demodata.AllCash, false, "", graph.AssetColor(rec, demodata.AllCash))
	worth := lines.Add(demodata.NetWorth, false, "", graph.AssetColor(rec, demodata.NetWorth))
	if ln, ok := followedLine(lines); !ok || ln.ID != cash {
		t.Errorf("with nothing selected: %+v, %v; want the first line", ln, ok)
	}
	lines.SetVisible(cash, false)
	if ln, ok := followedLine(lines); !ok || ln.ID != worth {
		t.Errorf("with the first line hidden: %+v, %v; want the first drawn line", ln, ok)
	}
	lines.Select(cash)
	if ln, ok := followedLine(lines); !ok || ln.ID != cash {
		t.Errorf("with a line selected: %+v, %v; want the selected line", ln, ok)
	}
	lines.Select(worth)
	if ln, ok := followedLine(lines); !ok || ln.ID != worth {
		t.Errorf("after selecting another: %+v, %v", ln, ok)
	}
	lines.Remove(worth)
	if ln, ok := followedLine(lines); ok && ln.ID == worth {
		t.Error("a removed line is still followed")
	}
	if _, ok := followedLine(graph.NewLines("")); ok {
		t.Error("no lines have a breakdown")
	}
}

func TestNetWorthBreakdownRowsAndMembers(t *testing.T) {
	rec := demodata.Family()
	c := newBreakdownCache(rec)
	k := bkKey{"", demodata.NetWorth, model.D(2025, time.June, 30), model.D(2025, time.December, 31)}
	e := c.get(k)
	if e.err != nil {
		t.Fatal(e.err)
	}
	if c.get(k) != e {
		t.Error("the cache does not return the same entry for the same key")
	}
	change := e.res.End - e.res.Start
	if change == 0 {
		t.Fatal("net worth did not change over the interval")
	}
	rows := breakdownRows(rec, e.res)
	sum := 0.0
	for _, r := range rows {
		sum += r.actual + r.projected
	}
	if math.Abs(sum-change) > 0.01 {
		t.Errorf("rows sum to %v, the change is %v", sum, change)
	}

	accts := c.memberAccounts("", demodata.NetWorth)
	if len(accts) != 14 {
		t.Fatalf("net worth has %d member accounts, want 14", len(accts))
	}
	members := c.members(k, e)
	if len(members) != len(accts) {
		t.Fatalf("%d member breakdowns for %d accounts", len(members), len(accts))
	}
	memberChange := 0.0
	for _, m := range members {
		memberChange += m.res.End - m.res.Start
	}
	if math.Abs(memberChange-change) > 0.01 {
		t.Errorf("members change by %v, the asset by %v", memberChange, change)
	}

	several := false
	for _, r := range rows {
		shares := memberShares(members, r.key)
		for _, s := range shares {
			if s.actual == 0 && s.projected == 0 {
				t.Errorf("%s lists %s with no share", r.label, s.name)
			}
		}
		several = several || len(shares) >= 2
	}
	if !several {
		t.Error("no cause has shares from several member accounts")
	}
}

func TestSingleAccountLineHasNoExpansion(t *testing.T) {
	c := newBreakdownCache(demodata.Family())
	if m := c.memberAccounts("", model.DefaultAssetID(demodata.Brokerage)); len(m) != 0 {
		t.Errorf("Brokerage has members %v", m)
	}
}

func TestMemberSharesGroupByCauseAndClass(t *testing.T) {
	res := func(ls ...engine.Line) *engine.BreakdownResult { return &engine.BreakdownResult{Lines: ls} }
	members := []bkMember{
		{"A", res(engine.Line{Cause: engine.CauseGrowth, Class: "stocks", Actual: 5}, engine.Line{Cause: engine.CauseIncome, Actual: 1})},
		{"B", res(engine.Line{Cause: engine.CauseGrowth, Class: "bonds", Projected: 7})},
		{"C", res(engine.Line{Cause: engine.CauseGrowth, Class: "stocks"})},
	}
	got := memberShares(members, rowKey{engine.CauseGrowth, "stocks"})
	if len(got) != 1 || got[0].name != "A" || got[0].actual != 5 {
		t.Errorf("stocks growth shares = %+v", got)
	}
	if got := memberShares(members, rowKey{engine.CauseGrowth, "bonds"}); len(got) != 1 || got[0].projected != 7 {
		t.Errorf("bonds growth shares = %+v", got)
	}
	if got := memberShares(members, rowKey{engine.CauseTax, ""}); len(got) != 0 {
		t.Errorf("tax shares = %+v", got)
	}
}
