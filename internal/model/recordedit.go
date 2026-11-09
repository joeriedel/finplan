// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
)

// AccountTypes lists the account types.
var AccountTypes = []AccountType{
	TypeChecking, TypeSavings, TypeCreditCard, TypeBrokerage, Type401k, TypeTSP, TypeIRA, TypeRothIRA,
	TypeHSA, Type529, TypeMortgage, TypeLoan, TypeEquity, TypeProperty,
}

// TaxTreatments lists the tax treatments.
var TaxTreatments = []TaxTreatment{Taxable, TaxDeferred, TaxFree, Tax529}

func (b DefaultBalance) clone() DefaultBalance {
	b.Intervals = slices.Clone(b.Intervals)
	for i, iv := range b.Intervals {
		b.Intervals[i].To = clonePtr(iv.To)
	}
	return b
}

// cloneSchedules copies a map of schedules, keeping nil.
func cloneSchedules(m map[string]Schedule) map[string]Schedule {
	if m == nil {
		return nil
	}
	c := make(map[string]Schedule, len(m))
	for k, s := range m {
		c[k] = s.clone()
	}
	return c
}

// Clone returns a deep copy of the record: nothing in it is shared with the original, and the copy's indexes and member numbering are its own. It is what an edit of accounts, assets or the budget changes while the original stays as it was.
func (r *Record) Clone() *Record {
	c := r.WithProjections(r.Projections)
	if r.Projections == nil {
		c.Projections = nil
	}
	c.Globals = cloneSchedules(r.Globals)
	c.Classes = slices.Clone(r.Classes)
	c.Accounts = make([]*Account, len(r.Accounts))
	for i, a := range r.Accounts {
		ca := *a
		ca.Default = a.Default.clone()
		ca.Snapshots = slices.Clone(a.Snapshots)
		ca.Transactions = slices.Clone(a.Transactions)
		c.Accounts[i] = &ca
	}
	c.Assets = make([]*Asset, len(r.Assets))
	for i, a := range r.Assets {
		ca := *a
		ca.Members = slices.Clone(a.Members)
		for j, m := range ca.Members {
			if m.Rule != nil {
				ca.Members[j].Rule = &Rule{Types: slices.Clone(m.Rule.Types), Tax: slices.Clone(m.Rule.Tax)}
			}
		}
		ca.Default = a.Default.clone()
		ca.Params = cloneSchedules(a.Params)
		c.Assets[i] = &ca
	}
	c.Budget.Items = slices.Clone(r.Budget.Items)
	for i, it := range c.Budget.Items {
		c.Budget.Items[i].Start, c.Budget.Items[i].End = clonePtr(it.Start), clonePtr(it.End)
	}
	c.accountIdx = maps.Clone(r.accountIdx)
	c.assetIdx = maps.Clone(r.assetIdx)
	return c
}

// renumber numbers every member of every asset 1, 2, ... in the order it was added, closing the gaps a removal leaves, because a stored record can only be restored with consecutive numbers.
func (r *Record) renumber() {
	type ref struct {
		a *Asset
		i int
	}
	var refs []ref
	for _, a := range r.Assets {
		for i := range a.Members {
			refs = append(refs, ref{a, i})
		}
	}
	slices.SortStableFunc(refs, func(x, y ref) int { return cmp.Compare(x.a.Members[x.i].Seq, y.a.Members[y.i].Seq) })
	for n, f := range refs {
		f.a.Members[f.i].Seq = n + 1
	}
	r.seq = len(refs)
}

// RemoveMember removes the member of the asset with the given Seq. The remaining members of the record are renumbered, keeping their order.
func (r *Record) RemoveMember(asset AssetID, seq int) error {
	a := r.Asset(asset)
	if a == nil {
		return fmt.Errorf("model: unknown asset %q", asset)
	}
	if _, isDefault := accountOfDefault(asset); isDefault {
		return fmt.Errorf("model: asset %q is an account's default asset: its member cannot be removed", asset)
	}
	i := slices.IndexFunc(a.Members, func(m Member) bool { return m.Seq == seq })
	if i < 0 {
		return fmt.Errorf("model: asset %q has no member with seq %d", asset, seq)
	}
	a.Members = slices.Delete(a.Members, i, i+1)
	r.renumber()
	return nil
}

// RemoveAsset deletes the asset and its members. It refuses, changing nothing, a default asset and an asset that another asset has as a member.
func (r *Record) RemoveAsset(id AssetID) error {
	i, ok := r.assetIdx[id]
	if !ok {
		return fmt.Errorf("model: unknown asset %q", id)
	}
	if _, isDefault := accountOfDefault(id); isDefault {
		return fmt.Errorf("model: asset %q is an account's default asset: it cannot be removed", id)
	}
	for _, o := range r.Assets {
		if slices.ContainsFunc(o.Members, func(m Member) bool { return m.Asset == id }) {
			return fmt.Errorf("model: asset %q cannot be removed: asset %q has it as a member", id, o.ID)
		}
	}
	r.Assets = slices.Delete(r.Assets, i, i+1)
	delete(r.assetIdx, id)
	for j := i; j < len(r.Assets); j++ {
		r.assetIdx[r.Assets[j].ID] = j
	}
	r.renumber()
	return nil
}
