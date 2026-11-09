// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

func d(y int, m time.Month, day int) model.Date { return model.D(y, m, day) }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// fixture is a small record with exact numbers:
//
//	chk:  checking, no growth, real data 2025-12-01..2026-01-01, balance 1000 on both ends
//	inv:  401k at 6%, real data 2025-12-01..2026-01-01 (snapshots 10000 and 10500, 300 transferred in)
//	late: tax-free savings, no growth, real data ends 2025-10-31 at 2000
//	bare: no actual data, default 500 until 2026-06-30
//
// Budget: salary 1000 gross monthly on the 15th into chk, rent 200 on the 20th, 300 from chk to inv
// on the 16th, 100 from chk to late on the 1st.
func fixture(t *testing.T) *model.Record {
	t.Helper()
	rec := model.NewRecord(d(2026, time.January, 1))
	chk := &model.Account{ID: "chk", Type: model.TypeChecking, Tax: model.Taxable}
	chk.AddSnapshot(d(2025, time.December, 1), 1000)
	chk.AddSnapshot(d(2026, time.January, 1), 1000)
	inv := &model.Account{ID: "inv", Type: model.Type401k, Tax: model.TaxDeferred, StatedRate: 0.06}
	inv.AddSnapshot(d(2025, time.December, 1), 10000)
	inv.AddSnapshot(d(2026, time.January, 1), 10500)
	late := &model.Account{ID: "late", Type: model.TypeSavings, Tax: model.TaxFree}
	late.AddSnapshot(d(2025, time.October, 1), 2000)
	late.AddSnapshot(d(2025, time.October, 31), 2000)
	bare := &model.Account{ID: "bare", Type: model.TypeSavings, Tax: model.Taxable}
	bare.Default.Intervals = []model.Interval{{From: model.MinDate, To: d(2026, time.June, 30), Value: 500}}
	for _, tx := range []model.Transaction{
		{Date: d(2025, time.December, 15), Amount: 500, Kind: model.TxIncome},
		{Date: d(2025, time.December, 16), Amount: -300, Kind: model.TxTransfer, Counter: "inv"},
		{Date: d(2025, time.December, 20), Amount: -200, Kind: model.TxExpense},
	} {
		must(t, chk.AddTransaction(tx))
	}
	must(t, inv.AddTransaction(model.Transaction{Date: d(2025, time.December, 16), Amount: 300, Kind: model.TxTransfer, Counter: "chk"}))
	for _, a := range []*model.Account{chk, inv, late, bare} {
		must(t, rec.AddAccount(a))
	}
	must(t, rec.AddAsset(&model.Asset{ID: "all", Members: []model.Member{
		model.AccountMember("chk"), model.AccountMember("inv"), model.AccountMember("late"), model.AccountMember("bare"),
	}}))
	must(t, rec.AddAsset(&model.Asset{ID: "pair", Members: []model.Member{model.AccountMember("chk"), model.AccountMember("inv")}}))
	monthly := func(day int) model.Recurrence { return model.Recurrence{Kind: model.Monthly, Day: day} }
	rec.Budget.Items = []model.BudgetItem{
		{ID: "salary", Kind: model.ItemIncome, Amount: 1000, Recurrence: monthly(15), Account: "chk"},
		{ID: "rent", Kind: model.ItemExpense, Amount: 200, Recurrence: monthly(20), Account: "chk"},
		{ID: "save", Kind: model.ItemTransfer, Amount: 300, Recurrence: monthly(16), From: "chk", To: "inv"},
		{ID: "late-deposit", Kind: model.ItemTransfer, Amount: 100, Recurrence: monthly(1), From: "chk", To: "late"},
	}
	return rec
}

