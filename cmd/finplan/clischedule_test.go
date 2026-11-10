// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
)

// scratchDB is the demo database with a savings account "scratch" that earns nothing and holds 10000, so that the payments of the items added to it show as the day-to-day changes of its balance.
func scratchDB(t *testing.T) string {
	t.Helper()
	path := seededFile(t)
	data(t, path, "", "account", "new", "Scratch", "--type", "savings", "--tax", "taxable", "--rate", "0")
	data(t, path, "", "entry", "add-snapshot", "scratch", "--date", "2026-11-01", "--balance", "10000")
	return path
}

// payment is a day the scratch account's balance changed, with the change.
type payment struct {
	Date   string
	Change float64
}

// payments lists the days from..to on which the balance of the scratch account changed, as the base projection projects it.
func payments(t *testing.T, path, from, to string) []payment {
	t.Helper()
	var b struct {
		Samples []struct {
			Date  string
			Value float64
		}
	}
	cliJSON(t, &b, "--db", path, "balance", "scratch", "--from", from, "--to", to, "--step", "day")
	var out []payment
	for i := 1; i < len(b.Samples); i++ {
		if c := b.Samples[i].Value - b.Samples[i-1].Value; math.Abs(c) > 0.005 {
			out = append(out, payment{b.Samples[i].Date, math.Round(c*100) / 100})
		}
	}
	return out
}

func dates(ps []payment) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Date)
	}
	return out
}

// A quarterly item starting 2027-01-15 pays four times a year.
func TestCLIQuarterlyItemPaysFourTimesAYear(t *testing.T) {
	path := scratchDB(t)
	data(t, path, "", "budget", "add", "--name", "Retainer", "--kind", "expense", "--amount", "100", "--account", "scratch", "--every", "quarterly", "--start", "2027-01-15")
	got := payments(t, path, "2027-01-01", "2027-12-31")
	if want := []string{"2027-01-15", "2027-04-15", "2027-07-15", "2027-10-15"}; !slices.Equal(dates(got), want) || got[0].Change != -100 {
		t.Errorf("payments %+v, want %v", got, want)
	}
}

// A custom every-3-weeks item pays every 21 days from its start.
func TestCLICustomEveryThreeWeeks(t *testing.T) {
	path := scratchDB(t)
	data(t, path, "", "budget", "add", "--name", "Sitter", "--kind", "expense", "--amount", "40", "--account", "scratch", "--every", "custom", "--count", "3", "--unit", "weeks", "--start", "2027-01-04")
	got := payments(t, path, "2027-01-01", "2027-03-31")
	want := []string{"2027-01-04", "2027-01-25", "2027-02-15", "2027-03-08", "2027-03-29"}
	if !slices.Equal(dates(got), want) || got[0].Change != -40 {
		t.Errorf("payments %+v, want %v", got, want)
	}
	var items []struct {
		ID         string
		Recurrence struct {
			Kind  string
			Count *int
			Unit  *string
			Day   *int
		}
	}
	cliJSON(t, &items, "--db", path, "budget", "list")
	for _, it := range items {
		if it.ID == "sitter" && (it.Recurrence.Kind != "custom" || it.Recurrence.Count == nil || *it.Recurrence.Count != 3 || it.Recurrence.Unit == nil || *it.Recurrence.Unit != "weeks" || it.Recurrence.Day != nil) {
			t.Errorf("sitter recurrence %+v", it.Recurrence)
		}
	}
}

