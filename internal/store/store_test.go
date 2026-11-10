// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fin.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

// richFamily is the demo family extended so that every model type is present.
func richFamily(t *testing.T) *model.Record {
	t.Helper()
	rec := demodata.Family()
	rec.Classes = []model.MarketClass{{ID: "lc", Name: "Large cap", Return: 0.07}, {ID: "tr", Name: "Treasuries", Return: 0.03}}
	if err := rec.AddAsset(&model.Asset{
		ID: "tuned", Name: "Tuned", Members: []model.Member{model.AccountMember(demodata.Brokerage)},
		Default: model.DefaultBalance{Intervals: []model.Interval{{From: 10, To: model.Ptr(model.Date(20)), Value: 5}, {From: 30, Value: 6}}},
		Params: map[string]model.Schedule{
			model.PropGrowth: model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(model.D(2030, time.January, 1)), Value: 0.05}, model.Segment{From: model.D(2031, time.January, 1), Value: 0.01}),
			"empty":          {},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// A member added after other assets, to an asset created earlier, interleaves the sequence.
	if err := rec.AddMember(demodata.Retirement, model.RuleMember(model.Rule{Types: []model.AccountType{model.TypeIRA}})); err != nil {
		t.Fatal(err)
	}
	if err := rec.AddMember(model.DefaultAssetID(demodata.Checking), model.AssetMember("tuned")); err != nil {
		t.Fatal(err)
	}
	rec.Assets[0].Params = map[string]model.Schedule{model.PropGrowth: model.Constant(0.01)}
	item := model.BudgetItem{
		ID: "bonus", Name: "Bonus", Kind: model.ItemIncome, Amount: 1000, Indexed: true, Growth: 0.03, Account: demodata.Checking,
		Recurrence: model.Recurrence{Kind: model.Annual, Month: time.March, Day: 1},
		Start:      model.Ptr(model.D(2027, time.January, 1)), End: model.Ptr(model.D(2030, time.January, 1)),
	}
	err := rec.AddProjection(&model.Projection{
		Name: "rich", Parent: demodata.ProjBase, Start: model.Ptr(model.D(2027, time.January, 1)), OverrideHistory: model.Ptr(false),
		AssetSettings: []model.AssetSetting{
			{Asset: demodata.Retirement, Start: model.Ptr(model.D(2028, time.January, 1)), OverrideHistory: model.Ptr(true)},
			{Asset: demodata.Liquid},
		},
		Mods: []model.Modification{
			{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "bonus"}, Property: model.PropAddItem, Item: &item},
			{Target: model.ObjectRef{Kind: model.KindGlobal, ID: model.PropInflation}, Property: model.PropInflation, Values: model.Constant(0.03)},
			{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "bonus"}, Property: model.PropAmount, Mode: model.ModeScale, Values: model.Constant(0.7)},
		},
		Shocks:     []model.Shock{{Class: "lc", Date: model.D(2028, time.June, 1), Change: -0.3}},
		Rebalances: []model.Rebalance{{Account: demodata.Brokerage, Date: model.D(2029, time.January, 1), Allocation: []model.Weight{{Class: "lc", Share: 0.6}, {Class: "tr", Share: 0.4}}}, {Account: demodata.Roth}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func mustSave(t *testing.T, s *Store, r *model.Record) {
	t.Helper()
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}
}

func mustLoad(t *testing.T, s *Store) *model.Record {
	t.Helper()
	r, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	want := richFamily(t)
	mustSave(t, s, want)
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Fatal("loaded record differs from the saved one")
	}
	plain := demodata.Family()
	s2, _ := openTemp(t)
	mustSave(t, s2, plain)
	if got := mustLoad(t, s2); !reflect.DeepEqual(got, plain) {
		t.Fatal("loaded Family differs from the original")
	}
}

func TestFormat1DocumentsLoadUnchanged(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	mustSave(t, s, want)
	var format int
	if err := s.db.QueryRow(`SELECT MAX(format) FROM documents`).Scan(&format); err != nil || format != formatCurrent || formatCurrent != 3 {
		t.Fatalf("format written: %d, %v", format, err)
	}
	// Format 1 documents have no mode and no growth.
	var withNew int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE kind IN ('budget', 'projection') AND (body LIKE '%"mode":%' OR body LIKE '%"growth":%')`).Scan(&withNew); err != nil || withNew != 0 {
		t.Fatalf("documents with a mode or growth: %d, %v", withNew, err)
	}
	if _, err := s.db.Exec(`UPDATE documents SET format = 1`); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Fatal("format 1 record loads differently")
	}
}

func TestFormat2DocumentsLoadUnchanged(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	mustSave(t, s, want)
	var withAccounts int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE kind = 'projection' AND body LIKE '%"accounts":%'`).Scan(&withAccounts); err != nil || withAccounts != 0 {
		t.Fatalf("projection documents with accounts: %d, %v", withAccounts, err)
	}
	if _, err := s.db.Exec(`UPDATE documents SET format = 2`); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Fatal("format 2 record loads differently")
	}
}

func TestProjectionAccountsRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	p := want.Projection("New job")
	p.Accounts = []model.ProjAccount{{ID: "villa", Name: "Villa", Type: model.TypeProperty, Tax: model.Taxable, StatedRate: 0.03, Opening: 250000, From: model.D(2027, 3, 1)}}
	mustSave(t, s, want)
	got := mustLoad(t, s)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("projection account did not round-trip")
	}
	if got.Account("villa") != nil {
		t.Fatal("a projection account became a record account")
	}
}

func TestEmptyDatabaseLoadsEmptyRecord(t *testing.T) {
	s, _ := openTemp(t)
	if got := mustLoad(t, s); !reflect.DeepEqual(got, model.NewRecord(0)) {
		t.Fatal("empty database should load as an empty record")
	}
}

func TestCorrections(t *testing.T) {
	s, _ := openTemp(t)
	clock := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { clock = clock.Add(time.Hour); return clock }
	rec := demodata.Family()
	mustSave(t, s, rec)

	snaps, err := s.Entries(demodata.Home)
	if err != nil || len(snaps) != 3 {
		t.Fatalf("home entries: %v, %v", snaps, err)
	}
	sn := snaps[1]
	if err := s.Correct(sn.ID, Values{Date: sn.Date, Amount: 111}, "typo"); err != nil {
		t.Fatal(err)
	}
	if err := s.Correct(sn.ID, Values{Date: sn.Date, Amount: 222}, "again"); err != nil {
		t.Fatal(err)
	}
	txs, _ := s.Entries(demodata.Checking)
	var tx Entry
	for _, e := range txs {
		if e.Kind == KindTransaction {
			tx = e
			break
		}
	}
	fixed := tx.Values
	fixed.Memo, fixed.Amount = "fixed memo", -1.5
	if err := s.Correct(tx.ID, fixed, ""); err != nil {
		t.Fatal(err)
	}

	got := mustLoad(t, s)
	if b := got.Account(demodata.Home).Snapshots[1].Balance; b != 222 {
		t.Errorf("corrected snapshot = %v", b)
	}
	found := false
	for _, t2 := range got.Account(demodata.Checking).Transactions {
		found = found || (t2.Memo == "fixed memo" && t2.Amount == -1.5)
	}
	if !found {
		t.Error("corrected transaction not loaded")
	}

	h, err := s.History(sn.ID)
	if err != nil || len(h) != 3 {
		t.Fatalf("history: %v, %v", h, err)
	}
	if h[0].Amount != snaps[1].Amount || h[1].Amount != 111 || h[2].Amount != 222 || h[1].Note != "typo" || h[2].Note != "again" || h[0].Note != "" {
		t.Errorf("history = %+v", h)
	}
	if !h[0].MadeAt.Before(h[1].MadeAt) || !h[1].MadeAt.Before(h[2].MadeAt) {
		t.Errorf("history times not increasing: %+v", h)
	}

	var amount float64
	if err := s.db.QueryRow(`SELECT amount FROM entries WHERE id = ?`, sn.ID).Scan(&amount); err != nil || amount != snaps[1].Amount {
		t.Errorf("original row = %v, %v", amount, err)
	}

	// Saving the loaded (corrected) record matches the stored corrected entries and adds nothing.
	var before, after int
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&before)
	mustSave(t, s, got)
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&after)
	if before != after {
		t.Errorf("entries %d -> %d", before, after)
	}

	// Two snapshots may not share a date.
	err = s.Correct(snaps[1].ID, Values{Date: snaps[0].Date, Amount: 1}, "")
	if err == nil {
		t.Error("snapshot correction onto an occupied date accepted")
	}
	if err := s.Correct(9999, Values{}, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown entry: %v", err)
	}
}

