// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"
	"sort"
)

// AccountID identifies an account.
type AccountID string

// AccountType is a descriptive attribute of an account.
type AccountType string

// Account types.
const (
	TypeChecking   AccountType = "checking"
	TypeSavings    AccountType = "savings"
	TypeCreditCard AccountType = "credit card"
	TypeBrokerage  AccountType = "brokerage"
	Type401k       AccountType = "401k"
	TypeTSP        AccountType = "TSP"
	TypeIRA        AccountType = "IRA"
	TypeRothIRA    AccountType = "Roth IRA"
	TypeHSA        AccountType = "HSA"
	Type529        AccountType = "529"
	TypeMortgage   AccountType = "mortgage"
	TypeLoan       AccountType = "loan"
	TypeEquity     AccountType = "equity"
	TypeProperty   AccountType = "property"
)

// TaxTreatment is a descriptive attribute of an account.
type TaxTreatment string

// Tax treatments.
const (
	Taxable     TaxTreatment = "taxable"
	TaxDeferred TaxTreatment = "tax-deferred"
	TaxFree     TaxTreatment = "tax-free"
	Tax529      TaxTreatment = "529"
)

// Sheltered reports whether the treatment is tax-advantaged.
func (t TaxTreatment) Sheltered() bool {
	return t == TaxDeferred || t == TaxFree || t == Tax529
}

// TxKind classifies a transaction.
type TxKind string

// Transaction kinds. A transfer moves money between two of the user's own accounts and is never spending.
const (
	TxIncome   TxKind = "income"
	TxExpense  TxKind = "expense"
	TxTransfer TxKind = "transfer"
)

// Snapshot is a recorded end-of-day balance.
type Snapshot struct {
	Date    Date
	Balance float64
}

// Transaction is a dated change to one account. Amount is signed from the account's view.
// A transfer names the account on the other side, which may be absent from the record.
type Transaction struct {
	Date    Date
	Amount  float64
	Kind    TxKind
	Counter AccountID
	Memo    string
}

// Span is an inclusive range of dates.
type Span struct {
	From, To Date
}

// Account is naked data: a balance history, a transaction history and descriptive attributes.
type Account struct {
	ID    AccountID
	Name  string
	Type  AccountType
	Tax   TaxTreatment
	Owner string
	// StatedRate is an annual rate that serves only as the default growth when the account is projected.
	StatedRate float64
	// Default is the fallback balance for dates with no actual data.
	Default DefaultBalance
	// Opens is set only on a projection's own accounts (see ProjAccount): the account has no value before that date and projects from it.
	Opens *Date
	// Closed is the date the account was closed, if it was. It keeps its history, and reads 0 on every date after Closed.
	Closed *Date

	Snapshots    []Snapshot
	Transactions []Transaction
}

// IsClosed reports whether the account has been closed.
func (a *Account) IsClosed() bool { return a.Closed != nil }

// ClosedOn reports whether the account is closed on d, that is closed and d is after its close date.
func (a *Account) ClosedOn(d Date) bool { return a.Closed != nil && d > *a.Closed }

// AddSnapshot records a balance, replacing any snapshot on the same date.
func (a *Account) AddSnapshot(d Date, balance float64) {
	i := sort.Search(len(a.Snapshots), func(i int) bool { return a.Snapshots[i].Date >= d })
	if i < len(a.Snapshots) && a.Snapshots[i].Date == d {
		a.Snapshots[i].Balance = balance
		return
	}
	a.Snapshots = append(a.Snapshots, Snapshot{})
	copy(a.Snapshots[i+1:], a.Snapshots[i:])
	a.Snapshots[i] = Snapshot{Date: d, Balance: balance}
}

// AddTransaction records a transaction, keeping the history in date order.
func (a *Account) AddTransaction(t Transaction) error {
	if t.Kind == TxTransfer && t.Counter == "" {
		return fmt.Errorf("model: account %q: transfer on %v names no counter account", a.ID, t.Date)
	}
	i := sort.Search(len(a.Transactions), func(i int) bool { return a.Transactions[i].Date > t.Date })
	a.Transactions = append(a.Transactions, Transaction{})
	copy(a.Transactions[i+1:], a.Transactions[i:])
	a.Transactions[i] = t
	return nil
}

// Span returns the account's real data span: from its earliest snapshot or transaction to its latest.
// An account with no snapshot has no actual data, because transactions alone cannot anchor a balance.
func (a *Account) Span() (Span, bool) {
	if len(a.Snapshots) == 0 {
		return Span{}, false
	}
	sp := Span{From: a.Snapshots[0].Date, To: a.Snapshots[len(a.Snapshots)-1].Date}
	if n := len(a.Transactions); n > 0 {
		sp.From = min(sp.From, a.Transactions[0].Date)
		sp.To = max(sp.To, a.Transactions[n-1].Date)
	}
	return sp, true
}

// LastEntry returns the date of the account's latest snapshot or transaction, and false if it has neither.
func (a *Account) LastEntry() (Date, bool) {
	var last Date
	ok := false
	if n := len(a.Snapshots); n > 0 {
		last, ok = a.Snapshots[n-1].Date, true
	}
	if n := len(a.Transactions); n > 0 {
		last, ok = max(last, a.Transactions[n-1].Date), true
	}
	return last, ok
}

// TransactionsIn returns the transactions dated after..upTo, that is excluding after and including upTo.
func (a *Account) TransactionsIn(after, upTo Date) []Transaction {
	lo := sort.Search(len(a.Transactions), func(i int) bool { return a.Transactions[i].Date > after })
	hi := sort.Search(len(a.Transactions), func(i int) bool { return a.Transactions[i].Date > upTo })
	if hi <= lo {
		return nil
	}
	return a.Transactions[lo:hi]
}

func (a *Account) txSum(after, upTo Date) float64 {
	sum := 0.0
	for _, t := range a.TransactionsIn(after, upTo) {
		sum += t.Amount
	}
	return sum
}

// ActualAt returns the balance on d from real data, and false outside the data span. Between snapshots
// the balance is filled from transactions, never from defaults. Before the first snapshot it is
// counted back from that snapshot through the transactions.
func (a *Account) ActualAt(d Date) (float64, bool) {
	sp, ok := a.Span()
	if !ok || d < sp.From || d > sp.To {
		return 0, false
	}
	i := sort.Search(len(a.Snapshots), func(i int) bool { return a.Snapshots[i].Date > d }) - 1
	if i < 0 {
		s := a.Snapshots[0]
		return s.Balance - a.txSum(d, s.Date), true
	}
	s := a.Snapshots[i]
	return s.Balance + a.txSum(s.Date, d), true
}

// UnexplainedIn returns the part of the actual balance change over after..upTo that the transactions
// do not explain: at each snapshot, the snapshot minus the previous snapshot plus the transactions between.
func (a *Account) UnexplainedIn(after, upTo Date) float64 {
	sum := 0.0
	for i := 1; i < len(a.Snapshots); i++ {
		cur, prev := a.Snapshots[i], a.Snapshots[i-1]
		if cur.Date > after && cur.Date <= upTo {
			sum += cur.Balance - (prev.Balance + a.txSum(prev.Date, cur.Date))
		}
	}
	return sum
}
