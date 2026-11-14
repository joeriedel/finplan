// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestItemLinkRoundTripsAndFormat14DataLoadsWithoutOne(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	acct := rec.Account(demodata.Checking)
	acct.Transactions[0].Item = "groceries"
	mustSave(t, s, rec)
	got := mustLoad(t, s)
	if l := got.Account(demodata.Checking).Transactions[0].Item; l != "groceries" {
		t.Errorf("the link loaded as %q, want groceries", l)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Error("the record with a link loaded differently")
	}

	// A record of format 14 has no links: every transaction reads as unlinked.
	old, _ := openTemp(t)
	want := demodata.Family()
	mustSave(t, old, want)
	if _, err := old.db.Exec(`UPDATE documents SET format = 14`); err != nil {
		t.Fatal(err)
	}
	loaded := mustLoad(t, old)
	for _, a := range loaded.Accounts {
		for _, tx := range a.Transactions {
			if tx.Item != "" {
				t.Fatalf("%s has a transaction linked to %q after loading format 14", a.ID, tx.Item)
			}
		}
	}
	if !reflect.DeepEqual(loaded, want) {
		t.Error("the format 14 record loaded differently")
	}
}

func TestMigrationSevenKeepsEntriesUnlinked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v6.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db, 6); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO entries (account, kind, date, amount, tx_kind, memo, made_at) VALUES ('checking', 'transaction', 5, -3, 'expense', 'Coffee', 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	es, err := s.Entries("checking")
	if err != nil || len(es) != 1 || es[0].Memo != "Coffee" || es[0].Item != "" {
		t.Fatalf("entries after the migration: %+v, %v", es, err)
	}
	if err := s.Correct(es[0].ID, Values{Date: 5, Amount: -3, TxKind: model.TxExpense, Memo: "Coffee", Item: "dining"}, "link"); err != nil {
		t.Fatal(err)
	}
	if e, err := s.Entry(es[0].ID); err != nil || e.Item != "dining" {
		t.Errorf("entry after the correction: %+v, %v", e, err)
	}
}
