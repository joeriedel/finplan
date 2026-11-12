// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

type tTerms struct {
	ID             string
	OriginalAmount *float64 `json:"original_amount"`
	TermMonths     *int     `json:"term_months"`
	FirstPayment   *string  `json:"first_payment"`
	PaidFrom       *string  `json:"paid_from"`
	MonthlyPayment *float64 `json:"monthly_payment"`
}

type tMatchItem struct {
	ID    string
	Match *struct {
		Percent float64
		UpTo    float64 `json:"up_to"`
		Account string
	}
}

// The mortgage terms of the demo and their editing through account edit.
func TestCLIMortgageTerms(t *testing.T) {
	path := seededFile(t)
	var accts []tTerms
	cliJSON(t, &accts, "--db", path, "account", "list")
	byID := map[string]tTerms{}
	for _, a := range accts {
		byID[a.ID] = a
	}
	m := byID["mortgage"]
	if m.OriginalAmount == nil || *m.OriginalAmount != 433600 || *m.TermMonths != 360 || m.FirstPayment == nil || *m.FirstPayment != "2021-02-01" || m.PaidFrom == nil || *m.PaidFrom != "checking" || m.MonthlyPayment == nil || !closeTo(*m.MonthlyPayment, 2599.65) {
		t.Errorf("demo mortgage %+v", m)
	}
	if c := byID["checking"]; c.OriginalAmount != nil || c.TermMonths != nil || c.PaidFrom != nil || c.MonthlyPayment != nil {
		t.Errorf("checking has terms: %+v", c)
	}

	var w struct{ Account tTerms }
	cliJSON(t, &w, "--db", path, "account", "edit", "mortgage", "--original", "300000", "--term-months", "180", "--first-payment", "2027-01-15", "--paid-from", "Savings")
	if a := w.Account; *a.OriginalAmount != 300000 || *a.TermMonths != 180 || *a.FirstPayment != "2027-01-15" || *a.PaidFrom != "savings" || a.MonthlyPayment == nil {
		t.Errorf("edited terms %+v", a)
	}
	cliJSON(t, &w, "--db", path, "account", "edit", "mortgage", "--clear-terms")
	if a := w.Account; *a.OriginalAmount != 0 || *a.TermMonths != 0 || a.FirstPayment != nil || a.PaidFrom != nil || a.MonthlyPayment != nil {
		t.Errorf("cleared terms %+v", a)
	}
	refusedWrite(t, path, "", exitFailure, "invalid", "account", "edit", "checking", "--term-months", "12")
	refusedWrite(t, path, "", exitFailure, "invalid", "account", "edit", "mortgage", "--term-months", "5000")
	refusedWrite(t, path, "", exitFailure, "invalid", "account", "edit", "mortgage", "--paid-from", "mortgage")
	refusedWrite(t, path, "", exitFailure, "not_found", "account", "edit", "mortgage", "--paid-from", "Nobody")
	refusedWrite(t, path, "", exitUsage, "usage", "account", "edit", "mortgage", "--first-payment", "soon")
}

