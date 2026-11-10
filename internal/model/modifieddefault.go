// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

// ModifiedDefault is a default balance with default modifications layered over it, in the order applied, the last applied winning. It never changes Base.
type ModifiedDefault struct {
	Base DefaultBalance
	Mods []Modification
	// Opens, if set, is the date a projection-defined account opens; nothing is covered before it.
	Opens *Date
}

// Covers returns the default on d and false if none covers it. A set modification gives its value on the dates its segments cover. A scale modification multiplies the default beneath it and has no effect where none covers d.
func (m ModifiedDefault) Covers(d Date) (float64, bool) {
	if m.Opens != nil && d < *m.Opens {
		return 0, false
	}
	return m.covers(d, len(m.Mods))
}

func (m ModifiedDefault) covers(d Date, n int) (float64, bool) {
	if n == 0 {
		return m.Base.Covers(d)
	}
	mod := m.Mods[n-1]
	f, ok := mod.Values.At(d)
	if !ok {
		return m.covers(d, n-1)
	}
	if !mod.IsScale() {
		return f, true
	}
	v, ok := m.covers(d, n-1)
	return v * f, ok
}

// At returns the default balance on d: the covering value, else 0.
func (m ModifiedDefault) At(d Date) float64 {
	v, _ := m.Covers(d)
	return v
}

// AccountOfDefaultAsset returns the account an asset id names if it is an account's default asset.
func AccountOfDefaultAsset(id AssetID) (AccountID, bool) { return accountOfDefault(id) }
