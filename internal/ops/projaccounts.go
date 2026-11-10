// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"slices"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// viewOf returns the record as projection p sees it, with p (which may be an edited copy) at the end of the chain of rec.
func viewOf(rec *model.Record, p *model.Projection) (*model.Record, error) {
	chain, err := rec.Chain(p.Name)
	if err != nil {
		return nil, err
	}
	chain[len(chain)-1] = p
	return rec.ForChain(chain)
}

// accountTaken reports whether an account or asset of rec, or an account of the projection or one building on it, already has the id. Other projections' accounts do not count: they never share a chain with the account.
func accountTaken(rec *model.Record, projection, id string) bool {
	if rec.Account(model.AccountID(id)) != nil || rec.Asset(model.AssetID(id)) != nil || rec.Asset(model.DefaultAssetID(model.AccountID(id))) != nil {
		return true
	}
	chain, err := rec.Chain(projection)
	if err != nil {
		return false
	}
	related := append(chain, rec.Descendants(projection)...)
	return slices.ContainsFunc(related, func(p *model.Projection) bool {
		return slices.ContainsFunc(p.Accounts, func(a model.ProjAccount) bool { return string(a.ID) == id })
	})
}

func checkProjAccount(a model.ProjAccount) error {
	if strings.TrimSpace(a.Name) == "" {
		return refuse(ErrInvalidValue, "an account needs a name")
	}
	if err := checkAttrs(a.Type, a.Tax, a.StatedRate); err != nil {
		return err
	}
	if !finite(a.Opening) {
		return refuse(ErrInvalidValue, "the opening balance %v is out of range", a.Opening)
	}
	if a.From == 0 {
		return refuse(ErrInvalidValue, "an account needs an opening date")
	}
	return nil
}

// AddProjAccount adds a hypothetical account to the projection. It needs a name, a type, a tax treatment and an opening date; an empty ID is made from the name, and an ID that a record account or asset, or an account of the projection, its ancestors or the projections building on it has is ErrNameTaken.
func AddProjAccount(st *store.Store, rec *model.Record, projection string, a model.ProjAccount, o Options) (*Result, error) {
	return editProjection(st, rec, o, projection, func(p *model.Projection) error {
		a.Name = strings.TrimSpace(a.Name)
		if err := checkProjAccount(a); err != nil {
			return err
		}
		id := strings.TrimSpace(string(a.ID))
		switch {
		case id == "":
			base := slug(a.Name)
			id = base
			for n := 2; accountTaken(rec, projection, id); n++ {
				id = base + "-" + strconv.Itoa(n)
			}
		case accountTaken(rec, projection, id):
			return refuse(ErrNameTaken, "there is already an account or asset %q", id)
		}
		a.ID = model.AccountID(id)
		p.Accounts = append(p.Accounts, a)
		return nil
	})
}

// ProjAccountChange is the attributes of a projection's account to change; nil leaves one as it is.
type ProjAccountChange struct {
	Name       *string
	Type       *model.AccountType
	Tax        *model.TaxTreatment
	StatedRate *float64
	Opening    *float64
	From       *model.Date
}

// EditProjAccount changes attributes of an account the projection itself defines.
func EditProjAccount(st *store.Store, rec *model.Record, projection string, id model.AccountID, ch ProjAccountChange, o Options) (*Result, error) {
	return editProjection(st, rec, o, projection, func(p *model.Projection) error {
		i := slices.IndexFunc(p.Accounts, func(a model.ProjAccount) bool { return a.ID == id })
		if i < 0 {
			return refuse(ErrUnknownObject, "projection %q does not define an account %q", projection, id)
		}
		a := &p.Accounts[i]
		if ch.Name != nil {
			a.Name = strings.TrimSpace(*ch.Name)
		}
		if ch.Type != nil {
			a.Type = *ch.Type
		}
		if ch.Tax != nil {
			a.Tax = *ch.Tax
		}
		if ch.StatedRate != nil {
			a.StatedRate = *ch.StatedRate
		}
		if ch.Opening != nil {
			a.Opening = *ch.Opening
		}
		if ch.From != nil {
			a.From = *ch.From
		}
		return checkProjAccount(*a)
	})
}

// RemoveProjAccount removes an account the projection defines. It refuses, with ErrInUse, while a modification, asset setting or budget item of the projection or one building on it refers to the account.
func RemoveProjAccount(st *store.Store, rec *model.Record, projection string, id model.AccountID, o Options) (*Result, error) {
	return edit(st, rec, o, func(next *model.Record) (Result, error) {
		p := next.Projection(projection)
		if p == nil {
			return Result{}, errUnknown(projection)
		}
		i := slices.IndexFunc(p.Accounts, func(a model.ProjAccount) bool { return a.ID == id })
		if i < 0 {
			return Result{}, refuse(ErrUnknownObject, "projection %q does not define an account %q", projection, id)
		}
		for _, q := range append([]*model.Projection{p}, next.Descendants(projection)...) {
			if refersTo(q, id) {
				return Result{}, refuse(ErrInUse, "account %q is still used by projection %q", id, q.Name)
			}
		}
		p.Accounts = slices.Delete(p.Accounts, i, i+1)
		if len(p.Accounts) == 0 {
			p.Accounts = nil
		}
		return Result{Projection: p}, nil
	})
}

// refersTo reports whether a setting, modification or budget item of p names the account or its default asset.
func refersTo(p *model.Projection, id model.AccountID) bool {
	asset := model.DefaultAssetID(id)
	if slices.ContainsFunc(p.AssetSettings, func(s model.AssetSetting) bool { return s.Asset == asset }) {
		return true
	}
	return slices.ContainsFunc(p.Mods, func(m model.Modification) bool {
		if m.Target.Kind == model.KindAccount && m.Target.ID == string(id) || m.Target.Kind == model.KindAsset && m.Target.ID == string(asset) {
			return true
		}
		return m.Item != nil && (m.Item.Account == id || m.Item.From == id || m.Item.To == id)
	})
}
