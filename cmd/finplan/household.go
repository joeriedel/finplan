// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
	"github.com/joeriedel/finplan/internal/tax"
)

// householdForm is the Add member form of the household editor: every field is the text or choice as entered.
type householdForm struct {
	name, birth string
	role        model.Role
}

// set changes one field: name, birth or role.
func (f *householdForm) set(field, text string) {
	switch field {
	case "name":
		f.name = text
	case "birth":
		f.birth = text
	case "role":
		f.role = model.Role(text)
	}
}

// member reads the form; ops checks the rest.
func (f householdForm) member() (model.HouseholdMember, error) {
	m := model.HouseholdMember{Name: strings.TrimSpace(f.name), Role: f.role}
	if strings.TrimSpace(f.birth) == "" {
		return m, errors.New("enter a birth date")
	}
	var err error
	m.Birth, err = parseDate(strings.TrimSpace(f.birth))
	return m, err
}

// householdProblem says why the member the form describes cannot be added, or "" if it can.
func householdProblem(st *store.Store, rec *model.Record, f householdForm) string {
	if strings.TrimSpace(f.name) == "" {
		return "enter a name"
	}
	m, err := f.member()
	if err != nil {
		return err.Error()
	}
	if m.Role == "" {
		return "pick a role"
	}
	_, err = ops.AddHouseholdMember(st, rec, m, ops.Options{DryRun: true})
	return reasonOf(err)
}

// memberChange reads the new text of one attribute of a member being edited: name, birth date, or the choice of role.
func memberChange(field, text string) (ops.MemberChange, error) {
	var ch ops.MemberChange
	switch field {
	case "name":
		ch.Name = model.Ptr(text)
	case "birth":
		d, err := parseDate(text)
		if err != nil {
			return ch, err
		}
		ch.Birth = &d
	case "role":
		ch.Role = model.Ptr(model.Role(text))
	case "ssbenefit", "high3":
		v := 0.0
		if text != "" {
			var err error
			if v, err = parseMoneyText(text); err != nil {
				return ch, err
			}
		}
		if field == "ssbenefit" {
			ch.SSBenefit = &v
		} else {
			ch.FERSHigh3 = &v
		}
	case "ssclaim", "years", "sep", "start":
		n := 0
		if text != "" {
			var err error
			if n, err = strconv.Atoi(text); err != nil {
				return ch, fmt.Errorf("%q is not a whole number", text)
			}
		}
		switch field {
		case "ssclaim":
			ch.SSClaimAge = &n
		case "years":
			ch.FERSYears = &n
		case "sep":
			ch.FERSSeparationAge = &n
		default:
			ch.FERSStartAge = &n
		}
	case "survivor":
		ch.FERSSurvivor = model.Ptr(model.Survivor(text))
	}
	return ch, nil
}