// A daycare of 1400 a month until 2029-07 and 600 after is paid at each amount, and the list shows the schedule; its amount is 0 since it starts after the record's today.
func TestCLIAmountScheduleIsPaidAtEachAmount(t *testing.T) {
	path := scratchDB(t)
	input := `{"name": "Daycare", "kind": "expense", "account": "scratch", "start": "2027-01-01",
		"recurrence": {"kind": "monthly", "day": 10},
		"amount": [{"from": "2027-01-01", "end": "2029-07-31", "value": 1400}, {"from": "2029-08-01", "value": 600}],
		"growth": 0}`
	data(t, path, input, "budget", "add")
	got := payments(t, path, "2029-06-01", "2029-09-30")
	want := []payment{{"2029-06-10", -1400}, {"2029-07-10", -1400}, {"2029-08-10", -600}, {"2029-09-10", -600}}
	if !slices.Equal(got, want) {
		t.Errorf("payments %+v, want %+v", got, want)
	}
	var items []struct {
		ID             string
		Amount         float64
		AmountSchedule []struct{ Value float64 } `json:"amount_schedule"`
		Growth         float64
		GrowthSchedule []struct{ Value float64 } `json:"growth_schedule"`
	}
	cliJSON(t, &items, "--db", path, "budget", "list")
	for _, it := range items {
		if it.ID == "daycare" && (it.Amount != 0 || len(it.AmountSchedule) != 2 || it.AmountSchedule[1].Value != 600 || it.Growth != 0 || it.GrowthSchedule == nil || len(it.GrowthSchedule) != 0) {
			t.Errorf("daycare %+v", it)
		}
	}
	// An edit of another field keeps the schedule, and --amount replaces it with one number.
	data(t, path, "", "budget", "edit", "daycare", "--name", "Daycare and camp")
	if got := payments(t, path, "2029-07-01", "2029-08-31"); got[1].Change != -600 {
		t.Errorf("after a rename %+v", got)
	}
	data(t, path, "", "budget", "edit", "daycare", "--amount", "900")
	if got := payments(t, path, "2029-07-01", "2029-08-31"); got[0].Change != -900 || got[1].Change != -900 {
		t.Errorf("after --amount %+v", got)
	}
}

// An item on the 31st pays on the last day of shorter months.
func TestCLIItemOnThe31st(t *testing.T) {
	path := scratchDB(t)
	data(t, path, "", "budget", "add", "--name", "Month end", "--kind", "income", "--amount", "10", "--account", "scratch", "--every", "monthly", "--start", "2027-01-31")
	got := payments(t, path, "2027-01-01", "2027-06-30")
	if want := []string{"2027-01-31", "2027-02-28", "2027-03-31", "2027-04-30", "2027-05-31", "2027-06-30"}; !slices.Equal(dates(got), want) {
		t.Errorf("payments %v, want %v", dates(got), want)
	}
}

func TestCLISemimonthlyAndWeeklyItems(t *testing.T) {
	path := scratchDB(t)
	data(t, path, "", "budget", "add", "--name", "Twice", "--kind", "income", "--amount", "10", "--account", "scratch", "--every", "semimonthly", "--start", "2027-01-01", "--day", "1", "--day2", "15")
	if got := payments(t, path, "2026-12-31", "2027-03-01"); !slices.Equal(dates(got), []string{"2027-01-01", "2027-01-15", "2027-02-01", "2027-02-15", "2027-03-01"}) {
		t.Errorf("semimonthly payments %v", dates(got))
	}
	// Editing the days of a semimonthly item keeps the rest.
	data(t, path, "", "budget", "edit", "twice", "--day2", "20")
	if got := payments(t, path, "2027-01-31", "2027-02-28"); !slices.Equal(dates(got), []string{"2027-02-01", "2027-02-20"}) {
		t.Errorf("after --day2 %v", dates(got))
	}
	data(t, path, "", "item", "add", "New job", "--name", "Weekly", "--kind", "expense", "--amount", "5", "--account", "scratch", "--every", "weekly", "--start", "2027-01-04")
	var b struct{ Samples []struct{ Value float64 } }
	cliJSON(t, &b, "--db", path, "balance", "scratch", "--from", "2027-01-03", "--to", "2027-01-12", "--step", "day", "--projection", "New job")
	if len(b.Samples) != 10 || b.Samples[0].Value-b.Samples[1].Value < 4.99 || b.Samples[7].Value-b.Samples[8].Value < 4.99 {
		t.Errorf("weekly projection payments %+v", b.Samples)
	}
}