func TestAppendOnly(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	mustSave(t, s, rec)
	es, _ := s.Entries(demodata.Home)
	if err := s.Correct(es[0].ID, Values{Date: es[0].Date, Amount: 5}, ""); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE entries SET amount = 0`, `DELETE FROM entries`,
		`UPDATE corrections SET amount = 0`, `DELETE FROM corrections`,
	} {
		if _, err := s.db.Exec(q); err == nil {
			t.Errorf("%s succeeded", q)
		}
	}

	count := func() (n int) {
		s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&n)
		return
	}
	n := count()
	altered := mustLoad(t, s)
	altered.Account(demodata.Home).Snapshots[2].Balance++
	if err := s.Save(altered); !errors.Is(err, ErrRecordChanged) {
		t.Errorf("altered entry: %v", err)
	}
	removed := mustLoad(t, s)
	removed.Account(demodata.Checking).Transactions = removed.Account(demodata.Checking).Transactions[1:]
	removed.Account(demodata.Home).AddSnapshot(model.D(2027, 1, 1), 1)
	if err := s.Save(removed); !errors.Is(err, ErrRecordChanged) {
		t.Errorf("removed entry: %v", err)
	}
	gone := model.NewRecord(rec.Today)
	if err := s.Save(gone); !errors.Is(err, ErrRecordChanged) {
		t.Errorf("absent account: %v", err)
	}
	if count() != n {
		t.Error("a failed Save wrote entries")
	}
	if !reflect.DeepEqual(mustLoad(t, s).Account(demodata.Home).Snapshots[2], rec.Account(demodata.Home).Snapshots[2]) {
		t.Error("a failed Save changed documents or entries")
	}

	appended := mustLoad(t, s)
	appended.Account(demodata.Home).AddSnapshot(model.D(2027, 1, 1), 600000)
	appended.Account(demodata.Checking).AddTransaction(model.Transaction{Date: model.D(2026, 1, 1), Amount: -9, Kind: model.TxExpense, Memo: "dup day"})
	mustSave(t, s, appended)
	if count() != n+2 {
		t.Errorf("entries = %d, want %d", count(), n+2)
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, appended) {
		t.Error("appended record did not round-trip")
	}
}

func TestReopen(t *testing.T) {
	s, path := openTemp(t)
	want := richFamily(t)
	mustSave(t, s, want)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := mustLoad(t, s2); !reflect.DeepEqual(got, want) {
		t.Fatal("reopened record differs")
	}
}

func TestMigrateFromVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db, 1); err != nil {
		t.Fatal(err)
	}
	var v int
	db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != 1 {
		t.Fatalf("user_version = %d", v)
	}
	if _, err := db.Exec(`SELECT * FROM presets`); err == nil {
		t.Fatal("presets table exists at version 1")
	}
	db.Close()

	// Populate the version 1 schema with SQL naming only the columns it had.
	src, srcPath := openTemp(t)
	want := demodata.Family()
	mustSave(t, src, want)
	src.Close()
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`ATTACH DATABASE '` + srcPath + `' AS src`,
		`INSERT INTO documents (kind, key, ordinal, format, body)
			SELECT kind, key, ordinal, format, body FROM src.documents`,
		`INSERT INTO entries (id, account, kind, date, amount, tx_kind, counter, memo, made_at)
			SELECT id, account, kind, date, amount, tx_kind, counter, memo, made_at FROM src.entries`,
		`INSERT INTO corrections (id, entry, date, amount, tx_kind, counter, memo, note, made_at)
			SELECT id, entry, date, amount, tx_kind, counter, memo, note, made_at FROM src.corrections`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Error("migrated database loads differently")
	}
	if err := s.PutPreset("p", 1, []byte("x")); err != nil {
		t.Error(err)
	}
}

func TestSaveStaleAfterVoid(t *testing.T) {
	s, _ := openTemp(t)
	mustSave(t, s, demodata.Family())
	stale := mustLoad(t, s)
	es, _ := s.Entries(demodata.Home)
	if err := s.Void(es[1].ID, ""); err != nil {
		t.Fatal(err)
	}
	var before int
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&before)
	if err := s.Save(stale); !errors.Is(err, ErrRecordChanged) {
		t.Fatalf("Save of a record loaded before the void: %v", err)
	}
	var after int
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&after)
	if after != before {
		t.Errorf("entry rows %d -> %d", before, after)
	}
}

func TestSaveStaleAfterCorrectAndVoid(t *testing.T) {
	s, _ := openTemp(t)
	mustSave(t, s, demodata.Family())
	stale := mustLoad(t, s)
	es, _ := s.Entries(demodata.Checking)
	last := es[len(es)-1]
	v := last.Values
	v.Amount = -43.25
	if err := s.Correct(last.ID, v, "fix"); err != nil {
		t.Fatal(err)
	}
	if err := s.Void(last.ID, "dup"); err != nil {
		t.Fatal(err)
	}
	var before, after int
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&before)
	if err := s.Save(stale); !errors.Is(err, ErrRecordChanged) {
		t.Fatalf("Save of a record loaded before correct and void: %v", err)
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&after)
	if after != before {
		t.Errorf("entry rows %d -> %d", before, after)
	}
}

func TestSaveNewEntryEqualToEarlierVoid(t *testing.T) {
	s, _ := openTemp(t)
	fare := model.Transaction{Date: model.D(2026, 1, 3), Amount: -2.75, Kind: model.TxExpense, Memo: "fare"}
	later := model.Transaction{Date: model.D(2026, 1, 4), Amount: -8.5, Kind: model.TxExpense, Memo: "lunch"}
	rec := demodata.Family()
	rec.Account(demodata.Checking).AddTransaction(fare)
	rec.Account(demodata.Checking).AddTransaction(later)
	mustSave(t, s, rec)
	findFares := func() []Entry {
		es, err := s.Entries(demodata.Checking)
		if err != nil {
			t.Fatal(err)
		}
		var fares []Entry
		for _, e := range es {
			if e.Values.Memo == "fare" && e.Values.Amount == -2.75 {
				fares = append(fares, e)
			}
		}
		return fares
	}
	before, _ := s.Entries(demodata.Checking)
	fares := findFares()
	if len(fares) != 1 {
		t.Fatalf("fare entries = %d, want 1", len(fares))
	}
	old := fares[0]
	if err := s.Void(old.ID, "duplicate"); err != nil {
		t.Fatal(err)
	}
	loaded := mustLoad(t, s)
	loaded.Account(demodata.Checking).AddTransaction(fare)
	mustSave(t, s, loaded)
	live, _ := s.Entries(demodata.Checking)
	if len(live) != len(before) {
		t.Errorf("live entries = %d, want %d", len(live), len(before))
	}
	fares = findFares()
	if len(fares) != 1 || fares[0].ID == old.ID {
		t.Errorf("live fare entries = %v, want one new entry besides voided %d", fares, old.ID)
	}
}

func TestSaveStaleAfterCorrectWithEqualNewEntry(t *testing.T) {
	s, _ := openTemp(t)
	fare := model.Transaction{Date: model.D(2026, 1, 3), Amount: -2.75, Kind: model.TxExpense, Memo: "fare"}
	rec := demodata.Family()
	rec.Account(demodata.Checking).AddTransaction(fare)
	mustSave(t, s, rec)
	loaded := mustLoad(t, s)
	es, _ := s.Entries(demodata.Checking)
	var a Entry
	for _, e := range es {
		if e.Values.Memo == "fare" {
			a = e
		}
	}
	v := a.Values
	v.Amount = -3
	if err := s.Correct(a.ID, v, "fare rose"); err != nil {
		t.Fatal(err)
	}
	fare.Amount = -3
	loaded.Account(demodata.Checking).AddTransaction(fare)
	var before, after int
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&before)
	if err := s.Save(loaded); !errors.Is(err, ErrRecordChanged) {
		t.Fatalf("Save after a correction since Load: %v", err)
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&after)
	if after != before {
		t.Errorf("entry rows %d -> %d", before, after)
	}
}

func TestSaveStaleAfterVoidAndRestore(t *testing.T) {
	s, _ := openTemp(t)
	fare := model.Transaction{Date: model.D(2026, 1, 3), Amount: -2.75, Kind: model.TxExpense, Memo: "fare"}
	rec := demodata.Family()
	rec.Account(demodata.Checking).AddTransaction(fare)
	mustSave(t, s, rec)
	loaded := mustLoad(t, s)
	es, _ := s.Entries(demodata.Checking)
	var a Entry
	for _, e := range es {
		if e.Values.Memo == "fare" {
			a = e
		}
	}
	if err := s.Void(a.ID, "dup"); err != nil {
		t.Fatal(err)
	}
	if err := s.Correct(a.ID, a.Values, "restore"); err != nil {
		t.Fatal(err)
	}
	loaded.Account(demodata.Checking).AddTransaction(fare)
	if err := s.Save(loaded); !errors.Is(err, ErrRecordChanged) {
		t.Fatalf("Save after void and restore since Load: %v", err)
	}
}

func TestVoid(t *testing.T) {
	s, _ := openTemp(t)
	clock := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { clock = clock.Add(time.Hour); return clock }
	rec := demodata.Family()
	mustSave(t, s, rec)
	before, _ := s.Entries(demodata.Home)
	sn := before[1]

	if err := s.Void(sn.ID, "duplicate"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Entries(demodata.Home)
	if len(after) != len(before)-1 {
		t.Fatalf("entries %d -> %d", len(before), len(after))
	}
	got := mustLoad(t, s)
	if n := len(got.Account(demodata.Home).Snapshots); n != len(before)-1 {
		t.Errorf("loaded snapshots = %d", n)
	}
	var rows int
	s.db.QueryRow(`SELECT COUNT(*) FROM entries WHERE id = ?`, sn.ID).Scan(&rows)
	if rows != 1 {
		t.Error("voided entry row was removed")
	}
	h, err := s.History(sn.ID)
	if err != nil || len(h) != 2 || h[0].Void || !h[1].Void || h[1].Note != "duplicate" || h[1].Amount != sn.Amount || h[1].Date != sn.Date {
		t.Fatalf("history: %+v, %v", h, err)
	}

	// Saving the loaded record neither fails nor brings the entry back.
	mustSave(t, s, got)
	if again, _ := s.Entries(demodata.Home); len(again) != len(before)-1 {
		t.Errorf("save re-inserted the voided entry: %d entries", len(again))
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&rows)
	var total int
	for _, a := range rec.Accounts {
		total += len(a.Snapshots) + len(a.Transactions)
	}
	if rows != total {
		t.Errorf("entry rows = %d, want %d", rows, total)
	}

	// A correction restores it.
	if err := s.Correct(sn.ID, Values{Date: sn.Date, Amount: 5}, "restored"); err != nil {
		t.Fatal(err)
	}
	restored, _ := s.Entries(demodata.Home)
	if len(restored) != len(before) || restored[1].Amount != 5 {
		t.Errorf("restored entries = %+v", restored)
	}
	if h, _ := s.History(sn.ID); len(h) != 3 || h[2].Void {
		t.Errorf("history after restore = %+v", h)
	}
	if err := s.Void(9999, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown entry: %v", err)
	}
}

func TestMigrateFromVersion2(t *testing.T) {
	// A version 3 database with a correction supplies the rows for the version 2 one.
	src, srcPath := openTemp(t)
	want := demodata.Family()
	mustSave(t, src, want)
	es, _ := src.Entries(demodata.Home)
	if err := src.Correct(es[1].ID, Values{Date: es[1].Date, Amount: 111}, "typo"); err != nil {
		t.Fatal(err)
	}
	want = mustLoad(t, src)
	src.Close()

	path := filepath.Join(t.TempDir(), "v2.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db, 2); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ATTACH DATABASE '` + srcPath + `' AS src`,
		`INSERT INTO documents SELECT * FROM src.documents`,
		`INSERT INTO entries SELECT * FROM src.entries`,
		`INSERT INTO corrections (id, entry, date, amount, tx_kind, counter, memo, note, made_at)
			SELECT id, entry, date, amount, tx_kind, counter, memo, note, made_at FROM src.corrections`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var v int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != 3 {
		t.Errorf("user_version = %d, want 3", v)
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Error("migrated database loads differently")
	}
	if err := s.Void(es[0].ID, ""); err != nil {
		t.Error(err)
	}
}

