// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package demodata

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
)

func d(y int, m time.Month, day int) model.Date { return model.D(y, m, day) }

func TestFamilyStructure(t *testing.T) {
	rec := Family()
	if rec.Today != FamilyToday {
		t.Errorf("today: got %v", rec.Today)
	}
	if len(rec.Accounts) != 13 {
		t.Errorf("accounts: got %d, want 13", len(rec.Accounts))
	}
	for _, a := range rec.Accounts {
		if rec.Asset(model.DefaultAssetID(a.ID)) == nil {
			t.Errorf("%s has no default asset", a.ID)
		}
	}
	for _, p := range []string{ProjBase, ProjNewJob, ProjWithdraw} {
		if _, err := rec.Chain(p); err != nil {
			t.Errorf("projection %q: %v", p, err)
		}
	}
	if got := rec.Projection(ProjNewJob); got.Parent != ProjBase || len(got.Mods) != 2 {
		t.Errorf("New job: got %+v", got)
	}
	wantAccounts := map[model.AssetID]int{AllCash: 2, Retirement: 3, Colleges: 3, NetWorth: 13, Liquid: 3}
	for id, n := range wantAccounts {
		res, err := rec.Resolve(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if len(res.Accounts) != n {
			t.Errorf("%s: got %d accounts %v, want %d", id, len(res.Accounts), res.Accounts, n)
		}
	}
	// Net worth counts every account exactly once.
	if res, _ := rec.Resolve(NetWorth); len(res.Overlaps) != 0 {
		t.Errorf("net worth overlaps: %v", res.Overlaps)
	}
	// Liquid names checking again after All cash already includes it.
	res, _ := rec.Resolve(Liquid)
	if len(res.Overlaps) != 1 || res.Overlaps[0].Duplicates[0] != Checking || res.Overlaps[0].Asset != Liquid {
		t.Errorf("liquid overlaps: got %v", res.Overlaps)
	}
	if b, err := rec.Balance(NetWorth, FamilyToday); err != nil || !b.Actual || b.Amount <= 0 {
		t.Errorf("net worth today: got %+v %v", b, err)
	}
	// Liabilities carry negative balances.
	for _, id := range []model.AccountID{Card, Mortgage} {
		if b, _ := rec.Balance(model.DefaultAssetID(id), FamilyToday); b.Amount >= 0 {
			t.Errorf("%s: got %v, want negative", id, b.Amount)
		}
	}
	for _, s := range rec.Account(Checking).Snapshots {
		if s.Balance <= 0 {
			t.Errorf("checking overdrawn on %v: %v", s.Date, s.Balance)
		}
	}
}

func TestFamilyDataSpans(t *testing.T) {
	rec := Family()
	last := func(id model.AccountID) model.Date {
		sp, ok := rec.Account(id).Span()
		if !ok {
			t.Fatalf("%s has no data", id)
		}
		return sp.To
	}
	if got := last(TSP); got != tspEnd {
		t.Errorf("TSP data ends %v, want %v", got, tspEnd)
	}
	if got := last(Home); got != d(2026, time.May, 10) || len(rec.Account(Home).Snapshots) != 3 || len(rec.Account(Home).Transactions) != 0 {
		t.Errorf("home: last %v, %d valuations", got, len(rec.Account(Home).Snapshots))
	}
	for _, id := range []model.AccountID{Checking, Savings, Card, Brokerage, K401, Roth, Mortgage, College1} {
		if got := last(id); got != FamilyToday {
			t.Errorf("%s data ends %v, want %v", id, got, FamilyToday)
		}
	}
	res, _ := rec.Resolve(Retirement)
	if got, ok := res.LastActual(); !ok || got != FamilyToday {
		t.Errorf("retirement last actual: got %v %v", got, ok)
	}
	// Roth's default covers the time before its history begins.
	if b, _ := rec.Balance(model.DefaultAssetID(Roth), d(2024, time.June, 1)); b.Actual || b.Amount != 70000 {
		t.Errorf("roth default: got %+v", b)
	}
	// Transactions and snapshots disagree only by growth: the unexplained part is small for cash.
	if u := rec.Account(Checking).UnexplainedIn(model.MinDate, model.MaxDate); math.Abs(u) > 1 {
		t.Errorf("checking unexplained: got %v", u)
	}
	if u := rec.Account(K401).UnexplainedIn(model.MinDate, model.MaxDate); u < 10000 {
		t.Errorf("401k unexplained growth: got %v", u)
	}
}

func TestFamilyProjections(t *testing.T) {
	rec := Family()
	at := func(proj string, asset model.AssetID, date model.Date) engine.Sample {
		t.Helper()
		res, err := engine.Balances(rec, proj, asset, []model.Date{date})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Skipped) != 0 {
			t.Fatalf("%s: diagnostics %+v", proj, res.Diagnostics)
		}
		return res.Samples[0]
	}
	starts, err := engine.Starts(rec, ProjBase, Retirement)
	if err != nil {
		t.Fatal(err)
	}
	got := map[model.AccountID]model.Date{}
	for _, s := range starts {
		got[s.Account] = s.Start
	}
	if want := map[model.AccountID]model.Date{K401: FamilyToday, TSP: tspEnd, Roth: FamilyToday}; !reflect.DeepEqual(got, want) {
		t.Errorf("starts: got %v, want %v", got, want)
	}

	// The TSP's real data ends 2026-06-30: read back before, projected from there, so in
	// the months between the aggregate is partly projected.
	if s := at(ProjBase, Retirement, tspEnd); s.Status != engine.StatusActual {
		t.Errorf("at the TSP's last data: got %v", s.Status)
	}
	mid := d(2026, time.September, 30)
	if s := at(ProjBase, Retirement, mid); s.Status != engine.StatusPartlyProjected {
		t.Errorf("between the TSP's end and today: got %v", s.Status)
	}
	if s := at(ProjBase, Retirement, d(2030, time.January, 1)); s.Status != engine.StatusProjected {
		t.Errorf("later: got %v", s.Status)
	}
	var sum float64
	for _, id := range []model.AccountID{K401, TSP, Roth} {
		sum += at(ProjBase, model.DefaultAssetID(id), mid).Amount
	}
	if want := at(ProjBase, Retirement, mid).Amount; math.Abs(sum-want) > 1e-6 {
		t.Errorf("retirement %v != sum of accounts %v", want, sum)
	}
	if tsp := at(ProjBase, model.DefaultAssetID(TSP), mid); tsp.Status != engine.StatusProjected || tsp.Amount <= rec.Account(TSP).Snapshots[len(rec.Account(TSP).Snapshots)-1].Balance {
		t.Errorf("tsp should have grown from its last value: got %+v", tsp)
	}

	// New job: the raise and the lower growth schedule change the answer; Base is untouched by it.
	when := d(2028, time.December, 31)
	base, job := at(ProjBase, NetWorth, when).Amount, at(ProjNewJob, NetWorth, when).Amount
	if base == job {
		t.Error("New job gives the same net worth as Base")
	}
	if bb, jb := at(ProjBase, model.DefaultAssetID(Brokerage), when).Amount, at(ProjNewJob, model.DefaultAssetID(Brokerage), when).Amount; !(jb < bb) {
		t.Errorf("brokerage at 4%% should trail 6%%: %v vs %v", jb, bb)
	}
	if again := at(ProjBase, NetWorth, when).Amount; again != base {
		t.Errorf("Base changed: %v vs %v", again, base)
	}

	// Withdraw: the 401k drops by the withdrawal and its tax; net worth loses only the tax and lost growth.
	after := d(2027, time.July, 1)
	k401Base, k401W := at(ProjBase, model.DefaultAssetID(K401), after).Amount, at(ProjWithdraw, model.DefaultAssetID(K401), after).Amount
	if drop := k401Base - k401W; drop < 132000 || drop > 135000 {
		t.Errorf("401k drop: got %v, want about 132000", drop)
	}
	lost := at(ProjBase, NetWorth, after).Amount - at(ProjWithdraw, NetWorth, after).Amount
	if lost < 32000 || lost > 34000 {
		t.Errorf("net worth lost: got %v, want about 32000", lost)
	}
}

