// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// NthLast is the VestRule.Nth of a rule that falls on the last such weekday of its month.
const NthLast = -1

// VestRule is the vest date of one month of the year: a fixed day of the month (Day, 1 to 28 in February and 1 to 30 or 31 elsewhere), or the Nth (1 to 4, or NthLast) Weekday of the month. A rule sets exactly one of Day and Nth. A fixed day that some year lacks (31 in a 30-day month, 29 to 31 in February) is refused by VestCalendar.Validate, so every rule has a date in every year.
type VestRule struct {
	Month   time.Month
	Day     int
	Nth     int
	Weekday time.Weekday
}

// DateOverride moves one generated vest date to another date.
type DateOverride struct {
	From, To Date
}

// VestCalendar is an employer's vest calendar, shared by the grants that name it: one rule per vest month, and overrides that move single generated dates.
type VestCalendar struct {
	Name      string
	Rules     []VestRule
	Overrides []DateOverride
}

// On returns the rule's date in the year. It assumes the rule is valid.
func (r VestRule) On(year int) Date {
	if r.Nth == 0 {
		return D(year, r.Month, r.Day)
	}
	if r.Nth == NthLast {
		last := D(year, r.Month, daysInMonth(year, r.Month))
		back := (int(last.Time().Weekday()) - int(r.Weekday) + 7) % 7
		return last.Add(-back)
	}
	first := D(year, r.Month, 1)
	ahead := (int(r.Weekday) - int(first.Time().Weekday()) + 7) % 7
	return first.Add(ahead + 7*(r.Nth-1))
}

// Validate refuses a calendar with no name or rules, a month with two rules or none that is valid, a rule that sets both or neither of Day and Nth, a fixed day that some year lacks, an Nth outside 1 to 4 and NthLast, and an override that does not move a generated date.
func (c *VestCalendar) Validate() error {
	if c.Name == "" {
		return errors.New("a vest calendar needs a name")
	}
	if len(c.Rules) == 0 {
		return fmt.Errorf("calendar %q has no rules", c.Name)
	}
	seen := map[time.Month]bool{}
	for _, r := range c.Rules {
		if r.Month < time.January || r.Month > time.December {
			return fmt.Errorf("calendar %q: month %d is not 1 to 12", c.Name, int(r.Month))
		}
		if seen[r.Month] {
			return fmt.Errorf("calendar %q has two rules for %v", c.Name, r.Month)
		}
		seen[r.Month] = true
		if (r.Day == 0) == (r.Nth == 0) {
			return fmt.Errorf("calendar %q: the %v rule sets exactly one of a day of the month and an Nth weekday", c.Name, r.Month)
		}
		if r.Day != 0 {
			// 2025 is not a leap year, so February's limit is 28: a rule on the 29th would have no date in most years.
			if limit := daysInMonth(2025, r.Month); r.Day < 1 || r.Day > limit {
				return fmt.Errorf("calendar %q: %v has no day %d in every year (1 to %d)", c.Name, r.Month, r.Day, limit)
			}
			continue
		}
		if r.Weekday < time.Sunday || r.Weekday > time.Saturday {
			return fmt.Errorf("calendar %q: the %v rule has no weekday", c.Name, r.Month)
		}
		if r.Nth != NthLast && (r.Nth < 1 || r.Nth > 4) {
			return fmt.Errorf("calendar %q: the %v rule needs the 1st to 4th or the last weekday", c.Name, r.Month)
		}
	}
	moved := map[Date]bool{}
	for _, o := range c.Overrides {
		if moved[o.From] {
			return fmt.Errorf("calendar %q: %v is overridden twice", c.Name, o.From)
		}
		moved[o.From] = true
		if o.To == o.From {
			return fmt.Errorf("calendar %q: the override of %v does not move it", c.Name, o.From)
		}
		y, m, _ := o.From.YMD()
		if !c.generates(y, m, o.From) {
			return fmt.Errorf("calendar %q: %v is not a date its rules generate", c.Name, o.From)
		}
	}
	return nil
}

func (c *VestCalendar) generates(year int, month time.Month, d Date) bool {
	return slices.ContainsFunc(c.Rules, func(r VestRule) bool { return r.Month == month && r.On(year) == d })
}

// Dates returns the vest dates from..to inclusive in order: the dates the rules generate with the overrides applied. An override can move a date into or out of the range.
func (c *VestCalendar) Dates(from, to Date) []Date {
	moves := map[Date]Date{}
	for _, o := range c.Overrides {
		moves[o.From] = o.To
	}
	fy, _, _ := from.YMD()
	ty, _, _ := to.YMD()
	var out []Date
	for y := fy - 1; y <= ty+1; y++ {
		for _, r := range c.Rules {
			d := r.On(y)
			if to2, ok := moves[d]; ok {
				d = to2
			}
			if d >= from && d <= to {
				out = append(out, d)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Has reports whether d is one of the calendar's vest dates.
func (c *VestCalendar) Has(d Date) bool {
	return len(c.Dates(d, d)) == 1
}

// Clone returns a deep copy.
func (c *VestCalendar) Clone() *VestCalendar {
	n := *c
	n.Rules = slices.Clone(c.Rules)
	n.Overrides = slices.Clone(c.Overrides)
	return &n
}
