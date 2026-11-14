// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import "sort"

// TransactionIndexAfter returns the index of the first transaction dated after d, which is len(Transactions) when none is.
func (a *Account) TransactionIndexAfter(d Date) int {
	return sort.Search(len(a.Transactions), func(i int) bool { return a.Transactions[i].Date > d })
}

// UnaccountedTx is a recorded transaction that no budget item accounts for.
type UnaccountedTx struct {
	Ref         TxRef
	Transaction Transaction
}

// UnaccountedMonth is the money that moved through one account in one calendar month without any budget item accounting for it: the signed sum, from the account's view, of its income and expense transactions that match no item. Transfers are never unaccounted.
type UnaccountedMonth struct {
	Account AccountID
	// Month is the first day of the calendar month.
	Month        Date
	Amount       float64
	Transactions []UnaccountedTx
}

// IsUnaccounted reports whether the transaction is income or expense that matches no budget item.
func (m *Matches) IsUnaccounted(ref TxRef, t Transaction) bool {
	if t.Kind != TxIncome && t.Kind != TxExpense {
		return false
	}
	_, matched := m.byTx[ref]
	return !matched
}

// Unaccounted returns the unaccounted amounts per account and calendar month for transactions dated from..to inclusive, accounts in record order and months oldest first; months with none are left out. An empty account means every account.
func (r *Record) Unaccounted(account AccountID, from, to Date) []UnaccountedMonth {
	m := r.Matches()
	var out []UnaccountedMonth
	for _, a := range r.Accounts {
		if account != "" && a.ID != account {
			continue
		}
		lo := a.TransactionIndexAfter(from - 1)
		for i := lo; i < len(a.Transactions) && a.Transactions[i].Date <= to; i++ {
			t := a.Transactions[i]
			ref := TxRef{a.ID, i}
			if !m.IsUnaccounted(ref, t) {
				continue
			}
			y, mo, _ := t.Date.YMD()
			month := D(y, mo, 1)
			if n := len(out); n == 0 || out[n-1].Account != a.ID || out[n-1].Month != month {
				out = append(out, UnaccountedMonth{Account: a.ID, Month: month})
			}
			cur := &out[len(out)-1]
			cur.Amount += t.Amount
			cur.Transactions = append(cur.Transactions, UnaccountedTx{ref, t})
		}
	}
	return out
}
