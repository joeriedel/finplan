// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// The Add form offers a shock and a rebalance as kinds of their own, like a focal grant: neither is an object kind a modification stores.
const (
	kindShock     model.ObjectKind = "shock"
	kindRebalance model.ObjectKind = "rebalance"
)

// shockForm is a shock as the user enters it: the class is an id, the rest text as entered. The change is in percent.
type shockForm struct {
	class, date, percent string
}

// untouched reports whether nothing has been picked or typed in the form.
func (f shockForm) untouched() bool { return f.class == "" && allBlank(f.date, f.percent) }

// shock builds the shock the form describes: what the form cannot read is an error, and ops checks the rest.
func (f shockForm) shock() (model.Shock, error) {
	if f.class == "" {
		return model.Shock{}, errors.New("pick a market class")
	}
	d, err := needDate(f.date, "the date of the shock")
	if err != nil {
		return model.Shock{}, err
	}
	if strings.TrimSpace(f.percent) == "" {
		return model.Shock{}, errors.New("enter the change of the class in percent, such as -40")
	}
	v, err := parseValue(model.PropGrowth, model.ModeSet, f.percent)
	if err != nil {
		return model.Shock{}, err
	}
	return model.Shock{Class: f.class, Date: d, Change: v}, nil
}

// shockProblem says why the shock the form describes cannot be added to the projection, or "" if it can.
func shockProblem(rec *model.Record, f shockForm) string {
	s, err := f.shock()
	if err != nil {
		return err.Error()
	}
	return reasonOf(ops.CheckShock(rec, s))
}

// rebalanceForm is a rebalance as the user enters it: the account is an id, the date and the percent of each class (by class id; blank is 0) text as entered.
type rebalanceForm struct {
	account, date string
	percents      map[string]string
}

// untouched reports whether nothing has been picked or typed in the form.
func (f rebalanceForm) untouched() bool {
	if f.account != "" || !allBlank(f.date) {
		return false
	}
	for _, s := range f.percents {
		if !allBlank(s) {
			return false
		}
	}
	return true
}

// rebalance builds the rebalance the form describes with a share for each class of rec that has a percent: what the form cannot read is an error, and ops checks the rest.
func (f rebalanceForm) rebalance(rec *model.Record) (model.Rebalance, error) {
	if f.account == "" {
		return model.Rebalance{}, errors.New("pick an account")
	}
	d, err := needDate(f.date, "the date of the rebalance")
	if err != nil {
		return model.Rebalance{}, err
	}
	r := model.Rebalance{Account: model.AccountID(f.account), Date: d}
	for _, c := range rec.Classes {
		text := f.percents[c.ID]
		if strings.TrimSpace(text) == "" {
			continue
		}
		v, err := parseValue(model.PropGrowth, model.ModeSet, text)
		if err != nil {
			return model.Rebalance{}, fmt.Errorf("class %s: %w", c.Name, err)
		}
		r.Allocation = append(r.Allocation, model.Weight{Class: c.ID, Share: v})
	}
	if len(r.Allocation) == 0 {
		return model.Rebalance{}, errors.New("enter the percent of each class to hold, such as 60 and 40")
	}
	return r, nil
}

// rebalanceProblem says why the rebalance the form describes cannot be added to the projection, or "" if it can. rec is the record as the projection sees it.
func rebalanceProblem(rec *model.Record, f rebalanceForm) string {
	r, err := f.rebalance(rec)
	if err != nil {
		return err.Error()
	}
	return reasonOf(ops.CheckRebalance(rec, r))
}

// shockSummary describes the shock in a line for the list.
func shockSummary(rec *model.Record, s model.Shock) string {
	name := s.Class
	if c := rec.Class(s.Class); c != nil {
		name = c.Name
	}
	pct := trimFloat(math.Round(s.Change*100*1e9) / 1e9)
	if s.Change > 0 {
		pct = "+" + pct
	}
	return fmt.Sprintf("%s%% to %s on %s", pct, name, s.Date)
}

// rebalanceSummary describes the rebalance in a line for the list.
func rebalanceSummary(rec *model.Record, r model.Rebalance) string {
	name := string(r.Account)
	if a := rec.Account(r.Account); a != nil {
		name = a.Name
	}
	parts := make([]string, len(r.Allocation))
	for i, w := range r.Allocation {
		class := w.Class
		if c := rec.Class(w.Class); c != nil {
			class = c.Name
		}
		parts[i] = trimFloat(math.Round(w.Share*100*1e9)/1e9) + "% " + class
	}
	return fmt.Sprintf("%s to %s on %s", name, strings.Join(parts, ", "), r.Date)
}

// investableChoices lists the open accounts of rec that hold an allocation, which a rebalance trades.
func investableChoices(rec *model.Record) (ids, names []string) {
	for _, a := range rec.Accounts {
		if !a.IsClosed() && model.Investable(a.Type) && len(a.Allocation) > 0 {
			ids, names = append(ids, string(a.ID)), append(names, a.Name)
		}
	}
	return ids, names
}

