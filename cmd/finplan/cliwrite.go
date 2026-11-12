// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// The write commands for projections and their items; cliwritedata.go has those for assets, accounts, entries and the base budget. Each makes its edit through internal/ops, so the CLI and the app agree on what an edit may do, and prints {dry_run, changed, projection, ...}: the projection as projection show prints it, after the edit. With --dry-run the edit is validated and applied to a copy and nothing is saved. An edit that would change nothing is not an error: it prints changed false.

const (
	writeOut     = "{dry_run, changed, projection: as projection show prints it, after the edit}"
	dryRunDesc   = "validate the edit and print what it would change, but save nothing; a database that needs upgrading is refused, not upgraded"
	jsonFileDesc = "read the JSON input from this file instead of stdin"
	scheduleNote = "a schedule is [{\"from\": \"YYYY-MM-DD\" or null for from the beginning, \"end\": \"YYYY-MM-DD\" or null, \"value\": number}, ...], sorted and not overlapping; a value holds from its from date until its end or the next segment"
	itemJSONNote = "the item is {id (optional), name, kind (income, expense or transfer), amount (a number above zero, or a schedule of them), recurrence: {kind (one-time, weekly, biweekly, semimonthly, monthly, quarterly, annual or custom), anchor (optional date an interval item counts from, else its start; the date of a one-time item, which start, if given, must equal), month (annual), day (monthly, semimonthly and annual), day2 (semimonthly), count and unit (custom: every count days, weeks, months or years)}, start (the first date, required), end (optional), indexed, income_type (income only: wages, the default, ordinary or tax-free), timing (lump: the amount lands on each occurrence's date; spread: it is spent evenly over the occurrence's period; when omitted, spread for an expense that recurs monthly or more often and lump otherwise), match (optional, wages income only: {percent: the percent of the employee's contributions to the account that the employer deposits, up_to: the percent of each paycheck's gross the match counts contributions up to, account: a 401k or TSP account}), growth (optional annual raise as a fraction: 0.03 for 3%, stepping the amount up on each anniversary of the start; a number or a schedule), account (income and expense), from and to (transfer)}; amount and growth as a schedule are [{from, end, value}, ...] like a modification's, e.g. a daycare of 1400 until 2029-07-31 and 600 after; budget list prints the same shape with amount_schedule and growth_schedule, which are accepted as the schedule form too; when both forms are given they must agree on the value in effect today (an empty growth_schedule is no growth), else give one, e.g. drop amount_schedule to set a single amount"
	modIndexNote = "the modification's index, as projection show numbers them from 0"
)

var itemKinds = []string{string(model.ItemIncome), string(model.ItemExpense), string(model.ItemTransfer)}

var incomeTypes = func() []string {
	var out []string
	for _, t := range model.IncomeTypes {
		out = append(out, string(t))
	}
	return out
}()

var itemTimings = func() []string {
	var out []string
	for _, t := range model.Timings {
		out = append(out, string(t))
	}
	return out
}()

var modModes = []string{string(model.ModeSet), string(model.ModeScale)}

// itemEvery are the --every names; annual is the same as annually.
var itemEvery = []string{string(model.OneTime), string(model.Weekly), string(model.Biweekly), string(model.Semimonthly), string(model.Monthly), string(model.Quarterly), "annually", string(model.Annual), string(model.Custom)}

var itemUnits = []string{string(model.UnitDays), string(model.UnitWeeks), string(model.UnitMonths), string(model.UnitYears)}

// itemFlags are the flags that describe a budget item; none is required, so that the item can come as JSON instead.
func itemFlags(forAdd bool) []flagDef {
	fs := []flagDef{
		{Name: "kind", Type: typeEnum, Choices: itemKinds, Description: "income, expense or transfer"},
		{Name: "name", Type: typeString, Description: "the item's name"},
		{Name: "amount", Type: typeNumber, Description: "the amount, above zero, per occurrence; a schedule of amounts comes as JSON input"},
		{Name: "every", Type: typeEnum, Choices: itemEvery, Description: "how often the item happens; custom needs --count and --unit, semimonthly needs --day2"},
		{Name: "start", Type: typeDate, Description: "the date of a one-time item, or the first date of a recurring one, which also sets the phase of weekly, biweekly, quarterly and custom items: " + dateNote},
		{Name: "on", Type: typeDate, Description: "the same as --start"},
		{Name: "day", Type: typeInt, Description: "day of the month of a monthly or semimonthly item; the day of --start when omitted for a monthly one"},
		{Name: "day2", Type: typeInt, Description: "second day of the month of a semimonthly item"},
		{Name: "count", Type: typeInt, Description: "N of a custom frequency, every N days, weeks, months or years"},
		{Name: "unit", Type: typeEnum, Choices: itemUnits, Description: "the unit of a custom frequency"},
		{Name: "end", Type: typeDate, Description: "last date the item happens: " + dateNote + "; none removes it"},
		{Name: "account", Type: typeString, Description: "account id of an income or expense item"},
		{Name: "from", Type: typeString, Description: "account id a transfer draws from"},
		{Name: "to", Type: typeString, Description: "account id a transfer pays into"},
		{Name: "indexed", Type: typeBool, Default: "false", Description: "the amount grows with inflation"},
		{Name: "income-type", Type: typeEnum, Choices: incomeTypes, Description: "how an income item is taxed: wages (the default; income tax and FICA), ordinary (other taxable income, no FICA) or tax-free"},
		{Name: "timing", Type: typeEnum, Choices: itemTimings, Description: "when the amount is spent in a period: lump, on each occurrence's date, or spread evenly over the period from one occurrence to the day before the next; spread is the default for an expense that recurs monthly or more often (a bill paid in one go is a lump), lump for everything else"},
		{Name: "match-percent", Type: typeNumber, Description: "employer match of a wages income item: the percent of the employee's contributions to --match-account the employer deposits, 100 for dollar for dollar (0 for none)"},
		{Name: "match-up-to", Type: typeNumber, Description: "employer match of a wages income item: the match counts contributions up to this percent of each paycheck's gross, 0 to 100"},
		{Name: "match-account", Type: typeString, Description: "employer match of a wages income item: the 401k or TSP account the match is deposited into and whose contributions it matches; none removes it"},
		{Name: "growth", Type: typeNumber, Description: "annual raise as a fraction, 0.03 for 3%: the amount steps up by it on each anniversary of the item's start (0 for none); a schedule of raises comes as JSON input"},
	}
	if forAdd {
		fs = append(fs, flagDef{Name: "id", Type: typeString, Description: "the item's id; made from the name when omitted"})
	}
	return fs
}

// projAccountFlags are the flags that describe a projection's own account. Adding needs a name, a type, a tax treatment and an opening date.
func projAccountFlags(forAdd bool) []flagDef {
	fs := []flagDef{
		{Name: "name", Type: typeString, Required: forAdd, Description: "the account's name"},
		{Name: "type", Type: typeEnum, Required: forAdd, Choices: accountTypes, Description: "account type; a debt is mortgage, loan or credit card"},
		{Name: "tax", Type: typeEnum, Required: forAdd, Choices: taxTreatments, Description: "tax treatment"},
		{Name: "rate", Type: typeNumber, Description: "stated rate, the account's growth: a fraction per year between -1 and 1 (appreciation for property, interest for a debt); 0 when omitted"},
		{Name: "opening", Type: typeNumber, Description: "opening balance on the opening date, negative for a debt; 0 when omitted"},
		{Name: "from", Type: typeDate, Required: forAdd, Description: "the opening date: the account has no value before it. " + dateNote},
	}
	if forAdd {
		fs = append(fs, flagDef{Name: "id", Type: typeString, Description: "the account's id; made from the name when omitted. It must differ from every account and asset id and from the accounts of the projection's relatives in its chain"})
	}
	return fs
}

