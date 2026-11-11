// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

const rates = 0.22 + 0.0575 + 0.0765

var (
	rsuAsset = model.DefaultAssetID("rsu")
	brkAsset = model.DefaultAssetID("brk")
)

// rsuRecord is an equity account "rsu" with one grant of 400 shares dated 2025-06-01, vesting 50% in each of two years from 2026-03-15 on the 15th of March, June, September and December (50 shares a vest), the stock ACME at 100 on the grant date and 120 on 2025-12-31, and a brokerage account "brk" that the vests pay into, holding 1000. Today is 2026-01-01. Asset "both" holds the two accounts.
func rsuRecord(t *testing.T) *model.Record {
	t.Helper()
	rec := model.NewRecord(d(2026, time.January, 1))
	cal := &model.VestCalendar{Name: "cal"}
	for _, m := range []time.Month{time.March, time.June, time.September, time.December} {
		cal.Rules = append(cal.Rules, model.VestRule{Month: m, Day: 15})
	}
	rec.Calendars = append(rec.Calendars, cal)
	rec.Prices.Add(model.PriceObservation{ID: 1, Symbol: "ACME", Date: d(2025, time.June, 1), Price: 100})
	rec.Prices.Add(model.PriceObservation{ID: 2, Symbol: "ACME", Date: d(2025, time.December, 31), Price: 120})
	rsu := &model.Account{ID: "rsu", Name: "RSU", Type: model.TypeEquity, Tax: model.Taxable, StatedRate: 0.05,
		Grants: []model.Grant{rsuGrant(t, cal, 1, "ACME", d(2025, time.June, 1), 400, 100, d(2026, time.March, 15), []float64{50, 50})}}
	brk := &model.Account{ID: "brk", Name: "Brokerage", Type: model.TypeBrokerage, Tax: model.Taxable}
	brk.AddSnapshot(d(2025, time.December, 1), 1000)
	brk.AddSnapshot(d(2026, time.January, 1), 1000)
	must(t, rec.AddAccount(rsu))
	must(t, rec.AddAccount(brk))
	must(t, rec.AddAsset(&model.Asset{ID: "both", Members: []model.Member{model.AccountMember("rsu"), model.AccountMember("brk")}}))
	return rec
}

func rsuGrant(t *testing.T, cal *model.VestCalendar, id int64, symbol string, date model.Date, shares int, price float64, first model.Date, years []float64) model.Grant {
	t.Helper()
	pct, err := model.GeneratePercents(cal, first, years, 0)
	must(t, err)
	return model.Grant{ID: id, Account: "rsu", Symbol: symbol, Date: date, Shares: shares, Price: price, Calendar: cal.Name, FirstVest: first,
		YearPercents: years, VestPercents: pct, Rates: model.DefaultWithholding, Brokerage: "brk"}
}

func value(t *testing.T, rec *model.Record, proj string, asset model.AssetID, date model.Date) float64 {
	t.Helper()
	return sample(t, rec, proj, asset, date).Amount
}

// priceGrown is 120 grown at the annual rate over the days from 2025-12-31 to date.
func priceGrown(rate float64, date model.Date) float64 {
	return 120 * math.Pow(1+rate, float64(date-d(2025, time.December, 31))/model.DaysPerYear)
}

func TestEquityStepsDownAtVestsAndBrokerageUp(t *testing.T) {
	rec := rsuRecord(t)
	addProjection(t, rec, "base", "")
	net := 6000 * (1 - rates)
	for _, c := range []struct {
		date       model.Date
		eq, brk    float64
		vestsSoFar int
	}{
		{d(2026, time.January, 1), 48000, 1000, 0},
		{d(2026, time.March, 14), 48000, 1000, 0},
		{d(2026, time.March, 15), 42000, 1000 + net, 1},
		{d(2026, time.June, 14), 42000, 1000 + net, 1},
		{d(2026, time.June, 15), 36000, 1000 + 2*net, 2},
		{d(2027, time.December, 15), 0, 1000 + 8*net, 8},
		{d(2028, time.June, 1), 0, 1000 + 8*net, 8},
	} {
		if got := value(t, rec, "base", rsuAsset, c.date); !near(got, c.eq) {
			t.Errorf("%v: equity %v, want %v", c.date, got, c.eq)
		}
		if got := value(t, rec, "base", brkAsset, c.date); !near(got, c.brk) {
			t.Errorf("%v: brokerage %v, want %v", c.date, got, c.brk)
		}
	}
	// The stated rate of the equity account does not apply; the two together lose only the withholding.
	got := value(t, rec, "base", "both", d(2026, time.March, 15))
	if want := 42000 + 1000 + net; !near(got, want) {
		t.Errorf("both on the first vest: %v, want %v", got, want)
	}
}

