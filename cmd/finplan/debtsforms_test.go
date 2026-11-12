// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestAccountChangeReadsMortgageTerms(t *testing.T) {
	for _, c := range []struct {
		field, text string
		ok          bool
	}{
		{"original", "300,000", false},
		{"original", "300000", true},
		{"original", "", true},
		{"term", "360", true},
		{"term", "30 years", false},
		{"firstpayment", "2027-02-01", true},
		{"firstpayment", "", false},
		{"paidfrom", "checking", true},
		{"paidfrom", "", true},
	} {
		ch, err := accountChange(c.field, c.text)
		if (err == nil) != c.ok {
			t.Errorf("%s %q: %v", c.field, c.text, err)
			continue
		}
		if err != nil {
			continue
		}
		switch c.field {
		case "original":
			if ch.OriginalAmount == nil || (c.text == "" && *ch.OriginalAmount != 0) || (c.text != "" && *ch.OriginalAmount != 300000) {
				t.Errorf("original %q read as %v", c.text, ch.OriginalAmount)
			}
		case "term":
			if ch.TermMonths == nil || *ch.TermMonths != 360 {
				t.Errorf("term read as %v", ch.TermMonths)
			}
		case "firstpayment":
			if ch.FirstPayment == nil || ch.FirstPayment.String() != "2027-02-01" {
				t.Errorf("first payment read as %v", ch.FirstPayment)
			}
		case "paidfrom":
			if ch.PaidFrom == nil || string(*ch.PaidFrom) != c.text {
				t.Errorf("paid from read as %v", ch.PaidFrom)
			}
		}
	}
}

func TestItemFormMatch(t *testing.T) {
	rec := demodata.Family()
	salary, _ := rec.Budget.Item("salary-a")
	f := formOf(salary, rec.Today)
	if f.matchPercent != "50" || f.matchUpTo != "6" || f.matchAccount != demodata.K401 {
		t.Fatalf("form %+v", f)
	}
	if got, err := f.item(rec.Today); err != nil || got.MatchPercent != 50 || got.MatchUpTo != 6 || got.MatchAccount != demodata.K401 {
		t.Errorf("item %+v: %v", got, err)
	}
	f.set("matchpercent", "75%")
	f.set("matchupto", "3")
	f.set("matchaccount", string(demodata.TSP))
	if got, err := f.item(rec.Today); err != nil || got.MatchPercent != 75 || got.MatchUpTo != 3 || got.MatchAccount != demodata.TSP || itemProblem(rec, f) != "" {
		t.Errorf("edited item %+v: %v, problem %q", got, err, itemProblem(rec, f))
	}
	f.set("matchpercent", "lots")
	if _, err := f.item(rec.Today); err == nil {
		t.Error("a match percent that is not a number was read")
	}
	f.set("matchpercent", "50")
	f.set("matchupto", "150")
	if itemProblem(rec, f) == "" {
		t.Error("a match above 100% of pay was accepted")
	}
	// An item that is not wages drops the match instead of being refused for it.
	f.set("matchupto", "6")
	f.set("income", string(model.IncomeOrdinary))
	if got, err := f.item(rec.Today); err != nil || got.MatchAccount != "" || got.MatchPercent != 0 || itemProblem(rec, f) != "" {
		t.Errorf("ordinary income item %+v: %v", got, err)
	}
	if f2 := newItemForm(); !f2.untouched() {
		t.Error("a new form is touched")
	} else if f2.set("matchpercent", "5"); f2.untouched() {
		t.Error("typing a match leaves the form untouched")
	}
}
