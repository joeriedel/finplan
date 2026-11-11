// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// The equity commands: vest calendars, grants, recorded vests and stock prices. Grants, vests and prices are facts with ids of their own, shared by one numbering; a correction keeps what it replaced in the fact's history, as an entry's does.

const (
	rulesNote     = "comma-separated rules, one per vest month: month=day for a fixed day (feb=15) or month=<n><weekday> for the nth weekday, n from 1 to 4 or last (mar=3mon is the third Monday of March, jun=lastfri the last Friday of June); months and weekdays by their first three letters"
	overridesNote = "comma-separated date=date pairs, each moving a generated vest date to another (2027-03-15=2027-03-16)"
	percentsNote  = "comma-separated percents summing to 100 (25,25,25,25)"
	rateNote      = "a fraction between 0 and 1 (0.22 is 22%); the rates must add up to less than 1"
	calendarOut   = "{name, rules: [rule as written to --rules], overrides: [{from, to}], grants: the number of grants using it}"
	grantOut      = "{id, account, symbol, date, shares, value (the dollar value the grant was entered as, null if entered as shares), price (the grant price, for reference), calendar, first_vest, year_percents, cliff_months, vest_percents (the percent each planned vest pays, in date order), rates: {federal, state, fica}, brokerage, vested, remaining}"
	vestOut       = "{id, grant, planned (the 1-based planned vest it fulfils), date, shares, price, gross (shares x price), withheld, withheld_entered (true if the amount was entered rather than computed from the rates), net, rates: {federal, state, fica}, tx_entry (the id of the brokerage entry that deposited the net proceeds)}"
	priceOut      = "{id, symbol, date, price}"
)

var weekdayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
var monthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}

func lookupName(names []string, s string) (int, bool) {
	if len(s) < 3 {
		return 0, false
	}
	i := slices.Index(names, strings.ToLower(s[:3]))
	return i, i >= 0
}

// parseRules reads the --rules value; see rulesNote.
func parseRules(s string) ([]model.VestRule, error) {
	var out []model.VestRule
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		mon, spec, ok := strings.Cut(tok, "=")
		m, okm := lookupName(monthNames, mon)
		if !ok || !okm || spec == "" {
			return nil, usageErr("--rules: %q is not month=day or month=<n><weekday>", tok)
		}
		r := model.VestRule{Month: time.Month(m + 1)}
		if day, err := strconv.Atoi(spec); err == nil {
			r.Day = day
		} else {
			nth, rest := strings.TrimPrefix(spec, "last"), spec
			if nth != spec {
				r.Nth, rest = model.NthLast, nth
			} else if len(spec) > 1 {
				if n, err := strconv.Atoi(spec[:1]); err == nil {
					r.Nth, rest = n, spec[1:]
				}
			}
			wd, okw := lookupName(weekdayNames, rest)
			if r.Nth == 0 || !okw {
				return nil, usageErr("--rules: %q is not month=day or month=<n><weekday>", tok)
			}
			r.Weekday = time.Weekday(wd)
		}
		out = append(out, r)
	}
	return out, nil
}

func formatRule(r model.VestRule) string {
	m := monthNames[r.Month-1]
	switch {
	case r.Nth == 0:
		return fmt.Sprintf("%s=%d", m, r.Day)
	case r.Nth == model.NthLast:
		return m + "=last" + weekdayNames[r.Weekday]
	}
	return fmt.Sprintf("%s=%d%s", m, r.Nth, weekdayNames[r.Weekday])
}

// parseOverrides reads the --overrides value; see overridesNote. An empty value is no overrides.
func parseOverrides(s string) ([]model.DateOverride, error) {
	var out []model.DateOverride
	for _, tok := range strings.Split(s, ",") {
		if tok = strings.TrimSpace(tok); tok == "" {
			continue
		}
		from, to, ok := strings.Cut(tok, "=")
		if !ok {
			return nil, usageErr("--overrides: %q is not date=date", tok)
		}
		f, err1 := parseCLIDate(from, false)
		t, err2 := parseCLIDate(to, false)
		if err := errors.Join(err1, err2); err != nil {
			return nil, usageErr("--overrides: %v", err)
		}
		out = append(out, model.DateOverride{From: f, To: t})
	}
	return out, nil
}

// parsePercents reads a comma-separated list of numbers for the flag.
func parsePercents(flag, s string) ([]float64, error) {
	var out []float64
	for _, tok := range strings.Split(s, ",") {
		v, err := parseNumber(strings.TrimSpace(tok))
		if err != nil {
			return nil, usageErr("--%s: %v", flag, err)
		}
		out = append(out, v)
	}
	return out, nil
}

type jrates struct {
	Federal float64 `json:"federal"`
	State   float64 `json:"state"`
	FICA    float64 `json:"fica"`
}

type jgrantFields struct {
	ID           int64     `json:"id"`
	Account      string    `json:"account"`
	Symbol       string    `json:"symbol"`
	Date         jdate     `json:"date"`
	Shares       int       `json:"shares"`
	Value        *float64  `json:"value"`
	Price        float64   `json:"price"`
	Calendar     string    `json:"calendar"`
	FirstVest    jdate     `json:"first_vest"`
	YearPercents []float64 `json:"year_percents"`
	CliffMonths  int       `json:"cliff_months"`
	VestPercents []float64 `json:"vest_percents"`
	Rates        jrates    `json:"rates"`
	Brokerage    string    `json:"brokerage"`
}