func TestStockGrowthRaisesEquityAndBrokerage(t *testing.T) {
	rec := rsuRecord(t)
	addProjection(t, rec, "base", "")
	addProjection(t, rec, "up", "base", growthMod(model.KindStock, "ACME", 0.15))
	date := d(2026, time.March, 15)
	px := priceGrown(0.15, date)
	if got, want := value(t, rec, "up", rsuAsset, date), 350*px; !near(got, want) {
		t.Errorf("equity %v, want %v", got, want)
	}
	if got, want := value(t, rec, "up", brkAsset, date), 1000+50*px*(1-rates); !near(got, want) {
		t.Errorf("brokerage %v, want %v", got, want)
	}
	if up, base := value(t, rec, "up", brkAsset, date), value(t, rec, "base", brkAsset, date); !(up > base) {
		t.Errorf("brokerage under +15%% %v, base %v", up, base)
	}
	// Between vests the unvested shares follow the price.
	mid := d(2026, time.May, 1)
	if got, want := value(t, rec, "up", rsuAsset, mid), 350*priceGrown(0.15, mid); !near(got, want) {
		t.Errorf("equity between vests %v, want %v", got, want)
	}
	// Reality and the base projection do not see the modification.
	if got := value(t, rec, "", rsuAsset, date); !near(got, 42000) {
		t.Errorf("reality %v", got)
	}
}

func TestStockCrashLowersFromItsDate(t *testing.T) {
	rec := rsuRecord(t)
	addProjection(t, rec, "base", "")
	crash := model.Modification{Target: model.ObjectRef{Kind: model.KindStock, ID: "ACME"}, Property: model.PropGrowth,
		Values: model.MustSchedule(model.Segment{From: d(2026, time.June, 1), End: model.Ptr(d(2027, time.May, 31)), Value: -0.40})}
	addProjection(t, rec, "crash", "base", crash)
	// Before the segment nothing differs.
	before := d(2026, time.May, 31)
	if a, b := value(t, rec, "crash", rsuAsset, before), value(t, rec, "base", rsuAsset, before); !near(a, b) {
		t.Errorf("before the crash %v vs %v", a, b)
	}
	// A year of -40% leaves 60% of the price.
	end := d(2027, time.June, 1)
	px := 120 * math.Pow(0.6, float64(end-d(2026, time.June, 1))/model.DaysPerYear)
	// Five vests are done by 2027-06-01 (the 2027-03-15 one included), 150 shares are left.
	if got, want := value(t, rec, "crash", rsuAsset, end), 150*px; !near(got, want) {
		t.Errorf("equity after the crash %v, want %v", got, want)
	}
	if !(value(t, rec, "crash", brkAsset, end) < value(t, rec, "base", brkAsset, end)) {
		t.Error("brokerage is not lower under the crash")
	}
}

