// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDatabasePath(t *testing.T) {
	home := func() (string, error) { return "/home/ann", nil }
	noHome := func() (string, error) { return "", errors.New("no home") }
	tests := []struct {
		name string
		xdg  string
		home func() (string, error)
		want string
		fail bool
	}{
		{"xdg", "/data", home, "/data/finplan/finplan.db", false},
		{"xdg needs no home", "/data", noHome, "/data/finplan/finplan.db", false},
		{"unset", "", home, "/home/ann/.local/share/finplan/finplan.db", false},
		{"relative", "data", home, "/home/ann/.local/share/finplan/finplan.db", false},
		{"no home", "", noHome, "", true},
	}
	for _, tt := range tests {
		got, err := databasePath(func(string) string { return tt.xdg }, tt.home)
		if (err != nil) != tt.fail || got != tt.want {
			t.Errorf("%s: got %q, %v; want %q (fail %v)", tt.name, got, err, tt.want, tt.fail)
		}
	}
}

func TestOpenDatabaseCreatesFolderAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share", "finplan", "finplan.db")
	st, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database not created: %v", err)
	}
	fi, err := os.Stat(filepath.Dir(path))
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("folder: %v, %v", fi, err)
	}
}
