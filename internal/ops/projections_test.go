// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

func TestSetDrift(t *testing.T) {
	dryRunMatches(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetDrift(st, rec, demodata.ProjBase, model.Ptr(3), o)
	})

	st, rec := seed(t)
	res, err := SetDrift(st, rec, demodata.ProjBase, model.Ptr(3), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjBase); got.DriftMonths == nil || *got.DriftMonths != 3 {
		t.Errorf("stored %+v", got.DriftMonths)
	}
	// Zero is off, which is stored, and nil inherits again.
	if res, err = SetDrift(st, res.Record, demodata.ProjBase, model.Ptr(0), Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjBase); got.DriftMonths == nil || *got.DriftMonths != 0 {
		t.Errorf("off is not stored: %+v", got.DriftMonths)
	}
	if _, err = SetDrift(st, res.Record, demodata.ProjBase, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).Projection(demodata.ProjBase); got.DriftMonths != nil {
		t.Errorf("not cleared: %v", *got.DriftMonths)
	}

	refused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetDrift(st, rec, demodata.ProjBase, model.Ptr(-1), Options{})
	})
	refused(t, ErrNoChange, func(st *store.Store, rec *model.Record) (*Result, error) {
		return SetDrift(st, rec, demodata.ProjDrift, model.Ptr(6), Options{})
	})
}