func TestCLIItemRecurrenceFlagsAreChecked(t *testing.T) {
	path := scratchDB(t)
	base := []string{"budget", "add", "--name", "X", "--kind", "income", "--amount", "1", "--account", "scratch", "--start", "2027-01-01"}
	for _, c := range []struct {
		args []string
		exit int
		code string
	}{
		{[]string{"--every", "custom"}, 2, "usage"},
		{[]string{"--every", "custom", "--count", "2"}, 2, "usage"},
		{[]string{"--every", "custom", "--count", "0", "--unit", "days"}, 1, "invalid"},
		{[]string{"--every", "custom", "--count", "2", "--unit", "fortnights"}, 2, "usage"},
		{[]string{"--every", "semimonthly"}, 2, "usage"},
		{[]string{"--every", "semimonthly", "--day", "5", "--day2", "5"}, 1, "invalid"},
		{[]string{"--every", "monthly", "--count", "2"}, 2, "usage"},
		{[]string{"--every", "quarterly", "--day", "2"}, 2, "usage"},
		{[]string{"--every", "bimonthly"}, 2, "usage"},
		{[]string{"--every", "monthly", "--on", "2027-01-01"}, 2, "usage"},
	} {
		refusedWrite(t, path, "", c.exit, c.code, append(slices.Clone(base), c.args...)...)
	}
	// annual stays an alias of annually.
	for _, every := range []string{"annual", "annually"} {
		w := data(t, path, "", "budget", "add", "--name", "Yearly "+every, "--kind", "expense", "--amount", "1", "--account", "scratch", "--start", "2027-06-15", "--every", every)
		if !w.Changed {
			t.Errorf("--every %s: %+v", every, w)
		}
	}
	var items []struct {
		ID         string
		Recurrence struct {
			Kind       string
			Month, Day *int
		}
	}
	cliJSON(t, &items, "--db", path, "budget", "list")
	for _, it := range items {
		if (it.ID == "yearly-annual" || it.ID == "yearly-annually") && (it.Recurrence.Kind != "annual" || *it.Recurrence.Month != 6 || *it.Recurrence.Day != 15) {
			t.Errorf("%s recurrence %+v", it.ID, it.Recurrence)
		}
	}
}

// The totals count a custom frequency by its average per month, in today's dollars.
func TestCLIBudgetTotalsCountCustomFrequencies(t *testing.T) {
	path := seededFile(t)
	var before budgetTotals
	cliJSON(t, &before, "--db", path, "budget", "totals")
	add := func(name, amount string, every ...string) {
		args := append([]string{"budget", "add", "--name", name, "--kind", "expense", "--amount", amount, "--account", "checking", "--start", "2026-01-01"}, every...)
		data(t, path, "", args...)
	}
	add("Every ten days", "100", "--every", "custom", "--count", "10", "--unit", "days")
	add("Every three weeks", "300", "--every", "custom", "--count", "3", "--unit", "weeks")
	add("Every two months", "600", "--every", "custom", "--count", "2", "--unit", "months")
	add("Every two years", "2400", "--every", "custom", "--count", "2", "--unit", "years")
	add("Weekly", "50", "--every", "weekly")
	add("Quarterly", "300", "--every", "quarterly")
	var after budgetTotals
	cliJSON(t, &after, "--db", path, "budget", "totals")
	want := 100*30.4375/10 + 300*4.348/3 + 600/2.0 + 2400/24.0 + 50*4.348 + 300/3.0
	if got := after.MonthlyExpenses - before.MonthlyExpenses; math.Abs(got-want) > 0.01 {
		t.Errorf("expenses rose by %v, want %v", got, want)
	}
	if after.MonthlyIncome != before.MonthlyIncome {
		t.Errorf("income %v changed from %v", after.MonthlyIncome, before.MonthlyIncome)
	}
}