var writeCommands = []*command{
	{
		Name: "projection new", Summary: "add an empty projection",
		Args:   []argDef{{Name: "name", Description: "name of the new projection", Required: true}},
		Flags:  []flagDef{{Name: "parent", Type: typeString, Description: "projection it builds on; none, so a top-level projection, when omitted"}},
		Output: writeOut,
		run:    cmdProjectionNew,
	},
	{
		Name: "projection clone", Summary: "add a copy of a projection",
		Args:   []argDef{{Name: "name", Description: "projection to copy", Required: true}},
		Flags:  []flagDef{{Name: "as", Type: typeString, Required: true, Description: "name of the copy"}},
		Output: writeOut,
		run:    cmdProjectionClone,
	},
	{
		Name: "projection rename", Summary: "rename a projection, pointing the projections that build on it and the saved view presets at the new name",
		Args:   []argDef{{Name: "name", Description: "projection to rename", Required: true}, {Name: "new-name", Description: "its new name", Required: true}},
		Output: "{dry_run, changed, projection, renamed: {from, to} or null, rewritten_presets: [name], warnings: [text]}; warnings are presets that could not be rewritten, the rename is saved regardless",
		run:    cmdProjectionRename,
	},
	{
		Name: "projection set-parent", Summary: "make a projection build on another, or on nothing",
		Args:   []argDef{{Name: "name", Description: "projection to change", Required: true}, {Name: "parent", Description: "projection to build on; top level when omitted"}},
		Output: writeOut,
		run:    cmdProjectionSetParent,
	},
	{
		Name: "projection delete", Summary: "delete a projection that no other builds on",
		Args:   []argDef{{Name: "name", Description: "projection to delete", Required: true}},
		Output: "{dry_run, changed, removed: name, projection: the projection as it was}",
		run:    cmdProjectionDelete,
	},
	{
		Name: "projection set-start", Summary: "set a projection's start date",
		Args: []argDef{
			{Name: "name", Description: "projection to change", Required: true},
			{Name: "start", Description: dateNote + "; none to inherit from the projection it builds on", Required: true},
		},
		Output: writeOut,
		run:    cmdProjectionSetStart,
	},
	{
		Name: "projection set-override-history", Summary: "set whether a projection's start overrides recorded data",
		Args: []argDef{
			{Name: "name", Description: "projection to change", Required: true},
			{Name: "value", Description: "true, false, or none to inherit", Required: true},
		},
		Output: writeOut,
		run:    cmdProjectionSetOverrideHistory,
	},
	{
		Name: "projection set-retirement", Summary: "set the date a projection's household retires: wages and the saving into its accounts stop, and its expenses are then covered from its accounts in the withdrawal order",
		Args: []argDef{
			{Name: "name", Description: "projection to change", Required: true},
			{Name: "date", Description: dateNote + "; none to inherit from the projection it builds on", Required: true},
		},
		Output: writeOut,
		run:    cmdProjectionSetRetirement,
	},
	{
		Name: "projection set-withdrawal-order", Summary: "set the order a retired household draws on its accounts",
		Args: []argDef{
			{Name: "name", Description: "projection to change", Required: true},
			{Name: "order", Description: "taxable,tax-deferred,roth in the order wanted, each once, comma-separated; none to inherit (taxable, then tax-deferred, then roth)", Required: true},
		},
		Output: writeOut,
		run:    cmdProjectionSetWithdrawalOrder,
	},
	{
		Name: "projection set-roth-bracket", Summary: "set the federal bracket each year's Roth conversion fills to: tax-deferred money is moved into a Roth IRA up to the top of the bracket, after the year's other income, and taxed as income",
		Args: []argDef{
			{Name: "name", Description: "projection to change", Required: true},
			{Name: "rate", Description: "a bracket rate of the tax tables, 22 or 0.22 for 22%; none to inherit", Required: true},
		},
		Output: writeOut,
		run:    cmdProjectionSetRothBracket,
	},
	{
		Name: "projection set-inflation", Summary: "set a projection's inflation rate to a constant, replacing its first global set inflation modification; scale ones are kept and still apply",
		Args: []argDef{
			{Name: "name", Description: "projection to change", Required: true},
			{Name: "rate", Description: "a fraction per year, 0.03 for 3% (put -- before a negative one); none removes the modification", Required: true},
		},
		Output: writeOut,
		run:    cmdProjectionSetInflation,
	},
	{
		Name: "projection add-account", Summary: "add a hypothetical account to a projection; it exists only under that projection and the ones building on it, is never written to the record, and is picked up by rule-based aggregates such as Net worth. A mortgage is a debt account with a negative opening balance and the interest as its rate, paid down by a monthly transfer item. Its name must not be an asset's name",
		Args:   []argDef{{Name: "projection", Description: "projection to add the account to", Required: true}},
		Flags:  projAccountFlags(true),
		Output: writeOut,
		run:    cmdProjectionAddAccount,
	},
	{
		Name: "projection edit-account", Summary: "change an account a projection defines; flags given override its current values; a new name must not be an asset's name",
		Args: []argDef{
			{Name: "projection", Description: "projection that defines the account", Required: true},
			{Name: "account", Description: "account id or name", Required: true},
		},
		Flags:  projAccountFlags(false),
		Output: writeOut,
		run:    cmdProjectionEditAccount,
	},
	{
		Name: "projection remove-account", Summary: "remove an account a projection defines; refused while a modification or item of the projection or one building on it uses it",
		Args: []argDef{
			{Name: "projection", Description: "projection that defines the account", Required: true},
			{Name: "account", Description: "account id or name", Required: true},
		},
		Output: writeOut,
		run:    cmdProjectionRemoveAccount,
	},
	{
		Name: "mod add", Summary: "add a modification of an item's amount or growth, an account's or asset's growth or default balance, a stock's or market class's growth, inflation, or a focal grant to an equity account, to a projection; its target must exist, for an item in the record or added earlier in the chain",
		Args: []argDef{{Name: "projection", Description: "projection to change", Required: true}},
		Flags: append([]flagDef{
			{Name: "target", Type: typeString, Required: true, Description: "what to change: item:<id or name>, account:<id or name>, asset:<id or name>, stock:<symbol>, class:<id or name> or global"},
			{Name: "property", Type: typeEnum, Required: true, Choices: []string{model.PropAmount, model.PropGrowth, model.PropDefault, model.PropInflation, model.PropAddGrant}, Description: "amount or growth (an annual raise) for an item, growth or default (the balance on dates with no actual data) for an account or asset, growth (the stock's annual price change, projected from its latest price) for a stock, growth (the class's annual return) for a market class, inflation for global, add_grant for an equity account"},
			{Name: "mode", Type: typeEnum, Choices: modModes, Default: string(model.ModeSet), Description: "set replaces the value beneath; scale multiplies it, so the result follows what lies beneath"},
			{Name: "value", Type: typeNumber, Description: "a constant value (a rate is a fraction: 0.04 for 4%; with --mode scale a multiplier: 0.7 cuts by 30%); without it the schedule is read as JSON. For add_grant the grant's dollar value"},
			{Name: "from", Type: typeDate, Description: "with --value, the date the value starts: " + dateNote + "; always when omitted. For add_grant the grant date, the first if --yearly"},
			{Name: "end", Type: typeDate, Description: "with --value and --from, the last date the value holds: " + dateNote + "; open-ended when omitted. For add_grant with --yearly, the last grant date"},
			{Name: "at", Type: typeInt, Description: "index to insert at; at the end when omitted"},
			{Name: "symbol", Type: typeString, Description: "add_grant: the stock's symbol"},
			{Name: "yearly", Type: typeBool, Default: "false", Description: "add_grant: repeat the grant every year on the month and day of --from"},
			{Name: "calendar", Type: typeString, Description: "add_grant: name of the vest calendar"},
			{Name: "year-percents", Type: typeString, Description: "add_grant: the percent vesting in each year from the first vest, as " + percentsNote},
			{Name: "cliff-months", Type: typeInt, Description: "add_grant: months of the cliff; 0 when omitted"},
			{Name: "first-vest-months", Type: typeInt, Description: "add_grant: the first vest is the first calendar date at least this many months after the grant date; 0 when omitted"},
			{Name: "deposit", Type: typeString, Description: "add_grant: account that net proceeds are deposited in: account id or name"},
		}, rateFlags()...),
		Input:  "without --value, a schedule: " + scheduleNote,
		Output: writeOut,
		run:    cmdModAdd,
	},
	{
		Name: "mod edit", Summary: "replace the values of a modification",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "index", Description: modIndexNote, Required: true},
		},
		Flags: []flagDef{
			{Name: "mode", Type: typeEnum, Choices: modModes, Description: "change how the modification combines with what lies beneath: set or scale; unchanged when omitted. Alone it keeps the values; with --value or --json-file it replaces them too"},
			{Name: "value", Type: typeNumber, Description: "a constant value (a multiplier for a scale modification); without it the schedule is read as JSON"},
			{Name: "from", Type: typeDate, Description: "with --value, the date the value starts: " + dateNote + "; always when omitted"},
			{Name: "end", Type: typeDate, Description: "with --value and --from, the last date the value holds: " + dateNote + "; open-ended when omitted"},
		},
		Input:  "without --value (and not --mode alone), a schedule: " + scheduleNote,
		Output: writeOut,
		run:    cmdModEdit,
	},
	{
		Name: "mod move", Summary: "move a modification so that it ends up at another index",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "from", Description: "index it is at", Required: true},
			{Name: "to", Description: "index it ends up at", Required: true},
		},
		Output: writeOut,
		run:    cmdModMove,
	},
	{
		Name: "stock growth", Summary: "set a stock's base growth, the annual rate its price is projected with from its latest recorded price when no projection says otherwise (0% when none is set), and its volatility, the annual standard deviation its price is drawn with in montecarlo (35% when none is set); projections change the growth with mod add --target stock:<symbol> --property growth. With only --volatility or --clear-volatility the growth is left alone",
		Args: []argDef{{Name: "symbol", Description: "the stock's symbol, as a grant or a price observation names it", Required: true}},
		Flags: []flagDef{
			{Name: "clear", Type: typeBool, Default: "false", Description: "remove the stock's growth schedule, so it projects flat"},
			{Name: "value", Type: typeNumber, Description: "a constant annual rate, a fraction: 0.08 for 8%; without it (and without --clear) the schedule is read as JSON"},
			{Name: "from", Type: typeDate, Description: "with --value, the date the rate starts: " + dateNote + "; always when omitted"},
			{Name: "end", Type: typeDate, Description: "with --value and --from, the last date the rate holds: " + dateNote + "; open-ended when omitted"},
			{Name: "volatility", Type: typeNumber, Description: "the stock's annual volatility, a fraction between 0 and 10: 0.35 for 35%"},
			{Name: "clear-volatility", Type: typeBool, Default: "false", Description: "remove the stock's volatility, so it has the default of 35%"},
		},
		Input:  "without --value, --clear, --volatility and --clear-volatility, a schedule: " + scheduleNote,
		Output: "{dry_run, changed, stock, growth: [{from, end, value}]}; with a volatility flag also volatility, the fraction in force",
		run:    cmdStockGrowth,
	},
	{
		Name: "mod remove", Summary: "remove a modification",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "index", Description: modIndexNote, Required: true},
		},
		Output: writeOut,
		run:    cmdModRemove,
	},
	{
		Name: "item add", Summary: "add a new budget item to a projection; the item exists only under that projection and the ones building on it",
		Args:   []argDef{{Name: "projection", Description: "projection to add the item to", Required: true}},
		Flags:  itemFlags(true),
		Input:  "instead of the flags, " + itemJSONNote,
		Output: writeOut,
		run:    cmdItemAdd,
	},
	{
		Name: "item edit", Summary: "change a budget item that a projection adds; flags given override the item's current values",
		Args: []argDef{
			{Name: "projection", Description: "projection that adds the item", Required: true},
			{Name: "item", Description: "item id or name", Required: true},
		},
		Flags:  itemFlags(false),
		Input:  "instead of the flags, the whole new item: " + itemJSONNote + "; its id, if given, must be the item's",
		Output: writeOut,
		run:    cmdItemEdit,
	},
	{
		Name: "item end", Summary: "make a budget item, the record's or one added in the chain, pay nothing from a date on",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "item", Description: "item id or name", Required: true},
			{Name: "date", Description: "first date it pays nothing: " + dateNote, Required: true},
		},
		Output: writeOut,
		run:    cmdItemEnd,
	},
}

