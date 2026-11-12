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
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
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
	rec.Classes = []model.MarketClass{
		{ID: "lc", Name: "Large cap", Return: model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(model.D(2030, time.January, 1)), Value: 0.07}, model.Segment{From: model.D(2031, time.January, 1), Value: 0.05}), Volatility: 0.15},
		{ID: "tr", Name: "Treasuries", Return: model.Constant(0.03), Volatility: 0.05},
		{ID: "flat", Name: "Flat"},
	}
	rec.Correlations = []model.Correlation{{A: "lc", B: "tr", Value: -0.2}}
	rec.Account(demodata.Brokerage).Allocation = []model.Holding{{Class: "lc", Percent: 60}, {Class: "tr", Percent: 40}}
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
		ID: "bonus", Name: "Bonus", Kind: model.ItemIncome, Amount: model.Constant(1000), Indexed: true, Growth: model.Constant(0.03), Account: demodata.Checking,
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
	if err := s.db.QueryRow(`SELECT MAX(format) FROM documents`).Scan(&format); err != nil || format != formatCurrent || formatCurrent != 10 {
		t.Fatalf("format written: %d, %v", format, err)
	}
	// Format 1 documents have no mode and no growth; a schedule of no growth is not stored.
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

// TestAccountDocumentWithOwnerLoads loads accounts whose documents still carry the retired owner field: the value is ignored and the next save drops it.
func TestAccountDocumentWithOwnerLoads(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	mustSave(t, s, want)
	if _, err := s.db.Exec(`UPDATE documents SET body = '{"owner":"Someone",' || substr(body, 2) WHERE kind = ?`, kindAccount); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Fatal("accounts with an owner load differently")
	}
	mustSave(t, s, want)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE kind = ? AND body LIKE '%owner%'`, kindAccount).Scan(&n); err != nil || n != 0 {
		t.Fatalf("account documents with an owner after a save: %d, %v", n, err)
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
	s.now = func() time.Time { return time.Date(2031, time.March, 9, 23, 30, 0, 0, time.Local) }
	today := model.D(2031, time.March, 9)
	want := model.NewRecord(today)
	want.Classes, want.Correlations = model.DefaultClasses(), model.DefaultCorrelations()
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("empty database should load as an empty record dated today with the shipped classes, got today %v", got.Today)
	}
}

func TestMigrationSeedsTheShippedClasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v4.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db, 4); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := mustLoad(t, s)
	if !reflect.DeepEqual(got.Classes, model.DefaultClasses()) || !reflect.DeepEqual(got.Correlations, model.DefaultCorrelations()) {
		t.Errorf("migrated database has classes %+v and correlations %+v", got.Classes, got.Correlations)
	}
}

func TestMigrationKeepsExistingClasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v4.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO documents (kind, key, ordinal, format, body) VALUES ('class', 'mine', 0, 6, '{"id":"mine","name":"Mine","return":0.07}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := mustLoad(t, s)
	want := []model.MarketClass{{ID: "mine", Name: "Mine", Return: model.Constant(0.07)}}
	if !reflect.DeepEqual(got.Classes, want) || got.Correlations != nil {
		t.Errorf("classes %+v, correlations %+v: a database with classes must keep them and get no others", got.Classes, got.Correlations)
	}
}

func storedToday(t *testing.T, s *Store) string {
	t.Helper()
	var body string
	if err := s.db.QueryRow(`SELECT body FROM documents WHERE kind = ?`, kindRecord).Scan(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// TestUnsetTodayFollowsTheClock: a stored zero today loads as the current date, a save keeps it stored as zero, and the next load gets the later date.
func TestUnsetTodayFollowsTheClock(t *testing.T) {
	s, _ := openTemp(t)
	s.now = func() time.Time { return time.Date(2031, time.March, 9, 23, 30, 0, 0, time.Local) }
	mustSave(t, s, model.NewRecord(0))
	if got := storedToday(t, s); got != `{"today":0}` {
		t.Fatalf("stored %s", got)
	}
	r := mustLoad(t, s)
	if want := model.D(2031, time.March, 9); r.Today != want {
		t.Fatalf("today %v, want %v", r.Today, want)
	}
	mustSave(t, s, r)
	if got := storedToday(t, s); got != `{"today":0}` {
		t.Fatalf("stored after save %s", got)
	}
	s.now = func() time.Time { return time.Date(2031, time.April, 2, 8, 0, 0, 0, time.Local) }
	if r = mustLoad(t, s); r.Today != model.D(2031, time.April, 2) {
		t.Fatalf("later load today %v", r.Today)
	}
}

// TestSetTodayLoadsUnchanged: a stored nonzero today is the record's today whatever the clock says, and a save writes it.
func TestSetTodayLoadsUnchanged(t *testing.T) {
	s, _ := openTemp(t)
	s.now = func() time.Time { return time.Date(2031, time.March, 9, 12, 0, 0, 0, time.Local) }
	want := demodata.Family()
	mustSave(t, s, want)
	r := mustLoad(t, s)
	if r.Today != demodata.FamilyToday {
		t.Fatalf("demo today %v", r.Today)
	}
	mustSave(t, s, r)
	if got := mustLoad(t, s); got.Today != demodata.FamilyToday {
		t.Fatalf("demo today after save %v", got.Today)
	}
	// A record dated by a caller after a defaulted load is that caller's date, not the unset one.
	s, _ = openTemp(t)
	mustSave(t, s, model.NewRecord(0))
	r = mustLoad(t, s)
	r.Today = model.D(2024, time.June, 1)
	mustSave(t, s, r)
	if got := mustLoad(t, s); got.Today != r.Today {
		t.Fatalf("saved today %v loaded as %v", r.Today, got.Today)
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
	copyFacts(t, s, srcPath)
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
	copyFacts(t, s, srcPath)
	var v int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Error("migrated database loads differently")
	}
	if err := s.Void(es[0].ID, ""); err != nil {
		t.Error(err)
	}
}

// copyFacts copies the equity facts of the database at srcPath into s, whose older schema the migration brought up to date: the older schemas had no facts, so the test of a migration supplies them afterwards.
func copyFacts(t *testing.T, s *Store, srcPath string) {
	t.Helper()
	for _, q := range []string{
		`ATTACH DATABASE '` + srcPath + `' AS srcfacts`,
		`INSERT INTO facts SELECT * FROM srcfacts.facts`,
		`INSERT INTO fact_corrections SELECT * FROM srcfacts.fact_corrections`,
		`DETACH DATABASE srcfacts`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrateFromVersion3(t *testing.T) {
	// A current database supplies the rows for the version 3 one: the entries and documents, which the version 4 migration leaves alone.
	src, srcPath := openTemp(t)
	mustSave(t, src, demodata.Family())
	es, _ := src.Entries(demodata.Home)
	if err := src.Correct(es[1].ID, Values{Date: es[1].Date, Amount: 111}, "typo"); err != nil {
		t.Fatal(err)
	}
	facts, err := src.Facts(FactPrice)
	if err != nil || len(facts) == 0 {
		t.Fatalf("demo prices: %v, %v", len(facts), err)
	}
	if err := src.CorrectFact(facts[0].ID, FactValue{Price: &model.PriceObservation{Symbol: "ACME", Date: facts[0].Price.Date, Price: 1.5}}, "typo"); err != nil {
		t.Fatal(err)
	}
	want := mustLoad(t, src)
	src.Close()

	path := filepath.Join(t.TempDir(), "v3.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db, 3); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ATTACH DATABASE '` + srcPath + `' AS src`,
		`INSERT INTO documents SELECT * FROM src.documents`,
		`INSERT INTO entries SELECT * FROM src.entries`,
		`INSERT INTO corrections SELECT * FROM src.corrections`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`SELECT * FROM main.facts`); err == nil {
		t.Fatal("facts table exists at version 3")
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var v int
	s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != len(migrations) {
		t.Errorf("user_version = %d, want %d", v, len(migrations))
	}
	copyFacts(t, s, srcPath)
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Error("migrated database loads differently")
	} else if !reflect.DeepEqual(got.Classes, model.DefaultClasses()) {
		t.Errorf("migrated classes = %v, want the shipped classes", got.Classes)
	}
	if err := s.VoidFact(facts[1].ID, "gone"); err != nil {
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

func TestClosedAccountRoundTrips(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	closed := model.D(2026, time.March, 1)
	rec.Accounts[0].Closed = &closed
	mustSave(t, s, rec)
	got := mustLoad(t, s)
	if c := got.Accounts[0].Closed; c == nil || *c != closed {
		t.Fatalf("Closed = %v, want %v", c, closed)
	}
	if !reflect.DeepEqual(got.Accounts, rec.Accounts) {
		t.Error("loaded accounts differ")
	}
	// Documents of format 3 have no close date and read as open accounts.
	if _, err := s.db.Exec(`UPDATE documents SET format = 3 WHERE kind = 'account' AND ordinal = 1`); err != nil {
		t.Fatal(err)
	}
	if c := mustLoad(t, s).Accounts[1].Closed; c != nil {
		t.Errorf("a format 3 account reads as closed on %v", c)
	}
	var body string
	if err := s.db.QueryRow(`SELECT body FROM documents WHERE kind = 'account' AND ordinal = 1`).Scan(&body); err != nil || strings.Contains(body, "closed") {
		t.Errorf("open account stores a close date: %s, %v", body, err)
	}
}

// TestFormat4BudgetDocumentsLoadUnchanged loads a hand-written format 4 database: budget items whose amount and growth are numbers, one of every legacy recurrence, an item with no start, and a projection that adds an item.
func TestFormat4BudgetDocumentsLoadUnchanged(t *testing.T) {
	s, _ := openTemp(t)
	mustSave(t, s, demodata.Family())
	n := func(y int, m time.Month, d int) int { return int(model.D(y, m, d)) }
	budget := fmt.Sprintf(`{"items": [
		{"id": "rent", "name": "Rent", "kind": "expense", "amount": 1500, "recurrence": {"kind": "monthly", "anchor": 0, "month": 0, "day": 31, "day2": 0}, "start": %d, "end": %d, "indexed": true, "growth": 0.03, "account": "checking", "from": "", "to": ""},
		{"id": "pay-a", "name": "Pay A", "kind": "income", "amount": 2000, "recurrence": {"kind": "semimonthly", "anchor": 0, "month": 0, "day": 15, "day2": 31}, "start": null, "end": null, "indexed": false, "account": "checking", "from": "", "to": ""},
		{"id": "pay-b", "name": "Pay B", "kind": "income", "amount": 1800, "recurrence": {"kind": "biweekly", "anchor": %d, "month": 0, "day": 0, "day2": 0}, "start": null, "end": null, "indexed": false, "growth": 0, "account": "checking", "from": "", "to": ""},
		{"id": "trip", "name": "Trip", "kind": "expense", "amount": 3000, "recurrence": {"kind": "annual", "anchor": 0, "month": 6, "day": 15, "day2": 0}, "start": null, "end": null, "indexed": false, "account": "checking", "from": "", "to": ""},
		{"id": "gift", "name": "Gift", "kind": "transfer", "amount": 250, "recurrence": {"kind": "one-time", "anchor": %d, "month": 0, "day": 0, "day2": 0}, "start": %d, "end": null, "indexed": false, "account": "", "from": "checking", "to": "outside"}
	]}`, n(2025, 1, 1), n(2030, 12, 31), n(2024, 11, 8), n(2027, 6, 1), n(2027, 6, 1))
	proj := fmt.Sprintf(`{"name": "New job", "parent": "", "start": null, "override_history": null, "asset_settings": [], "mods": [
		{"target": {"kind": "budget item", "id": "side"}, "property": "add_item", "values": null, "item": {"id": "side", "name": "Side", "kind": "income", "amount": 400, "recurrence": {"kind": "biweekly", "anchor": %d, "month": 0, "day": 0, "day2": 0}, "start": %d, "end": null, "indexed": false, "growth": 0.02, "account": "checking", "from": "", "to": ""}}
	], "shocks": [], "rebalances": []}`, n(2027, 1, 8), n(2027, 1, 8))
	for kind, body := range map[string]string{kindBudget: budget, kindProjection: proj} {
		if _, err := s.db.Exec(`UPDATE documents SET format = 4, body = ? WHERE kind = ? AND key = ?`, body, kind, map[string]string{kindBudget: "", kindProjection: "New job"}[kind]); err != nil {
			t.Fatal(err)
		}
	}
	got := mustLoad(t, s)
	at := func(y int, m time.Month, d int) *model.Date { return model.Ptr(model.D(y, m, d)) }
	want := []model.BudgetItem{
		{ID: "rent", Name: "Rent", Kind: model.ItemExpense, Amount: model.Constant(1500), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 31}, Start: at(2025, 1, 1), End: at(2030, 12, 31), Indexed: true, Growth: model.Constant(0.03), Account: "checking"},
		{ID: "pay-a", Name: "Pay A", Kind: model.ItemIncome, Amount: model.Constant(2000), Recurrence: model.Recurrence{Kind: model.Semimonthly, Day: 15, Day2: 31}, Account: "checking", IncomeType: model.IncomeWages},
		{ID: "pay-b", Name: "Pay B", Kind: model.ItemIncome, Amount: model.Constant(1800), Recurrence: model.Recurrence{Kind: model.Biweekly, Anchor: model.D(2024, 11, 8)}, Account: "checking", IncomeType: model.IncomeWages},
		{ID: "trip", Name: "Trip", Kind: model.ItemExpense, Amount: model.Constant(3000), Recurrence: model.Recurrence{Kind: model.Annual, Month: time.June, Day: 15}, Account: "checking"},
		{ID: "gift", Name: "Gift", Kind: model.ItemTransfer, Amount: model.Constant(250), Recurrence: model.Recurrence{Kind: model.OneTime, Anchor: model.D(2027, 6, 1)}, Start: at(2027, 6, 1), From: "checking", To: "outside"},
	}
	if !reflect.DeepEqual(got.Budget.Items, want) {
		t.Fatalf("budget items load as %+v, want %+v", got.Budget.Items, want)
	}
	added := got.Projection("New job").Mods[0].Item
	wantAdded := model.BudgetItem{ID: "side", Name: "Side", Kind: model.ItemIncome, Amount: model.Constant(400), Recurrence: model.Recurrence{Kind: model.Biweekly, Anchor: model.D(2027, 1, 8)}, Start: at(2027, 1, 8), Growth: model.Constant(0.02), Account: "checking", IncomeType: model.IncomeWages}
	if added == nil || !reflect.DeepEqual(*added, wantAdded) {
		t.Fatalf("added item loads as %+v, want %+v", added, wantAdded)
	}
	// The dates are the ones the legacy recurrences gave, items without a start included.
	after, upTo := model.D(2026, time.November, 9), model.D(2027, time.December, 31)
	if d := got.Budget.Items[2].Dates(after, upTo); d[0] != model.D(2026, time.November, 20) || len(d) != 30 {
		t.Errorf("biweekly: %d dates from %v", len(d), d[0])
	}
	if d := got.Budget.Items[1].Dates(after, upTo); len(d) != 28 || d[0] != model.D(2026, time.November, 15) || d[1] != model.D(2026, time.November, 30) {
		t.Errorf("semimonthly: %d dates, %v", len(d), d)
	}
	if d := got.Budget.Items[0].Dates(after, upTo); len(d) != 14 || d[0] != model.D(2026, time.November, 30) || d[3] != model.D(2027, time.February, 28) {
		t.Errorf("monthly on the 31st: %d dates, %v", len(d), d)
	}
	if d := got.Budget.Items[3].Dates(after, upTo); len(d) != 1 || d[0] != model.D(2027, time.June, 15) {
		t.Errorf("annual: %v", d)
	}
	if d := got.Budget.Items[4].Dates(after, upTo); len(d) != 1 || d[0] != model.D(2027, time.June, 1) {
		t.Errorf("one-time: %v", d)
	}
	// Saved again, it is a current-format document that loads the same.
	mustSave(t, s, got)
	var format int
	if err := s.db.QueryRow(`SELECT MAX(format) FROM documents`).Scan(&format); err != nil || format != formatCurrent {
		t.Fatalf("format written: %d, %v", format, err)
	}
	if again := mustLoad(t, s); !reflect.DeepEqual(again.Budget.Items, want) {
		t.Fatal("resaved items differ")
	}
}

