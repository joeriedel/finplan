// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// equityBalance is the equity account's balance on d.
func equityBalance(t *testing.T, a *app, d model.Date) float64 {
	t.Helper()
	b, err := a.rec.Balance(model.DefaultAssetID(demodata.Equity), d)
	if err != nil {
		t.Fatal(err)
	}
	return b.Amount
}

// liveEntries is the number of live entries of the account.
func liveEntries(t *testing.T, a *app, id model.AccountID) int {
	t.Helper()
	es, err := ops.ListEntries(a.st, id, false)
	if err != nil {
		t.Fatal(err)
	}
	return len(es)
}

// firstPlanned is the first planned row of the demo's first grant.
func firstPlanned(t *testing.T, a *app) (*model.Grant, ops.ScheduleRow) {
	t.Helper()
	g, rows, err := ops.GrantSchedule(a.rec, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Vest == nil {
			return g, r
		}
	}
	t.Fatal("no planned vest")
	return nil, ops.ScheduleRow{}
}

func TestGrantAndScheduleRows(t *testing.T) {
	a := demoApp(t)
	acct := a.rec.Account(demodata.Equity)
	rows := grantRows(a.rec, acct)
	if len(rows) != 2 {
		t.Fatalf("%d grant rows", len(rows))
	}
	r := rows[0]
	vested := 0
	for _, v := range acct.Vests {
		if v.Grant == r.ID {
			vested += v.Shares
		}
	}
	px, _ := a.rec.Prices.At(demodata.Symbol, model.MaxDate)
	if r.Symbol != demodata.Symbol || r.Vested != vested || r.Unvested != r.Shares-vested || vested == 0 || r.Unvested == 0 || r.UnvestedValue != float64(r.Unvested)*px {
		t.Errorf("row %+v, latest price %v", r, px)
	}

	_, sched, err := ops.GrantSchedule(a.rec, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec, planned := scheduleCells(sched[0]), scheduleCells(sched[len(sched)-1])
	if rec[3] != ops.VestRecorded || rec[4] != formatDollars(sched[0].Vest.Price) || rec[5] != formatDollars(sched[0].Vest.Net()) {
		t.Errorf("recorded cells %v", rec)
	}
	if planned[3] != ops.VestPlanned || planned[4] != "" || planned[5] != "" {
		t.Errorf("planned cells %v", planned)
	}
	if signed(0, "day") != "-" || signed(3, "day") != "+3 days" || signed(-1, "share") != "-1 share" {
		t.Error("signed")
	}
}

func TestAddGrantFormByValue(t *testing.T) {
	a := demoApp(t)
	acct := a.rec.Account(demodata.Equity)
	f := newGrantForm(acct)
	if !f.untouched() || grantProblem(a.st, a.rec, acct.ID, f) == "" {
		t.Fatalf("a fresh form is untouched and cannot be added: %+v", f)
	}
	if f.symbol != demodata.Symbol || f.fed != "22" || f.state != "5.75" || f.fica != "7.65" {
		t.Errorf("prefill %+v", f)
	}
	f.amount, f.price, f.date, f.years = "75000", "210", "2026-11-08", "25/25/25/25"
	f.first = firstVestChoices(a.rec, f.calendar, f.date)[0]
	if f.untouched() {
		t.Error("a typed form is touched")
	}
	pv := previewGrant(a.rec, f)
	if !pv.HasShares || pv.Shares != model.SharesFor(75000, 210) || pv.Shares != 357 || len(pv.Plan) != 16 || pv.PlanErr != "" {
		t.Fatalf("preview %+v", pv)
	}
	total := 0
	for _, p := range pv.Plan {
		total += p.Shares
		if p.Percent != 6.25 {
			t.Errorf("percent %v", p.Percent)
		}
	}
	if total != 357 {
		t.Errorf("planned shares add up to %d", total)
	}
	if p := grantProblem(a.st, a.rec, acct.ID, f); p != "" {
		t.Fatal(p)
	}
	in, _ := f.input(acct.ID)
	if !a.addEntry("add grant", func() (*ops.Result, error) { return ops.AddGrant(a.st, a.rec, in, ops.Options{}) }) {
		t.Fatal("add refused")
	}
	if g := a.rec.Account(demodata.Equity).Grants; len(g) != 3 || g[2].Shares != 357 || g[2].Value != 75000 || g[2].Rates != model.DefaultWithholding {
		t.Errorf("grants %+v", g)
	}
	f.years = "30/30"
	if pv := previewGrant(a.rec, f); pv.PlanErr == "" {
		t.Error("percents that add up to 60 give no schedule")
	}
	f.byShares, f.amount = true, "12.5"
	if _, err := f.derive(); err == nil {
		t.Error("fractional shares are refused")
	}
}

func TestParseRatesAndPercents(t *testing.T) {
	w, err := parseRates("22", "5.75", "7.65")
	if err != nil || w != model.DefaultWithholding {
		t.Errorf("%+v %v", w, err)
	}
	if _, err := parseRates("x", "1", "1"); err == nil {
		t.Error("a bad rate is refused")
	}
	p, err := parseYearPercents("40/30/20/10")
	if err != nil || !reflect.DeepEqual(p, []float64{40, 30, 20, 10}) {
		t.Errorf("%v %v", p, err)
	}
	if _, err := parseYearPercents(" "); err == nil {
		t.Error("blank percents are refused")
	}
}

func TestVestFormUndoRedo(t *testing.T) {
	a := demoApp(t)
	g, row := firstPlanned(t, a)
	f := newVestForm(a.rec, g, row)
	if f.date != row.Date.String() || f.shares != strconv.Itoa(row.Shares) || f.fed != "22" || f.price == "" {
		t.Fatalf("prefill %+v", f)
	}
	base, err := f.figures()
	if err != nil {
		t.Fatal(err)
	}
	f.fed = "24"
	fg, _ := f.figures()
	wantW := math.Round(fg.V*(0.24+0.0575+0.0765)*100) / 100
	if fg.V != base.V || fg.W == base.W || fg.W != wantW || fg.N != math.Round((fg.V-fg.W)*100)/100 {
		t.Errorf("figures %+v after %+v", fg, base)
	}
	if p := vestProblem(a.st, a.rec, *f); p != "" {
		t.Fatal(p)
	}

	when := row.Date
	lastPx := priceRows(a.rec, demodata.Symbol)[0].Date
	vestPx, _ := strconv.ParseFloat(f.price, 64)
	vests, brokerage := len(a.rec.Account(demodata.Equity).Vests), liveEntries(t, a, demodata.Brokerage)
	prices, bal := len(a.rec.Prices.All()), equityBalance(t, a, lastPx)
	vested := a.rec.Account(demodata.Equity).VestedBy(g.ID, model.MaxDate)
	in, _ := f.input()
	if !a.recordVest("record vest", func() (*ops.Result, error) { return ops.RecordVest(a.st, a.rec, in, ops.Options{}) }) {
		t.Fatal("not recorded")
	}
	acct := a.rec.Account(demodata.Equity)
	if len(acct.Vests) != vests+1 || liveEntries(t, a, demodata.Brokerage) != brokerage+1 || len(a.rec.Prices.All()) != prices+1 {
		t.Fatalf("recorded: %d vests, %d deposits, %d prices", len(acct.Vests), liveEntries(t, a, demodata.Brokerage), len(a.rec.Prices.All()))
	}
	after := equityBalance(t, a, when)
	if want := bal - float64(row.Shares)*vestPx; bal == 0 || math.Abs(after-want) > 0.01 {
		t.Errorf("balance %v after the vest, want %v less the vested shares at %v", after, bal, vestPx)
	}
	last := acct.Vests[len(acct.Vests)-1]
	if last.Rates.Federal != 0.24 || last.Net() != fg.N {
		t.Errorf("vest %+v", last)
	}

	a.undo()
	acct = a.rec.Account(demodata.Equity)
	if len(acct.Vests) != vests || acct.VestedBy(g.ID, model.MaxDate) != vested || liveEntries(t, a, demodata.Brokerage) != brokerage || len(a.rec.Prices.All()) != prices || equityBalance(t, a, lastPx) != bal {
		t.Fatalf("undo left %d vests, %d deposits, %d prices", len(acct.Vests), liveEntries(t, a, demodata.Brokerage), len(a.rec.Prices.All()))
	}
	a.redo()
	acct = a.rec.Account(demodata.Equity)
	if len(acct.Vests) != vests+1 || liveEntries(t, a, demodata.Brokerage) != brokerage+1 || len(a.rec.Prices.All()) != prices+1 || equityBalance(t, a, when) != after {
		t.Fatalf("redo left %d vests, %d deposits, %d prices", len(acct.Vests), liveEntries(t, a, demodata.Brokerage), len(a.rec.Prices.All()))
	}
	if v := acct.Vests[len(acct.Vests)-1]; v.TxEntry != last.TxEntry || v.ID != last.ID {
		t.Errorf("redo restored %+v, recorded %+v", v, last)
	}
}

func TestTieToDeposit(t *testing.T) {
	a := demoApp(t)
	g, row := firstPlanned(t, a)
	add := func(d model.Date, amount float64) int64 {
		kind := model.TxIncome
		if amount < 0 {
			kind = model.TxExpense
		}
		var id int64
		if !a.addEntry("deposit", func() (*ops.Result, error) {
			res, err := ops.AddTransaction(a.st, a.rec, demodata.Brokerage, model.Transaction{Date: d, Amount: amount, Kind: kind}, ops.Options{})
			if err == nil {
				id = res.Entry.ID
			}
			return res, err
		}) {
			t.Fatal("deposit refused")
		}
		return id
	}
	near, nearer := add(row.Date.Add(-10), 500), add(row.Date.Add(3), 600)
	add(row.Date.Add(15), 700)
	add(row.Date, -50)
	voided := add(row.Date.Add(1), 800)
	if !a.addEntry("void", func() (*ops.Result, error) { return ops.VoidEntry(a.st, a.rec, voided, "", ops.Options{}) }) {
		t.Fatal("void refused")
	}
	deps, err := unmatchedDeposits(a.st, a.rec, demodata.Brokerage, row.Date)
	if err != nil || len(deps) != 2 || deps[0].ID != nearer || deps[1].ID != near {
		t.Fatalf("unmatched %+v %v", deps, err)
	}

	f := newVestForm(a.rec, g, row)
	f.tie = true
	if vestProblem(a.st, a.rec, *f) == "" {
		t.Error("a tied vest needs its deposit picked")
	}
	f.tx = nearer
	before := liveEntries(t, a, demodata.Brokerage)
	in, _ := f.input()
	if !a.recordVest("record vest", func() (*ops.Result, error) { return ops.RecordVest(a.st, a.rec, in, ops.Options{}) }) {
		t.Fatal("not recorded")
	}
	if n := liveEntries(t, a, demodata.Brokerage); n != before {
		t.Errorf("tying recorded %d new entries", n-before)
	}
	if deps, _ := unmatchedDeposits(a.st, a.rec, demodata.Brokerage, row.Date); len(deps) != 1 || deps[0].ID != near {
		t.Errorf("a tied deposit is still offered: %+v", deps)
	}
	// Undoing a tied recording voids the vest and its price but leaves the deposit, which it did not create.
	a.undo()
	if n := liveEntries(t, a, demodata.Brokerage); n != before {
		t.Errorf("undo left %d entries, want %d", n, before)
	}
}

func TestAddPriceChangesBalance(t *testing.T) {
	a := demoApp(t)
	d := a.rec.Today
	lastPx := priceRows(a.rec, demodata.Symbol)[0]
	before := equityBalance(t, a, lastPx.Date)
	unvested := before / lastPx.Price
	f := priceForm{d.String(), "300"}
	if p := priceProblem(a.st, a.rec, demodata.Symbol, f); p != "" {
		t.Fatal(p)
	}
	dd, px, _ := f.read()
	if !a.addEntry("add price", func() (*ops.Result, error) {
		return ops.AddPrice(a.st, a.rec, demodata.Symbol, dd, px, ops.Options{})
	}) {
		t.Fatal("price refused")
	}
	if after := equityBalance(t, a, d); before == 0 || math.Abs(after-unvested*300) > 0.01 {
		t.Errorf("balance %v then %v, want %v", before, after, unvested*300)
	}
	if rows := priceRows(a.rec, demodata.Symbol); rows[0].Price != 300 || rows[0].Date != dd {
		t.Errorf("newest price row %+v", rows[0])
	}
	if priceProblem(a.st, a.rec, demodata.Symbol, priceForm{"2026-01-01", "-1"}) == "" {
		t.Error("a negative price is refused")
	}
}

func TestEditCalendarRegeneratesAndUndoes(t *testing.T) {
	a := demoApp(t)
	d := newCalDraft()
	if !d.untouched() || calendarProblem(a.st, a.rec, d) == "" {
		t.Fatal("a new calendar draft is untouched and cannot be saved")
	}
	d.name, d.rules[0].day = "Test vests", "15"
	if p := calendarProblem(a.st, a.rec, d); p != "" {
		t.Fatal(p)
	}
	c, _ := d.calendar()
	if !a.edit("add vest calendar", func() (*ops.Result, error) { return ops.NewCalendar(a.st, a.rec, c, ops.Options{}) }) {
		t.Fatal("not added")
	}
	from, to := a.rec.Today, a.rec.Today.Add(3*366)
	cal := a.rec.Calendar("Test vests")
	d = draftOfCalendar(cal)
	before := cal.Dates(from, to)
	if len(before) < 3 || before[0].String()[8:] != "15" {
		t.Fatalf("dates %v", before)
	}
	orig := a.rec

	d.rules[0].day = "20"
	edited, _ := d.calendar()
	if p := calendarProblem(a.st, a.rec, d); p != "" {
		t.Fatal(p)
	}
	if !a.edit("edit vest calendar", func() (*ops.Result, error) {
		return ops.EditCalendar(a.st, a.rec, edited.Name, edited.Rules, edited.Overrides, ops.Options{})
	}) {
		t.Fatal("not edited")
	}
	after := a.rec.Calendar("Test vests").Dates(from, to)
	if after[0].String()[8:] != "20" || after[0] == before[0] {
		t.Fatalf("regenerated %v", after)
	}

	a.undo()
	if !reflect.DeepEqual(a.rec.Calendars, orig.Calendars) {
		t.Errorf("undo left %+v, want %+v", a.rec.Calendar("Test vests"), cal)
	}
	a.undo()
	if a.rec.Calendar("Test vests") != nil {
		t.Error("undo of the add left the calendar")
	}

	// An override moves one generated date; a rule change drops overrides of dates it no longer generates.
	demo := a.rec.Calendar(demodata.CalendarName)
	dd := draftOfCalendar(demo)
	gen := demo.Dates(from, to)[0]
	dd.overrides = moveOverride(dd.overrides, gen, gen.Add(1))
	oc, _ := dd.calendar()
	if moved := oc.Dates(from, to)[0]; moved != gen.Add(1) {
		t.Errorf("override moved %v to %v", gen, moved)
	}
	dd.rules[0] = ruleDraft{month: time.March, kind: ruleFixed, day: "31"}
	if _, err := dd.calendar(); err != nil {
		t.Fatal(err)
	}
	if p := calendarProblem(a.st, a.rec, dd); p == "" {
		t.Error("a rule that leaves the grants' first vests off the calendar is refused")
	}
}

func TestRuleHint(t *testing.T) {
	today := model.D(2026, time.November, 11)
	for _, c := range []struct {
		d    ruleDraft
		want string
	}{
		{ruleDraft{month: time.March, kind: 3, weekday: time.Monday}, "3rd Monday \u00b7 next: 2027-03-15"},
		{ruleDraft{month: time.December, kind: 2, weekday: time.Monday}, "2nd Monday \u00b7 next: 2026-12-14"},
		{ruleDraft{month: time.November, kind: ruleFixed, day: "11"}, "day 11 \u00b7 next: 2026-11-11"},
		{ruleDraft{month: time.November, kind: ruleFixed, day: "10"}, "day 10 \u00b7 next: 2027-11-10"},
		{ruleDraft{month: time.February, kind: ruleLast, weekday: time.Friday}, "last Friday \u00b7 next: 2027-02-26"},
		{ruleDraft{month: time.June, kind: ruleFixed, day: "31"}, ""},
		{ruleDraft{month: time.June, kind: ruleFixed}, ""},
	} {
		if got := ruleHint(c.d, today); got != c.want {
			t.Errorf("%+v: %q, want %q", c.d, got, c.want)
		}
	}
}

func TestMoveAndRemoveOverride(t *testing.T) {
	d1, d2 := model.D(2027, time.March, 15), model.D(2027, time.June, 21)
	moved := moveOverride(nil, d2, d2.Add(1))
	moved = moveOverride(moved, d1, d1.Add(2))
	if want := []model.DateOverride{{From: d1, To: d1.Add(2)}, {From: d2, To: d2.Add(1)}}; !reflect.DeepEqual(moved, want) {
		t.Fatalf("moved %v, want %v", moved, want)
	}
	// A date that is already an override's target updates that override rather than chaining a second one.
	again := moveOverride(moved, d1.Add(2), d1.Add(5))
	if want := []model.DateOverride{{From: d1, To: d1.Add(5)}, {From: d2, To: d2.Add(1)}}; !reflect.DeepEqual(again, want) {
		t.Errorf("updated %v, want %v", again, want)
	}
	// Moving back to the generated date removes the override.
	back := moveOverride(again, d1.Add(5), d1)
	if want := []model.DateOverride{{From: d2, To: d2.Add(1)}}; !reflect.DeepEqual(back, want) {
		t.Errorf("moved back %v, want %v", back, want)
	}
	if got := removeOverride(moved, d2); !reflect.DeepEqual(got, []model.DateOverride{{From: d1, To: d1.Add(2)}}) || len(moved) != 2 {
		t.Errorf("removed %v from %v", got, moved)
	}
}

func TestMoveVestDate(t *testing.T) {
	a := demoApp(t)
	g := &a.rec.Account(demodata.Equity).Grants[0]
	_, rows, err := ops.GrantSchedule(a.rec, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	var row ops.ScheduleRow
	for _, r := range rows {
		if r.Vest == nil {
			row = r
			break
		}
	}
	f := moveForm{grant: g.ID, planned: row.Index, from: row.Date}
	if !f.untouched() || moveProblem(a.st, a.rec, g, f) == "" {
		t.Fatal("a blank form is untouched and cannot be saved")
	}
	f.to = "soon"
	if moveProblem(a.st, a.rec, g, f) == "" {
		t.Error("a date that does not parse is refused")
	}
	f.to = row.Date.String()
	if p := moveProblem(a.st, a.rec, g, f); p != noChangeText {
		t.Errorf("moving to its own date: %q", p)
	}
	f.to = row.Date.Add(1).String()
	if p := moveProblem(a.st, a.rec, g, f); p != "" {
		t.Fatal(p)
	}
	cal := a.rec.Calendar(g.Calendar)
	if !a.edit("move vest date", func() (*ops.Result, error) {
		return ops.EditCalendar(a.st, a.rec, cal.Name, cal.Rules, moveOverride(cal.Overrides, f.from, row.Date.Add(1)), ops.Options{})
	}) {
		t.Fatal("not moved")
	}
	if !a.rec.Calendar(g.Calendar).Has(row.Date.Add(1)) || a.rec.Calendar(g.Calendar).Has(row.Date) {
		t.Error("the vest did not move")
	}
}

func TestGrantDraftUntouched(t *testing.T) {
	a := demoApp(t)
	g := &a.rec.Account(demodata.Equity).Grants[0]
	d := draftOfGrant(g)
	if !d.untouched(g) {
		t.Error("the draft of the grant is untouched")
	}
	d.note = "typo"
	if d.untouched(g) {
		t.Error("a note touches the form")
	}
	d = draftOfGrant(g)
	d.price += "1"
	if d.untouched(g) {
		t.Error("a changed price touches the form")
	}
}

// recordDemoVest records the first planned vest of the demo's first grant as one undo step.
func recordDemoVest(t *testing.T, a *app) (*model.Grant, model.RecordedVest) {
	t.Helper()
	g, row := firstPlanned(t, a)
	in, err := newVestForm(a.rec, g, row).input()
	if err != nil {
		t.Fatal(err)
	}
	if !a.recordVest("record vest", func() (*ops.Result, error) { return ops.RecordVest(a.st, a.rec, in, ops.Options{}) }) {
		t.Fatal("not recorded")
	}
	acct := a.rec.Account(demodata.Equity)
	return g, acct.Vests[len(acct.Vests)-1]
}

func TestRedoLeavesAVestTheUserVoided(t *testing.T) {
	a := demoApp(t)
	_, v := recordDemoVest(t, a)
	vests, brokerage := len(a.rec.Account(demodata.Equity).Vests), liveEntries(t, a, demodata.Brokerage)
	if !a.addEntry("void vest", func() (*ops.Result, error) { return ops.VoidVest(a.st, a.rec, v.ID, "", ops.Options{}) }) {
		t.Fatal("void refused")
	}
	a.undo()
	if liveEntries(t, a, demodata.Brokerage) != brokerage-1 {
		t.Fatal("undo left the deposit")
	}
	a.redo()
	if n := len(a.rec.Account(demodata.Equity).Vests); n != vests-1 {
		t.Errorf("redo revived the vest the user voided: %d vests, want %d", n, vests-1)
	}
	if liveEntries(t, a, demodata.Brokerage) != brokerage {
		t.Error("redo did not restore the deposit")
	}
	a.undo()
	if liveEntries(t, a, demodata.Brokerage) != brokerage-1 || len(a.rec.Account(demodata.Equity).Vests) != vests-1 {
		t.Error("undo after redo changed more than redo restored")
	}
}

func TestRefusedRedoLeavesNoDepositWithoutItsVest(t *testing.T) {
	a := demoApp(t)
	g, _ := recordDemoVest(t, a)
	brokerage := liveEntries(t, a, demodata.Brokerage)
	a.undo()
	// Shrinking the grant to what it has vested leaves no shares for the vest the redo would record again.
	shares := a.rec.Account(demodata.Equity).VestedBy(g.ID, model.MaxDate)
	if !a.addEntry("correct grant", func() (*ops.Result, error) {
		return ops.CorrectGrant(a.st, a.rec, g.ID, ops.GrantChange{Shares: &shares}, "", ops.Options{})
	}) {
		t.Fatal("correction refused")
	}
	a.redo()
	if n := liveEntries(t, a, demodata.Brokerage); n != brokerage-1 {
		t.Errorf("the refused redo restored the deposit: %d entries, want %d", n, brokerage-1)
	}
	if _, ok := a.history.PeekRedo(); !ok {
		t.Error("the refused redo left the history")
	}
}

func TestGrantFormKeepsAnEmptiedRate(t *testing.T) {
	a := demoApp(t)
	acct := a.rec.Account(demodata.Equity)
	var u equityUI
	u.enter(acct)
	u.grant.fed, u.grant.amount = "", "5"
	u.enter(acct)
	if u.grant.fed != "" || u.grant.amount != "5" {
		t.Errorf("the form was reset: %+v", u.grant)
	}
	u.enter(a.rec.Account(demodata.Brokerage))
	u.enter(acct)
	if u.grant.fed != "22" || u.grant.amount != "" {
		t.Errorf("a form for another account was kept: %+v", u.grant)
	}
}
