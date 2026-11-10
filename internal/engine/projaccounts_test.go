// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// villaRecord is the fixture plus an aggregate of every account by rule, and projections "villa" (child "child") that define a 1000 account with 12% growth opening on 2026-03-01, funded by 100 on the 1st of each month from chk.
func villaRecord(t *testing.T) *model.Record {
	t.Helper()
	rec := fixture(t)
	must(t, rec.AddAsset(&model.Asset{ID: "everything", Members: []model.Member{model.RuleMember(model.Rule{})}}))
	fund := model.BudgetItem{ID: "fund", Kind: model.ItemTransfer, Amount: 100, Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, From: "chk", To: "villa"}
	must(t, rec.AddProjection(&model.Projection{
		Name:     "villa",
		Accounts: []model.ProjAccount{{ID: "villa", Name: "Villa", Type: model.TypeProperty, Tax: model.Taxable, StatedRate: 0.12, Opening: 1000, From: d(2026, time.March, 1)}},
		Mods:     []model.Modification{{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "fund"}, Property: model.PropAddItem, Item: &fund}},
	}))
	must(t, rec.AddProjection(&model.Projection{Name: "child", Parent: "villa"}))
	return rec
}

func TestProjectionAccountValue(t *testing.T) {
	rec := villaRecord(t)
	villa := model.DefaultAssetID("villa")
	for _, tc := range []struct {
		date model.Date
		want float64
	}{
		{d(2026, time.February, 28), 0},
		{d(2026, time.March, 1), 1100},
	} {
		if got := sample(t, rec, "villa", villa, tc.date).Amount; !near(got, tc.want) {
			t.Errorf("villa on %v: got %v, want %v", tc.date, got, tc.want)
		}
	}
	// One month of growth at 12%, with the 100 from chk on the opening day and on the 1st.
	apr := d(2026, time.April, 1)
	if got, want := sample(t, rec, "villa", villa, apr).Amount, 1100*math.Pow(1.12, float64(apr-d(2026, time.March, 1))/model.DaysPerYear)+100; !near(got, want) {
		t.Errorf("villa on %v: got %v, want %v", apr, got, want)
	}
	if got, want := sample(t, rec, "child", villa, apr).Amount, sample(t, rec, "villa", villa, apr).Amount; !near(got, want) {
		t.Errorf("child: got %v, want %v", got, want)
	}
}

func TestProjectionAccountOutsideItsProjection(t *testing.T) {
	rec := villaRecord(t)
	villa := model.DefaultAssetID("villa")
	for _, proj := range []string{"", "other"} {
		if proj != "" {
			must(t, rec.AddProjection(&model.Projection{Name: proj}))
		}
		if _, err := Balances(rec, proj, villa, []model.Date{d(2026, time.April, 1)}); err == nil {
			t.Errorf("projection %q knows the account", proj)
		}
	}
	if rec.Account("villa") != nil || rec.Asset(villa) != nil {
		t.Error("the record gained the account")
	}
}

func TestRuleAggregateIncludesProjectionAccounts(t *testing.T) {
	rec := villaRecord(t)
	day := d(2026, time.March, 1)
	base := sample(t, rec, "", "everything", day).Amount
	with := sample(t, rec, "villa", "everything", day).Amount
	// The aggregate gains the 1000 opening; the 100 from chk on the opening day arrives in villa and leaves chk, and the February one is not modelled.
	if !near(with-base, 1000) {
		t.Errorf("aggregate gains %v, want 1000", with-base)
	}
	must(t, rec.AddProjection(&model.Projection{Name: "other"}))
	if got := sample(t, rec, "other", "everything", day).Amount; !near(got, base) {
		t.Errorf("a projection without the account: got %v, want %v", got, base)
	}
}

