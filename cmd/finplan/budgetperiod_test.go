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

// The table has no Over or under column, and a wages row shows the deposit expected with its gross in the tooltip.
func TestPeriodTableColumnsAndWagesText(t *testing.T) {
	for _, c := range periodColumns {
		if c.header == "Over or under" {
			t.Error("the table still has an Over or under column")
		}
	}
	a := demoApp(t)
	s := &a.budget.period
	s.load(a.rec, a.rec.Today)
	for _, r := range s.rows {
		v := r.Current()
		text, tip := budgetedText(v), budgetedTip(v)
		if text != formatDollars(v.Budgeted) {
			t.Errorf("%s: budgeted text %q", r.Item.ID, text)
		}
		if wages := r.Item.IsWages(); wages != strings.HasPrefix(tip, "Gross ") || wages != strings.Contains(tip, " · deposit "+formatDollars(v.Budgeted)) {
			t.Errorf("%s: budgeted tip %q", r.Item.ID, tip)
		}
	}
}

func TestFullTextTipShowsTheWholeTextOnlyWhenClipped(t *testing.T) {
	if got := fullTextTip("matched $5,000, $12 under", false, "hint"); got != "hint" {
		t.Errorf("unclipped: %q", got)
	}
	if got := fullTextTip("matched $5,000, $12 under", true, "hint"); got != "matched $5,000, $12 under\n\nhint" {
		t.Errorf("clipped: %q", got)
	}
}

// A payroll deduction stands as its paycheck does: the demo's deductions are matched, at their amounts, beside their matched paychecks.
func TestPeriodDeductionFollowsItsPaycheck(t *testing.T) {
	a := demoApp(t)
	s := &a.budget.period
	s.load(a.rec, pastCursorDate)
	checked := 0
	for _, l := range s.standing {
		if l.Item.Paycheck == "" {
			continue
		}
		pay, ok := s.standing[l.Item.Paycheck]
		if !ok || pay.State != engine.StateMatched {
			continue
		}
		checked++
		if l.State != engine.StateMatched || l.Amount != l.View.Budgeted || l.Variance != 0 {
			t.Errorf("%s: %+v beside its matched paycheck", l.Item.ID, l)
		}
	}
	if checked == 0 {
		t.Fatal("no deduction beside a matched paycheck")
	}
}