func TestFocalGrantEveryMarch(t *testing.T) {
	rec := rsuRecord(t)
	addProjection(t, rec, "base", "")
	focal := model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: "rsu"}, Property: model.PropAddGrant, Grant: &model.FocalGrant{
		Symbol: "ACME", Value: 40000, Date: d(2027, time.March, 1), Yearly: true, Calendar: "cal", YearPercents: []float64{40, 30, 20, 10},
		FirstVestMonths: 1, Brokerage: "brk"}}
	addProjection(t, rec, "grants", "base", focal)
	// 40000 / 120 = 333 shares; the first vest is the first calendar date at least a month after the grant, 2027-06-15, and each year's percent is split over four vests.
	grant := 333 * 120.0
	if a, b := value(t, rec, "grants", rsuAsset, d(2027, time.February, 28)), value(t, rec, "base", rsuAsset, d(2027, time.February, 28)); !near(a, b) {
		t.Errorf("before the first grant %v vs %v", a, b)
	}
	if got, want := value(t, rec, "grants", rsuAsset, d(2027, time.March, 1))-value(t, rec, "base", rsuAsset, d(2027, time.March, 1)), grant; !near(got, want) {
		t.Errorf("first grant adds %v, want %v", got, want)
	}
	// Year one is 40%: 4 vests of 10% of 333, floored to 33 shares.
	after := d(2027, time.June, 15)
	if got, want := value(t, rec, "grants", rsuAsset, after)-value(t, rec, "base", rsuAsset, after), grant-33*120; !near(got, want) {
		t.Errorf("after the first focal vest %v, want %v", got, want)
	}
	if got, want := value(t, rec, "grants", brkAsset, after)-value(t, rec, "base", brkAsset, after), 33*120*(1-rates); !near(got, want) {
		t.Errorf("brokerage after the first focal vest %v, want %v", got, want)
	}
	// The 2028 grant (333 shares at a flat 120) arrives on its date.
	d28 := d(2028, time.March, 1)
	if got, want := value(t, rec, "grants", rsuAsset, d28)-value(t, rec, "grants", rsuAsset, d28.Add(-1)), grant; !near(got, want) {
		t.Errorf("second grant adds %v, want %v", got, want)
	}
	// By 2028-06-01 the 2027 grant has vested four times 33 shares, and the 2028 grant has not vested yet.
	if got, want := value(t, rec, "grants", rsuAsset, d(2028, time.June, 1)), (333-4*33+333)*120.0; !near(got, want) {
		t.Errorf("equity in mid 2028 %v, want %v", got, want)
	}
}

func TestEquityBreakdown(t *testing.T) {
	rec := rsuRecord(t)
	cal := rec.Calendar("cal")
	rec.Account("rsu").Grants = append(rec.Account("rsu").Grants,
		rsuGrant(t, cal, 2, "ACME", d(2025, time.December, 10), 100, 115, d(2026, time.June, 15), []float64{100}))
	rec.Prices.Add(model.PriceObservation{ID: 3, Symbol: "ACME", Date: d(2025, time.December, 10), Price: 115})
	addProjection(t, rec, "base", "")
	from, to := d(2025, time.December, 5), d(2026, time.June, 30)
	b, err := Breakdown(rec, "base", rsuAsset, from, to)
	must(t, err)
	if got := b.Total(); !near(got, b.End-b.Start) {
		t.Errorf("total %v, want %v", got, b.End-b.Start)
	}
	// 125 shares vest by the end of June at 120: 15000, of which 35.4% is withheld.
	v := 125 * 120.0
	for _, c := range []struct {
		cause             Cause
		actual, projected float64
	}{
		{CauseGrant, 100 * 115, 0},
		{CauseGrowth, 400*(120-100) + 100*(120-115), 0},
		{CauseVest, 0, -v * (1 - rates)},
		{CauseTax, 0, -v * rates},
	} {
		if l := line(b, c.cause); !near(l.Actual, c.actual) || !near(l.Projected, c.projected) {
			t.Errorf("%s: got %+v, want actual %v projected %v", c.cause, l, c.actual, c.projected)
		}
	}
	if l := line(b, CauseUnexplained); l.Actual != 0 || l.Projected != 0 {
		t.Errorf("unexplained %+v", l)
	}
	// With the account the proceeds go to, the deposit nets out against the vest.
	b, err = Breakdown(rec, "base", "both", from, to)
	must(t, err)
	if got := b.Total(); !near(got, b.End-b.Start) {
		t.Errorf("both: total %v, want %v", got, b.End-b.Start)
	}
	for _, c := range []Cause{CauseVest, CauseTransfer, CauseUnexplained, CauseDefault} {
		if l := line(b, c); math.Abs(l.Actual)+math.Abs(l.Projected) > 1e-6 {
			t.Errorf("both: %s %+v", c, l)
		}
	}
	if l := line(b, CauseTax); !near(l.Projected, -v*rates) {
		t.Errorf("both: tax %+v", l)
	}
}

