// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

const (
	targetArg = "the asset or account to look at, by asset id, asset name, account id or account name (ids win over names); an account stands for its own default asset"
	dateNote  = "YYYY-MM-DD, or YYYY-MM for the first day of the month"
	endNote   = "YYYY-MM-DD, or YYYY-MM for the last day of the month"
	projDesc  = "projection to evaluate under, whose own accounts and those it inherits can be targets; reality when omitted"
	diagShape = "diagnostics {skipped: [{projection, index, target: {kind, id}, property, reason}], overlaps: [{asset, seq, member, duplicates, against}], notes: [problems with the tax calculation, contributions the 401(k) limit refused, the date money runs out under a retirement date, the date a 529 account runs out and checking takes over its college costs, and a Roth conversion bracket with no Roth IRA to convert into]}"
	jsonNote  = "Dates are YYYY-MM-DD, null when absent or unbounded. Money is a plain number. Statuses are lowercase: default, actual, projected, partly_projected."
)

// commands is every CLI command, in help order.
var commands = []*command{
	{
		Name: "help", Summary: "describe the commands and their flags",
		Flags:  []flagDef{{Name: "json", Type: typeBool, Default: "false", Description: "print the description as JSON for programs"}},
		Output: "text; with --json {global_flags: [{name, type, default, required, description}], commands: [{name, summary, args, flags, output, write, input}]}; write is true for a command that changes the database. " + jsonNote,
		run:    cmdHelp,
	},
	{
		Name: "account list", Summary: "list the accounts",
		Output: "[{id, name, type, tax, stated_rate (a fraction per year), default: [{from, to, value}], closed, allocation: [{class, percent}], rebalance_yearly, beneficiary, basis, balance, last_data, stale}]; beneficiary and basis are a 529's beneficiary (a dependent's name) and contributions as of its latest balance, null on any other account and while unset; allocation is the account's split among market classes in percent, empty for an account with none, which grows at its stated_rate; rebalance_yearly is true if projections rebalance the account to its allocation every 1 January; default is the default balance intervals in the order added, the last added winning where they overlap; closed is the date the account was closed, null while it is open, and it reads 0 after that date; balance is the balance on the record's today (0 once today is after the account's close date); last_data is the date of the latest record entry, null without a snapshot; stale is true for an open account whose last_data is more than 45 days before the record's today, as the Graph labels it",
		run:    cmdAccountList,
	},
	{
		Name: "account entries", Summary: "list an account's record entries (snapshots and transactions) with their current values, in the order they were stored; voided entries are left out unless --voided is given",
		Args:   []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Flags:  []flagDef{{Name: "voided", Type: typeBool, Default: "false", Description: "include voided entries, marked void: true"}},
		Output: "[{id, kind (snapshot or transaction), date, amount, tx_kind, counter, memo}]; empty fields are null; with --voided each also has void (true for a voided entry)",
		run:    cmdAccountEntries,
	},
	{
		Name: "entry history", Summary: "show every revision of one entry: its original values, then each correction or void",
		Args:   []argDef{{Name: "entry-id", Description: "entry id, as listed by account entries", Required: true}},
		Output: "{id, revisions: [{date, amount, tx_kind, counter, memo, made_at (RFC 3339), note, void}]}",
		run:    cmdEntryHistory,
	},
	{
		Name: "asset list", Summary: "list the assets with their members and any members left out as invalid",
		Output: "[{id, name, kind: account for an account's default asset, else asset, members: [{seq, member, account, asset, rule: {types, tax} or null}], invalid: [{seq, member, duplicates, against}], error, default: [{from, to, value}], params: {growth: [{from, end, value}]}}]; error is null unless the asset cannot be resolved, for example because of a cycle; default is the default balance intervals, an account's default asset (id acct:<account>) showing the account's, and params the asset's parameter settings",
		run:    cmdAssetList,
	},
	{
		Name: "asset params", Summary: "show an asset's default balance intervals and growth setting, and for each account under it the layers of its growth and the effective value",
		Args:   []argDef{{Name: "asset", Description: assetNote, Required: true}},
		Output: "{asset, default: [{from, to, value}] (in the order they apply, the last winning where they overlap), growth: [{from, end, value}] or null, today, accounts: [{account, name, layers: [{source, asset, schedule: [{from, end, value}], today, applies}], effective: [{from, end, value}], today}]}; layers are in the order they apply: source is stated_rate (asset null) for the account's stated rate, then asset for each asset's growth setting, the last applied winning on each date; today is a layer's or the effective value on the record's today, null where it has none, and applies marks the layer that sets the effective value today, the layers before it being overwritten",
		run:    cmdAssetParams,
	},
	{
		Name: "budget list", Summary: "list the budget items",
		Output: "[{id, name, kind, amount, amount_schedule: [{from, end, value}], recurrence: {kind, anchor, month, day, day2, count, unit}, start, end, indexed, growth, growth_schedule: [{from, end, value}], account, from, to}]; amount and growth are the values in effect on the record's today (growth 0 for none), and the schedules hold every segment, growth_schedule being empty for no growth; recurrence kind is one-time, weekly, biweekly, semimonthly, monthly, quarterly, annual (shown as Annually) or custom, and the recurrence fields its kind does not use are null: anchor is the date a one-time item happens or an interval item counts from (null when the start is used), month and day are for annual, day for monthly, day and day2 for semimonthly, count and unit (days, weeks, months or years) for custom; growth is the annual raise as a fraction (0.03 for 3%)",
		run:    cmdBudgetList,
	},
	{
		Name: "budget totals", Summary: "show the monthly income and expenses of the budget items active on the record's today",
		Output: "{as_of, monthly_income, monthly_expenses, monthly_net}; amounts are in today's dollars, rounded to cents, with no raises or inflation applied: the amount in effect on as_of counts, a monthly item once a month, a semimonthly one twice, a biweekly one 26/12 times, a weekly one 4.348 times, a quarterly one a third, an annual one a twelfth, a one-time one not at all, and a custom frequency by its average: every N days 30.4375/N times a month, every N weeks 4.348/N, every N months 1/N, every N years 1/(12N); transfers are in neither total; an item counts when its start is empty or not after as_of and its end is empty or not before it, and a biweekly item's first date (its recurrence anchor) is not after as_of",
		run:    cmdBudgetTotals,
	},
	{
		Name: "projection list", Summary: "list the projections",
		Output: "[{name, parent, start, override_history, modification_count}]",
		run:    cmdProjectionList,
	},
	{
		Name: "projection show", Summary: "show a projection's settings, its own hypothetical accounts and its modifications",
		Args:   []argDef{{Name: "name", Description: "projection name", Required: true}},
		Output: "{name, parent, start, override_history, retire_on (the date wages stop and the household draws on its accounts), withdrawal_order ([taxable, tax-deferred, roth] in the order they are drawn on), roth_bracket (the federal bracket rate each year's Roth conversion fills to, a fraction: 0.22 for 22%); each of the three is null when the projection inherits it, asset_settings: [{asset, start, override_history}], accounts: [{id, name, type, tax, stated_rate, opening, from}] (the hypothetical accounts the projection defines itself, not those it inherits), modifications: [{index, target: {kind, id}, property, mode (set or scale), schedule: [{from, end, value}], item}], shocks: [{index, class, date, percent (-40 for a 40% drop)}], rebalances: [{index, account, date, allocation: [{class, percent}]}]}; modifications are in the order they apply; shocks and rebalances are in the order they were added, each numbered from 0",
		run:    cmdProjectionShow,
	},
	{
		Name: "balance", Summary: "the value of an asset or account on a series of dates",
		Args: []argDef{{Name: "target", Description: targetArg, Required: true}},
		Flags: []flagDef{
			{Name: "from", Type: typeDate, Required: true, Description: "first sample date: " + dateNote},
			{Name: "to", Type: typeDate, Required: true, EndOfMonth: true, Description: "last sample date, inclusive: " + endNote},
			{Name: "step", Type: typeEnum, Default: "month", Choices: []string{"day", "week", "month", "year"}, Description: "spacing of the samples; month and year keep the day of the month of --from, clamped to the end of shorter months"},
			{Name: "projection", Type: typeString, Description: projDesc},
		},
		Output: "{target (asset id), projection, from, to, step, samples: [{date, value, status}], " + diagShape + "}. " + jsonNote,
		run:    cmdBalance,
	},
	{
		Name: "montecarlo", Summary: "percentile bands of the value of an asset or account on a series of dates over many random runs; each run draws every market class's yearly return (correlated, with the class's volatility) and every stock's yearly price return (with the stock's volatility) around the expected rates, from where the projection starts",
		Args: []argDef{{Name: "target", Description: targetArg, Required: true}},
		Flags: []flagDef{
			{Name: "from", Type: typeDate, Required: true, Description: "first sample date: " + dateNote},
			{Name: "to", Type: typeDate, Required: true, EndOfMonth: true, Description: "last sample date, inclusive: " + endNote},
			{Name: "step", Type: typeEnum, Default: "month", Choices: []string{"day", "week", "month", "year"}, Description: "spacing of the samples; month and year keep the day of the month of --from, clamped to the end of shorter months"},
			{Name: "projection", Type: typeString, Description: projDesc},
			{Name: "runs", Type: typeInt, Default: "1000", Description: "the number of random runs, at least 1"},
			{Name: "seed", Type: typeInt, Default: "1", Description: "the seed of the random draws, 0 or more; the same seed gives the same bands"},
			{Name: "percentiles", Type: typeString, Default: "10,25,50,75,90", Description: "comma-separated percentiles to report, each from 0 to 100"},
			{Name: "success-age", Type: typeInt, Default: "95", Description: "the age of the household's first earner up to which a run must stay above zero to count as a success, 1 or more"},
		},
		Output: "{target (asset id), projection, from, to, step, runs, seed, percentiles: [numbers], dates: [date], bands: [{percentile, values: [value per date]}] (in the order of --percentiles), success_age, success_through (the earlier of --to and the date the first earner reaches the success age), success_percent (the percent of runs, 0 to 100, whose value is above zero at every sampled date from today through success_through; null when the household has no earner, the age is before today or no sampled date lies in that stretch)}. " + jsonNote,
		run:    cmdMonteCarlo,
	},
	{
		Name: "breakdown", Summary: "split the change in an asset or account over a period by cause",
		Args: []argDef{{Name: "target", Description: targetArg, Required: true}},
		Flags: []flagDef{
			{Name: "from", Type: typeDate, Required: true, Description: "start of the period (the change counts after this date): " + dateNote},
			{Name: "to", Type: typeDate, Required: true, EndOfMonth: true, Description: "end of the period, inclusive: " + endNote},
			{Name: "projection", Type: typeString, Description: projDesc},
		},
		Output: "{target (asset id), projection, from, to, start, end, total, lines: [{cause, class, actual, projected}], " + diagShape + "}. class is null except on the growth lines of an account with an allocation, which has one growth line per market class, in the order of the classes, instead of a single one, and on shock lines, the change a market shock made to the holdings of a class (cause shock; cause gains tax is the part of a year's tax that realized capital gains add, and tax the rest). " + jsonNote,
		run:    cmdBreakdown,
	},
	{
		Name: "starts", Summary: "where each account switches from recorded data to projection",
		Args:   []argDef{{Name: "target", Description: targetArg + "; every account when omitted"}},
		Flags:  []flagDef{{Name: "projection", Type: typeString, Description: projDesc}},
		Output: "[{account, start, last_actual, has_actual, override_history, projection_start}]; last_actual is null without actual data",
		run:    cmdStarts,
	},
}

