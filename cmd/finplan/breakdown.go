// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// bkInterval is the span the breakdown panel explains, in the engine's terms: the change after from up to and including to. Unset, it is the 12 months ending at the cursor.
type bkInterval struct {
	from, to model.Date
	set      bool
}

// window returns the span the breakdown covers: the interval if set, else the 12 months ending at the cursor.
func (iv *bkInterval) window(cursor model.Date) (from, to model.Date) {
	if iv.set {
		return iv.from, iv.to
	}
	y, m, d := cursor.YMD()
	return model.D(y-1, m, d), cursor
}

// paint sets the interval from a drag between two dates, in either direction; a drag that starts and ends on one date sets nothing.
func (iv *bkInterval) paint(a, b model.Date) {
	if a != b {
		iv.from, iv.to, iv.set = min(a, b), max(a, b), true
	}
}

// setWindow sets the interval to (from, to], which must have from before to.
func (iv *bkInterval) setWindow(from, to model.Date) {
	iv.from, iv.to, iv.set = from, to, true
}

// setEdge moves the start (to false) or the end (to true) of the interval to d, keeping at least a day between them. Unset, the interval first becomes the span shown at the cursor.
func (iv *bkInterval) setEdge(cursor model.Date, end bool, d model.Date) {
	from, to := iv.window(cursor)
	if end {
		to = max(d, from+1)
	} else {
		from = min(d, to-1)
	}
	iv.setWindow(from, to)
}

func (iv *bkInterval) clear() { *iv = bkInterval{} }

// firstDay is the first day a window covers: the day after from, since the engine's window excludes it.
func firstDay(from model.Date) model.Date { return from + 1 }

// yearAt returns the window of the calendar year that holds the date.
func yearAt(d model.Date) (from, to model.Date) {
	y, _, _ := d.YMD()
	return model.D(y-1, 12, 31), model.D(y, 12, 31)
}

// sinceStart returns the window from the earliest date of the record's history (dataStart, which includes the day itself) to today.
func sinceStart(dataStart, today model.Date) (from, to model.Date) {
	return dataStart - 1, max(today, dataStart)
}

// nextTenYears returns the window from today to the same day ten years on.
func nextTenYears(today model.Date) (from, to model.Date) {
	y, m, d := today.YMD()
	return today, model.D(y+10, m, d)
}

// causeLabels are the labels of the causes that are not just the cause capitalised.
var causeLabels = map[engine.Cause]string{
	engine.CauseSocialSecurity: "Social Security",
	engine.CauseFERS:           "FERS",
}

// causeLabel returns the label of a cause: the cause with its first letter capitalised, except for the acronyms and names in causeLabels.
func causeLabel(c engine.Cause) string {
	if l, ok := causeLabels[c]; ok {
		return l
	}
	s := string(c)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// rowKey identifies a row of the breakdown: the cause and, for growth and shocks of an account with an allocation, the market class.
type rowKey struct {
	cause engine.Cause
	class string
}

// bkRow is a row of the panel: one line of the engine's breakdown, labelled.
type bkRow struct {
	key               rowKey
	label             string
	actual, projected float64
}

func (r bkRow) total() float64 { return r.actual + r.projected }

// breakdownRows returns a row for each line of the result, in the engine's order. rec names the classes.
func breakdownRows(rec *model.Record, res *engine.BreakdownResult) []bkRow {
	rows := make([]bkRow, len(res.Lines))
	for i, l := range res.Lines {
		label := causeLabel(l.Cause)
		if l.Class != "" {
			name := l.Class
			if c := rec.Class(l.Class); c != nil && c.Name != "" {
				name = c.Name
			}
			label += ": " + name
		}
		rows[i] = bkRow{rowKey{l.Cause, l.Class}, label, l.Actual, l.Projected}
	}
	return rows
}

// followedLine returns the line the breakdown follows: the selected line, else the first drawn one.
func followedLine(l *graph.Lines) (graph.Line, bool) {
	if id, ok := l.Selected(); ok {
		if ln, ok := l.Get(id); ok {
			return ln, true
		}
	}
	for _, ln := range l.All() {
		if l.Drawn(ln.ID) {
			return ln, true
		}
	}
	return graph.Line{}, false
}

// bkShare is one member account's share of a cause.
type bkShare struct {
	name              string
	actual, projected float64
}

// bkMember is a member account of an asset with its own breakdown over the same window.
type bkMember struct {
	name string
	res  *engine.BreakdownResult
}

// memberShares returns the members that have a share of the row's cause, in member order.
func memberShares(members []bkMember, key rowKey) []bkShare {
	var out []bkShare
	for _, m := range members {
		for _, l := range m.res.Lines {
			if (rowKey{l.Cause, l.Class}) == key && (l.Actual != 0 || l.Projected != 0) {
				out = append(out, bkShare{m.name, l.Actual, l.Projected})
			}
		}
	}
	return out
}

// maxBreakdowns bounds the breakdown cache; following the cursor over many days starts it over rather than growing.
const maxBreakdowns = 256

type bkKey struct {
	proj     string
	asset    model.AssetID
	from, to model.Date
}

type bkAcctKey struct {
	proj  string
	asset model.AssetID
}

type bkAccount struct {
	id   model.AccountID
	name string
}

// bkEntry is the cached breakdown of one asset over one window and, once asked for, those of its member accounts.
type bkEntry struct {
	res     *engine.BreakdownResult
	err     error
	members []bkMember
	done    bool
}

// breakdownCache memoizes engine breakdowns so the engine does not run every frame. Like dataCache it belongs to one record and is replaced when the record is reloaded.
type breakdownCache struct {
	rec      *model.Record
	entries  map[bkKey]*bkEntry
	accounts map[bkAcctKey][]bkAccount
}

func newBreakdownCache(rec *model.Record) *breakdownCache {
	return &breakdownCache{rec: rec, entries: map[bkKey]*bkEntry{}, accounts: map[bkAcctKey][]bkAccount{}}
}

// get returns the asset's breakdown over (from, to] under the projection.
func (c *breakdownCache) get(k bkKey) *bkEntry {
	if e, ok := c.entries[k]; ok {
		return e
	}
	if len(c.entries) >= maxBreakdowns {
		clear(c.entries)
	}
	e := &bkEntry{}
	e.res, e.err = engine.Breakdown(c.rec, k.proj, k.asset, k.from, k.to)
	c.entries[k] = e
	return e
}

// memberAccounts returns the accounts under the asset, or nil when there is one or none: a single account has nothing to expand into.
func (c *breakdownCache) memberAccounts(proj string, asset model.AssetID) []bkAccount {
	k := bkAcctKey{proj, asset}
	if a, ok := c.accounts[k]; ok {
		return a
	}
	var out []bkAccount
	if view, err := c.rec.For(proj); err == nil {
		if res, err := view.Resolve(asset); err == nil && len(res.Accounts) > 1 {
			for _, id := range res.Accounts {
				name := string(id)
				if a := view.Account(id); a != nil {
					name = a.Name
				}
				out = append(out, bkAccount{id, name})
			}
		}
	}
	c.accounts[k] = out
	return out
}

// members returns the breakdown of each member account of the entry's asset over the same window, computed once per entry.
func (c *breakdownCache) members(k bkKey, e *bkEntry) []bkMember {
	if e.done {
		return e.members
	}
	e.done = true
	for _, a := range c.memberAccounts(k.proj, k.asset) {
		if res, err := engine.Breakdown(c.rec, k.proj, model.DefaultAssetID(a.id), k.from, k.to); err == nil {
			e.members = append(e.members, bkMember{a.name, res})
		}
	}
	return e.members
}