func init() {
	writeCommands = append(writeCommands, assetSettingCommands...)
	writeCommands = append(writeCommands, dataCommands...)
	writeCommands = append(writeCommands, taxCommands...)
	for _, cmd := range writeCommands {
		cmd.Write = true
		cmd.Flags = append(cmd.Flags, flagDef{Name: "dry-run", Type: typeBool, Default: "false", Description: dryRunDesc})
		if cmd.Input != "" {
			cmd.Flags = append(cmd.Flags, flagDef{Name: "json-file", Type: typeString, Description: jsonFileDesc})
		}
	}
	commands = append(commands, writeCommands...)
}

func (c *call) options() ops.Options { return ops.Options{DryRun: c.str("dry-run") == "true"} }

// opsError turns the error of an operation into a CLI error.
func opsError(err error) error {
	var se *ops.SaveError
	switch {
	case errors.Is(err, ops.ErrUnknownProjection):
		return failErr("not_found", "%v", err)
	case errors.Is(err, ops.ErrUnknownTarget):
		return failErr("unknown_target", "%v", err)
	case errors.Is(err, ops.ErrNameTaken):
		return failErr("name_taken", "%v", err)
	case errors.Is(err, ops.ErrUnknownObject), errors.Is(err, store.ErrNotFound):
		return failErr("not_found", "%v", err)
	case errors.Is(err, ops.ErrInUse):
		return failErr("in_use", "%v", err)
	case errors.Is(err, store.ErrRecordChanged):
		return failErr("record_changed", "%v", err)
	case errors.Is(err, ops.ErrHasChildren):
		return failErr("has_children", "%v", err)
	case errors.Is(err, model.ErrCycle):
		return failErr("cycle", "%v", err)
	case errors.Is(err, ops.ErrInvalidValue), errors.Is(err, ops.ErrNoObject), errors.Is(err, ops.ErrEmptyName):
		return failErr("invalid", "%v", err)
	case errors.As(err, &se):
		return failErr("database", "%v", err)
	}
	return err
}

