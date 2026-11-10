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
	if !(newAcctForm{}).untouched() || (newAcctForm{name: "x"}).untouched() || (newAcctForm{rate: "3"}).untouched() {
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
