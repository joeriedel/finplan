// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/tax"
)

func stateTaxRow(t *testing.T, a *app, year int) string {
	t.Helper()
	return taxRowValue(t, taxViewOf(a.rec, demodata.ProjBase, year), "State tax (VA)")
}

func setStateTable(t *testing.T, a *app, s tax.State) {
	t.Helper()
	if !a.edit("edit tax table", func() (*ops.Result, error) { return stateSpec.save(a.st, a.rec, s) }) {
		t.Fatalf("the edit of %s %d was refused", s.Name, s.Year)
	}
}

// Scenario 1: editing a Virginia bracket changes the Taxes view's state tax, and Revert for the field, the bracket list and the table restores the shipped value and the numbers.
func TestTaxTableEditChangesTaxesAndReverts(t *testing.T) {
	a := demoApp(t)
	const year = 2026
	base := stateTaxRow(t, a, year)
	shipped, _ := tax.Defaults().StateYear("VA", year)
	va, _ := a.rec.Tax.StateYear("VA", year)

	top := len(va.Brackets) - 1
	edited := stateSpec.withBrackets(va, tax.MarriedJoint, bracketsWith(va.Brackets, top, va.Brackets[top].Over, 0.09))
	setStateTable(t, a, edited)
	changed := stateTaxRow(t, a, year)
	if changed == base {
		t.Fatalf("state tax stayed %s after the top bracket went to 9%%", base)
	}
	// The bracket list reverts as a whole.
	va, _ = a.rec.Tax.StateYear("VA", year)
	setStateTable(t, a, stateSpec.withBrackets(va, tax.MarriedJoint, shipped.Brackets))
	if got := stateTaxRow(t, a, year); got != base {
		t.Errorf("after the bracket revert %s, want %s", got, base)
	}

	// A field reverts by setting it back to the shipped value.
	exempt := stateSpec.fields[1]
	if exempt.id != "stateexempt" {
		t.Fatalf("field %q", exempt.id)
	}
	va, _ = a.rec.Tax.StateYear("VA", year)
	setStateTable(t, a, exempt.with(va, tax.MarriedJoint, 0))
	if got := stateTaxRow(t, a, year); got == base {
		t.Errorf("state tax stayed %s without the exemption", base)
	}
	va, _ = a.rec.Tax.StateYear("VA", year)
	setStateTable(t, a, exempt.with(va, tax.MarriedJoint, exempt.get(shipped, tax.MarriedJoint)))
	if got, _ := a.rec.Tax.StateYear("VA", year); !reflect.DeepEqual(got, shipped) || stateTaxRow(t, a, year) != base {
		t.Errorf("after the field revert %+v", got)
	}

	// The whole table reverts through the op.
	setStateTable(t, a, edited)
	if !a.edit("revert tax table", func() (*ops.Result, error) { return ops.RevertStateTable(a.st, a.rec, "VA", year, ops.Options{}) }) {
		t.Fatal("revert refused")
	}
	if got := stateTaxRow(t, a, year); got != base {
		t.Errorf("after the table revert %s, want %s", got, base)
	}

	// Each edit is one undo step.
	setStateTable(t, a, edited)
	a.undo()
	if got := stateTaxRow(t, a, year); got != base {
		t.Errorf("after undo %s, want %s", got, base)
	}
}

// Scenario 2: a copy of Virginia changed to a flat 4% and named in the household taxes by that rate.
func TestNewStateTableFlatRate(t *testing.T) {
	a := demoApp(t)
	const year = 2026
	base := stateTaxRow(t, a, year)
	if !a.edit("add state tax table", func() (*ops.Result, error) {
		return ops.CopyStateTables(a.st, a.rec, "VA", "FLAT", ops.Options{})
	}) {
		t.Fatal("copy refused")
	}
	for _, y := range taxTableYears(a.rec, taxTableSel{state: "FLAT"}) {
		s, _ := a.rec.Tax.StateYear("FLAT", y)
		setStateTable(t, a, stateSpec.withBrackets(s, tax.MarriedJoint, []tax.Bracket{{Over: 0, Rate: 0.04}}))
	}
	if !a.edit("change household", func() (*ops.Result, error) {
		return ops.SetHousehold(a.st, a.rec, ops.HouseholdChange{State: model.Ptr("FLAT")}, ops.Options{})
	}) {
		t.Fatal("household refused")
	}
	ty, err := engine.TaxYearOf(a.rec, demodata.ProjBase, year)
	if err != nil {
		t.Fatal(err)
	}
	if want := 0.04 * ty.Result.StateTaxable; ty.Result.StateTax == 0 || math.Abs(ty.Result.StateTax-want) > 0.01 {
		t.Errorf("state tax %v, want %v", ty.Result.StateTax, want)
	}
	if got := taxRowValue(t, taxViewOf(a.rec, demodata.ProjBase, year), "State tax (FLAT)"); got == base {
		t.Errorf("state tax stayed %s", base)
	}
}