func TestRecordedVestsAreActualAndNetOutOfTheDeposit(t *testing.T) {
	rec := rsuRecord(t)
	rsu, brk := rec.Account("rsu"), rec.Account("brk")
	// The first planned vest happened on 2025-09-15 under a calendar that has it.
	rsu.Grants[0].FirstVest = d(2025, time.September, 15)
	pct, err := model.GeneratePercents(rec.Calendar("cal"), rsu.Grants[0].FirstVest, rsu.Grants[0].YearPercents, 0)
	must(t, err)
	rsu.Grants[0].VestPercents = pct
	rec.Prices.Add(model.PriceObservation{ID: 3, Symbol: "ACME", Date: d(2025, time.September, 15), Price: 110})
	v := model.RecordedVest{ID: 4, Grant: 1, Planned: 1, Date: d(2025, time.September, 15), Shares: 50, Price: 110, Rates: model.DefaultWithholding, TxEntry: 9}
	rsu.Vests = append(rsu.Vests, v)
	brk.Snapshots = nil
	brk.AddSnapshot(d(2025, time.August, 1), 1000)
	brk.AddSnapshot(d(2025, time.December, 31), 1000+v.Net())
	must(t, brk.AddTransaction(model.Transaction{Date: v.Date, Amount: v.Net(), Kind: model.TxTransfer, Counter: "rsu"}))
	addProjection(t, rec, "base", "")

	from, to := d(2025, time.August, 1), d(2025, time.December, 31)
	b, err := Breakdown(rec, "base", rsuAsset, from, to)
	must(t, err)
	if l := line(b, CauseVest); !near(l.Actual, -v.Net()) {
		t.Errorf("vest %+v, want %v", l, -v.Net())
	}
	if l := line(b, CauseTax); !near(l.Actual, -v.WithheldAmount()) {
		t.Errorf("tax %+v, want %v", l, -v.WithheldAmount())
	}
	// Price moves of the unvested shares: 400 shares at 100 to 110 on the vest, then 350 shares to 120.
	if l := line(b, CauseGrowth); !near(l.Actual, 400*10+350*10) {
		t.Errorf("growth %+v", l)
	}
	if l := line(b, CauseUnexplained); math.Abs(l.Actual) > 1e-6 {
		t.Errorf("unexplained %+v", l)
	}
	if !near(b.Total(), b.End-b.Start) {
		t.Errorf("total %v, want %v", b.Total(), b.End-b.Start)
	}
	b, err = Breakdown(rec, "base", "both", from, to)
	must(t, err)
	for _, c := range []Cause{CauseVest, CauseTransfer, CauseUnexplained} {
		if l := line(b, c); math.Abs(l.Actual) > 1e-6 {
			t.Errorf("both: %s %+v", c, l)
		}
	}
	// The recorded vest is not projected again: seven vests remain, not eight.
	if got, want := value(t, rec, "base", rsuAsset, d(2027, time.December, 15)), 0.0; !near(got, want) {
		t.Errorf("equity after every vest %v", got)
	}
	end := value(t, rec, "base", brkAsset, d(2027, time.December, 15))
	if want := 1000 + v.Net() + 350*120*(1-rates); !near(end, want) {
		t.Errorf("brokerage at the end %v, want %v", end, want)
	}
}

func TestOverdueVestsHappenTheDayAfterStart(t *testing.T) {
	rec := rsuRecord(t)
	rsu := rec.Account("rsu")
	rsu.Grants[0] = rsuGrant(t, rec.Calendar("cal"), 1, "ACME", d(2025, time.June, 1), 200, 100, d(2025, time.March, 15), []float64{100})
	rsu.Vests = append(rsu.Vests, model.RecordedVest{ID: 3, Grant: 1, Planned: 1, Date: d(2025, time.March, 15), Shares: 50, Price: 100, Rates: model.DefaultWithholding})
	brk := rec.Account("brk")
	brk.Snapshots = nil
	brk.AddSnapshot(d(2025, time.November, 1), 1000)
	brk.AddSnapshot(d(2025, time.December, 15), 1000)
	addProjection(t, rec, "base", "")
	// Planned vests 2 to 4 (2025-06-15 to 2025-12-15) are before the start, 2025-12-31, and not recorded.
	if got := value(t, rec, "base", rsuAsset, d(2025, time.December, 31)); !near(got, 150*120) {
		t.Errorf("actual on the start %v", got)
	}
	if got := value(t, rec, "base", rsuAsset, d(2026, time.January, 1)); !near(got, 0) {
		t.Errorf("after the overdue vests %v", got)
	}
	if got, want := value(t, rec, "base", brkAsset, d(2026, time.January, 1)), 1000+150*120*(1-rates); !near(got, want) {
		t.Errorf("brokerage after the overdue vests %v, want %v", got, want)
	}
	if got := value(t, rec, "base", brkAsset, d(2025, time.December, 31)); !near(got, 1000) {
		t.Errorf("brokerage before %v", got)
	}
}

