// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// periodRecord is a checking account whose data ends on 2026-01-16 at 5000, with the items. Groceries is a spread item of 310 on the 1st (its period is January, 31 days) and insurance a lump of 100 on the 10th; the cutoff is mid-period for both.
func periodRecord(t *testing.T, items ...model.BudgetItem) *model.Record {
	t.Helper()
	rec := model.NewRecord(d(2026, time.January, 16))
	chk := &model.Account{ID: "chk", Type: model.TypeChecking, Tax: model.Taxable}
	chk.AddSnapshot(d(2026, time.January, 1), 5000)
	chk.AddSnapshot(d(2026, time.January, 16), 5000)
	must(t, rec.AddAccount(chk))
	rec.Budget.Items = items
	return rec
}

func periodItem(id string, amount float64, day int, timing model.Timing) model.BudgetItem {
	return model.BudgetItem{ID: id, Name: id, Kind: model.ItemExpense, Amount: model.Constant(amount), Recurrence: model.Recurrence{Kind: model.Monthly, Day: day},
		Start: model.Ptr(d(2025, time.December, 1)), Account: "chk", Timing: timing}
}

func chkOn(t *testing.T, rec *model.Record, date model.Date) float64 {
	t.Helper()
	return sample(t, rec, "", model.DefaultAssetID("chk"), date).Amount
}

func occurrence(rec *model.Record, item string, period model.Date, status model.OccurrenceStatus, posted, expected *float64) {
	rec.Occurrences.Add(model.Occurrence{Item: item, Period: period, Status: status, Posted: posted, Expected: expected})
}

func periodOf(t *testing.T, rec *model.Record, item string) model.PeriodView {
	t.Helper()
	ps, err := Periods(rec, "", rec.Today, 6)
	must(t, err)
	for _, p := range ps {
		if p.Item.ID == item {
			return p.Current()
		}
	}
	t.Fatalf("no period for %s", item)
	return model.PeriodView{}
}

func TestMidPeriodAppliesLumpsAfterTheCutoffAndTheUnspentShareOfSpreadItems(t *testing.T) {
	rec := periodRecord(t, periodItem("groceries", 310, 1, model.Spread), periodItem("rent", 200, 20, model.Lump))
	// 16 of January's 31 days are in the balance: 150 is left, 10 a day from the 17th. Rent of the 20th of December is in it, and the one on the 20th of January is not.
	for _, c := range []struct {
		date model.Date
		want float64
	}{
		{d(2026, time.January, 16), 5000},
		{d(2026, time.January, 19), 5000 - 30},
		{d(2026, time.January, 20), 5000 - 40 - 200},
		{d(2026, time.January, 31), 5000 - 150 - 200},
		{d(2026, time.February, 1), 5000 - 150 - 200 - 310},
	} {
		if got := chkOn(t, rec, c.date); !near(got, c.want) {
			t.Errorf("%v: balance %v, want %v", c.date, got, c.want)
		}
	}
}

func TestPastDueLumpMarkedPendingIsAppliedTheNextDay(t *testing.T) {
	rec := periodRecord(t, periodItem("insurance", 100, 10, model.Lump))
	if got := chkOn(t, rec, d(2026, time.January, 17)); !near(got, 5000) {
		t.Fatalf("a lump dated before the cutoff is in the balance: got %v", got)
	}
	occurrence(rec, "insurance", d(2026, time.January, 10), model.StatusPending, nil, nil)
	for _, c := range []struct {
		date model.Date
		want float64
	}{{d(2026, time.January, 16), 5000}, {d(2026, time.January, 17), 4900}, {d(2026, time.February, 9), 4900}, {d(2026, time.February, 10), 4800}} {
		if got := chkOn(t, rec, c.date); !near(got, c.want) {
			t.Errorf("%v: balance %v, want %v", c.date, got, c.want)
		}
	}
}

func TestPostedAmountReplacesTheEstimate(t *testing.T) {
	rec := periodRecord(t, periodItem("groceries", 310, 1, model.Spread))
	occurrence(rec, "groceries", d(2026, time.January, 1), model.StatusAuto, model.Ptr(100.0), nil)
	if got := chkOn(t, rec, d(2026, time.January, 31)); !near(got, 5000-210) {
		t.Errorf("balance %v, want %v", got, 5000-210.0)
	}
	if v := periodOf(t, rec, "groceries"); v.PostedAuto || v.Posted != 100 || v.Remaining != 210 || v.Variance != -210 {
		t.Errorf("period %+v", v)
	}
}

