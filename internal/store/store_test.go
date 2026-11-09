// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
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
		ID: "bonus", Name: "Bonus", Kind: model.ItemIncome, Amount: 1000, Indexed: true, Account: demodata.Checking,
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

	// Write data through the version 1 schema, as an older build would have.
	s1 := &Store{now: time.Now}
	s1.db, _ = sql.Open("sqlite", path)
	s1.db.SetMaxOpenConns(1)
	want := demodata.Family()
	mustSave(t, s1, want)
	s1.Close()

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
