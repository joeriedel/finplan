// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestTimingAndOccurrencesRoundTripAndFormat13ItemsLoadWithTheDefault(t *testing.T) {
	s, _ := openTemp(t)
	rec := demodata.Family()
	mustSave(t, s, rec)
	got := mustLoad(t, s)
	for _, c := range []struct {
		id     string
		timing model.Timing
	}{{"groceries", model.Spread}, {"insurance", model.Lump}, {"salary-a", model.Lump}, {"save-401k", model.Lump}} {
		if it, _ := got.Budget.Item(c.id); it.Timing != c.timing {
			t.Errorf("%s loaded with timing %q, want %q", c.id, it.Timing, c.timing)
		}
	}
	if !reflect.DeepEqual(got.Occurrences.All(), rec.Occurrences.All()) || len(got.Occurrences.All()) != 1 {
		t.Errorf("occurrences %+v, want %+v", got.Occurrences.All(), rec.Occurrences.All())
	}

	// A document of format 13 has no timing: its items read as their defaults, expenses that recur monthly or more often as spread.
	old, _ := openTemp(t)
	want := demodata.Family()
	for i := range want.Budget.Items {
		want.Budget.Items[i].Timing = ""
	}
	mustSave(t, old, want)
	var withNew int
	if err := old.db.QueryRow(`SELECT COUNT(*) FROM documents WHERE body LIKE '%"timing"%'`).Scan(&withNew); err != nil || withNew != 0 {
		t.Fatalf("documents with timing: %d, %v", withNew, err)
	}
	if _, err := old.db.Exec(`UPDATE documents SET format = 13`); err != nil {
		t.Fatal(err)
	}
	loaded := mustLoad(t, old)
	if !reflect.DeepEqual(loaded.Budget, want.Budget) {
		t.Fatal("the format 13 budget loaded differently")
	}
	for id, timing := range map[string]model.Timing{"groceries": model.Spread, "transport": model.Spread, "insurance": model.Lump, "vacation": model.Lump, "salary-b": model.Lump, "save-roth": model.Lump} {
		it, _ := loaded.Budget.Item(id)
		if it.EffectiveTiming() != timing {
			t.Errorf("%s reads as %q, want %q", id, it.EffectiveTiming(), timing)
		}
	}
}

func TestOccurrenceFactKindNeedsMigrationSix(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.db.Exec(`INSERT INTO facts (kind, body, made_at) VALUES ('occurrence', '{}', 0)`); err != nil {
		t.Fatalf("the facts table refuses the occurrence kind: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO facts (kind, body, made_at) VALUES ('nothing', '{}', 0)`); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("the facts table accepts an unknown kind: %v", err)
	}
}

func TestMigrationSixKeepsFactsAndTheirCorrections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db, 5); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO facts (kind, body, made_at) VALUES ('price', '{"symbol":"ACME","date":1,"price":2}', 1)`,
		`INSERT INTO fact_corrections (fact, body, note, void, made_at) VALUES (1, '{"symbol":"ACME","date":1,"price":3}', 'typo', 0, 2)`,
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
	fs, err := s.Facts(FactPrice)
	if err != nil || len(fs) != 1 || fs[0].ID != 1 || fs[0].Price.Price != 3 {
		t.Fatalf("facts after the migration: %+v, %v", fs, err)
	}
	if revs, err := s.FactHistory(1); err != nil || len(revs) != 2 {
		t.Errorf("history after the migration: %+v, %v", revs, err)
	}
	var violations int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Errorf("foreign key violations %d, %v", violations, err)
	}
	if _, err := s.db.Exec(`UPDATE facts SET kind = 'vest'`); err == nil {
		t.Error("facts can be updated after the migration")
	}
	if _, err := s.db.Exec(`DELETE FROM facts`); err == nil {
		t.Error("facts can be deleted after the migration")
	}
	var id int64
	if err := s.db.QueryRow(`INSERT INTO facts (kind, body, made_at) VALUES ('occurrence', '{}', 3) RETURNING id`).Scan(&id); err != nil || id != 2 {
		t.Errorf("next fact id %d, %v", id, err)
	}
}

func TestSecondOccurrenceOfOnePeriodIsRefusedAndStoredDuplicatesStillSave(t *testing.T) {
	s, _ := openTemp(t)
	period := model.D(2026, time.November, 3)
	occ := func(posted float64) FactValue {
		return FactValue{Occurrence: &model.Occurrence{Item: "groceries", Period: period, Status: model.StatusAuto, Posted: model.Ptr(posted)}}
	}
	if err := s.Write(nil, func(f FactTx) error { _, err := f.Add(occ(100)); return err }); err != nil {
		t.Fatal(err)
	}
	err := s.Write(nil, func(f FactTx) error { _, err := f.Add(occ(200)); return err })
	if !errors.Is(err, ErrRecordChanged) || !strings.Contains(err.Error(), "already records") {
		t.Fatalf("a second occurrence of the period: %v", err)
	}
	if fs, err := s.Facts(FactOccurrence); err != nil || len(fs) != 1 {
		t.Fatalf("facts after the refusal: %+v, %v", fs, err)
	}

	// Duplicates that were stored before the check exist: the latest wins and the record still saves.
	body, err := encodeFact(occ(300))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO facts (kind, body, made_at) VALUES ('occurrence', ?, 5)`, body); err != nil {
		t.Fatal(err)
	}
	rec := mustLoad(t, s)
	if o, ok := rec.Occurrences.Get("groceries", period); !ok || *o.Posted != 300 {
		t.Fatalf("the occurrence read: %+v, %v", o, ok)
	}
	mustSave(t, s, rec)
}
