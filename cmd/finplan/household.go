// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
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
	}
	return ch, nil
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
			t.a.edit("remove household member", func() (*ops.Result, error) {
				return ops.RemoveHouseholdMember(t.a.st, t.a.rec, m.Name, ops.Options{})
			})
		}
		imgui.PopID()
	}

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