type jgrant struct {
	jgrantFields
	Vested    int `json:"vested"`
	Remaining int `json:"remaining"`
}

func grantFields(g model.Grant) jgrantFields {
	f := jgrantFields{g.ID, string(g.Account), g.Symbol, jd(g.Date), g.Shares, nil, g.Price, g.Calendar, jd(g.FirstVest),
		g.YearPercents, g.CliffMonths, g.VestPercents, jrates(g.Rates), string(g.Brokerage)}
	if g.Value != 0 {
		f.Value = &g.Value
	}
	return f
}

func grantJSON(rec *model.Record, g model.Grant) jgrant {
	vested := 0
	if a := rec.Account(g.Account); a != nil {
		vested = a.VestedBy(g.ID, model.MaxDate)
	}
	return jgrant{grantFields(g), vested, g.Shares - vested}
}

type jvest struct {
	ID              int64   `json:"id"`
	Grant           int64   `json:"grant"`
	Planned         int     `json:"planned"`
	Date            jdate   `json:"date"`
	Shares          int     `json:"shares"`
	Price           float64 `json:"price"`
	Gross           float64 `json:"gross"`
	Withheld        float64 `json:"withheld"`
	WithheldEntered bool    `json:"withheld_entered"`
	Net             float64 `json:"net"`
	Rates           jrates  `json:"rates"`
	TxEntry         *int64  `json:"tx_entry"`
}

func vestJSON(v model.RecordedVest) jvest {
	j := jvest{v.ID, v.Grant, v.Planned, jd(v.Date), v.Shares, v.Price, v.Gross(), v.WithheldAmount(), v.Withheld != nil, v.Net(), jrates(v.Rates), nil}
	if v.TxEntry != 0 {
		j.TxEntry = &v.TxEntry
	}
	return j
}

type jprice struct {
	ID     int64   `json:"id"`
	Symbol string  `json:"symbol"`
	Date   jdate   `json:"date"`
	Price  float64 `json:"price"`
}

func priceJSON(p model.PriceObservation) jprice { return jprice{p.ID, p.Symbol, jd(p.Date), p.Price} }

type jcalendarOverride struct {
	From jdate `json:"from"`
	To   jdate `json:"to"`
}

type jcalendar struct {
	Name      string              `json:"name"`
	Rules     []string            `json:"rules"`
	Overrides []jcalendarOverride `json:"overrides"`
	Grants    int                 `json:"grants"`
}

func calendarJSON(rec *model.Record, c *model.VestCalendar) jcalendar {
	j := jcalendar{Name: c.Name, Rules: []string{}, Overrides: []jcalendarOverride{}}
	for _, r := range c.Rules {
		j.Rules = append(j.Rules, formatRule(r))
	}
	for _, o := range c.Overrides {
		j.Overrides = append(j.Overrides, jcalendarOverride{jd(o.From), jd(o.To)})
	}
	for _, a := range rec.Accounts {
		for _, g := range a.Grants {
			if g.Calendar == c.Name {
				j.Grants++
			}
		}
	}
	return j
}

func rateFlags() []flagDef {
	return []flagDef{
		{Name: "federal", Type: typeNumber, Description: "federal withholding rate: " + rateNote},
		{Name: "state", Type: typeNumber, Description: "Virginia withholding rate: " + rateNote},
		{Name: "fica", Type: typeNumber, Description: "FICA withholding rate: " + rateNote},
	}
}

// ratesFrom returns the rates the flags set on top of base, and whether any was set.
func (c *call) ratesFrom(base model.Withholding) (model.Withholding, bool, error) {
	set := false
	for name, dst := range map[string]*float64{"federal": &base.Federal, "state": &base.State, "fica": &base.FICA} {
		v, ok, err := c.number(name)
		if err != nil {
			return base, false, err
		}
		if ok {
			*dst, set = v, true
		}
	}
	return base, set, nil
}

