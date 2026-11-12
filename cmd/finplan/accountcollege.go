// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
)

// college draws the settings of a 529 account: the dependent it is for, and the contributions it holds as of its latest balance, which the earnings part of a non-qualified withdrawal is worked out from.
func (t *acctTab) college(acct *model.Account) {
	em := imgui.FontSize()
	id := acct.ID
	imgui.Spacing()
	imgui.SeparatorText("College savings")
	wrappedText("An expense on the Budget tab paid from this account is a qualified college cost: the account pays it while it has money and checking pays the rest. A transfer out of it to another kind of account is not qualified, and its earnings are taxed with the additional tax.")
	names := []string{"not set"}
	ids := []string{""}
	for _, m := range t.a.rec.Household.Members {
		if m.Role == model.RoleDependent {
			names, ids = append(names, m.Name), append(ids, m.Name)
		}
	}
	preview := "not set"
	if acct.Beneficiary != "" {
		preview = acct.Beneficiary
	}
	t.label("Beneficiary")
	if k := pick("beneficiary", preview, em*16, names, slices.Index(ids, acct.Beneficiary)); k >= 0 {
		t.editAttr(id, "beneficiary", ids[k])
	}
	basis := ""
	if acct.Basis != nil {
		basis = formatValue(model.PropAmount, model.ModeSet, *acct.Basis)
	}
	t.label("Contributions")
	if s, done := t.field(string(id)+"basis", "total contributed; blank for the balance", basis, em*10); done {
		t.editAttr(id, "basis", s)
	}
}
