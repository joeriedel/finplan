// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"cmp"
	"slices"
	"sort"

	"github.com/joeriedel/finplan/internal/model"
)

// priceKey names a price path: a stock's, or, when the account has no recorded price of the stock on or before its start, that of one grant of it.
type priceKey struct {
	symbol string
	grant  int
	each   bool
}

// stockPrices is the price of each stock an equity account holds on any date under one projection, as seen by that account's path: up to the path's start the recorded price, and from the start the price the account's starting value used, grown by the stock's growth schedule. Each equity path has its own, so its vests, drift and deposits agree with its starting value.
type stockPrices struct {
	book   *model.PriceBook
	start  model.Date
	anchor map[priceKey]float64
	// from is, for a grant's key whose grant is dated after start, the date its price starts compounding from: its own.
	from   map[priceKey]model.Date
	growth map[string]model.Schedule
	// latest is, for a stock without a recorded price on or before start, the key of the account's latest grant of it dated on or before start, which the focal grants of the stock use.
	latest map[string]priceKey
}

// newStockPrices prices the stocks of the account's grants and the focal grants at the account's start: by the latest recorded price on or before start, or, with none, each grant by its own price, as the account's starting value does. The projection's growth modifications are layered over the stock's base growth, like the growth of an account.
func newStockPrices(rec *model.Record, a *model.Account, start model.Date, focal []model.FocalGrant, settings []model.Setting[rateLayer]) *stockPrices {
	s := &stockPrices{book: rec.Prices, start: start, anchor: map[priceKey]float64{}, from: map[priceKey]model.Date{}, growth: map[string]model.Schedule{}, latest: map[string]priceKey{}}
	var symbols []string
	for _, g := range a.Grants {
		symbols = append(symbols, g.Symbol)
	}
	for _, f := range focal {
		symbols = append(symbols, f.Symbol)
	}
	for _, sym := range symbols {
		if _, done := s.growth[sym]; done {
			continue
		}
		if p, ok := rec.Prices.At(sym, start); ok {
			s.anchor[priceKey{symbol: sym}] = p
		} else {
			last := model.Date(0)
			found := false
			for i, g := range a.Grants {
				if g.Symbol != sym {
					continue
				}
				k := priceKey{symbol: sym, grant: i, each: true}
				s.anchor[k] = g.Price
				if g.Date > start {
					s.from[k] = g.Date
				}
				if g.Date <= start && (!found || g.Date >= last) {
					last, found = g.Date, true
					s.latest[sym] = k
				}
			}
		}
		layered, _, _ := model.Apply(rateLayer{s: rec.StockGrowthOf(sym)}, settings, func(id model.AssetID) bool { return string(id) == sym }, rateLayer.over)
		s.growth[sym] = layered.s
	}
	return s
}

// key returns the price path of the account's i-th grant.
func (s *stockPrices) key(i int, g *model.Grant) priceKey {
	if _, ok := s.anchor[priceKey{symbol: g.Symbol}]; ok {
		return priceKey{symbol: g.Symbol}
	}
	return priceKey{symbol: g.Symbol, grant: i, each: true}
}

// focalKey returns the price path of a focal grant of the stock, and false if the account has no price for it.
func (s *stockPrices) focalKey(symbol string) (priceKey, bool) {
	if _, ok := s.anchor[priceKey{symbol: symbol}]; ok {
		return priceKey{symbol: symbol}, true
	}
	k, ok := s.latest[symbol]
	return k, ok
}

// at returns the price on d along the path, and false if the account has no price for it.
func (s *stockPrices) at(k priceKey, d model.Date) (float64, bool) {
	p, ok := s.anchor[k]
	if !ok {
		return 0, false
	}
	if d <= s.start {
		if r, ok := s.book.At(k.symbol, d); ok {
			return r, true
		}
		return p, true
	}
	from := s.start
	if f, ok := s.from[k]; ok {
		if d <= f {
			return p, true
		}
		from = f
	}
	return p * s.growth[k.symbol].Compound(from, d), true
}

// equityEvent is a grant or a vest of an equity account after its path starts.
type equityEvent struct {
	date    model.Date
	key     priceKey
	shares  int
	vest    bool
	rates   model.Withholding
	deposit model.AccountID
}

// equityPlan is what an equity account holds from its path's start and what happens to it after: the unvested shares per stock, and the grants and unrecorded vests to come, grants before vests on one date.
type equityPlan struct {
	account model.AccountID
	start   model.Date
	prices  *stockPrices
	held    map[priceKey]int
	events  []equityEvent
}