func TestPostedOverBudgetLeavesNothingMoreAndShowsTheOverage(t *testing.T) {
	rec := periodRecord(t, periodItem("groceries", 310, 1, model.Spread))
	occurrence(rec, "groceries", d(2026, time.January, 1), model.StatusAuto, model.Ptr(400.0), nil)
	if got := chkOn(t, rec, d(2026, time.January, 31)); !near(got, 5000) {
		t.Errorf("balance %v, want 5000", got)
	}
	if v := periodOf(t, rec, "groceries"); v.Remaining != 0 || v.Variance != 90 {
		t.Errorf("period %+v", v)
	}
}

func TestPostedStatusAppliesNothingMore(t *testing.T) {
	rec := periodRecord(t, periodItem("groceries", 310, 1, model.Spread))
	occurrence(rec, "groceries", d(2026, time.January, 1), model.StatusPosted, nil, nil)
	if got := chkOn(t, rec, d(2026, time.January, 31)); !near(got, 5000) {
		t.Errorf("balance %v, want 5000", got)
	}
	if v := periodOf(t, rec, "groceries"); v.Posted != 310 || v.Remaining != 0 {
		t.Errorf("period %+v", v)
	}
}

func TestExpectedAmountDrivesTheRemainder(t *testing.T) {
	rec := periodRecord(t, periodItem("groceries", 310, 1, model.Spread))
	occurrence(rec, "groceries", d(2026, time.January, 1), model.StatusAuto, nil, model.Ptr(460.0))
	want := 5000 - 460.0*15/31
	if got := chkOn(t, rec, d(2026, time.January, 31)); !near(got, want) {
		t.Errorf("balance %v, want %v", got, want)
	}
	// Only the period with the occurrence is expected to differ: the next one is the budgeted 310.
	if got := chkOn(t, rec, d(2026, time.February, 1)); !near(got, want-310) {
		t.Errorf("balance %v, want %v", got, want-310)
	}
}

func TestPreviousPeriodChangesTheProjectionOnlyWhenPending(t *testing.T) {
	rec := periodRecord(t, periodItem("groceries", 310, 1, model.Spread))
	dec := d(2025, time.December, 1)
	occurrence(rec, "groceries", dec, model.StatusPosted, model.Ptr(50.0), nil)
	if got := chkOn(t, rec, d(2026, time.January, 17)); !near(got, 5000-10) {
		t.Errorf("a posted previous period applied something: balance %v", got)
	}
	// Pending with 100 posted of 310: the remaining 210 lands the day after the cutoff, with the day's share of the current period.
	occurrence(rec, "groceries", dec, model.StatusPending, model.Ptr(100.0), nil)
	if got := chkOn(t, rec, d(2026, time.January, 16)); !near(got, 5000) {
		t.Errorf("balance on the cutoff %v", got)
	}
	if got := chkOn(t, rec, d(2026, time.January, 17)); !near(got, 5000-210-10) {
		t.Errorf("balance %v, want %v", got, 5000-210.0-10)
	}
}

func TestVarianceHistoryOfClosedPeriods(t *testing.T) {
	rec := periodRecord(t, periodItem("groceries", 310, 1, model.Spread))
	occurrence(rec, "groceries", d(2025, time.December, 1), model.StatusAuto, model.Ptr(350.0), model.Ptr(330.0))
	ps, err := Periods(rec, "", rec.Today, 6)
	must(t, err)
	h := ps[0].Periods
	if len(h) != 2 || h[0].Period.Start != d(2025, time.December, 1) || h[1].Period.Start != d(2026, time.January, 1) {
		t.Fatalf("history %+v", h)
	}
	// A closed period's variance is its expected amount, when set, less the budget; the open one's is posted less budget: 160 estimated of 310.
	if !h[0].Closed || h[0].Variance != 20 || h[1].Closed || !near(h[1].Variance, 160-310) {
		t.Errorf("variances %v and %v", h[0].Variance, h[1].Variance)
	}
}

