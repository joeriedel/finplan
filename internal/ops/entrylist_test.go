// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestListEntriesNewestFirst(t *testing.T) {
	st, rec := seed(t)
	day := model.D(2030, 1, 5)
	var ids []int64
	for _, amount := range []float64{-1, -2} {
		res, err := AddTransaction(st, rec, demodata.Checking, model.Transaction{Date: day, Amount: amount, Kind: model.TxExpense}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		rec = res.Record
		ids = append(ids, res.Entry.ID)
	}
	es, err := ListEntries(st, demodata.Checking, false)
	if err != nil || len(es) < 3 {
		t.Fatalf("%d entries, %v", len(es), err)
	}
	if es[0].ID != ids[1] || es[1].ID != ids[0] {
		t.Errorf("the same-day entries are %d, %d; want %d, %d", es[0].ID, es[1].ID, ids[1], ids[0])
	}
	for i := 1; i < len(es); i++ {
		if es[i].Date > es[i-1].Date {
			t.Fatalf("entry %d is dated after the one before it", i)
		}
	}
	stored, _ := st.Entries(demodata.Checking)
	if len(es) != len(stored) {
		t.Errorf("%d listed, %d stored", len(es), len(stored))
	}
}

func TestListEntriesVoided(t *testing.T) {
	st, rec := seed(t)
	tx := firstTransaction(t, st, demodata.Checking, model.TxExpense)
	before, _ := ListEntries(st, demodata.Checking, false)
	if _, err := VoidEntry(st, rec, tx.ID, "dup", Options{}); err != nil {
		t.Fatal(err)
	}
	live, _ := ListEntries(st, demodata.Checking, false)
	all, err := ListEntries(st, demodata.Checking, true)
	if err != nil || len(live) != len(before)-1 || len(all) != len(before) {
		t.Fatalf("%d before, %d live, %d with voided, %v", len(before), len(live), len(all), err)
	}
	for _, e := range live {
		if e.Void || e.ID == tx.ID {
			t.Errorf("live list holds %+v", e)
		}
	}
	voided := 0
	for _, e := range all {
		if e.Void {
			voided++
			if e.ID != tx.ID {
				t.Errorf("entry %d is flagged void", e.ID)
			}
		}
	}
	if voided != 1 {
		t.Errorf("%d entries flagged void", voided)
	}
}

func TestEntryHistory(t *testing.T) {
	st, rec := seed(t)
	tx := firstTransaction(t, st, demodata.Checking, model.TxExpense)
	res, err := CorrectEntry(st, rec, tx.ID, EntryChange{Amount: model.Ptr(-1.5)}, "typo", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VoidEntry(st, res.Record, tx.ID, "dup", Options{}); err != nil {
		t.Fatal(err)
	}
	revs, err := EntryHistory(st, tx.ID)
	if err != nil || len(revs) != 3 {
		t.Fatalf("%+v, %v", revs, err)
	}
	if revs[0].Values != tx.Values || revs[0].Note != "" || revs[1].Amount != -1.5 || revs[1].Note != "typo" || revs[1].Void || !revs[2].Void || revs[2].Note != "dup" {
		t.Errorf("history %+v", revs)
	}
	if _, err := EntryHistory(st, 99999); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown entry: %v", err)
	}
}

func TestCorrectAndVoidReturnTheReloadedRecord(t *testing.T) {
	st, rec := seed(t)
	tx := firstTransaction(t, st, demodata.Checking, model.TxExpense)
	for name, do := range map[string]func(dry bool) (*Result, error){
		"correct": func(dry bool) (*Result, error) {
			return CorrectEntry(st, rec, tx.ID, EntryChange{Memo: model.Ptr("fixed")}, "", Options{DryRun: dry})
		},
		"void": func(dry bool) (*Result, error) { return VoidEntry(st, rec, tx.ID, "", Options{DryRun: dry}) },
	} {
		dry, err := do(true)
		if err != nil || dry.Record != nil {
			t.Fatalf("%s dry run: %+v, %v", name, dry, err)
		}
		res, err := do(false)
		if err != nil || res.Record == nil {
			t.Fatalf("%s: %+v, %v", name, res, err)
		}
		rec = res.Record
		if !reflect.DeepEqual(res.Record, reload(t, st)) {
			t.Errorf("%s: the record is not the stored one", name)
		}
	}
}
