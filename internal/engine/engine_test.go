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

// fica is the payroll tax withheld from the fixture's salary: it is far below the standard deduction, so no income tax is.
const fica = 0.0765

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
	bare.Default.Intervals = []model.Interval{{From: model.MinDate, To: model.Ptr(d(2026, time.June, 30)), Value: 500}}
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
		{ID: "salary", Kind: model.ItemIncome, Amount: model.Constant(1000), Recurrence: monthly(15), Account: "chk"},
		{ID: "rent", Kind: model.ItemExpense, Amount: model.Constant(200), Recurrence: monthly(20), Account: "chk"},
		{ID: "save", Kind: model.ItemTransfer, Amount: model.Constant(300), Recurrence: monthly(16), From: "chk", To: "inv"},
		{ID: "late-deposit", Kind: model.ItemTransfer, Amount: model.Constant(100), Recurrence: monthly(1), From: "chk", To: "late"},
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

// rateOf returns the growth rate the engine settles on for one account on a line, on a date.
func rateOf(t *testing.T, rec *model.Record, proj string, line model.AssetID, acct model.AccountID, date model.Date) float64 {
	t.Helper()
	r, err := prepare(rec, proj, line, model.MinDate)
	must(t, err)
	v, ok := r.paths[acct].rate.At(date)
	if !ok {
		t.Fatalf("%s on %s: no rate on %v", acct, line, date)
	}
	return v
}

func growthMod(kind model.ObjectKind, id string, rate float64) model.Modification {
	return model.Modification{Target: model.ObjectRef{Kind: kind, ID: id}, Property: model.PropGrowth, Values: model.Constant(rate)}
}

func addProjection(t *testing.T, rec *model.Record, name, parent string, mods ...model.Modification) {
	t.Helper()
	must(t, rec.AddProjection(&model.Projection{Name: name, Parent: parent, Mods: mods}))
}

func TestGrowthSettingsApplyInOrder(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddAsset(&model.Asset{ID: "cash", Members: []model.Member{model.AccountMember("chk"), model.AccountMember("late")}}))
	addProjection(t, rec, "exception last", "", growthMod(model.KindAsset, "cash", 0.07), growthMod(model.KindAccount, "chk", 0.01))
	addProjection(t, rec, "exception first", "", growthMod(model.KindAccount, "chk", 0.01), growthMod(model.KindAsset, "cash", 0.07))
	date := d(2026, time.February, 1)
	for _, c := range []struct {
		proj     string
		chk, oth float64
	}{{"exception last", 0.01, 0.07}, {"exception first", 0.07, 0.07}} {
		if got := rateOf(t, rec, c.proj, "all", "chk", date); !near(got, c.chk) {
			t.Errorf("%s: chk got %v, want %v", c.proj, got, c.chk)
		}
		if got := rateOf(t, rec, c.proj, "all", "late", date); !near(got, c.oth) {
			t.Errorf("%s: late got %v, want %v", c.proj, got, c.oth)
		}
		if got := rateOf(t, rec, c.proj, "all", "inv", date); !near(got, 0.06) {
			t.Errorf("%s: inv outside both got %v, want its stated 0.06", c.proj, got)
		}
	}
}

func TestAssetDefinitionOrder(t *testing.T) {
	rec := fixture(t)
	growth := func(r float64) map[string]model.Schedule {
		return map[string]model.Schedule{model.PropGrowth: model.Constant(r)}
	}
	must(t, rec.AddAsset(&model.Asset{ID: "inner", Members: []model.Member{model.AccountMember("inv")}, Params: growth(0.02)}))
	must(t, rec.AddAsset(&model.Asset{ID: "outer", Members: []model.Member{model.AssetMember("inner"), model.AccountMember("late")}, Params: growth(0.03)}))
	addProjection(t, rec, "proj", "", growthMod(model.KindAsset, "inner", 0.05))
	date := d(2026, time.February, 1)
	for _, c := range []struct {
		proj string
		line model.AssetID
		want float64
	}{
		{"", "outer", 0.03},     // the outer asset's Params apply after the inner's
		{"", "inner", 0.02},     // outer is not on this line
		{"proj", "outer", 0.05}, // projection settings overwrite asset definitions
		{"", "all", 0.06},       // neither asset is on this line, so the stated rate stays
	} {
		if got := rateOf(t, rec, c.proj, c.line, "inv", date); !near(got, c.want) {
			t.Errorf("proj %q line %s: got %v, want %v", c.proj, c.line, got, c.want)
		}
	}
	// The account's own default asset is innermost, so an outer asset still overwrites it.
	rec.Asset(model.DefaultAssetID("inv")).Params = growth(0.09)
	if got := rateOf(t, rec, "", "outer", "inv", date); !near(got, 0.03) {
		t.Errorf("default asset params: got %v, want 0.03", got)
	}
	if got := rateOf(t, rec, "", model.DefaultAssetID("inv"), "inv", date); !near(got, 0.09) {
		t.Errorf("default asset params alone: got %v, want 0.09", got)
	}
}

