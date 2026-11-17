// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package demodata

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
)

func statusAt(t *testing.T, rec *model.Record, d model.Date) map[string]engine.LineStatus {
	t.Helper()
	v, err := engine.StatusAsOf(rec, d)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]engine.LineStatus{}
	for _, l := range v.Lines {
		out[l.Item.ID] = l
	}
	return out
}

// On 3 October 2026 the childcare bill, due that day, has not been paid yet (it is paid a few days from its date), the paychecks have matched, and groceries, whose cycle began the same day, have a large first purchase already. Nine days later the childcare payment counts.
func TestStatusInAPastMonth(t *testing.T) {
	rec := Family()
	d := model.D(2026, time.October, 3)
	got := statusAt(t, rec, d)
	if l := got["childcare"]; l.State != engine.StateMissing || len(l.Matches) != 0 {
		t.Errorf("childcare on %s: %+v", d, l)
	}
	if l := got["salary-a"]; l.State != engine.StateMatched || len(l.Matches) != 1 || l.Amount <= 0 {
		t.Errorf("salary-a: %+v", l)
	}
	if l := got["groceries"]; l.State != engine.StateHot || l.Variance <= 0 || len(l.Matches) == 0 {
		t.Errorf("groceries: %+v", l)
	}
	for id, l := range got {
		// A payroll deduction touches only the TSP account, whose records end earlier.
		if id == "save-tsp" {
			continue
		}
		if l.Cutoff != d {
			t.Errorf("%s is evaluated as of %s, not %s", id, l.Cutoff, d)
		}
		for _, m := range l.Matches {
			if m.Transaction.Date > d {
				t.Errorf("%s counts the transaction of %s, after %s", id, m.Transaction.Date, d)
			}
		}
	}
	later := statusAt(t, rec, d.Add(9))
	if l := later["childcare"]; l.State != engine.StateMatched || len(l.Matches) != 1 {
		t.Errorf("childcare nine days later: %+v", l)
	}
}

// Today the childcare bill of 3 November is missing and a spread line that nothing was said about is judged on pace; groceries carry a posted amount over their budget. After today nothing is recorded, so a later date is judged on what is recorded up to today and its paychecks are not yet due.
func TestStatusToday(t *testing.T) {
	rec := Family()
	got := statusAt(t, rec, rec.Today)
	if l := got["childcare"]; l.State != engine.StateMissing {
		t.Errorf("childcare: %+v", l)
	}
	if l := got["utilities"]; l.State != engine.StateOnPace || l.Amount <= 0 {
		t.Errorf("utilities: %+v", l)
	}
	if l := got["groceries"]; l.State != engine.StateMatched || l.Variance != 250 {
		t.Errorf("groceries are 250 over: %+v", l)
	}
	future := statusAt(t, rec, rec.Today.Add(12))
	if l := future["salary-a"]; l.State != engine.StateNotYetDue || l.Cutoff != rec.Today {
		t.Errorf("salary-a after today: %+v", l)
	}
	if l := future["childcare"]; l.State != engine.StateMissing {
		t.Errorf("childcare after today: %+v", l)
	}
}

// Base with drift carries the average monthly unaccounted amount of the six months before the projection starts forward on the checking account and the card, a month at a time from December, so after a year it is lower than Base by twelve times what the two average. The balances grow a little on the way, so the match is to a few percent.
func TestDriftLowersTheProjection(t *testing.T) {
	rec := Family()
	end := model.D(2027, time.November, 8)
	at := func(proj string) float64 {
		t.Helper()
		res, err := engine.Balances(rec, proj, NetWorth, []model.Date{end})
		if err != nil || len(res.Skipped) != 0 {
			t.Fatalf("%s: %v %+v", proj, err, res.Skipped)
		}
		return res.Samples[0].Amount
	}
	monthly := 0.0
	for _, id := range []model.AccountID{Checking, Card} {
		monthly += engine.MonthlyUnaccounted(rec, id, rec.Today.Add(1), 6)
	}
	if monthly > -300 {
		t.Fatalf("the demo's checking and card drift by %.2f a month", monthly)
	}
	got, want := at(ProjDrift)-at(ProjBase), 12*monthly
	if math.Abs(got-want) > 0.03*math.Abs(want) {
		t.Errorf("drift lowers net worth by %.2f, want about %.2f", got, want)
	}
}

// A projection built on Base with drift inherits the setting, and an explicit zero turns it off again.
func TestDriftInherits(t *testing.T) {
	rec := Family()
	if err := rec.AddProjection(&model.Projection{Name: "Child", Parent: ProjDrift}); err != nil {
		t.Fatal(err)
	}
	if err := rec.AddProjection(&model.Projection{Name: "Child off", Parent: ProjDrift, DriftMonths: model.Ptr(0)}); err != nil {
		t.Fatal(err)
	}
	end := []model.Date{model.D(2027, time.November, 8)}
	at := func(proj string) float64 {
		t.Helper()
		res, err := engine.Balances(rec, proj, NetWorth, end)
		if err != nil {
			t.Fatal(err)
		}
		return res.Samples[0].Amount
	}
	if at("Child") != at(ProjDrift) || at("Child off") != at(ProjBase) || at(ProjDrift) == at(ProjBase) {
		t.Errorf("inherited %v, off %v, drift %v, base %v", at("Child"), at("Child off"), at(ProjDrift), at(ProjBase))
	}
}
