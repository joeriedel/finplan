// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"math"
	"slices"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// finite reports whether v is a number within the range an amount may have.
func finite(v float64) bool { return !math.IsNaN(v) && v <= maxValue && v >= -maxValue }

// checkAttrs refuses an account type or tax treatment that does not exist and a stated rate that is not a fraction between -1 and 1.
func checkAttrs(typ model.AccountType, tax model.TaxTreatment, rate float64) error {
	switch {
	case !slices.Contains(model.AccountTypes, typ):
		return refuse(ErrInvalidValue, "unknown account type %q", typ)
	case !slices.Contains(model.TaxTreatments, tax):
		return refuse(ErrInvalidValue, "unknown tax treatment %q", tax)
	case math.IsNaN(rate) || rate < -1 || rate > 1:
		return refuse(ErrInvalidValue, "the stated rate %v must be a fraction per year between -1 and 1", rate)
	}
	return nil
}

// NewAccount adds an account with no record entries, and its default asset. The account needs a name, a type and a tax treatment; an empty ID is made from the name, and an ID that an account or asset has is ErrNameTaken, and so is a name that another account or an asset has, ignoring case. Snapshots, transactions and default balances in acct are ignored.
func NewAccount(st *store.Store, rec *model.Record, acct model.Account, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		name := strings.TrimSpace(acct.Name)
		if name == "" {
			return Result{}, refuse(ErrInvalidValue, "an account needs a name")
		}
		if err := freeAccountName(next, name, ""); err != nil {
			return Result{}, err
		}
		if err := checkAttrs(acct.Type, acct.Tax, acct.StatedRate); err != nil {
			return Result{}, err
		}
		id, err := newID(next, string(acct.ID), name)
		if err != nil {
			return Result{}, err
		}
		if next.Asset(model.DefaultAssetID(model.AccountID(id))) != nil {
			return Result{}, refuse(ErrNameTaken, "there is already an asset %q", model.DefaultAssetID(model.AccountID(id)))
		}
		a := &model.Account{ID: model.AccountID(id), Name: name, Type: acct.Type, Tax: acct.Tax, StatedRate: acct.StatedRate}
		if err := next.AddAccount(a); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		return Result{Account: a, Asset: next.Asset(model.DefaultAssetID(a.ID))}, nil
	})
}

// AccountChange is the attributes of an account to change; nil leaves one as it is.
type AccountChange struct {
	Name       *string
	Type       *model.AccountType
	Tax        *model.TaxTreatment
	StatedRate *float64
}

// EditAccount changes attributes of an account. Renaming the account renames its default asset too; a name that another account or an asset has is ErrNameTaken.
func EditAccount(st *store.Store, rec *model.Record, id model.AccountID, ch AccountChange, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Account(id)
		if a == nil {
			return Result{}, unknownAccount(id)
		}
		if ch.Name != nil {
			name := strings.TrimSpace(*ch.Name)
			if name == "" {
				return Result{}, refuse(ErrInvalidValue, "an account needs a name")
			}
			if err := freeAccountName(next, name, id); err != nil {
				return Result{}, err
			}
			a.Name = name
			next.Asset(model.DefaultAssetID(id)).Name = a.Name
		}
		if ch.Type != nil && *ch.Type != a.Type {
			if len(a.Grants) > 0 {
				return Result{}, refuse(ErrInUse, "account %q has grants: its type cannot change", id)
			}
			for _, o := range next.Accounts {
				for _, g := range o.Grants {
					if g.Brokerage == id {
						return Result{}, refuse(ErrInUse, "account %q is the brokerage account of grant %d: its type cannot change", id, g.ID)
					}
				}
			}
			if next.Household.Checking == id {
				return Result{}, refuse(ErrInUse, "account %q is the household's reconciliation account: its type cannot change", id)
			}
			a.Type = *ch.Type
		}
		if ch.Tax != nil {
			a.Tax = *ch.Tax
		}
		if ch.StatedRate != nil {
			a.StatedRate = *ch.StatedRate
		}
		if err := checkAttrs(a.Type, a.Tax, a.StatedRate); err != nil {
			return Result{}, err
		}
		return Result{Account: a, Asset: next.Asset(model.DefaultAssetID(id))}, nil
	})
}