func TestParentChainAppliesBeforeChild(t *testing.T) {
	rec := fixture(t)
	addProjection(t, rec, "parent", "", growthMod(model.KindAccount, "inv", 0.02), growthMod(model.KindAsset, "all", 0.09))
	addProjection(t, rec, "child", "parent", growthMod(model.KindAccount, "inv", 0.01))
	date := d(2026, time.February, 1)
	if got := rateOf(t, rec, "parent", "all", "inv", date); !near(got, 0.09) {
		t.Errorf("parent alone: got %v, want 0.09", got)
	}
	if got := rateOf(t, rec, "child", "all", "inv", date); !near(got, 0.01) {
		t.Errorf("child over a later, broader parent setting: got %v, want 0.01", got)
	}
	if got := rateOf(t, rec, "child", "all", "late", date); !near(got, 0.09) {
		t.Errorf("child leaves the parent's setting elsewhere: got %v, want 0.09", got)
	}
}

func TestLaterScheduleOverwritesOnlyFromItsStart(t *testing.T) {
	rec := fixture(t)
	from := d(2027, time.January, 1)
	must(t, rec.AddProjection(&model.Projection{Name: "later", Mods: []model.Modification{{
		Target: model.ObjectRef{Kind: model.KindAccount, ID: "inv"}, Property: model.PropGrowth,
		Values: model.MustSchedule(model.Segment{From: from, Value: 0.04}, model.Segment{From: from.Add(365), Value: 0.05}),
	}}}))
	for _, c := range []struct {
		date model.Date
		want float64
	}{{from.Add(-1), 0.06}, {from, 0.04}, {from.Add(365), 0.05}} {
		if got := rateOf(t, rec, "later", "all", "inv", c.date); !near(got, c.want) {
			t.Errorf("%v: got %v, want %v", c.date, got, c.want)
		}
	}
}

func TestProjectionSettingReachesAccountOnItsOwnLine(t *testing.T) {
	rec := fixture(t)
	addProjection(t, rec, "wide", "", growthMod(model.KindAsset, "all", 0.07))
	// The line's asset does not contain the setting's target.
	if got := rateOf(t, rec, "wide", model.DefaultAssetID("chk"), "chk", d(2026, time.February, 1)); !near(got, 0.07) {
		t.Errorf("got %v, want 0.07", got)
	}
}

func TestAssetDefinitionDoesNotApplyOutsideItsAsset(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddAsset(&model.Asset{ID: "wrap", Members: []model.Member{model.AccountMember("inv")},
		Params: map[string]model.Schedule{model.PropGrowth: model.Constant(0.10)}}))
	date := d(2026, time.February, 1)
	if got := rateOf(t, rec, "", "pair", "inv", date); !near(got, 0.06) {
		t.Errorf("line outside wrap: got %v, want 0.06", got)
	}
	if got := rateOf(t, rec, "", "wrap", "inv", date); !near(got, 0.10) {
		t.Errorf("line wrap: got %v, want 0.10", got)
	}
}

