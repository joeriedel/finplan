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
)

// The write commands. Each makes its edit through internal/ops, so the CLI and the app agree on what an edit may do, and prints {dry_run, changed, projection, ...}: the projection as projection show prints it, after the edit. With --dry-run the edit is validated and applied to a copy and nothing is saved. An edit that would change nothing is not an error: it prints changed false.

const (
	writeOut     = "{dry_run, changed, projection: as projection show prints it, after the edit}"
	dryRunDesc   = "validate the edit and print what it would change, but save nothing"
	jsonFileDesc = "read the JSON input from this file instead of stdin"
	scheduleNote = "a schedule is [{\"from\": \"YYYY-MM-DD\", \"end\": \"YYYY-MM-DD\" or null, \"value\": number}, ...], sorted and not overlapping; a value holds from its from date until its end or the next segment"
	itemJSONNote = "the item is {id (optional), name, kind (income, expense or transfer), amount (above zero), recurrence: {kind (one-time, monthly, biweekly or annual), anchor (one-time and biweekly), month (annual), day (monthly and annual)}, start (optional), end (optional), indexed, account (income and expense), from and to (transfer)}, the shape budget list prints"
	modIndexNote = "the modification's index, as projection show numbers them from 0"
)

var itemKinds = []string{string(model.ItemIncome), string(model.ItemExpense), string(model.ItemTransfer)}

var itemEvery = []string{string(model.OneTime), string(model.Monthly), string(model.Biweekly), string(model.Annual)}

// itemFlags are the flags that describe a budget item; none is required, so that the item can come as JSON instead.
func itemFlags(forAdd bool) []flagDef {
	fs := []flagDef{
		{Name: "kind", Type: typeEnum, Choices: itemKinds, Description: "income, expense or transfer"},
		{Name: "name", Type: typeString, Description: "the item's name"},
		{Name: "amount", Type: typeNumber, Description: "the amount, above zero, per occurrence"},
		{Name: "every", Type: typeEnum, Choices: itemEvery, Description: "how often the item happens"},
		{Name: "on", Type: typeDate, Description: "the date of a one-time item, or the first date of a recurring one: " + dateNote},
		{Name: "day", Type: typeInt, Description: "day of the month of a monthly item; the day of --on when omitted"},
		{Name: "end", Type: typeDate, Description: "last date the item happens: " + dateNote + "; none removes it"},
		{Name: "account", Type: typeString, Description: "account id of an income or expense item"},
		{Name: "from", Type: typeString, Description: "account id a transfer draws from"},
		{Name: "to", Type: typeString, Description: "account id a transfer pays into"},
		{Name: "indexed", Type: typeBool, Default: "false", Description: "the amount grows with inflation"},
	}
	if forAdd {
		fs = append(fs, flagDef{Name: "id", Type: typeString, Description: "the item's id; made from the name when omitted"})
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
		Name: "projection set-inflation", Summary: "set a projection's inflation rate to a constant, replacing its first global inflation modification",
		Args: []argDef{
			{Name: "name", Description: "projection to change", Required: true},
			{Name: "rate", Description: "a fraction per year, 0.03 for 3% (put -- before a negative one); none removes the modification", Required: true},
		},
		Output: writeOut,
		run:    cmdProjectionSetInflation,
	},
	{
		Name: "mod add", Summary: "add a modification of an item's amount, an account's or asset's growth, or inflation to a projection; its target must exist, for an item in the record or added earlier in the chain",
		Args: []argDef{{Name: "projection", Description: "projection to change", Required: true}},
		Flags: []flagDef{
			{Name: "target", Type: typeString, Required: true, Description: "what to change: item:<id or name>, account:<id or name>, asset:<id or name> or global"},
			{Name: "property", Type: typeEnum, Required: true, Choices: []string{model.PropAmount, model.PropGrowth, model.PropInflation}, Description: "amount for an item, growth for an account or asset, inflation for global"},
			{Name: "value", Type: typeNumber, Description: "a constant value (a rate is a fraction: 0.04 for 4%); without it the schedule is read as JSON"},
			{Name: "from", Type: typeDate, Description: "with --value, the date the value starts: " + dateNote + "; always when omitted"},
			{Name: "at", Type: typeInt, Description: "index to insert at; at the end when omitted"},
		},
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
			{Name: "value", Type: typeNumber, Description: "a constant value; without it the schedule is read as JSON"},
			{Name: "from", Type: typeDate, Description: "with --value, the date the value starts: " + dateNote + "; always when omitted"},
		},
		Input:  "without --value, a schedule: " + scheduleNote,
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
		out["projection"] = projectionJSON(c.rec.Projection(name))
	case err != nil:
		return nil, opsError(err)
	case res.Projection != nil:
		out["projection"] = projectionJSON(res.Projection)
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
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "removed": res.Removed, "projection": projectionJSON(p)}, nil
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
		if c.set["from"] {
			return model.Schedule{}, usageErr("--from needs --value")
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
	if !ok {
		return model.Constant(v), nil
	}
	return model.NewSchedule(model.Segment{From: from, Value: v})
}

