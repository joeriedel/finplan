// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package demodata

import (
	"hash/fnv"
	"math"
	"math/rand/v2"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// The recorded history of the Family record is not exactly its budget: amounts fluctuate, bills land a few days from their dates, money goes to payees the budget does not name, and one bill of the current period has not been paid. The noise comes from seeded sources, so the record is the same every time.
const (
	noiseSeed = 20261108
	// amountSwing is how far a variable expense strays from its budgeted amount, as a fraction of it.
	amountSwing = 0.15
	// depositSwing is how many dollars a paycheck's deposit strays from the net the budget projects for it, either way.
	depositSwing = 3.0
	// dateSwing is how many days either side of its date a lump bill may be paid.
	dateSwing = 3
)

// skippedBillItem and skippedBillDate are the bill of the current period that has not been paid: the childcare of 3 November 2026, the first of the checking account's lump bills past due as of FamilyToday.
const skippedBillItem = "childcare"

var skippedBillDate = model.D(2026, time.November, 3)

// fixedBills are expenses whose amount does not vary.
var fixedBills = map[string]bool{"insurance": true}

// payees are the unbudgeted payees: none of them is named like a budget item, so the matcher leaves their transactions unaccounted.
var payees = []struct {
	memo     string
	low, top float64
}{
	{"Hardware store", 12, 85},
	{"Pharmacy", 8, 45},
	{"Doctor copay", 25, 50},
	{"Online shopping", 15, 90},
	{"Corner Bistro", 22, 70},
}

// unbudgetedPerMonth is how many transactions to payees outside the budget an account records a month, about a fifth of the transactions its budget items imply.
var unbudgetedPerMonth = map[model.AccountID]int{Checking: 16, Card: 12}

// cardSettleDay is the day of the month on which the checking account pays the card the unbudgeted charges made since the last time.
const cardSettleDay = 28

// seededFor returns the source of the account's noise for the purpose.
func seededFor(id model.AccountID, purpose string) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(string(id) + "/" + purpose))
	return rand.New(rand.NewPCG(noiseSeed, h.Sum64()))
}

// jitter scales amount by up to amountSwing either way, to whole cents.
func jitter(rng *rand.Rand, amount float64) float64 {
	return math.Round(amount*(1+amountSwing*(2*rng.Float64()-1))*100) / 100
}

// unbudgeted returns the account's expenses to payees the budget does not name, from just after the history opens to end.
func unbudgeted(id model.AccountID, end model.Date) []model.Transaction {
	perMonth := unbudgetedPerMonth[id]
	if perMonth == 0 {
		return nil
	}
	rng := seededFor(id, "unbudgeted")
	days := int(end - historyOpen)
	n := days * perMonth * 12 / 365
	out := make([]model.Transaction, 0, n)
	for range n {
		p := payees[rng.IntN(len(payees))]
		amount := math.Round((p.low+(p.top-p.low)*rng.Float64())*100) / 100
		out = append(out, model.Transaction{Date: historyOpen.Add(1 + rng.IntN(days)), Amount: -amount, Kind: model.TxExpense, Memo: p.memo})
	}
	return out
}

// cardSettlements returns the payments, as the card sees them, of the unbudgeted charges on the card: each month's cardSettleDay pays what was charged since the one before. The checking account makes the same payments, so the card owes only what has been charged since the last.
func cardSettlements(end model.Date) []model.Transaction {
	charges := unbudgeted(Card, end)
	var out []model.Transaction
	prev := historyOpen
	for y, m, _ := historyOpen.YMD(); ; m++ {
		day := model.D(y, m, cardSettleDay)
		if day > end {
			break
		}
		if day > prev {
			sum := 0.0
			for _, c := range charges {
				if c.Date > prev && c.Date <= day {
					sum += c.Amount
				}
			}
			if sum != 0 {
				out = append(out, model.Transaction{Date: day, Amount: -math.Round(sum*100) / 100, Kind: model.TxTransfer, Counter: Checking, Memo: "Card balance payment"})
			}
			prev = day
		}
	}
	return out
}