func sample(t *testing.T, rec *model.Record, proj string, asset model.AssetID, date model.Date) Sample {
	t.Helper()
	res, err := Balances(rec, proj, asset, []model.Date{date})
	must(t, err)
	return res.Samples[0]
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestProjectionStartRule(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddAsset(&model.Asset{ID: "grp", Members: []model.Member{model.AccountMember("chk")}}))
	must(t, rec.AddAsset(&model.Asset{ID: "top", Members: []model.Member{
		model.AssetMember("grp"), model.AccountMember("inv"), model.AccountMember("late"), model.AccountMember("bare"),
	}}))
	for _, p := range []*model.Projection{
		{Name: "plain", Start: model.Ptr(d(2025, time.December, 10))},
		{Name: "override", Start: model.Ptr(d(2025, time.December, 10)), OverrideHistory: model.Ptr(true)},
		{Name: "per-asset", Start: model.Ptr(d(2026, time.February, 1)), AssetSettings: []model.AssetSetting{
			{Asset: "grp", Start: model.Ptr(d(2025, time.December, 5)), OverrideHistory: model.Ptr(true)},
		}},
		{Name: "child", Parent: "override", OverrideHistory: model.Ptr(false)},
	} {
		must(t, rec.AddProjection(p))
	}
	// "top" holds every account; bare has no actual data and late's data ends early.
	cases := []struct {
		proj string
		want map[model.AccountID]model.Date
	}{
		{"plain", map[model.AccountID]model.Date{
			"chk": d(2026, time.January, 1), "inv": d(2026, time.January, 1), "late": d(2025, time.October, 31), "bare": d(2025, time.December, 10),
		}},
		{"override", map[model.AccountID]model.Date{
			"chk": d(2025, time.December, 10), "inv": d(2025, time.December, 10), "late": d(2025, time.October, 31), "bare": d(2025, time.December, 10),
		}},
		{"per-asset", map[model.AccountID]model.Date{
			"chk": d(2025, time.December, 5), "inv": d(2026, time.January, 1), "late": d(2025, time.October, 31), "bare": d(2026, time.February, 1),
		}},
		{"child", map[model.AccountID]model.Date{
			"chk": d(2026, time.January, 1), "inv": d(2026, time.January, 1), "late": d(2025, time.October, 31), "bare": d(2025, time.December, 10),
		}},
		{"", map[model.AccountID]model.Date{
			"chk": d(2026, time.January, 1), "inv": d(2026, time.January, 1), "late": d(2025, time.October, 31), "bare": d(2026, time.January, 1),
		}},
	}
	for _, c := range cases {
		starts, err := Starts(rec, c.proj, "top")
		must(t, err)
		got := map[model.AccountID]model.Date{}
		for _, s := range starts {
			got[s.Account] = s.Start
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %v, want %v", c.proj, got, c.want)
		}
	}
	starts, _ := Starts(rec, "per-asset", "top")
	if !starts[0].OverrideHistory || starts[1].OverrideHistory || !starts[0].HasActual || starts[3].HasActual {
		t.Errorf("start details: got %+v", starts)
	}
}

func TestOverrideHistoryIgnoresLaterRealData(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddProjection(&model.Projection{Name: "back", Start: model.Ptr(d(2025, time.December, 10)), OverrideHistory: model.Ptr(true)}))
	// Real data says 1000 on 2026-01-01. Starting at 12-10 replays 12-15 salary +750, 16th -300, 20th -200, 1st -100.
	got := sample(t, rec, "back", model.DefaultAssetID("chk"), d(2026, time.January, 1))
	if got.Status != StatusProjected || !near(got.Amount, 1150) {
		t.Errorf("overridden: got %+v, want 1150 projected", got)
	}
	got = sample(t, rec, "back", model.DefaultAssetID("chk"), d(2025, time.December, 10))
	if got.Status != StatusActual || !near(got.Amount, 1000) {
		t.Errorf("at the start the real value is read: got %+v", got)
	}
}

