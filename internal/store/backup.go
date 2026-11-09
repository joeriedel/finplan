// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataVersion returns SQLite's data version for the store's connection. It changes when another
// connection commits to the database and not for the store's own writes, so a changed value means
// another program wrote.
func (s *Store) DataVersion() (int64, error) {
	var v int64
	err := s.db.QueryRow(`PRAGMA data_version`).Scan(&v)
	return v, err
}

// Backup writes a consistent copy of the database to path, replacing any file already there. The copy
// is built beside path and renamed over it, so a failure leaves an existing file untouched.
// It refuses the store's own database file.
func (s *Store) Backup(path string) error {
	if target, err := os.Stat(path); err == nil {
		if own, err := os.Stat(s.path); err == nil && os.SameFile(target, own) {
			return fmt.Errorf("store: backup target %s is the open database", path)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".finplan-backup-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Close(); err != nil {
		return err
	}
	// VACUUM INTO refuses a file that is not empty; an empty one, as CreateTemp made, is accepted.
	if _, err := s.db.Exec(`VACUUM INTO ?`, name); err != nil {
		return err
	}
	return os.Rename(name, path)
}