func TestOccurrenceOverrideReachesTaxes(t *testing.T) {
	rec := fixture(t)
	base := taxYear(t, rec, "", 2026).Wages
	june := d(2026, time.June, 15)
	occurrence(rec, "salary", june, model.StatusAuto, model.Ptr(1500.0), nil)
	if got := taxYear(t, rec, "", 2026).Wages; !near(got, base+500) {
		t.Errorf("wages with an amount posted %v, want %v", got, base+500)
	}
	occurrence(rec, "salary", june, model.StatusAuto, nil, model.Ptr(1200.0))
	if got := taxYear(t, rec, "", 2026).Wages; !near(got, base+200) {
		t.Errorf("wages with an amount expected %v, want %v", got, base+200)
	}
	occurrence(rec, "salary", june, model.StatusPosted, nil, model.Ptr(1200.0))
	if got := taxYear(t, rec, "", 2026).Wages; !near(got, base+200) {
		t.Errorf("wages with the occurrence posted %v, want %v", got, base+200)
	}
}

func TestOverrideOnAPeriodAfterTheCutoffReplacesItsOccurrence(t *testing.T) {
	// The cutoff is the 16th; rent of 200 is due on the 20th, groceries of 310 are spread over the 31 days from the 20th.
	rent := func() *model.Record { return periodRecord(t, periodItem("rent", 200, 20, model.Lump)) }
	for _, c := range []struct {
		name  string
		set   func(rec *model.Record)
		want  float64
		wants map[model.Date]float64
	}{
		{"posted in full", func(rec *model.Record) {
			occurrence(rec, "rent", d(2026, time.January, 20), model.StatusPosted, nil, nil)
		}, 0, map[model.Date]float64{d(2026, time.January, 31): 5000}},
		{"part posted", func(rec *model.Record) {
			occurrence(rec, "rent", d(2026, time.January, 20), model.StatusAuto, model.Ptr(50.0), nil)
		}, 150, map[model.Date]float64{d(2026, time.January, 19): 5000, d(2026, time.January, 20): 4850}},
		{"expected less", func(rec *model.Record) {
			occurrence(rec, "rent", d(2026, time.January, 20), model.StatusAuto, nil, model.Ptr(120.0))
		}, 120, map[model.Date]float64{d(2026, time.January, 20): 4880}},
	} {
		rec := rent()
		c.set(rec)
		for date, want := range c.wants {
			if got := chkOn(t, rec, date); !near(got, want) {
				t.Errorf("%s: %v balance %v, want %v", c.name, date, got, want)
			}
		}
		rec.Today = d(2026, time.January, 20)
		if v := periodOf(t, rec, "rent"); !near(v.Remaining, c.want) {
			t.Errorf("%s: the view's remaining %v, the engine applies %v", c.name, v.Remaining, c.want)
		}
	}

	// A spread item's remainder is spread over the period's 31 days: 248 of 310 is 8 a day.
	rec := periodRecord(t, periodItem("groceries", 310, 20, model.Spread))
	occurrence(rec, "groceries", d(2026, time.January, 20), model.StatusAuto, model.Ptr(62.0), nil)
	for date, want := range map[model.Date]float64{d(2026, time.January, 19): 5000, d(2026, time.January, 22): 5000 - 24, d(2026, time.February, 19): 5000 - 248} {
		if got := chkOn(t, rec, date); !near(got, want) {
			t.Errorf("spread: %v balance %v, want %v", date, got, want)
		}
	}
}

func TestPendingPaycheckBooksItsWithholdingWithTheRemainder(t *testing.T) {
	// A paycheck dated the 15th, marked pending, with the balances known to the 16th: it lands on the 17th, taxed as the paycheck of the 15th is.
	plain := fixture(t)
	want := chkOn(t, plain, d(2026, time.January, 15))
	if want <= 1000 || want >= 2000 {
		t.Fatalf("the paycheck nets %v, want between 1000 and 2000 for a gross of 1000", want-1000)
	}
	rec := fixture(t)
	rec.Account("chk").AddSnapshot(d(2026, time.January, 16), 1000)
	occurrence(rec, "salary", d(2026, time.January, 15), model.StatusPending, nil, nil)
	if got := chkOn(t, rec, d(2026, time.January, 17)); !near(got, want) {
		t.Errorf("balance after the pending paycheck %v, want %v", got, want)
	}
}

func TestPartlyPostedPaycheckIsTaxedAsTheFullPaycheck(t *testing.T) {
	// The cutoff is the 1st of January: the paycheck of the 15th of December is in its period, 600 of its 1000 have posted and the rest is booked, so the year counts 1000.
	base := taxYear(t, fixture(t), "", 2025)
	rec := fixture(t)
	occurrence(rec, "salary", d(2025, time.December, 15), model.StatusAuto, model.Ptr(600.0), nil)
	got := taxYear(t, rec, "", 2025)
	if !near(got.Wages, base.Wages) || !near(got.WithheldPaychecks, base.WithheldPaychecks) {
		t.Errorf("wages %v and withheld %v, want %v and %v", got.Wages, got.WithheldPaychecks, base.Wages, base.WithheldPaychecks)
	}
}