func TestExactFlows(t *testing.T) {
	rec := fixture(t)
	chk := model.DefaultAssetID("chk")
	// Actual: 1000 + 500 (12-15) - 300 (12-16).
	if got := sample(t, rec, "", chk, d(2025, time.December, 16)); got.Status != StatusActual || !near(got.Amount, 1200) {
		t.Errorf("actual: got %+v", got)
	}
	// From 2026-01-01: three salaries (750 net of the 25% placeholder tax), rents and saves, and two late deposits.
	want := 1000.0 + 3*(750-200-300) - 2*100
	if got := sample(t, rec, "", chk, d(2026, time.March, 31)); got.Status != StatusProjected || !near(got.Amount, want) {
		t.Errorf("projected: got %+v, want %v", got, want)
	}
	// Growth compounds over exact elapsed time: inv at 6% with 300 added on the 16th.
	inv := model.DefaultAssetID("inv")
	got := sample(t, rec, "", inv, d(2026, time.January, 15))
	if want := 10500 * math.Pow(1.06, 14/model.DaysPerYear); !near(got.Amount, want) {
		t.Errorf("inv before flow: got %v, want %v", got.Amount, want)
	}
	got = sample(t, rec, "", inv, d(2026, time.January, 17))
	if want := (10500*math.Pow(1.06, 15/model.DaysPerYear) + 300) * math.Pow(1.06, 1/model.DaysPerYear); !near(got.Amount, want) {
		t.Errorf("inv after flow: got %v, want %v", got.Amount, want)
	}
}

func TestAggregateProjectsEachAccountFromItsOwnEnd(t *testing.T) {
	rec := fixture(t)
	all := model.AssetID("all")
	// 2025-12-20: chk and inv are real, late ran out on 10-31 and is projected from there
	// (deposits on 11-01 and 12-01), bare is on defaults.
	got := sample(t, rec, "", all, d(2025, time.December, 20))
	if got.Status != StatusPartlyProjected || !near(got.Amount, 1000+10300+2200+500) {
		t.Errorf("partly projected: got %+v, want %v", got, 1000+10300+2200+500)
	}
	// The aggregate is the sum of the accounts asked one by one.
	sum := 0.0
	for _, id := range []model.AccountID{"chk", "inv", "late", "bare"} {
		sum += sample(t, rec, "", model.DefaultAssetID(id), d(2025, time.December, 20)).Amount
	}
	if !near(sum, got.Amount) {
		t.Errorf("sum of accounts %v != aggregate %v", sum, got.Amount)
	}
	// While only late has data it is all actual; defaults fill the rest.
	if got := sample(t, rec, "", all, d(2025, time.October, 15)); got.Status != StatusActual || !near(got.Amount, 2000+500) {
		t.Errorf("actual: got %+v", got)
	}
	// Far out everything is projected.
	if got := sample(t, rec, "", all, d(2027, time.January, 1)); got.Status != StatusProjected {
		t.Errorf("projected: got %+v", got)
	}
	// No account has data before 2025-10: only defaults.
	if got := sample(t, rec, "", "pair", d(2025, time.September, 1)); got.Status != StatusDefault || got.Amount != 0 {
		t.Errorf("defaults only: got %+v", got)
	}
}

func TestScopeComputesOnlyAccountsUnderTheAsset(t *testing.T) {
	rec := fixture(t)
	date := d(2026, time.March, 31)
	chk := sample(t, rec, "", model.DefaultAssetID("chk"), date).Amount
	inv := sample(t, rec, "", model.DefaultAssetID("inv"), date).Amount
	if pair := sample(t, rec, "", "pair", date).Amount; !near(pair, chk+inv) {
		t.Errorf("pair %v != %v + %v", pair, chk, inv)
	}
	// Both the saves into inv (3 x 300) and the deposits to the tax-free late (2 x 100, Feb 1 and Mar 1) leave chk for an outside sheltered account, so each counts as a contribution.
	bd, err := Breakdown(rec, "", model.DefaultAssetID("chk"), d(2026, time.January, 1), date)
	must(t, err)
	if l := line(bd, CauseContribution); !near(l.Projected, -1100) {
		t.Errorf("contributions leave chk: got %+v", l)
	}
	bd, err = Breakdown(rec, "", "pair", d(2026, time.January, 1), date)
	must(t, err)
	// Only the deposits to the outside tax-free account remain; the saves into inv cancel.
	if l := line(bd, CauseContribution); !near(l.Projected, -200) {
		t.Errorf("internal transfer should cancel: got %+v", l)
	}
}