// The employer match of a wages item through budget add and edit.
func TestCLIEmployerMatch(t *testing.T) {
	path := seededFile(t)
	var list []tMatchItem
	cliJSON(t, &list, "--db", path, "budget", "list")
	for _, it := range list {
		switch it.ID {
		case "salary-a":
			if it.Match == nil || it.Match.Percent != 50 || it.Match.UpTo != 6 || it.Match.Account != "401k" {
				t.Errorf("salary-a match %+v", it.Match)
			}
		case "groceries":
			if it.Match != nil {
				t.Errorf("groceries has a match %+v", it.Match)
			}
		}
	}
	var w struct{ Item tMatchItem }
	cliJSON(t, &w, "--db", path, "budget", "edit", "salary-b", "--match-percent", "50", "--match-up-to", "6", "--match-account", "401k")
	if m := w.Item.Match; m == nil || m.Percent != 50 || m.UpTo != 6 || m.Account != "401k" {
		t.Errorf("edited match %+v", m)
	}
	cliJSON(t, &w, "--db", path, "budget", "edit", "salary-b", "--amount", "3800")
	if w.Item.Match == nil {
		t.Error("an edit of the amount lost the match")
	}
	cliJSON(t, &w, "--db", path, "budget", "edit", "salary-b", "--match-percent", "0", "--match-up-to", "0", "--match-account", "none")
	if w.Item.Match != nil {
		t.Errorf("cleared match %+v", w.Item.Match)
	}
	refusedWrite(t, path, "", exitFailure, "invalid", "budget", "edit", "salary-b", "--match-percent", "100", "--match-up-to", "101", "--match-account", "tsp")
	refusedWrite(t, path, "", exitFailure, "invalid", "budget", "edit", "salary-b", "--match-percent", "100", "--match-up-to", "4", "--match-account", "checking")
	refusedWrite(t, path, "", exitFailure, "invalid", "budget", "edit", "groceries", "--match-percent", "100", "--match-up-to", "4", "--match-account", "tsp")
	code, out := wcli(t, `{"name": "Bonus pay", "kind": "income", "amount": 1000, "recurrence": {"kind": "monthly", "day": 2}, "start": "2027-01-01", "account": "checking", "match": {"percent": 100, "up_to": 3, "account": "tsp"}}`, "--db", path, "budget", "add")
	if code != 0 || !strings.Contains(string(out), `"up_to"`) {
		t.Errorf("JSON add with a match: %d %s", code, out)
	}
}

// Scenarios of the debts through the command line: the mortgage's payments split into interest and principal, and the 401k limit shows as a note.
func TestCLIDebtAndLimitScenarios(t *testing.T) {
	path := seededFile(t)
	if p := breakdownLine(t, path, "mortgage", "", "2027-01-01", "2027-12-31", "principal"); p <= 0 {
		t.Errorf("principal on the mortgage %v", p)
	}
	if breakdownLine(t, path, "mortgage", "", "2027-01-01", "2027-12-31", "interest") != 0 {
		t.Error("the mortgage shows interest")
	}
	// A breakdown window excludes its from date, so from 2026-12-31 holds the twelve payments of 2027.
	interest, principal := breakdownLine(t, path, "checking", "", "2026-12-31", "2027-12-31", "interest"), breakdownLine(t, path, "checking", "", "2026-12-31", "2027-12-31", "principal")
	if interest >= 0 || principal >= 0 || !closeTo(interest+principal, -12*2599.65) {
		t.Errorf("checking paid %v interest and %v principal", interest, principal)
	}
	if c := breakdownLine(t, path, "card", "", "2027-01-01", "2027-12-31", "interest"); c >= 0 {
		t.Errorf("the card compounds %v", c)
	}
	if breakdownLine(t, path, "401k", "", "2027-01-01", "2027-12-31", "match") <= 0 {
		t.Error("no match deposited into the 401k")
	}

	write(t, path, "", "budget", "add", "--kind", "transfer", "--name", "Big 401k", "--amount", "5000", "--every", "monthly", "--start", "2027-01-01", "--from", "checking", "--to", "401k")
	var bd struct {
		Lines []struct {
			Cause     string
			Projected float64
		}
		Diagnostics struct{ Notes []string }
	}
	cliJSON(t, &bd, "--db", path, "breakdown", "401k", "--from", "2026-12-31", "--to", "2027-12-31")
	// The 2027 limit is the shipped table indexed by the demo's inflation.
	rec := demodata.Family()
	inflation, _ := rec.Globals[model.PropInflation].At(model.D(2027, time.January, 1))
	limit, err := rec.Tax.Limit401k(2027, inflation)
	if err != nil || limit <= 0 {
		t.Fatalf("limit %v, %v", limit, err)
	}
	capped := false
	for _, l := range bd.Lines {
		if l.Cause == "contribution" {
			capped = closeTo(l.Projected, limit)
		}
	}
	noted := false
	for _, n := range bd.Diagnostics.Notes {
		noted = noted || strings.Contains(n, fmt.Sprintf("2027 401(k) limit of %.2f refuses", limit))
	}
	if !capped || !noted {
		t.Errorf("contributions capped %v, noted %v: %+v", capped, noted, bd)
	}
}
