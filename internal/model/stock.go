// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import "slices"

// Stocks are known by their symbol alone: a grant names one and price observations are kept under it. A stock has two settings of its own: the growth schedule it is projected with, StockGrowth of the record (a stock without one projects flat, 0%), and the volatility its Monte Carlo price draws use, StockVolatility.

// StockGrowthOf returns the base growth schedule of the stock, empty if it has none.
func (r *Record) StockGrowthOf(symbol string) Schedule { return r.StockGrowth[symbol] }

// DefaultStockVolatility is the annual volatility of a stock that has none set.
const DefaultStockVolatility = 0.35

// StockVolatilityOf returns the annual volatility of the stock as a fraction, DefaultStockVolatility if none is set.
func (r *Record) StockVolatilityOf(symbol string) float64 {
	if v, ok := r.StockVolatility[symbol]; ok {
		return v
	}
	return DefaultStockVolatility
}

// Symbols returns the symbols the record knows, from grants, price observations, stock growth schedules and stock volatilities, sorted.
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
	for s := range r.StockVolatility {
		out = append(out, s)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// HasStock reports whether a grant, a price observation, a stock growth schedule or a stock volatility names the symbol.
func (r *Record) HasStock(symbol string) bool {
	_, ok := slices.BinarySearch(r.Symbols(), symbol)
	return ok
}