// finish turns the result of an operation on the projection into the command's output. An edit that changes nothing is a result with changed false. The caller may add fields to the map.
func (c *call) finish(name string, res *ops.Result, err error) (map[string]any, error) {
	out := map[string]any{"dry_run": c.options().DryRun, "changed": true}
	switch {
	case errors.Is(err, ops.ErrNoChange):
		out["changed"] = false
		out["projection"] = projectionJSON(c.rec, c.rec.Projection(name))
	case err != nil:
		return nil, opsError(err)
	case res.Projection != nil:
		out["projection"] = projectionJSON(res.Record, res.Projection)
	}
	return out, nil
}

// projectionArg opens the database and returns the projection the first argument names.
func (c *call) projectionArg() (*model.Projection, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	p := rec.Projection(c.args[0])
	if p == nil {
		return nil, failErr("not_found", "there is no projection named %q", c.args[0])
	}
	return p, nil
}

func cmdProjectionNew(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	res, err := ops.NewProjection(st, rec, c.args[0], c.str("parent"), c.options())
	return c.finish(strings.TrimSpace(c.args[0]), res, err)
}

func cmdProjectionClone(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	res, err := ops.CloneProjection(st, rec, c.args[0], c.str("as"), c.options())
	return c.finish(c.args[0], res, err)
}

func cmdProjectionRename(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	res, err := ops.RenameProjection(st, rec, c.args[0], c.args[1], c.options())
	out, err := c.finish(c.args[0], res, err)
	if err != nil {
		return nil, err
	}
	out["renamed"], out["rewritten_presets"], out["warnings"] = nil, []string{}, []string{}
	if res != nil {
		if res.Rename != nil {
			out["renamed"] = map[string]string{"from": res.Rename.From, "to": res.Rename.To}
		}
		if res.Rewritten != nil {
			out["rewritten_presets"] = res.Rewritten
		}
		for _, w := range res.Warnings {
			out["warnings"] = append(out["warnings"].([]string), w.Error())
		}
	}
	return out, nil
}

func cmdProjectionSetParent(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	parent := ""
	if len(c.args) > 1 {
		parent = c.args[1]
	}
	res, err := ops.SetParent(st, rec, c.args[0], parent, c.options())
	return c.finish(c.args[0], res, err)
}

func cmdProjectionDelete(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	res, err := ops.DeleteProjection(c.st, c.rec, p.Name, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "removed": res.Removed, "projection": projectionJSON(c.rec, p)}, nil
}

func cmdProjectionAddAccount(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	rate, _, err := c.number("rate")
	if err != nil {
		return nil, err
	}
	opening, _, err := c.number("opening")
	if err != nil {
		return nil, err
	}
	from, _, err := c.date("from")
	if err != nil {
		return nil, err
	}
	a := model.ProjAccount{
		ID: model.AccountID(c.str("id")), Name: c.str("name"), Type: model.AccountType(c.str("type")),
		Tax: model.TaxTreatment(c.str("tax")), StatedRate: rate, Opening: opening, From: from,
	}
	res, err := ops.AddProjAccount(c.st, c.rec, p.Name, a, c.options())
	return c.finish(p.Name, res, err)
}

// projAccountArg returns the projection the first argument names and the id of the account of its own that the second names, by id or else by name.
func (c *call) projAccountArg() (*model.Projection, model.AccountID, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, "", err
	}
	for _, byName := range []bool{false, true} {
		for _, a := range p.Accounts {
			if key := string(a.ID); byName && a.Name == c.args[1] || !byName && key == c.args[1] {
				return p, a.ID, nil
			}
		}
	}
	return nil, "", failErr("not_found", "projection %q does not define an account %q", p.Name, c.args[1])
}

func cmdProjectionEditAccount(c *call) (any, error) {
	p, id, err := c.projAccountArg()
	if err != nil {
		return nil, err
	}
	var ch ops.ProjAccountChange
	if c.set["name"] {
		ch.Name = model.Ptr(c.str("name"))
	}
	if c.set["type"] {
		ch.Type = model.Ptr(model.AccountType(c.str("type")))
	}
	if c.set["tax"] {
		ch.Tax = model.Ptr(model.TaxTreatment(c.str("tax")))
	}
	if v, ok, err := c.number("rate"); err != nil {
		return nil, err
	} else if ok {
		ch.StatedRate = &v
	}
	if v, ok, err := c.number("opening"); err != nil {
		return nil, err
	} else if ok {
		ch.Opening = &v
	}
	if d, ok, err := c.date("from"); err != nil {
		return nil, err
	} else if ok {
		ch.From = &d
	}
	if ch == (ops.ProjAccountChange{}) {
		return nil, usageErr("projection edit-account needs at least one of --name, --type, --tax, --rate, --opening and --from")
	}
	res, err := ops.EditProjAccount(c.st, c.rec, p.Name, id, ch, c.options())
	return c.finish(p.Name, res, err)
}

func cmdProjectionRemoveAccount(c *call) (any, error) {
	p, id, err := c.projAccountArg()
	if err != nil {
		return nil, err
	}
	res, err := ops.RemoveProjAccount(c.st, c.rec, p.Name, id, c.options())
	return c.finish(p.Name, res, err)
}

func cmdProjectionSetStart(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var start *model.Date
	if c.args[1] != "none" {
		d, err := parseCLIDate(c.args[1], false)
		if err != nil {
			return nil, usageErr("<start>: %v", err)
		}
		start = &d
	}
	res, err := ops.SetStart(st, rec, c.args[0], start, c.options())
	return c.finish(c.args[0], res, err)
}

func cmdProjectionSetOverrideHistory(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var v *bool
	switch c.args[1] {
	case "true", "false":
		v = model.Ptr(c.args[1] == "true")
	case "none":
	default:
		return nil, usageErr("<value>: %q is not true, false or none", c.args[1])
	}
	res, err := ops.SetOverrideHistory(st, rec, c.args[0], v, c.options())
	return c.finish(c.args[0], res, err)
}

func cmdProjectionSetRetirement(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var on *model.Date
	if c.args[1] != "none" {
		d, err := parseCLIDate(c.args[1], false)
		if err != nil {
			return nil, usageErr("<date>: %v", err)
		}
		on = &d
	}
	res, err := ops.SetRetirement(st, rec, c.args[0], on, c.options())
	return c.finish(c.args[0], res, err)
}

func cmdProjectionSetWithdrawalOrder(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var order []model.Bucket
	if c.args[1] != "none" {
		for _, name := range strings.Split(c.args[1], ",") {
			order = append(order, model.Bucket(strings.TrimSpace(name)))
		}
		if !model.ValidWithdrawalOrder(order) {
			return nil, usageErr("<order>: %q does not name each of taxable, tax-deferred and roth once", c.args[1])
		}
	}
	res, err := ops.SetWithdrawalOrder(st, rec, c.args[0], order, c.options())
	return c.finish(c.args[0], res, err)
}

func cmdProjectionSetRothBracket(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var rate *float64
	if c.args[1] != "none" {
		v, err := parseNumber(c.args[1])
		if err != nil {
			return nil, usageErr("<rate>: %v", err)
		}
		if v > 1 {
			v /= 100
		}
		rate = &v
	}
	res, err := ops.SetRothBracket(st, rec, c.args[0], rate, c.options())
	return c.finish(c.args[0], res, err)
}

