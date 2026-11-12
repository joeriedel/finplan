// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/joeriedel/finplan/internal/model"
)

func TestProblemCacheRunsOncePerKey(t *testing.T) {
	var c problemCache
	runs := 0
	problem := func() string { runs++; return "no" }
	rec, other := model.NewRecord(0), model.NewRecord(0)
	form := entryDraft{date: "2030-01-01", amount: "-5", kind: model.TxExpense}

	for range 5 {
		if got := c.get(problemKey(rec, "checking", form), problem); got != "no" {
			t.Fatalf("problem = %q", got)
		}
	}
	if runs != 1 {
		t.Fatalf("the problem ran %d times for one key", runs)
	}
	form.amount = "-6"
	c.get(problemKey(rec, "checking", form), problem)
	c.get(problemKey(rec, "checking", form), problem)
	if runs != 2 {
		t.Fatalf("the problem ran %d times after a field changed, want 2", runs)
	}
	c.get(problemKey(other, "checking", form), problem)
	c.get(problemKey(other, "checking", form), problem)
	if runs != 3 {
		t.Fatalf("the problem ran %d times after the record was replaced, want 3", runs)
	}
}

func TestProblemKeyChangesWithEveryTransactionField(t *testing.T) {
	rec := model.NewRecord(0)
	base := entryDraft{date: "2030-01-01", amount: "-5", memo: "m", note: "n", kind: model.TxTransfer, counter: "savings"}
	key := problemKey(rec, "checking", base)
	if problemKey(rec, "checking", base) != key {
		t.Fatal("the key is not stable")
	}
	changed := map[string]string{
		"date":    problemKey(rec, "checking", entryDraft{"2030-01-02", "-5", "m", "n", model.TxTransfer, "savings"}),
		"amount":  problemKey(rec, "checking", entryDraft{"2030-01-01", "-6", "m", "n", model.TxTransfer, "savings"}),
		"memo":    problemKey(rec, "checking", entryDraft{"2030-01-01", "-5", "x", "n", model.TxTransfer, "savings"}),
		"note":    problemKey(rec, "checking", entryDraft{"2030-01-01", "-5", "m", "x", model.TxTransfer, "savings"}),
		"kind":    problemKey(rec, "checking", entryDraft{"2030-01-01", "-5", "m", "n", model.TxExpense, "savings"}),
		"counter": problemKey(rec, "checking", entryDraft{"2030-01-01", "-5", "m", "n", model.TxTransfer, "card"}),
		"account": problemKey(rec, "savings", base),
		"record":  problemKey(model.NewRecord(0), "checking", base),
	}
	for what, k := range changed {
		if k == key {
			t.Errorf("the key does not change with the %s", what)
		}
	}
}