func TestYearlyRebalanceRoundTrips(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	rec.Account(demodata.Brokerage).RebalanceYearly = true
	mustSave(t, s, rec)
	got := mustLoad(t, s)
	if !got.Account(demodata.Brokerage).RebalanceYearly || got.Account(demodata.K401).RebalanceYearly {
		t.Error("the yearly rebalance setting did not round-trip")
	}
}

func TestCheckSave(t *testing.T) {
	rec := demodata.Family()
	if err := CheckSave(rec); err != nil {
		t.Fatal(err)
	}
	rec.Projections = append(rec.Projections, &model.Projection{Name: rec.Projections[0].Name})
	if err := CheckSave(rec); err == nil || !strings.Contains(err.Error(), "cannot be restored") {
		t.Errorf("a record Load cannot rebuild: %v", err)
	}
	s, _ := openTemp(t)
	if err := s.Save(rec); err == nil || !strings.Contains(err.Error(), "cannot be restored") {
		t.Errorf("Save: %v", err)
	}
}

func TestFactsRoundTripCorrectVoidHistory(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	mustSave(t, s, want)
	got := mustLoad(t, s)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("Family with its grants, vests and prices loads differently")
	}
	// Each demo vest names the brokerage transfer of its net proceeds.
	for _, v := range got.Account(demodata.Equity).Vests {
		e, err := s.Entry(v.TxEntry)
		if err != nil || e.Account != demodata.Brokerage || e.Kind != KindTransaction || e.TxKind != model.TxTransfer ||
			e.Counter != demodata.Equity || e.Amount != v.Net() || e.Date != v.Date {
			t.Errorf("vest %d: entry %d = %+v, %v", v.ID, v.TxEntry, e, err)
		}
	}
	// The deposit stays linked to its vest when its entry is corrected to another date and amount.
	first := got.Account(demodata.Equity).Vests[0]
	e, err := s.Entry(first.TxEntry)
	if err != nil {
		t.Fatal(err)
	}
	moved := e.Values
	moved.Date, moved.Amount = moved.Date.Add(2), moved.Amount+0.37
	if err := s.Correct(e.ID, moved, "settled"); err != nil {
		t.Fatal(err)
	}
	linked := 0
	for _, tx := range mustLoad(t, s).Account(demodata.Brokerage).Transactions {
		if tx.Entry == first.TxEntry && tx.Date == moved.Date && tx.Amount == moved.Amount {
			linked++
		}
	}
	if linked != 1 {
		t.Errorf("%d transactions linked to the corrected deposit, want 1", linked)
	}
	if err := s.Correct(e.ID, e.Values, "undo"); err != nil {
		t.Fatal(err)
	}
	// A correction supersedes a fact, keeps the original, and a void takes it out of Load.
	v := got.Account(demodata.Equity).Vests[0]
	fixed := v
	fixed.Price = 1
	if err := s.CorrectFact(v.ID, FactValue{Vest: &fixed}, "typo"); err != nil {
		t.Fatal(err)
	}
	if err := s.CorrectFact(v.ID, FactValue{Price: &model.PriceObservation{}}, ""); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("correct with another kind: %v", err)
	}
	if err := s.VoidFact(v.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	if after := mustLoad(t, s); len(after.Account(demodata.Equity).Vests) != len(got.Account(demodata.Equity).Vests)-1 {
		t.Error("a voided vest is still loaded")
	}
	revs, err := s.FactHistory(v.ID)
	if err != nil || len(revs) != 3 || revs[0].Vest.Price != v.Price || revs[1].Vest.Price != 1 || revs[1].Note != "typo" || !revs[2].Void {
		t.Fatalf("history: %+v, %v", revs, err)
	}
	if err := s.CorrectFact(v.ID, FactValue{Vest: &fixed}, "back"); err != nil {
		t.Fatal(err)
	}
	if f, err := s.Fact(v.ID); err != nil || f.Void || f.Vest.Price != 1 {
		t.Errorf("restored fact: %+v, %v", f, err)
	}
	if _, err := s.Fact(9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown fact: %v", err)
	}
	// Facts are append-only.
	if _, err := s.db.Exec(`UPDATE facts SET body = x'00'`); err == nil {
		t.Error("a fact was updated")
	}
	if _, err := s.db.Exec(`DELETE FROM fact_corrections`); err == nil {
		t.Error("a fact correction was deleted")
	}
	// A record that lacks a stored fact, or has a changed one, is stale.
	if err := s.Save(got); !errors.Is(err, ErrRecordChanged) {
		t.Errorf("stale record with a corrected vest: %v", err)
	}
	// A new fact in the record that has no id is left to FactTx.Add.
	fresh := mustLoad(t, s)
	fresh.Prices.Add(model.PriceObservation{Symbol: "ACME", Date: 1, Price: 3})
	if err := s.Save(fresh); err != nil {
		t.Errorf("save with a pending price: %v", err)
	}
	if n := len(mustLoad(t, s).Prices.All()); n != len(fresh.Prices.All())-1 {
		t.Errorf("a price without an id was stored: %d", n)
	}
}

func TestCalendarDocumentRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	rec.Calendars[0].Overrides = []model.DateOverride{{From: model.D(2027, time.March, 15), To: model.D(2027, time.March, 16)}}
	mustSave(t, s, rec)
	if got := mustLoad(t, s); !reflect.DeepEqual(got, rec) {
		t.Error("calendar with an override loads differently")
	}
	rec.Calendars[0].Rules[0] = model.VestRule{Month: time.April, Day: 31}
	if err := s.Save(rec); err == nil {
		t.Error("a calendar with a rule on April 31 was saved")
	}
}

func TestStockGrowthAndFocalGrantRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	rec.StockGrowth[demodata.Symbol] = model.MustSchedule(
		model.Segment{From: model.MinDate, End: model.Ptr(model.D(2028, time.December, 31)), Value: 0.08},
		model.Segment{From: model.D(2029, time.January, 1), Value: 0.03})
	g := rec.Projection(demodata.ProjAcmeGrants).Mods[0].Grant
	g.Last, g.Rates, g.CliffMonths = model.Ptr(model.D(2030, time.March, 1)), &model.Withholding{Federal: 0.24, State: 0.05, FICA: 0.0765}, 3
	mustSave(t, s, rec)
	got := mustLoad(t, s)
	if !reflect.DeepEqual(got, rec) {
		t.Error("stock growth or focal grants load differently")
	}
	if got.Projection(demodata.ProjAcmeUp).Mods[0].Target.Kind != model.KindStock {
		t.Error("the stock modification lost its kind")
	}
	delete(rec.StockGrowth, demodata.Symbol)
	mustSave(t, s, rec)
	if got := mustLoad(t, s); len(got.StockGrowth) != 0 {
		t.Errorf("deleted stock growth is still stored: %v", got.StockGrowth)
	}
}

