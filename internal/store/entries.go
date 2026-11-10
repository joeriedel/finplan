// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// EntryKind says whether an entry is a balance snapshot or a transaction.
type EntryKind string

// Entry kinds.
const (
	KindSnapshot    EntryKind = "snapshot"
	KindTransaction EntryKind = "transaction"
)

// Values are the contents of an entry. For a snapshot only Date and Amount (the balance) are used and
// TxKind, Counter and Memo must be empty.
type Values struct {
	Date    model.Date
	Amount  float64
	TxKind  model.TxKind
	Counter model.AccountID
	Memo    string
}

// Entry is a stored record entry with its current, corrected values.
type Entry struct {
	ID      int64
	Account model.AccountID
	Kind    EntryKind
	Values
	// Void is set for a voided entry, which only Entry returns.
	Void bool
}

// Revision is one version of an entry: the original, then each correction. Note is empty for the original.
// Void is set for a correction that voided the entry.
type Revision struct {
	Values
	MadeAt time.Time
	Note   string
	Void   bool
}

type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

const currentEntries = `SELECT e.id, e.account, e.kind,
		COALESCE(c.date, e.date), COALESCE(c.amount, e.amount), COALESCE(c.tx_kind, e.tx_kind),
		COALESCE(c.counter, e.counter), COALESCE(c.memo, e.memo), COALESCE(c.void, 0)
	FROM entries e LEFT JOIN corrections c ON c.id = (SELECT MAX(id) FROM corrections WHERE entry = e.id)`