func line(b *BreakdownResult, c Cause) Line {
	for _, l := range b.Lines {
		if l.Cause == c {
			return l
		}
	}
	return Line{}
}

func TestBreakdownTotalEqualsBalanceChange(t *testing.T) {
	rec := fixture(t)
	intervals := [][2]model.Date{
		{d(2025, time.December, 5), d(2026, time.April, 30)}, // spans actual and projected
		{d(2025, time.October, 15), d(2026, time.June, 1)},   // starts before some accounts have data
		{d(2026, time.February, 1), d(2027, time.February, 1)},
		{d(2025, time.December, 2), d(2025, time.December, 18)},
		{d(2026, time.March, 1), d(2026, time.March, 1)},
	}
	assets := []model.AssetID{"all", "pair", model.DefaultAssetID("chk"), model.DefaultAssetID("inv"), model.DefaultAssetID("late")}
	for _, proj := range []string{"", "back"} {
		if proj != "" {
			must(t, rec.AddProjection(&model.Projection{Name: proj, Start: model.Ptr(d(2025, time.December, 10)), OverrideHistory: model.Ptr(true)}))
		}
		for _, a := range assets {
			for _, iv := range intervals {
				bd, err := Breakdown(rec, proj, a, iv[0], iv[1])
				must(t, err)
				res, err := Balances(rec, proj, a, []model.Date{iv[0], iv[1]})
				must(t, err)
				change := res.Samples[1].Amount - res.Samples[0].Amount
				if !near(bd.Total(), change) || !near(bd.End-bd.Start, change) {
					t.Errorf("%q %s %v..%v: breakdown %v, balance change %v", proj, a, iv[0], iv[1], bd.Total(), change)
				}
				// Inside chk's and inv's data no default applies, so a default line means misattribution.
				if iv == intervals[3] && a != "all" && a != model.DefaultAssetID("late") {
					if l := line(bd, CauseDefault); l != (Line{}) {
						t.Errorf("%q %s %v..%v: unexpected default line %+v", proj, a, iv[0], iv[1], l)
					}
				}
			}
		}
	}
}

func TestBreakdownCauses(t *testing.T) {
	rec := fixture(t)
	bd, err := Breakdown(rec, "", "pair", d(2025, time.December, 5), d(2026, time.March, 1))
	must(t, err)
	// The 12-16 transfer is recorded on both sides and cancels. inv's 10500 snapshot is 200 more than
	// 10000 + 300 explains. The 12-15 income and 12-20 expense are actual; later flows are projected.
	if l := line(bd, CauseUnexplained); !near(l.Actual, 200) || l.Projected != 0 {
		t.Errorf("unexplained: got %+v", l)
	}
	if l := line(bd, CauseIncome); !near(l.Actual, 500) || !near(l.Projected, 2000) {
		t.Errorf("income: got %+v", l)
	}
	if l := line(bd, CauseExpense); !near(l.Actual, -200) || !near(l.Projected, -400) {
		t.Errorf("expense: got %+v", l)
	}
	if l := line(bd, CauseTax); !near(l.Projected, -500) || l.Actual != 0 {
		t.Errorf("tax: got %+v", l)
	}
	if l := line(bd, CauseGrowth); l.Projected <= 0 || l.Actual != 0 {
		t.Errorf("growth: got %+v", l)
	}
	if l := line(bd, CauseDefault); l != (Line{}) {
		t.Errorf("no defaults are involved: got %+v", l)
	}
	// The saves into inv cancel; only the Feb 1 and Mar 1 deposits to the outside tax-free account show.
	if l := line(bd, CauseContribution); !near(l.Projected, -200) || l.Actual != 0 {
		t.Errorf("contribution: got %+v", l)
	}
	for _, c := range []Cause{CauseTransfer, CauseWithdrawal} {
		if l := line(bd, c); l != (Line{}) {
			t.Errorf("%s should not appear: got %+v", c, l)
		}
	}
	// Defaults appear as their own cause where an account is read back from defaults.
	acct := rec.Account("bare")
	acct.Default.Intervals = append(acct.Default.Intervals, model.Interval{From: d(2025, time.December, 20), To: d(2026, time.January, 31), Value: 800})
	bd, err = Breakdown(rec, "", model.DefaultAssetID("bare"), d(2025, time.December, 10), d(2025, time.December, 25))
	must(t, err)
	if l := line(bd, CauseDefault); !near(l.Actual, 300) || len(bd.Lines) != 1 {
		t.Errorf("default change: got %+v", bd.Lines)
	}
}