// The item budget list prints is accepted back as the item of budget edit, and its schedule is kept.
func TestCLIListedItemRoundTripsThroughEdit(t *testing.T) {
	path := scratchDB(t)
	input := `{"name": "Daycare", "kind": "expense", "account": "scratch", "start": "2027-01-01",
		"recurrence": {"kind": "monthly", "day": 10},
		"amount": [{"from": "2027-01-01", "end": "2029-07-31", "value": 1400}, {"from": "2029-08-01", "value": 600}]}`
	data(t, path, input, "budget", "add")
	code, out := wcli(t, "", "--db", path, "budget", "list")
	if code != 0 {
		t.Fatalf("budget list exited %d: %s", code, out)
	}
	var items []map[string]any
	if err := json.Unmarshal(out, &items); err != nil {
		t.Fatal(err)
	}
	var listed map[string]any
	for _, it := range items {
		if it["id"] == "daycare" {
			listed = it
		}
	}
	if listed == nil {
		t.Fatalf("no daycare in %s", out)
	}
	listed["name"] = "Daycare and camp"
	edited, _ := json.Marshal(listed)
	w := data(t, path, string(edited), "budget", "edit", "daycare")
	if w.Item == nil || w.Item.Name != "Daycare and camp" {
		t.Errorf("edit result %+v", w.Item)
	}
	if got := payments(t, path, "2029-07-01", "2029-08-31"); len(got) != 2 || got[0].Change != -1400 || got[1].Change != -600 {
		t.Errorf("after the round trip %+v", got)
	}
}

// A growth schedule that starts in the future lists as growth 0 and is accepted back unchanged.
func TestCLIListedGrowthScheduleStartingLaterRoundTrips(t *testing.T) {
	path := scratchDB(t)
	input := `{"name": "Rent", "kind": "expense", "account": "scratch", "start": "2027-01-01",
		"recurrence": {"kind": "monthly", "day": 1}, "amount": 1000,
		"growth": [{"from": "2028-01-01", "value": 0.03}]}`
	data(t, path, input, "budget", "add")
	_, out := wcli(t, "", "--db", path, "budget", "list")
	var items []map[string]any
	if err := json.Unmarshal(out, &items); err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it["id"] == "rent" {
			it["name"] = "Rent and fees"
			edited, _ := json.Marshal(it)
			w := data(t, path, string(edited), "budget", "edit", "rent")
			if w.Item == nil || w.Item.Name != "Rent and fees" {
				t.Errorf("edit result %+v", w.Item)
			}
			_, out = wcli(t, "", "--db", path, "budget", "list")
			if !strings.Contains(string(out), `"from": "2028-01-01"`) && !strings.Contains(string(out), `"from":"2028-01-01"`) {
				t.Errorf("growth schedule lost: %s", out)
			}
			return
		}
	}
	t.Fatalf("no rent in %s", out)
}

// Editing a custom item's count without a date flag keeps its start and its anchor.
func TestCLIEditKeepsStartAndAnchor(t *testing.T) {
	path := scratchDB(t)
	input := `{"name": "Sitter", "kind": "expense", "account": "scratch", "start": "2027-01-01", "amount": 50,
		"recurrence": {"kind": "custom", "count": 3, "unit": "weeks", "anchor": "2025-01-03"}}`
	data(t, path, input, "budget", "add")
	check := func(what string, want []string) {
		t.Helper()
		if got := payments(t, path, "2026-12-01", "2027-02-15"); !slices.Equal(dates(got), want) {
			t.Errorf("%s: payments %v, want %v", what, dates(got), want)
		}
	}
	check("added", []string{"2027-01-08", "2027-01-29"})
	data(t, path, "", "budget", "edit", "sitter", "--count", "2")
	check("after --count 2", []string{"2027-01-01", "2027-01-15", "2027-01-29", "2027-02-12"})
	data(t, path, "", "budget", "edit", "sitter", "--unit", "weeks")
	check("after --unit", []string{"2027-01-01", "2027-01-15", "2027-01-29", "2027-02-12"})
}

