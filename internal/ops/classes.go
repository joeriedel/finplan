// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// editClasses runs change on a deep copy of the record and commits it, refusing a change that leaves the classes and correlations as they were.
func editClasses(st *store.Store, rec *model.Record, o Options, change func(next *model.Record) error) (*Result, error) {
	next := rec.Clone()
	if err := change(next); err != nil {
		return nil, err
	}
	if reflect.DeepEqual(rec.Classes, next.Classes) && reflect.DeepEqual(rec.Correlations, next.Correlations) {
		return nil, ErrNoChange
	}
	return commit(st, next, Result{}, o)
}

func unknownClass(id string) error {
	return refuse(ErrUnknownTarget, "there is no market class %q", id)
}

// checkClassReturn refuses a return schedule with a rate that is not a number above -1.
func checkClassReturn(s model.Schedule) error {
	if err := checkSchedule(s); err != nil {
		return err
	}
	for _, seg := range s.Segments() {
		if seg.Value <= -1 {
			return refuse(ErrInvalidValue, "a rate of %v must be above -1", seg.Value)
		}
	}
	return nil
}

func checkVolatility(v float64) error {
	if math.IsNaN(v) || v < 0 || v > 10 {
		return refuse(ErrInvalidValue, "the volatility %v must be a fraction per year between 0 and 10", v)
	}
	return nil
}

// AddClass adds a market class. It needs a name; an empty ID is made from the name, and an ID that another class has is ErrNameTaken, and so is a name that another class has, ignoring case. The return schedule's rates must be above -1 and the volatility not negative.
func AddClass(st *store.Store, rec *model.Record, c model.MarketClass, o Options) (*Result, error) {
	return editClasses(st, rec, o, func(next *model.Record) error {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" {
			return refuse(ErrInvalidValue, "a market class needs a name")
		}
		if c.ID = strings.TrimSpace(c.ID); c.ID == "" {
			base := slug(c.Name)
			c.ID = base
			for n := 2; next.Class(c.ID) != nil; n++ {
				c.ID = base + "-" + strconv.Itoa(n)
			}
		}
		if next.Class(c.ID) != nil {
			return refuse(ErrNameTaken, "there is already a market class %q", c.ID)
		}
		if classNameTaken(next, c.Name, "") {
			return refuse(ErrNameTaken, "there is already a market class named %q", c.Name)
		}
		if err := checkClassReturn(c.Return); err != nil {
			return err
		}
		if err := checkVolatility(c.Volatility); err != nil {
			return err
		}
		next.Classes = append(next.Classes, c)
		return nil
	})
}

func classNameTaken(rec *model.Record, name, except string) bool {
	return slices.ContainsFunc(rec.Classes, func(c model.MarketClass) bool { return c.ID != except && strings.EqualFold(c.Name, name) })
}

// ClassChange is the attributes of a market class to change; nil leaves one as it is. Return sets the return to that constant rate.
type ClassChange struct {
	Name       *string
	Return     *float64
	Volatility *float64
}

// EditClass changes attributes of a market class; a name that another class has is ErrNameTaken.
func EditClass(st *store.Store, rec *model.Record, id string, ch ClassChange, o Options) (*Result, error) {
	return editClasses(st, rec, o, func(next *model.Record) error {
		c := next.Class(id)
		if c == nil {
			return unknownClass(id)
		}
		if ch.Name != nil {
			name := strings.TrimSpace(*ch.Name)
			if name == "" {
				return refuse(ErrInvalidValue, "a market class needs a name")
			}
			if classNameTaken(next, name, id) {
				return refuse(ErrNameTaken, "there is already a market class named %q", name)
			}
			c.Name = name
		}
		if ch.Return != nil {
			c.Return = model.Constant(*ch.Return)
			if err := checkClassReturn(c.Return); err != nil {
				return err
			}
		}
		if ch.Volatility != nil {
			if err := checkVolatility(*ch.Volatility); err != nil {
				return err
			}
			c.Volatility = *ch.Volatility
		}
		return nil
	})
}

