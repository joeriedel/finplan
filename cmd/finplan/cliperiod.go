// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// recentPeriods is how many periods of an item budget period list shows variance for: the current one and the five before it.
const recentPeriods = 6

const periodOut = "{item, name, period_start, period_end, timing, budgeted, expected (null when not set), posted, posted_auto (true when posted is the estimate: a lump counts as posted once its date is on or before the item's cutoff, a spread item in proportion to the days of the period up to the cutoff), status (auto, posted or pending), remaining (what the projection still applies for the period: expected or budgeted less posted, not below 0, and nothing when posted), variance (posted less budgeted while the period is open; once it is over, expected if set, else posted, less budgeted), cutoff (the date the item's balances run to), occurrence_id (null when nothing was recorded for the period), history: [{period_start, period_end, budgeted, expected, posted, status, variance}] (the last 6 periods, oldest first, the current one last)}; amounts are rounded to cents, and budgeted is the base budget's amount"

var periodCommands = []*command{
	{
		Name: "budget period list", Summary: "show each budget item's current period against what has posted, with the variance of its recent periods",
		Flags: []flagDef{
			{Name: "date", Type: typeDate, Description: "show the period containing this date: " + dateNote + "; the record's today when omitted"},
			{Name: "projection", Type: typeString, Description: projDesc + "; its start decides the cutoff of each item"},
		},
		Output: "[" + periodOut + "]",
		run:    cmdPeriodList,
	},
	{
		Name: "budget period revisions", Summary: "show every revision of one period's recorded values: the original, then each correction or void",
		Args:   []argDef{{Name: "occurrence-id", Description: "occurrence id, as budget period list shows it", Required: true}},
		Output: "{id, revisions: [{item, period_start, status, posted, expected, made_at (RFC 3339), note, void}]}",
		run:    cmdPeriodRevisions,
	},
}

var periodWriteCommands = []*command{
	{
		Name: "budget period set", Summary: "record what has posted of a budget item's current or previous period, what to expect instead of the budgeted amount, or that it is pending; only the values given change, and the earlier ones stay in budget period revisions. The projection applies only what remains: a posted status applies nothing more, a pending one applies the whole remainder",
		Args: []argDef{{Name: "item", Description: "item id or name", Required: true}},
		Flags: []flagDef{
			{Name: "period-start", Type: typeDate, Description: "a date in the period, or its start: " + dateNote + "; the period containing the record's today when omitted. Only that period and the one before it can be set"},
			{Name: "status", Type: typeEnum, Choices: periodStatuses, Description: "auto leaves what has posted to the estimate, posted says the period's amount has posted, pending that what remains is still to come, even for a lump whose date has passed"},
			{Name: "posted", Type: typeNumber, Description: "the amount posted in the period so far, which may exceed the budgeted amount; it replaces the estimate"},
			{Name: "expected", Type: typeNumber, Description: "the amount expected for this period only, instead of the budgeted one"},
			{Name: "clear", Type: typeBool, Default: "false", Description: "void what was recorded for the period, so it is as if nothing was; cannot be combined with the other values"},
			{Name: "note", Type: typeString, Description: "why, kept in budget period revisions"},
		},
		Output: "{dry_run, changed, period: " + periodOut + "}; the occurrence id is null in a dry run",
		run:    cmdPeriodSet,
	},
}

var periodStatuses = func() []string {
	var out []string
	for _, s := range model.OccurrenceStatuses {
		out = append(out, string(s))
	}
	return out
}()

func init() {
	commands = append(commands, periodCommands...)
	writeCommands = append(writeCommands, periodWriteCommands...)
}

type jperiodHistory struct {
	Start    jdate    `json:"period_start"`
	End      jdate    `json:"period_end"`
	Budgeted float64  `json:"budgeted"`
	Expected *float64 `json:"expected"`
	Posted   float64  `json:"posted"`
	Status   string   `json:"status"`
	Variance float64  `json:"variance"`
}

type jperiod struct {
	Item       string           `json:"item"`
	Name       string           `json:"name"`
	Start      jdate            `json:"period_start"`
	End        jdate            `json:"period_end"`
	Timing     string           `json:"timing"`
	Budgeted   float64          `json:"budgeted"`
	Expected   *float64         `json:"expected"`
	Posted     float64          `json:"posted"`
	PostedAuto bool             `json:"posted_auto"`
	Status     string           `json:"status"`
	Remaining  float64          `json:"remaining"`
	Variance   float64          `json:"variance"`
	Cutoff     jdate            `json:"cutoff"`
	Occurrence *int64           `json:"occurrence_id"`
	History    []jperiodHistory `json:"history"`
}

