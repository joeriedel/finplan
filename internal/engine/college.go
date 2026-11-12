// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"sort"

	"github.com/joeriedel/finplan/internal/model"
)

// 529 accounts. An expense item paid from a 529 account is a qualified college cost: the account pays it while it holds money, the household's reconciliation (checking) account pays the rest the same day, and no tax is due. A transfer out of a 529 account to any other kind of account is a non-qualified withdrawal, whose earnings part is ordinary income with the additional tax. Each 529 path tracks its contribution basis: contributions add to it and every withdrawal removes the same share of it as it takes of the balance. Both depend on the balance, which includes growth, so they are worked out after the budget's flows are collected and before the paths settle.

// collegeCost is one occurrence of an expense item paid from a 529 account.
type collegeCost struct {
	date   model.Date
	item   string
	amount float64
}

// earning is the earnings part of a non-qualified withdrawal.
type earning struct {
	date   model.Date
	amount float64
}

// earningsIn is the earnings the path's non-qualified withdrawals took in the calendar year.
func (p *path) earningsIn(y int) float64 {
	sum := 0.0
	for _, e := range p.nonQualified {
		if ey, _, _ := e.date.YMD(); ey == y {
			sum += e.amount
		}
	}
	return sum
}

// availBefore is the balance the path holds on d before its expenses that day: the grown balance plus that day's income and the college costs already booked, but not its transfers.
func (p *path) availBefore(d model.Date) float64 {
	if d <= p.start {
		return p.startVal
	}
	bal := p.valueAt(d)
	for _, c := range p.changes {
		if c.date == d && c.rank > rankExpense {
			bal -= c.amount
		}
	}
	return bal
}

// settle529 books the college costs of the 529 paths and works out the basis and the non-qualified earnings of their withdrawals.
func (h *householdTax) settle529(paths []*path, byID map[model.AccountID]*path) {
	for _, p := range paths {
		if p.acct.Type == model.Type529 && !p.idle {
			h.p529 = append(h.p529, p)
		}
	}
	for _, p := range h.p529 {
		h.payCollege(p, byID)
	}
	h.trackBasis()
}

// payCollege books the college costs of the path: each is paid from the 529 account up to its balance and the shortfall from the reconciliation account, which is noted once for each item. With no reconciliation account open the 529 account pays all of it.
func (h *householdTax) payCollege(p *path, byID map[model.AccountID]*path) {
	if len(p.college) == 0 {
		return
	}
	sort.SliceStable(p.college, func(i, j int) bool { return p.college[i].date < p.college[j].date })
	p.settle()
	for _, c := range p.college {
		pay, short := c.amount, 0.0
		chk := byID[ReconciliationAccount(h.rec, c.date)]
		if chk != nil {
			pay = min(c.amount, max(p.availBefore(c.date), 0))
			short = c.amount - pay
		} else {
			h.note("there is no open checking account for %s: it is paid from %s even if it holds less", c.item, p.acct.Name)
		}
		if pay != 0 {
			p.changes = append(p.changes, change{date: c.date, cause: CauseExpense, amount: -pay, rank: rankExpense})
			p.settle()
		}
		if short > 0.005 {
			// Checking's actual data already holds a bill dated up to its start.
			if c.date > chk.start {
				chk.changes = append(chk.changes, change{date: c.date, cause: CauseExpense, amount: -short, rank: rankExpense})
			}
			h.note("%s ran out; %s is paid from checking from then on", p.acct.Name, c.item)
		}
	}
}

// basisEvent is one change to a 529 path with the balance just before it.
type basisEvent struct {
	p      *path
	c      change
	before float64
}

// trackBasis walks the changes of the 529 paths in date order. A transfer in from another kind of account adds to the basis; a withdrawal removes basis in proportion to the balance it takes, which a transfer to another 529 account moves to that account, and one to any other account is non-qualified, its earnings the amount less its share of the basis.
func (h *householdTax) trackBasis() {
	byPath := map[model.AccountID]*path{}
	basis := map[*path]float64{}
	var events []basisEvent
	for _, p := range h.p529 {
		p.settle()
		byPath[p.acct.ID] = p
		basis[p] = p.startVal
		if p.acct.Basis != nil {
			basis[p] = *p.acct.Basis
		}
		for i := 0; i < len(p.changes); {
			d := p.changes[i].date
			run, j := p.startVal, i
			if d > p.start {
				run = p.valueAt(d)
				for ; j < len(p.changes) && p.changes[j].date == d; j++ {
					run -= p.changes[j].amount
				}
			}
			for j = i; j < len(p.changes) && p.changes[j].date == d; j++ {
				events = append(events, basisEvent{p, p.changes[j], run})
				run += p.changes[j].amount
			}
			i = j
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i].c, events[j].c
		if a.date != b.date {
			return a.date < b.date
		}
		return a.rank < b.rank
	})
	for _, e := range events {
		p, c := e.p, e.c
		switch {
		case c.amount > 0 && c.counter != "":
			if byPath[c.counter] == nil {
				basis[p] += c.amount
			}
		case c.amount > 0 && c.cause == CauseOpening && p.acct.Basis == nil:
			basis[p] += c.amount
		case c.amount < 0:
			amt := -c.amount
			removed, earnings := 0.0, 0.0
			if e.before > 0 {
				removed = min(basis[p], basis[p]*amt/e.before)
				earnings = max(0, amt*(1-basis[p]/e.before))
			}
			basis[p] -= removed
			switch dst := byPath[c.counter]; {
			case dst != nil:
				basis[dst] += removed
			case c.counter != "":
				p.nonQualified = append(p.nonQualified, earning{c.date, earnings})
			}
		}
	}
}