func TestBackupRefusesOwnDatabase(t *testing.T) {
	s, path := openTemp(t)
	mustSave(t, s, demodata.Family())
	if err := s.Backup(path); err == nil {
		t.Fatal("Backup over the open database succeeded")
	}
	if got := mustLoad(t, s); got == nil {
		t.Error("store unusable after refused backup")
	}
}

func TestBackup(t *testing.T) {
	s, _ := openTemp(t)
	want := richFamily(t)
	mustSave(t, s, want)
	if err := s.PutPreset("p", 1, []byte("x")); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "backup.db")
	// A second backup over the first replaces it.
	for i := 0; i < 2; i++ {
		if err := s.Backup(target); err != nil {
			t.Fatal(err)
		}
	}
	b, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := mustLoad(t, b); !reflect.DeepEqual(got, want) {
		t.Error("backup loads differently")
	}
	if p, err := b.Preset("p"); err != nil || string(p.Body) != "x" {
		t.Errorf("backup preset: %+v, %v", p, err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".finplan-backup-*"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	if err := s.Backup(filepath.Join(dir, "missing", "b.db")); err == nil {
		t.Error("backup into a missing folder succeeded")
	}
}

func TestDataVersion(t *testing.T) {
	s, path := openTemp(t)
	v0, err := s.DataVersion()
	if err != nil {
		t.Fatal(err)
	}
	mustSave(t, s, demodata.Family())
	if v, _ := s.DataVersion(); v != v0 {
		t.Errorf("own write changed the version: %d -> %d", v0, v)
	}
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.PutPreset("p", 1, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.DataVersion(); v == v0 {
		t.Error("another store's write did not change the version")
	}
}

func TestWAL(t *testing.T) {
	s, _ := openTemp(t)
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v", mode, err)
	}
}

func TestPresets(t *testing.T) {
	s, _ := openTemp(t)
	bodies := map[string][]byte{
		"binary": {0xff, 0xfe, 0x00, 0x80, 'a', 0x00},
		"empty":  {},
		"nil":    nil,
		"text":   []byte(`{"k":1}`),
	}
	i := 1
	for name, b := range bodies {
		if err := s.PutPreset(name, i, b); err != nil {
			t.Fatal(err)
		}
		i++
	}
	for name, b := range bodies {
		p, err := s.Preset(name)
		if err != nil || !bytes.Equal(p.Body, b) || p.Name != name || p.Format < 1 {
			t.Errorf("%s: %+v, %v", name, p, err)
		}
	}
	if err := s.PutPreset("text", 7, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Preset("text"); p.Format != 7 || string(p.Body) != "new" {
		t.Errorf("replace: %+v", p)
	}
	ps, err := s.Presets()
	if err != nil || len(ps) != 4 || ps[0].Name != "binary" || ps[3].Name != "text" {
		t.Errorf("presets: %+v, %v", ps, err)
	}
	if err := s.DeletePreset("text"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Preset("text"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted preset: %v", err)
	}
	if err := s.PutPreset("bad", 0, nil); err == nil {
		t.Error("format 0 accepted")
	}
}

func TestFutureVersionsAreErrors(t *testing.T) {
	s, path := openTemp(t)
	mustSave(t, s, demodata.Family())
	if _, err := s.db.Exec(`UPDATE documents SET format = 99 WHERE kind = 'budget'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("future document format: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE documents SET format = 1`); err != nil {
		t.Fatal(err)
	}
	mustLoad(t, s)
	if _, err := s.db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(path); !errors.Is(err, ErrFutureSchema) {
		t.Errorf("future schema: %v", err)
	}
	// The failed Open left the database untouched.
	db, _ := sql.Open("sqlite", path)
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&n); err != nil || n == 0 {
		t.Errorf("documents after failed open: %d, %v", n, err)
	}
}

// interleaved builds a record from steps, failing the test on any model error.
func interleaved(t *testing.T, steps ...func(r *model.Record) error) *model.Record {
	t.Helper()
	rec := model.NewRecord(model.D(2026, time.January, 1))
	for _, f := range steps {
		if err := f(rec); err != nil {
			t.Fatal(err)
		}
	}
	return rec
}

func addAccount(id model.AccountID) func(*model.Record) error {
	return func(r *model.Record) error { return r.AddAccount(&model.Account{ID: id, Name: string(id)}) }
}

func addAsset(id model.AssetID, ms ...model.Member) func(*model.Record) error {
	return func(r *model.Record) error { return r.AddAsset(&model.Asset{ID: id, Members: ms}) }
}

// failingAsset adds an asset that must fail, as a step.
func failingAsset(id model.AssetID, ms ...model.Member) func(*model.Record) error {
	return func(r *model.Record) error {
		if r.AddAsset(&model.Asset{ID: id, Members: ms}) == nil {
			return fmt.Errorf("AddAsset %q succeeded, want failure", id)
		}
		return nil
	}
}

func addMember(asset model.AssetID, m model.Member) func(*model.Record) error {
	return func(r *model.Record) error { return r.AddMember(asset, m) }
}

func TestRoundTripInterleavedMembers(t *testing.T) {
	rule := model.RuleMember(model.Rule{Types: []model.AccountType{model.TypeIRA}})
	cases := map[string]*model.Record{
		"empty group, account, then member": interleaved(t,
			addAccount("a"), addAsset("x"), addAccount("b"), addMember("x", rule)),
		"group with member, account, then member": interleaved(t,
			addAsset("x", rule), addAccount("a"), addMember("x", rule)),
		"asset member of a later default, members added early": interleaved(t,
			addAccount("a"), addAsset("x"), addAsset("y", rule), addAccount("b"),
			addMember("x", model.AccountMember("b")), addAccount("c"),
			addMember("y", model.AssetMember(model.DefaultAssetID("c"))), addMember("x", rule),
			addMember("x", model.AssetMember("y")), addAsset("z", model.AssetMember("x")), addMember("y", model.AccountMember("a"))),
		"after a failed asset with an invalid later member": interleaved(t,
			addAccount("a"), failingAsset("w", rule, model.AssetMember("missing")),
			addAccount("b"), addAsset("x", rule), addMember("x", model.AccountMember("b"))),
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := openTemp(t)
			mustSave(t, s, want)
			if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
				t.Fatal("loaded record differs from the saved one")
			}
		})
	}
}

