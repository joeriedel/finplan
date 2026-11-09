// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"database/sql"
	"fmt"
)

// migrations are applied in order and never edited once released: migration n takes the schema from
// version n-1 to n. PRAGMA user_version holds the version applied.
var migrations = []string{
	// 1: the append-only record and the editable documents.
	`CREATE TABLE entries (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		account TEXT    NOT NULL,
		kind    TEXT    NOT NULL CHECK (kind IN ('snapshot', 'transaction')),
		date    INTEGER NOT NULL,
		amount  REAL    NOT NULL,
		tx_kind TEXT    NOT NULL DEFAULT '',
		counter TEXT    NOT NULL DEFAULT '',
		memo    TEXT    NOT NULL DEFAULT '',
		made_at INTEGER NOT NULL
	);
	CREATE INDEX entries_account_date ON entries (account, date);
	CREATE TABLE corrections (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		entry   INTEGER NOT NULL REFERENCES entries (id),
		date    INTEGER NOT NULL,
		amount  REAL    NOT NULL,
		tx_kind TEXT    NOT NULL DEFAULT '',
		counter TEXT    NOT NULL DEFAULT '',
		memo    TEXT    NOT NULL DEFAULT '',
		note    TEXT    NOT NULL DEFAULT '',
		made_at INTEGER NOT NULL
	);
	CREATE INDEX corrections_entry ON corrections (entry, id);
	CREATE TRIGGER entries_no_update BEFORE UPDATE ON entries
		BEGIN SELECT RAISE(ABORT, 'entries are append-only'); END;
	CREATE TRIGGER entries_no_delete BEFORE DELETE ON entries
		BEGIN SELECT RAISE(ABORT, 'entries are append-only'); END;
	CREATE TRIGGER corrections_no_update BEFORE UPDATE ON corrections
		BEGIN SELECT RAISE(ABORT, 'corrections are append-only'); END;
	CREATE TRIGGER corrections_no_delete BEFORE DELETE ON corrections
		BEGIN SELECT RAISE(ABORT, 'corrections are append-only'); END;
	CREATE TABLE documents (
		kind    TEXT    NOT NULL,
		key     TEXT    NOT NULL,
		ordinal INTEGER NOT NULL,
		format  INTEGER NOT NULL,
		body    BLOB    NOT NULL,
		PRIMARY KEY (kind, key)
	);`,
	// 2: view presets, kept apart from the documents because Save replaces those wholesale.
	`CREATE TABLE presets (
		name   TEXT    PRIMARY KEY,
		format INTEGER NOT NULL,
		body   BLOB    NOT NULL
	);`,
	// 3: a correction can void its entry, taking it out of the record without deleting it.
	`ALTER TABLE corrections ADD COLUMN void INTEGER NOT NULL DEFAULT 0;`,
}

// migrate brings the database up to schema version target in one transaction. It fails, changing
// nothing, if the database is already past what this build knows.
func migrate(db *sql.DB, target int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var v int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("%w: version %d, this build knows %d", ErrFutureSchema, v, len(migrations))
	}
	for ; v < target; v++ {
		if _, err := tx.Exec(migrations[v]); err != nil {
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