func TestPendingContributionAtTheLimitMovesNothing(t *testing.T) {
	// The savings of 30000 a month reach the 401(k) limit long before December, so the pending one of the 16th is refused whole.
	setup := func() *model.Record {
		rec := fixture(t)
		for i := range rec.Budget.Items {
			if rec.Budget.Items[i].ID == "save" {
				rec.Budget.Items[i].Amount = model.Constant(30000)
			}
		}
		return rec
	}
	rec := setup()
	occurrence(rec, "save", d(2025, time.December, 16), model.StatusPending, nil, nil)
	base := setup()
	for _, id := range []model.AccountID{"chk", "inv"} {
		got := sample(t, rec, "", model.DefaultAssetID(id), d(2026, time.January, 2)).Amount
		want := sample(t, base, "", model.DefaultAssetID(id), d(2026, time.January, 2)).Amount
		if !near(got, want) {
			t.Errorf("%s: balance %v, want %v", id, got, want)
		}
	}
}

// matchRecord is a checking account known to November 8 and a 401(k) known to October 31. The salary of 5000 on the 1st matches 50% of 6% of the gross into the 401(k) and starts on October 1.
func matchRecord(t *testing.T) *model.Record {
	t.Helper()
	rec := model.NewRecord(d(2026, time.November, 8))
	chk := &model.Account{ID: "chk", Type: model.TypeChecking, Tax: model.Taxable}
	chk.AddSnapshot(d(2026, time.October, 1), 1000)
	chk.AddSnapshot(d(2026, time.November, 8), 1000)
	k := &model.Account{ID: "k401", Type: model.Type401k, Tax: model.TaxDeferred}
	k.AddSnapshot(d(2026, time.October, 1), 3000)
	k.AddSnapshot(d(2026, time.October, 31), 3000)
	must(t, rec.AddAccount(chk))
	must(t, rec.AddAccount(k))
	rec.Budget.Items = []model.BudgetItem{
		{ID: "salary", Kind: model.ItemIncome, Amount: model.Constant(5000), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1},
			Start: model.Ptr(d(2026, time.October, 1)), Account: "chk", MatchAccount: "k401", MatchPercent: 50, MatchUpTo: 6},
		{ID: "save", Kind: model.ItemTransfer, Amount: model.Constant(400), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1},
			Start: model.Ptr(d(2026, time.October, 1)), From: "chk", To: "k401"},
	}
	return rec
}

func TestMatchAfterTheMatchAccountsCutoffIgnoresThePaycheckFact(t *testing.T) {
	base := matchRecord(t)
	want := sample(t, base, "", model.DefaultAssetID("k401"), d(2026, time.November, 2)).Amount
	if want <= 3000 {
		t.Fatalf("the 401(k) holds %v on November 2, want the match on top of 3000", want)
	}
	for _, c := range []struct {
		name   string
		posted *float64
		status model.OccurrenceStatus
	}{{"posted", nil, model.StatusPosted}, {"part posted", model.Ptr(2000.0), model.StatusAuto}} {
		rec := matchRecord(t)
		occurrence(rec, "salary", d(2026, time.November, 1), c.status, c.posted, nil)
		if got := sample(t, rec, "", model.DefaultAssetID("k401"), d(2026, time.November, 2)).Amount; !near(got, want) {
			t.Errorf("%s: the 401(k) holds %v on November 2, want %v", c.name, got, want)
		}
	}
}

func TestTaxCountsOnlyTheRemainderTheEngineBooks(t *testing.T) {
	// The salary ends on November 5, before the cutoff of the 8th: nothing more of the November paycheck is booked, so the year counts the 5000 of October and the 2000 posted.
	rec := matchRecord(t)
	rec.Budget.Items[0].End = model.Ptr(d(2026, time.November, 5))
	occurrence(rec, "salary", d(2026, time.November, 1), model.StatusAuto, model.Ptr(2000.0), nil)
	if got := taxYear(t, rec, "", 2026); !near(got.Wages, 7000) {
		t.Errorf("wages %v, want 7000", got.Wages)
	}
}

