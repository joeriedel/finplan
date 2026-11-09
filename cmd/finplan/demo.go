// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/store"
)

// openDemo creates a throwaway database holding the invented Family record and its view presets. The returned
// function closes the database and removes it.
func openDemo() (*store.Store, func(), error) {
	dir, err := os.MkdirTemp("", "finplan-demo-")
	if err != nil {
		return nil, nil, err
	}
	st, err := seedDemo(filepath.Join(dir, "demo.db"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	return st, func() {
		st.Close()
		os.RemoveAll(dir)
	}, nil
}

func seedDemo(path string) (*store.Store, error) {
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	rec := demodata.Family()
	err = st.Save(rec)
	for _, p := range familyPresets(rec) {
		if err != nil {
			break
		}
		var body []byte
		if body, err = graph.EncodePreset(p); err == nil {
			err = st.PutPreset(p.Name, graph.PresetFormat, body)
		}
	}
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("seed demo database: %w", err)
	}
	return st, nil
}