var equityReadCommands = []*command{
	{
		Name: "calendar list", Summary: "list the vest calendars: one rule per vest month, and the overrides that move single dates",
		Output: "[" + calendarOut + "]",
		run:    cmdCalendarList,
	},
	{
		Name: "calendar dates", Summary: "list the vest dates a calendar generates in a range, overrides applied",
		Args: []argDef{{Name: "calendar", Description: "calendar name", Required: true}},
		Flags: []flagDef{
			{Name: "from", Type: typeDate, Required: true, Description: "first day of the range: " + dateNote},
			{Name: "to", Type: typeDate, Required: true, EndOfMonth: true, Description: "last day of the range: YYYY-MM-DD, or YYYY-MM for the last day of the month"},
		},
		Output: "{calendar, dates: [date, in order]}",
		run:    cmdCalendarDates,
	},
	{
		Name: "account grants", Summary: "list an equity account's grants with the shares vested and left; voided grants are left out",
		Args:   []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Output: "[" + grantOut + "]",
		run:    cmdAccountGrants,
	},
	{
		Name: "grant schedule", Summary: "show a grant's planned vests, each planned or recorded; a recorded one shows the vest and how its date and shares differ from the plan",
		Args:   []argDef{{Name: "grant-id", Description: "grant id, as account grants lists it", Required: true}},
		Output: "{grant: " + grantOut + ", vests: [{planned (the 1-based number), date, percent, shares, status (planned or recorded), vest: " + vestOut + " or null, date_diff (days the recorded date is after the planned one) or null, share_diff (recorded shares less planned) or null}]}",
		run:    cmdGrantSchedule,
	},
	{
		Name: "grant history", Summary: "show every revision of one grant: its original values, then each correction or void",
		Args:   []argDef{{Name: "grant-id", Description: "grant id, as account grants lists it", Required: true}},
		Output: "{id, revisions: [{grant values as account grants prints them without vested and remaining, made_at (RFC 3339), note, void}]}",
		run:    cmdGrantHistory,
	},
	{
		Name: "vest history", Summary: "show every revision of one recorded vest: its original values, then each correction or void",
		Args:   []argDef{{Name: "vest-id", Description: "vest id, as grant schedule lists it", Required: true}},
		Output: "{id, revisions: [{vest values as grant schedule prints them, made_at (RFC 3339), note, void}]}",
		run:    cmdVestHistory,
	},
	{
		Name: "price history", Summary: "list a stock's price observations in date order; voided ones are left out unless --voided is given",
		Args:   []argDef{{Name: "symbol", Description: "stock symbol", Required: true}},
		Flags:  []flagDef{{Name: "voided", Type: typeBool, Default: "false", Description: "include voided observations, marked void: true"}},
		Output: "[" + priceOut + "]; with --voided each also has void",
		run:    cmdPriceHistory,
	},
	{
		Name: "price revisions", Summary: "show every revision of one price observation: its original values, then each correction or void",
		Args:   []argDef{{Name: "price-id", Description: "price observation id, as price history lists it", Required: true}},
		Output: "{id, revisions: [{symbol, date, price, made_at (RFC 3339), note, void}]}",
		run:    cmdPriceRevisions,
	},
}

