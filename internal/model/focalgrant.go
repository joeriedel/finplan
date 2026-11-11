// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
)

// FocalGrant is a hypothetical grant, or a yearly series of them, that a projection adds to an equity account (PropAddGrant). Each grant is worth Value dollars in shares at the projected price on its date, the shares floored, and vests like a real grant: by the calendar, the year percents and the cliff, from the first calendar date at least FirstVestMonths months after the grant date.
type FocalGrant struct {
	Symbol string
	Value  float64
	// Date is the grant date, or the first of them if Yearly.
	Date Date
	// Yearly repeats the grant on the month and day of Date each year, through Last if it is set. A 29 February grant falls on 28 February in other years.
	Yearly bool
	Last   *Date
	// Calendar names the vest calendar.
	Calendar        string
	YearPercents    []float64
	CliffMonths     int
	FirstVestMonths int
	// Rates is the withholding of the grants' vests; nil means DefaultWithholding.
	Rates *Withholding
	// Brokerage is the account that net proceeds are deposited in.
	Brokerage AccountID
}

// Clone returns a deep copy.
func (f FocalGrant) Clone() FocalGrant {
	f.Last = clonePtr(f.Last)
	f.Rates = clonePtr(f.Rates)
	f.YearPercents = slices.Clone(f.YearPercents)
	return f
}

// WithholdingRates returns the rates the grant's vests are withheld at.
func (f FocalGrant) WithholdingRates() Withholding {
	if f.Rates != nil {
		return *f.Rates
	}
	return DefaultWithholding
}

// Dates returns the grant dates up to and including upTo.
func (f FocalGrant) Dates(upTo Date) []Date {
	var out []Date
	if f.Date > upTo {
		return nil
	}
	out = append(out, f.Date)
	if !f.Yearly {
		return out
	}
	y, m, day := f.Date.YMD()
	for y++; ; y++ {
		d := D(y, m, day)
		if _, got, _ := d.YMD(); got != m {
			d = D(y, m, 28)
		}
		if d > upTo || (f.Last != nil && d > *f.Last) {
			return out
		}
		out = append(out, d)
	}
}

// addMonths returns the date n months after d, on the same day of the month or the last day of a shorter month.
func addMonths(d Date, n int) Date {
	y, m, day := d.YMD()
	first := D(y, m+time.Month(n), 1)
	fy, fm, _ := first.YMD()
	return D(fy, fm, min(day, daysInMonth(fy, fm)))
}

// GrantOn returns the grant that is made on date at the stock price: its shares, first vest and percents. The grant has no account or id.
func (f FocalGrant) GrantOn(cal *VestCalendar, date Date, price float64) (Grant, error) {
	if !(price > 0) || math.IsInf(price, 0) {
		return Grant{}, fmt.Errorf("the price %v on %v is not positive", price, date)
	}
	from := addMonths(date, f.FirstVestMonths)
	firsts := cal.Dates(from, from.Add(366))
	if len(firsts) == 0 {
		return Grant{}, fmt.Errorf("calendar %q has no vest date within a year of %v", cal.Name, from)
	}
	pct, err := GeneratePercents(cal, firsts[0], f.YearPercents, f.CliffMonths)
	if err != nil {
		return Grant{}, err
	}
	rates := f.WithholdingRates()
	return Grant{Symbol: f.Symbol, Date: date, Shares: SharesFor(f.Value, price), Price: price, Value: f.Value,
		Calendar: cal.Name, FirstVest: firsts[0], YearPercents: slices.Clone(f.YearPercents), CliffMonths: f.CliffMonths,
		VestPercents: pct, Rates: rates, Brokerage: f.Brokerage}, nil
}

// Check refuses a focal grant that could not generate grants under the calendar: an empty symbol, a value that is not positive, a last date before the first, negative months, percents that do not sum to 100 or a cliff the calendar cannot satisfy.
func (f FocalGrant) Check(cal *VestCalendar) error {
	switch {
	case f.Symbol == "":
		return errors.New("a focal grant needs a stock symbol")
	case !(f.Value > 0) || math.IsInf(f.Value, 0):
		return fmt.Errorf("the value %v must be positive", f.Value)
	case f.Last != nil && !f.Yearly:
		return errors.New("a last grant date needs a yearly grant")
	case f.Last != nil && *f.Last < f.Date:
		return errors.New("the last grant date is before the first")
	case f.FirstVestMonths < 0:
		return errors.New("the months before the first vest cannot be negative")
	case f.Brokerage == "":
		return errors.New("a focal grant needs a deposit account")
	}
	if r := f.WithholdingRates(); r.Federal < 0 || r.State < 0 || r.FICA < 0 || r.Sum() >= 1 {
		return errors.New("the withholding rates must be non-negative and add up to under 100%")
	}
	if cal == nil {
		return errors.New("there is no such vest calendar")
	}
	_, err := f.GrantOn(cal, f.Date, 1)
	return err
}
