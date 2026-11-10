// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// withID finds the id the store gave the entry that an add saved, which is the newest entry of the account with these values. A dry run saved nothing and leaves the id 0.
func withID(st *store.Store, res *Result, err error, o Options) (*Result, error) {
	if err != nil || o.DryRun {
		return res, err
	}
	e := res.Entry
	stored, err := st.Entries(e.Account)
	if err != nil {
		return nil, fmt.Errorf("the entry was saved but could not be read back: %w", err)
	}
	for i := len(stored) - 1; i >= 0; i-- {
		if stored[i].Kind == e.Kind && stored[i].Values == e.Values {
			e.ID = stored[i].ID
			break
		}
	}
	return res, nil
}

// checkOpenOn refuses date after the close date of the account.
func checkOpenOn(a *model.Account, date model.Date) error {
	if a.ClosedOn(date) {
		return refuse(ErrInvalidValue, "account %q is closed on %v: an entry cannot be dated %v", a.ID, *a.Closed, date)
	}
	return nil
}

// AddSnapshot records the balance of the account at the end of the day. A date after the account's close date is ErrInvalidValue. An account that already has a snapshot on the date is ErrInvalidValue: a recorded balance is changed by CorrectEntry, never replaced.
func AddSnapshot(st *store.Store, rec *model.Record, account model.AccountID, date model.Date, balance float64, o Options) (*Result, error) {
	res, err := editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Account(account)
		if a == nil {
			return Result{}, unknownAccount(account)
		}
		if !finite(balance) {
			return Result{}, refuse(ErrInvalidValue, "%v is not a number within range", balance)
		}
		if err := checkOpenOn(a, date); err != nil {
			return Result{}, err
		}
		if slices.ContainsFunc(a.Snapshots, func(s model.Snapshot) bool { return s.Date == date }) {
			return Result{}, refuse(ErrInvalidValue, "account %q already has a snapshot on %v: correct that entry instead", account, date)
		}
		a.AddSnapshot(date, balance)
		return Result{Account: a, Entry: &store.Entry{Account: account, Kind: store.KindSnapshot, Values: store.Values{Date: date, Amount: balance}}}, nil
	})
	return withID(st, res, err, o)
}

// checkTx refuses a transaction kind that does not exist, an amount out of range, a transfer without a counter account the record has or with the account itself, and a date after the close date of the account or of its counter account.
func checkTx(rec *model.Record, account model.AccountID, v store.Values) error {
	if !slices.Contains([]model.TxKind{model.TxIncome, model.TxExpense, model.TxTransfer}, v.TxKind) {
		return refuse(ErrInvalidValue, "unknown transaction kind %q: use income, expense or transfer", v.TxKind)
	}
	if !finite(v.Amount) {
		return refuse(ErrInvalidValue, "%v is not a number within range", v.Amount)
	}
	if v.TxKind == model.TxTransfer {
		if v.Counter == "" {
			return refuse(ErrInvalidValue, "a transfer needs a counter account")
		}
		if v.Counter == account {
			return refuse(ErrInvalidValue, "a transfer needs a counter account other than %q", account)
		}
	}
	if v.Counter != "" {
		c := rec.Account(v.Counter)
		if c == nil {
			return unknownAccount(v.Counter)
		}
		if err := checkOpenOn(c, v.Date); err != nil {
			return err
		}
	}
	if a := rec.Account(account); a != nil {
		return checkOpenOn(a, v.Date)
	}
	return nil
}

// AddTransaction records a transaction of the account; the amount is signed from the account's view. A transfer needs a counter account the record has. An identical transaction already recorded does not stop it: two equal payments can happen.
func AddTransaction(st *store.Store, rec *model.Record, account model.AccountID, t model.Transaction, o Options) (*Result, error) {
	res, err := editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Account(account)
		if a == nil {
			return Result{}, unknownAccount(account)
		}
		v := store.Values{Date: t.Date, Amount: t.Amount, TxKind: t.Kind, Counter: t.Counter, Memo: t.Memo}
		if err := checkTx(next, account, v); err != nil {
			return Result{}, err
		}
		if err := a.AddTransaction(t); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		return Result{Account: a, Entry: &store.Entry{Account: account, Kind: store.KindTransaction, Values: v}}, nil
	})
	return withID(st, res, err, o)
}

// EntryChange is the values of an entry to change; nil leaves one as it is.
type EntryChange struct {
	Date    *model.Date
	Amount  *float64
	TxKind  *model.TxKind
	Counter *model.AccountID
	Memo    *string
}