func TestPerAssetStartAndOverrideFollowOrder(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddAsset(&model.Asset{ID: "ret", Members: []model.Member{model.AccountMember("chk"), model.AccountMember("inv")}}))
	must(t, rec.AddAsset(&model.Asset{ID: "tsp", Members: []model.Member{model.AccountMember("inv")}}))
	y2024, y2020 := d(2024, time.January, 1), d(2020, time.January, 1)
	setting := func(asset model.AssetID, start model.Date, ov bool) model.AssetSetting {
		return model.AssetSetting{Asset: asset, Start: model.Ptr(start), OverrideHistory: model.Ptr(ov)}
	}
	for name, settings := range map[string][]model.AssetSetting{
		"narrow last": {setting("ret", y2024, false), setting("tsp", y2020, true)},
		"broad last":  {setting("tsp", y2020, true), setting("ret", y2024, false)},
	} {
		must(t, rec.AddProjection(&model.Projection{Name: name, AssetSettings: settings}))
	}
	for _, c := range []struct {
		proj     string
		invStart model.Date
		invOv    bool
	}{{"narrow last", y2020, true}, {"broad last", y2024, false}} {
		starts, err := Starts(rec, c.proj, "ret")
		must(t, err)
		for _, s := range starts {
			switch s.Account {
			case "inv":
				if s.ProjectionStart != c.invStart || s.OverrideHistory != c.invOv {
					t.Errorf("%s: inv got %+v, want start %v override %v", c.proj, s, c.invStart, c.invOv)
				}
			case "chk":
				if s.ProjectionStart != y2024 || s.OverrideHistory {
					t.Errorf("%s: chk got %+v", c.proj, s)
				}
			}
		}
	}
}

func TestOverrideHistoryIgnoresLaterRealData(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddProjection(&model.Projection{Name: "back", Start: model.Ptr(d(2025, time.December, 10)), OverrideHistory: model.Ptr(true)}))
	// Real data says 1000 on 2026-01-01. Starting at 12-10 replays 12-15 salary +923.50 net of payroll tax, 16th -300, 20th -200, 1st -100.
	got := sample(t, rec, "back", model.DefaultAssetID("chk"), d(2026, time.January, 1))
	if got.Status != StatusProjected || !near(got.Amount, 1000+1000*(1-fica)-600) {
		t.Errorf("overridden: got %+v, want %v projected", got, 1000+1000*(1-fica)-600)
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
	// From 2026-01-01: three salaries (net of payroll tax), rents and saves, and two late deposits.
	want := 1000.0 + 3*(1000*(1-fica)-200-300) - 2*100
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
	// 2025-12-20: chk and inv are real, late ran out on 10-31 and is projected from there, but its deposits
	// from chk count only after chk's data ends on 2026-01-01, so late stays at 2000; bare is on defaults.
	got := sample(t, rec, "", all, d(2025, time.December, 20))
	if got.Status != StatusPartlyProjected || !near(got.Amount, 1000+10300+2000+500) {
		t.Errorf("partly projected: got %+v, want %v", got, 1000+10300+2000+500)
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
	if l := line(bd, CauseTax); !near(l.Projected, -2000*fica) || l.Actual != 0 {
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
	acct.Default.Intervals = append(acct.Default.Intervals, model.Interval{From: d(2025, time.December, 20), To: model.Ptr(d(2026, time.January, 31)), Value: 800})
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
		Values:   model.MustSchedule(model.Segment{From: d(2026, time.February, 1), Value: 1300}),
	}
	must(t, rec.AddProjection(&model.Projection{Name: "raise", Mods: []model.Modification{raise}}))
	must(t, rec.AddProjection(&model.Projection{Name: "broken", Parent: "raise", Mods: []model.Modification{
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "gone"}, Property: model.PropAmount, Values: model.Constant(1)},
		{Target: model.ObjectRef{Kind: model.KindAccount, ID: "gone"}, Property: model.PropGrowth, Values: model.Constant(1)},
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "rent"}, Property: "colour"},
		{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "salary"}, Property: model.PropAddItem, Item: &model.BudgetItem{ID: "salary"}},
	}}))
	base := sample(t, rec, "", chk, d(2026, time.March, 31)).Amount
	// Salary Feb 15 and Mar 15 are 1300 gross: each pays 300 more, net of payroll tax; the year stays under the deductions.
	if got := sample(t, rec, "raise", chk, d(2026, time.March, 31)).Amount; !near(got, base+2*300*(1-fica)) {
		t.Errorf("raise: got %v, want %v", got, base+2*300*(1-fica))
	}
	res, err := Balances(rec, "broken", chk, []model.Date{d(2026, time.March, 31)})
	must(t, err)
	if len(res.Skipped) != 4 || res.Skipped[0].Reason != "budget item not found" || res.Skipped[3].Index != 3 || res.Skipped[3].Projection != "broken" {
		t.Errorf("skipped: got %+v", res.Skipped)
	}
	if !near(res.Samples[0].Amount, base+2*300*(1-fica)) {
		t.Errorf("a skipped modification must not stop the others: got %v", res.Samples[0].Amount)
	}
	if _, err := Balances(rec, "missing", chk, nil); err == nil {
		t.Error("unknown projection accepted")
	}
}

