// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"
	"testing"
)

// matchRecord is a checking account with the given transactions and one budget item per argument.
func matchRecord(t *testing.T, items []BudgetItem, txs ...Transaction) *Record {
	t.Helper()
	rec := NewRecord(D(2026, 6, 30))
	a := &Account{ID: "checking", Name: "Checking", Type: TypeChecking}
	if err := rec.AddAccount(a); err != nil {
		t.Fatal(err)
	}
	for _, tx := range txs {
		if err := a.AddTransaction(tx); err != nil {
			t.Fatal(err)
		}
	}
	rec.Budget = Budget{Items: items}
	return rec
}

func rent(amount float64) BudgetItem {
	return BudgetItem{ID: "rent", Name: "Rent", Kind: ItemExpense, Amount: Constant(amount), Recurrence: Recurrence{Kind: Monthly, Day: 1}, Account: "checking", Timing: Lump}
}

func expense(day int, amount float64, memo string) Transaction {
	return Transaction{Date: D(2026, 3, day), Amount: -amount, Kind: TxExpense, Memo: memo}
}

func matchedDates(rec *Record, item string) []Date {
	var out []Date
	for _, m := range rec.Matches().Item(item) {
		out = append(out, m.Transaction.Date)
	}
	return out
}

func TestMatchLumpWithinTheWindow(t *testing.T) {
	// The occurrence is 1 March: 8 days away is outside the window, 7 is inside.
	rec := matchRecord(t, []BudgetItem{rent(1000)}, expense(9, 1000, "Rent"))
	if got := matchedDates(rec, "rent"); len(got) != 0 {
		t.Errorf("a payment 8 days after the due date matched: %v", got)
	}
	rec = matchRecord(t, []BudgetItem{rent(1000)}, expense(8, 1000, "Rent"))
	if got := matchedDates(rec, "rent"); !reflect.DeepEqual(got, []Date{D(2026, 3, 8)}) {
		t.Errorf("a payment 7 days after the due date: %v", got)
	}
	// 7 days before the 1 April occurrence is 25 March.
	rec = matchRecord(t, []BudgetItem{rent(1000)}, expense(25, 1000, "Rent"))
	if got := matchedDates(rec, "rent"); !reflect.DeepEqual(got, []Date{D(2026, 3, 25)}) {
		t.Errorf("a payment 7 days before the due date: %v", got)
	}
}

func TestMatchLumpTakesTheNearestAndOnePerOccurrence(t *testing.T) {
	rec := matchRecord(t, []BudgetItem{rent(1000)}, expense(2, 1000, "Rent"), expense(5, 1000, "Rent"), expense(3, 1000, "Rent"))
	if got := matchedDates(rec, "rent"); !reflect.DeepEqual(got, []Date{D(2026, 3, 2)}) {
		t.Errorf("matched %v, want only the transaction nearest 1 March", got)
	}
	if _, ok := rec.Matches().Of(TxRef{"checking", 1}); ok {
		t.Error("a transaction that lost to a nearer one matches")
	}
}

func TestMatchLumpTieIsBrokenByAmount(t *testing.T) {
	// The payments of 27 February and 3 March are both 2 days from the occurrence of 1 March.
	rec := matchRecord(t, []BudgetItem{rent(1000)},
		Transaction{Date: D(2026, 2, 27), Amount: -1300, Kind: TxExpense, Memo: "Rent"},
		Transaction{Date: D(2026, 3, 3), Amount: -1010, Kind: TxExpense, Memo: "Rent"})
	var got []Date
	for _, m := range rec.Matches().Item("rent") {
		if m.Period == D(2026, 3, 1) {
			got = append(got, m.Transaction.Date)
		}
	}
	if !reflect.DeepEqual(got, []Date{D(2026, 3, 3)}) {
		t.Errorf("the occurrence of 1 March matched %v, want the payment closer in amount", got)
	}
}

func TestMatchEachTransactionServesOneOccurrence(t *testing.T) {
	// Weekly occurrences are 7 days apart: one payment in the middle lies in two windows and matches only one.
	weekly := rent(100)
	weekly.Recurrence = Recurrence{Kind: Weekly, Anchor: D(2026, 3, 1)}
	rec := matchRecord(t, []BudgetItem{weekly}, expense(4, 100, "Rent"))
	if n := len(rec.Matches().Item("rent")); n != 1 {
		t.Errorf("one payment matched %d occurrences", n)
	}
}

func TestMatchIsCaseInsensitiveAndNeedsTheAccount(t *testing.T) {
	rec := matchRecord(t, []BudgetItem{rent(1000)}, expense(1, 1000, "rENT"), expense(1, 5, "Rent and more"))
	if got := matchedDates(rec, "rent"); len(got) != 1 {
		t.Errorf("matched %v", got)
	}
	other := rent(1000)
	other.Account = "savings"
	rec = matchRecord(t, []BudgetItem{other}, expense(1, 1000, "Rent"))
	if got := matchedDates(rec, "rent"); len(got) != 0 {
		t.Errorf("a transaction of another account matched: %v", got)
	}
}

