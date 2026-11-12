// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

func within(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// marketFixture is a checking account with 5,000 and two accounts with 1,000,000 each on today, held 60% large cap (10% a year) and 40% bonds (4.5%): the taxable brokerage "a" and the 401k "k".
func marketFixture(t *testing.T) *model.Record {
	t.Helper()
	today := d(2026, time.January, 1)
	rec := model.NewRecord(today)
	rec.Classes = model.DefaultClasses()
	chk := &model.Account{ID: "chk", Type: model.TypeChecking, Tax: model.Taxable}
	chk.AddSnapshot(today, 5000)
	alloc := []model.Holding{{Class: model.ClassLargeCap, Percent: 60}, {Class: model.ClassBonds, Percent: 40}}
	a := &model.Account{ID: "a", Type: model.TypeBrokerage, Tax: model.Taxable, Allocation: alloc}
	a.AddSnapshot(today, 1000000)
	k := &model.Account{ID: "k", Type: model.Type401k, Tax: model.TaxDeferred, Allocation: alloc}
	k.AddSnapshot(today, 1000000)
	for _, acct := range []*model.Account{chk, a, k} {
		must(t, rec.AddAccount(acct))
	}
	return rec
}

func balanceOf(t *testing.T, rec *model.Record, proj string, id model.AccountID, date model.Date) float64 {
	t.Helper()
	return sample(t, rec, proj, model.DefaultAssetID(id), date).Amount
}

func rebalanceTo6040(account model.AccountID, date model.Date) model.Rebalance {
	return model.Rebalance{Account: account, Date: date, Allocation: []model.Weight{{Class: model.ClassLargeCap, Share: 0.6}, {Class: model.ClassBonds, Share: 0.4}}}
}

func TestShockDropsEachAccountByItsExposure(t *testing.T) {
	rec := marketFixture(t)
	on := d(2026, time.July, 1)
	must(t, rec.AddProjection(&model.Projection{Name: "crash", Shocks: []model.Shock{{Class: model.ClassLargeCap, Date: on, Change: -0.40}}}))
	must(t, rec.AddProjection(&model.Projection{Name: "child", Parent: "crash"}))
	days := on - rec.Today
	lc, bonds := 600000*grow(0.10, days), 400000*grow(0.045, days)
	for _, id := range []model.AccountID{"a", "k"} {
		if got := balanceOf(t, rec, "", id, on); !within(got, lc+bonds, 1e-4) {
			t.Fatalf("%s without the shock = %v, want %v", id, got, lc+bonds)
		}
		for _, proj := range []string{"crash", "child"} {
			if got, want := balanceOf(t, rec, proj, id, on), 0.6*lc+bonds; !within(got, want, 1e-4) {
				t.Errorf("%s under %s on the shock's date = %v, want %v (large cap down 40%%)", id, proj, got, want)
			}
		}
	}
	if got := balanceOf(t, rec, "crash", "chk", on); got != 5000 {
		t.Errorf("checking has no allocation and is %v, want 5000", got)
	}
	end := d(2027, time.January, 1)
	later := end - rec.Today
	if got, want := balanceOf(t, rec, "crash", "a", end), 600000*0.6*grow(0.10, later)+400000*grow(0.045, later); !within(got, want, 1e-4) {
		t.Errorf("a year on = %v, want %v: the shocked holding keeps growing", got, want)
	}
}

func TestShockOfUnknownClassIsSkipped(t *testing.T) {
	rec := marketFixture(t)
	must(t, rec.AddProjection(&model.Projection{Name: "bad", Shocks: []model.Shock{{Class: "nope", Date: d(2026, time.July, 1), Change: -0.2}, {Class: model.ClassBonds, Date: d(2026, time.July, 1), Change: -1}}}))
	res, err := Balances(rec, "bad", model.DefaultAssetID("a"), []model.Date{d(2026, time.August, 1)})
	must(t, err)
	if len(res.Skipped) != 2 || res.Skipped[0].Property != "shock" || res.Skipped[0].Reason != "class not found" {
		t.Errorf("skipped %+v", res.Skipped)
	}
}

// rebalanceGain is the long-term gain of selling large cap down to 60% of the account on a date after a +100% large cap shock, from the lot opened today.
func rebalanceGain(shock, on model.Date, today model.Date) float64 {
	atShock := 600000 * grow(0.10, shock-today)
	lc := 2 * atShock * grow(0.10, on-shock)
	total := lc + 400000*grow(0.045, on-today)
	sold := lc - 0.6*total
	return sold - 600000*sold/lc
}

func TestRebalanceRealizesGainsInTaxableAccountOnly(t *testing.T) {
	rec := marketFixture(t)
	shock, on := d(2026, time.July, 1), d(2027, time.March, 1)
	boom := []model.Shock{{Class: model.ClassLargeCap, Date: shock, Change: 1.0}}
	must(t, rec.AddProjection(&model.Projection{Name: "drift", Shocks: boom}))
	must(t, rec.AddProjection(&model.Projection{Name: "reb-a", Shocks: boom, Rebalances: []model.Rebalance{rebalanceTo6040("a", on)}}))
	must(t, rec.AddProjection(&model.Projection{Name: "reb-k", Shocks: boom, Rebalances: []model.Rebalance{rebalanceTo6040("k", on)}}))

	base, err := TaxYearOf(rec, "drift", 2027)
	must(t, err)
	if base.ShortTermGains != 0 || base.LongTermGains != 0 || base.GainsTax != 0 {
		t.Errorf("without a rebalance: %v short, %v long, %v tax", base.ShortTermGains, base.LongTermGains, base.GainsTax)
	}
	ty, err := TaxYearOf(rec, "reb-a", 2027)
	must(t, err)
	want := rebalanceGain(shock, on, rec.Today)
	if ty.ShortTermGains != 0 || !within(ty.LongTermGains, want, 1e-3) || want < 10000 {
		t.Errorf("rebalance in the brokerage: %v short, %v long, want 0 and %v", ty.ShortTermGains, ty.LongTermGains, want)
	}
	if ty.GainsTax <= 0 || !within(ty.Result.Total-base.Result.Total, ty.GainsTax, 1e-3) {
		t.Errorf("gains tax %v, the year's tax rose by %v", ty.GainsTax, ty.Result.Total-base.Result.Total)
	}
	if ty.Input.LongTermGains != ty.LongTermGains {
		t.Errorf("the tax is computed from %v of long-term gains, not %v", ty.Input.LongTermGains, ty.LongTermGains)
	}
	if other, err := TaxYearOf(rec, "reb-a", 2026); err != nil || other.LongTermGains != 0 {
		t.Errorf("the 2026 tax has gains %v (%v)", other.LongTermGains, err)
	}
	inK, err := TaxYearOf(rec, "reb-k", 2027)
	must(t, err)
	if inK.ShortTermGains != 0 || inK.LongTermGains != 0 || inK.GainsTax != 0 {
		t.Errorf("rebalance in the 401k realized %v short, %v long", inK.ShortTermGains, inK.LongTermGains)
	}

	// A rebalance moves money between classes: the balance is the same on its date and the account then drifts less.
	for _, id := range []model.AccountID{"a", "k"} {
		proj := "reb-" + string(id)
		if got, was := balanceOf(t, rec, proj, id, on), balanceOf(t, rec, "drift", id, on); !within(got, was, 1e-4) {
			t.Errorf("%s balance on the rebalance date = %v, was %v", id, got, was)
		}
		if got, was := balanceOf(t, rec, proj, id, d(2028, time.March, 1)), balanceOf(t, rec, "drift", id, d(2028, time.March, 1)); !(got < was) {
			t.Errorf("%s a year after the rebalance = %v, want below the drifting %v", id, got, was)
		}
	}
	// The sale is paid in tax in April of the next year, shown as its own cause, and rebalancing nets to nothing.
	bd, err := Breakdown(rec, "reb-a", model.DefaultAssetID("chk"), rec.Today, d(2028, time.December, 31))
	must(t, err)
	var gainsTax float64
	for _, l := range bd.Lines {
		if l.Cause == CauseGainsTax {
			gainsTax += l.Projected
		}
	}
	if !within(gainsTax, -ty.GainsTax, 1e-3) || !within(bd.Total(), bd.End-bd.Start, 1e-3) {
		t.Errorf("checking breakdown: gains tax %v want %v, total %v want %v: %+v", gainsTax, -ty.GainsTax, bd.Total(), bd.End-bd.Start, bd.Lines)
	}
}

func TestRebalanceIntoANewClass(t *testing.T) {
	rec := marketFixture(t)
	on := d(2026, time.July, 1)
	must(t, rec.AddProjection(&model.Projection{Name: "cash", Rebalances: []model.Rebalance{
		{Account: "k", Date: on, Allocation: []model.Weight{{Class: model.ClassCash, Share: 1}}},
	}}))
	end := d(2027, time.January, 1)
	days := on - rec.Today
	total := 600000*grow(0.10, days) + 400000*grow(0.045, days)
	if got, want := balanceOf(t, rec, "cash", "k", end), total*grow(0.03, end-on); !within(got, want, 1e-3) {
		t.Errorf("401k all in cash = %v, want %v", got, want)
	}
}

func TestWithdrawalSellsOldestLotsFirst(t *testing.T) {
	rec := marketFixture(t)
	rec.Account("a").AddSnapshot(rec.Today, 100000)
	contribute, withdraw := d(2026, time.March, 1), d(2027, time.February, 1)
	rec.Budget.Items = []model.BudgetItem{
		{ID: "buy", Kind: model.ItemTransfer, Amount: model.Constant(20000), Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: contribute}, From: "chk", To: "a"},
		{ID: "sell", Kind: model.ItemTransfer, Amount: model.Constant(115000), Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: withdraw}, From: "a", To: "chk"},
	}
	// Each leg's opening lot is sold out and part of the lot the contribution opened.
	type legLots struct {
		rate, opening, bought float64
	}
	days, since := withdraw-rec.Today, withdraw-contribute
	legs := []legLots{{0.10, 60000, 12000}, {0.045, 40000, 8000}}
	var value []float64
	total := 0.0
	for _, l := range legs {
		v := l.opening*grow(l.rate, days) + l.bought*grow(l.rate, since)
		value = append(value, v)
		total += v
	}
	var long, short float64
	for i, l := range legs {
		sold := 115000 * value[i] / total
		open := l.opening * grow(l.rate, days)
		if sold <= open {
			t.Fatalf("leg %d sells %v, which does not reach past its opening lot %v", i, sold, open)
		}
		long += open - l.opening
		rest := sold - open
		short += rest - rest/grow(l.rate, since)
	}
	ty, err := TaxYearOf(rec, "", 2027)
	must(t, err)
	if !within(ty.LongTermGains, long, 1e-3) || !within(ty.ShortTermGains, short, 1e-3) || short <= 0 {
		t.Errorf("gains: %v long and %v short, want %v and %v", ty.LongTermGains, ty.ShortTermGains, long, short)
	}
	if ty.GainsTax < 0 || !within(ty.Input.ShortTermGains, short, 1e-3) {
		t.Errorf("gains tax %v, input %+v", ty.GainsTax, ty.Input)
	}
}

