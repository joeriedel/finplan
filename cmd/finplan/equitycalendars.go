// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// Rule kinds of a ruleDraft: a fixed day, or the 1st to 4th or the last weekday of the month.
const (
	ruleFixed = 0
	ruleLast  = 5
)

var (
	ruleKindLabels = []string{"fixed day", "1st", "2nd", "3rd", "4th", "last"}
	monthLabels    = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	weekdayLabels  = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
)

// ruleDraft is the vest rule of one month as entered: kind is ruleFixed with a day, or the number of the weekday in the month (1 to 4, or ruleLast) with a weekday.
type ruleDraft struct {
	month   time.Month
	kind    int
	day     string
	weekday time.Weekday
}

// draftOfRule is the draft of a saved vest rule.
func draftOfRule(r model.VestRule) ruleDraft {
	d := ruleDraft{month: r.Month, weekday: r.Weekday}
	switch {
	case r.Nth == 0:
		d.day = strconv.Itoa(r.Day)
	case r.Nth == model.NthLast:
		d.kind = ruleLast
	default:
		d.kind = r.Nth
	}
	return d
}

// rule reads the draft as a vest rule.
func (d ruleDraft) rule() (model.VestRule, error) {
	r := model.VestRule{Month: d.month}
	switch d.kind {
	case ruleFixed:
		day, err := strconv.Atoi(strings.TrimSpace(d.day))
		if err != nil {
			return r, fmt.Errorf("%v: the day of the month is a whole number", d.month)
		}
		r.Day = day
	case ruleLast:
		r.Nth, r.Weekday = model.NthLast, d.weekday
	default:
		r.Nth, r.Weekday = d.kind, d.weekday
	}
	return r, nil
}

// calDraft is a vest calendar as entered: its rules and the overrides that move generated dates. A new calendar also has a name.
type calDraft struct {
	isNew     bool
	name      string
	rules     []ruleDraft
	overrides []model.DateOverride
}

// newCalDraft is the draft of a calendar not yet made: no name and one empty rule.
func newCalDraft() calDraft {
	return calDraft{isNew: true, rules: []ruleDraft{{month: time.January}}}
}

// draftOfCalendar is the draft of a saved vest calendar.
func draftOfCalendar(c *model.VestCalendar) calDraft {
	d := calDraft{name: c.Name, overrides: slices.Clone(c.Overrides)}
	for _, r := range c.Rules {
		d.rules = append(d.rules, draftOfRule(r))
	}
	return d
}

// untouched reports whether nothing has been typed or chosen for a new calendar.
func (d calDraft) untouched() bool {
	return allBlank(d.name) && len(d.rules) == 1 && d.rules[0] == (ruleDraft{month: time.January})
}

// calendar reads the draft as a vest calendar; its overrides are in date order.
func (d calDraft) calendar() (model.VestCalendar, error) {
	c := model.VestCalendar{Name: strings.TrimSpace(d.name)}
	for _, rd := range d.rules {
		r, err := rd.rule()
		if err != nil {
			return c, err
		}
		c.Rules = append(c.Rules, r)
	}
	c.Overrides = slices.Clone(d.overrides)
	// An override of a date the rules no longer generate is dropped, so that changing a rule does not leave one behind that cannot be seen.
	probe := model.VestCalendar{Rules: c.Rules}
	c.Overrides = slices.DeleteFunc(c.Overrides, func(o model.DateOverride) bool { return len(probe.Dates(o.From, o.From)) != 1 })
	slices.SortFunc(c.Overrides, func(a, b model.DateOverride) int { return int(a.From - b.From) })
	return c, nil
}

// calendarProblem says why the draft cannot be saved as a new calendar or as the changed one, or "" if it can.
func calendarProblem(st *store.Store, rec *model.Record, d calDraft) string {
	c, err := d.calendar()
	if err != nil {
		return err.Error()
	}
	if d.isNew {
		if c.Name == "" {
			return "enter a name"
		}
		_, err = ops.NewCalendar(st, rec, c, ops.Options{DryRun: true})
	} else {
		_, err = ops.EditCalendar(st, rec, c.Name, c.Rules, c.Overrides, ops.Options{DryRun: true})
	}
	return noChangeReason(err)
}