func TestSameDateOrder(t *testing.T) {
	rec := rsuRecord(t)
	day := d(2026, time.March, 15)
	rsu := rec.Account("rsu")
	rsu.Grants = append(rsu.Grants, rsuGrant(t, rec.Calendar("cal"), 2, "ACME", day, 100, 120, d(2027, time.March, 15), []float64{100}))
	// The grant dated on the vest is after the start only when the projection starts before the account's data ends.
	must(t, rec.AddProjection(&model.Projection{Name: "early", Start: model.Ptr(d(2025, time.December, 31)), OverrideHistory: model.Ptr(true)}))
	one := func(id string, kind model.ItemKind) model.BudgetItem {
		return model.BudgetItem{ID: id, Kind: kind, Amount: model.Constant(10), Account: "rsu", From: "rsu", To: "brk",
			Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: day}}
	}
	rec.Budget.Items = []model.BudgetItem{one("transfer", model.ItemTransfer), one("expense", model.ItemExpense), one("income", model.ItemIncome)}
	r, err := prepare(rec, "early", rsuAsset, d(2026, time.April, 1))
	must(t, err)
	var got []Cause
	for _, c := range r.paths["rsu"].changes {
		if c.date == day {
			got = append(got, c.cause)
		}
	}
	want := []Cause{CauseIncome, CauseTax, CauseGrant, CauseVest, CauseTax, CauseExpense, CauseTransfer}
	if !slices.Equal(got, want) {
		t.Fatalf("changes on the date: %v, want %v", got, want)
	}
}

func TestTwoSymbolsInOneAccount(t *testing.T) {
	rec := rsuRecord(t)
	rsu := rec.Account("rsu")
	rsu.Grants = append(rsu.Grants, rsuGrant(t, rec.Calendar("cal"), 2, "BETA", d(2025, time.June, 1), 100, 50, d(2026, time.March, 15), []float64{100}))
	rec.Prices.Add(model.PriceObservation{ID: 3, Symbol: "BETA", Date: d(2025, time.December, 31), Price: 60})
	addProjection(t, rec, "base", "")
	addProjection(t, rec, "grow", "base", growthMod(model.KindStock, "BETA", 0.10))
	if got := value(t, rec, "base", rsuAsset, d(2026, time.January, 1)); !near(got, 400*120+100*60) {
		t.Errorf("both stocks today %v", got)
	}
	date := d(2026, time.March, 15)
	beta := 60 * math.Pow(1.10, float64(date-d(2025, time.December, 31))/model.DaysPerYear)
	// Each stock is projected on its own: ACME flat, BETA +10%; one 25-share BETA vest and one 50-share ACME vest are done.
	want := 350*120 + 75*beta
	if got := value(t, rec, "grow", rsuAsset, date); !near(got, want) {
		t.Errorf("equity %v, want %v", got, want)
	}
	net := (50*120 + 25*beta) * (1 - rates)
	if got := value(t, rec, "grow", brkAsset, date); !near(got, 1000+net) {
		t.Errorf("brokerage %v, want %v", got, 1000+net)
	}
	b, err := Breakdown(rec, "grow", rsuAsset, d(2026, time.January, 1), d(2026, time.June, 1))
	must(t, err)
	if !near(b.Total(), b.End-b.Start) {
		t.Errorf("total %v, want %v", b.Total(), b.End-b.Start)
	}
}

