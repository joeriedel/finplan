// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strconv"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// mortgage draws the terms of a mortgage account: the amount borrowed, the term, the first payment and the account that pays. With all four set the mortgage is amortized, and the level monthly payment they give is shown.
func (t *acctTab) mortgage(acct *model.Account) {
	em := imgui.FontSize()
	id := acct.ID
	imgui.Spacing()
	imgui.SeparatorText("Mortgage terms")
	wrappedText("With all four terms set, the forecast pays the mortgage off on a schedule: a level monthly payment from the paying account, split each month into interest and principal. The interest rate is the rate per year above.")
	original := ""
	if acct.OriginalAmount != 0 {
		original = formatValue(model.PropAmount, model.ModeSet, acct.OriginalAmount)
	}
	t.label("Original amount")
	if s, done := t.field(string(id)+"original", "amount borrowed", original, em*10); done {
		t.editAttr(id, "original", s)
	}
	term := ""
	if acct.TermMonths != 0 {
		term = strconv.Itoa(acct.TermMonths)
	}
	t.label("Term in months")
	if s, done := t.field(string(id)+"term", "e.g. 360", term, em*10); done {
		t.editAttr(id, "term", s)
	}
	first := ""
	if acct.FirstPayment != nil {
		first = acct.FirstPayment.String()
	}
	t.label("First payment")
	if s, done := t.field(string(id)+"firstpayment", dateHint, first, em*10); done {
		t.editAttr(id, "firstpayment", s)
	}
	ids, names := []string{""}, []string{"not set"}
	for _, o := range t.a.rec.Accounts {
		if o.ID != id && !o.IsClosed() {
			ids, names = append(ids, string(o.ID)), append(names, o.Name)
		}
	}
	t.label("Paid from")
	if k := pick("paidfrom", itemAccountPreview(t.a.rec, ids, names, acct.PaidFrom), em*16, names, slices.Index(ids, string(acct.PaidFrom))); k >= 0 {
		t.editAttr(id, "paidfrom", ids[k])
	}
	t.label("Monthly payment")
	imgui.AlignTextToFramePadding()
	if acct.Amortized() {
		imgui.TextUnformatted(formatDollars(acct.MonthlyPayment()))
	} else {
		disabledText("set all four terms to amortize")
	}
	t.label("")
	if imgui.Button("Clear terms##mortgage") {
		t.a.edit("clear mortgage terms", func() (*ops.Result, error) {
			return ops.EditAccount(t.a.st, t.a.rec, id, ops.AccountChange{ClearTerms: true}, ops.Options{})
		})
	}
}
