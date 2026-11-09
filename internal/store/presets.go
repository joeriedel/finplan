// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// Preset is a view preset: an opaque document the store never interprets. Format is the version of
// the body's encoding, owned by whoever writes it.
type Preset struct {
	Name   string
	Format int
	Body   []byte
}

// PutPreset stores the preset, replacing any with the same name.
func (s *Store) PutPreset(name string, format int, body []byte) error {
	if name == "" || format < 1 {
		return errors.New("store: preset needs a name and a format version of at least 1")
	}
	if body == nil {
		body = []byte{}
	}
	_, err := s.db.Exec(`INSERT INTO presets (name, format, body) VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET format = excluded.format, body = excluded.body`, name, format, body)
	return err
}

// Preset returns the named preset, or an error matching ErrNotFound.
func (s *Store) Preset(name string) (Preset, error) {
	p := Preset{Name: name}
	err := s.db.QueryRow(`SELECT format, body FROM presets WHERE name = ?`, name).Scan(&p.Format, &p.Body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Preset{}, fmt.Errorf("%w: preset %q", ErrNotFound, name)
		}
		return Preset{}, err
	}
	return p, nil
}

// Presets returns every preset, sorted by name.
func (s *Store) Presets() ([]Preset, error) {
	rows, err := s.db.Query(`SELECT name, format, body FROM presets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Preset
	for rows.Next() {
		var p Preset
		if err := rows.Scan(&p.Name, &p.Format, &p.Body); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, rows.Err()
}

// DeletePreset removes the named preset; deleting one that does not exist is not an error.
func (s *Store) DeletePreset(name string) error {
	_, err := s.db.Exec(`DELETE FROM presets WHERE name = ?`, name)
	return err
}
