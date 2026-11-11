// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestRestoreDocumentsRefusesLeavingAGrantWithoutItsCalendar(t *testing.T) {
	st, rec := seed(t)
	cal := model.VestCalendar{Name: "Acme 2", Rules: []model.VestRule{{Month: time.May, Day: 1}}}
	res, err := NewCalendar(st, rec, cal, Options{})
	if err != nil {
		t.Fatal(err)
	}
	withCal := res.Record
	g, err := AddGrant(st, withCal, GrantInput{Account: demodata.Equity, Symbol: "ACME", Date: model.D(2027, time.February, 1), Shares: 100, Price: 1,
		Calendar: "Acme 2", FirstVest: model.D(2027, time.May, 1), YearPercents: []float64{50, 50}, Brokerage: demodata.Brokerage}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	back, err := RestoreDocuments(st, g.Record, rec, Options{})
	if !errors.Is(err, ErrInUse) || back != nil || !strings.Contains(err.Error(), "Acme 2") {
		t.Fatalf("undo of the calendar under a grant: %v", err)
	}
	if reload(t, st).Calendar("Acme 2") == nil {
		t.Error("the calendar was removed")
	}
}

func TestRestoreDocumentsRefusesBreakingAGrantsPlan(t *testing.T) {
	st, rec := seed(t)
	cal := model.VestCalendar{Name: "Acme 2", Rules: []model.VestRule{{Month: time.May, Day: 1}}}
	res, err := NewCalendar(st, rec, cal, Options{})
	if err != nil {
		t.Fatal(err)
	}
	before := res.Record
	edited, err := EditCalendar(st, before, "Acme 2", []model.VestRule{{Month: time.May, Day: 1}, {Month: time.November, Day: 1}}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := AddGrant(st, edited.Record, GrantInput{Account: demodata.Equity, Symbol: "ACME", Date: model.D(2027, time.February, 1), Shares: 100, Price: 1,
		Calendar: "Acme 2", FirstVest: model.D(2027, time.November, 1), YearPercents: []float64{50, 50}, Brokerage: demodata.Brokerage}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	back, err := RestoreDocuments(st, g.Record, before, Options{})
	if !errors.Is(err, ErrInUse) || back != nil || !strings.Contains(err.Error(), "grant") {
		t.Fatalf("undo of the rule edit under a grant: %v", err)
	}
	if n := len(reload(t, st).Calendar("Acme 2").Rules); n != 2 {
		t.Errorf("the rules were restored: %d", n)
	}
}

func TestRestoreDocumentsRefusesLeavingAGrantOnANonEquityAccount(t *testing.T) {
	st, rec := seed(t)
	src := rec.Clone()
	src.Account(demodata.Equity).Type = model.TypeBrokerage
	back, err := RestoreDocuments(st, rec, src, Options{})
	if !errors.Is(err, ErrInUse) || back != nil || !strings.Contains(err.Error(), "grant") {
		t.Fatalf("restore of a state where the grant's account is not equity: %v", err)
	}
	if reload(t, st).Account(demodata.Equity).Type != model.TypeEquity {
		t.Error("the account type was restored")
	}
}

func TestRestoreDocumentsRefusesLeavingAGrantWithoutItsBrokerage(t *testing.T) {
	st, rec := seed(t)
	src := rec.Clone()
	src.Account(demodata.Brokerage).Type = model.TypeEquity
	back, err := RestoreDocuments(st, rec, src, Options{})
	if !errors.Is(err, ErrInUse) || back != nil || !strings.Contains(err.Error(), "grant") {
		t.Fatalf("restore of a state where the grant's brokerage is not a brokerage: %v", err)
	}
	if reload(t, st).Account(demodata.Brokerage).Type != model.TypeBrokerage {
		t.Error("the account type was restored")
	}
}
