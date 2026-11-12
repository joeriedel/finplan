// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import "github.com/joeriedel/finplan/internal/model"

// Shocks, rebalances and lots. A taxable account's holdings in each class are tracked as dated lots of units: a leg's price index starts at 1 on the path's start and follows the leg's growth and every shock, so a lot's value is its units at the price and it keeps its cost. A sale takes the oldest lots first and realizes the proceeds less the cost of the units sold.

// Skipped properties of a shock or rebalance.
const (
	propShock     = "shock"
	propRebalance = "rebalance"
)

// Order of events on one date: shocks, then a yearly rebalance, then the rebalances the projection names, the last of them winning.
const (
	eventShock = iota
	eventYearly
	eventRebalance
)

// legEvent is a shock of a class or a rebalance of the path's legs on a date.
type legEvent struct {
	date model.Date
	kind int
	// class and change are a shock's class and fraction.
	class  string
	change float64
	// target is a rebalance's share in each leg, in leg order.
	target []float64
}

// gain is the capital gain realized by the sales of one date, short-term (held a year or less) and long-term.
type gain struct {
	date        model.Date
	short, long float64
}

// lot is the units of a leg bought on one date, and what they cost.
type lot struct {
	date        model.Date
	units, cost float64
}

// lotBook is the lots of each leg, the oldest first.
type lotBook struct {
	lots [][]lot
}

// buy adds a lot of value's worth bought at the price on the date, unless the value or the price is not positive.
func (b *lotBook) buy(leg int, d model.Date, value, price float64) {
	if value <= 0 || price <= 0 {
		return
	}
	b.lots[leg] = append(b.lots[leg], lot{date: d, units: value / price, cost: value})
}

// sell sells value's worth at the price from the leg's oldest lots, and returns the short-term and long-term gain it realizes. A sale beyond what the lots hold realizes nothing on the excess.
func (b *lotBook) sell(leg int, d model.Date, value, price float64) (short, long float64) {
	if value <= 0 || price <= 0 {
		return 0, 0
	}
	units := value / price
	lots := b.lots[leg]
	n := 0
	for n < len(lots) && units > 0 {
		l := &lots[n]
		take := min(units, l.units)
		cost := l.cost * take / l.units
		g := take*price - cost
		y, m, day := l.date.YMD()
		if d <= model.D(y+1, m, day) {
			short += g
		} else {
			long += g
		}
		units -= take
		l.units -= take
		l.cost -= cost
		if l.units > 0 {
			break
		}
		n++
	}
	b.lots[leg] = lots[n:]
	return short, long
}

// gainsIn returns the capital gain the path realized in the calendar year.
func (p *path) gainsIn(year int) (short, long float64) {
	for _, g := range p.gains {
		if y, _, _ := g.date.YMD(); y == year {
			short += g.short
			long += g.long
		}
	}
	return short, long
}
