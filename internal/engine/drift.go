// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// DefaultDriftMonths is the number of months of unaccounted money a drift setting averages when none is given.
const DefaultDriftMonths = 6

// MonthlyUnaccounted is the average monthly net unaccounted amount of the account over the n complete calendar months that end before the month containing from; a month with none counts as zero.
func MonthlyUnaccounted(rec *model.Record, account model.AccountID, from model.Date, n int) float64 {
	y, m, _ := from.YMD()
	end := model.D(y, m, 1).Add(-1)
	start := model.D(y, m-time.Month(n), 1)
	total := 0.0
	for _, u := range rec.Unaccounted(account, start, end) {
		total += u.Amount
	}
	return total / float64(n)
}

// addDrift books the account's average monthly unaccounted amount, over the months before its flows start, on the first day of each month after them, up to the horizon, as a projected flow. months is the setting of the projection: 0 is off. An equity or amortized account has no flows of this kind.
func (p *path) addDrift(rec *model.Record, months int, horizon model.Date) {
	if months <= 0 || p.idle || p.plan != nil || p.amortized {
		return
	}
	avg := MonthlyUnaccounted(rec, p.acct.ID, p.flowsAfter.Add(1), months)
	if avg == 0 {
		return
	}
	rank := rankExpense
	if avg > 0 {
		rank = rankIncome
	}
	y, m, _ := p.flowsAfter.YMD()
	for m++; ; m++ {
		d := model.D(y, m, 1)
		if d > horizon {
			break
		}
		if !p.acct.ClosedOn(d) {
			p.changes = append(p.changes, change{date: d, cause: CauseUnaccounted, amount: avg, rank: rank})
		}
	}
}