// A weekly --every edit keeps the start and the anchor the item counts from.
func TestCLIWeeklyEveryEditKeepsAnchor(t *testing.T) {
	path := scratchDB(t)
	input := `{"name": "Weekly", "kind": "expense", "account": "scratch", "start": "2027-01-05", "amount": 5,
		"recurrence": {"kind": "weekly", "anchor": "2025-01-03"}}`
	data(t, path, input, "budget", "add")
	want := []string{"2027-01-08", "2027-01-15", "2027-01-22"}
	if got := payments(t, path, "2027-01-01", "2027-01-25"); !slices.Equal(dates(got), want) {
		t.Fatalf("added: payments %v, want %v", dates(got), want)
	}
	data(t, path, "", "budget", "edit", "weekly", "--every", "weekly", "--amount", "6")
	if got := payments(t, path, "2027-01-01", "2027-01-25"); !slices.Equal(dates(got), want) || got[0].Change != -6 {
		t.Errorf("after --every weekly: payments %+v, want %v", got, want)
	}
}

// A numeric amount or growth that differs from the schedule given with it is refused; one that agrees is accepted.
func TestCLIItemJSONNumberAndScheduleMustAgree(t *testing.T) {
	path := scratchDB(t)
	base := `{"name": "Daycare", "kind": "expense", "account": "scratch", "start": "2027-01-01", "recurrence": {"kind": "monthly", "day": 10}, "amount": 1400}`
	data(t, path, base, "budget", "add")
	for _, in := range []string{
		`{"name": "Daycare", "kind": "expense", "account": "scratch", "start": "2027-01-01", "recurrence": {"kind": "monthly", "day": 10}, "amount": 1500, "amount_schedule": [{"from": "2026-01-01", "value": 1400}]}`,
		`{"name": "Daycare", "kind": "expense", "account": "scratch", "start": "2027-01-01", "recurrence": {"kind": "monthly", "day": 10}, "amount": 1400, "growth": 0.03, "growth_schedule": []}`,
	} {
		if code, out := wcli(t, in, "--db", path, "budget", "edit", "daycare"); code == 0 {
			t.Errorf("edit of %s exited 0: %s", in, out)
		}
	}
	agree := `{"name": "Daycare", "kind": "expense", "account": "scratch", "start": "2027-01-01", "recurrence": {"kind": "monthly", "day": 10}, "amount": 1400, "amount_schedule": [{"from": "2026-01-01", "value": 1400}], "growth": 0, "growth_schedule": []}`
	data(t, path, agree, "budget", "edit", "daycare")
	future := `{"name": "Daycare", "kind": "expense", "account": "scratch", "start": "2027-01-01", "recurrence": {"kind": "monthly", "day": 10}, "amount": 0, "amount_schedule": [{"from": "2027-01-01", "value": 1400}]}`
	data(t, path, future, "budget", "edit", "daycare")
}

// A misspelled key in an inline segment is refused.
func TestCLIInlineSegmentUnknownKeyRefused(t *testing.T) {
	path := scratchDB(t)
	in := `{"name": "Daycare", "kind": "expense", "account": "scratch", "start": "2027-01-01", "recurrence": {"kind": "monthly", "day": 10},
		"amount": [{"from": "2027-01-01", "until": "2027-12-31", "value": 100}]}`
	if code, out := wcli(t, in, "--db", path, "budget", "add"); code == 0 {
		t.Errorf("add exited 0: %s", out)
	}
}

// An annual item edited without a date flag keeps its month and day.
func TestCLIAnnualEditKeepsMonthAndDay(t *testing.T) {
	path := scratchDB(t)
	in := `{"name": "Fee", "kind": "expense", "account": "scratch", "start": "2027-01-01", "amount": 800,
		"recurrence": {"kind": "annual", "month": 4, "day": 15}}`
	data(t, path, in, "budget", "add")
	data(t, path, "", "budget", "edit", "fee", "--every", "annually", "--amount", "900")
	got := payments(t, path, "2027-01-01", "2027-12-31")
	if len(got) != 1 || got[0].Date != "2027-04-15" || got[0].Change != -900 {
		t.Errorf("payments %+v, want 2027-04-15 of -900", got)
	}
}