func TestShortTermIsHeldOneYearOrLess(t *testing.T) {
	for _, c := range []struct {
		on        model.Date
		short     bool
		wantLabel string
	}{{d(2027, time.January, 1), true, "a year to the day"}, {d(2027, time.January, 2), false, "a day more than a year"}} {
		rec := marketFixture(t)
		rec.Budget.Items = []model.BudgetItem{
			{ID: "sell", Kind: model.ItemTransfer, Amount: model.Constant(10000), Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: c.on}, From: "a", To: "chk"},
		}
		ty, err := TaxYearOf(rec, "", 2027)
		must(t, err)
		gain := ty.LongTermGains
		if c.short {
			gain = ty.ShortTermGains
		}
		if gain <= 0 || (c.short && ty.LongTermGains != 0) || (!c.short && ty.ShortTermGains != 0) {
			t.Errorf("%s: %v short, %v long", c.wantLabel, ty.ShortTermGains, ty.LongTermGains)
		}
	}
}

func TestSalesInShelteredAccountsAreNotTaxed(t *testing.T) {
	rec := marketFixture(t)
	rec.Budget.Items = []model.BudgetItem{
		{ID: "sell", Kind: model.ItemTransfer, Amount: model.Constant(50000), Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: d(2027, time.June, 1)}, From: "k", To: "chk"},
	}
	ty, err := TaxYearOf(rec, "", 2027)
	must(t, err)
	if ty.ShortTermGains != 0 || ty.LongTermGains != 0 {
		t.Errorf("401k withdrawal realized %v short, %v long", ty.ShortTermGains, ty.LongTermGains)
	}
}