var equityWriteCommands = []*command{
	{
		Name: "calendar new", Summary: "add a vest calendar, shared by the grants of one employer",
		Args:   []argDef{{Name: "name", Description: "name of the calendar", Required: true}},
		Flags:  calendarFlags(true),
		Output: "{dry_run, changed, calendar: " + calendarOut + "}",
		run:    cmdCalendarNew,
	},
	{
		Name: "calendar edit", Summary: "replace a calendar's rules or overrides; a grant whose planned vests the change would break is refused",
		Args:   []argDef{{Name: "name", Description: "calendar name", Required: true}},
		Flags:  calendarFlags(false),
		Output: "{dry_run, changed, calendar: " + calendarOut + "}",
		run:    cmdCalendarEdit,
	},
	{
		Name: "calendar delete", Summary: "delete a vest calendar; one that a grant uses is refused",
		Args:   []argDef{{Name: "name", Description: "calendar name", Required: true}},
		Output: "{dry_run, changed, removed: the name, calendar: " + calendarOut + " as it was}",
		run:    cmdCalendarDelete,
	},
	{
		Name: "grant add", Summary: "record a grant of shares to an equity account, entered as a number of shares or as a dollar value that buys the whole shares at the grant price",
		Args: []argDef{{Name: "account", Description: "equity account id or name", Required: true}},
		Flags: append([]flagDef{
			{Name: "symbol", Type: typeString, Required: true, Description: "the stock's symbol"},
			{Name: "date", Type: typeDate, Required: true, Description: "grant date: " + dateNote},
			{Name: "shares", Type: typeInt, Description: "number of shares; give this or --value"},
			{Name: "value", Type: typeNumber, Description: "dollar value of the grant; the shares are its whole shares at --price"},
			{Name: "price", Type: typeNumber, Required: true, Description: "grant price per share, for reference: each vest is valued at the price on its vest date"},
			{Name: "calendar", Type: typeString, Required: true, Description: "name of the vest calendar"},
			{Name: "first-vest", Type: typeDate, Required: true, Description: "first vest date, a date of the calendar: " + dateNote},
			{Name: "year-percents", Type: typeString, Required: true, Description: "the percent vesting in each year from the first vest, as " + percentsNote + "; each year's percent is split equally over its calendar vests"},
			{Name: "cliff-months", Type: typeInt, Description: "months from the first vest's month whose vests are held and paid together on the first vest after; 0 when omitted"},
			{Name: "vest-percents", Type: typeString, Description: "the percent each planned vest pays, as " + percentsNote + ", instead of the ones generated from --year-percents"},
			{Name: "brokerage", Type: typeString, Required: true, Description: "brokerage account that net proceeds are deposited in: account id or name"},
		}, rateFlags()...),
		Output: "{dry_run, changed, grant: " + grantOut + "}; the id is null in a dry run",
		run:    cmdGrantAdd,
	},
	{
		Name: "grant correct", Summary: "correct a grant: only the values given change, and the earlier values stay in grant history. Correcting a voided grant restores it. Changing the calendar, first vest, year percents or cliff regenerates the vest percents unless --vest-percents is given",
		Args: []argDef{{Name: "grant-id", Description: "grant id, as account grants lists it", Required: true}},
		Flags: append([]flagDef{
			{Name: "symbol", Type: typeString, Description: "the stock's symbol"},
			{Name: "date", Type: typeDate, Description: "grant date: " + dateNote},
			{Name: "shares", Type: typeInt, Description: "number of shares"},
			{Name: "value", Type: typeNumber, Description: "dollar value of the grant; the shares are its whole shares at the grant price"},
			{Name: "price", Type: typeNumber, Description: "grant price per share"},
			{Name: "calendar", Type: typeString, Description: "name of the vest calendar"},
			{Name: "first-vest", Type: typeDate, Description: "first vest date: " + dateNote},
			{Name: "year-percents", Type: typeString, Description: percentsNote},
			{Name: "cliff-months", Type: typeInt, Description: "months of the cliff"},
			{Name: "vest-percents", Type: typeString, Description: percentsNote},
			{Name: "brokerage", Type: typeString, Description: "brokerage account id or name"},
			{Name: "note", Type: typeString, Description: "why the grant is corrected, kept in grant history"},
		}, rateFlags()...),
		Output: "{dry_run, changed, grant: " + grantOut + " with the values after the correction}",
		run:    cmdGrantCorrect,
	},
	{
		Name: "grant void", Summary: "take a grant out of the record, with its history kept; a grant with recorded vests is refused: void the vests first",
		Args:   []argDef{{Name: "grant-id", Description: "grant id, as account grants lists it", Required: true}},
		Flags:  []flagDef{{Name: "note", Type: typeString, Description: "why the grant is voided, kept in grant history"}},
		Output: "{dry_run, changed, voided, grant: " + grantOut + " as it was}",
		run:    cmdGrantVoid,
	},
	{
		Name: "vest record", Summary: "record that a planned vest happened: its price observation, and a transfer of the net proceeds into the grant's brokerage account, or, with --entry, a tie to an existing deposit and no second one",
		Args: []argDef{{Name: "grant-id", Description: "grant id, as account grants lists it", Required: true}},
		Flags: append([]flagDef{
			{Name: "planned", Type: typeInt, Required: true, Description: "the 1-based number of the planned vest, as grant schedule lists it"},
			{Name: "price", Type: typeNumber, Required: true, Description: "the stock's price on the vest date"},
			{Name: "date", Type: typeDate, Description: "the vest date, if it differs from the planned one: " + dateNote},
			{Name: "shares", Type: typeInt, Description: "the shares that vested, if they differ from the planned ones"},
			{Name: "withheld", Type: typeNumber, Description: "the amount withheld, instead of the grant's rates"},
			{Name: "entry", Type: typeInt, Description: "id of an existing entry of the grant's brokerage account that deposited the proceeds, as account entries lists it; no deposit is recorded"},
		}, rateFlags()...),
		Output: "{dry_run, changed, vest: " + vestOut + ", price: the observation recorded as price history prints it, null if one with the price was there, entry: the deposit recorded as account entries prints it, null when tied to an existing one}; ids are null in a dry run",
		run:    cmdVestRecord,
	},
	{
		Name: "vest correct", Summary: "correct a recorded vest: only the values given change, and the earlier values stay in vest history; its deposit and price observation are not changed. Correcting a voided vest restores it",
		Args: []argDef{{Name: "vest-id", Description: "vest id, as grant schedule lists it", Required: true}},
		Flags: append([]flagDef{
			{Name: "planned", Type: typeInt, Description: "the planned vest it fulfils"},
			{Name: "date", Type: typeDate, Description: "the vest date: " + dateNote},
			{Name: "shares", Type: typeInt, Description: "the shares that vested"},
			{Name: "price", Type: typeNumber, Description: "the stock's price on the vest date"},
			{Name: "withheld", Type: typeNumber, Description: "the amount withheld, instead of rates"},
			{Name: "entry", Type: typeInt, Description: "id of the brokerage entry that deposited the proceeds"},
			{Name: "note", Type: typeString, Description: "why the vest is corrected, kept in vest history"},
		}, rateFlags()...),
		Output: "{dry_run, changed, vest: " + vestOut + " with the values after the correction}",
		run:    cmdVestCorrect,
	},
	{
		Name: "vest void", Summary: "take a recorded vest out of the record, with its history kept; its deposit and price observation stay: void those with entry void and price void",
		Args:   []argDef{{Name: "vest-id", Description: "vest id, as grant schedule lists it", Required: true}},
		Flags:  []flagDef{{Name: "note", Type: typeString, Description: "why the vest is voided, kept in vest history"}},
		Output: "{dry_run, changed, voided, vest: " + vestOut + " as it was}",
		run:    cmdVestVoid,
	},
	{
		Name: "price add", Summary: "record the price of one share of a stock on a date",
		Args: []argDef{{Name: "symbol", Description: "stock symbol", Required: true}},
		Flags: []flagDef{
			{Name: "date", Type: typeDate, Required: true, Description: "the day: " + dateNote},
			{Name: "price", Type: typeNumber, Required: true, Description: "price of one share"},
		},
		Output: "{dry_run, changed, price: " + priceOut + "}; the id is null in a dry run",
		run:    cmdPriceAdd,
	},
	{
		Name: "price correct", Summary: "correct a price observation: only the values given change, and the earlier values stay in price revisions. Correcting a voided observation restores it",
		Args: []argDef{{Name: "price-id", Description: "price observation id, as price history lists it", Required: true}},
		Flags: []flagDef{
			{Name: "symbol", Type: typeString, Description: "stock symbol"},
			{Name: "date", Type: typeDate, Description: "the day: " + dateNote},
			{Name: "price", Type: typeNumber, Description: "price of one share"},
			{Name: "note", Type: typeString, Description: "why the price is corrected, kept in price revisions"},
		},
		Output: "{dry_run, changed, price: " + priceOut + " with the values after the correction}",
		run:    cmdPriceCorrect,
	},
	{
		Name: "price void", Summary: "take a price observation out of the record, with its history kept",
		Args:   []argDef{{Name: "price-id", Description: "price observation id, as price history lists it", Required: true}},
		Flags:  []flagDef{{Name: "note", Type: typeString, Description: "why the price is voided, kept in price revisions"}},
		Output: "{dry_run, changed, voided, price: " + priceOut + " as it was}",
		run:    cmdPriceVoid,
	},
}