func TestStockGrowthScaleMode(t *testing.T) {
	rec := rsuRecord(t)
	rec.StockGrowth["ACME"] = model.Constant(0.10)
	addProjection(t, rec, "base", "")
	scale := model.Modification{Target: model.ObjectRef{Kind: model.KindStock, ID: "ACME"}, Property: model.PropGrowth, Mode: model.ModeScale, Values: model.Constant(0.5)}
	addProjection(t, rec, "half", "base", scale)
	date := d(2026, time.May, 1)
	if got, want := value(t, rec, "base", rsuAsset, date), 350*priceGrown(0.10, date); !near(got, want) {
		t.Errorf("base stock growth: %v, want %v", got, want)
	}
	if got, want := value(t, rec, "half", rsuAsset, date), 350*priceGrown(0.05, date); !near(got, want) {
		t.Errorf("scaled stock growth: %v, want %v", got, want)
	}
}

func TestAccountGrowthOfEquityAccountIsSkipped(t *testing.T) {
	rec := rsuRecord(t)
	addProjection(t, rec, "grow", "", growthMod(model.KindAccount, "rsu", 0.2), growthMod(model.KindStock, "NOPE", 0.2))
	res, err := Balances(rec, "grow", rsuAsset, []model.Date{d(2026, time.May, 1)})
	must(t, err)
	if len(res.Skipped) != 2 || !strings.Contains(res.Skipped[0].Reason, "equity account") || res.Skipped[1].Reason != "stock not found" {
		t.Errorf("skipped: %+v", res.Skipped)
	}
	if got := res.Samples[0].Amount; !near(got, 350*120) {
		t.Errorf("equity %v", got)
	}
}

// A recorded vest that the account's data reaches only after the start is projected under the override-history start, so no share is left behind.
func TestVestRecordedAfterTheStartIsProjected(t *testing.T) {
	rec := rsuRecord(t)
	rec.Account("rsu").Vests = append(rec.Account("rsu").Vests, model.RecordedVest{ID: 3, Grant: 1, Planned: 1, Date: d(2026, time.March, 15), Shares: 50, Price: 120, Rates: model.DefaultWithholding})
	must(t, rec.AddProjection(&model.Projection{Name: "early", Start: model.Ptr(d(2025, time.December, 31)), OverrideHistory: model.Ptr(true)}))
	if got, want := value(t, rec, "early", rsuAsset, d(2026, time.March, 14)), 400*120.0; !near(got, want) {
		t.Errorf("before the vest %v, want %v", got, want)
	}
	if got, want := value(t, rec, "early", rsuAsset, d(2026, time.March, 15)), 350*120.0; !near(got, want) {
		t.Errorf("on the vest %v, want %v", got, want)
	}
	if got := value(t, rec, "early", rsuAsset, d(2027, time.December, 15)); !near(got, 0) {
		t.Errorf("after every vest %v", got)
	}
	if got, want := value(t, rec, "early", brkAsset, d(2027, time.December, 15)), 1000+400*120*(1-rates); !near(got, want) {
		t.Errorf("brokerage at the end %v, want %v", got, want)
	}
}

// The price of a stock starts from the one the account's starting value uses: recorded on its start, with growth from there.
func TestStockPriceStartsFromTheAccountsStartValue(t *testing.T) {
	rec := rsuRecord(t)
	rsu := rec.Account("rsu")
	// Vest 1 is recorded on its date at the price of the last observation but one, so the data of the account ends on 2026-03-15 without a new observation.
	*rec.Prices = model.PriceBook{}
	rec.Prices.Add(model.PriceObservation{ID: 1, Symbol: "ACME", Date: d(2026, time.January, 2), Price: 100})
	rsu.Vests = append(rsu.Vests, model.RecordedVest{ID: 3, Grant: 1, Planned: 1, Date: d(2026, time.March, 15), Shares: 50, Price: 100, Rates: model.DefaultWithholding})
	brk := rec.Account("brk")
	brk.Snapshots = nil
	brk.AddSnapshot(d(2026, time.January, 1), 1000)
	brk.AddSnapshot(d(2026, time.March, 15), 1000)
	addProjection(t, rec, "base", "")
	addProjection(t, rec, "up", "base", growthMod(model.KindStock, "ACME", 0.10))
	start := d(2026, time.March, 15)
	if got := value(t, rec, "up", rsuAsset, start); !near(got, 350*100) {
		t.Fatalf("on the start %v", got)
	}
	date := d(2026, time.June, 15)
	px := 100 * math.Pow(1.10, float64(date-start)/model.DaysPerYear)
	if got, want := value(t, rec, "up", rsuAsset, date), 300*px; !near(got, want) {
		t.Errorf("equity after the next vest %v, want %v", got, want)
	}
	if got, want := value(t, rec, "up", brkAsset, date), 1000+50*px*(1-rates); !near(got, want) {
		t.Errorf("brokerage %v, want %v", got, want)
	}
	if got := value(t, rec, "up", rsuAsset, d(2027, time.December, 15)); !near(got, 0) {
		t.Errorf("after the last vest %v", got)
	}
}

