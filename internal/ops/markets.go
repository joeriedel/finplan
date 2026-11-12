// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

func checkMarketDate(d model.Date) error {
	if d == model.MinDate || d == model.MaxDate {
		return refuse(ErrInvalidValue, "the date must be a day")
	}
	return nil
}

// CheckShock returns the reason a shock cannot be stored: an unknown class (ErrUnknownTarget), a date that is not a day, or a change that is not above -100% (ErrInvalidValue).
func CheckShock(rec *model.Record, s model.Shock) error {
	switch {
	case rec.Class(s.Class) == nil:
		return unknownClass(s.Class)
	case math.IsNaN(s.Change) || s.Change <= -1 || s.Change > maxValue:
		return refuse(ErrInvalidValue, "a shock of %v%% must be above -100%%", s.Change*100)
	}
	return checkMarketDate(s.Date)
}

// CheckRebalance returns the reason a rebalance cannot be stored: an unknown account or class (ErrUnknownObject, ErrUnknownTarget), an account that cannot hold an allocation, a date that is not a day, or weights that are negative, repeat a class or do not sum to 100% (ErrInvalidValue).
func CheckRebalance(rec *model.Record, r model.Rebalance) error {
	a := rec.Account(r.Account)
	switch {
	case a == nil:
		return unknownAccount(r.Account)
	case !model.Investable(a.Type):
		return refuse(ErrInvalidValue, "an account of type %q cannot be rebalanced", a.Type)
	}
	if len(r.Allocation) == 0 {
		return refuse(ErrInvalidValue, "a rebalance needs a target allocation")
	}
	sum := 0.0
	seen := map[string]bool{}
	for _, w := range r.Allocation {
		switch {
		case rec.Class(w.Class) == nil:
			return unknownClass(w.Class)
		case seen[w.Class]:
			return refuse(ErrInvalidValue, "class %q is in the target twice", w.Class)
		case math.IsNaN(w.Share) || w.Share < 0:
			return refuse(ErrInvalidValue, "the share of class %q must not be negative", w.Class)
		}
		seen[w.Class] = true
		sum += w.Share
	}
	if math.Abs(sum-1) > model.AllocationTolerance/100 {
		return refuse(ErrInvalidValue, "the target sums to %s%%, not 100%%", strconv.FormatFloat(math.Round(sum*100*1e6)/1e6, 'f', -1, 64))
	}
	return checkMarketDate(r.Date)
}

// projectionView is the record as the projection sees it, with its hypothetical accounts; rec itself if the projection does not exist, which the edit then refuses.
func projectionView(rec *model.Record, name string) *model.Record {
	if view, err := rec.For(name); err == nil {
		return view
	}
	return rec
}

func shockIndex(p *model.Projection, index int) error {
	if index < 0 || index >= len(p.Shocks) {
		return fmt.Errorf("projection %q: shock index %d out of range", p.Name, index)
	}
	return nil
}

func rebalanceIndex(p *model.Projection, index int) error {
	if index < 0 || index >= len(p.Rebalances) {
		return fmt.Errorf("projection %q: rebalance index %d out of range", p.Name, index)
	}
	return nil
}

// AddShock appends a shock to the projection: on its date every account holding the class loses or gains the fraction Change of its holding. The shock is checked by CheckShock.
func AddShock(st *store.Store, rec *model.Record, name string, s model.Shock, o Options) (*Result, error) {
	if err := CheckShock(rec, s); err != nil {
		return nil, err
	}
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		p.Shocks = append(p.Shocks, s)
		return nil
	})
}

// SetShock replaces the projection's shock at index with s.
func SetShock(st *store.Store, rec *model.Record, name string, index int, s model.Shock, o Options) (*Result, error) {
	if err := CheckShock(rec, s); err != nil {
		return nil, err
	}
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if err := shockIndex(p, index); err != nil {
			return err
		}
		p.Shocks[index] = s
		return nil
	})
}

// RemoveShock removes the projection's shock at index.
func RemoveShock(st *store.Store, rec *model.Record, name string, index int, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if err := shockIndex(p, index); err != nil {
			return err
		}
		p.Shocks = slices.Delete(p.Shocks, index, index+1)
		return nil
	})
}

// AddRebalance appends a rebalance to the projection: on its date the account's holdings are traded to the target. The rebalance is checked by CheckRebalance against the projection's accounts, its own included.
func AddRebalance(st *store.Store, rec *model.Record, name string, r model.Rebalance, o Options) (*Result, error) {
	if err := CheckRebalance(projectionView(rec, name), r); err != nil {
		return nil, err
	}
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		r.Allocation = slices.Clone(r.Allocation)
		p.Rebalances = append(p.Rebalances, r)
		return nil
	})
}

// SetRebalance replaces the projection's rebalance at index with r.
func SetRebalance(st *store.Store, rec *model.Record, name string, index int, r model.Rebalance, o Options) (*Result, error) {
	if err := CheckRebalance(projectionView(rec, name), r); err != nil {
		return nil, err
	}
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if err := rebalanceIndex(p, index); err != nil {
			return err
		}
		r.Allocation = slices.Clone(r.Allocation)
		p.Rebalances[index] = r
		return nil
	})
}

// RemoveRebalance removes the projection's rebalance at index.
func RemoveRebalance(st *store.Store, rec *model.Record, name string, index int, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if err := rebalanceIndex(p, index); err != nil {
			return err
		}
		p.Rebalances = slices.Delete(p.Rebalances, index, index+1)
		return nil
	})
}

// SetRebalanceYearly turns the account's yearly rebalance to its allocation on or off. Only an account that can hold an allocation has the setting (ErrInvalidValue).
func SetRebalanceYearly(st *store.Store, rec *model.Record, id model.AccountID, on bool, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Account(id)
		if a == nil {
			return Result{}, unknownAccount(id)
		}
		if !model.Investable(a.Type) {
			return Result{}, refuse(ErrInvalidValue, "an account of type %q cannot be rebalanced", a.Type)
		}
		a.RebalanceYearly = on
		return Result{Account: a, Asset: next.Asset(model.DefaultAssetID(id))}, nil
	})
}