// SetClassGrowth sets the return schedule of the class, the annual rate its balances grow at when no projection modifies it, or clears it with nil so the class projects flat. Every rate must be a number above -1.
func SetClassGrowth(st *store.Store, rec *model.Record, id string, growth *model.Schedule, o Options) (*Result, error) {
	return editClasses(st, rec, o, func(next *model.Record) error {
		c := next.Class(id)
		if c == nil {
			return unknownClass(id)
		}
		c.Return = model.Schedule{}
		if growth != nil {
			if err := checkClassReturn(*growth); err != nil {
				return err
			}
			c.Return = *growth
		}
		return nil
	})
}

// RemoveClass removes a market class and its correlations. It refuses (ErrInUse) while an account's allocation, a modification, a shock or a rebalance names the class.
func RemoveClass(st *store.Store, rec *model.Record, id string, o Options) (*Result, error) {
	return editClasses(st, rec, o, func(next *model.Record) error {
		i := slices.IndexFunc(next.Classes, func(c model.MarketClass) bool { return c.ID == id })
		if i < 0 {
			return unknownClass(id)
		}
		for _, a := range next.Accounts {
			if slices.ContainsFunc(a.Allocation, func(h model.Holding) bool { return h.Class == id }) {
				return refuse(ErrInUse, "account %q has class %q in its allocation", a.ID, id)
			}
		}
		for _, p := range next.Projections {
			if slices.ContainsFunc(p.Mods, func(m model.Modification) bool { return m.Target.Kind == model.KindClass && m.Target.ID == id }) {
				return refuse(ErrInUse, "projection %q modifies class %q", p.Name, id)
			}
			if slices.ContainsFunc(p.Shocks, func(s model.Shock) bool { return s.Class == id }) ||
				slices.ContainsFunc(p.Rebalances, func(r model.Rebalance) bool {
					return slices.ContainsFunc(r.Allocation, func(w model.Weight) bool { return w.Class == id })
				}) {
				return refuse(ErrInUse, "projection %q has a shock or rebalance of class %q", p.Name, id)
			}
		}
		next.Classes = slices.Delete(next.Classes, i, i+1)
		next.Correlations = slices.DeleteFunc(next.Correlations, func(c model.Correlation) bool { return c.A == id || c.B == id })
		return nil
	})
}

// SetCorrelation sets the correlation of two different classes, a number from -1 to 1; 0 removes the pair, which reads as uncorrelated.
func SetCorrelation(st *store.Store, rec *model.Record, a, b string, v float64, o Options) (*Result, error) {
	return editClasses(st, rec, o, func(next *model.Record) error {
		for _, id := range []string{a, b} {
			if next.Class(id) == nil {
				return unknownClass(id)
			}
		}
		switch {
		case a == b:
			return refuse(ErrInvalidValue, "a class is always fully correlated with itself")
		case math.IsNaN(v) || v < -1 || v > 1:
			return refuse(ErrInvalidValue, "a correlation of %v must be between -1 and 1", v)
		}
		next.Correlations = model.SetCorrelation(next.Correlations, a, b, v)
		return nil
	})
}

// SetAllocation sets how the account's balance is split among market classes, or clears it with an empty allocation so the account grows at its stated rate. With useDefault the allocation is model.DefaultAllocation for the account's type, and alloc must be empty. The allocation must be valid for the account's type (ErrInvalidValue); a class the record lacks is ErrUnknownTarget.
func SetAllocation(st *store.Store, rec *model.Record, id model.AccountID, alloc []model.Holding, useDefault bool, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Account(id)
		if a == nil {
			return Result{}, unknownAccount(id)
		}
		if useDefault {
			if len(alloc) > 0 {
				return Result{}, refuse(ErrInvalidValue, "give an allocation or ask for the default, not both")
			}
			if alloc = model.DefaultAllocation(a.Type); alloc == nil {
				return Result{}, refuse(ErrInvalidValue, "an account of type %q has no default allocation", a.Type)
			}
		}
		for _, h := range alloc {
			if next.Class(h.Class) == nil {
				return Result{}, unknownClass(h.Class)
			}
		}
		if err := next.CheckAllocation(a.Type, alloc); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		a.Allocation = slices.Clone(alloc)
		if len(alloc) == 0 {
			a.Allocation = nil
		}
		return Result{Account: a, Asset: next.Asset(model.DefaultAssetID(id))}, nil
	})
}
