// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package graph holds the window-free logic of the Graph tab: the line list and the projection switcher,
// series colours, sample times for a visible range, handoff and status spans, cursor lookup and
// the number and geometry helpers the drawing code needs. It imports no UI or cgo packages.
package graph

import (
	"slices"

	"github.com/joeriedel/finplan/internal/model"
)

// Color is an sRGB colour without alpha.
type Color struct{ R, G, B uint8 }

// RGB returns the colour of a 0xRRGGBB value.
func RGB(v uint32) Color { return Color{uint8(v >> 16), uint8(v >> 8), uint8(v)} }

// Lighten mixes the colour toward white by f, which is clamped to [0, 1].
func (c Color) Lighten(f float64) Color {
	f = max(0, min(1, f))
	mix := func(v uint8) uint8 { return uint8(float64(v) + (255-float64(v))*f + 0.5) }
	return Color{mix(c.R), mix(c.G), mix(c.B)}
}

// Floats returns the channels in [0, 1].
func (c Color) Floats() (r, g, b float32) {
	return float32(c.R) / 255, float32(c.G) / 255, float32(c.B) / 255
}

// Series colours.
var (
	ColorDebt       = RGB(0xE06C75)
	ColorCash       = RGB(0x98C379)
	ColorBrokerage  = RGB(0x61AFEF)
	ColorRetirement = RGB(0xC678DD)
	ColorTSP        = RGB(0xE5C07B)
	ColorSavings    = RGB(0x56B6C2)
	ColorEquity     = RGB(0xF08A4B)
	ColorTotal      = RGB(0xD8DEE9)
)

// kind groups account types that share a series colour.
type kind int

const (
	kindOther kind = iota
	kindCash
	kindSavings
	kindDebt
	kindBrokerage
	kindRetirement
	kindTSP
	kindEquity
)

func kindOf(t model.AccountType) kind {
	switch t {
	case model.TypeChecking:
		return kindCash
	case model.TypeSavings, model.Type529:
		return kindSavings
	case model.TypeCreditCard, model.TypeMortgage, model.TypeLoan:
		return kindDebt
	case model.TypeBrokerage:
		return kindBrokerage
	case model.Type401k, model.TypeIRA, model.TypeRothIRA, model.TypeHSA:
		return kindRetirement
	case model.TypeTSP:
		return kindTSP
	case model.TypeEquity:
		return kindEquity
	}
	return kindOther // property
}

func (k kind) color() Color {
	switch k {
	case kindCash:
		return ColorCash
	case kindSavings:
		return ColorSavings
	case kindDebt:
		return ColorDebt
	case kindBrokerage:
		return ColorBrokerage
	case kindRetirement:
		return ColorRetirement
	case kindTSP:
		return ColorTSP
	case kindEquity:
		return ColorEquity
	}
	return ColorTotal
}

// TypeColor returns the series colour of an account type. Checking is cash, savings and 529 share teal,
// credit cards and loans are debt, IRAs, 401k and HSAs are retirement, equity has its own colour, and property uses the totals colour.
func TypeColor(t model.AccountType) Color { return kindOf(t).color() }

// AssetColor returns the series colour of an asset: its account type's colour for a single account, and
// for an aggregate the colour its accounts share, where checking with savings counts as cash and the TSP
// with other retirement accounts as retirement. An aggregate of mixed kinds, or one that cannot be resolved, uses the totals colour.
func AssetColor(rec *model.Record, id model.AssetID) Color {
	res, err := rec.Resolve(id)
	if err != nil || len(res.Accounts) == 0 {
		return ColorTotal
	}
	if len(res.Accounts) == 1 {
		if a := rec.Account(res.Accounts[0]); a != nil {
			return TypeColor(a.Type)
		}
		return ColorTotal
	}
	kinds := map[kind]bool{}
	for _, aid := range res.Accounts {
		a := rec.Account(aid)
		if a == nil {
			return ColorTotal
		}
		kinds[kindOf(a.Type)] = true
	}
	only := func(ks ...kind) bool {
		for k := range kinds {
			if !slices.Contains(ks, k) {
				return false
			}
		}
		return true
	}
	switch {
	case len(kinds) == 1:
		for k := range kinds {
			return k.color()
		}
	case only(kindCash, kindSavings):
		return ColorCash
	case only(kindRetirement, kindTSP):
		return ColorRetirement
	}
	return ColorTotal
}
