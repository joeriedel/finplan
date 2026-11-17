// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
)

func TestPayrollDeductionsRoundTripAndFormat16ItemsLoadWithoutThem(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	mustSave(t, s, rec)
	got := mustLoad(t, s)
	if it, _ := got.Budget.Item("save-401k"); it.Paycheck != "salary-a" {
		t.Errorf("save-401k loaded deducted from %q, want salary-a", it.Paycheck)
	}
	if !reflect.DeepEqual(got.Budget, rec.Budget) {
		t.Fatal("the budget loaded differently")
	}

	// A document of format 16 has no paycheck: its items read as ordinary.
	old, _ := openTemp(t)
	want := demodata.Family()
	for i := range want.Budget.Items {
		want.Budget.Items[i].Paycheck = ""
	}
	mustSave(t, old, want)
	var withNew int
	if err := old.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE body LIKE '%"paycheck"%'`).Scan(&withNew); err != nil || withNew != 0 {
		t.Fatalf("documents with a paycheck: %d, %v", withNew, err)
	}
	if _, err := old.db.Exec(`UPDATE documents SET format = 16`); err != nil {
		t.Fatal(err)
	}
	loaded := mustLoad(t, old)
	if !reflect.DeepEqual(loaded.Budget, want.Budget) {
		t.Fatal("the format 16 budget loaded differently")
	}
	for _, it := range loaded.Budget.Items {
		if it.Paycheck != "" {
			t.Errorf("%s loaded deducted from %q", it.ID, it.Paycheck)
		}
	}
}
