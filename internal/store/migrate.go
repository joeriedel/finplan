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
	// 4: equity facts (grants, recorded vests, stock price observations), append-only like entries. A fact's body is a JSON document of its kind; a correction carries the whole new body, and the latest correction wins.
	`CREATE TABLE facts (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		kind    TEXT    NOT NULL CHECK (kind IN ('grant', 'vest', 'price')),
		body    BLOB    NOT NULL,
		made_at INTEGER NOT NULL
	);
	CREATE TABLE fact_corrections (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		fact    INTEGER NOT NULL REFERENCES facts (id),
		body    BLOB    NOT NULL,
		note    TEXT    NOT NULL DEFAULT '',
		void    INTEGER NOT NULL DEFAULT 0,
		made_at INTEGER NOT NULL
	);
	CREATE INDEX fact_corrections_fact ON fact_corrections (fact, id);
	CREATE TRIGGER facts_no_update BEFORE UPDATE ON facts
		BEGIN SELECT RAISE(ABORT, 'facts are append-only'); END;
	CREATE TRIGGER facts_no_delete BEFORE DELETE ON facts
		BEGIN SELECT RAISE(ABORT, 'facts are append-only'); END;
	CREATE TRIGGER fact_corrections_no_update BEFORE UPDATE ON fact_corrections
		BEGIN SELECT RAISE(ABORT, 'fact corrections are append-only'); END;
	CREATE TRIGGER fact_corrections_no_delete BEFORE DELETE ON fact_corrections
		BEGIN SELECT RAISE(ABORT, 'fact corrections are append-only'); END;`,
	// 5: the shipped market classes and their correlations, inserted into a database that has no classes; the bodies are format 7 documents.
	`INSERT INTO documents (kind, key, ordinal, format, body)
	SELECT * FROM (VALUES
		('class', 'us-large-cap', 0, 7, '{"id":"us-large-cap","name":"US large cap","return":[{"from":-2147483648,"end":null,"value":0.1}],"volatility":0.16}'),
		('class', 'us-small-cap', 1, 7, '{"id":"us-small-cap","name":"US small cap","return":[{"from":-2147483648,"end":null,"value":0.11}],"volatility":0.2}'),
		('class', 'international', 2, 7, '{"id":"international","name":"International","return":[{"from":-2147483648,"end":null,"value":0.08}],"volatility":0.18}'),
		('class', 'us-bonds', 3, 7, '{"id":"us-bonds","name":"US bonds","return":[{"from":-2147483648,"end":null,"value":0.045}],"volatility":0.06}'),
		('class', 'treasuries', 4, 7, '{"id":"treasuries","name":"Treasuries","return":[{"from":-2147483648,"end":null,"value":0.04}],"volatility":0.05}'),
		('class', 'cash', 5, 7, '{"id":"cash","name":"Cash","return":[{"from":-2147483648,"end":null,"value":0.03}],"volatility":0.01}'),
		('correlations', '', 0, 7, '[{"a":"international","b":"treasuries","value":-0.05},{"a":"international","b":"us-bonds","value":0.1},{"a":"international","b":"us-large-cap","value":0.75},{"a":"international","b":"us-small-cap","value":0.7},{"a":"treasuries","b":"us-bonds","value":0.9},{"a":"treasuries","b":"us-large-cap","value":-0.1},{"a":"treasuries","b":"us-small-cap","value":-0.15},{"a":"us-bonds","b":"us-large-cap","value":0.1},{"a":"us-bonds","b":"us-small-cap","value":0.05},{"a":"us-large-cap","b":"us-small-cap","value":0.85}]')
	) WHERE NOT EXISTS (SELECT 1 FROM documents WHERE kind = 'class');`,
	// 6: occurrence facts, the per-period records of budget items. The facts table is rebuilt to widen its kind check; the corrections are parked and rebuilt too, so no row references facts while it is dropped and foreign keys stay on.
	`CREATE TABLE facts_new (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		kind    TEXT    NOT NULL CHECK (kind IN ('grant', 'vest', 'price', 'occurrence')),
		body    BLOB    NOT NULL,
		made_at INTEGER NOT NULL
	);
	INSERT INTO facts_new (id, kind, body, made_at) SELECT id, kind, body, made_at FROM facts;
	CREATE TABLE fact_corrections_parked AS SELECT id, fact, body, note, void, made_at FROM fact_corrections;
	DROP TABLE fact_corrections;
	DROP TABLE facts;
	ALTER TABLE facts_new RENAME TO facts;
	CREATE TABLE fact_corrections (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		fact    INTEGER NOT NULL REFERENCES facts (id),
		body    BLOB    NOT NULL,
		note    TEXT    NOT NULL DEFAULT '',
		void    INTEGER NOT NULL DEFAULT 0,
		made_at INTEGER NOT NULL
	);
	INSERT INTO fact_corrections (id, fact, body, note, void, made_at) SELECT id, fact, body, note, void, made_at FROM fact_corrections_parked;
	DROP TABLE fact_corrections_parked;
	CREATE INDEX fact_corrections_fact ON fact_corrections (fact, id);
	CREATE TRIGGER facts_no_update BEFORE UPDATE ON facts
		BEGIN SELECT RAISE(ABORT, 'facts are append-only'); END;
	CREATE TRIGGER facts_no_delete BEFORE DELETE ON facts
		BEGIN SELECT RAISE(ABORT, 'facts are append-only'); END;
	CREATE TRIGGER fact_corrections_no_update BEFORE UPDATE ON fact_corrections
		BEGIN SELECT RAISE(ABORT, 'fact corrections are append-only'); END;
	CREATE TRIGGER fact_corrections_no_delete BEFORE DELETE ON fact_corrections
		BEGIN SELECT RAISE(ABORT, 'fact corrections are append-only'); END;`,
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