// Another equity account that starts earlier and holds other stock does not set the price of a stock it does not hold.
func TestStockPriceIgnoresAccountsThatDoNotHoldIt(t *testing.T) {
	rec := rsuRecord(t)
	one := &model.VestCalendar{Name: "one", Rules: []model.VestRule{{Month: time.March, Day: 15}}}
	rec.Calendars = append(rec.Calendars, one)
	g := rsuGrant(t, one, 2, "FOO", d(2024, time.June, 1), 100, 10, d(2025, time.March, 15), []float64{100})
	g.Account = "foo"
	rec.Prices.Add(model.PriceObservation{ID: 3, Symbol: "FOO", Date: d(2025, time.March, 15), Price: 10})
	foo := &model.Account{ID: "foo", Name: "Foo", Type: model.TypeEquity, Tax: model.Taxable, Grants: []model.Grant{g},
		Vests: []model.RecordedVest{{ID: 4, Grant: 2, Planned: 1, Date: d(2025, time.March, 15), Shares: 100, Price: 10, Rates: model.DefaultWithholding}}}
	must(t, rec.AddAccount(foo))
	addProjection(t, rec, "base", "")
	if got, want := value(t, rec, "base", rsuAsset, d(2026, time.March, 15)), 350*120.0; !near(got, want) {
		t.Errorf("equity %v, want %v", got, want)
	}
	if got, want := value(t, rec, "base", brkAsset, d(2026, time.March, 15)), 1000+50*120*(1-rates); !near(got, want) {
		t.Errorf("brokerage %v, want %v", got, want)
	}
}

// A deposit tied to a recorded vest by its entry is read back as the vest's proceeds whatever its date and amount.
func TestTiedDepositIsMatchedByEntryNotByValues(t *testing.T) {
	rec := rsuRecord(t)
	rsu, brk := rec.Account("rsu"), rec.Account("brk")
	rsu.Grants[0].FirstVest = d(2025, time.September, 15)
	pct, err := model.GeneratePercents(rec.Calendar("cal"), rsu.Grants[0].FirstVest, rsu.Grants[0].YearPercents, 0)
	must(t, err)
	rsu.Grants[0].VestPercents = pct
	v := model.RecordedVest{ID: 4, Grant: 1, Planned: 1, Date: d(2025, time.September, 15), Shares: 50, Price: 110, Rates: model.DefaultWithholding, TxEntry: 9}
	rsu.Vests = append(rsu.Vests, v)
	deposit := v.Net() + 0.37
	brk.Snapshots = nil
	brk.AddSnapshot(d(2025, time.August, 1), 1000)
	brk.AddSnapshot(d(2025, time.December, 31), 1000+deposit)
	must(t, brk.AddTransaction(model.Transaction{Date: d(2025, time.September, 17), Amount: deposit, Kind: model.TxIncome, Entry: 9}))
	addProjection(t, rec, "base", "")
	from, to := d(2025, time.August, 1), d(2025, time.December, 31)
	b, err := Breakdown(rec, "base", brkAsset, from, to)
	must(t, err)
	if l := line(b, CauseTransfer); !near(l.Actual, deposit) {
		t.Errorf("transfer %+v, want %v", l, deposit)
	}
	if l := line(b, CauseIncome); l.Actual != 0 {
		t.Errorf("income %+v", l)
	}
	b, err = Breakdown(rec, "base", "both", from, to)
	must(t, err)
	if l := line(b, CauseIncome); l.Actual != 0 {
		t.Errorf("both: income %+v", l)
	}
}