func calendarFlags(required bool) []flagDef {
	return []flagDef{
		{Name: "rules", Type: typeString, Required: required, Description: "the vest months: " + rulesNote},
		{Name: "overrides", Type: typeString, Description: "the dates moved: " + overridesNote + "; empty removes them all"},
	}
}

func init() {
	commands = append(commands, equityReadCommands...)
	writeCommands = append(writeCommands, equityWriteCommands...)
}

func cmdCalendarList(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	out := []jcalendar{}
	for _, cal := range rec.Calendars {
		out = append(out, calendarJSON(rec, cal))
	}
	return out, nil
}

// calendarNamed returns the calendar the name gives.
func (c *call) calendarNamed(name string) (*model.VestCalendar, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	cal := rec.Calendar(name)
	if cal == nil {
		return nil, failErr("not_found", "there is no vest calendar %q", name)
	}
	return cal, nil
}

func cmdCalendarDates(c *call) (any, error) {
	cal, err := c.calendarNamed(c.args[0])
	if err != nil {
		return nil, err
	}
	from, _, err := c.date("from")
	if err != nil {
		return nil, err
	}
	to, _, err := c.date("to")
	if err != nil {
		return nil, err
	}
	dates := []jdate{}
	for _, d := range cal.Dates(from, to) {
		dates = append(dates, jd(d))
	}
	return struct {
		Calendar string  `json:"calendar"`
		Dates    []jdate `json:"dates"`
	}{cal.Name, dates}, nil
}

func cmdAccountGrants(c *call) (any, error) {
	acct, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	out := []jgrant{}
	for _, g := range c.rec.Account(acct).Grants {
		out = append(out, grantJSON(c.rec, g))
	}
	return out, nil
}

// factArg reads the id at pos and opens the database.
func (c *call) factArg(pos int, what string) (int64, error) {
	id, err := strconv.ParseInt(c.args[pos], 10, 64)
	if err != nil {
		return 0, usageErr("%s id %q is not a number", what, c.args[pos])
	}
	if _, _, err := c.open(); err != nil {
		return 0, err
	}
	return id, nil
}