func TestModificationsAndSkipping(t *testing.T) {
	rec := fixture(t)
	chk := model.DefaultAssetID("chk")
	raise := model.Modification{
		Target:   model.ObjectRef{Kind: model.KindBudgetItem, ID: "salary"},
		Property: model.PropAmount,
		Values:   model.NewSchedule(model.Segment{From: d(2026, time.February, 1), Value: 2000}),
	}
	must(t, rec.AddProjection(&model.Projection{Name: "raise", Mods: []model.Modification{raise}}))
	must(t, rec.AddProjection(&model.Projection{Name: "broken", Parent: "raise", Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "gone"}, Property: model.PropAmount, Values: model.Constant(1)},
		{Target: model.ObjectRef{Kind: model.KindAccount, ID: "gone"}, Property: model.PropGrowth, Values: model.Constant(1)},
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "rent"}, Property: "colour"},
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "salary"}, Property: model.PropAddItem, Item: &model.BudgetItem{ID: "salary"}},
	}}))
	base := sample(t, rec, "", chk, d(2026, time.March, 31)).Amount
	// Salary Feb 15 and Mar 15 are 2000 gross: 750 net becomes 1500 each.
	if got := sample(t, rec, "raise", chk, d(2026, time.March, 31)).Amount; !near(got, base+1500) {
		t.Errorf("raise: got %v, want %v", got, base+1500)
	}
	res, err := Balances(rec, "broken", chk, []model.Date{d(2026, time.March, 31)})
	must(t, err)
	if len(res.Skipped) != 4 || res.Skipped[0].Reason != "budget item not found" || res.Skipped[3].Index != 3 || res.Skipped[3].Projection != "broken" {
		t.Errorf("skipped: got %+v", res.Skipped)
	}
	if !near(res.Samples[0].Amount, base+1500) {
		t.Errorf("a skipped modification must not stop the others: got %v", res.Samples[0].Amount)
	}
	if _, err := Balances(rec, "missing", chk, nil); err == nil {
		t.Error("unknown projection accepted")
	}
}

func TestWithdrawalPaysTax(t *testing.T) {
	rec := fixture(t)
	w := model.BudgetItem{
		ID: "out", Kind: model.ItemTransfer, Amount: 1000, From: "inv", To: "chk",
		Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: d(2026, time.February, 10)},
	}
	must(t, rec.AddProjection(&model.Projection{Name: "w", Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "out"}, Property: model.PropAddItem, Item: &w},
	}}))
	from, to := d(2026, time.February, 1), d(2026, time.March, 1)
	bd, err := Breakdown(rec, "w", model.DefaultAssetID("inv"), from, to)
	must(t, err)
	if l := line(bd, CauseWithdrawal); !near(l.Projected, -1000) {
		t.Errorf("withdrawal: got %+v", l)
	}
	if l := line(bd, CauseTax); !near(l.Projected, -1000*PlaceholderWithdrawalTaxRate) {
		t.Errorf("tax: got %+v", l)
	}
	// Seen from the pair, the money only moves inside it and just the tax is lost.
	a, b := sample(t, rec, "", "pair", to).Amount, sample(t, rec, "w", "pair", to).Amount
	if lost := a - b; lost < 1000*PlaceholderWithdrawalTaxRate || lost > 1000*PlaceholderWithdrawalTaxRate+10 {
		t.Errorf("lost to tax and growth: got %v", lost)
	}
}