func TestWithdrawalIsTaxedAtReconciliation(t *testing.T) {
	rec := fixture(t)
	w := model.BudgetItem{
		ID: "out", Kind: model.ItemTransfer, Amount: model.Constant(1000), From: "inv", To: "chk",
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
	if l := line(bd, CauseTax); l != (Line{}) {
		t.Errorf("a withdrawal is not taxed where it happens: got %+v", l)
	}
	// Seen from the pair, the money only moves inside it, so only growth differs.
	a, b := sample(t, rec, "", "pair", to).Amount, sample(t, rec, "w", "pair", to).Amount
	if lost := a - b; lost < 0 || lost > 10 {
		t.Errorf("lost to growth: got %v", lost)
	}
	// The year's income stays under the standard deduction, so nothing is settled in April: the tax that month is the 15 April paycheck's.
	bd, err = Breakdown(rec, "w", model.DefaultAssetID("chk"), d(2027, time.April, 1), d(2027, time.April, 30))
	must(t, err)
	if l := line(bd, CauseTax); !near(l.Projected, -1000*fica) {
		t.Errorf("reconciliation: got %+v", l)
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
	// The projection layer beats the wrapper's own Params.
	low := sample(t, rec, "low", "wrap", date).Amount
	if !(low < stated) {
		t.Errorf("projection growth 0: %v should be below %v", low, stated)
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
			rec.Asset("dflt").Default.Intervals = []model.Interval{{From: model.MinDate, To: model.Ptr(d(2026, time.June, 30)), Value: 123.45}}
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

func TestEndedGrowthSettingsOverlapInEitherOrder(t *testing.T) {
	rec := fixture(t)
	ended := model.Modification{
		Target: model.ObjectRef{Kind: model.KindAccount, ID: "inv"}, Property: model.PropGrowth,
		Values: model.MustSchedule(model.Segment{From: d(2026, time.March, 1), End: model.Ptr(d(2026, time.May, 31)), Value: 0.01}),
	}
	open := growthMod(model.KindAccount, "inv", 0.07)
	addProjection(t, rec, "ended last", "", open, ended)
	addProjection(t, rec, "ended first", "", ended, open)
	for _, c := range []struct {
		proj string
		date model.Date
		want float64
	}{
		{"ended last", d(2026, time.February, 1), 0.07},
		{"ended last", d(2026, time.April, 1), 0.01},
		{"ended last", d(2026, time.June, 1), 0.07},
		{"ended first", d(2026, time.February, 1), 0.07},
		{"ended first", d(2026, time.April, 1), 0.07}, // the later open-ended setting covers every date
		{"ended first", d(2026, time.June, 1), 0.07},
	} {
		if got := rateOf(t, rec, c.proj, "all", "inv", c.date); !near(got, c.want) {
			t.Errorf("%s on %v: got %v, want %v", c.proj, c.date, got, c.want)
		}
	}
}

func TestEndedBudgetAmountChange(t *testing.T) {
	rec := fixture(t)
	chk := model.DefaultAssetID("chk")
	from, to := d(2026, time.January, 1), d(2026, time.March, 31)
	bd, err := Breakdown(rec, "", chk, from, to)
	must(t, err)
	if l := line(bd, CauseExpense); !near(l.Projected, -600) {
		t.Fatalf("base rent: got %v, want -600", l.Projected)
	}
	// Rent is cut to 100 for February only; January and March pay the base 200.
	addProjection(t, rec, "cut", "", model.Modification{
		Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "rent"}, Property: model.PropAmount,
		Values: model.MustSchedule(model.Segment{From: d(2026, time.February, 1), End: model.Ptr(d(2026, time.February, 28)), Value: 100}),
	})
	bd, err = Breakdown(rec, "cut", chk, from, to)
	must(t, err)
	if l := line(bd, CauseExpense); !near(l.Projected, -500) {
		t.Errorf("cut rent: got %v, want -500", l.Projected)
	}
	// The unbounded rent item runs to the end of a long horizon.
	bd, err = Breakdown(rec, "", chk, from, d(2027, time.January, 31))
	must(t, err)
	if l := line(bd, CauseExpense); !near(l.Projected, -2600) {
		t.Errorf("open-ended rent: got %v, want -2600", l.Projected)
	}
}

func TestEndedInflationChange(t *testing.T) {
	rec := fixture(t)
	rec.Globals[model.PropInflation] = model.Constant(0.10)
	rec.Budget.Items[1].Indexed = true // rent
	pause := d(2026, time.July, 1)
	addProjection(t, rec, "pause", "", model.Modification{
		Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation,
		Values: model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(pause.Add(-1)), Value: 0}),
	})
	bd, err := Breakdown(rec, "pause", model.DefaultAssetID("chk"), d(2026, time.January, 1), d(2027, time.January, 31))
	must(t, err)
	// No inflation accrues before July; from then on the base 10% applies.
	want := 0.0
	for i := 0; i < 13; i++ {
		date := d(2026, time.January+time.Month(i), 20)
		f := 1.0
		if date > pause {
			f = math.Pow(1.10, float64(date-pause)/model.DaysPerYear)
		}
		want -= 200 * f
	}
	if l := line(bd, CauseExpense); !near(l.Projected, want) {
		t.Errorf("got %v, want %v", l.Projected, want)
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

func TestClosedAccountReadsZeroAfterItsCloseDate(t *testing.T) {
	rec := fixture(t)
	rec.Account("inv").Closed = model.Ptr(d(2026, time.February, 15))
	inv := model.DefaultAssetID("inv")
	for _, proj := range []string{"", "p"} {
		if proj != "" {
			addProjection(t, rec, proj, "", growthMod(model.KindAccount, "inv", 0.1))
		}
		on, after := d(2026, time.February, 15), d(2026, time.February, 16)
		if got := sample(t, rec, proj, inv, on); got.Amount <= 0 {
			t.Errorf("%q: on the close date: got %+v, want the balance", proj, got)
		}
		for _, asset := range []model.AssetID{inv, "pair"} {
			for _, date := range []model.Date{after, d(2030, time.January, 1)} {
				want := 0.0
				if asset == "pair" {
					want = sample(t, rec, proj, model.DefaultAssetID("chk"), date).Amount
				}
				if got := sample(t, rec, proj, asset, date); !near(got.Amount, want) {
					t.Errorf("%q: %s on %v: got %v, want %v", proj, asset, date, got.Amount, want)
				}
			}
		}
	}
	if rec.Account("inv").ClosedOn(d(2026, time.February, 15)) {
		t.Error("closed on its close date")
	}
}

func TestItemStopsAfterAClosedAccountsCloseDate(t *testing.T) {
	rec := fixture(t)
	chk := model.DefaultAssetID("chk")
	later := d(2026, time.June, 30)
	for _, proj := range []string{"", "p"} {
		if proj != "" {
			addProjection(t, rec, proj, "", growthMod(model.KindAccount, "inv", 0.1))
		}
		before := sample(t, rec, proj, chk, later).Amount
		rec.Account("inv").Closed = model.Ptr(d(2026, time.February, 1))
		after := sample(t, rec, proj, chk, later).Amount
		rec.Account("inv").Closed = nil
		// save moves 300 on the 16th of each month; the ones after February 1 (February to June) stop.
		if want := before + 5*300; !near(after, want) {
			t.Errorf("%q: chk on %v: got %v, want %v", proj, later, after, want)
		}
	}
}

// TestTransferCountedOnBothSides checks that a transfer between two accounts whose data ends on different dates is projected on both sides or on neither, in either direction and whichever side ends first.
func TestTransferCountedOnBothSides(t *testing.T) {
	for _, c := range []struct {
		name       string
		from, to   model.AccountID
		aEnd, bEnd model.Date
	}{
		{"a to b, a ends first", "a", "b", d(2026, time.January, 10), d(2026, time.March, 10)},
		{"a to b, b ends first", "a", "b", d(2026, time.March, 10), d(2026, time.January, 10)},
		{"b to a, a ends first", "b", "a", d(2026, time.January, 10), d(2026, time.March, 10)},
		{"b to a, b ends first", "b", "a", d(2026, time.March, 10), d(2026, time.January, 10)},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := model.NewRecord(d(2026, time.January, 1))
			a := &model.Account{ID: "a", Type: model.TypeChecking, Tax: model.Taxable}
			a.AddSnapshot(d(2025, time.December, 1), 5000)
			a.AddSnapshot(c.aEnd, 5000)
			b := &model.Account{ID: "b", Type: model.TypeSavings, Tax: model.Taxable}
			b.AddSnapshot(d(2025, time.December, 1), 3000)
			b.AddSnapshot(c.bEnd, 3000)
			must(t, rec.AddAccount(a))
			must(t, rec.AddAccount(b))
			must(t, rec.AddAsset(&model.Asset{ID: "both", Members: []model.Member{model.AccountMember("a"), model.AccountMember("b")}}))
			rec.Budget.Items = []model.BudgetItem{
				{ID: "move", Kind: model.ItemTransfer, Amount: model.Constant(70), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, From: c.from, To: c.to},
			}
			bd, err := Breakdown(rec, "", "both", d(2025, time.December, 15), d(2026, time.May, 1))
			must(t, err)
			if l := line(bd, CauseUnexplained); !near(l.Actual+l.Projected, 0) {
				t.Errorf("unexplained %v, want 0", l)
			}
			if !near(bd.Total(), bd.End-bd.Start) {
				t.Errorf("total %v, change %v", bd.Total(), bd.End-bd.Start)
			}
			if !near(bd.End, bd.Start) {
				t.Errorf("a transfer inside the target moved the total from %v to %v", bd.Start, bd.End)
			}
		})
	}
}

// TestRecordedTransferCountedOnBothSides checks that a transfer the later-ending account recorded in the gap after the earlier one's data ends also lands on the earlier account, in either direction.
func TestRecordedTransferCountedOnBothSides(t *testing.T) {
	for _, c := range []struct {
		name       string
		laterIsSrc bool
	}{
		{"later account is the source", true},
		{"later account is the destination", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := model.NewRecord(d(2026, time.January, 1))
			later := &model.Account{ID: "later", Type: model.TypeChecking, Tax: model.Taxable}
			early := &model.Account{ID: "early", Type: model.TypeSavings, Tax: model.Taxable}
			sign := 1.0
			if c.laterIsSrc {
				sign = -1
			}
			later.AddSnapshot(d(2025, time.December, 1), 5000)
			later.AddSnapshot(d(2026, time.March, 10), 5000+2*70*sign)
			early.AddSnapshot(d(2025, time.December, 1), 3000)
			early.AddSnapshot(d(2026, time.January, 10), 3000)
			for _, day := range []model.Date{d(2026, time.February, 1), d(2026, time.March, 1)} {
				must(t, later.AddTransaction(model.Transaction{Date: day, Amount: 70 * sign, Kind: model.TxTransfer, Counter: "early"}))
			}
			must(t, rec.AddAccount(later))
			must(t, rec.AddAccount(early))
			must(t, rec.AddAsset(&model.Asset{ID: "both", Members: []model.Member{model.AccountMember("later"), model.AccountMember("early")}}))
			from, to := model.AccountID("later"), model.AccountID("early")
			if !c.laterIsSrc {
				from, to = to, from
			}
			rec.Budget.Items = []model.BudgetItem{
				{ID: "move", Kind: model.ItemTransfer, Amount: model.Constant(70), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, From: from, To: to},
			}
			for _, date := range []model.Date{d(2025, time.December, 15), d(2026, time.February, 15), d(2026, time.March, 20), d(2026, time.May, 1)} {
				if got := sample(t, rec, "", "both", date); !near(got.Amount, 8000) {
					t.Errorf("aggregate on %v is %v, want 8000", date, got.Amount)
				}
			}
			bd, err := Breakdown(rec, "", "both", d(2025, time.December, 15), d(2026, time.May, 1))
			must(t, err)
			if l := line(bd, CauseUnexplained); !near(l.Actual+l.Projected, 0) {
				t.Errorf("unexplained %v, want 0", l)
			}
			if !near(bd.Total(), bd.End-bd.Start) {
				t.Errorf("total %v, change %v", bd.Total(), bd.End-bd.Start)
			}
			// The early account alone receives or pays the recorded transfers.
			single := sample(t, rec, "", model.DefaultAssetID("early"), d(2026, time.March, 20))
			if want := 3000 - 2*70*sign; !near(single.Amount, want) {
				t.Errorf("early on 2026-03-20 is %v, want %v", single.Amount, want)
			}
		})
	}
}
