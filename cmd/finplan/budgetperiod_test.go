// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
)

// The view follows the Graph's cursor until a date is typed, and is loaded again when the date or the record changes.
func TestPeriodViewFollowsTheCursor(t *testing.T) {
	a := demoApp(t)
	s := &a.budget.period
	if d := s.shownDate(a); d != a.rec.Today {
		t.Fatalf("the cursor rests on %s, today is %s", d, a.rec.Today)
	}
	a.graph.cur.lock = pastCursorDate
	if d := s.shownDate(a); d != pastCursorDate {
		t.Fatalf("followed %s, want %s", d, pastCursorDate)
	}
	s.load(a.rec, pastCursorDate)
	got := map[string]engine.LineStatus{}
	for _, l := range s.standing {
		got[l.Item.ID] = l
	}
	if l := got["childcare"]; l.State != engine.StateMissing || standingText(l) != "missing" {
		t.Errorf("childcare on %s: %+v", pastCursorDate, l)
	}
	if l := got["groceries"]; l.State != engine.StateHot || !strings.HasPrefix(standingText(l), "running hot") {
		t.Errorf("groceries: %q", standingText(got["groceries"]))
	}
	if l := got["salary-a"]; !strings.HasPrefix(standingText(l), "matched ") {
		t.Errorf("salary-a: %q", standingText(l))
	}
	if len(s.rows) != len(s.standing) {
		t.Errorf("%d rows, %d standings", len(s.rows), len(s.standing))
	}
	typed := model.D(2026, time.September, 15)
	s.at = &typed
	a.graph.cur.lock = a.rec.Today
	if d := s.shownDate(a); d != typed {
		t.Errorf("a typed date shows %s", d)
	}
}

// The Unaccounted row of an account for the month is what budget unaccounted gives for that month.
func TestPeriodUnaccountedIsTheCLIsForTheMonth(t *testing.T) {
	a := demoApp(t)
	s := &a.budget.period
	s.load(a.rec, pastCursorDate)
	path := seededFile(t)
	rows := unaccountedOf(t, path, "--from", "2026-10", "--to", "2026-10")
	if len(rows) == 0 || len(rows) != len(s.unaccounted) {
		t.Fatalf("CLI rows %+v, view rows %+v", rows, s.unaccounted)
	}
	for i, u := range s.unaccounted {
		r := rows[i]
		if string(u.Account) != r.Account || math.Abs(u.Amount-r.Amount) > 0.005 || len(u.Transactions) != len(r.Transactions) {
			t.Errorf("%s: view %.2f over %d transactions, CLI %.2f over %d", u.Account, u.Amount, len(u.Transactions), r.Amount, len(r.Transactions))
		}
	}
}

func TestParseDrift(t *testing.T) {
	for text, want := range map[string]int{"off": 0, "OFF": 0, "6": 6, "12 months": 12} {
		if got, err := parseDrift(text); err != nil || *got != want {
			t.Errorf("%q: %v, %v", text, got, err)
		}
	}
	if got, err := parseDrift(""); err != nil || got != nil {
		t.Errorf("empty inherits: %v, %v", got, err)
	}
	for _, text := range []string{"0", "-3", "many", "2.5"} {
		if got, err := parseDrift(text); err == nil {
			t.Errorf("%q gives %d", text, *got)
		}
	}
}
