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

// shockWith returns the shock with its date ("date") or change in percent ("percent") set to what text says; what it cannot read is an error and ops checks the rest.
func shockWith(s model.Shock, field, text string) (model.Shock, error) {
	switch field {
	case "date":
		d, err := needDate(text, "the date of the shock")
		if err != nil {
			return s, err
		}
		s.Date = d
	case "percent":
		v, err := parseValue(model.PropGrowth, model.ModeSet, text)
		if err != nil {
			return s, err
		}
		s.Change = v
	}
	return s, nil
}

// targetText writes the target allocation of the rebalance as it is edited and read back by parseTarget: "60% US large cap, 40% US bonds".
func targetText(rec *model.Record, r model.Rebalance) string {
	parts := make([]string, len(r.Allocation))
	for i, w := range r.Allocation {
		parts[i] = trimFloat(math.Round(w.Share*100*1e9)/1e9) + "% " + className(rec, w.Class)
	}
	return strings.Join(parts, ", ")
}

// parseTarget reads a target allocation typed as "60% US large cap, 40% US bonds": a percent and then a class by its name or id, separated by commas. The percents are checked together by ops, so the whole allocation is edited at once.
func parseTarget(rec *model.Record, text string) ([]model.Weight, error) {
	var out []model.Weight
	for _, part := range strings.Split(text, ",") {
		pct, name, _ := strings.Cut(strings.TrimSpace(part), " ")
		name = strings.TrimSpace(name)
		if pct == "" || name == "" {
			return nil, fmt.Errorf("%q is not a percent and a class, such as 60%% %s", strings.TrimSpace(part), className(rec, model.ClassLargeCap))
		}
		v, err := parseValue(model.PropGrowth, model.ModeSet, pct)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rec.Classes, func(c model.MarketClass) bool {
			return strings.EqualFold(c.Name, name) || strings.EqualFold(c.ID, name)
		})
		if i < 0 {
			return nil, fmt.Errorf("there is no market class %q", name)
		}
		if slices.ContainsFunc(out, func(w model.Weight) bool { return w.Class == rec.Classes[i].ID }) {
			return nil, fmt.Errorf("class %s is given twice", rec.Classes[i].Name)
		}
		out = append(out, model.Weight{Class: rec.Classes[i].ID, Share: v})
	}
	return out, nil
}

// rebalanceWith returns the rebalance with its date ("date") or target allocation ("allocation") set to what text says; what it cannot read is an error and ops checks the rest.
func rebalanceWith(rec *model.Record, r model.Rebalance, field, text string) (model.Rebalance, error) {
	switch field {
	case "date":
		d, err := needDate(text, "the date of the rebalance")
		if err != nil {
			return r, err
		}
		r.Date = d
	case "allocation":
		w, err := parseTarget(rec, text)
		if err != nil {
			return r, err
		}
		r.Allocation = w
	}
	return r, nil
}

// setShock replaces the projection's shock at index, through ops.SetShock. It reports whether it did.
func (t *projTab) setShock(proj string, index int, s model.Shock) bool {
	return t.a.edit("change shock", func() (*ops.Result, error) {
		return ops.SetShock(t.a.st, t.a.rec, proj, index, s, ops.Options{})
	})
}

// setRebalance replaces the projection's rebalance at index, through ops.SetRebalance. It reports whether it did.
func (t *projTab) setRebalance(proj string, index int, r model.Rebalance) bool {
	return t.a.edit("change rebalance", func() (*ops.Result, error) {
		return ops.SetRebalance(t.a.st, t.a.rec, proj, index, r, ops.Options{})
	})
}

// editShock changes one field of the projection's shock at index to the text typed; a refusal is shown by the field.
func (t *projTab) editShock(proj string, index int, field, text string) {
	id := "shock" + field + strconv.Itoa(index)
	s, err := shockWith(t.a.rec.Projection(proj).Shocks[index], field, text)
	if err != nil {
		t.setErr(id, err.Error())
		return
	}
	t.commit(id, func() { t.setShock(proj, index, s) })
}

// editRebalance changes one field of the projection's rebalance at index to the text typed; a refusal is shown by the field.
func (t *projTab) editRebalance(proj string, index int, field, text string) {
	id := "rebalance" + field + strconv.Itoa(index)
	r, err := rebalanceWith(effective(t.a.rec, proj), t.a.rec.Projection(proj).Rebalances[index], field, text)
	if err != nil {
		t.setErr(id, err.Error())
		return
	}
	t.commit(id, func() { t.setRebalance(proj, index, r) })
}