// ruleSummary names the days a rule falls on, such as "day 15" or "3rd Monday".
func ruleSummary(r model.VestRule) string {
	if r.Nth == 0 {
		return fmt.Sprintf("day %d", r.Day)
	}
	kind := r.Nth
	if r.Nth == model.NthLast {
		kind = ruleLast
	}
	return ruleKindLabels[kind] + " " + r.Weekday.String()
}

// nextRuleDate is the first date the rule generates on or after today, before any override. The rule must be valid.
func nextRuleDate(r model.VestRule, today model.Date) model.Date {
	y, _, _ := today.YMD()
	if d := r.On(y); d >= today {
		return d
	}
	return r.On(y + 1)
}

// ruleHint describes the draft rule and its next date on or after today, or is "" while the rule is not valid.
func ruleHint(d ruleDraft, today model.Date) string {
	r, err := d.rule()
	if err != nil {
		return ""
	}
	probe := model.VestCalendar{Name: "probe", Rules: []model.VestRule{r}}
	if probe.Validate() != nil {
		return ""
	}
	return ruleSummary(r) + " \u00b7 next: " + nextRuleDate(r, today).String()
}

// moveOverride returns the overrides with the vest now on cur moved to the date to, in date order. A date that is already the target of an override is moved by updating that override, and moving a date back to the one the rules generate removes the override.
func moveOverride(overrides []model.DateOverride, cur, to model.Date) []model.DateOverride {
	from := cur
	for _, o := range overrides {
		if o.To == cur {
			from = o.From
		}
	}
	out := removeOverride(overrides, from)
	if to != from {
		out = append(out, model.DateOverride{From: from, To: to})
	}
	slices.SortFunc(out, func(a, b model.DateOverride) int { return int(a.From - b.From) })
	return out
}

// removeOverride returns the overrides without the one of the generated date from.
func removeOverride(overrides []model.DateOverride, from model.Date) []model.DateOverride {
	return slices.DeleteFunc(slices.Clone(overrides), func(o model.DateOverride) bool { return o.From == from })
}

// calendarsSection draws the vest calendar editor: the picker of calendars, the rules with their next dates, the moved dates, and save and delete.
func (t *acctTab) calendarsSection(acct *model.Account) {
	u, a := &t.eq, t.a
	em := imgui.FontSize()
	imgui.Spacing()
	if !u.section("Vest calendars", "calendars") {
		return
	}
	wrappedText("A calendar says on which dates an employer's grants vest: one rule for each vest month, a fixed day or the Nth weekday. A date it generates can be moved from a grant's schedule. Grants share the calendar they name.")

	cal := a.rec.Calendar(u.cal)
	if !u.calNew && cal == nil && len(a.rec.Calendars) > 0 {
		u.cal, u.calBase = a.rec.Calendars[0].Name, nil
		cal = a.rec.Calendars[0]
	}
	if u.calNew && !u.calDraft.isNew {
		u.calDraft = newCalDraft()
	}
	if !u.calNew && cal != nil && (u.calBase == nil || !reflect.DeepEqual(u.calBase, cal)) {
		u.calDraft, u.calBase = draftOfCalendar(cal), cal.Clone()
	}

	t.label("Calendar")
	labels := []string{"(new calendar)"}
	sel := 0
	for i, c := range a.rec.Calendars {
		labels = append(labels, c.Name)
		if !u.calNew && c.Name == u.cal {
			sel = i + 1
		}
	}
	preview := "(new calendar)"
	if !u.calNew {
		preview = u.cal
	}
	if k := pick("calpick", preview, em*16, labels, sel); k >= 0 {
		u.calBase = nil
		if k == 0 {
			u.calNew = true
		} else {
			u.calNew, u.cal = false, labels[k]
		}
	}
	d := &u.calDraft
	if d.isNew {
		t.label("Name")
		imgui.SetNextItemWidth(em * 16)
		imgui.InputTextWithHint("##calname", "e.g. Acme vests", &d.name, 0, nil)
	}
	t.ruleTable(d)
	if imgui.Button("Add rule") {
		d.rules = append(d.rules, ruleDraft{month: time.January})
	}
	t.movedDates(d)

	t.label("")
	problem := calendarProblem(a.st, a.rec, *d)
	formButton("Save calendar", problem, problem == noChangeText || d.isNew && d.untouched(), func() {
		c, _ := d.calendar()
		if d.isNew {
			if a.edit("add vest calendar", func() (*ops.Result, error) { return ops.NewCalendar(a.st, a.rec, c, ops.Options{}) }) {
				u.calNew, u.cal, u.calBase = false, c.Name, nil
			}
			return
		}
		a.edit("edit vest calendar", func() (*ops.Result, error) {
			return ops.EditCalendar(a.st, a.rec, c.Name, c.Rules, c.Overrides, ops.Options{})
		})
	})
	if cal != nil && !d.isNew {
		imgui.SameLine()
		_, err := ops.DeleteCalendar(a.st, a.rec, cal.Name, ops.Options{DryRun: true})
		actionButton("Delete calendar", reasonOf(err), func() {
			a.edit("delete vest calendar", func() (*ops.Result, error) { return ops.DeleteCalendar(a.st, a.rec, cal.Name, ops.Options{}) })
		})
	}
}

