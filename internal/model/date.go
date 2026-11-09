// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package model holds finplan's data model: accounts with their record, assets that wrap them,
// parameter schedules, budgets and projections, plus the rules for resolving assets
// (overlap, default balances, coverage and parameter precedence). It performs no projection;
// that is the engine's job. It depends on the standard library only.
package model

import (
	"math"
	"time"
)

// DaysPerYear converts elapsed days to years for compounding.
const DaysPerYear = 365.25

// Date is a calendar day, counted as days since 1970-01-01 UTC.
type Date int32

// Bounds of the date range, used as "unbounded" in schedules and intervals.
const (
	MinDate Date = math.MinInt32
	MaxDate Date = math.MaxInt32
)

// D returns the date for a calendar day. Out-of-range days normalize as in time.Date.
func D(year int, month time.Month, day int) Date {
	return Date(time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// Time returns midnight UTC at the start of the date.
func (d Date) Time() time.Time { return time.Unix(int64(d)*86400, 0).UTC() }

// YMD returns the calendar day.
func (d Date) YMD() (int, time.Month, int) { return d.Time().Date() }

// Add returns the date n days later.
func (d Date) Add(n int) Date { return d + Date(n) }

func (d Date) String() string {
	switch d {
	case MinDate:
		return "-inf"
	case MaxDate:
		return "+inf"
	}
	return d.Time().Format("2006-01-02")
}

// Ptr returns a pointer to v, for optional fields.
func Ptr[T any](v T) *T { return &v }

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
