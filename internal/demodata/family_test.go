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
	if len(rec.Accounts) != 14 {
		t.Errorf("accounts: got %d, want 14", len(rec.Accounts))
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
	wantAccounts := map[model.AssetID]int{AllCash: 2, Retirement: 3, Colleges: 4, NetWorth: 14, Liquid: 3}
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

	// Withdraw: the 401k drops by the withdrawal and the growth it would have earned; net worth loses only that growth until the tax is paid with the return in April 2028, then the extra tax too.
	after := d(2027, time.July, 1)
	k401Base, k401W := at(ProjBase, model.DefaultAssetID(K401), after).Amount, at(ProjWithdraw, model.DefaultAssetID(K401), after).Amount
	if drop := k401Base - k401W; drop < 100000 || drop > 102000 {
		t.Errorf("401k drop: got %v, want about 100000", drop)
	}
	lost := at(ProjBase, NetWorth, after).Amount - at(ProjWithdraw, NetWorth, after).Amount
	if lost < 0 || lost > 2000 {
		t.Errorf("net worth lost: got %v, want only lost growth", lost)
	}
	base27, err := engine.TaxYearOf(rec, ProjBase, 2027)
	if err != nil {
		t.Fatal(err)
	}
	w27, err := engine.TaxYearOf(rec, ProjWithdraw, 2027)
	if err != nil {
		t.Fatal(err)
	}
	extra, paid := w27.Result.Total-base27.Result.Total, d(2028, time.June, 1)
	lost = at(ProjBase, NetWorth, paid).Amount - at(ProjWithdraw, NetWorth, paid).Amount
	if lost < extra || lost > extra+8000 {
		t.Errorf("net worth lost after the return: got %v, want the extra tax %v and a little growth", lost, extra)
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
	// The withdrawal's tax is real money leaving, in the projected column only, with the 2027 return in April 2028.
	bd, err = engine.Breakdown(rec, ProjWithdraw, NetWorth, d(2026, time.January, 1), d(2028, time.December, 31))
	if err != nil {
		t.Fatal(err)
	}
	base, err := engine.Breakdown(rec, ProjBase, NetWorth, d(2026, time.January, 1), d(2028, time.December, 31))
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
	base27, err := engine.TaxYearOf(rec, ProjBase, 2027)
	if err != nil {
		t.Fatal(err)
	}
	w27, err := engine.TaxYearOf(rec, ProjWithdraw, 2027)
	if err != nil {
		t.Fatal(err)
	}
	if diff, want := taxOf(bd).Projected-taxOf(base).Projected, base27.Result.Total-w27.Result.Total; math.Abs(diff-want) > 0.01 || want > -30000 {
		t.Errorf("withdrawal tax: got %v, want %v", diff, want)
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

func TestCrashProjectionShocksAndRebalances(t *testing.T) {
	rec := Family()
	p := rec.Projection(ProjCrash)
	if p == nil || len(p.Shocks) != 1 || len(p.Rebalances) != 1 || p.Shocks[0].Change != -0.40 || p.Rebalances[0].Account != Brokerage {
		t.Fatalf("projection %+v", p)
	}
	res, err := engine.Balances(rec, ProjCrash, model.DefaultAssetID(Brokerage), []model.Date{d(2027, time.March, 1), d(2027, time.March, 2), d(2028, time.January, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("skipped %+v", res.Skipped)
	}
	if before, after := res.Samples[0].Amount, res.Samples[1].Amount; !(after < 0.9*before) {
		t.Errorf("brokerage %v before the shock and %v after", before, after)
	}
	ty, err := engine.TaxYearOf(rec, ProjCrash, 2027)
	if err != nil {
		t.Fatal(err)
	}
	if ty.ShortTermGains+ty.LongTermGains <= 0 || ty.GainsTax <= 0 {
		t.Errorf("the rebalance realized %v short and %v long, gains tax %v", ty.ShortTermGains, ty.LongTermGains, ty.GainsTax)
	}
	// The shock alone sells nothing, in the 401k and the other sheltered accounts as everywhere.
	if err := rec.AddProjection(&model.Projection{Name: "shock only", Parent: ProjBase, Shocks: p.Shocks}); err != nil {
		t.Fatal(err)
	}
	shock, err := engine.TaxYearOf(rec, "shock only", 2027)
	if err != nil {
		t.Fatal(err)
	}
	if shock.ShortTermGains != 0 || shock.LongTermGains != 0 {
		t.Errorf("the shock alone realized %v short and %v long", shock.ShortTermGains, shock.LongTermGains)
	}
}

func TestFamilyBenefits(t *testing.T) {
	rec := Family()
	a, b := rec.Household.Members[0], rec.Household.Members[1]
	if a.SSBenefit != 3100 || a.SSClaimAge != 67 || a.HasFERS() {
		t.Errorf("Parent A %+v", a)
	}
	if b.SSBenefit != 1200 || b.SSClaimAge != 62 || b.FERSHigh3 != 105000 || b.FERSYears != 30 || b.FERSStartAge != 57 || b.FERSSurvivor != model.SurvivorPartial {
		t.Errorf("Parent B %+v", b)
	}
	got, _, err := engine.BenefitsOf(rec, "", 2060)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SSOwn <= 0 || got[1].SSSpousal <= 0 || got[1].FERSAnnuity <= 0 {
		t.Errorf("benefits in 2060: %+v", got)
	}
}

func TestRetireAt60Projection(t *testing.T) {
	rec := Family()
	p := rec.Projection(ProjRetire60)
	// Parent A is born 1985-03-12 and turns 60 on the retirement date.
	if p == nil || p.Parent != ProjBase || p.RetireOn == nil || *p.RetireOn != d(2045, time.March, 12) || p.RothBracket == nil || *p.RothBracket != 0.22 {
		t.Fatalf("projection %+v", p)
	}
	if got := rec.Household.Members[0].AgeIn(2045); got != 60 {
		t.Errorf("Parent A is %d in 2045", got)
	}
	base, err := engine.TaxYearOf(rec, ProjBase, 2046)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := engine.TaxYearOf(rec, ProjRetire60, 2046)
	if err != nil {
		t.Fatal(err)
	}
	if base.Wages <= 0 || retired.Wages != 0 {
		t.Errorf("wages in 2046: %v in Base, %v retired", base.Wages, retired.Wages)
	}
	if retired.Drawn <= 0 && retired.Conversion <= 0 {
		t.Errorf("a retired household in 2046 draws on its accounts or converts: %+v", retired)
	}
	if res, err := engine.Balances(rec, ProjRetire60, NetWorth, []model.Date{d(2056, time.December, 31)}); err != nil || len(res.Skipped) != 0 {
		t.Errorf("net worth to 2056: %v %+v", err, res.Skipped)
	}
}

// TestFamilyCardStaysBounded checks that the card owes money while its opening balance is paid off, and then neither builds a large credit nor compounds a debt.
func TestFamilyCardStaysBounded(t *testing.T) {
	rec := Family()
	if err := rec.AddAsset(&model.Asset{ID: "card-only", Name: "Card", Members: []model.Member{model.AccountMember(Card)}}); err != nil {
		t.Fatal(err)
	}
	dates := []model.Date{d(2026, time.December, 31), d(2045, time.December, 31), d(2060, time.December, 31), d(2100, time.December, 31)}
	res, err := engine.Balances(rec, ProjBase, "card-only", dates)
	if err != nil {
		t.Fatal(err)
	}
	if res.Samples[0].Amount >= 0 {
		t.Errorf("the card owes nothing on %v: %v", dates[0], res.Samples[0].Amount)
	}
	for _, s := range res.Samples[1:] {
		if math.Abs(s.Amount) >= 5000 {
			t.Errorf("the card is not small on %v: %v", s.Date, s.Amount)
		}
	}
}

func TestFamilyCollegeSavings(t *testing.T) {
	rec := Family()
	bills := map[string]int{}
	for _, it := range rec.Budget.Items {
		if a := rec.Account(it.Account); it.Kind == model.ItemExpense && a != nil && a.Type == model.Type529 {
			bills[a.Beneficiary]++
			if it.Start == nil || it.End == nil {
				t.Errorf("%s has no college years", it.ID)
			}
		}
	}
	n := 0
	for _, a := range rec.Accounts {
		if a.Type != model.Type529 {
			continue
		}
		n++
		i := rec.Household.Member(a.Beneficiary)
		if i < 0 || rec.Household.Members[i].Role != model.RoleDependent {
			t.Errorf("%s: beneficiary %q is not a dependent", a.ID, a.Beneficiary)
			continue
		}
		last := a.Snapshots[len(a.Snapshots)-1].Balance
		if a.Basis == nil || *a.Basis >= last || *a.Basis <= 0 {
			t.Errorf("%s: basis %v, balance %v", a.ID, a.Basis, last)
		}
		if bills[a.Beneficiary] != 8 {
			t.Errorf("%s: %d college bills, want 8", a.ID, bills[a.Beneficiary])
		}
	}
	if n != 4 {
		t.Errorf("%d 529 accounts, want 4", n)
	}
	// The oldest child is in college today and the others start later, each after turning 18.
	for _, c := range colleges {
		m := rec.Household.Members[rec.Household.Member(c.child)]
		by, bm, _ := m.Birth.YMD()
		year := by + 18
		if bm >= time.August {
			year++
		}
		if c.first != model.D(year, time.August, 1) {
			t.Errorf("%s starts college on %s, want the August after turning 18", c.child, c.first)
		}
	}
	if colleges[0].first > FamilyToday {
		t.Error("the oldest child is in college today")
	}
}