// ruleTable draws one row for each rule of the draft: month, how the day is chosen, the day or weekday and a button that removes the rule.
func (t *acctTab) ruleTable(d *calDraft) {
	em := imgui.FontSize()
	if !imgui.BeginTableV("##rules", 5, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	for _, h := range []string{"Month", "Day", "On", "", "Next"} {
		imgui.TableSetupColumnV(h, 0, 1, 0)
	}
	imgui.TableHeadersRow()
	remove := -1
	for i := range d.rules {
		r := &d.rules[i]
		imgui.PushIDInt(int32(i))
		imgui.TableNextRowV(0, 0)
		imgui.TableSetColumnIndex(0)
		if k := pick("rmonth", r.month.String(), -1, monthLabels, int(r.month)-1); k >= 0 {
			r.month = time.Month(k + 1)
		}
		imgui.TableSetColumnIndex(1)
		if k := pick("rkind", ruleKindLabels[r.kind], -1, ruleKindLabels, r.kind); k >= 0 {
			r.kind = k
		}
		imgui.TableSetColumnIndex(2)
		if r.kind == ruleFixed {
			imgui.SetNextItemWidth(em * 5)
			imgui.InputTextWithHint("##rday", "1-28", &r.day, 0, nil)
		} else if k := pick("rweekday", r.weekday.String(), -1, weekdayLabels, int(r.weekday)); k >= 0 {
			r.weekday = time.Weekday(k)
		}
		imgui.TableSetColumnIndex(3)
		if imgui.Button("x") {
			remove = i
		}
		imgui.TableSetColumnIndex(4)
		disabledText(ruleHint(*r, t.a.rec.Today))
		imgui.PopID()
	}
	if remove >= 0 {
		d.rules = slices.Delete(d.rules, remove, remove+1)
	}
}

// movedDates draws the dates the draft moves, each as "from -> to" with a button that restores the generated date once the calendar is saved.
func (t *acctTab) movedDates(d *calDraft) {
	c, err := d.calendar()
	if err != nil || len(c.Overrides) == 0 {
		return
	}
	imgui.Spacing()
	imgui.TextUnformatted("Moved dates")
	remove := model.Date(0)
	for _, o := range c.Overrides {
		imgui.PushIDInt(int32(o.From))
		imgui.TextUnformatted(o.From.String() + " -> " + o.To.String())
		imgui.SameLine()
		if imgui.Button("Remove") {
			remove = o.From
		}
		imgui.PopID()
	}
	if remove != 0 {
		d.overrides = removeOverride(d.overrides, remove)
	}
}
