// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func sixtyForty() []model.Weight {
	return []model.Weight{{Class: model.ClassLargeCap, Share: 0.6}, {Class: model.ClassBonds, Share: 0.4}}
}

func TestShockOps(t *testing.T) {
	st, rec := seed(t)
	day := model.D(2027, time.March, 2)
	s := model.Shock{Class: model.ClassLargeCap, Date: day, Change: -0.4}
	if _, err := AddShock(st, rec, demodata.ProjBase, s, Options{DryRun: true}); err != nil || len(reload(t, st).Projection(demodata.ProjBase).Shocks) != 0 {
		t.Fatalf("dry run: %v", err)
	}
	res, err := AddShock(st, rec, demodata.ProjBase, s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjBase).Shocks; len(got) != 1 || got[0] != s {
		t.Fatalf("stored %+v", got)
	}
	rec = res.Record
	up := model.Shock{Class: model.ClassBonds, Date: day, Change: 0.1}
	if _, err := SetShock(st, rec, demodata.ProjBase, 0, up, Options{}); err != nil || reload(t, st).Projection(demodata.ProjBase).Shocks[0] != up {
		t.Fatalf("set: %v", err)
	}
	rec = reload(t, st)
	if _, err := SetShock(st, rec, demodata.ProjBase, 0, s, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	for _, c := range []struct {
		what string
		s    model.Shock
		want error
	}{
		{"unknown class", model.Shock{Class: "gold", Date: day, Change: -0.1}, ErrUnknownTarget},
		{"minus 100%", model.Shock{Class: model.ClassBonds, Date: day, Change: -1}, ErrInvalidValue},
		{"below minus 100%", model.Shock{Class: model.ClassBonds, Date: day, Change: -1.5}, ErrInvalidValue},
		{"not a number", model.Shock{Class: model.ClassBonds, Date: day, Change: math.NaN()}, ErrInvalidValue},
		{"no date", model.Shock{Class: model.ClassBonds, Date: model.MinDate, Change: -0.1}, ErrInvalidValue},
	} {
		if _, err := AddShock(st, rec, demodata.ProjBase, c.s, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.what, err)
		}
		if err := CheckShock(rec, c.s); !errors.Is(err, c.want) {
			t.Errorf("check, %s: %v", c.what, err)
		}
	}
	if _, err := AddShock(st, rec, "Nope", s, Options{}); !errors.Is(err, ErrUnknownProjection) {
		t.Errorf("unknown projection: %v", err)
	}
	if _, err := SetShock(st, rec, demodata.ProjBase, 4, s, Options{}); err == nil {
		t.Error("set out of range")
	}
	if _, err := RemoveShock(st, rec, demodata.ProjBase, 0, Options{}); err != nil || len(reload(t, st).Projection(demodata.ProjBase).Shocks) != 0 {
		t.Errorf("remove: %v", err)
	}
	rec = reload(t, st)
	if _, err := RemoveShock(st, rec, demodata.ProjBase, 0, Options{}); err == nil {
		t.Error("remove out of range")
	}
}

func TestRebalanceOps(t *testing.T) {
	st, rec := seed(t)
	day := model.D(2027, time.September, 1)
	r := model.Rebalance{Account: demodata.Brokerage, Date: day, Allocation: sixtyForty()}
	res, err := AddRebalance(st, rec, demodata.ProjBase, r, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjBase).Rebalances; len(got) != 1 || got[0].Account != r.Account || len(got[0].Allocation) != 2 {
		t.Fatalf("stored %+v", got)
	}
	rec = res.Record
	r.Allocation = []model.Weight{{Class: model.ClassLargeCap, Share: 1}, {Class: model.ClassBonds, Share: 0}}
	if _, err := SetRebalance(st, rec, demodata.ProjBase, 0, r, Options{}); err != nil || reload(t, st).Projection(demodata.ProjBase).Rebalances[0].Allocation[0].Share != 1 {
		t.Fatalf("set: %v", err)
	}
	rec = reload(t, st)
	bad := func(mod func(r *model.Rebalance)) model.Rebalance {
		c := model.Rebalance{Account: demodata.Brokerage, Date: day, Allocation: sixtyForty()}
		mod(&c)
		return c
	}
	for _, c := range []struct {
		what string
		r    model.Rebalance
		want error
	}{
		{"unknown account", bad(func(r *model.Rebalance) { r.Account = "nope" }), ErrUnknownObject},
		{"checking cannot hold an allocation", bad(func(r *model.Rebalance) { r.Account = demodata.Checking }), ErrInvalidValue},
		{"unknown class", bad(func(r *model.Rebalance) { r.Allocation[1].Class = "gold" }), ErrUnknownTarget},
		{"duplicate class", bad(func(r *model.Rebalance) { r.Allocation[1].Class = model.ClassLargeCap }), ErrInvalidValue},
		{"negative share", bad(func(r *model.Rebalance) { r.Allocation[0].Share, r.Allocation[1].Share = 1.2, -0.2 }), ErrInvalidValue},
		{"sum 90%", bad(func(r *model.Rebalance) { r.Allocation[1].Share = 0.3 }), ErrInvalidValue},
		{"no target", bad(func(r *model.Rebalance) { r.Allocation = nil }), ErrInvalidValue},
		{"no date", bad(func(r *model.Rebalance) { r.Date = model.MaxDate }), ErrInvalidValue},
	} {
		if _, err := AddRebalance(st, rec, demodata.ProjBase, c.r, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.what, err)
		}
	}
	if _, err := RemoveRebalance(st, rec, demodata.ProjBase, 0, Options{}); err != nil || len(reload(t, st).Projection(demodata.ProjBase).Rebalances) != 0 {
		t.Errorf("remove: %v", err)
	}
	rec = reload(t, st)
	if _, err := RemoveRebalance(st, rec, demodata.ProjBase, 0, Options{}); err == nil {
		t.Error("remove out of range")
	}
}