func TestFamilyBreakdowns(t *testing.T) {
	rec := Family()
	intervals := [][2]model.Date{
		{d(2025, time.March, 1), d(2026, time.March, 1)},                 // all actual
		{d(2026, time.May, 1), d(2026, time.December, 1)},                // TSP and home end, others still actual
		{d(2026, time.January, 1), d(2030, time.January, 1)},             // actual into projected
		{d(2027, time.January, 1), d(2035, time.January, 1)},             // projected
		{d(2026, time.November, 8), d(2026, time.November, 8).Add(1000)}, // starts at today
	}
	assets := []model.AssetID{NetWorth, Retirement, AllCash, Colleges, Liquid, model.DefaultAssetID(K401), model.DefaultAssetID(Home)}
	for _, proj := range []string{"", ProjBase, ProjNewJob, ProjWithdraw} {
		for _, a := range assets {
			for _, iv := range intervals {
				bd, err := engine.Breakdown(rec, proj, a, iv[0], iv[1])
				if err != nil {
					t.Fatal(err)
				}
				res, err := engine.Balances(rec, proj, a, []model.Date{iv[0], iv[1]})
				if err != nil {
					t.Fatal(err)
				}
				change := res.Samples[1].Amount - res.Samples[0].Amount
				if math.Abs(bd.Total()-change) > 1e-5 {
					t.Errorf("%q %s %v..%v: breakdown %v, balance change %v", proj, a, iv[0], iv[1], bd.Total(), change)
				}
				// Every account has data across the first interval, so a default line means misattribution.
				if iv == intervals[0] {
					for _, l := range bd.Lines {
						if l.Cause == engine.CauseDefault {
							t.Errorf("%q %s %v..%v: unexpected default line %+v", proj, a, iv[0], iv[1], l)
						}
					}
				}
			}
		}
	}

	// Net worth contains every account, so no transfer, contribution or withdrawal remains: all cancel.
	bd, err := engine.Breakdown(rec, ProjWithdraw, NetWorth, d(2026, time.January, 1), d(2028, time.January, 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range bd.Lines {
		switch l.Cause {
		case engine.CauseTransfer, engine.CauseContribution, engine.CauseWithdrawal, engine.CauseDefault:
			t.Errorf("net worth shows %+v", l)
		}
	}
	// The withdrawal's tax is real money leaving, in the projected column only.
	base, err := engine.Breakdown(rec, ProjBase, NetWorth, d(2026, time.January, 1), d(2028, time.January, 1))
	if err != nil {
		t.Fatal(err)
	}
	taxOf := func(b *engine.BreakdownResult) engine.Line {
		for _, l := range b.Lines {
			if l.Cause == engine.CauseTax {
				return l
			}
		}
		return engine.Line{}
	}
	if diff := taxOf(bd).Projected - taxOf(base).Projected; math.Abs(diff+32000) > 1e-6 {
		t.Errorf("withdrawal tax: got %v, want -32000", diff)
	}
	// The 401k alone sees the withdrawal and the tax.
	k, _ := engine.Breakdown(rec, ProjWithdraw, model.DefaultAssetID(K401), d(2027, time.January, 1), d(2027, time.December, 31))
	for _, l := range k.Lines {
		if l.Cause == engine.CauseWithdrawal && math.Abs(l.Projected+100000) > 1e-6 {
			t.Errorf("withdrawal: got %+v", l)
		}
	}
}

func TestFamilyDeterministic(t *testing.T) {
	a, b := Family(), Family()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("Family builds different records")
	}
	times := []model.Date{d(2025, time.June, 1), d(2026, time.October, 1), d(2040, time.January, 1)}
	for _, proj := range []string{"", ProjBase, ProjNewJob, ProjWithdraw} {
		r1, err1 := engine.Balances(a, proj, NetWorth, times)
		r2, err2 := engine.Balances(b, proj, NetWorth, times)
		if err1 != nil || err2 != nil || !reflect.DeepEqual(r1, r2) {
			t.Fatalf("%q: %v %v: %+v vs %+v", proj, err1, err2, r1, r2)
		}
	}
}

func TestAcmeScenarios(t *testing.T) {
	rec := Family()
	at := func(proj string, id model.AccountID, when model.Date) float64 {
		t.Helper()
		res, err := engine.Balances(rec, proj, model.DefaultAssetID(id), []model.Date{when})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Skipped) != 0 {
			t.Fatalf("%s: skipped %+v", proj, res.Skipped)
		}
		return res.Samples[0].Amount
	}
	when := d(2028, time.June, 30)
	baseEquity, baseBrokerage := at(ProjBase, Equity, when), at(ProjBase, Brokerage, when)
	if baseEquity <= 0 {
		t.Fatalf("base equity %v", baseEquity)
	}
	if up := at(ProjAcmeUp, Equity, when); !(up > baseEquity) {
		t.Errorf("equity under +15%%: %v, base %v", up, baseEquity)
	}
	if up := at(ProjAcmeUp, Brokerage, when); !(up > baseBrokerage) {
		t.Errorf("brokerage under +15%%: %v, base %v", up, baseBrokerage)
	}
	if crash := at(ProjAcmeCrash, Equity, when); !(crash < baseEquity) {
		t.Errorf("equity under the crash: %v, base %v", crash, baseEquity)
	}
	if before := d(2027, time.May, 31); at(ProjAcmeCrash, Equity, before) != at(ProjBase, Equity, before) {
		t.Error("the crash lowers the equity before its date")
	}
	if g := at(ProjAcmeGrants, Equity, when); !(g > baseEquity) {
		t.Errorf("equity with yearly grants: %v, base %v", g, baseEquity)
	}
}
