// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"
)

// PriceObservation is the price of one share of a stock on a date. When a symbol has several observations on one date, the one with the highest ID, the latest entered, counts.
type PriceObservation struct {
	ID     int64
	Symbol string
	Date   Date
	Price  float64
}

// PriceBook is the price observations of every stock, ordered by symbol, date and ID. The zero value and a nil book hold none.
type PriceBook struct {
	obs []PriceObservation
}

// Add inserts an observation in order.
func (b *PriceBook) Add(o PriceObservation) {
	i := sort.Search(len(b.obs), func(i int) bool { return priceLess(o, b.obs[i]) })
	b.obs = slices.Insert(b.obs, i, o)
}

func priceLess(a, b PriceObservation) bool {
	if a.Symbol != b.Symbol {
		return a.Symbol < b.Symbol
	}
	if a.Date != b.Date {
		return a.Date < b.Date
	}
	return a.ID < b.ID
}

// All returns every observation in order. The caller must not change it.
func (b *PriceBook) All() []PriceObservation {
	if b == nil {
		return nil
	}
	return b.obs
}

// Symbol returns the observations of the symbol in date order.
func (b *PriceBook) Symbol(symbol string) []PriceObservation {
	var out []PriceObservation
	for _, o := range b.All() {
		if o.Symbol == symbol {
			out = append(out, o)
		}
	}
	return out
}

// At returns the symbol's latest price on or before d, and false if it has none by then.
func (b *PriceBook) At(symbol string, d Date) (float64, bool) {
	o := b.All()
	i := sort.Search(len(o), func(i int) bool { return o[i].Symbol > symbol || (o[i].Symbol == symbol && o[i].Date > d) }) - 1
	if i < 0 || o[i].Symbol != symbol {
		return 0, false
	}
	return o[i].Price, true
}

func (b *PriceBook) clone() *PriceBook {
	if b == nil {
		return nil
	}
	return &PriceBook{obs: slices.Clone(b.obs)}
}

// Withholding is the shares of a vest's value that are withheld, as fractions: 0.22 is 22%.
type Withholding struct {
	Federal, State, FICA float64
}

// DefaultWithholding is the rates a grant starts with: 22% federal, 5.75% Virginia and 7.65% FICA.
var DefaultWithholding = Withholding{Federal: 0.22, State: 0.0575, FICA: 0.0765}

// Sum returns the combined rate.
func (w Withholding) Sum() float64 { return w.Federal + w.State + w.FICA }

// Grant is a grant of restricted stock units to an equity account: Shares of one stock on a date, vesting by the calendar and percentages it keeps. The grant price is for reference only; each vest is valued at the stock's price on its vest date.
type Grant struct {
	// ID is the grant's stored fact id; 0 before it is stored.
	ID      int64
	Account AccountID
	Symbol  string
	Date    Date
	Shares  int
	Price   float64
	// Value is the dollar value the grant was entered as, with Shares its floor over Price; 0 if it was entered as shares.
	Value float64
	// Calendar names the vest calendar. FirstVest is the first date the shares vest on, a date of the calendar.
	Calendar  string
	FirstVest Date
	// YearPercents is the percent of the shares that vest in each year from FirstVest, and CliffMonths the months from the first vest's month in which vests are held and paid together on the first vest after.
	YearPercents []float64
	CliffMonths  int
	// VestPercents is the percent of the shares each planned vest pays, in date order; they sum to 100. They are generated from YearPercents and may then be edited one by one.
	VestPercents []float64
	Rates        Withholding
	// Brokerage is the brokerage account that net proceeds are deposited in.
	Brokerage AccountID
}