func TestHouseholdAndTaxTablesRoundTrip(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	want.Household.Filing = tax.Single
	want.Household.State = "FLAT"
	want.Household.Checking = demodata.Checking
	flat := tax.State{Name: "FLAT", Year: 2025, Brackets: []tax.Bracket{{Over: 0, Rate: 0.04}}, StandardDeduction: map[tax.FilingStatus]float64{tax.Single: 1000}}
	want.Tax.SetState(flat)
	want.Tax.Federal[1].Status[tax.MarriedJoint].Brackets[1].Rate = 0.13
	mustSave(t, s, want)
	if got := mustLoad(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded household %+v and tables differ from the saved ones", got.Household)
	}
}

func TestMemberBenefitsRoundTripAndOldMembersHaveNone(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	want.Household.Members[0].SSBenefit, want.Household.Members[0].SSClaimAge = 2500, 68
	want.Household.Members[1].FERSHigh3, want.Household.Members[1].FERSYears = 90000, 25
	want.Household.Members[1].FERSSeparationAge, want.Household.Members[1].FERSStartAge = 60, 60
	want.Household.Members[1].FERSSurvivor = model.SurvivorFull
	mustSave(t, s, want)
	if got := mustLoad(t, s); !reflect.DeepEqual(got.Household, want.Household) {
		t.Fatalf("loaded members %+v differ from the saved ones %+v", got.Household.Members, want.Household.Members)
	}
	old := fmt.Sprintf(`{"members":[{"name":"Old","birth":%d,"role":"earner"}],"filing":"mfj","state":"VA"}`, model.D(1980, 1, 2))
	if _, err := s.db.Exec(`UPDATE documents SET body = ?, format = 9 WHERE kind = ?`, old, kindHousehold); err != nil {
		t.Fatal(err)
	}
	m := mustLoad(t, s).Household.Members
	if len(m) != 1 || m[0].Name != "Old" || m[0].SSBenefit != 0 || m[0].HasFERS() {
		t.Errorf("a format 9 member loads as %+v", m)
	}
}

