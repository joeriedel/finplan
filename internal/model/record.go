// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"
	"slices"
)

// Record is everything entered: accounts, assets, the base budget, global parameters and projections.
// Today is the record's reference date; it stands in for a clock so the engine stays a pure function.
// Change the accounts, assets and members only through the Add and Remove methods, which keep the indexes and the member sequence consistent; the budget, globals, classes and projections are plain fields. An edit changes a copy, a Clone or (for projections only) an EditableCopy, so the original stays as it was until the edit is accepted.
type Record struct {
	Today       Date
	Accounts    []*Account
	Assets      []*Asset
	Budget      Budget
	Globals     map[string]Schedule
	Classes     []MarketClass
	Projections []*Projection
	// Calendars are the employers' vest calendars; Prices the stock price observations that equity accounts are valued at.
	Calendars []*VestCalendar
	Prices    *PriceBook

	// StockGrowth is each stock's base growth schedule by symbol; a stock with none is projected flat.
	StockGrowth map[string]Schedule

	seq        int
	accountIdx map[AccountID]int
	assetIdx   map[AssetID]int
}

// NewRecord returns an empty record.
func NewRecord(today Date) *Record {
	return &Record{
		Today:       today,
		Globals:     map[string]Schedule{},
		Prices:      &PriceBook{},
		StockGrowth: map[string]Schedule{},
		accountIdx:  map[AccountID]int{},
		assetIdx:    map[AssetID]int{},
	}
}

// Account returns the account, or nil.
func (r *Record) Account(id AccountID) *Account {
	if i, ok := r.accountIdx[id]; ok {
		return r.Accounts[i]
	}
	return nil
}

// Asset returns the asset, or nil.
func (r *Record) Asset(id AssetID) *Asset {
	if i, ok := r.assetIdx[id]; ok {
		return r.Assets[i]
	}
	return nil
}

// Calendar returns the vest calendar with the name, or nil.
func (r *Record) Calendar(name string) *VestCalendar {
	for _, c := range r.Calendars {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Grant returns the grant with the stored id and the account it is in, or nil.
func (r *Record) Grant(id int64) (*Grant, *Account) {
	for _, a := range r.Accounts {
		for i := range a.Grants {
			if a.Grants[i].ID == id {
				return &a.Grants[i], a
			}
		}
	}
	return nil, nil
}

// Vest returns the recorded vest with the stored id and the account it is in, or nil.
func (r *Record) Vest(id int64) (*RecordedVest, *Account) {
	for _, a := range r.Accounts {
		for i := range a.Vests {
			if a.Vests[i].ID == id {
				return &a.Vests[i], a
			}
		}
	}
	return nil, nil
}

// Projection returns the projection with the name, or nil.
func (r *Record) Projection(name string) *Projection {
	for _, p := range r.Projections {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// AddAccount adds an account and its default asset.
func (r *Record) AddAccount(a *Account) error {
	if a.ID == "" || r.Account(a.ID) != nil {
		return fmt.Errorf("model: account id %q is empty or already used", a.ID)
	}
	r.accountIdx[a.ID] = len(r.Accounts)
	r.Accounts = append(r.Accounts, a)
	a.prices = r.Prices
	return r.addAsset(&Asset{
		ID:      DefaultAssetID(a.ID),
		Name:    a.Name,
		Members: []Member{AccountMember(a.ID)},
	})
}

// AddAsset adds an asset, numbering its members in order. It fails, adding nothing, if the id is
// taken or reserved, a member names something that does not exist, or a member would form a cycle.
func (r *Record) AddAsset(a *Asset) error {
	if _, reserved := accountOfDefault(a.ID); reserved {
		return fmt.Errorf("model: asset id %q is reserved for default assets", a.ID)
	}
	return r.addAsset(a)
}

func (r *Record) addAsset(a *Asset) error {
	if a.ID == "" || r.Asset(a.ID) != nil {
		return fmt.Errorf("model: asset id %q is empty or already used", a.ID)
	}
	members := a.Members
	a.Members = nil
	r.assetIdx[a.ID] = len(r.Assets)
	r.Assets = append(r.Assets, a)
	seq := r.seq
	for _, m := range members {
		if err := r.AddMember(a.ID, m); err != nil {
			r.seq = seq
			r.Assets = r.Assets[:len(r.Assets)-1]
			delete(r.assetIdx, a.ID)
			a.Members = members
			return err
		}
	}
	return nil
}

// AddMember appends a member to an asset, recording when it was added. A member that would make the
// asset contain itself is rejected with an error matching ErrCycle.
func (r *Record) AddMember(asset AssetID, m Member) error {
	a := r.Asset(asset)
	if a == nil {
		return fmt.Errorf("model: unknown asset %q", asset)
	}
	set := 0
	for _, ok := range []bool{m.Account != "", m.Asset != "", m.Rule != nil} {
		if ok {
			set++
		}
	}
	if set != 1 {
		return fmt.Errorf("model: asset %q: a member names exactly one of account, asset or rule", asset)
	}
	switch {
	case m.Asset != "":
		if r.Asset(m.Asset) == nil {
			return fmt.Errorf("model: asset %q: unknown member asset %q", asset, m.Asset)
		}
		if p := r.assetPath(m.Asset, asset, map[AssetID]bool{}); p != nil {
			return &CycleError{Path: append([]AssetID{asset}, p...)}
		}
	case m.Account != "":
		if r.Account(m.Account) == nil {
			return fmt.Errorf("model: asset %q: unknown member account %q", asset, m.Account)
		}
	}
	r.seq++
	m.Seq = r.seq
	a.Members = append(a.Members, m)
	return nil
}

// assetPath returns a path of nested assets from..to through every member, valid or not, or nil.
func (r *Record) assetPath(from, to AssetID, seen map[AssetID]bool) []AssetID {
	if from == to {
		return []AssetID{from}
	}
	a := r.Asset(from)
	if a == nil || seen[from] {
		return nil
	}
	seen[from] = true
	for _, m := range a.Members {
		if m.Asset != "" {
			if p := r.assetPath(m.Asset, to, seen); p != nil {
				return append([]AssetID{from}, p...)
			}
		}
	}
	return nil
}

// AddProjection adds a projection. Its parent is checked when the chain is resolved.
func (r *Record) AddProjection(p *Projection) error {
	if p.Name == "" || r.Projection(p.Name) != nil {
		return fmt.Errorf("model: projection name %q is empty or already used", p.Name)
	}
	r.Projections = append(r.Projections, p)
	return nil
}

// Chain returns the projection and its ancestors, root first.
func (r *Record) Chain(name string) ([]*Projection, error) {
	var chain []*Projection
	seen := map[string]bool{}
	for n := name; n != ""; {
		if seen[n] {
			return nil, fmt.Errorf("model: projection %q: parent chain: %w", n, ErrCycle)
		}
		seen[n] = true
		p := r.Projection(n)
		if p == nil {
			return nil, fmt.Errorf("model: unknown projection %q", n)
		}
		chain = append(chain, p)
		n = p.Parent
	}
	slices.Reverse(chain)
	return chain, nil
}

// Balance is an asset's balance on a date and where it came from.
type Balance struct {
	Amount float64
	// Actual is true if any account under the asset has actual data on the date, false if the value
	// came from defaults alone.
	Actual bool
}

// Balance returns the asset's balance on d from real data and defaults, with no projection.
func (r *Record) Balance(id AssetID, d Date) (Balance, error) {
	res, err := r.Resolve(id)
	if err != nil {
		return Balance{}, err
	}
	return res.Balance(d), nil
}