// entryError turns the failure of a store call on an entry into the operation's error.
func entryError(err error, id int64) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return refuse(ErrUnknownObject, "there is no entry %d", id)
	case errors.Is(err, store.ErrInvalidEntry):
		return refuse(ErrInvalidValue, "%v", err)
	}
	return &SaveError{err}
}

// CorrectEntry stores a correction of the entry with the changed values, and a note. Only the values given change; the original stays in the entry's history. A snapshot has a date and an amount only. Correcting a voided entry restores it. A correction that leaves the entry as it is is ErrNoChange. The store's checks decide whether a correction is allowed, in a dry run too: the correction is made in a transaction that is rolled back. The result has the reloaded Record unless the run was a dry run, which has none.
func CorrectEntry(st *store.Store, rec *model.Record, id int64, ch EntryChange, note string, o Options) (*Result, error) {
	e, err := st.Entry(id)
	if err != nil {
		return nil, entryError(err, id)
	}
	v := e.Values
	if ch.Date != nil {
		v.Date = *ch.Date
	}
	if ch.Amount != nil {
		v.Amount = *ch.Amount
	}
	if ch.TxKind != nil {
		v.TxKind = *ch.TxKind
	}
	if ch.Counter != nil {
		v.Counter = *ch.Counter
	}
	if ch.Memo != nil {
		v.Memo = *ch.Memo
	}
	if !finite(v.Amount) {
		return nil, refuse(ErrInvalidValue, "%v is not a number within range", v.Amount)
	}
	if e.Kind == store.KindTransaction {
		if err := checkTx(rec, e.Account, v); err != nil {
			return nil, err
		}
	} else if a := rec.Account(e.Account); a != nil {
		if err := checkOpenOn(a, v.Date); err != nil {
			return nil, err
		}
	}
	if v == e.Values && !e.Void {
		return nil, ErrNoChange
	}
	if o.DryRun {
		err = st.TryCorrect(id, v, note)
	} else {
		err = st.Correct(id, v, note)
	}
	if err != nil {
		return nil, entryError(err, id)
	}
	e.Values, e.Void = v, false
	return withReloaded(st, &Result{Entry: &e}, o)
}

// VoidEntry stores a correction that takes the entry out of the record, with a note. The entry stays in its history, and CorrectEntry restores it. An entry already voided is ErrNoChange. A dry run makes the void in a transaction that is rolled back. The result has the reloaded Record unless the run was a dry run, which has none.
func VoidEntry(st *store.Store, rec *model.Record, id int64, note string, o Options) (*Result, error) {
	e, err := st.Entry(id)
	if err != nil {
		return nil, entryError(err, id)
	}
	if e.Void {
		return nil, ErrNoChange
	}
	if o.DryRun {
		err = st.TryVoid(id, note)
	} else {
		err = st.Void(id, note)
	}
	if err != nil {
		return nil, entryError(err, id)
	}
	e.Void = true
	return withReloaded(st, &Result{Entry: &e}, o)
}

// withReloaded sets the record of a saved correction or void to the stored record, which the store applied the correction to.
func withReloaded(st *store.Store, res *Result, o Options) (*Result, error) {
	if o.DryRun {
		return res, nil
	}
	rec, err := st.Load()
	if err != nil {
		return nil, fmt.Errorf("the entry was saved but the record could not be reloaded: %w", err)
	}
	res.Record = rec
	return res, nil
}

// ListEntries returns the account's entries newest first: by date, and by id among entries of one date. A voided entry is left out unless withVoided is set, and then has Void set.
func ListEntries(st *store.Store, account model.AccountID, withVoided bool) ([]store.Entry, error) {
	var es []store.Entry
	var err error
	if withVoided {
		es, err = st.EntriesWithVoided(account)
	} else {
		es, err = st.Entries(account)
	}
	if err != nil {
		return nil, err
	}
	slices.SortFunc(es, func(a, b store.Entry) int {
		if a.Date != b.Date {
			return cmp.Compare(b.Date, a.Date)
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return es, nil
}

// EntryHistory returns the entry's original values followed by each correction and void in the order made. An unknown id is ErrUnknownObject.
func EntryHistory(st *store.Store, id int64) ([]store.Revision, error) {
	revs, err := st.History(id)
	if err != nil {
		return nil, entryError(err, id)
	}
	return revs, nil
}
