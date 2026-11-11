// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import "slices"

// Stocks are known by their symbol alone: a grant names one and price observations are kept under it. The only thing a stock has of its own is the growth schedule it is projected with, StockGrowth of the record; a stock without one projects flat (0%).

// StockGrowthOf returns the base growth schedule of the stock, empty if it has none.
func (r *Record) StockGrowthOf(symbol string) Schedule { return r.StockGrowth[symbol] }

// Symbols returns the symbols the record knows, from grants, price observations and stock growth schedules, sorted.
func (r *Record) Symbols() []string {
	var out []string
	for _, o := range r.Prices.All() {
		out = append(out, o.Symbol)
	}
	for _, a := range r.Accounts {
		for _, g := range a.Grants {
			out = append(out, g.Symbol)
		}
	}
	for s := range r.StockGrowth {
		out = append(out, s)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// HasStock reports whether a grant, a price observation or a stock growth schedule names the symbol.
func (r *Record) HasStock(symbol string) bool {
	_, ok := slices.BinarySearch(r.Symbols(), symbol)
	return ok
}
