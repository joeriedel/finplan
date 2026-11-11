// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

func newGrant(t *testing.T, st *store.Store, rec *model.Record) (*model.Record, model.Grant) {
	t.Helper()
	res, err := AddGrant(st, rec, GrantInput{Account: demodata.Equity, Symbol: "acme", Date: model.D(2027, time.February, 1), Value: 100000, Price: 187.50,
		Calendar: demodata.CalendarName, FirstVest: model.D(2027, time.March, 15), YearPercents: []float64{25, 25, 25, 25}, Brokerage: demodata.Brokerage}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return res.Record, *res.Grant
}

func TestCalendarOps(t *testing.T) {
	st, rec := seed(t)
	cal := model.VestCalendar{Name: " Beta ", Rules: []model.VestRule{{Month: time.May, Day: 1}}}
	res, err := NewCalendar(st, rec, cal, Options{DryRun: true})
	if err != nil || res.Calendar.Name != "Beta" || reload(t, st).Calendar("Beta") != nil {
		t.Fatalf("dry run: %v", err)
	}
	if res, err = NewCalendar(st, rec, cal, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if rec.Calendar("Beta") == nil {
		t.Fatal("calendar not saved")
	}
	for name, c := range map[string]model.VestCalendar{
		"taken":   {Name: "Beta", Rules: cal.Rules},
		"30 Feb":  {Name: "G", Rules: []model.VestRule{{Month: time.February, Day: 30}}},
		"no name": {Rules: cal.Rules},
	} {
		if _, err := NewCalendar(st, rec, c, Options{}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := NewCalendar(st, rec, model.VestCalendar{Name: "Beta", Rules: cal.Rules}, Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("taken name: %v", err)
	}
	if _, err := NewCalendar(st, rec, model.VestCalendar{Name: "G", Rules: []model.VestRule{{Month: time.February, Day: 30}}}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("30 Feb: %v", err)
	}
	// Overrides and rules are edited whole; no change is refused.
	ov := []model.DateOverride{{From: model.D(2027, time.May, 1), To: model.D(2027, time.May, 3)}}
	if _, err := EditCalendar(st, rec, "Beta", cal.Rules, nil, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("no change: %v", err)
	}
	if _, err := EditCalendar(st, rec, "Beta", cal.Rules, ov, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Calendar("Beta"); len(got.Overrides) != 1 {
		t.Errorf("override not saved: %+v", got)
	}
	if _, err := EditCalendar(st, rec, "Nope", cal.Rules, nil, Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown calendar: %v", err)
	}
	// A calendar a grant uses cannot lose the vests the grant plans on, or be deleted.
	rec = reload(t, st)
	used := rec.Calendar(demodata.CalendarName)
	if _, err := EditCalendar(st, rec, used.Name, used.Rules[:3], nil, Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("edit that breaks a grant: %v", err)
	}
	if _, err := DeleteCalendar(st, rec, used.Name, Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("delete of a calendar in use: %v", err)
	}
	if res, err = DeleteCalendar(st, rec, "Beta", Options{}); err != nil || res.Removed != "Beta" {
		t.Fatal(err)
	}
	if reload(t, st).Calendar("Beta") != nil {
		t.Error("calendar not deleted")
	}
}

func TestAddGrantAndSchedule(t *testing.T) {
	st, rec := seed(t)
	rec, g := newGrant(t, st, rec)
	if g.Shares != 533 || g.Symbol != "ACME" || g.Rates != model.DefaultWithholding || g.ID == 0 || len(g.VestPercents) != 16 {
		t.Fatalf("grant = %+v", g)
	}
	_, rows, err := GrantSchedule(rec, g.ID)
	if err != nil || len(rows) != 16 || rows[0].Status != VestPlanned || rows[15].Shares != 38 {
		t.Fatalf("schedule: %d rows, %v", len(rows), err)
	}
	if _, err := AddGrant(st, rec, GrantInput{Account: demodata.Equity, Symbol: "ACME", Date: 1, Shares: 10, Value: 5, Price: 1}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("shares and value: %v", err)
	}
	in := GrantInput{Account: demodata.Equity, Symbol: "ACME", Date: model.D(2027, time.February, 1), Shares: 100, Price: 1, Calendar: demodata.CalendarName,
		FirstVest: model.D(2027, time.March, 15), YearPercents: []float64{100}, Brokerage: demodata.Brokerage}
	for name, mod := range map[string]func(*GrantInput){
		"not an equity account": func(i *GrantInput) { i.Account = demodata.Checking },
		"brokerage not one":     func(i *GrantInput) { i.Brokerage = demodata.Checking },
		"unknown calendar":      func(i *GrantInput) { i.Calendar = "none" },
		"first vest not a date": func(i *GrantInput) { i.FirstVest = model.D(2027, time.March, 16) },
		"percents":              func(i *GrantInput) { i.YearPercents = []float64{60, 30} },
		"no symbol":             func(i *GrantInput) { i.Symbol = " " },
		"bad price":             func(i *GrantInput) { i.Price = 0 },
		"bad rate":              func(i *GrantInput) { i.Rates = &model.Withholding{Federal: 1} },
	} {
		bad := in
		mod(&bad)
		if _, err := AddGrant(st, rec, bad, Options{DryRun: true}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	before := len(rec.Account(demodata.Equity).Grants)
	if res, err := AddGrant(st, rec, in, Options{DryRun: true}); err != nil || res.Grant.ID != 0 || len(reload(t, st).Account(demodata.Equity).Grants) != before {
		t.Errorf("dry run: %v", err)
	}
	if _, err := AddSnapshot(st, rec, demodata.Equity, model.D(2026, time.November, 1), 5, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("snapshot on an equity account: %v", err)
	}
}

func TestRecordVestDeposit(t *testing.T) {
	st, rec := seed(t)
	rec, g := newGrant(t, st, rec)
	brokerage := entryCount(t, st, demodata.Brokerage)
	prices := len(rec.Prices.All())
	res, err := RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210}, Options{DryRun: true})
	if err != nil || res.Vest.ID != 0 || entryCount(t, st, demodata.Brokerage) != brokerage {
		t.Fatalf("dry run: %v", err)
	}
	res, err = RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	v := res.Vest
	if v.ID == 0 || v.Shares != 33 || v.Date != model.D(2027, time.March, 15) || v.Gross() != 6930 || v.WithheldAmount() != 2453.22 || v.Net() != 4476.78 {
		t.Fatalf("vest = %+v", v)
	}
	// One transfer of N into the brokerage account from the equity account, named by the vest.
	if entryCount(t, st, demodata.Brokerage) != brokerage+1 || res.Entry == nil || res.Entry.ID != v.TxEntry {
		t.Fatalf("entries %d, entry %+v, vest %+v", entryCount(t, st, demodata.Brokerage), res.Entry, v)
	}
	e, err := st.Entry(v.TxEntry)
	if err != nil || e.Account != demodata.Brokerage || e.Amount != 4476.78 || e.TxKind != model.TxTransfer || e.Counter != demodata.Equity || e.Date != v.Date {
		t.Errorf("deposit = %+v, %v", e, err)
	}
	// The vest's price is observed.
	loaded := res.Record
	if len(loaded.Prices.All()) != prices+1 {
		t.Errorf("prices %d, want %d", len(loaded.Prices.All()), prices+1)
	}
	if p, ok := loaded.Prices.At("ACME", v.Date); !ok || p != 210 || res.Price == nil || res.Price.ID == 0 {
		t.Errorf("price on the vest date = %v, %v", p, ok)
	}
	_, rows, err := GrantSchedule(loaded, g.ID)
	if err != nil || rows[0].Status != VestRecorded || rows[0].Vest.ID != v.ID || rows[0].DateDiff != 0 || rows[0].ShareDiff != 0 || rows[1].Status != VestPlanned {
		t.Errorf("schedule: %+v, %v", rows[:2], err)
	}
	// The planned vest cannot be recorded twice.
	if _, err := RecordVest(st, loaded, VestInput{Grant: g.ID, Planned: 1, Price: 210}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("second vest 1: %v", err)
	}
	if entryCount(t, st, demodata.Brokerage) != brokerage+1 {
		t.Error("a refused vest left a deposit")
	}
	// Recording the next vest on another date and share count shows how it differs from the plan.
	date, shares := model.D(2027, time.June, 28), 30
	res, err = RecordVest(st, loaded, VestInput{Grant: g.ID, Planned: 2, Price: 210, Date: &date, Shares: &shares, Withheld: model.Ptr(1000.0)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Vest.Net() != 30*210-1000 || res.Price == nil {
		t.Errorf("second vest = %+v, price %+v", res.Vest, res.Price)
	}
	_, rows, _ = GrantSchedule(res.Record, g.ID)
	if rows[1].Status != VestRecorded || rows[1].DateDiff != 7 || rows[1].ShareDiff != -3 {
		t.Errorf("schedule row 2: %+v", rows[1])
	}
}

func TestRecordVestTiedToExistingDeposit(t *testing.T) {
	st, rec := seed(t)
	rec, g := newGrant(t, st, rec)
	// The deposit arrives as an ordinary transaction first.
	dep, err := AddTransaction(st, rec, demodata.Brokerage, model.Transaction{Date: model.D(2027, time.March, 17), Amount: 4476.78, Kind: model.TxTransfer, Counter: demodata.Equity, Memo: "RSU"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	before := entryCount(t, st, demodata.Brokerage)
	if _, err := RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210, TxEntry: 99999}, Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown entry: %v", err)
	}
	other, _ := AddTransaction(st, rec, demodata.Checking, model.Transaction{Date: 5, Amount: 1, Kind: model.TxIncome}, Options{})
	rec = reload(t, st)
	if _, err := RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210, TxEntry: other.Entry.ID}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("entry of another account: %v", err)
	}
	res, err := RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210, TxEntry: dep.Entry.ID}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Vest.TxEntry != dep.Entry.ID || res.Entry != nil || entryCount(t, st, demodata.Brokerage) != before {
		t.Errorf("vest %+v, entry %+v, brokerage entries %d (was %d)", res.Vest, res.Entry, entryCount(t, st, demodata.Brokerage), before)
	}
	if _, err := RecordVest(st, res.Record, VestInput{Grant: g.ID, Planned: 2, Price: 210, TxEntry: dep.Entry.ID}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("a deposit tied to two vests: %v", err)
	}
}

func TestRecordVestRefusals(t *testing.T) {
	st, rec := seed(t)
	rec, g := newGrant(t, st, rec)
	early := g.Date.Add(-1)
	big := 534
	for name, in := range map[string]VestInput{
		"before the grant":  {Grant: g.ID, Planned: 1, Price: 210, Date: &early},
		"more than granted": {Grant: g.ID, Planned: 1, Price: 210, Shares: &big},
		"unknown grant":     {Grant: 99999, Planned: 1, Price: 210},
		"planned 0":         {Grant: g.ID, Planned: 0, Price: 210},
		"planned 17":        {Grant: g.ID, Planned: 17, Price: 210},
		"no price":          {Grant: g.ID, Planned: 1},
		"withheld too much": {Grant: g.ID, Planned: 1, Price: 210, Withheld: model.Ptr(1e6)},
		"both":              {Grant: g.ID, Planned: 1, Price: 210, Withheld: model.Ptr(1.0), Rates: &model.Withholding{}},
	} {
		if _, err := RecordVest(st, rec, in, Options{DryRun: true}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// What the grant has left counts what earlier vests took.
	first := 500
	res, err := RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210, Shares: &first}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	over := 34
	if _, err := RecordVest(st, res.Record, VestInput{Grant: g.ID, Planned: 2, Price: 210, Shares: &over}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("more than the remaining shares: %v", err)
	}
	if _, err := RecordVest(st, res.Record, VestInput{Grant: g.ID, Planned: 2, Price: 210, Shares: model.Ptr(33)}, Options{}); err != nil {
		t.Errorf("exactly the remaining shares: %v", err)
	}
}

func TestEquityBalanceThroughOps(t *testing.T) {
	st, rec := seed(t)
	rec, g := newGrant(t, st, rec)
	eq := func(r *model.Record, d model.Date) float64 {
		v, ok := r.Account(demodata.Equity).ActualAt(d)
		if !ok {
			t.Fatalf("no actual on %v", d)
		}
		return v
	}
	base := eq(rec, g.Date)
	// The balance of the grant on its date is its shares at the latest price (the demo has observations from before the grant, so the grant price is not used): the demo's other grants add their own.
	own := func(r *model.Record, d model.Date) float64 {
		a := r.Account(demodata.Equity)
		sum := 0.0
		for _, x := range a.Grants {
			if x.ID != g.ID && x.Date <= d {
				p, ok := r.Prices.At(x.Symbol, d)
				if !ok {
					p = x.Price
				}
				sum += float64(x.Shares-a.VestedBy(x.ID, d)) * p
			}
		}
		return sum
	}
	if math.Abs(base-own(rec, g.Date)-533*233.10) > 1e-6 {
		t.Errorf("on the grant date: %v", base)
	}
	vd := model.D(2027, time.March, 15)
	res, err := RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := res.Record
	if got := eq(r, vd); math.Abs(got-own(r, vd)-500*210) > 1e-6 {
		t.Errorf("after the vest on its date: %v", got)
	}
	if got := eq(r, vd.Add(-1)); math.Abs(got-own(r, vd.Add(-1))-533*233.10) > 1e-6 {
		t.Errorf("the day before the vest: %v", got)
	}
	pr, err := AddPrice(st, r, "acme", vd.Add(30), 225, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := eq(pr.Record, vd.Add(30)); math.Abs(got-own(pr.Record, vd.Add(30))-500*225) > 1e-6 {
		t.Errorf("after a later price: %v", got)
	}
	// Through the asset path the readers use.
	if b, err := pr.Record.Balance(model.DefaultAssetID(demodata.Equity), vd.Add(30)); err != nil || !b.Actual || math.Abs(b.Amount-eq(pr.Record, vd.Add(30))) > 1e-6 {
		t.Errorf("asset balance = %+v, %v", b, err)
	}
}

func TestCorrectAndVoidKeepHistory(t *testing.T) {
	st, rec := seed(t)
	rec, g := newGrant(t, st, rec)
	res, err := RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec, v := res.Record, *res.Vest
	// Vest: correct the price, keep the original, and refuse changes that do nothing or break rules.
	if _, err := CorrectVest(st, rec, v.ID, VestChange{Price: model.Ptr(210.0)}, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("no change: %v", err)
	}
	if _, err := CorrectVest(st, rec, v.ID, VestChange{Shares: model.Ptr(534)}, "", Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("too many shares: %v", err)
	}
	if res, err = CorrectVest(st, rec, v.ID, VestChange{Price: model.Ptr(212.0)}, "typo", Options{DryRun: true}); err != nil || res.Vest.Price != 212 {
		t.Fatal(err)
	}
	if res, err = CorrectVest(st, rec, v.ID, VestChange{Price: model.Ptr(212.0)}, "typo", Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Record.Vest(v.ID); got == nil || got.Price != 212 {
		t.Fatalf("corrected vest = %+v", got)
	}
	revs, err := FactHistory(st, v.ID, store.FactVest)
	if err != nil || len(revs) != 2 || revs[0].Vest.Price != 210 || revs[1].Vest.Price != 212 || revs[1].Note != "typo" {
		t.Fatalf("vest history: %+v, %v", revs, err)
	}
	// A grant with a recorded vest cannot be voided; void the vest first.
	rec = res.Record
	if _, err := VoidGrant(st, rec, g.ID, "", Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("void grant with a vest: %v", err)
	}
	if res, err = VoidVest(st, rec, v.ID, "wrong", Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Record.Vest(v.ID); got != nil {
		t.Error("a voided vest is in the record")
	}
	if _, err := VoidVest(st, res.Record, v.ID, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("void twice: %v", err)
	}
	if res, err = VoidGrant(st, res.Record, g.ID, "wrong", Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Record.Grant(g.ID); got != nil {
		t.Error("a voided grant is in the record")
	}
	if revs, err = FactHistory(st, g.ID, store.FactGrant); err != nil || len(revs) != 2 || !revs[1].Void || revs[0].Grant.Shares != 533 {
		t.Errorf("grant history: %+v, %v", revs, err)
	}
	// A vest cannot come back while its grant is voided; the grant can, corrected.
	if _, err := CorrectVest(st, res.Record, v.ID, VestChange{Price: model.Ptr(211.0)}, "", Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("restore a vest of a voided grant: %v", err)
	}
	if res, err = CorrectGrant(st, res.Record, g.ID, GrantChange{Shares: model.Ptr(540)}, "restored, 540", Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Record.Grant(g.ID); got == nil || got.Shares != 540 {
		t.Fatalf("restored grant = %+v", got)
	}
	if revs, _ = FactHistory(st, g.ID, store.FactGrant); len(revs) != 3 {
		t.Errorf("grant history after restore: %d", len(revs))
	}
	// Changing the first vest regenerates the vest percents; shares under what has vested are refused.
	if res, err = CorrectGrant(st, res.Record, g.ID, GrantChange{YearPercents: []float64{50, 50}}, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Record.Grant(g.ID); len(got.VestPercents) != 8 {
		t.Errorf("percents after year percents changed: %v", got.VestPercents)
	}
	if res, err = CorrectGrant(st, res.Record, g.ID, GrantChange{Value: model.Ptr(50000.0)}, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := res.Record.Grant(g.ID); got.Shares != 266 || got.Value != 50000 {
		t.Errorf("shares from value: %+v", got)
	}
	// Price: add, correct, void, history; a void changes the balance back.
	pr, err := AddPrice(st, res.Record, "ACME", model.D(2027, time.May, 1), 250, Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := pr.Price.ID
	if _, err := AddPrice(st, pr.Record, "", 1, 1, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("price without a symbol: %v", err)
	}
	if pr, err = CorrectPrice(st, pr.Record, id, PriceChange{Price: model.Ptr(251.0)}, "typo", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CorrectPrice(st, pr.Record, id, PriceChange{Price: model.Ptr(251.0)}, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("price no change: %v", err)
	}
	if pr, err = VoidPrice(st, pr.Record, id, "gone", Options{}); err != nil {
		t.Fatal(err)
	}
	if p, _ := pr.Record.Prices.At("ACME", model.D(2027, time.May, 2)); p == 251 || p == 250 {
		t.Errorf("voided price still counts: %v", p)
	}
	if revs, err = FactHistory(st, id, store.FactPrice); err != nil || len(revs) != 3 || revs[0].Price.Price != 250 || revs[1].Price.Price != 251 || !revs[2].Void {
		t.Errorf("price history: %+v, %v", revs, err)
	}
	if _, err := FactHistory(st, id, store.FactGrant); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("history of the wrong kind: %v", err)
	}
	if _, err := FactHistory(st, 99999, store.FactPrice); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("history of an unknown fact: %v", err)
	}
}

func TestCorrectGrantWithRecordedVests(t *testing.T) {
	st, rec := seed(t)
	rec, g := newGrant(t, st, rec)
	res, err := RecordVest(st, rec, VestInput{Grant: g.ID, Planned: 1, Price: 210}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = res.Record
	refused := map[string]GrantChange{
		"year percents":  {YearPercents: []float64{50, 50}},
		"cliff":          {CliffMonths: model.Ptr(12)},
		"first vest":     {FirstVest: model.Ptr(model.D(2027, time.June, 21))},
		"vest percents":  {VestPercents: []float64{10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 5, 5, 0, 0, 0, 0}},
		"grant date":     {Date: model.Ptr(model.D(2027, time.April, 1))},
		"shares to void": {Shares: model.Ptr(10)},
	}
	for name, ch := range refused {
		if _, err := CorrectGrant(st, rec, g.ID, ch, "", Options{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := CorrectGrant(st, rec, g.ID, GrantChange{YearPercents: []float64{25, 25, 25, 25}}, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same plan: %v", err)
	}
	for name, ch := range map[string]GrantChange{
		"rates":  {Rates: &model.Withholding{Federal: 0.2, State: 0.05, FICA: 0.0765}},
		"price":  {Price: model.Ptr(190.0)},
		"shares": {Shares: model.Ptr(540)},
	} {
		if _, err := CorrectGrant(st, rec, g.ID, ch, "", Options{}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := CorrectGrant(st, rec, g.ID, GrantChange{YearPercents: []float64{50, 50}}, "", Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("plan change: %v", err)
	}
}

func TestAccountEditAndDeleteSeeGrants(t *testing.T) {
	st, rec := seed(t)
	rec, g := newGrant(t, st, rec)
	checking := model.TypeChecking
	for _, id := range []model.AccountID{g.Account, g.Brokerage} {
		if _, err := EditAccount(st, rec, id, AccountChange{Type: &checking}, Options{}); !errors.Is(err, ErrInUse) {
			t.Errorf("type change of %q: %v", id, err)
		}
		if _, err := DeleteAccount(st, rec, id, Options{}); !errors.Is(err, ErrInUse) {
			t.Errorf("delete of %q: %v", id, err)
		}
	}
	if _, err := CloseAccount(st, rec, g.Account, model.D(2020, time.January, 1), Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("close before the grant: %v", err)
	}
	// A brokerage account that only a grant names is not deletable either.
	b := model.Account{ID: "gbrk", Name: "Grant brokerage", Type: model.TypeBrokerage, Tax: model.Taxable}
	if _, err := NewAccount(st, rec, b, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	res, err := AddGrant(st, rec, GrantInput{Account: g.Account, Symbol: "acme", Date: g.Date, Shares: 10, Price: 1, Calendar: g.Calendar,
		FirstVest: g.FirstVest, YearPercents: []float64{100}, Brokerage: "gbrk"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteAccount(st, res.Record, "gbrk", Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("delete of a brokerage account only a grant names: %v", err)
	}
}