func TestYearlyRebalanceReturnsToTheAllocation(t *testing.T) {
	rec := marketFixture(t)
	rec.Account("a").RebalanceYearly = true
	jan := d(2027, time.January, 1)
	end := d(2027, time.December, 31)
	plain := marketFixture(t)
	if got, was := balanceOf(t, rec, "", "a", jan), balanceOf(t, plain, "", "a", jan); !within(got, was, 1e-4) {
		t.Errorf("the rebalance on 1 January moved the balance: %v, was %v", got, was)
	}
	v := balanceOf(t, rec, "", "a", jan)
	days := end - jan
	if got, want := balanceOf(t, rec, "", "a", end), v*(0.6*grow(0.10, days)+0.4*grow(0.045, days)); !within(got, want, 1e-3) {
		t.Errorf("after a yearly rebalance the year grows %v, want %v", got, want)
	}
	if got, drift := balanceOf(t, rec, "", "a", end), balanceOf(t, plain, "", "a", end); !(got < drift) {
		t.Errorf("rebalanced %v, drifting %v: rebalancing to 60/40 should give up some of the faster class", got, drift)
	}
	// The first of January is a sale of exactly a year after the opening lot: short-term.
	ty, err := TaxYearOf(rec, "", 2027)
	must(t, err)
	if ty.ShortTermGains <= 0 || ty.LongTermGains != 0 {
		t.Errorf("yearly rebalance gains: %v short, %v long", ty.ShortTermGains, ty.LongTermGains)
	}
}