type jbudgetItem struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Kind       string      `json:"kind"`
	Amount     float64     `json:"amount"`
	AmountSch  []jsegment  `json:"amount_schedule"`
	Recurrence jrecurrence `json:"recurrence"`
	Start      *jdate      `json:"start"`
	End        *jdate      `json:"end"`
	Indexed    bool        `json:"indexed"`
	Growth     float64     `json:"growth"`
	GrowthSch  []jsegment  `json:"growth_schedule"`
	Account    *string     `json:"account"`
	From       *string     `json:"from"`
	To         *string     `json:"to"`
	// IncomeType is how an income item is taxed, wages when none is set; null for the other kinds.
	IncomeType *string `json:"income_type"`
	// Match is the employer match of a wages income item; null for an item without one.
	Match *jmatch `json:"match"`
}

// jmatch is an employer match: the percent of the employee's contributions to the account that the employer deposits, up to up_to percent of each paycheck's gross.
type jmatch struct {
	Percent float64 `json:"percent"`
	UpTo    float64 `json:"up_to"`
	Account string  `json:"account"`
}

type jrecurrence struct {
	Kind   string  `json:"kind"`
	Anchor *jdate  `json:"anchor"`
	Month  *int    `json:"month"`
	Day    *int    `json:"day"`
	Day2   *int    `json:"day2"`
	Count  *int    `json:"count"`
	Unit   *string `json:"unit"`
}