func TestMatchLinkBeatsMemo(t *testing.T) {
	water := BudgetItem{ID: "water", Name: "Water", Kind: ItemExpense, Amount: Constant(50), Recurrence: Recurrence{Kind: Monthly, Day: 1}, Account: "checking", Timing: Lump}
	rec := matchRecord(t, []BudgetItem{rent(1000), water},
		Transaction{Date: D(2026, 3, 1), Amount: -50, Kind: TxExpense, Memo: "Landlord", Item: "water"},
		Transaction{Date: D(2026, 3, 1), Amount: -1000, Kind: TxExpense, Memo: "Rent", Item: "water"})
	if got := rec.Matches().Item("water"); len(got) != 1 || got[0].Transaction.Memo != "Landlord" {
		t.Errorf("water matched %+v", got)
	}
	// The payment linked to water and named Rent never matches rent by its memo.
	if got := rec.Matches().Item("rent"); len(got) != 0 {
		t.Errorf("a transaction linked to another item matched rent: %+v", got)
	}
	// A link to an item the record lacks matches nothing.
	rec = matchRecord(t, []BudgetItem{rent(1000)}, Transaction{Date: D(2026, 3, 1), Amount: -1000, Kind: TxExpense, Memo: "Rent", Item: "gone"})
	if got := rec.Matches().Item("rent"); len(got) != 0 {
		t.Errorf("matched %+v", got)
	}
}

func TestMatchSpreadCollectsThePeriod(t *testing.T) {
	food := BudgetItem{ID: "food", Name: "Groceries", Kind: ItemExpense, Amount: Constant(600), Recurrence: Recurrence{Kind: Monthly, Day: 1}, Account: "checking", Timing: Spread}
	rec := matchRecord(t, []BudgetItem{food}, expense(2, 100, "Groceries"), expense(15, 120.5, "groceries"), expense(31, 30, "Groceries"), Transaction{Date: D(2026, 4, 2), Amount: -40, Kind: TxExpense, Memo: "Groceries"})
	pm := rec.PeriodMatch(food, Period{D(2026, 3, 1), D(2026, 3, 31)}, D(2026, 6, 30))
	if pm == nil || !pm.Matched || pm.Amount != 250.5 {
		t.Errorf("march %+v", pm)
	}
	if pm := rec.PeriodMatch(food, Period{D(2026, 4, 1), D(2026, 4, 30)}, D(2026, 6, 30)); pm == nil || pm.Amount != 40 {
		t.Errorf("april %+v", pm)
	}
	// An account with no transaction in the period is not tracked.
	if pm := rec.PeriodMatch(food, Period{D(2026, 5, 1), D(2026, 5, 31)}, D(2026, 6, 30)); pm != nil {
		t.Errorf("may %+v", pm)
	}
}

func TestPeriodMatchOfALumpAndItsMissingFlag(t *testing.T) {
	it := rent(1000)
	rec := matchRecord(t, []BudgetItem{it}, expense(1, 1000, "Rent"), expense(20, 15, "Pharmacy"))
	p := Period{D(2026, 3, 1), D(2026, 3, 31)}
	v := EvalPeriod(it, p, 1000, nil, D(2026, 3, 25), rec.PeriodMatch(it, p, D(2026, 3, 25)))
	if v.Posted != 1000 || !v.PostedAuto || v.Missing || v.Remaining != 0 {
		t.Errorf("matched %+v", v)
	}
	// The same period without the payment: the account has transactions in it, so the rent is missing and still to come.
	rec = matchRecord(t, []BudgetItem{it}, expense(20, 15, "Pharmacy"))
	v = EvalPeriod(it, p, 1000, nil, D(2026, 3, 25), rec.PeriodMatch(it, p, D(2026, 3, 25)))
	if v.Posted != 0 || !v.Missing || v.Remaining != 1000 {
		t.Errorf("missing %+v", v)
	}
	// Not due yet is not missing.
	v = EvalPeriod(it, p, 1000, nil, D(2026, 2, 25), rec.PeriodMatch(it, p, D(2026, 2, 25)))
	if v.Missing {
		t.Errorf("not due %+v", v)
	}
	// An override wins over the matches, and a period with no transactions keeps the date rule.
	v = EvalPeriod(it, p, 1000, &Occurrence{Status: StatusAuto}, D(2026, 3, 25), rec.PeriodMatch(it, p, D(2026, 3, 25)))
	if v.Missing || v.Posted != 1000 {
		t.Errorf("override %+v", v)
	}
	v = EvalPeriod(it, p, 1000, nil, D(2026, 3, 25), nil)
	if v.Missing || v.Posted != 1000 {
		t.Errorf("snapshots only %+v", v)
	}
	// Transactions after the cutoff do not make the period tracked.
	if pm := rec.PeriodMatch(it, p, D(2026, 3, 10)); pm != nil {
		t.Errorf("tracked by a transaction after the cutoff: %+v", pm)
	}
}