// RecordedVest is a vest that happened: the shares that vested on Date at Price, the tax withheld and the deposit of the proceeds.
type RecordedVest struct {
	// ID is the vest's stored fact id; 0 before it is stored.
	ID    int64
	Grant int64
	// Planned is the 1-based number of the planned vest this fulfils.
	Planned int
	Date    Date
	Shares  int
	Price   float64
	Rates   Withholding
	// Withheld is the amount withheld when it was entered instead of computed from Rates; nil otherwise.
	Withheld *float64
	// TxEntry is the id of the brokerage account's entry that deposited the net proceeds.
	TxEntry int64
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

// Gross returns the value of the vested shares, V = shares x price, to the cent.
func (v RecordedVest) Gross() float64 { return round2(float64(v.Shares) * v.Price) }

// WithheldAmount returns W: the entered amount, or V x the sum of the rates, to the cent.
func (v RecordedVest) WithheldAmount() float64 {
	if v.Withheld != nil {
		return *v.Withheld
	}
	return round2(v.Gross() * v.Rates.Sum())
}

// Net returns the proceeds, N = V - W.
func (v RecordedVest) Net() float64 { return round2(v.Gross() - v.WithheldAmount()) }

// SharesFor returns the shares a grant of the value buys at the price: the whole shares, rounded down.
func SharesFor(value, price float64) int {
	return int(math.Floor(value/price + 1e-9))
}

// PlannedVest is one vest of a grant's schedule.
type PlannedVest struct {
	// Index is the 1-based number of the vest in date order.
	Index   int
	Date    Date
	Percent float64
	Shares  int
}

// monthsFrom counts the calendar months from first's month to d's.
func monthsFrom(first, d Date) int {
	fy, fm, _ := first.YMD()
	y, m, _ := d.YMD()
	return (y-fy)*12 + int(m-fm)
}

// vestDates returns the calendar's dates from first for the years. The first vest must be a date of the calendar.
func vestDates(cal *VestCalendar, first Date, years int) ([]Date, error) {
	if years < 1 {
		return nil, errors.New("a grant vests over at least one year")
	}
	if !cal.Has(first) {
		return nil, fmt.Errorf("%v is not a vest date of calendar %q", first, cal.Name)
	}
	fy, fm, _ := first.YMD()
	end := D(fy, fm+time.Month(12*years), 1).Add(-1)
	return cal.Dates(first, end), nil
}

// cliffStart returns the index of the first date that pays: the first at least cliff months after the first vest's month.
func cliffStart(dates []Date, cliff int) (int, error) {
	if cliff < 0 {
		return 0, errors.New("a cliff cannot be negative")
	}
	for i, d := range dates {
		if monthsFrom(dates[0], d) >= cliff {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no vest falls on or after the %d month cliff", cliff)
}

// PlanDates returns the dates of the planned vests of a grant that vests by the calendar from first, over the years, with the cliff: the calendar dates, except that the vests before the cliff are paid with the first vest on or after it, as one planned vest dated on that vest's date.
func PlanDates(cal *VestCalendar, first Date, years, cliff int) ([]Date, error) {
	dates, err := vestDates(cal, first, years)
	if err != nil {
		return nil, err
	}
	j, err := cliffStart(dates, cliff)
	if err != nil {
		return nil, err
	}
	return dates[j:], nil
}

// GeneratePercents returns the percent of the shares each planned vest pays: year k's percent split equally over the calendar vests in the year's 12 months from the first vest's month, the vests before the cliff paid with the first vest after it (see PlanDates). The year percents must be one per year and sum to 100.
func GeneratePercents(cal *VestCalendar, first Date, yearPercents []float64, cliff int) ([]float64, error) {
	if err := checkPercents(yearPercents); err != nil {
		return nil, fmt.Errorf("year percents: %w", err)
	}
	dates, err := vestDates(cal, first, len(yearPercents))
	if err != nil {
		return nil, err
	}
	j, err := cliffStart(dates, cliff)
	if err != nil {
		return nil, err
	}
	perYear := make([]int, len(yearPercents))
	for _, d := range dates {
		perYear[monthsFrom(first, d)/12]++
	}
	pct := make([]float64, len(dates))
	for i, d := range dates {
		y := monthsFrom(first, d) / 12
		pct[i] = yearPercents[y] / float64(perYear[y])
	}
	for _, p := range pct[:j] {
		pct[j] += p
	}
	return pct[j:], nil
}

// checkPercents refuses percents that are negative or do not sum to 100.
func checkPercents(p []float64) error {
	if len(p) == 0 {
		return errors.New("there are none")
	}
	sum := 0.0
	for _, x := range p {
		if x < 0 || math.IsNaN(x) {
			return fmt.Errorf("%v is not a percent", x)
		}
		sum += x
	}
	if math.Abs(sum-100) > 1e-6 {
		return fmt.Errorf("they add up to %v, not 100", sum)
	}
	return nil
}

// Plan returns the grant's planned vests under the calendar: the dates, the percents it keeps and the shares, each the whole shares of the total's percent and the last vest the rest, so the shares add up to the grant's. It fails if the first vest is not a calendar date, the calendar no longer yields the vests the percents were made for, or the percents do not add up to 100.
func (g *Grant) Plan(cal *VestCalendar) ([]PlannedVest, error) {
	if err := checkPercents(g.VestPercents); err != nil {
		return nil, fmt.Errorf("vest percents: %w", err)
	}
	dates, err := PlanDates(cal, g.FirstVest, len(g.YearPercents), g.CliffMonths)
	if err != nil {
		return nil, err
	}
	if len(dates) != len(g.VestPercents) {
		return nil, fmt.Errorf("calendar %q has %d vests for the grant but it has percents for %d", cal.Name, len(dates), len(g.VestPercents))
	}
	out := make([]PlannedVest, len(dates))
	left := g.Shares
	for i, d := range dates {
		n := int(math.Floor(float64(g.Shares)*g.VestPercents[i]/100 + 1e-9))
		if i == len(dates)-1 {
			n = left
		}
		n = min(n, left)
		left -= n
		out[i] = PlannedVest{Index: i + 1, Date: d, Percent: g.VestPercents[i], Shares: n}
	}
	return out, nil
}

// Clone returns a deep copy.
func (g Grant) Clone() Grant {
	g.YearPercents = slices.Clone(g.YearPercents)
	g.VestPercents = slices.Clone(g.VestPercents)
	return g
}

// Clone returns a deep copy.
func (v RecordedVest) Clone() RecordedVest {
	v.Withheld = clonePtr(v.Withheld)
	return v
}

// hasGrants reports whether the account's actual balance comes from its grants and vests, as an equity account's does once it has a grant, rather than from snapshots.
func (a *Account) hasGrants() bool { return a.Type == TypeEquity && len(a.Grants) > 0 }

// VestedBy returns the shares of the grant that recorded vests dated on or before d have vested.
func (a *Account) VestedBy(grant int64, d Date) int {
	n := 0
	for _, v := range a.Vests {
		if v.Grant == grant && v.Date <= d {
			n += v.Shares
		}
	}
	return n
}

// equityAt returns the value on d of the shares the account's grants hold: for each grant dated on or before d, the shares granted less those vested by d, at the stock's latest price on or before d, or at the grant price if it has none yet.
func (a *Account) equityAt(d Date) float64 {
	sum := 0.0
	for _, g := range a.Grants {
		if g.Date > d {
			continue
		}
		price, ok := a.prices.At(g.Symbol, d)
		if !ok {
			price = g.Price
		}
		sum += float64(g.Shares-a.VestedBy(g.ID, d)) * price
	}
	return sum
}

// equitySpan runs from the first grant to the latest grant, vest, price of a stock it holds, or transaction.
func (a *Account) equitySpan() Span {
	sp := Span{From: a.Grants[0].Date, To: a.Grants[0].Date}
	for _, g := range a.Grants {
		sp.From, sp.To = min(sp.From, g.Date), max(sp.To, g.Date)
		for _, o := range a.prices.Symbol(g.Symbol) {
			sp.To = max(sp.To, o.Date)
		}
	}
	for _, v := range a.Vests {
		sp.To = max(sp.To, v.Date)
	}
	if n := len(a.Transactions); n > 0 {
		sp.From = min(sp.From, a.Transactions[0].Date)
		sp.To = max(sp.To, a.Transactions[n-1].Date)
	}
	return sp
}
