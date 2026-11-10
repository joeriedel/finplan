// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// effective is the record as the projection sees it, with the accounts its chain defines; rec itself if the chain cannot be resolved.
func effective(rec *model.Record, proj string) *model.Record {
	if view, err := rec.For(proj); err == nil {
		return view
	}
	return rec
}

// accountRow is one account a projection has of its own or inherits.
type accountRow struct {
	model.ProjAccount
	// Owner is the projection that defines the account; the selected projection's own accounts (Own) are editable.
	Owner string
	Own   bool
}

// accountRows lists the hypothetical accounts the projection sees: those its ancestors define, root first, then its own.
func accountRows(rec *model.Record, proj string) []accountRow {
	chain, err := rec.Chain(proj)
	if err != nil {
		return nil
	}
	var out []accountRow
	for _, p := range chain {
		for _, a := range p.Accounts {
			out = append(out, accountRow{ProjAccount: a, Owner: p.Name, Own: p.Name == proj})
		}
	}
	return out
}

// acctForm is the Add account form: every field is the text or choice as entered.
type acctForm struct {
	name, rate, opening, from string
	typ                       model.AccountType
	tax                       model.TaxTreatment
}

// set changes one field: name, type, tax, rate, opening or from.
func (f *acctForm) set(field, text string) {
	switch field {
	case "name":
		f.name = text
	case "type":
		f.typ = model.AccountType(text)
	case "tax":
		f.tax = model.TaxTreatment(text)
	case "rate":
		f.rate = text
	case "opening":
		f.opening = text
	case "from":
		f.from = text
	}
}

// account reads the form. A blank rate or opening balance is 0; the rate is a percentage.
func (f acctForm) account() (model.ProjAccount, error) {
	a := model.ProjAccount{Name: strings.TrimSpace(f.name), Type: f.typ, Tax: f.tax}
	var err error
	if a.StatedRate, err = acctRate(f.rate); err != nil {
		return a, err
	}
	if a.Opening, err = acctOpening(f.opening); err != nil {
		return a, err
	}
	if a.From, err = parseDate(strings.TrimSpace(f.from)); err != nil {
		return a, fmt.Errorf("the opening date: %w", err)
	}
	return a, nil
}

func acctRate(text string) (float64, error) {
	if strings.TrimSpace(text) == "" {
		return 0, nil
	}
	return parseValue(model.PropGrowth, model.ModeSet, text)
}

func acctOpening(text string) (float64, error) {
	if strings.TrimSpace(text) == "" {
		return 0, nil
	}
	return parseValue(model.PropAmount, model.ModeSet, text)
}

// acctProblem says why the account the form describes cannot be added to the projection, or "" if it can.
func acctProblem(st *store.Store, rec *model.Record, proj string, f acctForm) string {
	a, err := f.account()
	if err != nil {
		return err.Error()
	}
	if a.Type == "" {
		return "pick a type"
	}
	if a.Tax == "" {
		return "pick a tax treatment"
	}
	_, err = ops.AddProjAccount(st, rec, proj, a, ops.Options{DryRun: true})
	return reasonOf(err)
}

// acctChange reads the new text or choice of one field of an account being edited.
func acctChange(field, text string) (ops.ProjAccountChange, error) {
	var ch ops.ProjAccountChange
	var err error
	switch field {
	case "name":
		ch.Name = model.Ptr(text)
	case "type":
		ch.Type = model.Ptr(model.AccountType(text))
	case "tax":
		ch.Tax = model.Ptr(model.TaxTreatment(text))
	case "rate":
		var v float64
		if v, err = acctRate(text); err == nil {
			ch.StatedRate = &v
		}
	case "opening":
		var v float64
		if v, err = acctOpening(text); err == nil {
			ch.Opening = &v
		}
	case "from":
		var d model.Date
		if d, err = parseDate(text); err == nil {
			ch.From = &d
		} else {
			err = fmt.Errorf("the opening date: %w", err)
		}
	}
	return ch, err
}

// accounts draws the hypothetical accounts of the projection: its own, which can be changed, and those inherited.
func (t *projTab) accounts(p *model.Projection) {
	rec := t.a.rec
	imgui.SeparatorText("Accounts in this projection")
	imgui.TextWrapped("Hypothetical accounts exist only under this projection and the ones building on it. They are never part of the record, and aggregates such as Net worth include them.")
	rows := accountRows(rec, p.Name)
	em := imgui.FontSize()
	if len(rows) == 0 {
		imgui.TextDisabled("No accounts yet.")
	} else if imgui.BeginTableV("##accounts", 7, imgui.TableFlagsRowBg|imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		imgui.TableSetupColumnV("Name", 0, 2, 0)
		imgui.TableSetupColumnV("Type", imgui.TableColumnFlagsWidthFixed, em*8, 0)
		imgui.TableSetupColumnV("Tax", imgui.TableColumnFlagsWidthFixed, em*8, 0)
		imgui.TableSetupColumnV("Rate per year", imgui.TableColumnFlagsWidthFixed, em*7, 0)
		imgui.TableSetupColumnV("Opening balance", imgui.TableColumnFlagsWidthFixed, em*9, 0)
		imgui.TableSetupColumnV("Opens on", imgui.TableColumnFlagsWidthFixed, em*9, 0)
		imgui.TableSetupColumnV("##remove", imgui.TableColumnFlagsWidthFixed, em*2, 0)
		imgui.TableHeadersRow()
		for _, r := range rows {
			t.accountRow(p, r)
		}
		imgui.EndTable()
	}
	t.addAccountForm(p)
}

