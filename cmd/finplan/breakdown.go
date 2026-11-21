// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"sync"

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
	key     bkKey
	res     *engine.BreakdownResult
	err     error
	members []bkMember
	done    bool
}

// bkSlot and bkMembersSlot are the bgRunner slots of the breakdown of the line the panel follows and of its members: the panel shows one of each at a time, so a new ask replaces the one before it that has not started.
type (
	bkSlot        struct{}
	bkMembersSlot struct{}
)

// breakdownCache memoizes engine breakdowns so the engine does not run every frame, and runs the engine in the background: the panel asks for a breakdown that is not cached and shows it in a later frame. Like dataCache it belongs to one record and is replaced, and closed, when the record is reloaded.
type breakdownCache struct {
	rec  *model.Record
	snap recCopy
	run  *bgRunner

	mu      sync.Mutex
	entries map[bkKey]*bkEntry
	// want is the key last asked for and wanted whether one was; shown is the entry last returned, which the panel keeps showing while another is computed; wantMembers is the entry whose members were last asked for.
	want        bkKey
	wanted      bool
	shown       *bkEntry
	wantMembers *bkEntry
	closed      bool
	// lands counts the entries and member sets that landed, for the loop to tell whether it has redrawn for the latest.
	lands uint64
	// accounts is read and written on the UI thread only.
	accounts map[bkAcctKey][]bkAccount
}

func newBreakdownCache(rec *model.Record) *breakdownCache {
	return &breakdownCache{rec: rec, snap: recCopy{src: rec}, run: newBGRunner(), entries: map[bkKey]*bkEntry{}, accounts: map[bkAcctKey][]bkAccount{}}
}

// get returns the asset's breakdown over (from, to] under the projection and whether it is that one: if it is not cached it is asked for, and the entry returned is the one shown last for the same asset and projection, nil if none.
func (c *breakdownCache) get(k bkKey) (*bkEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[k]; ok {
		c.shown = e
		return e, true
	}
	if !c.closed && !(c.wanted && c.want == k) {
		c.want, c.wanted = k, true
		rec := c.snap.get()
		c.run.submit(bkSlot{}, func() { c.compute(rec, k) })
	}
	if e := c.shown; e != nil && e.key.proj == k.proj && e.key.asset == k.asset {
		return e, false
	}
	return nil, false
}

// compute is the job of get.
func (c *breakdownCache) compute(rec *model.Record, k bkKey) {
	c.mu.Lock()
	skip := c.closed || !c.wanted || c.want != k
	c.mu.Unlock()
	if skip {
		return
	}
	e := &bkEntry{key: k}
	e.res, e.err = engine.Breakdown(rec, k.proj, k.asset, k.from, k.to)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.storeLocked(e)
}

// storeLocked adds the entry to the cache. A full cache starts over but keeps the wanted entry: get does not ask again for a key it has asked for, so an older job landing after the wanted one must not evict it.
func (c *breakdownCache) storeLocked(e *bkEntry) {
	if len(c.entries) >= maxBreakdowns {
		w, kept := c.entries[c.want]
		clear(c.entries)
		if kept && c.wanted {
			c.entries[c.want] = w
		}
	}
	c.entries[e.key] = e
	c.lands++
}

// landCount returns the number of results that have landed.
func (c *breakdownCache) landCount() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lands
}

// busy reports whether a job is queued or running.
func (c *breakdownCache) busy() bool { return c.run.busy() }

// close keeps the running jobs from landing: the cache is being replaced.
func (c *breakdownCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
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

// members returns the breakdown of each member account of the entry's asset over the same window, and whether they are known: they are asked for the first time and computed in the background, once per entry.
func (c *breakdownCache) members(e *bkEntry) ([]bkMember, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.done {
		return e.members, true
	}
	if !c.closed && c.wantMembers != e {
		c.wantMembers = e
		rec := c.snap.get()
		accounts := c.memberAccounts(e.key.proj, e.key.asset)
		c.run.submit(bkMembersSlot{}, func() { c.computeMembers(rec, e, accounts) })
	}
	return nil, false
}

// computeMembers is the job of members.
func (c *breakdownCache) computeMembers(rec *model.Record, e *bkEntry, accounts []bkAccount) {
	c.mu.Lock()
	skip := c.closed || c.wantMembers != e
	c.mu.Unlock()
	if skip {
		return
	}
	var members []bkMember
	for _, a := range accounts {
		if res, err := engine.Breakdown(rec, e.key.proj, model.DefaultAssetID(a.id), e.key.from, e.key.to); err == nil {
			members = append(members, bkMember{a.name, res})
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		e.members, e.done = members, true
		c.lands++
	}
}