// newEquityPlan plans the account from start up to horizon. Its real grants dated after start and the focal grants dated after start (generated up to horizon, shares from the projected price on their dates) add their shares on their dates; every planned vest that no recorded vest dated on or before start fulfils happens on its date, or on the day after start if that date is not after start. A grant's vests never vest more than its shares that are left.
func newEquityPlan(rec *model.Record, a *model.Account, start, horizon model.Date, prices *stockPrices, focal []model.FocalGrant) *equityPlan {
	pl := &equityPlan{account: a.ID, start: start, prices: prices, held: map[priceKey]int{}}
	grants := slices.Clone(a.Grants)
	var keys []priceKey
	for i := range grants {
		keys = append(keys, prices.key(i, &grants[i]))
	}
	for _, f := range focal {
		for _, d := range f.Dates(horizon) {
			if d <= start {
				continue
			}
			k, ok := prices.focalKey(f.Symbol)
			if !ok {
				continue
			}
			price, ok := prices.at(k, d)
			if !ok {
				continue
			}
			if g, err := f.GrantOn(rec.Calendar(f.Calendar), d, price); err == nil {
				grants = append(grants, g)
				keys = append(keys, k)
			}
		}
	}
	for i := range grants {
		g := &grants[i]
		k := keys[i]
		left := g.Shares
		if g.Date <= start {
			left = max(0, left-a.VestedBy(g.ID, start))
			pl.held[k] += left
		} else {
			pl.events = append(pl.events, equityEvent{date: g.Date, key: k, shares: g.Shares})
		}
		cal := rec.Calendar(g.Calendar)
		if cal == nil {
			continue
		}
		vests, err := g.Plan(cal)
		if err != nil {
			continue
		}
		for _, pv := range vests {
			if slices.ContainsFunc(a.Vests, func(v model.RecordedVest) bool { return v.Grant == g.ID && v.Planned == pv.Index && v.Date <= start }) {
				continue
			}
			n := min(pv.Shares, left)
			if n <= 0 {
				continue
			}
			left -= n
			pl.events = append(pl.events, equityEvent{date: max(pv.Date, start.Add(1)), key: k, shares: n, vest: true, rates: g.Rates, deposit: g.Brokerage})
		}
	}
	sort.SliceStable(pl.events, func(i, j int) bool {
		a, b := pl.events[i], pl.events[j]
		if a.date != b.date {
			return a.date < b.date
		}
		return !a.vest && b.vest
	})
	return pl
}

// shares is the unvested shares on the price path held on d, after the events of d.
func (pl *equityPlan) shares(k priceKey, d model.Date) int {
	n := pl.held[k]
	for _, ev := range pl.events {
		if ev.key != k || ev.date > d {
			continue
		}
		if ev.vest {
			n -= ev.shares
		} else {
			n += ev.shares
		}
	}
	return n
}

// drift is the change in the account's value from..to that comes from the price moves of the shares held on from.
func (pl *equityPlan) drift(from, to model.Date) float64 {
	var keys []priceKey
	for k := range pl.held {
		keys = append(keys, k)
	}
	for _, ev := range pl.events {
		keys = append(keys, ev.key)
	}
	slices.SortFunc(keys, func(a, b priceKey) int {
		if c := cmp.Compare(a.symbol, b.symbol); c != 0 {
			return c
		}
		return cmp.Compare(a.grant, b.grant)
	})
	sum := 0.0
	for _, s := range slices.Compact(keys) {
		n := pl.shares(s, from)
		if n == 0 {
			continue
		}
		p0, _ := pl.prices.at(s, from)
		p1, _ := pl.prices.at(s, to)
		sum += float64(n) * (p1 - p0)
	}
	return sum
}

// changes returns the account's grants and vests as changes to its balance: a vest takes V out as the net proceeds, with the deposit account as counter, and the withholding as tax.
func (pl *equityPlan) changes() []change {
	var out []change
	for _, ev := range pl.events {
		price, _ := pl.prices.at(ev.key, ev.date)
		v := float64(ev.shares) * price
		if !ev.vest {
			out = append(out, change{date: ev.date, cause: CauseGrant, amount: v, rank: rankGrant})
			continue
		}
		w := v * ev.rates.Sum()
		out = append(out,
			change{date: ev.date, cause: CauseVest, amount: -(v - w), counter: ev.deposit, rank: rankVest},
			change{date: ev.date, cause: CauseTax, amount: -w, rank: rankVest})
	}
	return out
}

// deposits returns the net proceeds of the plan's vests paid into the account after the date, as transfers from the equity account.
func (pl *equityPlan) deposits(into model.AccountID, after model.Date) []change {
	var out []change
	for _, ev := range pl.events {
		if !ev.vest || ev.deposit != into || ev.date <= after || into == pl.account {
			continue
		}
		price, _ := pl.prices.at(ev.key, ev.date)
		v := float64(ev.shares) * price
		out = append(out, change{date: ev.date, cause: CauseTransfer, amount: v - v*ev.rates.Sum(), counter: pl.account, rank: rankTransfer})
	}
	return out
}