func TestMatchesAreRederivedAfterAnEdit(t *testing.T) {
	rec := matchRecord(t, []BudgetItem{rent(1000)}, expense(1, 1000, "Rent"))
	if len(rec.Matches().Item("rent")) != 1 {
		t.Fatal("no match")
	}
	c := rec.Clone()
	c.Budget.Items[0].Name = "Mortgage"
	if len(c.Matches().Item("rent")) != 0 {
		t.Error("the clone kept the matches of the original")
	}
	if len(rec.Matches().Item("rent")) != 1 {
		t.Error("the original lost its matches")
	}
	c.Budget.Items = append(c.Budget.Items, BudgetItem{ID: "x", Name: "Rent", Kind: ItemExpense, Amount: Constant(1), Recurrence: Recurrence{Kind: Monthly, Day: 1}, Account: "checking", Timing: Lump})
	if len(c.Matches().Item("x")) != 1 {
		t.Error("an added item does not match its history")
	}
}

func TestUnaccounted(t *testing.T) {
	rec := matchRecord(t, []BudgetItem{rent(1000)},
		expense(1, 1000, "Rent"),
		expense(5, 20, "Pharmacy"),
		Transaction{Date: D(2026, 3, 9), Amount: 10, Kind: TxIncome, Memo: "Refund"},
		Transaction{Date: D(2026, 3, 10), Amount: -300, Kind: TxTransfer, Counter: "savings", Memo: "Move"},
		Transaction{Date: D(2026, 4, 2), Amount: -7, Kind: TxExpense, Memo: "Pharmacy"})
	got := rec.Unaccounted("", D(2026, 1, 1), D(2026, 12, 31))
	if len(got) != 2 || got[0].Month != D(2026, 3, 1) || got[0].Amount != -10 || len(got[0].Transactions) != 2 || got[1].Month != D(2026, 4, 1) || got[1].Amount != -7 {
		t.Errorf("unaccounted %+v", got)
	}
	if got := rec.Unaccounted("checking", D(2026, 4, 1), D(2026, 4, 30)); len(got) != 1 || got[0].Amount != -7 {
		t.Errorf("april %+v", got)
	}
	if got := rec.Unaccounted("savings", D(2026, 1, 1), D(2026, 12, 31)); len(got) != 0 {
		t.Errorf("savings %+v", got)
	}
}

func TestPeriodMatchCountsOnlyTransactionsUpToTheCutoff(t *testing.T) {
	food := BudgetItem{ID: "food", Name: "Groceries", Kind: ItemExpense, Amount: Constant(600), Recurrence: Recurrence{Kind: Monthly, Day: 1}, Account: "checking", Timing: Spread}
	rec := matchRecord(t, []BudgetItem{food}, expense(3, 200, "Groceries"), expense(20, 400, "Groceries"))
	p := Period{D(2026, 3, 1), D(2026, 3, 31)}
	pm := rec.PeriodMatch(food, p, D(2026, 3, 10))
	if pm == nil || !pm.Matched || pm.Amount != 200 {
		t.Fatalf("spread %+v", pm)
	}
	if v := EvalPeriod(food, p, 600, nil, D(2026, 3, 10), pm); v.Posted != 200 || v.Remaining != 400 {
		t.Errorf("spread view %+v", v)
	}

	it := rent(1000)
	rec = matchRecord(t, []BudgetItem{it}, expense(3, 15, "Pharmacy"), expense(20, 1000, "Rent"))
	pm = rec.PeriodMatch(it, p, D(2026, 3, 10))
	if pm == nil || pm.Matched {
		t.Fatalf("lump %+v", pm)
	}
}

