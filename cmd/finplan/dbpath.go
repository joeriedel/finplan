// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/joeriedel/finplan/internal/store"
)

// databasePath returns where the user's database lives: $XDG_DATA_HOME/finplan/finplan.db, or
// ~/.local/share/finplan/finplan.db when XDG_DATA_HOME is unset, empty or not an absolute path (the XDG
// specification says to ignore a relative one). getenv and userHome are os.Getenv and os.UserHomeDir in
// the program.
func databasePath(getenv func(string) string, userHome func() (string, error)) (string, error) {
	if dir := getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "finplan", "finplan.db"), nil
	}
	home, err := userHome()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("home directory is unknown")
	}
	return filepath.Join(home, ".local", "share", "finplan", "finplan.db"), nil
}

// openDatabase opens the database at path, creating its folder (private to the user) and the database as needed.
func openDatabase(path string) (*store.Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return store.Open(path)
}
