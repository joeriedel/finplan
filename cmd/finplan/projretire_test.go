// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestInheritedRetirement(t *testing.T) {
	rec := demodata.Family()
	child := &model.Projection{Name: "Child", Parent: demodata.ProjRetire60, WithdrawalOrder: []model.Bucket{model.BucketRoth, model.BucketDeferred, model.BucketTaxable}}
	if err := rec.AddProjection(child); err != nil {
		t.Fatal(err)
	}
	retire, order, bracket := inheritedRetirement(rec, child)
	if retire == nil || *retire != model.D(2045, 3, 12) || bracket == nil || *bracket != 0.22 || !slices.Equal(order, model.DefaultWithdrawalOrder()) {
		t.Errorf("a child inherits what its parent sets, not what it sets itself: %v %v %v", retire, order, bracket)
	}
	retire, order, bracket = inheritedRetirement(rec, rec.Projection(demodata.ProjRetire60))
	if retire != nil || bracket != nil || !slices.Equal(order, model.DefaultWithdrawalOrder()) {
		t.Errorf("Base sets none, so Retire at 60 inherits none: %v %v %v", retire, order, bracket)
	}
}

func TestRetirementFields(t *testing.T) {
	if d, err := parseRetireOn(""); d != nil || err != nil {
		t.Errorf("empty inherits: %v %v", d, err)
	}
	if d, err := parseRetireOn("2045-03-12"); err != nil || *d != model.D(2045, 3, 12) {
		t.Errorf("%v %v", d, err)
	}
	if _, err := parseRetireOn("soon"); err == nil {
		t.Error("soon accepted")
	}
	for _, text := range []string{"22", "22%", " 22 "} {
		if v, err := parseRothBracket(text); err != nil || *v != 0.22 {
			t.Errorf("%q: %v %v", text, v, err)
		}
	}
	if v, err := parseRothBracket(""); v != nil || err != nil {
		t.Errorf("empty inherits: %v %v", v, err)
	}
	if _, err := parseRothBracket("high"); err == nil {
		t.Error("high accepted")
	}
}

func TestWithdrawalOrders(t *testing.T) {
	orders := withdrawalOrders()
	if len(orders) != 6 {
		t.Fatalf("%d orders: %v", len(orders), orders)
	}
	for i, o := range orders {
		if !model.ValidWithdrawalOrder(o) || slices.ContainsFunc(orders[:i], func(p []model.Bucket) bool { return slices.Equal(p, o) }) {
			t.Errorf("order %v is invalid or repeated", o)
		}
	}
	if got := orderText(model.DefaultWithdrawalOrder()); got != "Taxable, then Tax-deferred, then Roth" {
		t.Errorf("order text: %q", got)
	}
}