func cmdProjectionSetInflation(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var rate *float64
	if c.args[1] != "none" {
		v, err := parseNumber(c.args[1])
		if err != nil {
			return nil, usageErr("<rate>: %v", err)
		}
		rate = &v
	}
	res, err := ops.SetInflation(st, rec, c.args[0], rate, c.options())
	return c.finish(c.args[0], res, err)
}

// ---- Modifications.

// index reads the positional argument at pos as an index into n things.
func (c *call) index(pos, n int) (int, error) {
	i, err := strconv.Atoi(c.args[pos])
	if err != nil {
		return 0, usageErr("<%s>: %q is not a whole number", c.cmd.Args[pos].Name, c.args[pos])
	}
	if i < 0 || i >= n {
		return 0, failErr("invalid", "<%s>: index %d is out of range: the projection has %d modifications", c.cmd.Args[pos].Name, i, n)
	}
	return i, nil
}

// readJSON reads the command's JSON input from --json-file, or from stdin when there is no file.
func (c *call) readJSON(v any) error {
	var data []byte
	var err error
	if path := c.str("json-file"); path != "" {
		data, err = os.ReadFile(path)
	} else {
		data, err = io.ReadAll(c.stdin)
	}
	if err != nil {
		return usageErr("read the JSON input: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return usageErr("expected JSON on stdin or in --json-file")
		}
		return usageErr("the JSON input: %v", err)
	}
	if dec.More() {
		return usageErr("the JSON input: unexpected data after the first value")
	}
	return nil
}

// dateField reads a date from JSON input.
func dateField(name, s string) (model.Date, error) {
	d, err := parseCLIDate(s, false)
	if err != nil {
		return 0, usageErr("the JSON input: %s: %v", name, err)
	}
	return d, nil
}

// schedule reads the schedule of a mod add or mod edit: the constant --value from --from, or JSON.
func (c *call) schedule() (model.Schedule, error) {
	v, ok, err := c.number("value")
	if err != nil {
		return model.Schedule{}, err
	}
	if !ok {
		if c.set["from"] || c.set["end"] {
			return model.Schedule{}, usageErr("--from and --end need --value")
		}
		return c.scheduleJSON()
	}
	if c.set["json-file"] {
		return model.Schedule{}, usageErr("--value and --json-file cannot be used together")
	}
	from, ok, err := c.date("from")
	if err != nil {
		return model.Schedule{}, err
	}
	end, endSet, err := c.date("end")
	if err != nil {
		return model.Schedule{}, err
	}
	if !ok {
		if endSet {
			return model.Schedule{}, usageErr("--end needs --from")
		}
		return model.Constant(v), nil
	}
	seg := model.Segment{From: from, Value: v}
	if endSet {
		seg.End = &end
	}
	sched, err := model.NewSchedule(seg)
	if err != nil {
		return model.Schedule{}, usageErr("%v", err)
	}
	return sched, nil
}

// segInput is one segment of a schedule as JSON input.
type segInput struct {
	From  *string  `json:"from"`
	End   *string  `json:"end"`
	Value *float64 `json:"value"`
}

// scheduleOf builds the schedule of the segments of JSON input; none is the empty schedule.
func scheduleOf(in []segInput) (model.Schedule, error) {
	// An empty schedule is the zero Schedule, which is what a stored item without growth holds; NewSchedule would give an empty non-nil slice that reflect.DeepEqual tells apart.
	if len(in) == 0 {
		return model.Schedule{}, nil
	}
	segs := make([]model.Segment, len(in))
	for i, s := range in {
		if s.Value == nil {
			return model.Schedule{}, usageErr("the JSON input: segment %d needs a value", i)
		}
		// A null from is how the list shows a segment that has no start.
		from := model.MinDate
		if s.From != nil {
			var err error
			if from, err = dateField("from", *s.From); err != nil {
				return model.Schedule{}, err
			}
		}
		segs[i] = model.Segment{From: from, Value: *s.Value}
		if s.End != nil {
			end, err := dateField("end", *s.End)
			if err != nil {
				return model.Schedule{}, err
			}
			segs[i].End = &end
		}
	}
	sched, err := model.NewSchedule(segs...)
	if err != nil {
		return model.Schedule{}, usageErr("the JSON input: %v", err)
	}
	return sched, nil
}

func (c *call) scheduleJSON() (model.Schedule, error) {
	var in []segInput
	if err := c.readJSON(&in); err != nil {
		return model.Schedule{}, err
	}
	sched, err := scheduleOf(in)
	if err != nil {
		return model.Schedule{}, err
	}
	if len(in) == 0 {
		return model.Schedule{}, usageErr("the JSON input: a schedule needs at least one segment")
	}
	return sched, nil
}

// itemsInChain returns the budget items the record has and every item the chain of projection p adds, even after the modification that would use them, so that a name can be looked up; the operation judges the order.
func itemsInChain(rec *model.Record, p *model.Projection) []model.BudgetItem {
	items := slices.Clone(rec.Budget.Items)
	chain, err := rec.Chain(p.Name)
	if err != nil {
		chain = []*model.Projection{p}
	}
	for _, q := range chain {
		for _, m := range q.Mods {
			if m.Property == model.PropAddItem && m.Item != nil {
				items = append(items, *m.Item)
			}
		}
	}
	return items
}

// pickItem finds the one item whose id, or failing that name, is s. An item not found gives s back as the id, which the operation refuses as an unknown target.
func pickItem(items []model.BudgetItem, s string) (string, error) {
	for _, byName := range []bool{false, true} {
		var ids []string
		for _, it := range items {
			key := it.ID
			if byName {
				key = it.Name
			}
			if key == s && !slices.Contains(ids, it.ID) {
				ids = append(ids, it.ID)
			}
		}
		switch len(ids) {
		case 0:
		case 1:
			return ids[0], nil
		default:
			return "", failErr("ambiguous", "%q matches more than one budget item: %s", s, strings.Join(ids, ", "))
		}
	}
	return s, nil
}

// modTarget reads --target for the projection, whose own and inherited accounts count as accounts.
func (c *call) modTarget(p *model.Projection) (model.ObjectRef, error) {
	kind, name, _ := strings.Cut(c.str("target"), ":")
	switch kind {
	case "global":
		if name != "" {
			return model.ObjectRef{}, usageErr("--target global takes no name")
		}
		return model.ObjectRef{Kind: model.KindGlobal}, nil
	case "stock":
		if name == "" {
			return model.ObjectRef{}, usageErr("--target stock needs a symbol: stock:<symbol>")
		}
		return model.ObjectRef{Kind: model.KindStock, ID: name}, nil
	case "class":
		if name == "" {
			return model.ObjectRef{}, usageErr("--target class needs a market class: class:<id or name>")
		}
		if _, _, err := c.open(); err != nil {
			return model.ObjectRef{}, err
		}
		id, err := classNamed(c.rec, name)
		return model.ObjectRef{Kind: model.KindClass, ID: id}, err
	case "item", "account", "asset":
		if name == "" {
			return model.ObjectRef{}, usageErr("--target %s needs a name: %s:<id or name>", kind, kind)
		}
	default:
		return model.ObjectRef{}, usageErr("--target %q: use item:<id or name>, account:<id or name>, asset:<id or name>, stock:<symbol>, class:<id or name> or global", c.str("target"))
	}
	view, err := c.rec.For(p.Name)
	if err != nil {
		return model.ObjectRef{}, failErr("invalid", "%v", err)
	}
	switch kind {
	case "item":
		id, err := pickItem(itemsInChain(c.rec, p), name)
		return model.ObjectRef{Kind: model.KindBudgetItem, ID: id}, err
	case "account":
		cand, err := resolveTarget(view, name, true)
		if err != nil {
			// Not found is the operation's to refuse as an unknown target; an ambiguous name is not.
			if ce := (*cliError)(nil); errors.As(err, &ce) && ce.Code == "ambiguous" {
				return model.ObjectRef{}, err
			}
			return model.ObjectRef{Kind: model.KindAccount, ID: name}, nil
		}
		return model.ObjectRef{Kind: model.KindAccount, ID: string(cand.account)}, nil
	}
	cand, err := resolveTarget(view, name, false)
	if err != nil {
		if ce := (*cliError)(nil); errors.As(err, &ce) && ce.Code == "ambiguous" {
			return model.ObjectRef{}, err
		}
		return model.ObjectRef{Kind: model.KindAsset, ID: name}, nil
	}
	if cand.account != "" {
		return model.ObjectRef{}, usageErr("--target asset:%s: that is an account; use account:%s", name, cand.account)
	}
	return model.ObjectRef{Kind: model.KindAsset, ID: string(cand.asset)}, nil
}