func TestOpenReadOnly(t *testing.T) {
	s, path := openTemp(t)
	want := demodata.Family()
	mustSave(t, s, want)
	s.Close() // the -wal and -shm files may be gone now

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ro.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Error("read-only database loads differently")
	}
	if err := ro.Save(want); err == nil {
		t.Error("Save succeeded on a read-only store")
	}
	if err := ro.Void(1, "x"); err == nil {
		t.Error("Void succeeded on a read-only store")
	}
	ro.Close()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("read-only open changed the database file")
	}
}

func TestOpenReadOnlyRefusals(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.db")
	if _, err := OpenReadOnly(missing); err == nil {
		t.Error("opened a missing file")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("created the missing file")
	}

	for _, c := range []struct {
		version int
		want    error
	}{{len(migrations) - 1, ErrOldSchema}, {len(migrations) + 1, ErrFutureSchema}} {
		path := filepath.Join(dir, fmt.Sprintf("v%d.db", c.version))
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, c.version)); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if _, err := OpenReadOnly(path); !errors.Is(err, c.want) {
			t.Errorf("version %d: err = %v, want %v", c.version, err, c.want)
		}
	}
}

func TestTryCorrectAndTryVoidStoreNothing(t *testing.T) {
	s, _ := openTemp(t)
	mustSave(t, s, demodata.Family())
	snaps, err := s.Entries(demodata.Home)
	if err != nil || len(snaps) != 3 {
		t.Fatalf("home entries: %v, %v", snaps, err)
	}
	sn := snaps[1]
	if err := s.TryCorrect(sn.ID, Values{Date: sn.Date, Amount: 1}, "n"); err != nil {
		t.Fatal(err)
	}
	if err := s.TryVoid(sn.ID, "n"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Entry(sn.ID)
	if err != nil || got != sn {
		t.Errorf("after the tries the entry is %+v, %v; was %+v", got, err, sn)
	}
	revs, err := s.History(sn.ID)
	if err != nil || len(revs) != 1 {
		t.Errorf("history after the tries: %v, %v", revs, err)
	}

	// The tries judge as the real calls do.
	other := snaps[0]
	bad := Values{Date: other.Date, Amount: 1}
	for name, err := range map[string]error{
		"try":  s.TryCorrect(sn.ID, bad, ""),
		"real": s.Correct(sn.ID, bad, ""),
	} {
		if !errors.Is(err, ErrInvalidEntry) {
			t.Errorf("%s correction onto a used date: %v", name, err)
		}
	}
	if err := s.TryCorrect(9999, Values{}, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("try on an unknown entry: %v", err)
	}
	if err := s.TryVoid(9999, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("try void on an unknown entry: %v", err)
	}

	if err := s.Void(sn.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Entry(sn.ID); err != nil || !got.Void {
		t.Errorf("voided entry: %+v, %v", got, err)
	}
	if _, err := s.Entry(9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown entry: %v", err)
	}
}

func TestDefaultBalanceModRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	end := model.D(2024, 6, 30)
	p := want.Projection("New job")
	p.Mods = append(p.Mods,
		model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Home)}, Property: model.PropDefault, Values: model.MustSchedule(model.Segment{From: model.D(2024, 1, 1), End: &end, Value: 250000})},
		model.Modification{Target: model.ObjectRef{Kind: model.KindAsset, ID: "retirement"}, Property: model.PropDefault, Mode: model.ModeScale, Values: model.MustSchedule(model.Segment{From: model.D(2024, 1, 1), End: &end, Value: 0.8})},
	)
	mustSave(t, s, want)
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Fatal("default balance modifications did not round-trip")
	}
}
