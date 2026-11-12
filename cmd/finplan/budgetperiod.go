// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"slices"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// periodsShown is how many periods of each item the This period view keeps variance for: the current one and the five before it.
const periodsShown = 6

// periodState is the This period view's memo of the engine's answer, which is dropped whenever the record is replaced (every edit, undo and reload installs a new one), and the status picked and the amounts typed for each item, keyed by item id.
type periodState struct {
	rec    *model.Record
	rows   []engine.ItemPeriods
	err    string
	status map[string]model.OccurrenceStatus
	posted map[string]string
	expect map[string]string
}

var periodStatusNames = map[model.OccurrenceStatus]string{model.StatusAuto: "Auto", model.StatusPosted: "Posted", model.StatusPending: "Pending"}

// load computes the rows for the record's today, and starts the fields of each from what is recorded.
func (s *periodState) load(rec *model.Record) {
	s.rec, s.rows, s.err = rec, nil, ""
	s.status, s.posted, s.expect = map[string]model.OccurrenceStatus{}, map[string]string{}, map[string]string{}
	rows, err := engine.Periods(rec, "", rec.Today, periodsShown)
	if err != nil {
		s.err = err.Error()
		return
	}
	s.rows = rows
	for _, r := range rows {
		v := r.Current()
		s.status[r.Item.ID] = v.Status
		if v.Override.Posted != nil {
			s.posted[r.Item.ID] = trimFloat(*v.Override.Posted)
		}
		if v.Override.Expected != nil {
			s.expect[r.Item.ID] = trimFloat(*v.Override.Expected)
		}
	}
}

// parseAmount reads a typed amount; blank is not set.
func parseAmount(text string) (v *float64, err error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	x, err := parseValue(model.PropAmount, model.ModeSet, text)
	if err != nil {
		return nil, err
	}
	if x < 0 {
		return nil, errors.New("an amount cannot be negative")
	}
	return &x, nil
}

// periodChange is what the typed and picked values change in the item's current period, and why they cannot be set, if so.
func (s *periodState) periodChange(r engine.ItemPeriods) (ops.OccurrenceChange, string) {
	id, v := r.Item.ID, r.Current()
	var ch ops.OccurrenceChange
	if st := s.status[id]; st != v.Status {
		ch.Status = &st
	}
	for _, f := range []struct {
		name string
		text string
		cur  *float64
		out  **float64
	}{{"posted", s.posted[id], v.Override.Posted, &ch.Posted}, {"expected", s.expect[id], v.Override.Expected, &ch.Expected}} {
		x, err := parseAmount(f.text)
		if err != nil {
			return ch, f.name + ": " + err.Error()
		}
		if x != nil && (f.cur == nil || *f.cur != *x) {
			*f.out = x
		}
	}
	if ch == (ops.OccurrenceChange{}) {
		return ch, "nothing is changed"
	}
	return ch, ""
}

// signedDollars is a variance as a signed amount of dollars.
func signedDollars(v float64) string {
	s := formatDollars(v)
	if v > 0 && s != "0" {
		return "+" + s
	}
	return s
}

// overUnder says how far the period is over or under its budget.
func overUnder(v float64) string {
	s := formatDollars(max(v, -v))
	switch {
	case s == "0":
		return "on budget"
	case v > 0:
		return s + " over"
	}
	return s + " under"
}

// setPeriod records the values the user typed for the item's current period through ops.SetOccurrence. Occurrences are facts, which are never undone, only corrected or cleared.
func (t *budgetTab) setPeriod(r engine.ItemPeriods, ch ops.OccurrenceChange) bool {
	start := r.Current().Period.Start
	return t.a.addEntry("set budget period", func() (*ops.Result, error) {
		return ops.SetOccurrence(t.a.st, t.a.rec, r.Item.ID, start, t.a.rec.Today, ch, "", ops.Options{})
	})
}

// clearPeriod voids what was recorded for the item's current period through ops.ClearOccurrence.
func (t *budgetTab) clearPeriod(r engine.ItemPeriods) bool {
	start := r.Current().Period.Start
	return t.a.addEntry("clear budget period", func() (*ops.Result, error) {
		return ops.ClearOccurrence(t.a.st, t.a.rec, r.Item.ID, start, t.a.rec.Today, "", ops.Options{})
	})
}