func cmdModAdd(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	target, err := c.modTarget(p)
	if err != nil {
		return nil, err
	}
	at, atSet, err := c.integer("at")
	if err != nil {
		return nil, err
	}
	if atSet && (at < 0 || at > len(p.Mods)) {
		return nil, failErr("invalid", "--at %d is out of range: the projection has %d modifications", at, len(p.Mods))
	}
	m := model.Modification{Target: target, Property: c.str("property"), Mode: model.Mode(c.str("mode"))}
	if m.Property == model.PropAddGrant {
		if m.Grant, err = c.focalGrant(); err != nil {
			return nil, err
		}
	} else if m.Values, err = c.schedule(); err != nil {
		return nil, err
	}
	var res *ops.Result
	if atSet {
		res, err = ops.InsertMod(c.st, c.rec, p.Name, at, m, c.options())
	} else {
		res, err = ops.AddMod(c.st, c.rec, p.Name, m, c.options())
	}
	return c.finish(p.Name, res, err)
}

// focalGrant reads the focal grant that mod add --property add_grant describes from its flags.
func (c *call) focalGrant() (*model.FocalGrant, error) {
	g := &model.FocalGrant{Symbol: c.str("symbol"), Calendar: c.str("calendar"), Yearly: c.str("yearly") == "true"}
	var ok bool
	var err error
	if g.Symbol == "" {
		return nil, usageErr("--property add_grant needs --symbol, the stock's symbol")
	}
	if g.Value, ok, err = c.number("value"); err != nil {
		return nil, err
	} else if !ok {
		return nil, usageErr("--property add_grant needs --value, the grant's dollar value")
	}
	if g.Date, ok, err = c.date("from"); err != nil {
		return nil, err
	} else if !ok {
		return nil, usageErr("--property add_grant needs --from, the grant date")
	}
	if end, ok, err := c.date("end"); err != nil {
		return nil, err
	} else if ok {
		g.Last = &end
	}
	if g.CliffMonths, _, err = c.integer("cliff-months"); err != nil {
		return nil, err
	}
	if g.FirstVestMonths, _, err = c.integer("first-vest-months"); err != nil {
		return nil, err
	}
	if !c.set["year-percents"] {
		return nil, usageErr("--property add_grant needs --year-percents")
	}
	if g.YearPercents, err = parsePercents("year-percents", c.str("year-percents")); err != nil {
		return nil, err
	}
	if rates, set, err := c.ratesFrom(model.DefaultWithholding); err != nil {
		return nil, err
	} else if set {
		g.Rates = &rates
	}
	if !c.set["deposit"] {
		return nil, usageErr("--property add_grant needs --deposit, the account that net proceeds are deposited in")
	}
	if g.Brokerage, err = c.accountNamed(c.str("deposit")); err != nil {
		return nil, err
	}
	return g, nil
}

func cmdStockGrowth(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	symbol := c.args[0]
	vol, volSet, err := c.number("volatility")
	if err != nil {
		return nil, err
	}
	clearVol := c.str("clear-volatility") == "true"
	if volSet && clearVol {
		return nil, usageErr("--volatility and --clear-volatility cannot be used together")
	}
	changesVol := volSet || clearVol
	if volSet {
		check := c.options()
		check.DryRun = true
		if _, err := ops.SetStockVolatility(st, rec, symbol, &vol, check); err != nil && !errors.Is(err, ops.ErrNoChange) {
			return nil, opsError(err)
		}
	}
	changesGrowth := !changesVol || c.set["value"] || c.set["from"] || c.set["end"] || c.set["json-file"] || c.str("clear") == "true"
	out := map[string]any{"dry_run": c.options().DryRun, "changed": false, "stock": symbol}
	if changesGrowth {
		var growth *model.Schedule
		if c.str("clear") == "true" {
			if c.set["value"] || c.set["from"] || c.set["end"] || c.set["json-file"] {
				return nil, usageErr("--clear cannot be combined with a schedule")
			}
		} else {
			s, err := c.schedule()
			if err != nil {
				return nil, err
			}
			growth = &s
		}
		res, err := ops.SetStockGrowth(st, rec, symbol, growth, c.options())
		switch {
		case errors.Is(err, ops.ErrNoChange):
		case err != nil:
			return nil, opsError(err)
		default:
			rec = res.Record
			out["changed"] = true
			out["growth"] = scheduleJSONOut(rec.StockGrowthOf(symbol))
		}
	}
	if changesVol {
		var v *float64
		if volSet {
			v = &vol
		}
		res, err := ops.SetStockVolatility(st, rec, symbol, v, c.options())
		switch {
		case errors.Is(err, ops.ErrNoChange):
		case err != nil:
			return nil, opsError(err)
		default:
			rec = res.Record
			out["changed"] = true
		}
		out["volatility"] = rec.StockVolatilityOf(symbol)
	}
	return out, nil
}

func cmdModEdit(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	i, err := c.index(1, len(p.Mods))
	if err != nil {
		return nil, err
	}
	if p.Mods[i].Property == model.PropAddItem {
		return nil, failErr("invalid", "modification %d adds an item: change it with item edit", i)
	}
	if p.Mods[i].Property == model.PropAddGrant {
		return nil, failErr("invalid", "modification %d adds a grant: remove it and add it again", i)
	}
	if c.set["mode"] && !c.set["value"] && !c.set["from"] && !c.set["end"] && !c.set["json-file"] {
		res, err := ops.SetModMode(c.st, c.rec, p.Name, i, model.Mode(c.str("mode")), nil, c.options())
		return c.finish(p.Name, res, err)
	}
	values, err := c.schedule()
	if err != nil {
		return nil, err
	}
	if !c.set["mode"] {
		res, err := ops.SetModValues(c.st, c.rec, p.Name, i, values, c.options())
		return c.finish(p.Name, res, err)
	}
	m := p.Mods[i]
	m.Mode, m.Values = model.Mode(c.str("mode")), values
	res, err := ops.SetMod(c.st, c.rec, p.Name, i, m, c.options())
	return c.finish(p.Name, res, err)
}