// marketRows lists the projection's shocks and rebalances, each edited in place (class or account, date, percent or target allocation) with a button that removes it.
func (t *projTab) marketRows(p *model.Projection) {
	if len(p.Shocks) == 0 && len(p.Rebalances) == 0 {
		return
	}
	rec := effective(t.a.rec, p.Name)
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.TextUnformatted("Shocks and rebalances, on their dates; a shock comes before a rebalance on the same date.")
	flags := imgui.TableFlagsRowBg | imgui.TableFlagsBordersInnerV | imgui.TableFlagsSizingStretchProp
	if !imgui.BeginTableV("##markets", 3, flags, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	imgui.TableSetupColumnV("Kind", imgui.TableColumnFlagsWidthFixed, em*6, 0)
	imgui.TableSetupColumnV("What", 0, 2, 0)
	imgui.TableSetupColumnV("##removemarket", imgui.TableColumnFlagsWidthFixed, em*2, 0)
	imgui.TableHeadersRow()
	row := func(id, kind, what, edit string, fields func(), remove func() (*ops.Result, error)) {
		imgui.PushIDStr(id)
		defer imgui.PopID()
		imgui.TableNextRowV(0, 0)
		imgui.TableSetColumnIndex(0)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(kind)
		imgui.TableSetColumnIndex(1)
		fields()
		imgui.TableSetColumnIndex(2)
		if imgui.SmallButton("x") {
			t.a.edit(edit, remove)
		}
		if imgui.IsItemHovered() {
			tooltipText("Remove this " + kind + ": " + what)
		}
	}
	classIDs, classNames := classChoices(rec)
	for i, s := range p.Shocks {
		n := strconv.Itoa(i)
		row("shock"+n, "shock", shockSummary(rec, s), "remove shock", func() {
			if k := pick("class", className(rec, s.Class), em*14, classNames, slices.Index(classIDs, s.Class)); k >= 0 {
				s.Class = classIDs[k]
				t.setShock(p.Name, i, s)
			}
			imgui.SameLine()
			if text, done := t.field("shockdate"+n, dateHint, s.Date.String(), em*9); done {
				t.editShock(p.Name, i, "date", text)
			}
			imgui.SameLine()
			if text, done := t.field("shockpercent"+n, "percent", formatPercentText(s.Change), em*6); done {
				t.editShock(p.Name, i, "percent", text)
			}
			imgui.SameLine()
			imgui.AlignTextToFramePadding()
			imgui.TextUnformatted("%")
		}, func() (*ops.Result, error) {
			return ops.RemoveShock(t.a.st, t.a.rec, p.Name, i, ops.Options{})
		})
	}
	ids, names := investableChoices(rec)
	for i, r := range p.Rebalances {
		n := strconv.Itoa(i)
		row("rebalance"+n, "rebalance", rebalanceSummary(rec, r), "remove rebalance", func() {
			acct := string(r.Account)
			if a := rec.Account(r.Account); a != nil {
				acct = a.Name
			}
			if k := pick("account", acct, em*14, names, slices.Index(ids, string(r.Account))); k >= 0 {
				r.Account = model.AccountID(ids[k])
				t.setRebalance(p.Name, i, r)
			}
			imgui.SameLine()
			if text, done := t.field("rebalancedate"+n, dateHint, r.Date.String(), em*9); done {
				t.editRebalance(p.Name, i, "date", text)
			}
			imgui.SameLine()
			if text, done := t.field("rebalanceallocation"+n, "e.g. 60% "+className(rec, model.ClassLargeCap)+", 40% "+className(rec, model.ClassBonds), targetText(rec, r), em*34); done {
				t.editRebalance(p.Name, i, "allocation", text)
			}
		}, func() (*ops.Result, error) {
			return ops.RemoveRebalance(t.a.st, t.a.rec, p.Name, i, ops.Options{})
		})
	}
}

// className is the name of the class, its id if the record has none like it.
func className(rec *model.Record, id string) string {
	if c := rec.Class(id); c != nil {
		return c.Name
	}
	return id
}

// classChoices lists the ids and names of the classes of rec.
func classChoices(rec *model.Record) (ids, names []string) {
	for _, c := range rec.Classes {
		ids, names = append(ids, c.ID), append(names, c.Name)
	}
	return ids, names
}