func TestProjectionAccountFlowsBeforeOpening(t *testing.T) {
	rec := villaRecord(t)
	chk := model.DefaultAssetID("chk")
	// chk is not debited for the transfer dated before villa opens, and is debited on the opening day.
	for _, day := range []model.Date{d(2026, time.February, 1), d(2026, time.February, 28)} {
		if a, b := sample(t, rec, "villa", chk, day).Amount, sample(t, rec, "", chk, day).Amount; !near(a, b) {
			t.Errorf("chk on %v: %v with villa, %v without", day, a, b)
		}
	}
	mar := d(2026, time.March, 1)
	if a, b := sample(t, rec, "villa", chk, mar).Amount, sample(t, rec, "", chk, mar).Amount; !near(a, b-100) {
		t.Errorf("chk on %v: %v with villa, want %v", mar, a, b-100)
	}
	for _, target := range []model.AssetID{model.DefaultAssetID("villa"), "everything"} {
		bd, err := Breakdown(rec, "villa", target, d(2026, time.January, 15), d(2026, time.April, 1))
		must(t, err)
		for _, l := range bd.Lines {
			if l.Cause == CauseUnexplained {
				t.Errorf("%v: unexplained line %+v", target, l)
			}
		}
		if !near(bd.Total(), bd.End-bd.Start) {
			t.Errorf("%v: breakdown total %v, change %v", target, bd.Total(), bd.End-bd.Start)
		}
	}
}

func TestProjectionAccountStartsAndBreakdown(t *testing.T) {
	rec := villaRecord(t)
	starts, err := Starts(rec, "villa", model.DefaultAssetID("villa"))
	must(t, err)
	if len(starts) != 1 || starts[0].Start != d(2026, time.March, 1) || starts[0].HasActual {
		t.Errorf("starts %+v", starts)
	}
	bd, err := Breakdown(rec, "villa", model.DefaultAssetID("villa"), d(2026, time.March, 1), d(2026, time.April, 1))
	must(t, err)
	if bd.Start != 1100 {
		t.Errorf("breakdown start %v", bd.Start)
	}
	if !near(bd.Total(), bd.End-bd.Start) {
		t.Errorf("breakdown total %v, change %v", bd.Total(), bd.End-bd.Start)
	}
}

func TestProjectionAccountCollision(t *testing.T) {
	rec := villaRecord(t)
	rec.Projection("child").Accounts = []model.ProjAccount{{ID: "villa", Name: "Again", Type: model.TypeProperty, Tax: model.Taxable, From: d(2026, time.March, 1)}}
	if _, err := Balances(rec, "child", "everything", []model.Date{d(2026, time.April, 1)}); err == nil {
		t.Error("two accounts with one id did not fail")
	}
}

func TestProjectionAccountOpeningBeforeProjectionStart(t *testing.T) {
	rec := fixture(t)
	opens := d(2025, time.July, 1)
	must(t, rec.AddProjection(&model.Projection{
		Name:     "early",
		Accounts: []model.ProjAccount{{ID: "early", Name: "Early", Type: model.TypeProperty, Tax: model.Taxable, StatedRate: 0.12, Opening: 1000, From: opens}},
	}))
	early := model.DefaultAssetID("early")
	for _, date := range []model.Date{opens, d(2026, time.January, 1), d(2026, time.July, 1)} {
		want := 1000 * math.Pow(1.12, float64(date-opens)/model.DaysPerYear)
		if got := sample(t, rec, "early", early, date).Amount; !near(got, want) {
			t.Errorf("early on %v: got %v, want %v", date, got, want)
		}
	}
}