// budgetItemJSON is the JSON shape of a budget item. Its amount and growth are the values in effect on today, 0 where there is none, and the schedules hold the rest.
func budgetItemJSON(it model.BudgetItem, today model.Date) jbudgetItem {
	r := it.Recurrence
	jr := jrecurrence{Kind: string(r.Kind)}
	switch r.Kind {
	case model.Biweekly, model.OneTime, model.Weekly, model.Quarterly, model.Custom:
		if r.Anchor != 0 || r.Kind == model.Biweekly || r.Kind == model.OneTime {
			jr.Anchor = model.Ptr(jd(r.Anchor))
		}
		if r.Kind == model.Custom {
			jr.Count, jr.Unit = &r.Count, model.Ptr(string(r.Unit))
		}
	case model.Monthly:
		jr.Day = &r.Day
	case model.Semimonthly:
		jr.Day, jr.Day2 = &r.Day, &r.Day2
	case model.Annual:
		m := int(r.Month)
		jr.Month, jr.Day = &m, &r.Day
	}
	amount, growth := valueListed(it.Amount, today), valueListed(it.Growth, today)
	return jbudgetItem{
		ID: it.ID, Name: it.Name, Kind: string(it.Kind), Amount: amount, AmountSch: scheduleJSONOut(it.Amount), Recurrence: jr,
		Start: jdp(it.Start), End: jdp(it.End), Indexed: it.Indexed, Growth: growth, GrowthSch: scheduleJSONOut(it.Growth),
		Account: nullable(string(it.Account)), From: nullable(string(it.From)), To: nullable(string(it.To)),
		IncomeType: incomeTypeJSON(it), Match: matchJSON(it),
	}
}

// matchJSON is the employer match of an item, null when it has none.
func matchJSON(it model.BudgetItem) *jmatch {
	if it.MatchPercent == 0 && it.MatchUpTo == 0 && it.MatchAccount == "" {
		return nil
	}
	return &jmatch{it.MatchPercent, it.MatchUpTo, string(it.MatchAccount)}
}

// incomeTypeJSON is the income type of an income item, null for the other kinds.
func incomeTypeJSON(it model.BudgetItem) *string {
	if it.Kind != model.ItemIncome {
		return nil
	}
	return model.Ptr(string(it.Income()))
}

// valueListed is the number budget list prints for a schedule: its value today, 0 when no segment covers today.
func valueListed(s model.Schedule, today model.Date) float64 {
	v, _ := s.At(today)
	return v
}

// candidate is an object a target name can mean.
type candidate struct {
	asset   model.AssetID
	account model.AccountID // set when the object is an account
	label   string
}

// findCandidates returns the distinct objects whose id, or with byName their name, is s. An account and its default asset are one object.
func findCandidates(rec *model.Record, s string, byName, accountsOnly bool) []candidate {
	var out []candidate
	add := func(c candidate) {
		for _, o := range out {
			if o.asset == c.asset {
				return
			}
		}
		out = append(out, c)
	}
	for _, a := range rec.Accounts {
		key := string(a.ID)
		if byName {
			key = a.Name
		}
		if key == s {
			add(candidate{asset: model.DefaultAssetID(a.ID), account: a.ID, label: fmt.Sprintf("account %q", a.ID)})
		}
	}
	if accountsOnly {
		return out
	}
	for _, a := range rec.Assets {
		key := string(a.ID)
		if byName {
			key = a.Name
		}
		if key == s {
			add(candidate{asset: a.ID, label: fmt.Sprintf("asset %q", a.ID)})
		}
	}
	return out
}