// choose draws a combo that picks one of values, shown by names, into cur.
func (t *projTab) choose(id, label string, cur *string, values, names []string) {
	t.label(label)
	preview := "choose"
	if i := slices.Index(values, *cur); i >= 0 {
		preview = names[i]
	}
	if k := pick(id, preview, imgui.FontSize()*24, names, slices.Index(values, *cur)); k >= 0 {
		*cur = values[k]
	}
}

// textRow draws a labelled text field.
func (t *projTab) textRow(id, label, hint string, v *string) {
	t.label(label)
	imgui.SetNextItemWidth(imgui.FontSize() * 24)
	imgui.InputTextWithHint("##"+id, hint, v, 0, nil)
}

// shockFields draws the Add form for a shock of the projection.
func (t *projTab) shockFields(p *model.Projection) {
	rec := effective(t.a.rec, p.Name)
	f := &t.addShock
	var ids, names []string
	for _, c := range rec.Classes {
		ids, names = append(ids, c.ID), append(names, c.Name)
	}
	t.choose("shockclass", "Market class", &f.class, ids, names)
	t.textRow("shockdate", "Date", dateHint, &f.date)
	t.textRow("shockpercent", "Change (percent)", "e.g. -40", &f.percent)
	formButton("Add##addshock", shockProblem(rec, *f), f.untouched(), func() { t.addShockTo(p.Name) })
}

// addShockTo adds the shock the Add form describes to the projection and clears the date and change. It reports whether it did.
func (t *projTab) addShockTo(proj string) bool {
	if shockProblem(effective(t.a.rec, proj), t.addShock) != "" {
		return false
	}
	s, _ := t.addShock.shock()
	if !t.a.edit("add shock", func() (*ops.Result, error) {
		return ops.AddShock(t.a.st, t.a.rec, proj, s, ops.Options{})
	}) {
		return false
	}
	t.addShock.date, t.addShock.percent = "", ""
	return true
}

// rebalanceFields draws the Add form for a rebalance of the projection.
func (t *projTab) rebalanceFields(p *model.Projection) {
	rec := effective(t.a.rec, p.Name)
	f := &t.addRebalance
	if f.percents == nil {
		f.percents = map[string]string{}
	}
	ids, names := investableChoices(rec)
	t.choose("rebacct", "Account", &f.account, ids, names)
	t.textRow("rebdate", "Date", dateHint, &f.date)
	for _, c := range rec.Classes {
		text := f.percents[c.ID]
		t.textRow("rebpct"+c.ID, c.Name+" (percent)", "0", &text)
		f.percents[c.ID] = text
	}
	formButton("Add##addrebalance", rebalanceProblem(rec, *f), f.untouched(), func() { t.addRebalanceTo(p.Name) })
}

// addRebalanceTo adds the rebalance the Add form describes to the projection and clears the date and percents. It reports whether it did.
func (t *projTab) addRebalanceTo(proj string) bool {
	rec := effective(t.a.rec, proj)
	if rebalanceProblem(rec, t.addRebalance) != "" {
		return false
	}
	r, _ := t.addRebalance.rebalance(rec)
	if !t.a.edit("add rebalance", func() (*ops.Result, error) {
		return ops.AddRebalance(t.a.st, t.a.rec, proj, r, ops.Options{})
	}) {
		return false
	}
	t.addRebalance.date, t.addRebalance.percents = "", map[string]string{}
	return true
}

// marketRows lists the projection's shocks and rebalances, each with a button that removes it.
func (t *projTab) marketRows(p *model.Projection) {
	if len(p.Shocks) == 0 && len(p.Rebalances) == 0 {
		return
	}
	rec := effective(t.a.rec, p.Name)
	imgui.Spacing()
	imgui.TextUnformatted("Shocks and rebalances, on their dates; a shock comes before a rebalance on the same date.")
	flags := imgui.TableFlagsRowBg | imgui.TableFlagsBordersInnerV | imgui.TableFlagsSizingStretchProp
	if !imgui.BeginTableV("##markets", 3, flags, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	imgui.TableSetupColumnV("Kind", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*6, 0)
	imgui.TableSetupColumnV("What", 0, 2, 0)
	imgui.TableSetupColumnV("##removemarket", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*2, 0)
	imgui.TableHeadersRow()
	row := func(id, kind, what, edit string, remove func() (*ops.Result, error)) {
		imgui.PushIDStr(id)
		defer imgui.PopID()
		imgui.TableNextRowV(0, 0)
		imgui.TableSetColumnIndex(0)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(kind)
		imgui.TableSetColumnIndex(1)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(what)
		imgui.TableSetColumnIndex(2)
		if imgui.SmallButton("x") {
			t.a.edit(edit, remove)
		}
		if imgui.IsItemHovered() {
			tooltipText("Remove this " + kind)
		}
	}
	for i, s := range p.Shocks {
		row("shock"+strconv.Itoa(i), "shock", shockSummary(rec, s), "remove shock", func() (*ops.Result, error) {
			return ops.RemoveShock(t.a.st, t.a.rec, p.Name, i, ops.Options{})
		})
	}
	for i, r := range p.Rebalances {
		row("rebalance"+strconv.Itoa(i), "rebalance", rebalanceSummary(rec, r), "remove rebalance", func() (*ops.Result, error) {
			return ops.RemoveRebalance(t.a.st, t.a.rec, p.Name, i, ops.Options{})
		})
	}
}