func cents(v float64) float64 { return math.Round(v*100) / 100 }

func centsPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return model.Ptr(cents(*v))
}

func periodJSON(p engine.ItemPeriods) jperiod {
	v := p.Current()
	j := jperiod{
		Item: p.Item.ID, Name: p.Item.Name, Start: jd(v.Period.Start), End: jd(v.Period.End), Timing: string(v.Timing),
		Budgeted: cents(v.Budgeted), Expected: centsPtr(v.Expected), Posted: cents(v.Posted), PostedAuto: v.PostedAuto,
		Status: string(v.Status), Remaining: cents(v.Remaining), Variance: cents(v.Variance), Cutoff: jd(p.Cutoff), History: []jperiodHistory{},
	}
	if v.Overridden && v.Override.ID != 0 {
		j.Occurrence = model.Ptr(v.Override.ID)
	}
	for _, h := range p.Periods {
		j.History = append(j.History, jperiodHistory{jd(h.Period.Start), jd(h.Period.End), cents(h.Budgeted), centsPtr(h.Expected), cents(h.Posted), string(h.Status), cents(h.Variance)})
	}
	return j
}

func cmdPeriodList(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	projection, err := c.projection(rec)
	if err != nil {
		return nil, err
	}
	asOf := rec.Today
	if d, ok, err := c.date("date"); err != nil {
		return nil, err
	} else if ok {
		asOf = d
	}
	periods, err := engine.Periods(rec, projection, asOf, recentPeriods)
	if err != nil {
		return nil, failErr("invalid", "%v", err)
	}
	out := []jperiod{}
	for _, p := range periods {
		out = append(out, periodJSON(p))
	}
	return out, nil
}

func cmdPeriodSet(c *call) (any, error) {
	it, err := c.baseItemArg(0)
	if err != nil {
		return nil, err
	}
	asOf := c.rec.Today
	day := asOf
	if d, ok, err := c.date("period-start"); err != nil {
		return nil, err
	} else if ok {
		day = d
	}
	per, ok := it.PeriodAt(day)
	if !ok {
		return nil, failErr("invalid", "budget item %q has no period on %s", it.ID, day)
	}
	var ch ops.OccurrenceChange
	if c.set["status"] {
		ch.Status = model.Ptr(model.OccurrenceStatus(c.str("status")))
	}
	if v, ok, err := c.number("posted"); err != nil {
		return nil, err
	} else if ok {
		ch.Posted = &v
	}
	if v, ok, err := c.number("expected"); err != nil {
		return nil, err
	} else if ok {
		ch.Expected = &v
	}
	var res *ops.Result
	switch clear := c.str("clear") == "true"; {
	case clear && ch != (ops.OccurrenceChange{}):
		return nil, usageErr("--clear cannot be combined with --status, --posted or --expected")
	case clear:
		res, err = ops.ClearOccurrence(c.st, c.rec, it.ID, per.Start, asOf, c.str("note"), c.options())
	case ch == (ops.OccurrenceChange{}):
		return nil, usageErr("budget period set needs at least one of --status, --posted, --expected and --clear")
	default:
		res, err = ops.SetOccurrence(c.st, c.rec, it.ID, per.Start, asOf, ch, c.str("note"), c.options())
	}
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	rec := c.rec
	if changed {
		rec = res.Record
	}
	periods, err := engine.Periods(rec, "", per.Start, recentPeriods)
	if err != nil {
		return nil, failErr("invalid", "%v", err)
	}
	for _, p := range periods {
		if p.Item.ID == it.ID {
			out["period"] = periodJSON(p)
		}
	}
	return out, nil
}

func cmdPeriodRevisions(c *call) (any, error) {
	id, revs, err := c.factRevisions(store.FactOccurrence, "occurrence")
	if err != nil {
		return nil, err
	}
	type jrevision struct {
		Item     string   `json:"item"`
		Start    jdate    `json:"period_start"`
		Status   string   `json:"status"`
		Posted   *float64 `json:"posted"`
		Expected *float64 `json:"expected"`
		jrevisionTail
	}
	out := struct {
		ID        int64       `json:"id"`
		Revisions []jrevision `json:"revisions"`
	}{ID: id, Revisions: []jrevision{}}
	for _, r := range revs {
		o := r.Occurrence
		out.Revisions = append(out.Revisions, jrevision{o.Item, jd(o.Period), string(o.Status), o.Posted, o.Expected, revisionTail(r)})
	}
	return out, nil
}
