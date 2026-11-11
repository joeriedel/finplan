// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package demodata

import (
	"fmt"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// rsuData is the invented restricted stock units of the Family record.
type rsuData struct {
	calendar *model.VestCalendar
	grants   []model.Grant
	vests    []model.RecordedVest
	prices   []model.PriceObservation
	// deposits are the brokerage transfers of the recorded vests' net proceeds, in the order of vests.
	deposits []model.Transaction
}

// vestPrices are the invented prices on the dates of the recorded vests, by grant and planned vest.
var vestPrices = map[int64][]float64{
	1: {196.20, 204.75, 211.30, 198.90, 215.40, 224.10, 229.55},
	2: {215.40, 224.10},
}

// otherPrices are invented observations between the vests, by date.
var otherPrices = []struct {
	date  model.Date
	price float64
}{
	{model.D(2024, time.December, 2), 181.00},
	{model.D(2025, time.January, 31), 190.50},
	{model.D(2025, time.July, 31), 207.25},
	{model.D(2026, time.January, 30), 208.00},
	{model.D(2026, time.October, 30), 232.40},
	{model.D(2026, time.November, 6), 233.10},
}

// familyRSU builds the employer's calendar (the third Monday of March, June and September and the second Monday of December) and two grants of ids 1 and 2 with their recorded vests: ids from 3 in order of grant and planned vest, then the prices, which are stored as the facts of those ids. The vests' TxEntry is set by addTo.
func familyRSU() *rsuData {
	d := &rsuData{calendar: &model.VestCalendar{Name: CalendarName, Rules: []model.VestRule{
		{Month: time.March, Nth: 3, Weekday: time.Monday},
		{Month: time.June, Nth: 3, Weekday: time.Monday},
		{Month: time.September, Nth: 3, Weekday: time.Monday},
		{Month: time.December, Nth: 2, Weekday: time.Monday},
	}}}
	specs := []struct {
		date, first model.Date
		value, px   float64
		years       []float64
	}{
		{model.D(2025, time.February, 10), model.D(2025, time.March, 17), 100000, 187.50, []float64{25, 25, 25, 25}},
		{model.D(2026, time.January, 12), model.D(2026, time.March, 16), 60000, 205.00, []float64{40, 30, 20, 10}},
	}
	for i, s := range specs {
		pct, err := model.GeneratePercents(d.calendar, s.first, s.years, 0)
		must(err)
		d.grants = append(d.grants, model.Grant{ID: int64(i + 1), Account: Equity, Symbol: Symbol, Date: s.date, Shares: model.SharesFor(s.value, s.px),
			Price: s.px, Value: s.value, Calendar: CalendarName, FirstVest: s.first, YearPercents: s.years, VestPercents: pct,
			Rates: model.DefaultWithholding, Brokerage: Brokerage})
	}
	id := int64(len(d.grants))
	for _, g := range d.grants {
		plan, err := g.Plan(d.calendar)
		must(err)
		for i, px := range vestPrices[g.ID] {
			id++
			v := model.RecordedVest{ID: id, Grant: g.ID, Planned: plan[i].Index, Date: plan[i].Date, Shares: plan[i].Shares, Price: px, Rates: g.Rates}
			d.vests = append(d.vests, v)
			d.prices = append(d.prices, model.PriceObservation{Symbol: Symbol, Date: v.Date, Price: px})
			d.deposits = append(d.deposits, model.Transaction{Date: v.Date, Amount: v.Net(), Kind: model.TxTransfer, Counter: Equity,
				Memo: fmt.Sprintf("%s vest of grant %d", Symbol, g.ID)})
		}
	}
	for _, o := range otherPrices {
		d.prices = append(d.prices, model.PriceObservation{Symbol: Symbol, Date: o.date, Price: o.price})
	}
	// The vests of grant 2 repeat the prices of grant 1's on the same dates; one observation each is enough.
	seen := map[model.Date]bool{}
	kept := d.prices[:0]
	for _, p := range d.prices {
		if !seen[p.Date] {
			seen[p.Date] = true
			kept = append(kept, p)
		}
	}
	d.prices = kept
	for i := range d.prices {
		id++
		d.prices[i].ID = id
	}
	return d
}

// addTo adds the equity account, the calendar and the prices to the record, whose accounts up to the brokerage are already in it. Each vest is tied to its deposit by the entry id the store will give the deposit when the record is saved into an empty database: entries are numbered from 1 in the order of the accounts, each account's snapshots before its transactions.
func (d *rsuData) addTo(rec *model.Record) {
	next := map[int]bool{}
	var base int64
	for _, a := range rec.Accounts {
		if a.ID != Brokerage {
			base += int64(len(a.Snapshots) + len(a.Transactions))
			continue
		}
		base += int64(len(a.Snapshots))
		for i := range d.vests {
			for j, t := range a.Transactions {
				if !next[j] && t == d.deposits[i] {
					next[j] = true
					d.vests[i].TxEntry = base + int64(j) + 1
					a.Transactions[j].Entry = d.vests[i].TxEntry
					break
				}
			}
		}
		break
	}
	a := &model.Account{ID: Equity, Name: "Acme RSUs", Type: model.TypeEquity, Tax: model.Taxable, Owner: "Parent A", StatedRate: 0.05, Grants: d.grants, Vests: d.vests}
	must(rec.AddAccount(a))
	rec.Calendars = append(rec.Calendars, d.calendar)
	for _, p := range d.prices {
		rec.Prices.Add(p)
	}
}