// queryEntries returns the entries matching the condition (an SQL expression, which may be empty), in id
// order. Voided entries are left out unless withVoided is set.
func queryEntries(q querier, withVoided bool, cond string, args ...any) ([]Entry, error) {
	where := ""
	if !withVoided {
		where = "COALESCE(c.void, 0) = 0"
	}
	if cond != "" {
		if where != "" {
			where += " AND "
		}
		where += cond
	}
	if where != "" {
		where = "WHERE " + where
	}
	rows, err := q.Query(currentEntries+" "+where+" ORDER BY e.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Account, &e.Kind, &e.Date, &e.Amount, &e.TxKind, &e.Counter, &e.Memo, &e.Void); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Entries returns the account's entries in the order they were stored, with their current values.
// Voided entries are left out.
func (s *Store) Entries(account model.AccountID) ([]Entry, error) {
	return queryEntries(s.db, false, `e.account = ?`, string(account))
}

// EntriesWithVoided returns the account's entries in the order they were stored, with their current values, voided ones included (Void is set).
func (s *Store) EntriesWithVoided(account model.AccountID) ([]Entry, error) {
	return queryEntries(s.db, true, `e.account = ?`, string(account))
}

// HasEntries reports whether the account has any entry stored, a voided one included.
func (s *Store) HasEntries(account model.AccountID) (bool, error) {
	es, err := queryEntries(s.db, true, `e.account = ?`, string(account))
	return len(es) > 0, err
}

// EntriesNaming returns the entries, voided ones included, whose counter account is the given one, in the order they were stored.
func (s *Store) EntriesNaming(counter model.AccountID) ([]Entry, error) {
	return queryEntries(s.db, true, `COALESCE(c.counter, e.counter) = ?`, string(counter))
}

// withTx runs f in a transaction, which is committed if f succeeds and commit is set, and rolled back otherwise.
func (s *Store) withTx(commit bool, f func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	if !commit {
		return nil
	}
	return tx.Commit()
}

// Entry returns the entry with its current values, a voided entry included (Void is set). It returns ErrNotFound for an unknown id.
func (s *Store) Entry(id int64) (Entry, error) {
	es, err := queryEntries(s.db, true, `e.id = ?`, id)
	if err != nil {
		return Entry{}, err
	}
	if len(es) == 0 {
		return Entry{}, fmt.Errorf("%w: entry %d", ErrNotFound, id)
	}
	return es[0], nil
}

// Correct stores a correction of the entry: from now on the entry has the new values. The original and earlier corrections stay stored. A snapshot correction that would give the account two current snapshots on one date, a snapshot correction that sets a kind, counter or memo, and a transfer correction without a counter account are rejected with an error matching ErrInvalidEntry. Correcting a voided entry restores it.
func (s *Store) Correct(id int64, v Values, note string) error {
	return s.withTx(true, func(tx *sql.Tx) error { return s.correct(tx, id, v, note) })
}

// TryCorrect runs Correct, with the same checks, in a transaction that is rolled back: it reports what Correct would, and stores nothing.
func (s *Store) TryCorrect(id int64, v Values, note string) error {
	return s.withTx(false, func(tx *sql.Tx) error { return s.correct(tx, id, v, note) })
}

func (s *Store) correct(tx *sql.Tx, id int64, v Values, note string) error {
	es, err := queryEntries(tx, true, `e.id = ?`, id)
	if err != nil {
		return err
	}
	if len(es) == 0 {
		return fmt.Errorf("%w: entry %d", ErrNotFound, id)
	}
	e := es[0]
	switch e.Kind {
	case KindSnapshot:
		if v.TxKind != "" || v.Counter != "" || v.Memo != "" {
			return fmt.Errorf("%w: entry %d is a snapshot: kind, counter and memo must be empty", ErrInvalidEntry, id)
		}
		others, err := queryEntries(tx, false, `e.account = ?`, string(e.Account))
		if err != nil {
			return err
		}
		for _, o := range others {
			if o.ID != id && o.Kind == KindSnapshot && o.Date == v.Date {
				return fmt.Errorf("%w: account %q already has a snapshot on %v", ErrInvalidEntry, e.Account, v.Date)
			}
		}
	case KindTransaction:
		if v.TxKind == model.TxTransfer && v.Counter == "" {
			return fmt.Errorf("%w: entry %d: a transfer names no counter account", ErrInvalidEntry, id)
		}
	}
	_, err = tx.Exec(`INSERT INTO corrections (entry, date, amount, tx_kind, counter, memo, note, made_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, int32(v.Date), v.Amount, string(v.TxKind), string(v.Counter), v.Memo, note, s.now().UnixNano())
	return err
}

// Void stores a correction that takes the entry out of the record: Load and Entries leave it out, while
// the entry and its history stay stored. A later Correct restores it.
func (s *Store) Void(id int64, note string) error {
	return s.withTx(true, func(tx *sql.Tx) error { return s.void(tx, id, note) })
}

// TryVoid runs Void, with the same checks, in a transaction that is rolled back.
func (s *Store) TryVoid(id int64, note string) error {
	return s.withTx(false, func(tx *sql.Tx) error { return s.void(tx, id, note) })
}

func (s *Store) void(tx *sql.Tx, id int64, note string) error {
	es, err := queryEntries(tx, true, `e.id = ?`, id)
	if err != nil {
		return err
	}
	if len(es) == 0 {
		return fmt.Errorf("%w: entry %d", ErrNotFound, id)
	}
	v := es[0].Values
	_, err = tx.Exec(`INSERT INTO corrections (entry, date, amount, tx_kind, counter, memo, note, void, made_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		id, int32(v.Date), v.Amount, string(v.TxKind), string(v.Counter), v.Memo, note, s.now().UnixNano())
	return err
}

// History returns the entry's original values followed by each correction, including voids, in the order made.
func (s *Store) History(id int64) ([]Revision, error) {
	var revs []Revision
	read := func(query string, args ...any) error {
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r Revision
			var at int64
			if err := rows.Scan(&r.Date, &r.Amount, &r.TxKind, &r.Counter, &r.Memo, &r.Note, &r.Void, &at); err != nil {
				return err
			}
			r.MadeAt = time.Unix(0, at).UTC()
			revs = append(revs, r)
		}
		return rows.Err()
	}
	err := read(`SELECT date, amount, tx_kind, counter, memo, '', 0, made_at FROM entries WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(revs) == 0 {
		return nil, fmt.Errorf("%w: entry %d", ErrNotFound, id)
	}
	err = read(`SELECT date, amount, tx_kind, counter, memo, note, void, made_at FROM corrections WHERE entry = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	return revs, nil
}

// entryKey identifies an entry's content for matching the model against the stored record.
type entryKey struct {
	Kind EntryKind
	Values
}

// appendNew stores the account's entries from the model that are not yet stored and reports
// ErrRecordChanged if a stored entry is absent from the model. Matching is by value as a multiset:
// each stored entry (with its current values) consumes one equal model entry, and the leftover model
// entries are appended in model order. A leftover that equals a revision of an entry voided or corrected since
// the record was loaded (changed) is not appended: the model predates the change, so it is reported as ErrRecordChanged.
func (s *Store) appendNew(tx *sql.Tx, a *model.Account, stored, changed []Entry) error {
	unmatched := map[entryKey]int{}
	for _, e := range stored {
		unmatched[entryKey{e.Kind, e.Values}]++
	}
	changedKeys := map[entryKey]bool{}
	for _, e := range changed {
		changedKeys[entryKey{e.Kind, e.Values}] = true
	}
	insert := func(k entryKey) error {
		if unmatched[k] > 0 {
			unmatched[k]--
			return nil
		}
		if changedKeys[k] {
			return fmt.Errorf("%w: account %q has an entry voided or corrected since the record was loaded", ErrRecordChanged, a.ID)
		}
		_, err := tx.Exec(`INSERT INTO entries (account, kind, date, amount, tx_kind, counter, memo, made_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(a.ID), string(k.Kind), int32(k.Date), k.Amount, string(k.TxKind), string(k.Counter), k.Memo, s.now().UnixNano())
		return err
	}
	for _, sn := range a.Snapshots {
		if err := insert(entryKey{KindSnapshot, Values{Date: sn.Date, Amount: sn.Balance}}); err != nil {
			return err
		}
	}
	for _, t := range a.Transactions {
		k := entryKey{KindTransaction, Values{t.Date, t.Amount, t.Kind, t.Counter, t.Memo}}
		if err := insert(k); err != nil {
			return err
		}
	}
	for _, n := range unmatched {
		if n > 0 {
			return fmt.Errorf("%w: account %q", ErrRecordChanged, a.ID)
		}
	}
	return nil
}