func TestPeriodMatchOfASpreadTransferUsesTheTrackedSource(t *testing.T) {
	rec := matchRecord(t, nil, expense(3, 15, "Pharmacy"))
	rec.Accounts[0].Transactions = nil
	sav := &Account{ID: "savings", Name: "Savings", Type: TypeSavings}
	if err := rec.AddAccount(sav); err != nil {
		t.Fatal(err)
	}
	tr := BudgetItem{ID: "save", Name: "Save", Kind: ItemTransfer, Amount: Constant(500), Recurrence: Recurrence{Kind: Monthly, Day: 31}, From: "checking", To: "savings", Timing: Spread}
	rec.Budget = Budget{Items: []BudgetItem{tr}}
	check := rec.Account("checking")
	for _, tx := range []Transaction{
		{Date: D(2026, 1, 31), Amount: -500, Kind: TxTransfer, Counter: "savings", Memo: "Save"},
		{Date: D(2026, 2, 10), Amount: -15, Kind: TxExpense, Memo: "Pharmacy"},
	} {
		if err := check.AddTransaction(tx); err != nil {
			t.Fatal(err)
		}
	}
	if err := sav.AddTransaction(Transaction{Date: D(2026, 2, 1), Amount: 500, Kind: TxTransfer, Counter: "checking", Memo: "Save"}); err != nil {
		t.Fatal(err)
	}
	rec.InvalidateMatches()
	feb := Period{D(2026, 2, 1), D(2026, 2, 28)}
	pm := rec.PeriodMatch(tr, feb, D(2026, 2, 15))
	if pm == nil || pm.Matched || pm.Amount != 0 {
		t.Fatalf("february %+v", pm)
	}
}

func TestPeriodMatchOfALumpTransferIsMatchedOnEitherLeg(t *testing.T) {
	rec := matchRecord(t, nil, expense(5, 40, "Groceries"))
	rec.Accounts[0].Transactions = nil
	if err := rec.Account("checking").AddTransaction(Transaction{Date: D(2026, 2, 5), Amount: -40, Kind: TxExpense, Memo: "Groceries"}); err != nil {
		t.Fatal(err)
	}
	sav := &Account{ID: "savings", Name: "Savings", Type: TypeSavings}
	if err := rec.AddAccount(sav); err != nil {
		t.Fatal(err)
	}
	tr := BudgetItem{ID: "save", Name: "Save", Kind: ItemTransfer, Amount: Constant(500), Recurrence: Recurrence{Kind: Monthly, Day: 1}, From: "checking", To: "savings", Timing: Lump}
	rec.Budget = Budget{Items: []BudgetItem{tr}}
	if err := sav.AddTransaction(Transaction{Date: D(2026, 2, 1), Amount: 500, Kind: TxTransfer, Counter: "checking", Memo: "Save"}); err != nil {
		t.Fatal(err)
	}
	rec.InvalidateMatches()
	feb := Period{D(2026, 2, 1), D(2026, 2, 28)}
	pm := rec.PeriodMatch(tr, feb, D(2026, 2, 15))
	if pm == nil || !pm.Matched || pm.Amount != 500 {
		t.Fatalf("february %+v", pm)
	}
	if !rec.Paid(tr, D(2026, 2, 1), D(2026, 2, 15)) {
		t.Error("not paid")
	}
}

func TestPeriodMatchAsOfIsTrackedByTransactionsInThePeriod(t *testing.T) {
	p := Period{D(2026, 3, 1), D(2026, 3, 31)}
	asOf := D(2026, 3, 5)

	// No transaction between the due date and the cutoff, many after: the lump is missing, not unknown.
	it := rent(1000)
	rec := matchRecord(t, []BudgetItem{it}, expense(20, 15, "Pharmacy"), expense(25, 30, "Pharmacy"))
	if pm := rec.PeriodMatch(it, p, asOf); pm != nil {
		t.Fatalf("PeriodMatch tracked %+v", pm)
	}
	pm := rec.PeriodMatchAsOf(it, p, asOf)
	if pm == nil {
		t.Fatal("not tracked")
	}
	if v := EvalPeriod(it, p, 1000, nil, asOf, pm); !v.Missing || v.Posted != 0 {
		t.Errorf("lump %+v", v)
	}

	// A spread line shows what actually matched up to the cutoff, not the pro-rata estimate.
	food := BudgetItem{ID: "food", Name: "Groceries", Kind: ItemExpense, Amount: Constant(600), Recurrence: Recurrence{Kind: Monthly, Day: 1}, Account: "checking", Timing: Spread}
	rec = matchRecord(t, []BudgetItem{food}, expense(20, 400, "Groceries"))
	pm = rec.PeriodMatchAsOf(food, p, asOf)
	if pm == nil || pm.Matched || pm.Amount != 0 {
		t.Fatalf("spread before spending %+v", pm)
	}
	rec = matchRecord(t, []BudgetItem{food}, expense(2, 50, "Groceries"), expense(20, 400, "Groceries"))
	pm = rec.PeriodMatchAsOf(food, p, asOf)
	if pm == nil || pm.Amount != 50 {
		t.Errorf("spread %+v", pm)
	}

	// Transactions only in a later period leave the account untracked in this one.
	rec = matchRecord(t, []BudgetItem{it}, Transaction{Date: D(2026, 4, 10), Amount: -15, Kind: TxExpense, Memo: "Pharmacy"})
	if pm := rec.PeriodMatchAsOf(it, p, asOf); pm != nil {
		t.Errorf("later period tracked %+v", pm)
	}
}
