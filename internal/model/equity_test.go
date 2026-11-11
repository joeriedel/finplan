// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"math"
	"slices"
	"testing"
	"time"
)

func acmeCalendar() *VestCalendar {
	return &VestCalendar{Name: "Acme", Rules: []VestRule{
		{Month: time.March, Nth: 3, Weekday: time.Monday},
		{Month: time.June, Nth: 3, Weekday: time.Monday},
		{Month: time.September, Nth: 3, Weekday: time.Monday},
		{Month: time.December, Nth: 2, Weekday: time.Monday},
	}}
}

func dateStrings(ds []Date) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.String())
	}
	return out
}

func TestCalendarDates(t *testing.T) {
	cal := acmeCalendar()
	if err := cal.Validate(); err != nil {
		t.Fatal(err)
	}
	// Worked out on a wall calendar, not by the rule's own arithmetic.
	want := []string{
		"2026-03-16", "2026-06-15", "2026-09-21", "2026-12-14",
		"2027-03-15", "2027-06-21", "2027-09-20", "2027-12-13",
		"2028-03-20", "2028-06-19", "2028-09-18", "2028-12-11",
		"2029-03-19", "2029-06-18", "2029-09-17", "2029-12-10",
		"2030-03-18", "2030-06-17", "2030-09-16", "2030-12-09",
	}
	got := dateStrings(cal.Dates(D(2026, time.January, 1), D(2030, time.December, 31)))
	if !slices.Equal(got, want) {
		t.Errorf("dates 2026-2030 = %v", got)
	}
	// The range is inclusive.
	if got := cal.Dates(D(2026, time.June, 15), D(2026, time.September, 21)); len(got) != 2 {
		t.Errorf("inclusive range: %v", dateStrings(got))
	}
	if !cal.Has(D(2027, time.March, 15)) || cal.Has(D(2027, time.March, 16)) {
		t.Error("Has")
	}
}

func TestCalendarLeapYearAndLastWeekday(t *testing.T) {
	cal := &VestCalendar{Name: "Feb", Rules: []VestRule{{Month: time.February, Nth: NthLast, Weekday: time.Tuesday}}}
	// 29 February 2028 is a Tuesday; 2027 and 2029 have 28 days and the last Tuesdays are the 23rd and the 27th.
	want := []string{"2027-02-23", "2028-02-29", "2029-02-27"}
	if got := dateStrings(cal.Dates(D(2027, 1, 1), D(2029, 12, 31))); !slices.Equal(got, want) {
		t.Errorf("last Tuesday of February = %v", got)
	}
	fixed := &VestCalendar{Name: "Fixed", Rules: []VestRule{{Month: time.February, Day: 28}, {Month: time.January, Day: 31}}}
	if err := fixed.Validate(); err != nil {
		t.Error(err)
	}
	if got := dateStrings(fixed.Dates(D(2028, 1, 1), D(2028, 12, 31))); !slices.Equal(got, []string{"2028-01-31", "2028-02-28"}) {
		t.Errorf("fixed days = %v", got)
	}
}

