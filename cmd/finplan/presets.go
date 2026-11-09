// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// familyPresets returns the invented view presets that ship with the Family record. A preset whose assets or
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
	return out
}