func TestRecordWithoutTaxTablesGetsTheShippedOnes(t *testing.T) {
	s, _ := openTemp(t)
	want := demodata.Family()
	want.Household = model.DefaultHousehold()
	mustSave(t, s, want)
	if _, err := s.db.Exec(`DELETE FROM documents WHERE kind IN (?, ?, ?)`, kindHousehold, kindTaxFederal, kindTaxState); err != nil {
		t.Fatal(err)
	}
	got := mustLoad(t, s)
	if !reflect.DeepEqual(got.Tax, tax.Defaults()) || !reflect.DeepEqual(got.Household, model.DefaultHousehold()) {
		t.Errorf("a record without tax documents loads with household %+v and %d federal tables", got.Household, len(got.Tax.Federal))
	}
	// A save writes the shipped tables, so the next load reads them back.
	mustSave(t, s, got)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE kind IN (?, ?)`, kindTaxFederal, kindTaxState).Scan(&n); err != nil || n != 6 {
		t.Errorf("%d tax table documents after a save, %v", n, err)
	}
}

func TestIncomeTypeOfLegacyItemsIsWages(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	rec.Budget.Items = []model.BudgetItem{
		{ID: "pay", Name: "Pay", Kind: model.ItemIncome, Amount: model.Constant(1), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Account: demodata.Checking},
		{ID: "interest", Name: "Interest", Kind: model.ItemIncome, Amount: model.Constant(1), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Account: demodata.Checking, IncomeType: model.IncomeOrdinary},
		{ID: "rent", Name: "Rent", Kind: model.ItemExpense, Amount: model.Constant(1), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Account: demodata.Checking},
	}
	mustSave(t, s, rec)
	if got := mustLoad(t, s); !reflect.DeepEqual(got.Budget.Items, rec.Budget.Items) {
		t.Fatalf("items load as %+v", got.Budget.Items)
	}
	// A document written before format 6 has no income type.
	if _, err := s.db.Exec(`UPDATE documents SET format = 5 WHERE kind = ?`, kindBudget); err != nil {
		t.Fatal(err)
	}
	var legacy string
	if err := s.db.QueryRow(`SELECT body FROM documents WHERE kind = ?`, kindBudget).Scan(&legacy); err != nil || strings.Contains(legacy, `"income_type":"wages"`) {
		t.Fatalf("the unset income type was stored: %s, %v", legacy, err)
	}
	items := mustLoad(t, s).Budget.Items
	if items[0].IncomeType != model.IncomeWages || items[1].IncomeType != model.IncomeOrdinary || items[2].IncomeType != "" {
		t.Errorf("income types %q, %q, %q", items[0].IncomeType, items[1].IncomeType, items[2].IncomeType)
	}
}

func TestMortgageTermsAndMatchesRoundTripAndFormat8Loads(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	mustSave(t, s, rec)
	got := mustLoad(t, s)
	if m := got.Account(demodata.Mortgage); m.OriginalAmount != 433600 || m.TermMonths != 360 || m.FirstPayment == nil || *m.FirstPayment != model.D(2021, time.February, 1) || m.PaidFrom != demodata.Checking {
		t.Errorf("mortgage terms %+v", m)
	}
	if it, _ := got.Budget.Item("salary-a"); it.MatchPercent != 50 || it.MatchUpTo != 6 || it.MatchAccount != demodata.K401 {
		t.Errorf("match %+v", it)
	}

	// A document of format 8 has none of the new fields and loads as an account without terms and an item without a match.
	old, _ := openTemp(t)
	want := demodata.Family()
	m := want.Account(demodata.Mortgage)
	m.OriginalAmount, m.TermMonths, m.FirstPayment, m.PaidFrom = 0, 0, nil, ""
	for i := range want.Budget.Items {
		want.Budget.Items[i].MatchPercent, want.Budget.Items[i].MatchUpTo, want.Budget.Items[i].MatchAccount = 0, 0, ""
	}
	mustSave(t, old, want)
	var withNew int
	if err := old.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE body LIKE '%original_amount%' OR body LIKE '%term_months%' OR body LIKE '%first_payment%' OR body LIKE '%paid_from%' OR body LIKE '%match_%'`).Scan(&withNew); err != nil || withNew != 0 {
		t.Fatalf("documents with new fields: %d, %v", withNew, err)
	}
	if _, err := old.db.Exec(`UPDATE documents SET format = 8`); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, old); !reflect.DeepEqual(got, want) {
		t.Fatal("the format 8 record loaded differently")
	}
}
