// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestClassLifecycle(t *testing.T) {
	st, rec := seed(t)
	if _, err := AddClass(st, rec, model.MarketClass{Name: "Gold", Return: model.Constant(0.02), Volatility: 0.15}, Options{DryRun: true}); err != nil || rec.Class("gold") != nil || reload(t, st).Class("gold") != nil {
		t.Fatalf("dry run: %v", err)
	}
	res, err := AddClass(st, rec, model.MarketClass{Name: "Gold", Return: model.Constant(0.02), Volatility: 0.15}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Class("gold"); got == nil || got.Name != "Gold" || got.Volatility != 0.15 {
		t.Fatalf("stored %+v", got)
	}
	rec = res.Record
	for _, c := range []struct {
		what string
		cl   model.MarketClass
		want error
	}{
		{"id taken", model.MarketClass{ID: "gold", Name: "Other"}, ErrNameTaken},
		{"name taken", model.MarketClass{Name: "gold"}, ErrNameTaken},
		{"no name", model.MarketClass{Name: " "}, ErrInvalidValue},
		{"rate -100%", model.MarketClass{Name: "Bad", Return: model.Constant(-1)}, ErrInvalidValue},
		{"negative volatility", model.MarketClass{Name: "Bad", Volatility: -0.1}, ErrInvalidValue},
	} {
		if _, err := AddClass(st, rec, c.cl, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.what, err)
		}
	}

	name, ret, vol := "Bullion", 0.03, 0.2
	res, err = EditClass(st, rec, "gold", ClassChange{Name: &name, Return: &ret, Volatility: &vol}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Class("gold"); got.Name != name || !reflect.DeepEqual(got.Return, model.Constant(ret)) || got.Volatility != vol {
		t.Errorf("edited %+v", got)
	}
	if _, err := EditClass(st, res.Record, "gold", ClassChange{Name: &name}, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same again: %v", err)
	}
	if _, err := EditClass(st, res.Record, "nope", ClassChange{Name: &name}, Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("unknown: %v", err)
	}
	other := "US bonds"
	if _, err := EditClass(st, res.Record, "gold", ClassChange{Name: &other}, Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("name of another class: %v", err)
	}

	if _, err := SetCorrelation(st, res.Record, "gold", model.ClassLargeCap, 0.3, Options{}); err != nil {
		t.Fatal(err)
	}
	got := reload(t, st)
	if v := got.CorrelationOf(model.ClassLargeCap, "gold"); v != 0.3 {
		t.Errorf("correlation %v", v)
	}
	for _, c := range []struct {
		what string
		a, b string
		v    float64
		want error
	}{
		{"same again", "gold", model.ClassLargeCap, 0.3, ErrNoChange},
		{"unknown class", "gold", "nope", 0.3, ErrUnknownTarget},
		{"itself", "gold", "gold", 0.3, ErrInvalidValue},
		{"above 1", "gold", model.ClassBonds, 1.1, ErrInvalidValue},
	} {
		if _, err := SetCorrelation(st, got, c.a, c.b, c.v, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.what, err)
		}
	}

	if _, err := RemoveClass(st, got, "gold", Options{}); err != nil {
		t.Fatal(err)
	}
	if after := reload(t, st); after.Class("gold") != nil || after.CorrelationOf("gold", model.ClassLargeCap) != 0 {
		t.Errorf("removed class remains: %+v", after.Classes)
	}
	if _, err := RemoveClass(st, got, "nope", Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("unknown: %v", err)
	}
}

func TestRemoveClassRefusedWhileInUse(t *testing.T) {
	st, rec := seed(t)
	// Allocations and the demo's class modification both name large cap.
	if _, err := RemoveClass(st, rec, model.ClassLargeCap, Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("allocated: %v", err)
	}
	for _, id := range []model.AccountID{demodata.Brokerage, demodata.K401, demodata.Roth, demodata.TSP, demodata.College1, demodata.College2, demodata.College3} {
		res, err := SetAllocation(st, rec, id, nil, false, Options{})
		if err != nil {
			t.Fatal(err)
		}
		rec = res.Record
	}
	if _, err := RemoveClass(st, rec, model.ClassLargeCap, Options{}); !errors.Is(err, ErrInUse) {
		t.Errorf("modified by a projection: %v", err)
	}
	if _, err := RemoveClass(st, rec, model.ClassBonds, Options{}); err != nil {
		t.Errorf("unused: %v", err)
	}
}