// resolveTarget finds the one object that s names.
func resolveTarget(rec *model.Record, s string, accountsOnly bool) (candidate, error) {
	kinds := "an asset or account"
	if accountsOnly {
		kinds = "an account"
	}
	for _, byName := range []bool{false, true} {
		switch found := findCandidates(rec, s, byName, accountsOnly); len(found) {
		case 0:
		case 1:
			return found[0], nil
		default:
			labels := make([]string, len(found))
			for i, f := range found {
				labels[i] = f.label
			}
			return candidate{}, failErr("ambiguous", "%q matches more than one object: %s", s, strings.Join(labels, ", "))
		}
	}
	return candidate{}, failErr("not_found", "%q is not %s", s, kinds)
}

// projection returns the --projection flag's name after checking that it exists.
func (c *call) projection(rec *model.Record) (string, error) {
	name := c.str("projection")
	if name != "" && rec.Projection(name) == nil {
		return "", failErr("not_found", "there is no projection named %q", name)
	}
	return name, nil
}

type jinterval struct {
	From  jdate   `json:"from"`
	To    *jdate  `json:"to"`
	Value float64 `json:"value"`
}

func defaultJSON(b model.DefaultBalance) []jinterval {
	out := []jinterval{}
	for _, iv := range b.Intervals {
		out = append(out, jinterval{jd(iv.From), jdp(iv.To), iv.Value})
	}
	return out
}

type jaccount struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Type       string      `json:"type"`
	Tax        string      `json:"tax"`
	StatedRate float64     `json:"stated_rate"`
	Default    []jinterval `json:"default"`
	Closed     *jdate      `json:"closed"`
	// Allocation is the account's split among market classes; empty for none.
	Allocation []jholding `json:"allocation"`
	// RebalanceYearly is whether the account rebalances to its allocation every 1 January in a projection.
	RebalanceYearly bool `json:"rebalance_yearly"`
	// The terms of a mortgage, null for any other account; first_payment and paid_from are null while unset, and monthly_payment is null until the terms are complete.
	OriginalAmount *float64 `json:"original_amount"`
	TermMonths     *int     `json:"term_months"`
	FirstPayment   *jdate   `json:"first_payment"`
	PaidFrom       *string  `json:"paid_from"`
	MonthlyPayment *float64 `json:"monthly_payment"`
	// The dependent a 529 is for and its contributions as of its latest balance; null on any other account, and while unset on a 529.
	Beneficiary *string  `json:"beneficiary"`
	Basis       *float64 `json:"basis"`
}

// jaccountRow is an account with what account list adds to it.
type jaccountRow struct {
	jaccount
	Balance  float64 `json:"balance"`
	LastData *jdate  `json:"last_data"`
	Stale    bool    `json:"stale"`
}

// accountJSON is the JSON shape of an account, as account list prints it and the write commands print the account they changed.
func accountJSON(a *model.Account) jaccount {
	j := jaccount{ID: string(a.ID), Name: a.Name, Type: string(a.Type), Tax: string(a.Tax), StatedRate: a.StatedRate, Default: defaultJSON(a.Default), Closed: jdp(a.Closed),
		Allocation: allocationJSON(a.Allocation), RebalanceYearly: a.RebalanceYearly}
	if a.Type == model.TypeMortgage {
		j.OriginalAmount, j.TermMonths, j.FirstPayment, j.PaidFrom = &a.OriginalAmount, &a.TermMonths, jdp(a.FirstPayment), nullable(string(a.PaidFrom))
		if a.Amortized() {
			j.MonthlyPayment = model.Ptr(a.MonthlyPayment())
		}
	}
	if a.Type == model.Type529 {
		j.Beneficiary, j.Basis = nullable(a.Beneficiary), a.Basis
	}
	return j
}

func cmdAccountList(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	out := []jaccountRow{}
	for _, r := range listAccounts(rec) {
		row := jaccountRow{jaccount: accountJSON(r.Account), Balance: r.Balance, Stale: r.Stale}
		if r.HasData {
			row.LastData = model.Ptr(jd(r.LastData))
		}
		out = append(out, row)
	}
	return out, nil
}

type jvalues struct {
	Date    jdate   `json:"date"`
	Amount  float64 `json:"amount"`
	TxKind  *string `json:"tx_kind"`
	Counter *string `json:"counter"`
	Memo    *string `json:"memo"`
}

func valuesJSON(v store.Values) jvalues {
	return jvalues{jd(v.Date), v.Amount, nullable(string(v.TxKind)), nullable(string(v.Counter)), nullable(v.Memo)}
}

type jentry struct {
	ID   *int64 `json:"id"`
	Kind string `json:"kind"`
	jvalues
}

// jentryVoid is an entry as account entries --voided prints it.
type jentryVoid struct {
	jentry
	Void bool `json:"void"`
}

// entryJSON is the JSON shape of an entry, as account entries prints it and the write commands print the entry they changed.
func entryJSON(e store.Entry) jentry {
	je := jentry{Kind: string(e.Kind), jvalues: valuesJSON(e.Values)}
	if e.ID != 0 {
		je.ID = &e.ID
	}
	return je
}

func cmdAccountEntries(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	acct, err := resolveTarget(rec, c.args[0], true)
	if err != nil {
		return nil, err
	}
	if c.str("voided") == "true" {
		entries, err := st.EntriesWithVoided(acct.account)
		if err != nil {
			return nil, failErr("database", "%v", err)
		}
		out := []jentryVoid{}
		for _, e := range entries {
			out = append(out, jentryVoid{entryJSON(e), e.Void})
		}
		return out, nil
	}
	entries, err := st.Entries(acct.account)
	if err != nil {
		return nil, failErr("database", "%v", err)
	}
	out := []jentry{}
	for _, e := range entries {
		out = append(out, entryJSON(e))
	}
	return out, nil
}