func cmdModMove(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	from, err := c.index(1, len(p.Mods))
	if err != nil {
		return nil, err
	}
	to, err := c.index(2, len(p.Mods))
	if err != nil {
		return nil, err
	}
	res, err := ops.MoveMod(c.st, c.rec, p.Name, from, to, c.options())
	return c.finish(p.Name, res, err)
}

func cmdModRemove(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	i, err := c.index(1, len(p.Mods))
	if err != nil {
		return nil, err
	}
	res, err := ops.RemoveMod(c.st, c.rec, p.Name, i, c.options())
	return c.finish(p.Name, res, err)
}

// ---- Budget items.

// itemInput is a budget item as JSON input; it has the shape budget list prints.
type itemInput struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Kind       string        `json:"kind"`
	Amount     numOrSegments `json:"amount"`
	Recurrence struct {
		Kind   string  `json:"kind"`
		Anchor *string `json:"anchor"`
		Month  int     `json:"month"`
		Day    int     `json:"day"`
		Day2   int     `json:"day2"`
		Count  int     `json:"count"`
		Unit   string  `json:"unit"`
	} `json:"recurrence"`
	Start   *string       `json:"start"`
	End     *string       `json:"end"`
	Indexed bool          `json:"indexed"`
	Growth  numOrSegments `json:"growth"`
	// IncomeType is for an income item; budget list prints it as wages when none is set.
	IncomeType string `json:"income_type"`
	// Timing is lump or spread; empty for the item's default.
	Timing string `json:"timing"`
	// Match is the employer match of a wages income item; absent or null for none.
	Match   *matchInput `json:"match"`
	Account string      `json:"account"`
	From    string      `json:"from"`
	To      string      `json:"to"`

	// A schedule, when present, is used; an Amount or Growth given with it must agree with it. An empty GrowthSchedule is no growth.
	AmountSchedule []segInput `json:"amount_schedule"`
	GrowthSchedule []segInput `json:"growth_schedule"`
}

// matchInput is an employer match as JSON input, the shape budget list prints.
type matchInput struct {
	Percent float64 `json:"percent"`
	UpTo    float64 `json:"up_to"`
	Account string  `json:"account"`
}

// numOrSegments is a number or a schedule's segments in JSON input; a number is a one-segment schedule.
type numOrSegments struct {
	num  *float64
	segs []segInput
}