func TestTaxTableText(t *testing.T) {
	for _, c := range []struct {
		rate float64
		text string
	}{{0.0575, "5.75"}, {0.02, "2"}, {0, "0"}, {0.37, "37"}, {0.0145, "1.45"}} {
		if got := formatPercentText(c.rate); got != c.text {
			t.Errorf("format %v = %q, want %q", c.rate, got, c.text)
		}
		for _, text := range []string{c.text, c.text + "%", " " + c.text + " % "} {
			if got, err := parsePercentText(text); err != nil || got != c.rate {
				t.Errorf("parse %q = %v, %v, want %v", text, got, err, c.rate)
			}
		}
	}
	if v, err := parseMoneyText("$1,234.50"); err != nil || v != 1234.5 {
		t.Errorf("money %v, %v", v, err)
	}
	for _, bad := range []string{"", "x", "NaN", "Inf"} {
		if _, err := parseMoneyText(bad); err == nil {
			t.Errorf("money %q was accepted", bad)
		}
		if _, err := parsePercentText(bad); err == nil {
			t.Errorf("percent %q was accepted", bad)
		}
	}
	if got := bracketsText([]tax.Bracket{{Over: 0, Rate: 0.02}, {Over: 3000, Rate: 0.0575}}); got != "$0 at 2%, $3,000 at 5.75%" {
		t.Errorf("brackets text %q", got)
	}
}

func TestTaxTableBrackets(t *testing.T) {
	bs := []tax.Bracket{{Over: 0, Rate: 0.1}, {Over: 100, Rate: 0.2}}
	if got := bracketsAdded(bs, tax.Bracket{Over: 50, Rate: 0.15}); !reflect.DeepEqual(got, []tax.Bracket{{Over: 0, Rate: 0.1}, {Over: 50, Rate: 0.15}, {Over: 100, Rate: 0.2}}) {
		t.Errorf("added %v", got)
	}
	if got := bracketsWithout(bs, 1); !reflect.DeepEqual(got, bs[:1]) || len(bs) != 2 {
		t.Errorf("without %v, input %v", got, bs)
	}
	if got := bracketsWith(bs, 1, 200, 0.3); got[1] != (tax.Bracket{Over: 200, Rate: 0.3}) || bs[1].Over != 100 {
		t.Errorf("with %v, input %v", got, bs)
	}
	va, _ := tax.Defaults().StateYear("VA", 2026)
	if _, problem := addedBracket(stateSpec, va, tax.Single, "x", "5"); problem == "" {
		t.Error("a threshold that is not a number was accepted")
	}
	if _, problem := addedBracket(stateSpec, va, tax.Single, formatMoneyText(va.Brackets[1].Over), "5"); problem == "" {
		t.Error("a repeated threshold was accepted")
	}
	next, problem := addedBracket(stateSpec, va, tax.Single, "1000000", "6")
	if problem != "" || len(next.Brackets) != len(va.Brackets)+1 || len(va.Brackets) != 4 {
		t.Errorf("added: %q %+v", problem, next.Brackets)
	}
}

func TestTaxTableSelection(t *testing.T) {
	a := demoApp(t)
	s := taxTableSel{}.resolved(a.rec, 2026)
	if s.federal || s.state != "VA" || s.year != 2026 || s.status != tax.MarriedJoint {
		t.Errorf("default %+v", s)
	}
	// A year the table lacks falls to the nearest it has.
	if s = (taxTableSel{state: "VA", year: 1999}).resolved(a.rec, 2040); s.year != 2026 {
		t.Errorf("year %d", s.year)
	}
	if s = (taxTableSel{federal: true}).resolved(a.rec, 2025); !s.federal || s.year != 2025 {
		t.Errorf("federal %+v", s)
	}
	// A state the record lacks falls back to the household's.
	if s = (taxTableSel{state: "ZZ"}).resolved(a.rec, 2025); s.state != "VA" {
		t.Errorf("state %+v", s)
	}
}

func TestTaxFieldsReadAndWrite(t *testing.T) {
	fed, _ := tax.Defaults().FederalYear(2025)
	for _, f := range fedSpec.fields {
		for _, st := range tax.FilingStatuses {
			before := fed.Clone()
			next := f.with(fed, st, 12345)
			if got := f.get(next, st); got != 12345 {
				t.Errorf("%s %s reads %v", f.id, st, got)
			}
			if !reflect.DeepEqual(fed, before) {
				t.Errorf("%s changed the table it was given", f.id)
			}
			if back := f.with(next, st, f.get(fed, st)); !reflect.DeepEqual(back, fed) {
				t.Errorf("%s did not restore the table", f.id)
			}
		}
	}
	va, _ := tax.Defaults().StateYear("VA", 2025)
	for _, f := range stateSpec.fields {
		next := f.with(va, tax.Single, 777)
		if f.get(next, tax.Single) != 777 {
			t.Errorf("%s reads %v", f.id, f.get(next, tax.Single))
		}
		if back := f.with(next, tax.Single, f.get(va, tax.Single)); !reflect.DeepEqual(back, va) {
			t.Errorf("%s did not restore the table", f.id)
		}
	}
	for _, f := range stateSpec.fields {
		if strings.TrimSpace(f.label) == "" {
			t.Errorf("%s has no label", f.id)
		}
	}
}
