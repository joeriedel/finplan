// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"cmp"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
)

// MatchWindow is how many days either side of an occurrence's due date a transaction may be dated and still match a lump item.
const MatchWindow = 7

// TxRef names a recorded transaction by its account and its index in the account's Transactions.
type TxRef struct {
	Account AccountID
	Index   int
}

// Match is one recorded transaction matched to a budget item. Period is the start of the item's period the transaction counts in: for a lump, the due date of the occurrence it matched.
type Match struct {
	Item        string
	Period      Date
	Ref         TxRef
	Transaction Transaction
}

// Matches is the derived pairing of a record's transactions with its budget items; nothing about it is stored.
//
// A transaction matches an item when it is on one of the item's accounts (Account, or From and To for a transfer) and either is linked to the item (Transaction.Item), or is not linked and its memo equals the item's name, ignoring case. A link wins over the memo: a transaction linked to one item matches no other. A lump item matches at most one transaction per occurrence and account, within MatchWindow days of the occurrence's date: the nearest by date, a tie broken by how close its amount is to the item's. A spread item collects every matching transaction of its period. A transaction matches at most one item; earlier items in the budget claim first.
type Matches struct {
	byItem map[string][]Match
	byTx   map[TxRef]string
}

// matchBox is the cache of a record's matches, replaced when the numbers of its items or transactions change.
type matchBox struct {
	m          *Matches
	items, txs int
}

var matchMu sync.Mutex

func (r *Record) txCount() int {
	n := 0
	for _, a := range r.Accounts {
		n += len(a.Transactions)
	}
	return n
}

// Matches returns the record's matches, derived once and kept until the record changes. Records are changed by editing a Clone, which has no cache of its own, so every edit made through ops re-derives them; code that changes a record in place calls InvalidateMatches.
func (r *Record) Matches() *Matches {
	matchMu.Lock()
	defer matchMu.Unlock()
	items, txs := len(r.Budget.Items), r.txCount()
	if b := r.matches; b != nil && b.items == items && b.txs == txs {
		return b.m
	}
	m := deriveMatches(r)
	r.matches = &matchBox{m, items, txs}
	return m
}

// InvalidateMatches drops the record's cached matches, for code that changed the record in place.
func (r *Record) InvalidateMatches() {
	matchMu.Lock()
	defer matchMu.Unlock()
	r.matches = nil
}

// itemAccounts lists the accounts the item's transactions are recorded on, the source of a transfer first.
func itemAccounts(it BudgetItem) []AccountID {
	if it.Kind == ItemTransfer {
		if it.From == it.To {
			return nil
		}
		return []AccountID{it.From, it.To}
	}
	return []AccountID{it.Account}
}

// receives is the sign of the item's flow on the account: positive where money comes in.
func (b BudgetItem) receives(account AccountID) float64 {
	switch {
	case b.Kind == ItemIncome, b.Kind == ItemTransfer && account == b.To:
		return 1
	}
	return -1
}

func (b BudgetItem) matchesMemo(t Transaction) bool {
	if t.Item != "" {
		return t.Item == b.ID
	}
	return b.Name != "" && strings.EqualFold(t.Memo, b.Name)
}

// IsWages reports whether the item is a wages paycheck: an income item taxed as wages.
func (b BudgetItem) IsWages() bool { return b.Kind == ItemIncome && b.Income() == IncomeWages }

// NetDeposits, set by the engine, returns for the record a function giving the net deposit projected for the wages paycheck of an item on a date: the gross less what the paycheck withholds. It reports false when it knows none, as when the engine is not linked in; a paycheck is then compared at its gross.
var NetDeposits func(r *Record) func(it BudgetItem, d Date) (float64, bool)

func deriveMatches(r *Record) *Matches {
	m := &Matches{byItem: map[string][]Match{}, byTx: map[TxRef]string{}}
	for _, it := range r.Budget.Items {
		for _, id := range itemAccounts(it) {
			a := r.Account(id)
			if a == nil {
				continue
			}
			var cands []int
			for i, t := range a.Transactions {
				if _, taken := m.byTx[TxRef{id, i}]; !taken && it.matchesMemo(t) {
					cands = append(cands, i)
				}
			}
			if len(cands) == 0 {
				continue
			}
			if it.EffectiveTiming() == Lump {
				m.matchLump(r, it, a, cands)
			} else {
				m.matchSpread(it, a, cands)
			}
		}
	}
	for _, ms := range m.byItem {
		slices.SortStableFunc(ms, func(x, y Match) int { return cmp.Compare(x.Transaction.Date, y.Transaction.Date) })
	}
	return m
}

func (m *Matches) add(it BudgetItem, a *Account, i int, period Date) {
	ref := TxRef{a.ID, i}
	t := a.Transactions[i]
	m.byTx[ref] = it.ID
	m.byItem[it.ID] = append(m.byItem[it.ID], Match{Item: it.ID, Period: period, Ref: ref, Transaction: t})
}

func (m *Matches) matchSpread(it BudgetItem, a *Account, cands []int) {
	for _, i := range cands {
		d := a.Transactions[i].Date
		if it.End != nil && d > *it.End {
			continue
		}
		if p, ok := it.PeriodAt(d); ok {
			m.add(it, a, i, p.Start)
		}
	}
}

