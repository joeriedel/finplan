// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"fmt"
	"slices"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// CloseAccount closes the account on date: it keeps its history, is left out of the pickers for new lines and objects, and reads 0 on every date after date. It is ErrUnknownObject for an account the record does not have, ErrInvalidValue for an account a projection defines (those are not closable), for an account already closed (reopen it first) and for a date before the account's last record entry and for a date before an entry of another account that names it as counter account.
func CloseAccount(st *store.Store, rec *model.Record, id model.AccountID, date model.Date, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Account(id)
		if a == nil {
			for _, p := range next.Projections {
				if slices.ContainsFunc(p.Accounts, func(pa model.ProjAccount) bool { return pa.ID == id }) {
					return Result{}, refuse(ErrInvalidValue, "account %q is defined by projection %q and cannot be closed", id, p.Name)
				}
			}
			return Result{}, unknownAccount(id)
		}
		if a.Closed != nil {
			return Result{}, refuse(ErrInvalidValue, "account %q is already closed on %v: reopen it first", id, *a.Closed)
		}
		if last, ok := a.LastEntry(); ok && date < last {
			return Result{}, refuse(ErrInvalidValue, "account %q has record entries up to %v: it cannot close on %v", id, last, date)
		}
		naming, err := st.EntriesNaming(id)
		if err != nil {
			return Result{}, fmt.Errorf("could not read the entries that name account %q: %w", id, err)
		}
		for _, e := range naming {
			if !e.Void && e.Date > date {
				return Result{}, refuse(ErrInvalidValue, "account %q is the counter account of an entry of account %q dated %v: it cannot close on %v", id, e.Account, e.Date, date)
			}
		}
		a.Closed = &date
		return Result{Account: a}, nil
	})
}

// ReopenAccount clears the close date of the account. It is ErrUnknownObject for an account the record does not have and ErrNoChange for one that is not closed.
func ReopenAccount(st *store.Store, rec *model.Record, id model.AccountID, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Account(id)
		if a == nil {
			return Result{}, unknownAccount(id)
		}
		if a.Closed == nil {
			return Result{}, ErrNoChange
		}
		a.Closed = nil
		return Result{Account: a}, nil
	})
}

// DeleteAccount deletes an account and its default asset. It is ErrUnknownObject for an account the record does not have, and ErrInUse, naming what blocks it, while the account has any entry stored (a voided one too), is the counter account of any entry stored, or is referred to by: a modification of any projection that targets the account or its default asset, or a budget item it adds that uses the account; an asset setting of any projection for its default asset; another asset's member; or a base budget item that uses it as account, from or to. The result's Account is the account as it was. An account with entries is closed instead (CloseAccount).
func DeleteAccount(st *store.Store, rec *model.Record, id model.AccountID, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Account(id)
		if a == nil {
			return Result{}, unknownAccount(id)
		}
		stored, err := st.HasEntries(id)
		if err != nil {
			return Result{}, fmt.Errorf("could not read the entries of account %q: %w", id, err)
		}
		if stored || len(a.Snapshots) > 0 || len(a.Transactions) > 0 {
			return Result{}, refuse(ErrInUse, "account %q has record entries: close it instead", id)
		}
		if err := checkNotCounter(st, id); err != nil {
			return Result{}, err
		}
		def := model.DefaultAssetID(id)
		for _, other := range next.Assets {
			if other.ID != def && slices.ContainsFunc(other.Members, func(m model.Member) bool { return m.Account == id || m.Asset == def }) {
				return Result{}, refuse(ErrInUse, "account %q is a member of asset %q", id, other.ID)
			}
		}
		for _, it := range next.Budget.Items {
			if it.Account == id || it.From == id || it.To == id {
				return Result{}, refuse(ErrInUse, "account %q is used by budget item %q", id, it.ID)
			}
		}
		for _, p := range next.Projections {
			if refersTo(p, id) {
				return Result{}, refuse(ErrInUse, "account %q is used by projection %q", id, p.Name)
			}
		}
		if err := next.RemoveAccount(id); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		return Result{Account: a, Removed: string(id)}, nil
	})
}

// checkNotCounter is ErrInUse if any entry stored, a voided one too, names the account as its counter account.
func checkNotCounter(st *store.Store, id model.AccountID) error {
	naming, err := st.EntriesNaming(id)
	if err != nil {
		return fmt.Errorf("could not read the entries that name account %q: %w", id, err)
	}
	if len(naming) > 0 {
		return refuse(ErrInUse, "account %q is the counter account of an entry of account %q", id, naming[0].Account)
	}
	return nil
}

// RestoreDocuments replaces the record's accounts and assets with copies of those of src, an earlier state of this record: how an editor undoes and redoes an edit of them. Record entries are not restored: an account keeps the entries it has, and one that has entries, or is the counter account of any, cannot be removed (ErrInUse), so such a restore saves nothing.
func RestoreDocuments(st *store.Store, rec *model.Record, src *model.Record, o Options) (*Result, error) {
	next := rec.WithDocuments(src)
	for _, a := range rec.Accounts {
		if next.Account(a.ID) != nil {
			continue
		}
		stored, err := st.HasEntries(a.ID)
		if err != nil {
			return nil, fmt.Errorf("could not read the entries of account %q: %w", a.ID, err)
		}
		if stored || len(a.Snapshots) > 0 || len(a.Transactions) > 0 {
			return nil, refuse(ErrInUse, "account %q has record entries and cannot be removed", a.ID)
		}
		if err := checkNotCounter(st, a.ID); err != nil {
			return nil, err
		}
	}
	return commit(st, next, Result{}, o)
}
