// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestTransactionLinkToABudgetItem(t *testing.T) {
	st, rec := seed(t)
	day := demodata.FamilyToday.Add(1)
	tx := model.Transaction{Date: day, Amount: -42, Kind: model.TxExpense, Memo: "Weekend dinner", Item: "dining"}

	// Dining out is on the card, which the link must be on.
	if _, err := AddTransaction(st, rec, demodata.Checking, tx, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("a link to an item that does not touch the account: %v", err)
	}
	bad := tx
	bad.Item = "no-such-item"
	if _, err := AddTransaction(st, rec, demodata.Card, bad, Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("a link to an unknown item: %v", err)
	}
	res, err := AddTransaction(st, rec, demodata.Card, tx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.Item != "dining" || res.Entry.ID == 0 {
		t.Errorf("entry %+v", res.Entry)
	}
	got := reload(t, st)
	matched := false
	for _, m := range got.Matches().Item("dining") {
		matched = matched || m.Transaction.Date == day && m.Transaction.Memo == "Weekend dinner"
	}
	if !matched {
		t.Error("the linked transaction does not match its item after the reload")
	}

	// A correction can move the link to another item of the account, and clear it.
	groceries := "groceries"
	if _, err := CorrectEntry(st, got, res.Entry.ID, EntryChange{Item: &groceries}, "", Options{}); err != nil {
		t.Fatal(err)
	}
	if e, _ := st.Entry(res.Entry.ID); e.Item != "groceries" {
		t.Errorf("item after the correction %q", e.Item)
	}
	none := ""
	if _, err := CorrectEntry(st, got, res.Entry.ID, EntryChange{Item: &none}, "unlink", Options{}); err != nil {
		t.Fatal(err)
	}
	if e, _ := st.Entry(res.Entry.ID); e.Item != "" {
		t.Errorf("item after clearing %q", e.Item)
	}
	if _, err := CorrectEntry(st, got, res.Entry.ID, EntryChange{Item: &none}, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("clearing a cleared link: %v", err)
	}
	revs, err := EntryHistory(st, res.Entry.ID)
	if err != nil || len(revs) != 3 || revs[0].Item != "dining" || revs[1].Item != "groceries" || revs[2].Item != "" {
		t.Errorf("history %+v, %v", revs, err)
	}
	salary := "salary-a"
	if _, err := CorrectEntry(st, got, res.Entry.ID, EntryChange{Item: &salary}, "", Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("correcting to an item of another account: %v", err)
	}
}