func TestGrowthPrecedence(t *testing.T) {
	rec := fixture(t)
	inv := model.DefaultAssetID("inv")
	must(t, rec.AddAsset(&model.Asset{
		ID: "wrap", Members: []model.Member{model.AccountMember("inv")},
		Params: map[string]model.Schedule{model.PropGrowth: model.Constant(0.10)},
	}))
	date := d(2026, time.February, 1)
	stated := sample(t, rec, "", inv, date).Amount
	// The wrapping asset beats the account's stated rate, but only when queried through the wrapper.
	viaWrap := sample(t, rec, "", "wrap", date).Amount
	if !(viaWrap > stated) {
		t.Errorf("wrapper growth: %v should beat %v", viaWrap, stated)
	}
	must(t, rec.AddProjection(&model.Projection{Name: "low", Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindAccount, ID: "inv"}, Property: model.PropGrowth, Values: model.Constant(0)},
	}}))
	// The projection is outermost: it beats the wrapper, which is outside the account's default asset.
	low := sample(t, rec, "low", "wrap", date).Amount
	if !(low < stated) {
		t.Errorf("projection growth 0: %v should be below %v", low, stated)
	}
}

func TestGrowthConflictIsReported(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddAsset(&model.Asset{ID: "g1", Members: []model.Member{model.AccountMember("inv")}, Params: map[string]model.Schedule{model.PropGrowth: model.Constant(0.01)}}))
	must(t, rec.AddAsset(&model.Asset{ID: "g2", Members: []model.Member{model.AccountMember("inv")}, Params: map[string]model.Schedule{model.PropGrowth: model.Constant(0.02)}}))
	must(t, rec.AddAsset(&model.Asset{ID: "both", Members: []model.Member{model.AssetMember("g1"), model.AssetMember("g2")}}))
	res, err := Balances(rec, "", "both", []model.Date{d(2026, time.February, 1)})
	must(t, err)
	if len(res.Conflicts) != 1 || res.Conflicts[0].Account != "inv" || res.Conflicts[0].Name != model.PropGrowth {
		t.Fatalf("conflicts: got %+v", res.Conflicts)
	}
	if len(res.Overlaps) != 1 || res.Overlaps[0].Asset != "both" {
		t.Errorf("overlaps: got %+v", res.Overlaps)
	}
}