func TestRebalanceSumMessageIsFinerThanTolerance(t *testing.T) {
	_, rec := seed(t)
	r := model.Rebalance{Account: demodata.Brokerage, Date: model.D(2027, time.September, 1),
		Allocation: []model.Weight{{Class: model.ClassLargeCap, Share: 0.6}, {Class: model.ClassBonds, Share: 0.4002}}}
	err := CheckRebalance(rec, r)
	if err == nil || !strings.Contains(err.Error(), "sums to 100.02%") {
		t.Fatalf("got %v", err)
	}
}

func TestRebalanceOfAProjectionsOwnAccount(t *testing.T) {
	st, rec := seed(t)
	own := model.ProjAccount{ID: "future", Name: "Future", Type: model.TypeBrokerage, Tax: model.Taxable, From: model.D(2027, time.January, 1)}
	if _, err := AddProjAccount(st, rec, demodata.ProjBase, own, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	r := model.Rebalance{Account: "future", Date: model.D(2027, time.September, 1), Allocation: sixtyForty()}
	if _, err := AddRebalance(st, rec, demodata.ProjBase, r, Options{}); err != nil {
		t.Errorf("rebalance of an account the projection defines: %v", err)
	}
}

func TestSetRebalanceYearly(t *testing.T) {
	st, rec := seed(t)
	res, err := SetRebalanceYearly(st, rec, demodata.Brokerage, true, Options{})
	if err != nil || !res.Account.RebalanceYearly || !reload(t, st).Account(demodata.Brokerage).RebalanceYearly {
		t.Fatalf("on: %v", err)
	}
	if _, err := SetRebalanceYearly(st, res.Record, demodata.Brokerage, true, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("on again: %v", err)
	}
	if _, err := SetRebalanceYearly(st, res.Record, demodata.Brokerage, false, Options{}); err != nil || reload(t, st).Account(demodata.Brokerage).RebalanceYearly {
		t.Errorf("off: %v", err)
	}
	if _, err := SetRebalanceYearly(st, rec, demodata.Checking, true, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("checking: %v", err)
	}
	if _, err := SetRebalanceYearly(st, rec, "nope", true, Options{}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown: %v", err)
	}
}

func TestDeletingAnAccountRemovesItsRebalances(t *testing.T) {
	st, rec := seed(t)
	acct := model.Account{ID: "extra", Name: "Extra", Type: model.TypeBrokerage, Tax: model.Taxable, Allocation: []model.Holding{{Class: model.ClassLargeCap, Percent: 100}}}
	if _, err := NewAccount(st, rec, acct, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	for _, acct := range []model.AccountID{"extra", demodata.Brokerage} {
		res, err := AddRebalance(st, rec, demodata.ProjBase, model.Rebalance{Account: acct, Date: model.D(2027, time.September, 1), Allocation: sixtyForty()}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		rec = res.Record
	}
	if _, err := DeleteAccount(st, rec, "extra", Options{}); err != nil {
		t.Fatal(err)
	}
	got := reload(t, st).Projection(demodata.ProjBase).Rebalances
	if len(got) != 1 || got[0].Account != demodata.Brokerage {
		t.Errorf("rebalances after the delete: %+v", got)
	}
}

func TestRemoveClassRefusedWhileAShockNamesIt(t *testing.T) {
	st, rec := seed(t)
	res, err := AddShock(st, rec, demodata.ProjBase, model.Shock{Class: model.ClassCash, Date: model.D(2027, time.March, 2), Change: -0.1}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveClass(st, res.Record, model.ClassCash, Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("remove a class a shock names: %v", err)
	}
}
