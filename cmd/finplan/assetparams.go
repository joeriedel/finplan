// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// growthLayer is one layer of an account's growth, as the table of effective growth and asset params show it.
type growthLayer struct {
	// Asset is the asset whose growth setting this is; it is empty for the account's stated rate.
	Asset    model.AssetID
	Schedule model.Schedule
	// Today is the layer's value on the record's today, if it has one.
	Today    float64
	HasToday bool
	// Applies is true for the layer that sets the effective value today: the last that has a value then. Layers before it are overwritten.
	Applies bool
}

// accountGrowth is the growth of one account under an asset: its layers in the order they apply, and what they add up to.
type accountGrowth struct {
	Account   model.AccountID
	Name      string
	Layers    []growthLayer
	Effective model.Schedule
	// Today is the effective value on the record's today.
	Today    float64
	HasToday bool
}

// assetGrowth returns the growth of each account under the asset, layered by the same function the engine uses.
func assetGrowth(rec *model.Record, id model.AssetID) ([]accountGrowth, error) {
	res, err := rec.Resolve(id)
	if err != nil {
		return nil, err
	}
	var out []accountGrowth
	for _, acct := range res.Accounts {
		layers, eff := res.GrowthLayers(acct)
		g := accountGrowth{Account: acct, Name: accountName(rec, acct), Effective: eff}
		g.Today, g.HasToday = eff.At(rec.Today)
		last := -1
		for i, l := range layers {
			gl := growthLayer{Asset: l.Asset, Schedule: l.Value}
			gl.Today, gl.HasToday = l.Value.At(rec.Today)
			if gl.HasToday {
				last = i
			}
			g.Layers = append(g.Layers, gl)
		}
		if last >= 0 {
			g.Layers[last].Applies = true
		}
		out = append(out, g)
	}
	return out, nil
}

// addIntervalProblem says why the interval the form describes cannot be added to the asset's default balances, or "" if it can.
func addIntervalProblem(st *store.Store, rec *model.Record, id model.AssetID, from, to, value string) string {
	iv, err := interval(from, to, value)
	if err != nil {
		return err.Error()
	}
	_, err = ops.AddDefault(st, rec, id, iv, ops.Options{DryRun: true})
	return reasonOf(err)
}

// editIntervalProblem says why the interval at index cannot be replaced by the interval the texts describe, or "" if it can; the same interval is no problem.
func editIntervalProblem(st *store.Store, rec *model.Record, id model.AssetID, index int, from, to, value string) string {
	iv, err := interval(from, to, value)
	if err != nil {
		return err.Error()
	}
	_, err = ops.EditDefault(st, rec, id, index, iv, ops.Options{DryRun: true})
	if errors.Is(err, ops.ErrNoChange) {
		return ""
	}
	return reasonOf(err)
}

// intervalTexts writes an interval as the texts of its from, to and balance fields: a blank from is the beginning of time and a blank to no end.
func intervalTexts(iv model.Interval) (from, to, value string) {
	if iv.From != model.MinDate {
		from = iv.From.String()
	}
	if iv.To != nil {
		to = iv.To.String()
	}
	return from, to, trimFloat(iv.Value)
}

// Kinds of growth setting an asset has.
const (
	growthNone     = "none"
	growthConstant = "constant"
	growthSchedule = "schedule"
)

// growthKind says whether the asset's growth setting is absent, one rate for all time, or a schedule; for a constant it also gives the rate.
func growthKind(a *model.Asset) (kind string, rate float64) {
	s, ok := a.Params[model.PropGrowth]
	if !ok || len(s.Segments()) == 0 {
		return growthNone, 0
	}
	if v, from, ok := constantOf(s); ok && from == nil {
		return growthConstant, v
	}
	return growthSchedule, 0
}

// growthConstantProblem says why the percentage typed cannot become the asset's growth setting, or "" if it can; the rate it has is no problem.
func growthConstantProblem(st *store.Store, rec *model.Record, id model.AssetID, text string) string {
	_, err := setGrowthConstant(st, rec, id, text, ops.Options{DryRun: true})
	if errors.Is(err, ops.ErrNoChange) {
		return ""
	}
	return reasonOf(err)
}

// setGrowthConstant sets the asset's growth to the percentage typed, for all time.
func setGrowthConstant(st *store.Store, rec *model.Record, id model.AssetID, text string, o ops.Options) (*ops.Result, error) {
	v, err := parseValue(model.PropGrowth, model.ModeSet, text)
	if err != nil {
		return nil, err
	}
	s := model.Constant(v)
	return ops.SetParam(st, rec, id, model.PropGrowth, &s, o)
}