func TestDeterministic(t *testing.T) {
	times := []model.Date{d(2025, time.September, 1), d(2025, time.December, 20), d(2026, time.March, 1), d(2030, time.January, 1)}
	for i := 0; i < 3; i++ {
		a, b := fixture(t), fixture(t)
		for _, asset := range []model.AssetID{"all", "pair"} {
			r1, err1 := Balances(a, "", asset, times)
			r2, err2 := Balances(b, "", asset, times)
			must(t, err1)
			must(t, err2)
			if !reflect.DeepEqual(r1, r2) {
				t.Fatalf("balances differ: %+v vs %+v", r1, r2)
			}
			b1, _ := Breakdown(a, "", asset, times[0], times[3])
			b2, _ := Breakdown(b, "", asset, times[0], times[3])
			if !reflect.DeepEqual(b1, b2) {
				t.Fatalf("breakdowns differ: %+v vs %+v", b1, b2)
			}
		}
	}
	// An asset-level default leaves a non-trivial residual in the breakdown; it must still be exact.
	for i := 0; i < 3; i++ {
		a, b := fixture(t), fixture(t)
		for _, rec := range []*model.Record{a, b} {
			must(t, rec.AddAsset(&model.Asset{ID: "dflt", Members: []model.Member{model.AccountMember("bare"), model.AccountMember("chk")}}))
			rec.Asset("dflt").Default.Intervals = []model.Interval{{From: model.MinDate, To: d(2026, time.June, 30), Value: 123.45}}
		}
		b1, err1 := Breakdown(a, "", "dflt", d(2025, time.November, 3), times[3])
		b2, err2 := Breakdown(b, "", "dflt", d(2025, time.November, 3), times[3])
		must(t, err1)
		must(t, err2)
		if !reflect.DeepEqual(b1, b2) {
			t.Fatalf("asset-default breakdowns differ: %+v vs %+v", b1, b2)
		}
	}
	// Sample order does not change any single answer.
	fwd, _ := Balances(fixture(t), "", "all", times)
	rev, _ := Balances(fixture(t), "", "all", []model.Date{times[3], times[2], times[1], times[0]})
	for i, s := range fwd.Samples {
		if s != rev.Samples[len(times)-1-i] {
			t.Errorf("sample %d: %+v vs %+v", i, s, rev.Samples[len(times)-1-i])
		}
	}
}

func TestInflationIndexing(t *testing.T) {
	rec := fixture(t)
	rec.Globals[model.PropInflation] = model.Constant(0.10)
	rec.Budget.Items[1].Indexed = true // rent
	chk := model.DefaultAssetID("chk")
	from, to := d(2026, time.January, 1), d(2027, time.January, 31)
	bd, err := Breakdown(rec, "", chk, from, to)
	must(t, err)
	flat := 13 * -200.0
	if l := line(bd, CauseExpense); !(l.Projected < flat-50) {
		t.Errorf("indexed rent should exceed 13 x 200: got %v", l.Projected)
	}
	must(t, rec.AddProjection(&model.Projection{Name: "calm", Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Values: model.Constant(0)},
	}}))
	bd, err = Breakdown(rec, "calm", chk, from, to)
	must(t, err)
	if l := line(bd, CauseExpense); !near(l.Projected, flat) {
		t.Errorf("zero inflation: got %v, want %v", l.Projected, flat)
	}
}

func TestBreakdownRejectsReversedInterval(t *testing.T) {
	if _, err := Breakdown(fixture(t), "", "all", d(2026, time.March, 1), d(2026, time.February, 1)); err == nil {
		t.Error("reversed interval accepted")
	}
}

func TestRuleMatchingNothingContributesZero(t *testing.T) {
	rec := fixture(t)
	none := model.RuleMember(model.Rule{Tax: []model.TaxTreatment{model.Tax529}})
	must(t, rec.AddAsset(&model.Asset{ID: "mixed", Members: []model.Member{model.AccountMember("chk"), none}}))
	must(t, rec.AddAsset(&model.Asset{ID: "alone", Members: []model.Member{none}}))
	date := d(2026, time.March, 31)
	if got, want := sample(t, rec, "", "mixed", date).Amount, sample(t, rec, "", model.DefaultAssetID("chk"), date).Amount; !near(got, want) {
		t.Errorf("mixed: got %v, want %v", got, want)
	}
	if got := sample(t, rec, "", "alone", date).Amount; !near(got, 0) {
		t.Errorf("alone: got %v, want 0", got)
	}
	from := d(2026, time.January, 1)
	mixed, err := Breakdown(rec, "", "mixed", from, date)
	must(t, err)
	chk, err := Breakdown(rec, "", model.DefaultAssetID("chk"), from, date)
	must(t, err)
	if !reflect.DeepEqual(mixed.Lines, chk.Lines) {
		t.Errorf("mixed lines %+v != chk lines %+v", mixed.Lines, chk.Lines)
	}
	if _, err := Breakdown(rec, "", "alone", from, date); err != nil {
		t.Fatal(err)
	}
}