func (c *call) scheduleJSON() (model.Schedule, error) {
	var in []struct {
		From  *string  `json:"from"`
		End   *string  `json:"end"`
		Value *float64 `json:"value"`
	}
	if err := c.readJSON(&in); err != nil {
		return model.Schedule{}, err
	}
	segs := make([]model.Segment, len(in))
	for i, s := range in {
		if s.From == nil || s.Value == nil {
			return model.Schedule{}, usageErr("the JSON input: segment %d needs a from and a value", i)
		}
		from, err := dateField("from", *s.From)
		if err != nil {
			return model.Schedule{}, err
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
	if len(segs) == 0 {
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

// modTarget reads --target for the projection.
func (c *call) modTarget(p *model.Projection) (model.ObjectRef, error) {
	kind, name, _ := strings.Cut(c.str("target"), ":")
	switch kind {
	case "global":
		if name != "" {
			return model.ObjectRef{}, usageErr("--target global takes no name")
		}
		return model.ObjectRef{Kind: model.KindGlobal}, nil
	case "item", "account", "asset":
		if name == "" {
			return model.ObjectRef{}, usageErr("--target %s needs a name: %s:<id or name>", kind, kind)
		}
	default:
		return model.ObjectRef{}, usageErr("--target %q: use item:<id or name>, account:<id or name>, asset:<id or name> or global", c.str("target"))
	}
	switch kind {
	case "item":
		id, err := pickItem(itemsInChain(c.rec, p), name)
		return model.ObjectRef{Kind: model.KindBudgetItem, ID: id}, err
	case "account":
		cand, err := resolveTarget(c.rec, name, true)
		if err != nil {
			// Not found is the operation's to refuse as an unknown target; an ambiguous name is not.
			if ce := (*cliError)(nil); errors.As(err, &ce) && ce.Code == "ambiguous" {
				return model.ObjectRef{}, err
			}
			return model.ObjectRef{Kind: model.KindAccount, ID: name}, nil
		}
		return model.ObjectRef{Kind: model.KindAccount, ID: string(cand.account)}, nil
	}
	cand, err := resolveTarget(c.rec, name, false)
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
	values, err := c.schedule()
	if err != nil {
		return nil, err
	}
	m := model.Modification{Target: target, Property: c.str("property"), Values: values}
	var res *ops.Result
	if atSet {
		res, err = ops.InsertMod(c.st, c.rec, p.Name, at, m, c.options())
	} else {
		res, err = ops.AddMod(c.st, c.rec, p.Name, m, c.options())
	}
	return c.finish(p.Name, res, err)
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
	values, err := c.schedule()
	if err != nil {
		return nil, err
	}
	res, err := ops.SetModValues(c.st, c.rec, p.Name, i, values, c.options())
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
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Amount     float64 `json:"amount"`
	Recurrence struct {
		Kind   string  `json:"kind"`
		Anchor *string `json:"anchor"`
		Month  int     `json:"month"`
		Day    int     `json:"day"`
	} `json:"recurrence"`
	Start   *string `json:"start"`
	End     *string `json:"end"`
	Indexed bool    `json:"indexed"`
	Account string  `json:"account"`
	From    string  `json:"from"`
	To      string  `json:"to"`
}

func (c *call) itemJSON() (model.BudgetItem, error) {
	var in itemInput
	if err := c.readJSON(&in); err != nil {
		return model.BudgetItem{}, err
	}
	it := model.BudgetItem{
		ID: in.ID, Name: in.Name, Kind: model.ItemKind(in.Kind), Amount: in.Amount, Indexed: in.Indexed,
		Account: model.AccountID(in.Account), From: model.AccountID(in.From), To: model.AccountID(in.To),
		Recurrence: model.Recurrence{Kind: model.RecurrenceKind(in.Recurrence.Kind), Month: time.Month(in.Recurrence.Month), Day: in.Recurrence.Day},
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

// recurrenceFor builds the recurrence and start date an item with the flags --every, --on and --day has. A one-time item has no start; the others start on --on.
func recurrenceFor(every string, on model.Date, day *int) (model.Recurrence, *model.Date, error) {
	_, m, d := on.YMD()
	switch model.RecurrenceKind(every) {
	case model.OneTime:
		if day != nil {
			return model.Recurrence{}, nil, usageErr("--day is for monthly items")
		}
		return model.Recurrence{Kind: model.OneTime, Anchor: on}, nil, nil
	case model.Biweekly:
		if day != nil {
			return model.Recurrence{}, nil, usageErr("--day is for monthly items")
		}
		return model.Recurrence{Kind: model.Biweekly, Anchor: on}, &on, nil
	case model.Annual:
		if day != nil {
			return model.Recurrence{}, nil, usageErr("--day is for monthly items")
		}
		return model.Recurrence{Kind: model.Annual, Month: m, Day: d}, &on, nil
	case model.Monthly:
		if day == nil {
			day = &d
		}
		return model.Recurrence{Kind: model.Monthly, Day: *day}, &on, nil
	}
	return model.Recurrence{}, nil, usageErr("--every %q is not one of %s", every, strings.Join(itemEvery, ", "))
}

// itemFromFlags builds the item the flags describe. With base, the flags given override base's fields.
func (c *call) itemFromFlags(base *model.BudgetItem) (model.BudgetItem, error) {
	var it model.BudgetItem
	if base != nil {
		it = *base
	} else {
		for _, f := range []string{"kind", "name", "amount", "every", "on"} {
			if !c.set[f] {
				return it, usageErr("item: missing flag --%s, or give the item as JSON", f)
			}
		}
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
		it.Amount = v
	}
	if c.set["indexed"] {
		it.Indexed = c.str("indexed") == "true"
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
	if base == nil || c.set["every"] || c.set["on"] || c.set["day"] {
		every, on, day := string(it.Recurrence.Kind), model.Date(0), (*int)(nil)
		switch it.Recurrence.Kind {
		case model.OneTime, model.Biweekly:
			on = it.Recurrence.Anchor
		default:
			if it.Start != nil {
				on = *it.Start
			}
		}
		if c.set["every"] {
			every = c.str("every")
		} else if it.Recurrence.Kind == model.Monthly && !c.set["on"] {
			day = &it.Recurrence.Day
		}
		if d, ok, err := c.date("on"); err != nil {
			return it, err
		} else if ok {
			on = d
		} else if base != nil && every != string(it.Recurrence.Kind) {
			return it, usageErr("item: changing --every needs --on")
		}
		if v, ok, err := c.integer("day"); err != nil {
			return it, err
		} else if ok {
			day = &v
		}
		var err error
		if it.Recurrence, it.Start, err = recurrenceFor(every, on, day); err != nil {
			return it, err
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
	return c.itemJSON()
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
