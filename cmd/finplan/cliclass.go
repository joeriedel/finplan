// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

const (
	classNote = "market class id or name (ids win over names)"
	classOut  = "{dry_run, changed, class: {id, name, return: [{from, end, value}], volatility}, after the edit; for class remove, {dry_run, changed, removed}}"
)

var classReadCommands = []*command{
	{
		Name: "class list", Summary: "list the market classes an account's allocation splits its balance among, with each class's expected return and volatility, and the correlations between classes",
		Output: "{classes: [{id, name, return: [{from, end, value}] (the annual growth, a fraction), volatility (the annual standard deviation of the return, a fraction)}], correlations: [{a, b, value}]}; a pair with no correlation is uncorrelated (0), a class is always fully correlated with itself, and a and b are in id order",
		run:    cmdClassList,
	},
}

var classWriteCommands = []*command{
	{
		Name: "class add", Summary: "add a market class",
		Args: []argDef{{Name: "name", Description: "the class's name", Required: true}},
		Flags: []flagDef{
			{Name: "id", Type: typeString, Description: "the class's id; made from the name when omitted"},
			{Name: "return", Type: typeNumber, Description: "expected annual return, a fraction: 0.07 for 7%; 0 when omitted"},
			{Name: "volatility", Type: typeNumber, Description: "annual standard deviation of the return, a fraction: 0.15 for 15%; 0 when omitted"},
		},
		Output: classOut,
		run:    cmdClassAdd,
	},
	{
		Name: "class set", Summary: "change a market class's name, its expected return (a constant for all dates) or its volatility; flags given override its current values",
		Args: []argDef{{Name: "class", Description: classNote, Required: true}},
		Flags: []flagDef{
			{Name: "name", Type: typeString, Description: "the class's name"},
			{Name: "return", Type: typeNumber, Description: "expected annual return, a fraction: 0.07 for 7%"},
			{Name: "volatility", Type: typeNumber, Description: "annual standard deviation of the return, a fraction: 0.15 for 15%"},
		},
		Output: classOut,
		run:    cmdClassSet,
	},
	{
		Name: "class remove", Summary: "remove a market class and its correlations; refused while an account's allocation or a modification names it",
		Args:   []argDef{{Name: "class", Description: classNote, Required: true}},
		Output: classOut,
		run:    cmdClassRemove,
	},
	{
		Name: "class correlation", Summary: "set the correlation of the returns of two market classes; 0 removes the pair, which reads as uncorrelated",
		Args: []argDef{
			{Name: "class-a", Description: classNote, Required: true},
			{Name: "class-b", Description: classNote, Required: true},
		},
		Flags:  []flagDef{{Name: "value", Type: typeNumber, Required: true, Description: "the correlation, from -1 to 1"}},
		Output: "{dry_run, changed, a, b, value}",
		run:    cmdClassCorrelation,
	},
	{
		Name: "class growth", Summary: "set a market class's return schedule, the annual rate its balances grow at when no projection says otherwise; projections change it with mod add --target class:<id> --property growth",
		Args: []argDef{{Name: "class", Description: classNote, Required: true}},
		Flags: []flagDef{
			{Name: "clear", Type: typeBool, Default: "false", Description: "remove the class's return schedule, so it projects flat"},
			{Name: "value", Type: typeNumber, Description: "a constant annual rate, a fraction: 0.08 for 8%; without it (and without --clear) the schedule is read as JSON"},
			{Name: "from", Type: typeDate, Description: "with --value, the date the rate starts: " + dateNote + "; always when omitted"},
			{Name: "end", Type: typeDate, Description: "with --value and --from, the last date the rate holds: " + dateNote + "; open-ended when omitted"},
		},
		Input:  "without --value and --clear, a schedule: " + scheduleNote,
		Output: classOut,
		run:    cmdClassGrowth,
	},
	{
		Name: "account allocation", Summary: "set or clear how an investment account's balance is split among market classes; an allocated account grows by class (each class at its own return) instead of at its stated rate, and money paid in is split by the allocation while money paid out comes from the classes pro rata; the percents must be above 0, name each class once and sum to 100",
		Args: []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Flags: []flagDef{
			{Name: "set", Type: typeString, Description: "the allocation as <class>=<percent> pairs separated by commas: us-large-cap=60,us-bonds=40"},
			{Name: "default", Type: typeBool, Default: "false", Description: "use the suggested allocation for the account's type: 60/40 US large cap and US bonds for a 401k, TSP, IRA or 529, all US large cap for a brokerage, Roth IRA or HSA"},
			{Name: "clear", Type: typeBool, Default: "false", Description: "remove the allocation, so the account grows at its stated rate"},
		},
		Output: accountOut,
		run:    cmdAccountAllocation,
	},
}

func init() {
	commands = append(commands, classReadCommands...)
	writeCommands = append(writeCommands, classWriteCommands...)
}

type jclass struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Return     []jsegment `json:"return"`
	Volatility float64    `json:"volatility"`
}

type jcorrelation struct {
	A     string  `json:"a"`
	B     string  `json:"b"`
	Value float64 `json:"value"`
}

type jholding struct {
	Class   string  `json:"class"`
	Percent float64 `json:"percent"`
}

func classJSON(c model.MarketClass) jclass {
	return jclass{c.ID, c.Name, scheduleJSONOut(c.Return), c.Volatility}
}