func (n *numOrSegments) UnmarshalJSON(b []byte) error {
	var v float64
	if err := json.Unmarshal(b, &v); err == nil {
		n.num = &v
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(&n.segs)
}

// schedule returns the schedule the input gives. A growth of the number 0 is no growth.
func (n numOrSegments) schedule(growth bool) (model.Schedule, error) {
	switch {
	case n.num == nil:
		return scheduleOf(n.segs)
	case growth && *n.num == 0:
		return model.Schedule{}, nil
	}
	return model.Constant(*n.num), nil
}

// itemJSON reads the item from JSON; base is the item being edited, nil when adding.
func (c *call) itemJSON(base *model.BudgetItem) (model.BudgetItem, error) {
	var in itemInput
	if err := c.readJSON(&in); err != nil {
		return model.BudgetItem{}, err
	}
	r := in.Recurrence
	it := model.BudgetItem{
		ID: in.ID, Name: in.Name, Kind: model.ItemKind(in.Kind), Indexed: in.Indexed, IncomeType: model.IncomeType(in.IncomeType), Timing: model.Timing(in.Timing),
		Account: model.AccountID(in.Account), From: model.AccountID(in.From), To: model.AccountID(in.To),
		Recurrence: model.Recurrence{Kind: model.RecurrenceKind(r.Kind), Month: time.Month(r.Month), Day: r.Day, Day2: r.Day2, Count: r.Count, Unit: model.RecurrenceUnit(r.Unit)},
	}
	if in.Match != nil {
		it.MatchPercent, it.MatchUpTo, it.MatchAccount = in.Match.Percent, in.Match.UpTo, model.AccountID(in.Match.Account)
	}
	_, rec, err := c.open()
	if err != nil {
		return it, err
	}
	if in.AmountSchedule != nil {
		it.Amount, err = scheduleOf(in.AmountSchedule)
		if err == nil && in.Amount.num != nil && *in.Amount.num != valueListed(it.Amount, rec.Today) {
			err = usageErr("the JSON input: amount and amount_schedule differ; edit one form, e.g. drop amount_schedule to set a single amount")
		}
	} else {
		it.Amount, err = in.Amount.schedule(false)
	}
	if err != nil {
		return it, err
	}
	if in.GrowthSchedule != nil {
		it.Growth, err = scheduleOf(in.GrowthSchedule)
		if err == nil && in.Growth.num != nil && *in.Growth.num != valueListed(it.Growth, rec.Today) {
			err = usageErr("the JSON input: growth and growth_schedule differ; edit one form, e.g. drop growth_schedule to set a single growth")
		}
	} else {
		it.Growth, err = in.Growth.schedule(true)
	}
	if err != nil {
		return it, err
	}
	for _, f := range []struct {
		name string
		in   *string
		out  **model.Date
	}{{"start", in.Start, &it.Start}, {"end", in.End, &it.End}} {
		if f.in != nil {
			d, err := dateField(f.name, *f.in)
			if err != nil {
				return it, err
			}
			*f.out = &d
		}
	}
	if in.Recurrence.Anchor != nil {
		d, err := dateField("anchor", *in.Recurrence.Anchor)
		if err != nil {
			return it, err
		}
		it.Recurrence.Anchor = d
	}
	// budget list shows the effective timing, so an item fed back as listed keeps the timing the base stored, empty or explicit.
	if base != nil && it.Timing == base.EffectiveTiming() {
		it.Timing = base.Timing
	}
	return it, nil
}

// itemFlagsSet reports whether any flag that describes an item is set.
func (c *call) itemFlagsSet() bool {
	for _, f := range itemFlags(true) {
		if c.set[f.Name] {
			return true
		}
	}
	return false
}

// recurrenceArgs are the flags --day, --day2, --count and --unit of an item, nil or empty where not given.
type recurrenceArgs struct {
	day, day2, count *int
	unit             string
}

// recurrenceFor builds the recurrence and start date of an item from the --every flag and the date on, which is the --start flag. A one-time, biweekly and annual item's date is also kept in the recurrence; weekly, quarterly and custom items take their phase from the start.
func recurrenceFor(every string, on model.Date, a recurrenceArgs) (model.Recurrence, *model.Date, error) {
	_, m, d := on.YMD()
	if every == "annually" {
		every = string(model.Annual)
	}
	kind := model.RecurrenceKind(every)
	if a.day != nil && kind != model.Monthly && kind != model.Semimonthly {
		return model.Recurrence{}, nil, usageErr("--day is for monthly and semimonthly items")
	}
	if a.day2 != nil && kind != model.Semimonthly {
		return model.Recurrence{}, nil, usageErr("--day2 is for semimonthly items")
	}
	if (a.count != nil || a.unit != "") && kind != model.Custom {
		return model.Recurrence{}, nil, usageErr("--count and --unit are for custom items")
	}
	switch kind {
	case model.OneTime, model.Biweekly:
		return model.Recurrence{Kind: kind, Anchor: on}, &on, nil
	case model.Weekly, model.Quarterly:
		return model.Recurrence{Kind: kind}, &on, nil
	case model.Annual:
		return model.Recurrence{Kind: kind, Month: m, Day: d}, &on, nil
	case model.Monthly:
		if a.day == nil {
			a.day = &d
		}
		return model.Recurrence{Kind: kind, Day: *a.day}, &on, nil
	case model.Semimonthly:
		if a.day == nil {
			a.day = &d
		}
		if a.day2 == nil {
			return model.Recurrence{}, nil, usageErr("a semimonthly item needs --day2")
		}
		return model.Recurrence{Kind: kind, Day: *a.day, Day2: *a.day2}, &on, nil
	case model.Custom:
		if a.count == nil || a.unit == "" {
			return model.Recurrence{}, nil, usageErr("a custom item needs --count and --unit")
		}
		return model.Recurrence{Kind: kind, Count: *a.count, Unit: model.RecurrenceUnit(a.unit)}, &on, nil
	}
	return model.Recurrence{}, nil, usageErr("--every %q is not one of %s", every, strings.Join(itemEvery, ", "))
}

// itemFromFlags builds the item the flags describe. With base, the flags given override base's fields.
func (c *call) itemFromFlags(base *model.BudgetItem) (model.BudgetItem, error) {
	var it model.BudgetItem
	if base != nil {
		it = *base
	} else {
		for _, f := range []string{"kind", "name", "amount", "every"} {
			if !c.set[f] {
				return it, usageErr("item: missing flag --%s, or give the item as JSON", f)
			}
		}
		if !c.set["start"] && !c.set["on"] {
			return it, usageErr("item: missing flag --start, or give the item as JSON")
		}
	}
	if c.set["start"] && c.set["on"] {
		return it, usageErr("--start and --on are the same flag; give one")
	}
	if c.set["kind"] {
		it.Kind = model.ItemKind(c.str("kind"))
	}
	if c.set["name"] {
		it.Name = c.str("name")
	}
	if c.set["id"] {
		it.ID = c.str("id")
	}
	if v, ok, err := c.number("amount"); err != nil {
		return it, err
	} else if ok {
		it.Amount = model.Constant(v)
	}
	if c.set["indexed"] {
		it.Indexed = c.str("indexed") == "true"
	}
	if c.set["income-type"] {
		it.IncomeType = model.IncomeType(c.str("income-type"))
	}
	if c.set["timing"] {
		it.Timing = model.Timing(c.str("timing"))
	}
	if v, ok, err := c.number("match-percent"); err != nil {
		return it, err
	} else if ok {
		it.MatchPercent = v
	}
	if v, ok, err := c.number("match-up-to"); err != nil {
		return it, err
	} else if ok {
		it.MatchUpTo = v
	}
	if c.set["match-account"] {
		it.MatchAccount = ""
		if c.str("match-account") != "none" {
			id, err := c.accountNamed(c.str("match-account"))
			if err != nil {
				return it, err
			}
			it.MatchAccount = id
		}
	}
	if v, ok, err := c.number("growth"); err != nil {
		return it, err
	} else if ok {
		it.Growth = model.Schedule{}
		if v != 0 {
			it.Growth = model.Constant(v)
		}
	}
	if c.set["account"] {
		it.Account = model.AccountID(c.str("account"))
	}
	if c.set["from"] {
		it.From = model.AccountID(c.str("from"))
	}
	if c.set["to"] {
		it.To = model.AccountID(c.str("to"))
	}
	if c.set["end"] && c.str("end") == "none" {
		it.End = nil
	} else if d, ok, err := c.date("end"); err != nil {
		return it, err
	} else if ok {
		it.End = &d
	}
	dateFlag := "start"
	if c.set["on"] {
		dateFlag = "on"
	}
	if base == nil || c.set["every"] || c.set["start"] || c.set["on"] || c.set["day"] || c.set["day2"] || c.set["count"] || c.set["unit"] {
		cur := it.Recurrence
		every := string(cur.Kind)
		if c.set["every"] {
			every = c.str("every")
			if every == "annually" {
				every = string(model.Annual)
			}
		}
		same := base != nil && every == string(cur.Kind)
		var on model.Date
		switch {
		case cur.Anchor != 0:
			on = cur.Anchor
		case it.Start != nil:
			on = *it.Start
		}
		var args recurrenceArgs
		if same {
			// What the item has stays unless a flag changes it; a monthly item's day follows a new --start.
			if cur.Kind == model.Semimonthly || (cur.Kind == model.Monthly && !c.set[dateFlag]) {
				args.day = &cur.Day
			}
			if cur.Kind == model.Semimonthly {
				args.day2 = &cur.Day2
			}
			if cur.Kind == model.Custom {
				args.count, args.unit = &cur.Count, string(cur.Unit)
			}
		}
		if d, ok, err := c.date(dateFlag); err != nil {
			return it, err
		} else if ok {
			on = d
		} else if base != nil && !same {
			return it, usageErr("item: changing --every needs --start")
		}
		for _, f := range []struct {
			name string
			out  **int
		}{{"day", &args.day}, {"day2", &args.day2}, {"count", &args.count}} {
			if v, ok, err := c.integer(f.name); err != nil {
				return it, err
			} else if ok {
				*f.out = &v
			}
		}
		if c.set["unit"] {
			args.unit = c.str("unit")
		}
		var err error
		if it.Recurrence, it.Start, err = recurrenceFor(every, on, args); err != nil {
			return it, err
		}
		if same && !c.set[dateFlag] {
			// Without a date flag the item keeps its start and the anchor it counts from.
			it.Start = base.Start
			switch cur.Kind {
			case model.OneTime, model.Biweekly, model.Weekly, model.Quarterly, model.Custom:
				it.Recurrence.Anchor = cur.Anchor
			case model.Annual:
				it.Recurrence.Month, it.Recurrence.Day = cur.Month, cur.Day
			}
		}
	}
	return it, nil
}

// itemInputFor reads the item from the flags, or from JSON when no item flag is given. With base, flags override base.
func (c *call) itemInputFor(base *model.BudgetItem) (model.BudgetItem, error) {
	if c.itemFlagsSet() {
		if c.set["json-file"] {
			return model.BudgetItem{}, usageErr("item flags and --json-file cannot be used together")
		}
		return c.itemFromFlags(base)
	}
	return c.itemJSON(base)
}

func cmdItemAdd(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	it, err := c.itemInputFor(nil)
	if err != nil {
		return nil, err
	}
	res, err := ops.AddItem(c.st, c.rec, p.Name, it, c.options())
	return c.finish(p.Name, res, err)
}

func cmdItemEdit(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	var own []model.BudgetItem
	for _, m := range p.Mods {
		if m.Property == model.PropAddItem && m.Item != nil {
			own = append(own, *m.Item)
		}
	}
	id, err := pickItem(own, c.args[1])
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(own, func(it model.BudgetItem) bool { return it.ID == id })
	if i < 0 {
		return nil, failErr("unknown_target", "projection %q does not add a budget item %q", p.Name, c.args[1])
	}
	it, err := c.itemInputFor(&own[i])
	if err != nil {
		return nil, err
	}
	res, err := ops.EditItem(c.st, c.rec, p.Name, id, it, c.options())
	return c.finish(p.Name, res, err)
}

func cmdItemEnd(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	from, err := parseCLIDate(c.args[2], false)
	if err != nil {
		return nil, usageErr("<date>: %v", err)
	}
	id, err := pickItem(itemsInChain(c.rec, p), c.args[1])
	if err != nil {
		return nil, err
	}
	res, err := ops.EndItem(c.st, c.rec, p.Name, id, from, c.options())
	return c.finish(p.Name, res, err)
}
