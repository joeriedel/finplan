// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/tax"
)

func TestSetAndRevertFederalTable(t *testing.T) {
	st, rec := seed(t)
	f, _ := rec.Tax.FederalYear(2025)
	f = f.Clone()
	f.Limit401k = 99000
	res, err := SetFederalTable(st, rec, f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := reload(t, st).Tax.FederalYear(2025); got.Limit401k != 99000 || !reflect.DeepEqual(got, *res.Federal) {
		t.Errorf("stored table %+v", got)
	}
	if orig, _ := rec.Tax.FederalYear(2025); orig.Limit401k == 99000 {
		t.Error("the input record was changed")
	}
	if _, err := SetFederalTable(st, res.Record, f, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("setting the same table: %v", err)
	}
	back, err := RevertFederalTable(st, res.Record, 2025, Options{})
	if err != nil {
		t.Fatal(err)
	}
	shipped, _ := tax.Defaults().FederalYear(2025)
	if got, _ := reload(t, st).Tax.FederalYear(2025); !reflect.DeepEqual(got, shipped) || !reflect.DeepEqual(back.Record.Tax, tax.Defaults()) {
		t.Error("revert did not restore the shipped table")
	}
	if _, err := RevertFederalTable(st, back.Record, 2025, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("reverting a shipped table: %v", err)
	}
	if _, err := RevertFederalTable(st, back.Record, 1999, Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("reverting a year with no shipped table: %v", err)
	}
}

func TestSetStateTable(t *testing.T) {
	st, rec := seed(t)
	flat := tax.State{Name: "FLAT", Year: 2025, Brackets: []tax.Bracket{{Over: 0, Rate: 0.05}}}
	res, err := SetStateTable(st, rec, flat, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reload(t, st).Tax.StateYear("FLAT", 2025); !ok || got.Brackets[0].Rate != 0.05 {
		t.Errorf("stored table %+v, %v", got, ok)
	}
	// A household can name the new state.
	if _, err := SetHousehold(st, res.Record, HouseholdChange{State: &flat.Name}, Options{}); err != nil {
		t.Errorf("naming the new state: %v", err)
	}
	if _, err := RevertStateTable(st, res.Record, "FLAT", 2025, Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("reverting a state with no shipped table: %v", err)
	}

	va, _ := rec.Tax.StateYear(tax.VA, 2025)
	va = va.Clone()
	va.Brackets[3].Rate = 0.06
	edited, err := SetStateTable(st, rec, va, Options{})
	if err != nil {
		t.Fatal(err)
	}
	back, err := RevertStateTable(st, edited.Record, tax.VA, 2025, Options{})
	if err != nil || !reflect.DeepEqual(back.Record.Tax, tax.Defaults()) {
		t.Errorf("revert: %v", err)
	}
}

func TestSetTableRefusals(t *testing.T) {
	st, rec := seed(t)
	f, _ := rec.Tax.FederalYear(2025)
	f = f.Clone()
	f.Status[tax.Single] = tax.FedStatus{}
	if _, err := SetFederalTable(st, rec, f, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("a federal table with an empty status: %v", err)
	}
	if _, err := SetStateTable(st, rec, tax.State{Name: "BAD", Year: 2025}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("a state table without brackets: %v", err)
	}
	if got := reload(t, st).Tax; !reflect.DeepEqual(got, tax.Defaults()) {
		t.Error("a refused edit changed the store")
	}
}

func TestCopyStateTables(t *testing.T) {
	st, rec := seed(t)
	res, err := CopyStateTables(st, rec, "VA", "COPY", Options{})
	if err != nil {
		t.Fatal(err)
	}
	stored := reload(t, st)
	for _, y := range []int{2024, 2025, 2026} {
		va, _ := stored.Tax.StateYear("VA", y)
		got, ok := stored.Tax.StateYear("COPY", y)
		va.Name = "COPY"
		if !ok || !reflect.DeepEqual(got, va) {
			t.Errorf("%d: copy %+v, %v", y, got, ok)
		}
	}
	if res.State == nil || res.State.Name != "COPY" || !stored.Tax.HasState("VA") {
		t.Errorf("result %+v", res.State)
	}
	// The copy owns its brackets.
	edited, _ := stored.Tax.StateYear("COPY", 2025)
	edited.Brackets[0].Rate = 0.5
	if va, _ := stored.Tax.StateYear("VA", 2025); va.Brackets[0].Rate == 0.5 {
		t.Error("the copy shares brackets with its source")
	}
	// A household can name the copy.
	name := "COPY"
	if _, err := SetHousehold(st, res.Record, HouseholdChange{State: &name}, Options{}); err != nil {
		t.Errorf("naming the copy: %v", err)
	}

	if _, err := CopyStateTables(st, res.Record, "VA", "COPY", Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("a name in use: %v", err)
	}
	if _, err := CopyStateTables(st, res.Record, "VA", "VA", Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("copying onto itself: %v", err)
	}
	if _, err := CopyStateTables(st, res.Record, "ZZ", "OTHER", Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("an unknown source: %v", err)
	}
	for _, bad := range []string{"", " X", "X "} {
		if _, err := CopyStateTables(st, res.Record, "VA", bad, Options{}); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("name %q: %v", bad, err)
		}
	}
	if _, err := CopyStateTables(st, rec, "VA", "DRY", Options{DryRun: true}); err != nil || reload(t, st).Tax.HasState("DRY") {
		t.Errorf("dry run: %v", err)
	}
}