// Without a price of the stock on or before the start, each grant starts from its own price, as the account's starting value does.
func TestGrantsWithoutPricesStartFromTheirOwnPrices(t *testing.T) {
	for _, rate := range []float64{0, 0.10} {
		rec := rsuRecord(t)
		rsu := rec.Account("rsu")
		*rec.Prices = model.PriceBook{}
		cal := rec.Calendar("cal")
		rsu.Grants = []model.Grant{
			rsuGrant(t, cal, 1, "ACME", d(2023, time.March, 1), 400, 10, d(2026, time.March, 15), []float64{50, 50}),
			rsuGrant(t, cal, 2, "ACME", d(2024, time.March, 1), 100, 50, d(2026, time.March, 15), []float64{100}),
		}
		addProjection(t, rec, "base", "")
		if rate != 0 {
			addProjection(t, rec, "up", "base", growthMod(model.KindStock, "ACME", rate))
		}
		proj := "base"
		if rate != 0 {
			proj = "up"
		}
		// With no prices or vests the account's last data is its latest grant, which is where its path starts.
		start := d(2024, time.March, 1)
		grow := func(date model.Date) float64 { return math.Pow(1+rate, float64(date-start)/model.DaysPerYear) }
		if got := value(t, rec, proj, rsuAsset, start); !near(got, 9000) {
			t.Fatalf("rate %v: on the start %v, want 9000", rate, got)
		}
		want := 1000.0
		for _, m := range []time.Month{time.March, time.June, time.September, time.December} {
			date := d(2026, m, 15)
			want += (50*10 + 25*50) * grow(date) * (1 - rates)
		}
		for _, m := range []time.Month{time.March, time.June, time.September, time.December} {
			date := d(2027, m, 15)
			want += 50 * 10 * grow(date) * (1 - rates)
		}
		end := d(2027, time.December, 15)
		if got := value(t, rec, proj, rsuAsset, end); !near(got, 0) {
			t.Errorf("rate %v: equity after the last vest %v, want 0", rate, got)
		}
		if got := value(t, rec, proj, brkAsset, end); !near(got, want) {
			t.Errorf("rate %v: brokerage at the end %v, want %v", rate, got, want)
		}
	}
}

// A grant dated after an override-history start is priced from its own price on its own date, not from the start.
func TestGrantAfterTheStartWithoutPricesStartsOnItsDate(t *testing.T) {
	rec := rsuRecord(t)
	rsu := rec.Account("rsu")
	*rec.Prices = model.PriceBook{}
	cal := rec.Calendar("cal")
	grantDate := d(2025, time.March, 1)
	rsu.Grants = []model.Grant{
		rsuGrant(t, cal, 1, "ACME", d(2023, time.March, 1), 400, 10, d(2026, time.March, 15), []float64{50, 50}),
		rsuGrant(t, cal, 2, "ACME", grantDate, 100, 50, d(2026, time.March, 15), []float64{100}),
	}
	must(t, rec.AddProjection(&model.Projection{Name: "early", Start: model.Ptr(d(2024, time.January, 1)), OverrideHistory: model.Ptr(true),
		Mods: []model.Modification{growthMod(model.KindStock, "ACME", 0.10)}}))
	start := d(2024, time.January, 1)
	grow := func(from, date model.Date) float64 { return math.Pow(1.10, float64(date-from)/model.DaysPerYear) }
	if got, want := value(t, rec, "early", rsuAsset, grantDate), 400*10*grow(start, grantDate)+100*50; !near(got, want) {
		t.Errorf("on the grant %v, want %v", got, want)
	}
	before := value(t, rec, "early", brkAsset, d(2026, time.January, 1))
	want := 0.0
	for _, y := range []int{2026, 2027} {
		for _, m := range []time.Month{time.March, time.June, time.September, time.December} {
			date := d(y, m, 15)
			want += 50 * 10 * grow(start, date) * (1 - rates)
			if y == 2026 {
				want += 25 * 50 * grow(grantDate, date) * (1 - rates)
			}
		}
	}
	end := d(2027, time.December, 15)
	if got := value(t, rec, "early", rsuAsset, end); !near(got, 0) {
		t.Errorf("equity after the last vest %v, want 0", got)
	}
	if got := value(t, rec, "early", brkAsset, end) - before; !near(got, want) {
		t.Errorf("deposits %v, want %v", got, want)
	}
}
