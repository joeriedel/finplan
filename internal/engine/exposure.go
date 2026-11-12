// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import "github.com/joeriedel/finplan/internal/model"

// AccountExposure is one account's holding of a class on the record's today: the account's balance, the class's target percent of its allocation, the amount the engine holds in the class and that amount's percent of the balance.
type AccountExposure struct {
	Account *model.Account
	Balance float64
	Percent float64
	Amount  float64
	Share   float64
}

// ClassExposure is what the allocated accounts hold of one class, in account order, and the total of their amounts.
type ClassExposure struct {
	Class    *model.MarketClass
	Accounts []AccountExposure
	Total    float64
}

// Exposure returns, per class of the record in order, the accounts allocated to it and the amounts the engine holds in the class on the record's today under reality. Up to the start of an account's extrapolation the actual balance is split by its allocation; after it the amounts are the drifted balances of the account's classes. An account's amounts over all classes add up to its balance; an account without an allocation, a closed account and one not yet open are exposed to no class.
func Exposure(rec *model.Record) ([]ClassExposure, error) {
	out := make([]ClassExposure, len(rec.Classes))
	for i := range rec.Classes {
		out[i].Class = &rec.Classes[i]
	}
	for _, a := range rec.Accounts {
		if len(a.Allocation) == 0 {
			continue
		}
		r, err := prepare(rec, "", model.DefaultAssetID(a.ID), rec.Today)
		if err != nil {
			return nil, err
		}
		p := r.paths[a.ID]
		bal := p.data(rec.Today).Amount
		if bal == 0 {
			continue
		}
		byClass := p.classBalances(rec.Today)
		for i := range out {
			amt, ok := byClass[out[i].Class.ID]
			if !ok {
				continue
			}
			var pct float64
			for _, h := range a.Allocation {
				if h.Class == out[i].Class.ID {
					pct = h.Percent
				}
			}
			out[i].Accounts = append(out[i].Accounts, AccountExposure{a, bal, pct, amt, amt / bal * 100})
			out[i].Total += amt
		}
	}
	return out, nil
}

// classBalances is the balance the path holds in each class on d. A date up to the start splits the actual balance by the allocation; a path without legs (an equity account) holds its balance by the allocation throughout.
func (p *path) classBalances(d model.Date) map[string]float64 {
	out := map[string]float64{}
	if d > p.start && p.plan == nil && p.legs != nil {
		pt := p.pointAt(d)
		for i, l := range p.legs {
			out[l.class] = pt.subs[i] * l.rate.Compound(pt.date, d)
		}
		return out
	}
	bal := p.data(d).Amount
	for _, h := range p.acct.Allocation {
		out[h.Class] = bal * h.Percent / 100
	}
	return out
}
