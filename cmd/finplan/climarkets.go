// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"slices"
	"strconv"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

const (
	shockIndexNote     = "the shock's index, as projection show numbers them from 0"
	rebalanceIndexNote = "the rebalance's index, as projection show numbers them from 0"
)

var marketCommands = []*command{
	{
		Name: "shock add", Summary: "add a market shock to a projection: on its date every account holding the class changes by the percent in proportion to its exposure, so a 40% drop in a class cuts an account that holds 60% of it in that class by 40% of that holding; accounts without an allocation are unaffected",
		Args: []argDef{{Name: "projection", Description: "projection to change", Required: true}},
		Flags: []flagDef{
			{Name: "class", Type: typeString, Required: true, Description: classNote},
			{Name: "date", Type: typeDate, Required: true, Description: "the day the shock hits: " + dateNote},
			{Name: "percent", Type: typeNumber, Required: true, Description: "the change of the class, in percent: -40 for a 40% drop; must be above -100"},
		},
		Output: writeOut,
		run:    cmdShockAdd,
	},
	{
		Name: "shock edit", Summary: "change a shock of a projection; flags given override its current values",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "index", Description: shockIndexNote, Required: true},
		},
		Flags: []flagDef{
			{Name: "class", Type: typeString, Description: classNote},
			{Name: "date", Type: typeDate, Description: "the day the shock hits: " + dateNote},
			{Name: "percent", Type: typeNumber, Description: "the change of the class, in percent: -40 for a 40% drop"},
		},
		Output: writeOut,
		run:    cmdShockEdit,
	},
	{
		Name: "shock remove", Summary: "remove a shock from a projection",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "index", Description: shockIndexNote, Required: true},
		},
		Output: writeOut,
		run:    cmdShockRemove,
	},
	{
		Name: "rebalance add", Summary: "add a rebalance to a projection: on its date the account's holdings are traded to the target allocation, and money paid in after it is split that way. In a taxable account the sales realize capital gains taxed that year; in a tax-advantaged account trades are never taxed. Only an account with an allocation is rebalanced",
		Args: []argDef{{Name: "projection", Description: "projection to change", Required: true}},
		Flags: []flagDef{
			{Name: "account", Type: typeString, Required: true, Description: "account id or name"},
			{Name: "date", Type: typeDate, Required: true, Description: "the day of the rebalance: " + dateNote},
			{Name: "set", Type: typeString, Required: true, Description: "the target as <class>=<percent> pairs separated by commas: us-large-cap=60,us-bonds=40; a class may be 0, and the percents must sum to 100"},
		},
		Output: writeOut,
		run:    cmdRebalanceAdd,
	},
	{
		Name: "rebalance edit", Summary: "change a rebalance of a projection; flags given override its current values",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "index", Description: rebalanceIndexNote, Required: true},
		},
		Flags: []flagDef{
			{Name: "account", Type: typeString, Description: "account id or name"},
			{Name: "date", Type: typeDate, Description: "the day of the rebalance: " + dateNote},
			{Name: "set", Type: typeString, Description: "the target as <class>=<percent> pairs separated by commas; the percents must sum to 100"},
		},
		Output: writeOut,
		run:    cmdRebalanceEdit,
	},
	{
		Name: "rebalance remove", Summary: "remove a rebalance from a projection",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "index", Description: rebalanceIndexNote, Required: true},
		},
		Output: writeOut,
		run:    cmdRebalanceRemove,
	},
	{
		Name: "account rebalance", Summary: "turn an investment account's yearly rebalance on or off; while on, projections rebalance the account to its allocation every 1 January after the projection starts (off by default)",
		Args:   []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Flags:  []flagDef{{Name: "yearly", Type: typeEnum, Required: true, Choices: []string{"on", "off"}, Description: "on rebalances every 1 January; off never does"}},
		Output: accountOut,
		run:    cmdAccountRebalance,
	},
}

func init() {
	writeCommands = append(writeCommands, marketCommands...)
}

type jshock struct {
	Index   int     `json:"index"`
	Class   string  `json:"class"`
	Date    jdate   `json:"date"`
	Percent float64 `json:"percent"`
}

type jrebal struct {
	Index      int        `json:"index"`
	Account    string     `json:"account"`
	Date       jdate      `json:"date"`
	Allocation []jholding `json:"allocation"`
}

