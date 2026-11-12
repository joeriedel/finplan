// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"slices"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

func TestSetRetirementSettings(t *testing.T) {
	on := model.Ptr(model.D(2040, 6, 1))
	order := []model.Bucket{model.BucketRoth, model.BucketTaxable, model.BucketDeferred}
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetRetirement(st, rec, demodata.ProjBase, on, o)
	})
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetWithdrawalOrder(st, rec, demodata.ProjBase, order, o)
	})
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetRothBracket(st, rec, demodata.ProjBase, model.Ptr(0.24), o)
	})

	st, rec := seed(t)
	res, err := SetRetirement(st, rec, demodata.ProjBase, on, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res, err = SetWithdrawalOrder(st, res.Record, demodata.ProjBase, order, Options{}); err != nil {
		t.Fatal(err)
	}
	if res, err = SetRothBracket(st, res.Record, demodata.ProjBase, model.Ptr(0.24), Options{}); err != nil {
		t.Fatal(err)
	}
	got := reload(t, st).Projection(demodata.ProjBase)
	if got.RetireOn == nil || *got.RetireOn != *on || !slices.Equal(got.WithdrawalOrder, order) || got.RothBracket == nil || *got.RothBracket != 0.24 {
		t.Errorf("stored %+v", got)
	}
	// Each setting is cleared back to inheriting.
	if res, err = SetRetirement(st, res.Record, demodata.ProjBase, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if res, err = SetWithdrawalOrder(st, res.Record, demodata.ProjBase, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err = SetRothBracket(st, res.Record, demodata.ProjBase, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	got = reload(t, st).Projection(demodata.ProjBase)
	if got.RetireOn != nil || got.WithdrawalOrder != nil || got.RothBracket != nil {
		t.Errorf("not cleared: %+v", got)
	}

	retire60 := func(rec *model.Record) *model.Projection { return rec.Projection(demodata.ProjRetire60) }
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetRetirement(st, rec, demodata.ProjRetire60, retire60(rec).RetireOn, Options{})
	})
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetRothBracket(st, rec, demodata.ProjRetire60, retire60(rec).RothBracket, Options{})
	})
	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetRetirement(st, rec, demodata.ProjBase, model.Ptr(model.D(1800, 1, 1)), Options{})
	})
	for _, bad := range [][]model.Bucket{
		{model.BucketTaxable, model.BucketRoth},
		{model.BucketTaxable, model.BucketTaxable, model.BucketRoth},
		{model.BucketTaxable, model.BucketRoth, "other"},
		{},
	} {
		refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
			return SetWithdrawalOrder(st, rec, demodata.ProjBase, bad, Options{})
		})
	}
	for _, bad := range []float64{0.23, 0, 1, -0.22} {
		refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
			return SetRothBracket(st, rec, demodata.ProjBase, &bad, Options{})
		})
	}
	refused(t, ErrUnknownProjection, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetRetirement(st, rec, "Nowhere", on, Options{})
	})
}