func (t *projTab) accountRow(p *model.Projection, r accountRow) {
	id := "acct" + string(r.ID)
	imgui.PushIDStr(id)
	defer imgui.PopID()
	imgui.TableNextRowV(0, 0)
	cell := func(i int) { imgui.TableSetColumnIndex(int32(i)) }
	text := func(i int, s string) {
		cell(i)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(s)
	}
	rate, opening := formatValue(model.PropGrowth, model.ModeSet, r.StatedRate), trimFloat(r.Opening)
	if !r.Own {
		text(0, r.Name+" (from "+r.Owner+")")
		text(1, string(r.Type))
		text(2, string(r.Tax))
		text(3, rate)
		text(4, opening)
		text(5, r.From.String())
		return
	}
	edit := func(i int, field, value string) {
		cell(i)
		if s, done := t.field(id+field, "", value, -1); done {
			t.commit(id+field, func() { t.editAccount(p.Name, r.ID, field, strings.TrimSpace(s)) })
		}
	}
	choose := func(i int, field, cur string, values []string) {
		cell(i)
		if k := pick(id+field, cur, -1, values, slices.Index(values, cur)); k >= 0 {
			t.editAccount(p.Name, r.ID, field, values[k])
		}
	}
	edit(0, "name", r.Name)
	choose(1, "type", string(r.Type), accountTypes)
	choose(2, "tax", string(r.Tax), taxTreatments)
	edit(3, "rate", rate)
	edit(4, "opening", opening)
	edit(5, "from", r.From.String())
	cell(6)
	if imgui.Button("x") {
		t.a.edit("remove account", func() (*ops.Result, error) {
			return ops.RemoveProjAccount(t.a.st, t.a.rec, p.Name, r.ID, ops.Options{})
		})
	}
}

// editAccount changes one field of an account the projection defines, through ops.EditProjAccount. A refusal is shown by the field.
func (t *projTab) editAccount(proj string, id model.AccountID, field, text string) {
	fid := "acct" + string(id) + field
	ch, err := acctChange(field, text)
	if err != nil {
		t.setErr(fid, err.Error())
		return
	}
	t.a.edit("change account", func() (*ops.Result, error) {
		return ops.EditProjAccount(t.a.st, t.a.rec, proj, id, ch, ops.Options{})
	})
}

func (t *projTab) addAccountForm(p *model.Projection) {
	em := imgui.FontSize()
	f := &t.addAcct
	input := func(field, label, hint, value string, width float32) {
		imgui.AlignTextToFramePadding()
		imgui.TextDisabled(label)
		imgui.SameLine()
		s := value
		imgui.SetNextItemWidth(width)
		if imgui.InputTextWithHint("##addacct"+field, hint, &s, 0, nil) {
			f.set(field, s)
		}
	}
	choose := func(field, label, cur string, values []string) {
		imgui.AlignTextToFramePadding()
		imgui.TextDisabled(label)
		imgui.SameLine()
		preview := "choose"
		if cur != "" {
			preview = cur
		}
		if k := pick("addacct"+field, preview, em*8, values, slices.Index(values, cur)); k >= 0 {
			f.set(field, values[k])
		}
	}
	imgui.Spacing()
	t.label("Add account")
	input("name", "name", "e.g. House", f.name, em*12)
	imgui.SameLine()
	choose("type", "type", string(f.typ), accountTypes)
	imgui.SameLine()
	choose("tax", "tax", string(f.tax), taxTreatments)
	t.label("")
	input("rate", "rate per year", "percent, e.g. 3", f.rate, em*6)
	imgui.SameLine()
	input("opening", "opening balance", "negative for a debt", f.opening, em*9)
	imgui.SameLine()
	input("from", "opens on", dateHint, f.from, em*8)
	t.label("")
	addButton(acctProblem(t.a.st, t.a.rec, p.Name, *f), func() {
		a, _ := f.account()
		if t.a.edit("add account", func() (*ops.Result, error) {
			return ops.AddProjAccount(t.a.st, t.a.rec, p.Name, a, ops.Options{})
		}) {
			t.addAcct = acctForm{}
		}
	})
}
