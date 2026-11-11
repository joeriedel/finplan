// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"reflect"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// SetStockGrowth sets the base growth schedule of the stock, the annual rate its price is projected with when no projection modifies it, or clears it with nil so the stock projects flat. The symbol must be named by a grant, a price observation or an existing schedule (ErrUnknownTarget), and every rate must be a number above -1. Setting what is already set is ErrNoChange.
func SetStockGrowth(st *store.Store, rec *model.Record, symbol string, growth *model.Schedule, o Options) (*Result, error) {
	next := rec.Clone()
	if !next.HasStock(symbol) {
		return nil, refuse(ErrUnknownTarget, "there is no stock %q: no grant or price names it", symbol)
	}
	if next.StockGrowth == nil {
		next.StockGrowth = map[string]model.Schedule{}
	}
	if growth == nil || len(growth.Segments()) == 0 {
		delete(next.StockGrowth, symbol)
	} else {
		if err := checkSchedule(*growth); err != nil {
			return nil, err
		}
		for _, seg := range growth.Segments() {
			if seg.Value <= -1 {
				return nil, refuse(ErrInvalidValue, "a rate of %v must be above -1", seg.Value)
			}
		}
		next.StockGrowth[symbol] = *growth
	}
	if reflect.DeepEqual(rec.StockGrowth, next.StockGrowth) {
		return nil, ErrNoChange
	}
	return commit(st, next, Result{}, o)
}
