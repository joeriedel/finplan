// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"math"
	"slices"
	"sort"

	"github.com/joeriedel/finplan/internal/model"
)

// amortize adds the payments of an amortized mortgage to its path and to the path of the account that pays it, nil if that account is not in the record. From the first payment on, each month's payment is the level payment of the terms; the month's interest is the balance owed times the rate for the month, and the rest of the payment pays the balance down. The payment of the last month of the term is whatever clears the balance. The payments follow the balance the mortgage path holds at its start and the other changes it has, so extra principal paid by a transfer cuts the interest that follows. Payments start after the data of both accounts ends, since the payer's recorded payments are mirrored into the mortgage, and stop when either account closes.
func (p *path) amortize(payer *path, horizon model.Date) {
	a := p.acct
	if p.idle || horizon <= p.start {
		return
	}
	pay := a.MonthlyPayment()
	owed := -p.startVal
	earlier := slices.Clone(p.changes)
	sort.SliceStable(earlier, func(i, j int) bool { return earlier[i].date < earlier[j].date })
	after, upTo := p.flowsAfter, horizon
	if a.Closed != nil {
		upTo = min(upTo, *a.Closed)
	}
	if payer != nil {
		after = max(after, payer.flowsAfter)
		if payer.acct.Closed != nil {
			upTo = min(upTo, *payer.acct.Closed)
		}
	}
	months := model.Recurrence{Kind: model.Custom, Count: 1, Unit: model.UnitMonths, Anchor: *a.FirstPayment}
	fy, fm, _ := a.FirstPayment.YMD()
	k := 0
	for _, d := range months.Between(after, upTo) {
		for ; k < len(earlier) && earlier[k].date <= d; k++ {
			owed -= earlier[k].amount
		}
		y, m, _ := d.YMD()
		n := (y-fy)*12 + int(m) - int(fm) + 1
		if n > a.TermMonths {
			break
		}
		if owed <= 0 {
			continue
		}
		rate, _ := p.rate.At(d)
		interest := math.Round(owed*rate/12*100) / 100
		principal := max(pay-interest, 0)
		if n == a.TermMonths || principal > owed {
			principal = owed
		}
		owed -= principal
		p.changes = append(p.changes, change{date: d, cause: CausePrincipal, amount: principal, counter: a.PaidFrom, rank: rankTransfer})
		if payer != nil {
			payer.changes = append(payer.changes,
				change{date: d, cause: CauseInterest, amount: -interest, rank: rankTransfer},
				change{date: d, cause: CausePrincipal, amount: -principal, counter: a.ID, rank: rankTransfer})
		}
	}
}