// periods draws the This period view: each budget item's period that contains today, with what has posted, what remains and how far over or under budget it is.
func (t *budgetTab) periods() {
	a := t.a
	s := &t.period
	if s.rec != a.rec {
		s.load(a.rec)
	}
	imgui.TextUnformatted("Periods on " + a.rec.Today.String() + ". A period runs from one occurrence of an item to the day before the next.")
	disabledText("The projection applies only what remains of each period after its account's last recorded date: the expected amount, or the budgeted one, less what has posted. Grey is an estimate until you set an amount.")
	if s.err != "" {
		errorText(s.err)
		return
	}
	if len(s.rows) == 0 {
		disabledText("No budget item has a period today.")
		return
	}
	headers := []string{"Item", "Period", "Timing", "Budgeted", "Expected", "Posted", "Status", "Remaining", "Over or under", "Recent periods", ""}
	weights := []float32{2, 2.4, 0.9, 1.1, 1.3, 1.3, 1.2, 1.1, 1.3, 2.2, 2}
	em := imgui.FontSize()
	if !imgui.BeginTableV("##budgetperiods", int32(len(headers)), imgui.TableFlagsBordersInnerV|imgui.TableFlagsPadOuterX|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	for i, h := range headers {
		imgui.TableSetupColumnV(h, 0, weights[i], 0)
	}
	imgui.TableHeadersRow()
	for _, r := range s.rows {
		v := r.Current()
		id := r.Item.ID
		imgui.PushIDStr(id)
		imgui.TableNextRowV(0, 0)
		cell := func(i int, text string) {
			imgui.TableSetColumnIndex(int32(i))
			imgui.AlignTextToFramePadding()
			imgui.TextUnformatted(text)
		}
		cell(0, r.Item.Name)
		cell(1, v.Period.Start.String()+" to "+v.Period.End.String())
		cell(2, string(v.Timing))
		cell(3, formatDollars(v.Budgeted))

		imgui.TableSetColumnIndex(4)
		expected := s.expect[id]
		imgui.SetNextItemWidth(-1)
		if imgui.InputTextWithHint("##expected", formatDollars(v.Budgeted), &expected, 0, nil) {
			s.expect[id] = expected
		}
		tooltipOn("The amount expected for this period only, instead of the budgeted one")

		imgui.TableSetColumnIndex(5)
		posted := s.posted[id]
		hint := formatDollars(v.Posted)
		if v.PostedAuto {
			hint += " est."
		}
		imgui.SetNextItemWidth(-1)
		if imgui.InputTextWithHint("##posted", hint, &posted, 0, nil) {
			s.posted[id] = posted
		}
		tooltipOn("The amount posted in this period so far; it may be more than the budgeted amount")

		imgui.TableSetColumnIndex(6)
		var names, values []string
		for _, st := range model.OccurrenceStatuses {
			names, values = append(names, periodStatusNames[st]), append(values, string(st))
		}
		imgui.SetNextItemWidth(-1)
		if k := pick("status", periodStatusNames[s.status[id]], em*7, names, slices.Index(values, string(s.status[id]))); k >= 0 {
			s.status[id] = model.OccurrenceStatus(values[k])
		}
		tooltipOn("Posted: nothing more is applied. Pending: what remains is still to come, even for a lump whose date has passed")

		cell(7, formatDollars(v.Remaining))
		cell(8, overUnder(v.Variance))

		var recent []string
		for _, h := range r.Periods {
			recent = append(recent, signedDollars(h.Variance))
		}
		cell(9, strings.Join(recent, "  "))
		if imgui.IsItemHovered() {
			var lines []string
			for _, h := range r.Periods {
				lines = append(lines, h.Period.Start.String()+": "+overUnder(h.Variance))
			}
			tooltipText("Over or under budget, oldest first:\n" + strings.Join(lines, "\n"))
		}

		imgui.TableSetColumnIndex(10)
		ch, problem := s.periodChange(r)
		formButton("Set##period", problem, problem == "nothing is changed", func() { t.setPeriod(r, ch) })
		if v.Overridden {
			imgui.SameLine()
			if imgui.Button("Clear##period") {
				t.clearPeriod(r)
			}
			tooltipOn("Forget what was recorded for this period")
		}
		imgui.PopID()
	}
}