func TestProjectionAccountBreakdownAcrossOpening(t *testing.T) {
	rec := villaRecord(t)
	from, to := d(2026, time.February, 15), d(2026, time.April, 1)
	for _, target := range []model.AssetID{model.DefaultAssetID("villa"), "everything"} {
		bd, err := Breakdown(rec, "villa", target, from, to)
		must(t, err)
		if !near(bd.Total(), bd.End-bd.Start) {
			t.Errorf("%v: total %v, change %v", target, bd.Total(), bd.End-bd.Start)
		}
		opening := 0.0
		for _, l := range bd.Lines {
			switch l.Cause {
			case CauseOpening:
				opening = l.Actual + l.Projected
			case CauseUnexplained, CauseDefault:
				t.Errorf("%v: unexplained remainder %+v", target, l)
			}
		}
		if !near(opening, 1000) {
			t.Errorf("%v: opening line %v, want 1000", target, opening)
		}
	}
	// A window starting on the opening day has no opening line.
	bd, err := Breakdown(rec, "villa", model.DefaultAssetID("villa"), d(2026, time.March, 1), to)
	must(t, err)
	for _, l := range bd.Lines {
		if l.Cause == CauseOpening {
			t.Errorf("opening line in a window after opening: %+v", l)
		}
	}
}

func TestProjectionAccountSharedCutoff(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddAsset(&model.Asset{ID: "everything", Members: []model.Member{model.RuleMember(model.Rule{})}}))
	pay := model.BudgetItem{ID: "pay", Kind: model.ItemTransfer, Amount: 100, Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, From: "late", To: "early"}
	must(t, rec.AddProjection(&model.Projection{
		Name:     "early",
		Accounts: []model.ProjAccount{{ID: "early", Name: "Early", Type: model.TypeSavings, Tax: model.Taxable, Opening: 1000, From: d(2025, time.July, 1)}},
		Mods:     []model.Modification{{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "pay"}, Property: model.PropAddItem, Item: &pay}},
	}))
	late, early := model.DefaultAssetID("late"), model.DefaultAssetID("early")
	// Neither side of the transfer is modelled until the later of the two cutoffs (the projection start), so they agree on every date.
	for _, tc := range []struct {
		date  model.Date
		moved float64
	}{
		{d(2025, time.November, 1), 0},
		{d(2026, time.January, 1), 0},
		{d(2026, time.February, 1), 100},
		{d(2026, time.March, 1), 200},
	} {
		date, n := tc.date, tc.moved/100
		l := sample(t, rec, "early", late, date).Amount
		e := sample(t, rec, "early", early, date).Amount
		all := sample(t, rec, "early", "everything", date).Amount
		base := sample(t, rec, "", "everything", date).Amount
		if !near(l, sample(t, rec, "", late, date).Amount-100*n) {
			t.Errorf("late on %v: %v", date, l)
		}
		if !near(e, 1000+100*n) {
			t.Errorf("early on %v: %v, want %v", date, e, 1000+100*n)
		}
		if !near(all-base, 1000) {
			t.Errorf("aggregate on %v gains %v, want 1000", date, all-base)
		}
	}
	// The fixture itself leaves some of the aggregate unexplained without any projection (inv's December-to-January snapshot gap, and the record-only late-deposit transfer counted on one side because late and chk start on different dates); the projection's transfer must add nothing to it.
	unexplained := func(bd *BreakdownResult) float64 {
		sum := 0.0
		for _, l := range bd.Lines {
			if l.Cause == CauseUnexplained {
				sum += l.Actual + l.Projected
			}
		}
		return sum
	}
	from, to := d(2025, time.October, 15), d(2026, time.February, 1)
	baseBd, err := Breakdown(rec, "", "everything", from, to)
	must(t, err)
	for _, target := range []model.AssetID{late, early, "everything"} {
		bd, err := Breakdown(rec, "early", target, from, to)
		must(t, err)
		want := 0.0
		if target == "everything" {
			want = unexplained(baseBd)
		}
		if got := unexplained(bd); !near(got, want) {
			t.Errorf("%v: unexplained %v, want %v", target, got, want)
		}
		if !near(bd.Total(), bd.End-bd.Start) {
			t.Errorf("%v: total %v, change %v", target, bd.Total(), bd.End-bd.Start)
		}
	}
}