func cmdEntryHistory(c *call) (any, error) {
	id, err := strconv.ParseInt(c.args[0], 10, 64)
	if err != nil {
		return nil, usageErr("entry id %q is not a number", c.args[0])
	}
	st, _, err := c.open()
	if err != nil {
		return nil, err
	}
	revs, err := st.History(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, failErr("not_found", "there is no entry %d", id)
	}
	if err != nil {
		return nil, failErr("database", "%v", err)
	}
	type jrevision struct {
		jvalues
		MadeAt string  `json:"made_at"`
		Note   *string `json:"note"`
		Void   bool    `json:"void"`
	}
	out := struct {
		ID        int64       `json:"id"`
		Revisions []jrevision `json:"revisions"`
	}{ID: id, Revisions: []jrevision{}}
	for _, r := range revs {
		out.Revisions = append(out.Revisions, jrevision{valuesJSON(r.Values), r.MadeAt.Format("2006-01-02T15:04:05Z07:00"), nullable(r.Note), r.Void})
	}
	return out, nil
}

type joverlap struct {
	Seq        int      `json:"seq"`
	Member     string   `json:"member"`
	Duplicates []string `json:"duplicates"`
	Against    int      `json:"against"`
}

func overlapJSON(o model.Overlap) joverlap {
	dups := []string{}
	for _, d := range o.Duplicates {
		dups = append(dups, string(d))
	}
	return joverlap{o.Seq, o.Member, dups, o.Against}
}

type jrule struct {
	Types []string `json:"types"`
	Tax   []string `json:"tax"`
}

type jmember struct {
	Seq     int     `json:"seq"`
	Member  string  `json:"member"`
	Account *string `json:"account"`
	Asset   *string `json:"asset"`
	Rule    *jrule  `json:"rule"`
}

type jasset struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Kind    string                `json:"kind"`
	Members []jmember             `json:"members"`
	Invalid []joverlap            `json:"invalid"`
	Error   *string               `json:"error"`
	Default []jinterval           `json:"default"`
	Params  map[string][]jsegment `json:"params"`
}

type jsegment struct {
	From  jdate   `json:"from"`
	End   *jdate  `json:"end"`
	Value float64 `json:"value"`
}

func scheduleJSONOut(s model.Schedule) []jsegment {
	out := []jsegment{}
	for _, seg := range s.Segments() {
		out = append(out, jsegment{jd(seg.From), jdp(seg.End), seg.Value})
	}
	return out
}

// assetJSON is the JSON shape of an asset with the members that are left out as invalid, as asset list prints it and the write commands print the asset they changed. The default balance of an account's default asset is the account's.
func assetJSON(rec *model.Record, a *model.Asset) jasset {
	ja := jasset{ID: string(a.ID), Name: a.Name, Kind: assetKind(a.ID).String(), Members: []jmember{}, Invalid: []joverlap{}, Default: defaultJSON(a.Default), Params: map[string][]jsegment{}}
	if acct, ok := strings.CutPrefix(string(a.ID), string(model.DefaultAssetID(""))); ok && rec.Account(model.AccountID(acct)) != nil {
		ja.Default = defaultJSON(rec.Account(model.AccountID(acct)).Default)
	}
	for name, s := range a.Params {
		ja.Params[name] = scheduleJSONOut(s)
	}
	for _, m := range a.Members {
		jm := jmember{Seq: m.Seq, Member: m.String(), Account: nullable(string(m.Account)), Asset: nullable(string(m.Asset))}
		if m.Rule != nil {
			jm.Rule = &jrule{Types: []string{}, Tax: []string{}}
			for _, t := range m.Rule.Types {
				jm.Rule.Types = append(jm.Rule.Types, string(t))
			}
			for _, t := range m.Rule.Tax {
				jm.Rule.Tax = append(jm.Rule.Tax, string(t))
			}
		}
		ja.Members = append(ja.Members, jm)
	}
	res, err := rec.Resolve(a.ID)
	if err != nil {
		ja.Error = model.Ptr(err.Error())
	} else {
		for _, o := range res.Overlaps {
			if o.Asset == a.ID {
				ja.Invalid = append(ja.Invalid, overlapJSON(o))
			}
		}
	}
	return ja
}

func cmdAssetList(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	out := []jasset{}
	for _, a := range rec.Assets {
		out = append(out, assetJSON(rec, a))
	}
	return out, nil
}

func cmdAssetParams(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	id, err := c.assetArg(0)
	if err != nil {
		return nil, err
	}
	growth, err := assetGrowth(rec, id)
	if err != nil {
		return nil, failErr("invalid", "%v", err)
	}
	type jlayer struct {
		Source   string     `json:"source"`
		Asset    *string    `json:"asset"`
		Schedule []jsegment `json:"schedule"`
		Today    *float64   `json:"today"`
		Applies  bool       `json:"applies"`
	}
	type jaccount struct {
		Account   string     `json:"account"`
		Name      string     `json:"name"`
		Layers    []jlayer   `json:"layers"`
		Effective []jsegment `json:"effective"`
		Today     *float64   `json:"today"`
	}
	ja := assetJSON(rec, rec.Asset(id))
	out := struct {
		Asset    string      `json:"asset"`
		Default  []jinterval `json:"default"`
		Growth   []jsegment  `json:"growth"`
		Today    jdate       `json:"today"`
		Accounts []jaccount  `json:"accounts"`
	}{string(id), ja.Default, ja.Params[model.PropGrowth], jd(rec.Today), []jaccount{}}
	opt := func(v float64, ok bool) *float64 {
		if !ok {
			return nil
		}
		return &v
	}
	for _, g := range growth {
		acct := jaccount{string(g.Account), g.Name, []jlayer{}, scheduleJSONOut(g.Effective), opt(g.Today, g.HasToday)}
		for _, l := range g.Layers {
			source := "stated_rate"
			if l.Asset != "" {
				source = "asset"
			}
			acct.Layers = append(acct.Layers, jlayer{source, nullable(string(l.Asset)), scheduleJSONOut(l.Schedule), opt(l.Today, l.HasToday), l.Applies})
		}
		out.Accounts = append(out.Accounts, acct)
	}
	return out, nil
}