func TestMatchIsNotBookedOnARemainderThePaycheckSideWroteOff(t *testing.T) {
	// October is behind the checking cutoff of November 8, so its unposted 3000 is never paid and earns no match, though October is the 401(k)'s cutoff period.
	base := matchRecord(t)
	want := sample(t, base, "", model.DefaultAssetID("k401"), d(2026, time.November, 2)).Amount
	rec := matchRecord(t)
	occurrence(rec, "salary", d(2026, time.October, 1), model.StatusAuto, model.Ptr(2000.0), nil)
	if got := sample(t, rec, "", model.DefaultAssetID("k401"), d(2026, time.November, 2)).Amount; !near(got, want) {
		t.Errorf("the 401(k) holds %v on November 2, want %v", got, want)
	}
}

func TestTaxInputsDoNotDependOnTheQueryHorizon(t *testing.T) {
	rec := matchRecord(t)
	rec.Budget.Items[1].Timing = model.Spread
	occurrence(rec, "save", d(2026, time.November, 1), model.StatusAuto, model.Ptr(100.0), nil)
	k := model.DefaultAssetID("k401")
	near15 := d(2026, time.November, 15)
	short, err := Balances(rec, "", k, []model.Date{near15})
	must(t, err)
	long, err := Balances(rec, "", k, []model.Date{near15, d(2026, time.November, 30)})
	must(t, err)
	if !near(short.Samples[0].Amount, long.Samples[0].Amount) {
		t.Errorf("the 401(k) holds %v on November 15 asked alone, %v asked with November 30", short.Samples[0].Amount, long.Samples[0].Amount)
	}
	if len(short.Notes) > 0 || len(long.Notes) > 0 {
		t.Errorf("notes %q and %q, want none", short.Notes, long.Notes)
	}
}

func TestSpreadRemainderCutShortByTheItemsEndIsNotCapped(t *testing.T) {
	// The spread transfer ends on November 20: the 300 left of its period is booked over the 12 days to then, and the 401(k) limit refuses none of it.
	rec := matchRecord(t)
	rec.Budget.Items[1].Timing = model.Spread
	rec.Budget.Items[1].End = model.Ptr(d(2026, time.November, 20))
	occurrence(rec, "save", d(2026, time.November, 1), model.StatusAuto, model.Ptr(100.0), nil)
	k := model.DefaultAssetID("k401")
	got, err := Balances(rec, "", k, []model.Date{d(2026, time.November, 20)})
	must(t, err)
	if want := 3000 + 300.0*12/22; !near(got.Samples[0].Amount, want) {
		t.Errorf("the 401(k) holds %v on November 20, want %v", got.Samples[0].Amount, want)
	}
	if len(got.Notes) > 0 {
		t.Errorf("notes %q, want none", got.Notes)
	}
}

func TestSpreadPeriodCutShortWithoutAFactBooksTaxAndMatchInTheShareOfWhatIsBooked(t *testing.T) {
	// The salary ends on November 20: 3000 of its 5000 are in the balance on the 8th and 2000 are booked, the same as for a lump with 3000 posted, so withholding and match are 40% of the paycheck's.
	spread := matchRecord(t)
	spread.Account("k401").AddSnapshot(d(2026, time.November, 8), 3000)
	spread.Budget.Items[0].Timing = model.Spread
	spread.Budget.Items[0].End = model.Ptr(d(2026, time.November, 20))
	lump := matchRecord(t)
	lump.Account("k401").AddSnapshot(d(2026, time.November, 8), 3000)
	lump.Budget.Items[0].End = model.Ptr(d(2026, time.November, 20))
	occurrence(lump, "salary", d(2026, time.November, 1), model.StatusAuto, model.Ptr(3000.0), nil)
	end := []model.Date{d(2026, time.November, 20)}
	for _, id := range []model.AccountID{"chk", "k401"} {
		got, err := Balances(spread, "", model.DefaultAssetID(id), end)
		must(t, err)
		want, err := Balances(lump, "", model.DefaultAssetID(id), end)
		must(t, err)
		if !near(got.Samples[0].Amount, want.Samples[0].Amount) {
			t.Errorf("%s holds %v on November 20, want %v", id, got.Samples[0].Amount, want.Samples[0].Amount)
		}
	}
	if got, want := taxYear(t, spread, "", 2026), taxYear(t, lump, "", 2026); !near(got.Wages, want.Wages) || !near(got.WithheldPaychecks, want.WithheldPaychecks) {
		t.Errorf("wages %v and withheld %v, want %v and %v", got.Wages, got.WithheldPaychecks, want.Wages, want.WithheldPaychecks)
	}
}