func TestCalendarRefusesInvalidRules(t *testing.T) {
	mon := time.Monday
	for name, rules := range map[string][]VestRule{
		"no rules":             nil,
		"29 February":          {{Month: time.February, Day: 29}},
		"30 February":          {{Month: time.February, Day: 30}},
		"31 April":             {{Month: time.April, Day: 31}},
		"day 0 and no nth":     {{Month: time.April}},
		"day 32":               {{Month: time.January, Day: 32}},
		"day and nth":          {{Month: time.May, Day: 3, Nth: 2, Weekday: mon}},
		"5th weekday":          {{Month: time.May, Nth: 5, Weekday: mon}},
		"nth -2":               {{Month: time.May, Nth: -2, Weekday: mon}},
		"month 13":             {{Month: 13, Day: 1}},
		"two rules in a month": {{Month: time.May, Day: 1}, {Month: time.May, Day: 2}},
	} {
		if err := (&VestCalendar{Name: "x", Rules: rules}).Validate(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := (&VestCalendar{Rules: []VestRule{{Month: time.May, Day: 1}}}).Validate(); err == nil {
		t.Error("an unnamed calendar is valid")
	}
}

func TestCalendarOverride(t *testing.T) {
	cal := acmeCalendar()
	cal.Overrides = []DateOverride{{From: D(2027, time.March, 15), To: D(2027, time.March, 18)}}
	if err := cal.Validate(); err != nil {
		t.Fatal(err)
	}
	got := dateStrings(cal.Dates(D(2027, time.January, 1), D(2027, time.June, 30)))
	if !slices.Equal(got, []string{"2027-03-18", "2027-06-21"}) {
		t.Errorf("with the override: %v", got)
	}
	// A date moved out of the range is gone from it, and one moved in appears.
	if got := cal.Dates(D(2027, time.March, 16), D(2027, time.March, 31)); len(got) != 1 {
		t.Errorf("moved into the range: %v", dateStrings(got))
	}
	if got := cal.Dates(D(2027, time.March, 1), D(2027, time.March, 16)); len(got) != 0 {
		t.Errorf("moved out of the range: %v", dateStrings(got))
	}
	cal.Overrides = []DateOverride{{From: D(2027, time.March, 16), To: D(2027, time.March, 18)}}
	if cal.Validate() == nil {
		t.Error("an override of a date the rules do not generate is valid")
	}
	cal.Overrides = []DateOverride{{From: D(2027, time.March, 15), To: D(2027, time.March, 15)}}
	if cal.Validate() == nil {
		t.Error("an override that moves nothing is valid")
	}
}

func testGrant(t *testing.T, years []float64, cliff int) (Grant, *VestCalendar) {
	t.Helper()
	cal := acmeCalendar()
	first := D(2027, time.March, 15)
	pct, err := GeneratePercents(cal, first, years, cliff)
	if err != nil {
		t.Fatal(err)
	}
	return Grant{ID: 1, Account: "rsu", Symbol: "ACME", Date: D(2027, time.February, 1), Shares: SharesFor(100000, 187.50), Price: 187.50,
		Calendar: "Acme", FirstVest: first, YearPercents: years, CliffMonths: cliff, VestPercents: pct, Rates: DefaultWithholding, Brokerage: "brk"}, cal
}

func TestGrantSharesFromValue(t *testing.T) {
	if n := SharesFor(100000, 187.50); n != 533 {
		t.Errorf("$100,000 at $187.50 = %d shares, want 533", n)
	}
	if n := SharesFor(1000, 250); n != 4 {
		t.Errorf("exact division = %d", n)
	}
}

func TestPlanEvenYears(t *testing.T) {
	g, cal := testGrant(t, []float64{25, 25, 25, 25}, 0)
	plan, err := g.Plan(cal)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 16 {
		t.Fatalf("%d vests, want 16", len(plan))
	}
	total := 0
	for i, p := range plan {
		want := 33
		if i == 15 {
			want = 533 - 15*33
		}
		if p.Shares != want || p.Index != i+1 || math.Abs(p.Percent-6.25) > 1e-9 {
			t.Errorf("vest %d = %+v, want %d shares at 6.25%%", i+1, p, want)
		}
		total += p.Shares
	}
	if total != 533 || plan[15].Shares != 38 {
		t.Errorf("shares add up to %d, last %d", total, plan[15].Shares)
	}
	if plan[0].Date != D(2027, time.March, 15) || plan[15].Date != D(2030, time.December, 9) {
		t.Errorf("dates %v to %v", plan[0].Date, plan[15].Date)
	}
}

func TestPlanUnevenYears(t *testing.T) {
	g, cal := testGrant(t, []float64{40, 30, 20, 10}, 0)
	want := []float64{10, 7.5, 5, 2.5}
	for i, p := range g.VestPercents {
		if math.Abs(p-want[i/4]) > 1e-9 {
			t.Errorf("vest %d percent %v, want %v", i+1, p, want[i/4])
		}
	}
	plan, err := g.Plan(cal)
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Shares != 53 || plan[4].Shares != 39 || plan[8].Shares != 26 || plan[12].Shares != 13 {
		t.Errorf("shares %d %d %d %d", plan[0].Shares, plan[4].Shares, plan[8].Shares, plan[12].Shares)
	}
	sum := 0
	for _, p := range plan {
		sum += p.Shares
	}
	if sum != 533 {
		t.Errorf("shares add up to %d", sum)
	}
}

func TestPlanCliff(t *testing.T) {
	g, cal := testGrant(t, []float64{25, 25, 25, 25}, 12)
	plan, err := g.Plan(cal)
	if err != nil {
		t.Fatal(err)
	}
	// The first four quarterly vests are held and paid with the fifth, on its date 2028-03-20: 5/16 of the grant in one vest.
	if len(plan) != 12 || plan[0].Date != D(2028, time.March, 20) || plan[0].Percent != 31.25 || plan[0].Shares != 166 || plan[1].Date <= plan[0].Date || plan[11].Shares != 37 {
		t.Errorf("plan = %+v", plan)
	}
	sum := 0
	for _, p := range plan {
		sum += p.Shares
	}
	if sum != 533 {
		t.Errorf("shares add up to %d", sum)
	}
	if _, err := GeneratePercents(cal, g.FirstVest, []float64{25, 25, 25, 25}, 48); err == nil {
		t.Error("a cliff past the last vest is accepted")
	}
}

func TestPlanRefusals(t *testing.T) {
	cal := acmeCalendar()
	if _, err := GeneratePercents(cal, D(2027, time.March, 16), []float64{100}, 0); err == nil {
		t.Error("a first vest that is not a calendar date is accepted")
	}
	if _, err := GeneratePercents(cal, D(2027, time.March, 15), []float64{50, 40}, 0); err == nil {
		t.Error("percents that do not add up to 100 are accepted")
	}
	g, _ := testGrant(t, []float64{50, 50}, 0)
	g.VestPercents[0]++
	if _, err := g.Plan(cal); err == nil {
		t.Error("edited vest percents that do not add up to 100 are accepted")
	}
	g, _ = testGrant(t, []float64{50, 50}, 0)
	g.VestPercents = g.VestPercents[1:]
	if _, err := g.Plan(cal); err == nil {
		t.Error("percents for another number of vests are accepted")
	}
}

func TestRecordedVestMoney(t *testing.T) {
	v := RecordedVest{Shares: 33, Price: 210, Rates: DefaultWithholding}
	// 6930 x (0.22 + 0.0575 + 0.0765) = 6930 x 0.354
	if v.Gross() != 6930 || v.WithheldAmount() != 2453.22 || v.Net() != 4476.78 {
		t.Errorf("gross %v withheld %v net %v", v.Gross(), v.WithheldAmount(), v.Net())
	}
	v.Withheld = Ptr(2000.0)
	if v.Net() != 4930 {
		t.Errorf("net with an entered amount = %v", v.Net())
	}
}

func equityRecord(t *testing.T) (*Record, *Account) {
	t.Helper()
	rec := NewRecord(D(2028, time.January, 1))
	eq := &Account{ID: "rsu", Name: "RSU", Type: TypeEquity}
	g, _ := testGrant(t, []float64{25, 25, 25, 25}, 0)
	eq.Grants = []Grant{g}
	if err := rec.AddAccount(eq); err != nil {
		t.Fatal(err)
	}
	return rec, eq
}

func TestEquityBalance(t *testing.T) {
	rec, eq := equityRecord(t)
	grantDay := D(2027, time.February, 1)
	if _, ok := eq.ActualAt(grantDay.Add(-1)); ok {
		t.Error("actual before the first grant")
	}
	// No price yet: the grant price.
	if v, ok := eq.ActualAt(grantDay); !ok || v != 533*187.50 {
		t.Errorf("on the grant date = %v, %v", v, ok)
	}
	vest := D(2027, time.March, 15)
	rec.Prices.Add(PriceObservation{ID: 1, Symbol: "ACME", Date: vest, Price: 210})
	eq.Vests = append(eq.Vests, RecordedVest{ID: 2, Grant: 1, Planned: 1, Date: vest, Shares: 33, Price: 210})
	if v, _ := eq.ActualAt(vest.Add(-1)); v != 533*187.50 {
		t.Errorf("the day before the vest = %v", v)
	}
	if v, _ := eq.ActualAt(vest); v != 500*210 {
		t.Errorf("on the vest date = %v, want %v", v, 500*210)
	}
	rec.Prices.Add(PriceObservation{ID: 3, Symbol: "ACME", Date: D(2027, time.April, 1), Price: 220})
	if v, _ := eq.ActualAt(D(2027, time.April, 1)); v != 500*220 {
		t.Errorf("after a later price = %v", v)
	}
	// The latest observation of a date counts, and other stocks' prices do not.
	rec.Prices.Add(PriceObservation{ID: 4, Symbol: "ACME", Date: D(2027, time.April, 1), Price: 230})
	rec.Prices.Add(PriceObservation{ID: 5, Symbol: "OTHER", Date: D(2027, time.April, 1), Price: 1})
	if v, _ := eq.ActualAt(D(2027, time.April, 1)); v != 500*230 {
		t.Errorf("two observations on one date = %v", v)
	}
	sp, ok := eq.Span()
	if !ok || sp.From != grantDay || sp.To != D(2027, time.April, 1) {
		t.Errorf("span = %+v, %v", sp, ok)
	}
	if _, ok := eq.ActualAt(sp.To.Add(1)); ok {
		t.Error("actual after the span")
	}
	// A later grant of the same stock adds its shares from its date.
	eq.Grants = append(eq.Grants, Grant{ID: 9, Account: "rsu", Symbol: "ACME", Date: D(2027, time.April, 1), Shares: 10, Price: 1})
	if v, _ := eq.ActualAt(D(2027, time.April, 1)); v != 500*230+10*230 {
		t.Errorf("two grants = %v", v)
	}
	// The unexplained change of an equity account is its whole change, as it has no transactions.
	if u := eq.UnexplainedIn(vest.Add(-1), vest); u != 500*210-533*187.50 {
		t.Errorf("unexplained = %v", u)
	}
}

func TestCloneKeepsEquity(t *testing.T) {
	rec, eq := equityRecord(t)
	rec.Prices.Add(PriceObservation{ID: 1, Symbol: "ACME", Date: D(2027, time.March, 15), Price: 210})
	rec.Calendars = append(rec.Calendars, acmeCalendar())
	c := rec.Clone()
	rec.Prices.Add(PriceObservation{ID: 2, Symbol: "ACME", Date: D(2027, time.March, 15), Price: 999})
	ca := c.Account("rsu")
	if v, _ := ca.ActualAt(D(2027, time.March, 15)); v != 533*210 {
		t.Errorf("clone shares the original's prices: %v", v)
	}
	ca.Grants[0].YearPercents[0] = 1
	if eq.Grants[0].YearPercents[0] == 1 {
		t.Error("clone shares the grant's slices")
	}
	if len(c.Calendars) != 1 || c.Calendar("Acme") == rec.Calendar("Acme") {
		t.Error("clone shares calendars")
	}
}

func TestLastEntryTakesTheLatestOfAll(t *testing.T) {
	g, _ := testGrant(t, []float64{100}, 0)
	a := &Account{ID: "eq", Type: TypeEquity, Grants: []Grant{g}}
	a.AddSnapshot(D(2020, time.January, 1), 5)
	if d, ok := a.LastEntry(); !ok || d != g.Date {
		t.Errorf("LastEntry = %v, %v; want the grant date %v", d, ok, g.Date)
	}
	vest := D(2027, time.June, 1)
	a.Vests = []RecordedVest{{Grant: g.ID, Planned: 1, Date: vest, Shares: 1, Price: 1}}
	if d, _ := a.LastEntry(); d != vest {
		t.Errorf("LastEntry = %v, want the vest date %v", d, vest)
	}
}