func cmdGrantSchedule(c *call) (any, error) {
	id, err := c.factArg(0, "grant")
	if err != nil {
		return nil, err
	}
	g, rows, err := ops.GrantSchedule(c.rec, id)
	if err != nil {
		return nil, opsError(err)
	}
	type jrow struct {
		Planned   int     `json:"planned"`
		Date      jdate   `json:"date"`
		Percent   float64 `json:"percent"`
		Shares    int     `json:"shares"`
		Status    string  `json:"status"`
		Vest      *jvest  `json:"vest"`
		DateDiff  *int    `json:"date_diff"`
		ShareDiff *int    `json:"share_diff"`
	}
	out := struct {
		Grant jgrant `json:"grant"`
		Vests []jrow `json:"vests"`
	}{grantJSON(c.rec, *g), []jrow{}}
	for _, r := range rows {
		j := jrow{Planned: r.Index, Date: jd(r.Date), Percent: r.Percent, Shares: r.Shares, Status: r.Status}
		if r.Vest != nil {
			v := vestJSON(*r.Vest)
			j.Vest, j.DateDiff, j.ShareDiff = &v, &r.DateDiff, &r.ShareDiff
		}
		out.Vests = append(out.Vests, j)
	}
	return out, nil
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

// factRevisions returns the id the first argument gives and the revisions of the fact of the kind.
func (c *call) factRevisions(kind store.FactKind, what string) (int64, []store.FactRevision, error) {
	id, err := c.factArg(0, what)
	if err != nil {
		return 0, nil, err
	}
	revs, err := ops.FactHistory(c.st, id, kind)
	if err != nil {
		return 0, nil, opsError(err)
	}
	return id, revs, nil
}

type jrevisionTail struct {
	MadeAt string  `json:"made_at"`
	Note   *string `json:"note"`
	Void   bool    `json:"void"`
}

func revisionTail(r store.FactRevision) jrevisionTail {
	return jrevisionTail{r.MadeAt.Format(timeLayout), nullable(r.Note), r.Void}
}

func cmdGrantHistory(c *call) (any, error) {
	id, revs, err := c.factRevisions(store.FactGrant, "grant")
	if err != nil {
		return nil, err
	}
	type jrevision struct {
		jgrantFields
		jrevisionTail
	}
	out := struct {
		ID        int64       `json:"id"`
		Revisions []jrevision `json:"revisions"`
	}{ID: id, Revisions: []jrevision{}}
	for _, r := range revs {
		out.Revisions = append(out.Revisions, jrevision{grantFields(*r.Grant), revisionTail(r)})
	}
	return out, nil
}

func cmdVestHistory(c *call) (any, error) {
	id, revs, err := c.factRevisions(store.FactVest, "vest")
	if err != nil {
		return nil, err
	}
	type jrevision struct {
		jvest
		jrevisionTail
	}
	out := struct {
		ID        int64       `json:"id"`
		Revisions []jrevision `json:"revisions"`
	}{ID: id, Revisions: []jrevision{}}
	for _, r := range revs {
		out.Revisions = append(out.Revisions, jrevision{vestJSON(*r.Vest), revisionTail(r)})
	}
	return out, nil
}

func cmdPriceRevisions(c *call) (any, error) {
	id, revs, err := c.factRevisions(store.FactPrice, "price")
	if err != nil {
		return nil, err
	}
	type jrevision struct {
		jprice
		jrevisionTail
	}
	out := struct {
		ID        int64       `json:"id"`
		Revisions []jrevision `json:"revisions"`
	}{ID: id, Revisions: []jrevision{}}
	for _, r := range revs {
		out.Revisions = append(out.Revisions, jrevision{priceJSON(*r.Price), revisionTail(r)})
	}
	return out, nil
}

func cmdPriceHistory(c *call) (any, error) {
	st, _, err := c.open()
	if err != nil {
		return nil, err
	}
	symbol := strings.ToUpper(strings.TrimSpace(c.args[0]))
	facts, err := st.FactsWithVoided(store.FactPrice)
	if err != nil {
		return nil, failErr("database", "%v", err)
	}
	voided := c.str("voided") == "true"
	type jpriceVoid struct {
		jprice
		Void bool `json:"void"`
	}
	var kept []store.Fact
	for _, f := range facts {
		if f.Price.Symbol == symbol && (voided || !f.Void) {
			kept = append(kept, f)
		}
	}
	slices.SortStableFunc(kept, func(a, b store.Fact) int {
		if a.Price.Date != b.Price.Date {
			return int(a.Price.Date - b.Price.Date)
		}
		return int(a.ID - b.ID)
	})
	if voided {
		out := []jpriceVoid{}
		for _, f := range kept {
			out = append(out, jpriceVoid{priceJSON(*f.Price), f.Void})
		}
		return out, nil
	}
	out := []jprice{}
	for _, f := range kept {
		out = append(out, priceJSON(*f.Price))
	}
	return out, nil
}

func cmdCalendarNew(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	rules, err := parseRules(c.str("rules"))
	if err != nil {
		return nil, err
	}
	cal := model.VestCalendar{Name: c.args[0], Rules: rules}
	if cal.Overrides, err = parseOverrides(c.str("overrides")); err != nil {
		return nil, err
	}
	res, err := ops.NewCalendar(st, rec, cal, c.options())
	return c.calendarResult(res, err, strings.TrimSpace(c.args[0]))
}

func cmdCalendarEdit(c *call) (any, error) {
	cal, err := c.calendarNamed(c.args[0])
	if err != nil {
		return nil, err
	}
	rules, overrides := cal.Rules, cal.Overrides
	if c.set["rules"] {
		if rules, err = parseRules(c.str("rules")); err != nil {
			return nil, err
		}
	}
	if c.set["overrides"] {
		if overrides, err = parseOverrides(c.str("overrides")); err != nil {
			return nil, err
		}
	}
	res, err := ops.EditCalendar(c.st, c.rec, cal.Name, rules, overrides, c.options())
	return c.calendarResult(res, err, cal.Name)
}

func cmdCalendarDelete(c *call) (any, error) {
	cal, err := c.calendarNamed(c.args[0])
	if err != nil {
		return nil, err
	}
	res, err := ops.DeleteCalendar(c.st, c.rec, cal.Name, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "removed": res.Removed, "calendar": calendarJSON(c.rec, res.Calendar)}, nil
}

// calendarResult is the output of an edit of the calendar.
func (c *call) calendarResult(res *ops.Result, err error, name string) (any, error) {
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	rec, cal := c.rec, c.rec.Calendar(name)
	if changed {
		rec, cal = res.Record, res.Calendar
	}
	out["calendar"] = calendarJSON(rec, cal)
	return out, nil
}

// recordAfter returns the record the result's operation produced: the reloaded one, the dry run's copy, or the current one for an edit that changed nothing.
func (c *call) recordAfter(res *ops.Result, changed bool) *model.Record {
	if changed && res.Record != nil {
		return res.Record
	}
	return c.rec
}

func cmdGrantAdd(c *call) (any, error) {
	acct, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	in := ops.GrantInput{Account: acct, Symbol: c.str("symbol"), Calendar: c.str("calendar")}
	if in.Date, _, err = c.date("date"); err != nil {
		return nil, err
	}
	if in.FirstVest, _, err = c.date("first-vest"); err != nil {
		return nil, err
	}
	if in.Price, _, err = c.number("price"); err != nil {
		return nil, err
	}
	if in.Value, _, err = c.number("value"); err != nil {
		return nil, err
	}
	if in.Shares, _, err = c.integer("shares"); err != nil {
		return nil, err
	}
	if in.CliffMonths, _, err = c.integer("cliff-months"); err != nil {
		return nil, err
	}
	if in.YearPercents, err = parsePercents("year-percents", c.str("year-percents")); err != nil {
		return nil, err
	}
	if c.set["vest-percents"] {
		if in.VestPercents, err = parsePercents("vest-percents", c.str("vest-percents")); err != nil {
			return nil, err
		}
	}
	if rates, ok, err := c.ratesFrom(model.DefaultWithholding); err != nil {
		return nil, err
	} else if ok {
		in.Rates = &rates
	}
	if in.Brokerage, err = c.accountNamed(c.str("brokerage")); err != nil {
		return nil, err
	}
	res, err := ops.AddGrant(c.st, c.rec, in, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "grant": grantJSON(res.Record, *res.Grant)}, nil
}

func cmdGrantCorrect(c *call) (any, error) {
	id, err := c.factArg(0, "grant")
	if err != nil {
		return nil, err
	}
	var ch ops.GrantChange
	if c.set["symbol"] {
		ch.Symbol = model.Ptr(c.str("symbol"))
	}
	if c.set["calendar"] {
		ch.Calendar = model.Ptr(c.str("calendar"))
	}
	if d, ok, err := c.date("date"); err != nil {
		return nil, err
	} else if ok {
		ch.Date = &d
	}
	if d, ok, err := c.date("first-vest"); err != nil {
		return nil, err
	} else if ok {
		ch.FirstVest = &d
	}
	if v, ok, err := c.number("price"); err != nil {
		return nil, err
	} else if ok {
		ch.Price = &v
	}
	if v, ok, err := c.number("value"); err != nil {
		return nil, err
	} else if ok {
		ch.Value = &v
	}
	if v, ok, err := c.integer("shares"); err != nil {
		return nil, err
	} else if ok {
		ch.Shares = &v
	}
	if v, ok, err := c.integer("cliff-months"); err != nil {
		return nil, err
	} else if ok {
		ch.CliffMonths = &v
	}
	if c.set["year-percents"] {
		if ch.YearPercents, err = parsePercents("year-percents", c.str("year-percents")); err != nil {
			return nil, err
		}
	}
	if c.set["vest-percents"] {
		if ch.VestPercents, err = parsePercents("vest-percents", c.str("vest-percents")); err != nil {
			return nil, err
		}
	}
	if g, _ := c.rec.Grant(id); g != nil {
		if rates, ok, err := c.ratesFrom(g.Rates); err != nil {
			return nil, err
		} else if ok {
			ch.Rates = &rates
		}
	} else if c.set["federal"] || c.set["state"] || c.set["fica"] {
		stored, err := c.st.Fact(id)
		if err != nil || stored.Grant == nil {
			return nil, failErr("not_found", "there is no grant %d", id)
		}
		rates, _, err := c.ratesFrom(stored.Grant.Rates)
		if err != nil {
			return nil, err
		}
		ch.Rates = &rates
	}
	if c.set["brokerage"] {
		b, err := c.accountNamed(c.str("brokerage"))
		if err != nil {
			return nil, err
		}
		ch.Brokerage = &b
	}
	if reflect.ValueOf(ch).IsZero() {
		return nil, usageErr("grant correct needs at least one value to change")
	}
	res, err := ops.CorrectGrant(c.st, c.rec, id, ch, c.str("note"), c.options())
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	rec, g := c.rec, model.Grant{}
	if changed {
		rec, g = c.recordAfter(res, true), *res.Grant
	} else if cur, _ := c.rec.Grant(id); cur != nil {
		g = *cur
	}
	out["grant"] = grantJSON(rec, g)
	return out, nil
}

func cmdGrantVoid(c *call) (any, error) {
	id, err := c.factArg(0, "grant")
	if err != nil {
		return nil, err
	}
	res, err := ops.VoidGrant(c.st, c.rec, id, c.str("note"), c.options())
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	if changed {
		out["voided"] = true
		out["grant"] = grantJSON(c.rec, *res.Grant)
	} else if f, err := c.st.Fact(id); err == nil {
		out["voided"] = true
		out["grant"] = grantJSON(c.rec, *f.Grant)
	}
	return out, nil
}

func cmdVestRecord(c *call) (any, error) {
	id, err := c.factArg(0, "grant")
	if err != nil {
		return nil, err
	}
	in := ops.VestInput{Grant: id}
	if in.Planned, _, err = c.integer("planned"); err != nil {
		return nil, err
	}
	if in.Price, _, err = c.number("price"); err != nil {
		return nil, err
	}
	if d, ok, err := c.date("date"); err != nil {
		return nil, err
	} else if ok {
		in.Date = &d
	}
	if n, ok, err := c.integer("shares"); err != nil {
		return nil, err
	} else if ok {
		in.Shares = &n
	}
	if w, ok, err := c.number("withheld"); err != nil {
		return nil, err
	} else if ok {
		in.Withheld = &w
	}
	if n, ok, err := c.integer("entry"); err != nil {
		return nil, err
	} else if ok {
		in.TxEntry = int64(n)
	}
	if g, _ := c.rec.Grant(id); g != nil {
		if rates, ok, err := c.ratesFrom(g.Rates); err != nil {
			return nil, err
		} else if ok {
			in.Rates = &rates
		}
	}
	res, err := ops.RecordVest(c.st, c.rec, in, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	out := map[string]any{"dry_run": c.options().DryRun, "changed": true, "vest": vestJSON(*res.Vest), "price": nil, "entry": nil}
	if res.Price != nil {
		out["price"] = priceJSON(*res.Price)
	}
	if res.Entry != nil {
		out["entry"] = entryJSON(*res.Entry)
	}
	return out, nil
}

func cmdVestCorrect(c *call) (any, error) {
	id, err := c.factArg(0, "vest")
	if err != nil {
		return nil, err
	}
	var ch ops.VestChange
	if n, ok, err := c.integer("planned"); err != nil {
		return nil, err
	} else if ok {
		ch.Planned = &n
	}
	if d, ok, err := c.date("date"); err != nil {
		return nil, err
	} else if ok {
		ch.Date = &d
	}
	if n, ok, err := c.integer("shares"); err != nil {
		return nil, err
	} else if ok {
		ch.Shares = &n
	}
	if v, ok, err := c.number("price"); err != nil {
		return nil, err
	} else if ok {
		ch.Price = &v
	}
	if v, ok, err := c.number("withheld"); err != nil {
		return nil, err
	} else if ok {
		ch.Withheld = &v
	}
	if n, ok, err := c.integer("entry"); err != nil {
		return nil, err
	} else if ok {
		ch.TxEntry = model.Ptr(int64(n))
	}
	if c.set["federal"] || c.set["state"] || c.set["fica"] {
		stored, err := c.st.Fact(id)
		if err != nil || stored.Vest == nil {
			return nil, failErr("not_found", "there is no vest %d", id)
		}
		rates, _, err := c.ratesFrom(stored.Vest.Rates)
		if err != nil {
			return nil, err
		}
		ch.Rates = &rates
	}
	if reflect.ValueOf(ch).IsZero() {
		return nil, usageErr("vest correct needs at least one value to change")
	}
	res, err := ops.CorrectVest(c.st, c.rec, id, ch, c.str("note"), c.options())
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	if changed {
		out["vest"] = vestJSON(*res.Vest)
	} else if v, _ := c.rec.Vest(id); v != nil {
		out["vest"] = vestJSON(*v)
	}
	return out, nil
}

func cmdVestVoid(c *call) (any, error) {
	id, err := c.factArg(0, "vest")
	if err != nil {
		return nil, err
	}
	res, err := ops.VoidVest(c.st, c.rec, id, c.str("note"), c.options())
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	if changed {
		out["voided"] = true
		out["vest"] = vestJSON(*res.Vest)
	} else if f, err := c.st.Fact(id); err == nil {
		out["voided"] = true
		out["vest"] = vestJSON(*f.Vest)
	}
	return out, nil
}

func cmdPriceAdd(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	date, _, err := c.date("date")
	if err != nil {
		return nil, err
	}
	price, _, err := c.number("price")
	if err != nil {
		return nil, err
	}
	res, err := ops.AddPrice(st, rec, c.args[0], date, price, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "price": priceJSON(*res.Price)}, nil
}

func cmdPriceCorrect(c *call) (any, error) {
	id, err := c.factArg(0, "price")
	if err != nil {
		return nil, err
	}
	var ch ops.PriceChange
	if c.set["symbol"] {
		ch.Symbol = model.Ptr(c.str("symbol"))
	}
	if d, ok, err := c.date("date"); err != nil {
		return nil, err
	} else if ok {
		ch.Date = &d
	}
	if v, ok, err := c.number("price"); err != nil {
		return nil, err
	} else if ok {
		ch.Price = &v
	}
	if ch == (ops.PriceChange{}) {
		return nil, usageErr("price correct needs at least one of --symbol, --date and --price")
	}
	res, err := ops.CorrectPrice(c.st, c.rec, id, ch, c.str("note"), c.options())
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	if changed {
		out["price"] = priceJSON(*res.Price)
	} else if f, err := c.st.Fact(id); err == nil {
		out["price"] = priceJSON(*f.Price)
	}
	return out, nil
}

func cmdPriceVoid(c *call) (any, error) {
	id, err := c.factArg(0, "price")
	if err != nil {
		return nil, err
	}
	res, err := ops.VoidPrice(c.st, c.rec, id, c.str("note"), c.options())
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	if changed {
		out["voided"] = true
		out["price"] = priceJSON(*res.Price)
	} else if f, err := c.st.Fact(id); err == nil {
		out["voided"] = true
		out["price"] = priceJSON(*f.Price)
	}
	return out, nil
}