// entryIndex reads the positional argument at pos as an index into the n things the projection has of the kind.
func (c *call) entryIndex(pos, n int, kind string) (int, error) {
	i, err := strconv.Atoi(c.args[pos])
	if err != nil {
		return 0, usageErr("<%s>: %q is not a whole number", c.cmd.Args[pos].Name, c.args[pos])
	}
	if i < 0 || i >= n {
		return 0, failErr("invalid", "<%s>: index %d is out of range: the projection has %d %s", c.cmd.Args[pos].Name, i, n, kind)
	}
	return i, nil
}

// percentOf is a fraction in percent, free of the float noise of the multiplication.
func percentOf(f float64) float64 { return math.Round(f*100*1e9) / 1e9 }

// parseWeights reads <class>=<percent> pairs as shares of a whole.
func parseWeights(rec *model.Record, s string) ([]model.Weight, error) {
	hs, err := parseAllocation(rec, s)
	if err != nil {
		return nil, err
	}
	var out []model.Weight
	for _, h := range hs {
		out = append(out, model.Weight{Class: h.Class, Share: h.Percent / 100})
	}
	return out, nil
}

func cmdShockAdd(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	class, err := classNamed(c.rec, c.str("class"))
	if err != nil {
		return nil, err
	}
	d, _, err := c.date("date")
	if err != nil {
		return nil, err
	}
	pct, _, err := c.number("percent")
	if err != nil {
		return nil, err
	}
	res, err := ops.AddShock(c.st, c.rec, p.Name, model.Shock{Class: class, Date: d, Change: pct / 100}, c.options())
	return c.finish(p.Name, res, err)
}

func cmdShockEdit(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	i, err := c.entryIndex(1, len(p.Shocks), "shocks")
	if err != nil {
		return nil, err
	}
	s := p.Shocks[i]
	if c.set["class"] {
		if s.Class, err = classNamed(c.rec, c.str("class")); err != nil {
			return nil, err
		}
	}
	if d, ok, err := c.date("date"); err != nil {
		return nil, err
	} else if ok {
		s.Date = d
	}
	if v, ok, err := c.number("percent"); err != nil {
		return nil, err
	} else if ok {
		s.Change = v / 100
	}
	res, err := ops.SetShock(c.st, c.rec, p.Name, i, s, c.options())
	return c.finish(p.Name, res, err)
}

func cmdShockRemove(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	i, err := c.entryIndex(1, len(p.Shocks), "shocks")
	if err != nil {
		return nil, err
	}
	res, err := ops.RemoveShock(c.st, c.rec, p.Name, i, c.options())
	return c.finish(p.Name, res, err)
}

func cmdRebalanceAdd(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	id, err := c.accountNamed(c.str("account"))
	if err != nil {
		return nil, err
	}
	d, _, err := c.date("date")
	if err != nil {
		return nil, err
	}
	ws, err := parseWeights(c.rec, c.str("set"))
	if err != nil {
		return nil, err
	}
	res, err := ops.AddRebalance(c.st, c.rec, p.Name, model.Rebalance{Account: id, Date: d, Allocation: ws}, c.options())
	return c.finish(p.Name, res, err)
}

func cmdRebalanceEdit(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	i, err := c.entryIndex(1, len(p.Rebalances), "rebalances")
	if err != nil {
		return nil, err
	}
	r := p.Rebalances[i]
	r.Allocation = slices.Clone(r.Allocation)
	if c.set["account"] {
		if r.Account, err = c.accountNamed(c.str("account")); err != nil {
			return nil, err
		}
	}
	if d, ok, err := c.date("date"); err != nil {
		return nil, err
	} else if ok {
		r.Date = d
	}
	if c.set["set"] {
		if r.Allocation, err = parseWeights(c.rec, c.str("set")); err != nil {
			return nil, err
		}
	}
	res, err := ops.SetRebalance(c.st, c.rec, p.Name, i, r, c.options())
	return c.finish(p.Name, res, err)
}

func cmdRebalanceRemove(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	i, err := c.entryIndex(1, len(p.Rebalances), "rebalances")
	if err != nil {
		return nil, err
	}
	res, err := ops.RemoveRebalance(c.st, c.rec, p.Name, i, c.options())
	return c.finish(p.Name, res, err)
}

func cmdAccountRebalance(c *call) (any, error) {
	id, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	res, err := ops.SetRebalanceYearly(c.st, c.rec, id, c.str("yearly") == "on", c.options())
	return c.accountResult(res, err, id)
}