func TestSetClassGrowth(t *testing.T) {
	st, rec := seed(t)
	growth := model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(model.D(2028, 12, 31)), Value: 0.08}, model.Segment{From: model.D(2029, 1, 1), Value: 0.03})
	res, err := SetClassGrowth(st, rec, model.ClassBonds, &growth, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Class(model.ClassBonds).Return; !reflect.DeepEqual(got.Segments(), growth.Segments()) {
		t.Errorf("stored %v", got.Segments())
	}
	if _, err := SetClassGrowth(st, res.Record, model.ClassBonds, &growth, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same again: %v", err)
	}
	if _, err := SetClassGrowth(st, rec, "nope", &growth, Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("unknown: %v", err)
	}
	bad := model.Constant(-1)
	if _, err := SetClassGrowth(st, rec, model.ClassBonds, &bad, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("-100%%: %v", err)
	}
	if _, err := SetClassGrowth(st, res.Record, model.ClassBonds, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Class(model.ClassBonds).Return; len(got.Segments()) != 0 {
		t.Errorf("cleared: %v", got.Segments())
	}
}

func TestSetAllocation(t *testing.T) {
	st, rec := seed(t)
	mix := []model.Holding{{Class: model.ClassSmallCap, Percent: 70}, {Class: model.ClassCash, Percent: 30}}
	if _, err := SetAllocation(st, rec, demodata.Brokerage, mix, false, Options{DryRun: true}); err != nil || !reflect.DeepEqual(reload(t, st).Account(demodata.Brokerage).Allocation, model.DefaultAllocation(model.TypeBrokerage)) {
		t.Fatalf("dry run: %v", err)
	}
	res, err := SetAllocation(st, rec, demodata.Brokerage, mix, false, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Account(demodata.Brokerage).Allocation; !reflect.DeepEqual(got, mix) {
		t.Errorf("stored %v", got)
	}
	if _, err := SetAllocation(st, res.Record, demodata.Brokerage, mix, false, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same again: %v", err)
	}
	res, err = SetAllocation(st, res.Record, demodata.Brokerage, nil, true, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Account(demodata.Brokerage).Allocation; !reflect.DeepEqual(got, model.DefaultAllocation(model.TypeBrokerage)) {
		t.Errorf("default: %v", got)
	}
	res, err = SetAllocation(st, res.Record, demodata.Brokerage, nil, false, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Account(demodata.Brokerage).Allocation; got != nil {
		t.Errorf("cleared: %v", got)
	}
	if _, err := SetAllocation(st, res.Record, demodata.Brokerage, nil, false, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("clear again: %v", err)
	}
	for _, c := range []struct {
		what    string
		id      model.AccountID
		alloc   []model.Holding
		useDef  bool
		wantErr error
	}{
		{"sum", demodata.Brokerage, []model.Holding{{Class: model.ClassCash, Percent: 90}}, false, ErrInvalidValue},
		{"not an investment account", demodata.Checking, mix, false, ErrInvalidValue},
		{"no default for the type", demodata.Checking, nil, true, ErrInvalidValue},
		{"both", demodata.Brokerage, mix, true, ErrInvalidValue},
		{"unknown class", demodata.Brokerage, []model.Holding{{Class: "nope", Percent: 100}}, false, ErrUnknownTarget},
		{"unknown account", "nope", mix, false, ErrUnknownObject},
	} {
		if _, err := SetAllocation(st, res.Record, c.id, c.alloc, c.useDef, Options{}); !errors.Is(err, c.wantErr) {
			t.Errorf("%s: %v", c.what, err)
		}
	}
	typ := model.TypeChecking
	if _, err := EditAccount(st, rec, demodata.K401, AccountChange{Type: &typ}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("a type that cannot hold the allocation: %v", err)
	}
}

func TestClassModification(t *testing.T) {
	st, rec := seed(t)
	mod := func(id, prop string, v float64) model.Modification {
		return model.Modification{Target: model.ObjectRef{Kind: model.KindClass, ID: id}, Property: prop, Values: model.Constant(v)}
	}
	if _, err := AddMod(st, rec, demodata.ProjWithdraw, mod(model.ClassBonds, model.PropGrowth, 0.02), Options{}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		what string
		m    model.Modification
		want error
	}{
		{"unknown class", mod("nope", model.PropGrowth, 0.02), ErrUnknownTarget},
		{"property", mod(model.ClassBonds, model.PropAmount, 0.02), ErrInvalidValue},
		{"-100%", mod(model.ClassBonds, model.PropGrowth, -1), ErrInvalidValue},
	} {
		if _, err := AddMod(st, rec, demodata.ProjWithdraw, c.m, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.what, err)
		}
	}
}