func cmdBudgetList(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	out := []jbudgetItem{}
	for _, it := range rec.Budget.Items {
		out = append(out, budgetItemJSON(it, rec.Today))
	}
	return out, nil
}

func cmdBudgetTotals(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	income, expenses := rec.Budget.MonthlyTotals(rec.Today)
	cents := func(v float64) float64 { return math.Round(v*100) / 100 }
	return struct {
		AsOf            jdate   `json:"as_of"`
		MonthlyIncome   float64 `json:"monthly_income"`
		MonthlyExpenses float64 `json:"monthly_expenses"`
		MonthlyNet      float64 `json:"monthly_net"`
	}{jd(rec.Today), cents(income), cents(expenses), cents(income - expenses)}, nil
}

func cmdProjectionList(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	type jprojection struct {
		Name              string  `json:"name"`
		Parent            *string `json:"parent"`
		Start             *jdate  `json:"start"`
		OverrideHistory   *bool   `json:"override_history"`
		ModificationCount int     `json:"modification_count"`
	}
	out := []jprojection{}
	for _, p := range rec.Projections {
		out = append(out, jprojection{p.Name, nullable(p.Parent), jdp(p.Start), p.OverrideHistory, len(p.Mods)})
	}
	return out, nil
}

func cmdProjectionShow(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	p := rec.Projection(c.args[0])
	if p == nil {
		return nil, failErr("not_found", "there is no projection named %q", c.args[0])
	}
	return projectionJSON(rec, p), nil
}

// projectionJSON is the JSON shape of a projection and its modifications, as projection show prints it and the write commands print the projection they changed. rec is the record that holds p; it says which modifications have a target that no longer exists.
func projectionJSON(rec *model.Record, p *model.Projection) any {
	type jsetting struct {
		Asset           string `json:"asset"`
		Start           *jdate `json:"start"`
		OverrideHistory *bool  `json:"override_history"`
	}
	type jtarget struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	type jaccount struct {
		ID      string  `json:"id"`
		Name    string  `json:"name"`
		Type    string  `json:"type"`
		Tax     string  `json:"tax"`
		Rate    float64 `json:"stated_rate"`
		Opening float64 `json:"opening"`
		From    jdate   `json:"from"`
	}
	type jmod struct {
		Index    int          `json:"index"`
		Target   jtarget      `json:"target"`
		Property string       `json:"property"`
		Mode     string       `json:"mode"`
		Schedule []jsegment   `json:"schedule"`
		Item     *jbudgetItem `json:"item"`
		Grant    *jfocalGrant `json:"grant"`
		Missing  bool         `json:"missing"`
		// MissingTarget names the target that does not exist; omitted when the modification's target resolves.
		MissingTarget string `json:"missing_target,omitempty"`
	}
	missing, _ := rec.MissingTargets(p.Name)
	out := struct {
		Name            string     `json:"name"`
		Parent          *string    `json:"parent"`
		Start           *jdate     `json:"start"`
		OverrideHistory *bool      `json:"override_history"`
		RetireOn        *jdate     `json:"retire_on"`
		WithdrawalOrder []string   `json:"withdrawal_order"`
		RothBracket     *float64   `json:"roth_bracket"`
		AssetSettings   []jsetting `json:"asset_settings"`
		Accounts        []jaccount `json:"accounts"`
		Modifications   []jmod     `json:"modifications"`
		Shocks          []jshock   `json:"shocks"`
		Rebalances      []jrebal   `json:"rebalances"`
	}{p.Name, nullable(p.Parent), jdp(p.Start), p.OverrideHistory, jdp(p.RetireOn), nil, p.RothBracket, []jsetting{}, []jaccount{}, []jmod{}, []jshock{}, []jrebal{}}
	for _, b := range p.WithdrawalOrder {
		out.WithdrawalOrder = append(out.WithdrawalOrder, string(b))
	}
	for _, a := range p.Accounts {
		out.Accounts = append(out.Accounts, jaccount{string(a.ID), a.Name, string(a.Type), string(a.Tax), a.StatedRate, a.Opening, jd(a.From)})
	}
	for _, s := range p.AssetSettings {
		out.AssetSettings = append(out.AssetSettings, jsetting{string(s.Asset), jdp(s.Start), s.OverrideHistory})
	}
	for i, m := range p.Mods {
		jm := jmod{Index: i, Target: jtarget{string(m.Target.Kind), m.Target.ID}, Property: m.Property, Mode: string(cmp.Or(m.Mode, model.ModeSet)), Schedule: scheduleJSONOut(m.Values)}
		if m.Item != nil {
			jm.Item = model.Ptr(budgetItemJSON(*m.Item, rec.Today))
		}
		if m.Grant != nil {
			jm.Grant = model.Ptr(focalGrantJSON(*m.Grant))
		}
		if i < len(missing) && missing[i] != "" {
			jm.Missing, jm.MissingTarget = true, missing[i]
		}
		out.Modifications = append(out.Modifications, jm)
	}
	for i, sh := range p.Shocks {
		out.Shocks = append(out.Shocks, jshock{i, sh.Class, jd(sh.Date), percentOf(sh.Change)})
	}
	for i, r := range p.Rebalances {
		jr := jrebal{i, string(r.Account), jd(r.Date), []jholding{}}
		for _, w := range r.Allocation {
			jr.Allocation = append(jr.Allocation, jholding{w.Class, percentOf(w.Share)})
		}
		out.Rebalances = append(out.Rebalances, jr)
	}
	return out
}