func TestYearlyRebalanceKeepsAnExplicitRebalanceTarget(t *testing.T) {
	rec := marketFixture(t)
	acct := rec.Account("a")
	acct.RebalanceYearly = true
	acct.Allocation = []model.Holding{{Class: model.ClassLargeCap, Percent: 100}}
	must(t, rec.AddProjection(&model.Projection{Name: "to6040", Rebalances: []model.Rebalance{rebalanceTo6040("a", d(2027, time.September, 1))}}))
	lo, hi := d(2027, time.December, 31), d(2028, time.January, 2)
	before, after := balanceOf(t, rec, "to6040", "a", lo), balanceOf(t, rec, "to6040", "a", hi)
	// The yearly rebalance trades only the drift of four months and keeps the bonds: the balance only grows.
	if !(after > before) || !within(after, before, 0.02*before) {
		t.Errorf("balance %v before the yearly rebalance and %v after", before, after)
	}
	rb, err := TaxYearOf(rec, "to6040", 2027)
	must(t, err)
	ty, err := TaxYearOf(rec, "to6040", 2028)
	must(t, err)
	// The bonds bought on 1 September are still held: the year after the yearly rebalance grows at 60/40, not at the stored 100% large cap.
	end, jan := d(2028, time.December, 31), d(2028, time.January, 1)
	if got, want := balanceOf(t, rec, "to6040", "a", end), balanceOf(t, rec, "to6040", "a", jan)*(0.6*grow(0.10, end-jan)+0.4*grow(0.045, end-jan)); !within(got, want, 1e-3) {
		t.Errorf("balance at the end of 2028 is %v, want %v at 60/40", got, want)
	}
	// Four months of drift is a small trade next to the 2027 rebalance itself; selling the bond lot would realize several times more.
	if gain := ty.ShortTermGains + ty.LongTermGains; !(gain < 0.05*(rb.ShortTermGains+rb.LongTermGains)) {
		t.Errorf("2028 realized %v, the 2027 rebalance %v", gain, rb.ShortTermGains+rb.LongTermGains)
	}
}

func TestBreakdownShowsShocksSeparatelyFromGrowth(t *testing.T) {
	rec := marketFixture(t)
	on := d(2026, time.July, 1)
	must(t, rec.AddProjection(&model.Projection{Name: "crash", Shocks: []model.Shock{{Class: model.ClassLargeCap, Date: on, Change: -0.40}}}))
	end := d(2027, time.January, 1)
	bd, err := Breakdown(rec, "crash", model.DefaultAssetID("a"), rec.Today, end)
	must(t, err)
	if len(bd.Lines) != 3 {
		t.Fatalf("lines: %+v", bd.Lines)
	}
	shock := bd.Lines[2]
	if want := -0.4 * 600000 * grow(0.10, on-rec.Today); shock.Cause != CauseShock || shock.Class != model.ClassLargeCap || !within(shock.Projected, want, 1e-3) {
		t.Errorf("shock line %+v, want %v", shock, want)
	}
	for _, l := range bd.Lines[:2] {
		if l.Cause != CauseGrowth || l.Projected <= 0 {
			t.Errorf("growth line %+v", l)
		}
	}
	if !within(bd.Total(), bd.End-bd.Start, 1e-3) {
		t.Errorf("lines sum to %v, the balance changed by %v", bd.Total(), bd.End-bd.Start)
	}
}

func TestRebalanceOfAnAccountWithoutAllocationIsSkipped(t *testing.T) {
	rec := marketFixture(t)
	must(t, rec.AddProjection(&model.Projection{Name: "bad", Rebalances: []model.Rebalance{rebalanceTo6040("chk", d(2026, time.July, 1)), rebalanceTo6040("ghost", d(2026, time.July, 1))}}))
	res, err := Balances(rec, "bad", model.DefaultAssetID("a"), []model.Date{d(2026, time.August, 1)})
	must(t, err)
	if len(res.Skipped) != 2 || res.Skipped[0].Property != "rebalance" || res.Skipped[1].Reason != "account not found" {
		t.Errorf("skipped %+v", res.Skipped)
	}
}
