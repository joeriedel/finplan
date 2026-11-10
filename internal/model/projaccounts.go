// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"
	"slices"
)

// ProjAccount is a hypothetical account defined by a projection. It exists only under that projection and the projections built on it, and is never part of the record's accounts. It has no value before From, Opening on From, and grows at StatedRate from there.
type ProjAccount struct {
	ID         AccountID
	Name       string
	Type       AccountType
	Tax        TaxTreatment
	StatedRate float64
	Opening    float64
	From       Date
}

// Account returns the account the engine sees: no data, a default balance of Opening from From on.
func (a ProjAccount) Account() *Account {
	from := a.From
	return &Account{
		ID:         a.ID,
		Name:       a.Name,
		Type:       a.Type,
		Tax:        a.Tax,
		StatedRate: a.StatedRate,
		Default:    DefaultBalance{Intervals: []Interval{{From: from, Value: a.Opening}}},
		Opens:      &from,
	}
}

// Descendants returns the projections built on the named one, directly or not, in record order.
func (r *Record) Descendants(name string) []*Projection {
	var out []*Projection
	for _, p := range r.Projections {
		chain, err := r.Chain(p.Name)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(chain[:len(chain)-1], func(a *Projection) bool { return a.Name == name }) {
			out = append(out, p)
		}
	}
	return out
}

// For returns the record as the projection sees it: the record plus the accounts defined along the projection's chain, each with its default asset, so rule members pick them up. The record itself is never changed. For the empty projection it returns r. An account id used by a base account or twice along the chain is an error.
func (r *Record) For(projection string) (*Record, error) {
	if projection == "" {
		return r, nil
	}
	chain, err := r.Chain(projection)
	if err != nil {
		return nil, err
	}
	return r.ForChain(chain)
}

// ForChain is For for a chain of projections, root first, which may hold edited copies of the record's projections.
func (r *Record) ForChain(chain []*Projection) (*Record, error) {
	var own []ProjAccount
	for _, p := range chain {
		own = append(own, p.Accounts...)
	}
	if len(own) == 0 {
		return r, nil
	}
	c := *r
	c.Accounts = slices.Clone(r.Accounts)
	c.Assets = slices.Clone(r.Assets)
	c.accountIdx = make(map[AccountID]int, len(r.accountIdx)+len(own))
	for k, v := range r.accountIdx {
		c.accountIdx[k] = v
	}
	c.assetIdx = make(map[AssetID]int, len(r.assetIdx)+len(own))
	for k, v := range r.assetIdx {
		c.assetIdx[k] = v
	}
	for _, pa := range own {
		if pa.ID == "" || c.Account(pa.ID) != nil {
			return nil, fmt.Errorf("model: projection %q: account id %q is empty or already used", chain[len(chain)-1].Name, pa.ID)
		}
		if err := c.AddAccount(pa.Account()); err != nil {
			return nil, err
		}
	}
	return &c, nil
}
