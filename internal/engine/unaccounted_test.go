// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// An income or expense transaction that matches no budget item is the unaccounted cause, not income or expense, and the lines still total the change.
func TestBreakdownMovesUnmatchedTransactionsToUnaccounted(t *testing.T) {
	rec := fixture(t)
	must(t, rec.Account("chk").AddTransaction(model.Transaction{Date: d(2025, time.December, 18), Amount: -35, Kind: model.TxExpense, Memo: "Pharmacy"}))
	must(t, rec.Account("chk").AddTransaction(model.Transaction{Date: d(2025, time.December, 19), Amount: 12, Kind: model.TxIncome, Memo: "Refund"}))
	rec.Account("chk").AddSnapshot(d(2025, time.December, 31), 1000-23)
	bd, err := Breakdown(rec, "", model.DefaultAssetID("chk"), d(2025, time.December, 1), d(2025, time.December, 31))
	must(t, err)
	if l := line(bd, CauseUnaccounted); !near(l.Actual, -23) {
		t.Errorf("unaccounted %+v, want -23", l)
	}
	if l := line(bd, CauseExpense); !near(l.Actual, -200) {
		t.Errorf("expense %+v, want only the rent, -200", l)
	}
	if l := line(bd, CauseIncome); !near(l.Actual, 500) {
		t.Errorf("income %+v, want only the salary, 500", l)
	}
	if !near(bd.Total(), bd.End-bd.Start) {
		t.Errorf("the lines total %v, the change is %v", bd.Total(), bd.End-bd.Start)
	}
	if l := line(bd, CauseDefault); !near(l.Actual+l.Projected, 0) {
		t.Errorf("a default line of %+v", l)
	}
}