func allocationJSON(alloc []model.Holding) []jholding {
	out := []jholding{}
	for _, h := range alloc {
		out = append(out, jholding{h.Class, h.Percent})
	}
	return out
}

// classNamed returns the id of the market class s names, by id, else by name ignoring case.
func classNamed(rec *model.Record, s string) (string, error) {
	if rec.Class(s) != nil {
		return s, nil
	}
	var ids []string
	for _, c := range rec.Classes {
		if strings.EqualFold(c.Name, s) {
			ids = append(ids, c.ID)
		}
	}
	switch len(ids) {
	case 0:
		return "", failErr("unknown_target", "there is no market class %q", s)
	case 1:
		return ids[0], nil
	}
	return "", failErr("ambiguous", "%q matches more than one market class: %s", s, strings.Join(ids, ", "))
}

func cmdClassList(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	classes, corrs := []jclass{}, []jcorrelation{}
	for _, cl := range rec.Classes {
		classes = append(classes, classJSON(cl))
	}
	for _, k := range rec.Correlations {
		corrs = append(corrs, jcorrelation{k.A, k.B, k.Value})
	}
	return map[string]any{"classes": classes, "correlations": corrs}, nil
}

// classResult is the output of an edit of a market class.
func (c *call) classResult(res *ops.Result, err error, id string) (any, error) {
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	rec := c.rec
	if changed {
		rec = res.Record
	}
	out["class"] = classJSON(*rec.Class(id))
	return out, nil
}

func cmdClassAdd(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	ret, _, err := c.number("return")
	if err != nil {
		return nil, err
	}
	vol, _, err := c.number("volatility")
	if err != nil {
		return nil, err
	}
	cl := model.MarketClass{ID: c.str("id"), Name: c.args[0], Return: model.Constant(ret), Volatility: vol}
	res, err := ops.AddClass(st, rec, cl, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "class": classJSON(res.Record.Classes[len(res.Record.Classes)-1])}, nil
}

func cmdClassSet(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	id, err := classNamed(rec, c.args[0])
	if err != nil {
		return nil, err
	}
	var ch ops.ClassChange
	if c.set["name"] {
		ch.Name = model.Ptr(c.str("name"))
	}
	if v, ok, err := c.number("return"); err != nil {
		return nil, err
	} else if ok {
		ch.Return = &v
	}
	if v, ok, err := c.number("volatility"); err != nil {
		return nil, err
	} else if ok {
		ch.Volatility = &v
	}
	if ch == (ops.ClassChange{}) {
		return nil, usageErr("class set needs at least one of --name, --return and --volatility")
	}
	res, err := ops.EditClass(c.st, rec, id, ch, c.options())
	return c.classResult(res, err, id)
}

func cmdClassRemove(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	id, err := classNamed(rec, c.args[0])
	if err != nil {
		return nil, err
	}
	if _, err := ops.RemoveClass(c.st, rec, id, c.options()); err != nil {
		return nil, opsError(err)
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "removed": id}, nil
}

func cmdClassCorrelation(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var ids [2]string
	for i := range ids {
		if ids[i], err = classNamed(rec, c.args[i]); err != nil {
			return nil, err
		}
	}
	v, _, err := c.number("value")
	if err != nil {
		return nil, err
	}
	a, b := ids[0], ids[1]
	if a > b {
		a, b = b, a
	}
	out := map[string]any{"dry_run": c.options().DryRun, "changed": true, "a": a, "b": b, "value": v}
	_, err = ops.SetCorrelation(c.st, rec, a, b, v, c.options())
	if errors.Is(err, ops.ErrNoChange) {
		out["changed"] = false
		return out, nil
	}
	if err != nil {
		return nil, opsError(err)
	}
	return out, nil
}

func cmdClassGrowth(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	id, err := classNamed(rec, c.args[0])
	if err != nil {
		return nil, err
	}
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
	res, err := ops.SetClassGrowth(c.st, rec, id, growth, c.options())
	return c.classResult(res, err, id)
}

// parseAllocation reads <class>=<percent> pairs separated by commas, naming each class by id or name.
func parseAllocation(rec *model.Record, s string) ([]model.Holding, error) {
	var out []model.Holding
	for _, pair := range strings.Split(s, ",") {
		name, pct, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, usageErr("--set: %q is not <class>=<percent>", strings.TrimSpace(pair))
		}
		id, err := classNamed(rec, strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(pct), 64)
		if err != nil {
			return nil, usageErr("--set: %q is not a number", strings.TrimSpace(pct))
		}
		out = append(out, model.Holding{Class: id, Percent: v})
	}
	return out, nil
}

func cmdAccountAllocation(c *call) (any, error) {
	id, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	modes := 0
	for _, f := range []string{"default", "clear"} {
		if c.str(f) == "true" {
			modes++
		}
	}
	if c.set["set"] {
		modes++
	}
	if modes != 1 {
		return nil, usageErr("account allocation needs exactly one of --set, --default and --clear")
	}
	var alloc []model.Holding
	if c.set["set"] {
		if alloc, err = parseAllocation(c.rec, c.str("set")); err != nil {
			return nil, err
		}
	}
	res, err := ops.SetAllocation(c.st, c.rec, id, slices.Clone(alloc), c.str("default") == "true", c.options())
	return c.accountResult(res, err, id)
}