func (m *Matches) matchLump(r *Record, it BudgetItem, a *Account, cands []int) {
	first, last := a.Transactions[cands[0]].Date, a.Transactions[cands[len(cands)-1]].Date
	type pair struct {
		occ         Date
		tx          int
		dist        int
		amountApart float64
	}
	var pairs []pair
	var deposit func(BudgetItem, Date) (float64, bool)
	if it.IsWages() && NetDeposits != nil {
		deposit = NetDeposits(r)
	}
	for _, occ := range it.Dates(first.Add(-MatchWindow-1), last.Add(MatchWindow)) {
		if it.Start != nil && occ < *it.Start || it.End != nil && occ > *it.End {
			continue
		}
		budgeted, _ := it.Amount.At(occ)
		if deposit != nil {
			if net, ok := deposit(it, occ); ok {
				budgeted = net
			}
		}
		for _, i := range cands {
			t := a.Transactions[i]
			if dist := int(max(t.Date-occ, occ-t.Date)); dist <= MatchWindow {
				pairs = append(pairs, pair{occ, i, dist, math.Abs(math.Abs(t.Amount) - budgeted)})
			}
		}
	}
	slices.SortFunc(pairs, func(x, y pair) int {
		return cmp.Or(cmp.Compare(x.dist, y.dist), cmp.Compare(x.amountApart, y.amountApart), cmp.Compare(x.occ, y.occ), cmp.Compare(x.tx, y.tx))
	})
	usedOcc := map[Date]bool{}
	for _, p := range pairs {
		if usedOcc[p.occ] {
			continue
		}
		if _, taken := m.byTx[TxRef{a.ID, p.tx}]; taken {
			continue
		}
		usedOcc[p.occ] = true
		m.add(it, a, p.tx, p.occ)
	}
}

// Item returns the transactions matched to the item, oldest first.
func (m *Matches) Item(id string) []Match { return m.byItem[id] }

// Of returns the id of the item the transaction matches, and false if it matches none.
func (m *Matches) Of(ref TxRef) (string, bool) {
	id, ok := m.byTx[ref]
	return id, ok
}

// PeriodMatch is what the matches say of one period of an item.
type PeriodMatch struct {
	// Matched is set when a transaction matched the period; for a lump, the occurrence.
	Matched bool
	// Amount is the matched amount, as a positive magnitude in the direction of the item's flow: the sum over a spread item's matches, the transaction of a lump. A spread transfer counts on its source account when that has transactions in the period, else on its destination; a lump transfer is matched when either leg is, counting the source leg when it matched.
	Amount float64
}

// Paid reports whether the matches pair the occurrence of the lump item due on d with a transaction dated on or before the cutoff, which can be before the due date.
func (r *Record) Paid(it BudgetItem, d, cutoff Date) bool {
	if it.EffectiveTiming() != Lump {
		return false
	}
	for _, m := range r.Matches().Item(it.ID) {
		if m.Period == d && m.Transaction.Date <= cutoff {
			return true
		}
	}
	return false
}

// hasTransactions reports whether the account has a recorded transaction dated from..to.
func (a *Account) hasTransactions(from, to Date) bool {
	i := sort.Search(len(a.Transactions), func(i int) bool { return a.Transactions[i].Date >= from })
	return i < len(a.Transactions) && a.Transactions[i].Date <= to
}

// PeriodMatch returns what the matches say of the period of a base budget item, counting only transactions dated on or before the cutoff, and nil when they say nothing: the item is not in the budget, or none of its accounts has a recorded transaction in the period up to the cutoff (an account known by balance snapshots only is not tracked by transactions).
func (r *Record) PeriodMatch(it BudgetItem, p Period, cutoff Date) *PeriodMatch {
	return r.periodMatch(it, p, cutoff, false)
}

// PeriodMatchAsOf is PeriodMatch for a look back at a past date: the cutoff limits which matches count, but an account is tracked by transactions when it has any in the whole period, so a quiet stretch between the due date and the cutoff does not make a missed payment look unknown.
func (r *Record) PeriodMatchAsOf(it BudgetItem, p Period, cutoff Date) *PeriodMatch {
	return r.periodMatch(it, p, cutoff, true)
}

func (r *Record) periodMatch(it BudgetItem, p Period, cutoff Date, asOf bool) *PeriodMatch {
	if _, ok := r.Budget.Item(it.ID); !ok {
		return nil
	}
	var side AccountID
	tracked := false
	for _, id := range itemAccounts(it) {
		a := r.Account(id)
		if a != nil && (a.hasTransactions(p.Start, min(p.End, cutoff)) || asOf && a.hasTransactions(p.Start, p.End)) {
			if !tracked {
				side = id
			}
			tracked = true
		}
	}
	if !tracked {
		return nil
	}
	pm := &PeriodMatch{}
	lump := it.EffectiveTiming() == Lump
	for _, m := range r.Matches().Item(it.ID) {
		if m.Period != p.Start || m.Transaction.Date > cutoff {
			continue
		}
		if lump {
			if pm.Matched && m.Ref.Account != itemAccounts(it)[0] {
				continue
			}
			pm.Amount = m.Transaction.Amount * it.receives(m.Ref.Account)
			pm.Matched = true
		} else if m.Ref.Account == side {
			pm.Amount += m.Transaction.Amount * it.receives(side)
			pm.Matched = true
		}
	}
	return pm
}
