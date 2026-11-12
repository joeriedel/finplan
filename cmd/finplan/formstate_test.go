// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestUntouchedForms(t *testing.T) {
	if !allBlank() || !allBlank("", "  ") || allBlank("", "x") {
		t.Error("allBlank wrong")
	}
	if !(newAcctForm{}).untouched() || (newAcctForm{name: "x"}).untouched() || (newAcctForm{rate: "3"}).untouched() || !(newAcctForm{typ: model.TypeEquity, rate: "3"}).untouched() {
		t.Error("newAcctForm.untouched wrong")
	}
	if !(entryDraft{kind: model.TxKind("x")}).untouched() || (entryDraft{amount: "5"}).untouched() || (entryDraft{memo: "m"}).untouched() {
		t.Error("entryDraft.untouched wrong")
	}
	if !(memberForm{}).untouched() || (memberForm{kind: memberRule}).untouched() {
		t.Error("memberForm.untouched wrong")
	}
	if !(memberForm{kind: memberAccount}).untouched() || !(memberForm{kind: memberAsset}).untouched() {
		t.Error("memberForm with a kind and no target should be untouched")
	}
	if (memberForm{kind: memberAccount, account: model.AccountID("x")}).untouched() || (memberForm{kind: memberAsset, asset: model.AssetID("x")}).untouched() {
		t.Error("memberForm with a target should be touched")
	}
	f := memberForm{kind: memberAsset, asset: model.AssetID("x")}
	if f = (memberForm{kind: f.kind}); !f.untouched() || f.kind != memberAsset {
		t.Error("memberForm after a reset should be untouched and keep its kind")
	}
}

func TestUntouchedProjectionForms(t *testing.T) {
	if !(acctForm{}).untouched() || !(acctForm{name: " "}).untouched() {
		t.Error("an empty acctForm should be untouched")
	}
	for _, f := range []acctForm{{name: "x"}, {rate: "3"}, {opening: "5"}, {from: "2030-01-01"}, {typ: model.AccountType("x")}, {tax: model.TaxTreatment("x")}} {
		if f.untouched() {
			t.Errorf("acctForm %+v should be touched", f)
		}
	}
	if !(settingForm{}).untouched() || (settingForm{asset: model.AssetID("x")}).untouched() || (settingForm{start: "2030-01-01"}).untouched() || (settingForm{override: 1}).untouched() {
		t.Error("settingForm.untouched wrong")
	}
	if !newItemForm().untouched() || (itemForm{name: "x"}).untouched() || (itemForm{amount: "5"}).untouched() || (itemForm{account: model.AccountID("x")}).untouched() {
		t.Error("itemForm.untouched wrong")
	}
	if !modUntouched("", " ", "", "") || modUntouched("x", "", "", "") || modUntouched("", "5", "", "") || modUntouched("", "", "2030-01-01", "") || modUntouched("", "", "", "2031-01-01") {
		t.Error("modUntouched wrong")
	}
}

func TestTypedInvalidShowsReason(t *testing.T) {
	a := demoApp(t)
	d := entryDraft{date: "soon"}
	if d.untouched() || txProblem(a.st, a.rec, demodata.Checking, d) == "" {
		t.Error("typed invalid draft should show a reason")
	}
}

func TestMoneyAndDateHints(t *testing.T) {
	if got := describeInterval(model.Interval{From: model.MinDate, Value: -600}); got != "always to no end: -$600" {
		t.Errorf("describeInterval = %q", got)
	}
	if got := formatDollars(29335); got != "$29,335" {
		t.Errorf("formatDollars = %q", got)
	}
	if strings.Contains(dateHint, "2006") {
		t.Error("dateHint shows the Go layout")
	}
	_, err := parseDate("x")
	if err == nil || strings.Contains(err.Error(), "2006") {
		t.Errorf("parseDate error = %v", err)
	}
}
