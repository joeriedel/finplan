// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package store saves and loads a model.Record to and from a SQLite database.
//
// The record proper, balance snapshots and transactions, is append-only: rows are never updated or
// deleted, and a change is stored as a correction that supersedes the entry while the original stays.
// Everything else (accounts, assets, the budget, projections, classes, globals) is a versioned JSON
// document that Save replaces freely. View presets are opaque documents the store never interprets.
// The schema is versioned with PRAGMA user_version and migrated forward on Open.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Errors matched with errors.Is.
var (
	// ErrRecordChanged is returned by Save when the model alters or drops an entry already stored.
	// Changes to stored entries go through Correct.
	ErrRecordChanged = errors.New("store: record entries changed outside the correction API")
	// ErrFutureSchema is returned by Open for a database written by a newer schema version.
	ErrFutureSchema = errors.New("store: database schema is newer than this build")
	// ErrUnknownFormat is returned for a stored document whose format version this build cannot read.
	ErrUnknownFormat = errors.New("store: unknown document format version")
	// ErrNotFound is returned for an entry or preset that does not exist.
	ErrNotFound = errors.New("store: not found")
)

// Store is an open database. It is safe for concurrent use.
type Store struct {
	db   *sql.DB
	path string
	now  func() time.Time

	mu       sync.Mutex // guards loaded and loadMark
	loaded   bool       // whether Load has run
	loadMark int64      // highest corrections.id seen by the most recent Load
}

// Open opens the database at path, creating it if needed and migrating it to the current schema.
func Open(path string) (*Store, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// One connection keeps the pragmas in force and avoids lock contention within the process.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path, now: time.Now}
	if err := s.init(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) init() error {
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		return err
	}
	if mode != "wal" {
		return fmt.Errorf("journal mode is %q, want wal", mode)
	}
	var fk int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		return err
	}
	if fk != 1 {
		return errors.New("foreign keys are off")
	}
	return migrate(s.db, len(migrations))
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