type jdiagnostics struct {
	Skipped  []jskipped   `json:"skipped"`
	Overlaps []joverlapOf `json:"overlaps"`
	Notes    []string     `json:"notes"`
}

type jskipped struct {
	Projection string `json:"projection"`
	Index      int    `json:"index"`
	Target     struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"target"`
	Property string `json:"property"`
	Reason   string `json:"reason"`
}

type joverlapOf struct {
	Asset string `json:"asset"`
	joverlap
}

func diagnosticsJSON(d engine.Diagnostics) jdiagnostics {
	out := jdiagnostics{Skipped: []jskipped{}, Overlaps: []joverlapOf{}, Notes: []string{}}
	for _, s := range d.Skipped {
		js := jskipped{Projection: s.Projection, Index: s.Index, Property: s.Property, Reason: s.Reason}
		js.Target.Kind, js.Target.ID = string(s.Target.Kind), s.Target.ID
		out.Skipped = append(out.Skipped, js)
	}
	for _, o := range d.Overlaps {
		out.Overlaps = append(out.Overlaps, joverlapOf{string(o.Asset), overlapJSON(o)})
	}
	out.Notes = append(out.Notes, d.Notes...)
	return out
}

// period reads the target, projection and --from/--to flags shared by balance and breakdown.
func (c *call) period() (rec *model.Record, target model.AssetID, projection string, from, to model.Date, err error) {
	from, _, err = c.date("from")
	if err != nil {
		return
	}
	if to, _, err = c.date("to"); err != nil {
		return
	}
	if to < from {
		err = usageErr("--to %v is before --from %v", to, from)
		return
	}
	if _, rec, err = c.open(); err != nil {
		return
	}
	if projection, err = c.projection(rec); err != nil {
		return
	}
	var view *model.Record
	if view, err = rec.For(projection); err != nil {
		err = failErr("invalid", "%v", err)
		return
	}
	var cand candidate
	if cand, err = resolveTarget(view, c.args[0], false); err != nil {
		return
	}
	target = cand.asset
	return
}

func cmdBalance(c *call) (any, error) {
	rec, target, projection, from, to, err := c.period()
	if err != nil {
		return nil, err
	}
	step := c.str("step")
	res, err := engine.Balances(rec, projection, target, sampleDates(from, to, step))
	if err != nil {
		return nil, failErr("engine", "%v", err)
	}
	type jsample struct {
		Date   jdate   `json:"date"`
		Value  float64 `json:"value"`
		Status string  `json:"status"`
	}
	out := struct {
		Target     string       `json:"target"`
		Projection *string      `json:"projection"`
		From       jdate        `json:"from"`
		To         jdate        `json:"to"`
		Step       string       `json:"step"`
		Samples    []jsample    `json:"samples"`
		Diag       jdiagnostics `json:"diagnostics"`
	}{string(target), nullable(projection), jd(from), jd(to), step, []jsample{}, diagnosticsJSON(res.Diagnostics)}
	for _, s := range res.Samples {
		out.Samples = append(out.Samples, jsample{jd(s.Date), s.Amount, strings.ReplaceAll(s.Status.String(), " ", "_")})
	}
	return out, nil
}

func cmdMonteCarlo(c *call) (any, error) {
	rec, target, projection, from, to, err := c.period()
	if err != nil {
		return nil, err
	}
	runs, seed, err := c.runsAndSeed()
	if err != nil {
		return nil, err
	}
	successAge, err := c.successAge()
	if err != nil {
		return nil, err
	}
	var pcts []float64
	for _, f := range strings.Split(c.str("percentiles"), ",") {
		p, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil || math.IsNaN(p) || p < 0 || p > 100 {
			return nil, usageErr("--percentiles: %q is not a number from 0 to 100", f)
		}
		pcts = append(pcts, p)
	}
	step := c.str("step")
	dates := sampleDates(from, to, step)
	res, err := engine.MonteCarlo(rec, projection, target, dates, engine.MonteCarloOptions{Runs: runs, Seed: seed, Percentiles: pcts})
	if err != nil {
		return nil, failErr("engine", "%v", err)
	}
	type jband struct {
		Percentile float64   `json:"percentile"`
		Values     []float64 `json:"values"`
	}
	out := struct {
		Target      string    `json:"target"`
		Projection  *string   `json:"projection"`
		From        jdate     `json:"from"`
		To          jdate     `json:"to"`
		Step        string    `json:"step"`
		Runs        int       `json:"runs"`
		Seed        uint64    `json:"seed"`
		Percentiles []float64 `json:"percentiles"`
		Dates       []jdate   `json:"dates"`
		Bands       []jband   `json:"bands"`
		SuccessAge  int       `json:"success_age"`
		Through     *jdate    `json:"success_through"`
		Success     *float64  `json:"success_percent"`
	}{string(target), nullable(projection), jd(from), jd(to), step, res.Runs, res.Seed, res.Percentiles, []jdate{}, []jband{}, successAge, nil, nil}
	if end, ok := engine.AgeDate(rec, successAge); ok {
		through := min(to, end)
		out.Through = jdp(&through)
		if pct, err := engine.Success(res, rec.Today, through); err == nil {
			out.Success = &pct
		}
	}
	for _, d := range dates {
		out.Dates = append(out.Dates, jd(d))
	}
	for i, p := range res.Percentiles {
		out.Bands = append(out.Bands, jband{p, res.Bands[i]})
	}
	return out, nil
}

// runsAndSeed reads and checks the --runs and --seed flags.
func (c *call) runsAndSeed() (runs int, seed uint64, err error) {
	runs, err = strconv.Atoi(c.str("runs"))
	if err != nil || runs < 1 {
		return 0, 0, usageErr("--runs: %q is not a whole number of at least 1", c.str("runs"))
	}
	seed, err = strconv.ParseUint(c.str("seed"), 10, 63)
	if err != nil {
		return 0, 0, usageErr("--seed: %q is not a whole number of 0 or more", c.str("seed"))
	}
	return runs, seed, nil
}

// successAge reads and checks the --success-age flag.
func (c *call) successAge() (int, error) {
	age, err := strconv.Atoi(c.str("success-age"))
	if err != nil || age < 1 {
		return 0, usageErr("--success-age: %q is not a whole number of at least 1", c.str("success-age"))
	}
	return age, nil
}

// sampleDates returns from, then each later step up to and including to. Month and year steps are computed from from each time, so that a clamped day does not carry into later months.
func sampleDates(from, to model.Date, step string) []model.Date {
	var out []model.Date
	for k := 0; ; k++ {
		var d model.Date
		switch step {
		case "day":
			d = from.Add(k)
		case "week":
			d = from.Add(7 * k)
		case "month":
			d = addMonths(from, k)
		default:
			d = addMonths(from, 12*k)
		}
		if d > to {
			return out
		}
		out = append(out, d)
	}
}

// addMonths returns the date n months after d on the same day of the month, or the month's last day if it is shorter.
func addMonths(d model.Date, n int) model.Date {
	y, m, day := d.YMD()
	months := y*12 + int(m) - 1 + n
	y, mm := months/12, time.Month(months%12+1)
	_, _, last := model.D(y, mm+1, 0).YMD()
	return model.D(y, mm, min(day, last))
}

func cmdBreakdown(c *call) (any, error) {
	rec, target, projection, from, to, err := c.period()
	if err != nil {
		return nil, err
	}
	res, err := engine.Breakdown(rec, projection, target, from, to)
	if err != nil {
		return nil, failErr("engine", "%v", err)
	}
	type jline struct {
		Cause     string  `json:"cause"`
		Class     *string `json:"class"`
		Actual    float64 `json:"actual"`
		Projected float64 `json:"projected"`
	}
	out := struct {
		Target     string       `json:"target"`
		Projection *string      `json:"projection"`
		From       jdate        `json:"from"`
		To         jdate        `json:"to"`
		Start      float64      `json:"start"`
		End        float64      `json:"end"`
		Total      float64      `json:"total"`
		Lines      []jline      `json:"lines"`
		Diag       jdiagnostics `json:"diagnostics"`
	}{string(target), nullable(projection), jd(from), jd(to), res.Start, res.End, res.Total(), []jline{}, diagnosticsJSON(res.Diagnostics)}
	for _, l := range res.Lines {
		out.Lines = append(out.Lines, jline{string(l.Cause), nullable(l.Class), l.Actual, l.Projected})
	}
	return out, nil
}

func cmdStarts(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	projection, err := c.projection(rec)
	if err != nil {
		return nil, err
	}
	view, err := rec.For(projection)
	if err != nil {
		return nil, failErr("invalid", "%v", err)
	}
	var targets []model.AssetID
	if len(c.args) > 0 {
		cand, err := resolveTarget(view, c.args[0], false)
		if err != nil {
			return nil, err
		}
		targets = append(targets, cand.asset)
	} else {
		for _, a := range view.Accounts {
			targets = append(targets, model.DefaultAssetID(a.ID))
		}
	}
	type jstart struct {
		Account         string `json:"account"`
		Start           jdate  `json:"start"`
		LastActual      *jdate `json:"last_actual"`
		HasActual       bool   `json:"has_actual"`
		OverrideHistory bool   `json:"override_history"`
		ProjectionStart jdate  `json:"projection_start"`
	}
	out := []jstart{}
	for _, t := range targets {
		starts, err := engine.Starts(rec, projection, t)
		if err != nil {
			return nil, failErr("engine", "%v", err)
		}
		for _, s := range starts {
			js := jstart{Account: string(s.Account), Start: jd(s.Start), HasActual: s.HasActual, OverrideHistory: s.OverrideHistory, ProjectionStart: jd(s.ProjectionStart)}
			if s.HasActual {
				js.LastActual = model.Ptr(jd(s.LastActual))
			}
			out = append(out, js)
		}
	}
	return out, nil
}

// jfocalGrant is the JSON shape of the focal grant of an add_grant modification; rates are the withholding rates in effect.
type jfocalGrant struct {
	Symbol          string    `json:"symbol"`
	Value           float64   `json:"value"`
	Date            jdate     `json:"date"`
	Yearly          bool      `json:"yearly"`
	Last            *jdate    `json:"last"`
	Calendar        string    `json:"calendar"`
	YearPercents    []float64 `json:"year_percents"`
	CliffMonths     int       `json:"cliff_months"`
	FirstVestMonths int       `json:"first_vest_months"`
	Federal         float64   `json:"federal"`
	State           float64   `json:"state"`
	FICA            float64   `json:"fica"`
	Deposit         string    `json:"deposit"`
}

func focalGrantJSON(g model.FocalGrant) jfocalGrant {
	r := g.WithholdingRates()
	return jfocalGrant{g.Symbol, g.Value, jd(g.Date), g.Yearly, jdp(g.Last), g.Calendar, g.YearPercents, g.CliffMonths, g.FirstVestMonths, r.Federal, r.State, r.FICA, string(g.Brokerage)}
}
