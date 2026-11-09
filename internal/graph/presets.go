// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import "github.com/joeriedel/finplan/internal/model"

// Preset is a saved view of the Graph tab: the lines, the switcher, the bands and the visible dates. It is plain
// data, with lines referred to by index into Lines, so that it can be stored as it is.
type Preset struct {
	Name     string
	Switcher string
	Lines    []PresetLine
	Bands    []PresetBand
	// From and To are the visible x range in unix seconds.
	From, To float64
}

// PresetLine is a line of a preset.
type PresetLine struct {
	Asset      model.AssetID
	Pinned     bool
	Projection string
	Color      Color
	Visible    bool
	Soloed     bool
}

// PresetBand is a band of a preset; Upper and Lower index Preset.Lines.
type PresetBand struct {
	Upper, Lower int
	Color        Color
	Hatch        bool
	TodayDollars bool
	Visible      bool
}

// Capture returns a preset, named name, of the lines and the x range from..to. The selection is not captured.
func Capture(name string, l *Lines, from, to float64) Preset {
	p := Preset{Name: name, Switcher: l.switcher, From: from, To: to}
	at := map[LineID]int{}
	for i, ln := range l.lines {
		at[ln.ID] = i
		p.Lines = append(p.Lines, PresetLine{ln.Asset, ln.Pinned, ln.Projection, ln.Color, ln.Visible, l.solo[ln.ID]})
	}
	for _, b := range l.bands {
		p.Bands = append(p.Bands, PresetBand{at[b.Upper], at[b.Lower], b.Color, b.Hatch, b.TodayDollars, b.Visible})
	}
	return p
}

// Restore builds a line list from the preset, with new ids, the exact colours and no selection. Bands whose
// line indexes are out of range or equal are skipped.
func (p Preset) Restore() *Lines {
	l := NewLines(p.Switcher)
	ids := make([]LineID, len(p.Lines))
	for i, pl := range p.Lines {
		ids[i] = l.Add(pl.Asset, pl.Pinned, pl.Projection, pl.Color)
		l.SetColor(ids[i], pl.Color)
		l.SetVisible(ids[i], pl.Visible)
		if pl.Soloed {
			l.ToggleSolo(ids[i])
		}
	}
	for _, pb := range p.Bands {
		if pb.Upper < 0 || pb.Upper >= len(ids) || pb.Lower < 0 || pb.Lower >= len(ids) {
			continue
		}
		id := l.AddBand(ids[pb.Upper], ids[pb.Lower], pb.Color)
		if id == 0 {
			continue
		}
		l.SetBandHatch(id, pb.Hatch)
		l.SetBandTodayDollars(id, pb.TodayDollars)
		l.SetBandVisible(id, pb.Visible)
	}
	return l
}
