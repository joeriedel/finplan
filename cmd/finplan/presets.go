// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// recordHas returns the predicate for Preset.Restore: whether the asset exists, and the projection if one is named. An asset exists if the record has it, or, as the accounts a projection defines are assets only under it, if the named projection does; a line with no projection also keeps an asset of any projection, as it may follow one.
func recordHas(rec *model.Record) func(asset model.AssetID, projection string) bool {
	return func(asset model.AssetID, projection string) bool {
		switch {
		case projection != "":
			return rec.Projection(projection) != nil && effective(rec, projection).Asset(asset) != nil
		case rec.Asset(asset) != nil:
			return true
		}
		return slices.ContainsFunc(rec.Projections, func(p *model.Projection) bool { return effective(rec, p.Name).Asset(asset) != nil })
	}
}

// projectionNames returns the names of the record's projections in order.
func projectionNames(rec *model.Record) []string {
	var names []string
	for _, p := range rec.Projections {
		names = append(names, p.Name)
	}
	return names
}

// loadPresets returns the stored view presets. A preset that cannot be decoded is left out and described in
// problems.
func loadPresets(st *store.Store) (presets []graph.Preset, problems []string, err error) {
	rows, err := st.Presets()
	if err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		p, err := graph.DecodePreset(r.Name, r.Format, r.Body)
		if err != nil {
			problems = append(problems, fmt.Sprintf("View preset %q was skipped: %v", r.Name, err))
			continue
		}
		presets = append(presets, p)
	}
	return presets, problems, nil
}

// familyPresets returns the invented view presets of the Family record, which the demo database holds. A preset whose assets or
// projections the record lacks is left out.
func familyPresets(rec *model.Record) []graph.Preset {
	has := func(proj string) bool {
		for _, p := range rec.Projections {
			if p.Name == proj {
				return true
			}
		}
		return false
	}
	span := func(fromDays, toDays int) (float64, float64) {
		return float64(rec.Today.Add(fromDays)) * secondsPerDay, float64(rec.Today.Add(toDays)) * secondsPerDay
	}
	var out []graph.Preset

	retirement := []model.AssetID{demodata.Retirement, demodata.NetWorth, demodata.AllCash}
	ok := has(demodata.ProjBase)
	for _, id := range retirement {
		ok = ok && rec.Asset(id) != nil
	}
	if ok {
		l := graph.NewLines(demodata.ProjBase)
		for _, id := range retirement {
			l.Add(id, false, "", graph.AssetColor(rec, id))
		}
		from, to := span(-365, 30*365)
		out = append(out, graph.Capture("Retirement overview", l, from, to))
	}

	k401 := model.DefaultAssetID(demodata.K401)
	if has(demodata.ProjBase) && has(demodata.ProjWithdraw) && rec.Asset(k401) != nil {
		l := graph.NewLines(demodata.ProjBase)
		base := l.Add(k401, true, demodata.ProjBase, graph.AssetColor(rec, k401))
		withdraw := l.Add(k401, true, demodata.ProjWithdraw, graph.AssetColor(rec, k401))
		band := l.AddBand(base, withdraw, graph.AssetColor(rec, k401))
		l.SetBandHatch(band, true)
		from, to := span(-365, 20*365)
		out = append(out, graph.Capture("Early withdrawal cost", l, from, to))
	}
	if has(demodata.ProjBase) && rec.Asset(demodata.NetWorth) != nil {
		l := graph.NewLines(demodata.ProjBase)
		id := l.Add(demodata.NetWorth, false, "", graph.AssetColor(rec, demodata.NetWorth))
		l.SetMonteCarlo(id, true)
		from, to := span(-365, 30*365)
		out = append(out, graph.Capture("Monte Carlo net worth", l, from, to))
	}
	return out
}