// intText is the text of a whole-number field: empty for zero.
func intText(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// moneyText is the text of an amount field: empty for zero.
func moneyText(v float64) string {
	if v == 0 {
		return ""
	}
	return formatMoneyText(v)
}

// fersForm is the form that gives a member without a FERS annuity one: every field is the text or choice as entered. The inputs are checked together, so they are submitted as one change.
type fersForm struct {
	high3, years, sep, start string
	survivor                 model.Survivor
}

// set changes one field: high3, years, sep, start or survivor.
func (f *fersForm) set(field, text string) {
	switch field {
	case "high3":
		f.high3 = text
	case "years":
		f.years = text
	case "sep":
		f.sep = text
	case "start":
		f.start = text
	case "survivor":
		f.survivor = model.Survivor(text)
	}
}

// change reads the form as the change that gives the member the annuity; ops checks the rest.
func (f fersForm) change() (ops.MemberChange, error) {
	var first error
	read := func(field, text string) ops.MemberChange {
		ch, err := memberChange(field, strings.TrimSpace(text))
		if err != nil && first == nil {
			first = err
		}
		return ch
	}
	ch := ops.MemberChange{
		FERSHigh3:         read("high3", f.high3).FERSHigh3,
		FERSYears:         read("years", f.years).FERSYears,
		FERSSeparationAge: read("sep", f.sep).FERSSeparationAge,
		FERSStartAge:      read("start", f.start).FERSStartAge,
		FERSSurvivor:      model.Ptr(f.survivor),
	}
	if f.survivor == "" {
		ch.FERSSurvivor = model.Ptr(model.SurvivorNone)
	}
	return ch, first
}

// fersProblem says why the member cannot be given the annuity the form describes, or "" if it can.
func fersProblem(st *store.Store, rec *model.Record, name string, f fersForm) string {
	ch, err := f.change()
	if err != nil {
		return err.Error()
	}
	if ch.FERSHigh3 == nil || *ch.FERSHigh3 <= 0 {
		return "enter the high-3 salary"
	}
	_, err = ops.EditHouseholdMember(st, rec, name, ch, ops.Options{DryRun: true})
	return reasonOf(err)
}

// removeFERSChange is the change that takes the FERS annuity off a member.
func removeFERSChange() ops.MemberChange {
	return ops.MemberChange{
		FERSHigh3: model.Ptr(0.0), FERSYears: model.Ptr(0), FERSSeparationAge: model.Ptr(0), FERSStartAge: model.Ptr(0),
		FERSSurvivor: model.Ptr(model.SurvivorNone),
	}
}

// survivorLabels are the display names of model.Survivors, in the same order.
var survivorLabels = []string{"None", "Partial (5%)", "Full (10%)"}

// benefits draws the collapsible Benefits section of member i, which is open for this frame if force: the Social Security and FERS inputs, each edited through ops.EditHouseholdMember.
func (t *acctTab) benefits(i int, m model.HouseholdMember, force bool) {
	em := imgui.FontSize()
	n := strconv.Itoa(i)
	if force {
		imgui.SetNextItemOpenV(true, imgui.CondAlways)
	}
	if !imgui.CollapsingHeaderTreeNodeFlags("Benefits##hhbenefits") {
		return
	}
	imgui.PushIDStr("benefits")
	defer imgui.PopID()
	row := func(label, field, hint, value, note string) {
		t.label(label)
		if s, done := t.field("hh"+field+n, hint, value, em*8); done {
			t.editMember(i, field, s)
		}
		if note != "" {
			imgui.SameLine()
			disabledText(note)
		}
	}
	row("Social Security", "ssbenefit", "none", moneyText(m.SSBenefit), "a month at full retirement age, today's dollars")
	row("Claim age", "ssclaim", "62-70", intText(m.SSClaimAge), "")
	if m.HasFERS() {
		row("FERS high-3", "high3", "none", moneyText(m.FERSHigh3), "a year, today's dollars")
		row("Years of service", "years", "", intText(m.FERSYears), "")
		row("Separation age", "sep", "", intText(m.FERSSeparationAge), "")
		row("Annuity start age", "start", "", intText(m.FERSStartAge), "")
		t.label("Survivor option")
		survivor := m.FERSSurvivor
		if survivor == "" {
			survivor = model.SurvivorNone
		}
		if k := pick("hhsurvivor"+n, survivorLabels[slices.Index(model.Survivors, survivor)], em*10, survivorLabels, slices.Index(model.Survivors, survivor)); k >= 0 {
			t.editMember(i, "survivor", string(model.Survivors[k]))
		}
		if e := t.errs["hhsurvivor"+n]; e != "" {
			errorBeside("Not changed: " + e)
		}
		if imgui.Button("Remove FERS##hhremovefers") {
			t.changeMember(m.Name, "remove FERS", removeFERSChange())
		}
	} else {
		t.addFERS(m)
	}
	if m.SSBenefit > 0 || m.HasFERS() {
		var parts []string
		if m.SSBenefit > 0 {
			parts = append(parts, formatDollars(m.SSBenefit)+" a month of Social Security at "+strconv.Itoa(m.SSClaimAge))
		}
		if m.HasFERS() {
			parts = append(parts, "FERS on a high-3 of "+formatDollars(m.FERSHigh3))
		}
		disabledText(strings.Join(parts, ", "))
	}
}

// addFERS draws the form that gives the member a FERS annuity.
func (t *acctTab) addFERS(m model.HouseholdMember) {
	em := imgui.FontSize()
	f := t.fers[m.Name]
	imgui.AlignTextToFramePadding()
	disabledText("FERS")
	for _, in := range []struct {
		field, hint string
		text        *string
		width       float32
	}{
		{"high3", "high-3 a year", &f.high3, em * 8},
		{"years", "years", &f.years, em * 4},
		{"sep", "separation age", &f.sep, em * 8},
		{"start", "start age", &f.start, em * 6},
	} {
		imgui.SameLine()
		s := *in.text
		imgui.SetNextItemWidth(in.width)
		if imgui.InputTextWithHint("##hhfers"+in.field, in.hint, &s, 0, nil) {
			f.set(in.field, s)
		}
	}
	survivor := f.survivor
	if survivor == "" {
		survivor = model.SurvivorNone
	}
	if k := pick("hhfserssurvivor", survivorLabels[slices.Index(model.Survivors, survivor)], em*10, survivorLabels, slices.Index(model.Survivors, survivor)); k >= 0 {
		f.set("survivor", string(model.Survivors[k]))
	}
	if t.fers == nil {
		t.fers = map[string]fersForm{}
	}
	t.fers[m.Name] = f
	imgui.SameLine()
	if t.fersCheck == nil {
		t.fersCheck = map[string]*problemCache{}
	}
	if t.fersCheck[m.Name] == nil {
		t.fersCheck[m.Name] = &problemCache{}
	}
	formButton("Add FERS##hhaddfers", t.fersCheck[m.Name].get(problemKey(t.a.rec, m.Name, f), func() string { return fersProblem(t.a.st, t.a.rec, m.Name, f) }), f.untouched(), func() {
		ch, _ := f.change()
		if t.changeMember(m.Name, "add FERS", ch) {
			delete(t.fers, m.Name)
			delete(t.fersCheck, m.Name)
		}
	})
}

// changeMember applies the change to the member through ops.EditHouseholdMember as one undoable edit. It reports whether it did.
func (t *acctTab) changeMember(name, what string, ch ops.MemberChange) bool {
	return t.a.edit(what, func() (*ops.Result, error) {
		return ops.EditHouseholdMember(t.a.st, t.a.rec, name, ch, ops.Options{})
	})
}

// stateNames lists the names of the state tax tables the record has, in the order of its tables.
func stateNames(rec *model.Record) []string {
	var names []string
	for _, s := range rec.Tax.State {
		if !slices.Contains(names, s.Name) {
			names = append(names, s.Name)
		}
	}
	return names
}

// checkingChoices lists what the reconciliation account picker offers: the first entry, with the empty id, stands for the first checking account; the others are the open checking accounts.
func checkingChoices(rec *model.Record) (ids, names []string) {
	ids, names = []string{""}, []string{"first checking account"}
	for _, a := range rec.Accounts {
		if a.Type == model.TypeChecking && !a.IsClosed() {
			ids, names = append(ids, string(a.ID)), append(names, a.Name)
		}
	}
	return ids, names
}

// setHousehold changes the household's settings through ops.SetHousehold.
func (t *acctTab) setHousehold(ch ops.HouseholdChange) {
	t.a.edit("change household", func() (*ops.Result, error) {
		return ops.SetHousehold(t.a.st, t.a.rec, ch, ops.Options{})
	})
}

// editMember changes one attribute of the member through ops.EditHouseholdMember; a refusal is shown by the field.
func (t *acctTab) editMember(i int, field, text string) {
	fid := "hh" + field + strconv.Itoa(i)
	ch, err := memberChange(field, strings.TrimSpace(text))
	if err != nil {
		t.errs[fid] = err.Error()
		return
	}
	name := t.a.rec.Household.Members[i].Name
	if !t.a.edit("change household member", func() (*ops.Result, error) {
		return ops.EditHouseholdMember(t.a.st, t.a.rec, name, ch, ops.Options{})
	}) && t.a.unchanged {
		delete(t.bufs, fid)
		delete(t.errs, fid)
	}
}

// household draws the household editor under the Add account form.
func (t *acctTab) household() {
	a := t.a
	h := a.rec.Household
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.SeparatorText("Household")
	if t.scrollHousehold {
		t.scrollHousehold = false
		imgui.SetScrollHereYV(0)
	}
	wrappedText("Who is on the tax return and how they file. The filing status, the dependents and the state table decide the household's tax.")

	t.label("Filing status")
	if k := pick("hhfiling", filingName(h.Filing), em*16, filingLabels, slices.Index(filingStatuses, string(h.Filing))); k >= 0 {
		t.setHousehold(ops.HouseholdChange{Filing: model.Ptr(tax.FilingStatus(filingStatuses[k]))})
	}
	t.label("State tax")
	states := stateNames(a.rec)
	if k := pick("hhstate", h.State, em*16, states, slices.Index(states, h.State)); k >= 0 {
		t.setHousehold(ops.HouseholdChange{State: model.Ptr(states[k])})
	}
	t.label("Reconciles in")
	ids, names := checkingChoices(a.rec)
	preview := names[0]
	if h.Checking != "" {
		preview = accountName(a.rec, h.Checking)
	}
	if k := pick("hhchecking", preview, em*16, names, slices.Index(ids, string(h.Checking))); k >= 0 {
		t.setHousehold(ops.HouseholdChange{Checking: model.Ptr(model.AccountID(ids[k]))})
	}

	imgui.Spacing()
	if len(h.Members) == 0 {
		disabledText("No members yet.")
	}
	for i, m := range h.Members {
		n := strconv.Itoa(i)
		imgui.PushIDStr("hhmember" + n)
		if s, done := t.field("hhname"+n, "", m.Name, em*10); done {
			t.editMember(i, "name", s)
		}
		imgui.SameLine()
		if s, done := t.field("hhbirth"+n, dateHint, m.Birth.String(), em*8); done {
			t.editMember(i, "birth", s)
		}
		imgui.SameLine()
		if k := pick("hhrole"+n, roleName(m.Role), em*8, roleLabels, slices.Index(householdRoles, string(m.Role))); k >= 0 {
			t.editMember(i, "role", householdRoles[k])
		}
		imgui.SameLine()
		if imgui.Button("x##hhremove") {
			plan, err := ops.HouseholdMemberRemovePlan(t.a.rec, m.Name)
			switch {
			case err != nil:
				t.a.notifyFailed("remove household member", err)
			case len(plan.Affected) > 0:
				t.hhDelName = m.Name
				t.hhDel.ask(fmt.Sprintf("household member %q", m.Name), plan)
			default:
				t.a.edit("remove household member", func() (*ops.Result, error) {
					return ops.RemoveHouseholdMember(t.a.st, t.a.rec, m.Name, ops.Options{})
				})
			}
		}
		if m.Role != model.RoleDependent {
			t.benefits(i, m, t.openBenefits)
		}
		imgui.PopID()
	}
	t.openBenefits = false
	imgui.PushIDStr("hhdel")
	if t.hhDel.draw(false, "") == deleteGo {
		name := t.hhDelName
		t.a.edit("remove household member", func() (*ops.Result, error) {
			return ops.RemoveHouseholdMember(t.a.st, t.a.rec, name, ops.Options{})
		})
	}
	imgui.PopID()

	f := &t.hh
	imgui.AlignTextToFramePadding()
	disabledText("add")
	imgui.SameLine()
	s := f.name
	imgui.SetNextItemWidth(em * 10)
	if imgui.InputTextWithHint("##hhnewname", "name", &s, 0, nil) {
		f.set("name", s)
	}
	imgui.SameLine()
	s = f.birth
	imgui.SetNextItemWidth(em * 8)
	if imgui.InputTextWithHint("##hhnewbirth", dateHint, &s, 0, nil) {
		f.set("birth", s)
	}
	imgui.SameLine()
	preview = "role"
	if f.role != "" {
		preview = roleName(f.role)
	}
	if k := pick("hhnewrole", preview, em*8, roleLabels, slices.Index(householdRoles, string(f.role))); k >= 0 {
		f.set("role", householdRoles[k])
	}
	formButton("Add##hhmember", t.hhCheck.get(problemKey(a.rec, "", *f), func() string { return householdProblem(a.st, a.rec, *f) }), f.untouched(), func() {
		m, _ := f.member()
		if a.edit("add household member", func() (*ops.Result, error) {
			return ops.AddHouseholdMember(a.st, a.rec, m, ops.Options{})
		}) {
			t.hh = householdForm{}
		}
	})
}
