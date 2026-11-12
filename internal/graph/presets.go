// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/joeriedel/finplan/internal/model"
)

// PresetFormat is the version of the encoding EncodePreset writes.
const PresetFormat = 1

// ErrUnknownPresetFormat is returned by DecodePreset for a format version this build cannot read.
var ErrUnknownPresetFormat = errors.New("graph: unknown preset format version")

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
	MonteCarlo bool
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
		p.Lines = append(p.Lines, PresetLine{ln.Asset, ln.Pinned, ln.Projection, ln.Color, ln.Visible, l.solo[ln.ID], ln.MonteCarlo})
	}
	for _, b := range l.bands {
		p.Bands = append(p.Bands, PresetBand{at[b.Upper], at[b.Lower], b.Color, b.Hatch, b.TodayDollars, b.Visible})
	}
	return p
}

// RenameProjection returns the preset with the projection from renamed to to in its switcher and pinned lines, and
// whether anything changed. The result shares nothing with p.
func (p Preset) RenameProjection(from, to string) (Preset, bool) {
	out := p
	out.Lines = append([]PresetLine(nil), p.Lines...)
	out.Bands = append([]PresetBand(nil), p.Bands...)
	changed := false
	if out.Switcher == from {
		out.Switcher, changed = to, true
	}
	for i, l := range out.Lines {
		if l.Pinned && l.Projection == from {
			out.Lines[i].Projection, changed = to, true
		}
	}
	return out, changed
}

// RemoveAssets returns the preset without the lines of the given assets and without the bands that used one of them
// (or whose line indexes are out of range), the remaining bands re-indexed, and how many lines and bands it removed.
// The result shares nothing with p. A preset with none of the assets comes back as it is, with 0 and 0.
func (p Preset) RemoveAssets(assets ...model.AssetID) (out Preset, lines, bands int) {
	out = p
	out.Lines = nil
	out.Bands = nil
	// at maps an old line index to its new one, -1 for a removed line.
	at := make([]int, len(p.Lines))
	for i, l := range p.Lines {
		if slices.Contains(assets, l.Asset) {
			at[i] = -1
			lines++
			continue
		}
		at[i] = len(out.Lines)
		out.Lines = append(out.Lines, l)
	}
	if lines == 0 {
		return p, 0, 0
	}
	for _, b := range p.Bands {
		if b.Upper < 0 || b.Upper >= len(at) || b.Lower < 0 || b.Lower >= len(at) || at[b.Upper] < 0 || at[b.Lower] < 0 {
			bands++
			continue
		}
		b.Upper, b.Lower = at[b.Upper], at[b.Lower]
		out.Bands = append(out.Bands, b)
	}
	return out, lines, bands
}

// Restore builds a line list from the preset, with new ids, the exact colours and no selection. has tells
// whether the current record has the asset and, when projection is not empty, that projection; a line it
// rejects is skipped, and so are the bands that use it. Bands whose line indexes are out of range or equal are
// skipped too. The switcher is the preset's if projections lists it, else the first of projections, or empty.
func (p Preset) Restore(has func(asset model.AssetID, projection string) bool, projections []string) *Lines {
	switcher := ""
	if len(projections) > 0 {
		switcher = projections[0]
	}
	for _, name := range projections {
		if name == p.Switcher {
			switcher = name
		}
	}
	l := NewLines(switcher)
	// ids holds the new id of each kept line and 0 for a skipped one, so band indexes need no remapping.
	ids := make([]LineID, len(p.Lines))
	for i, pl := range p.Lines {
		if !has(pl.Asset, pl.Projection) {
			continue
		}
		ids[i] = l.Add(pl.Asset, pl.Pinned, pl.Projection, pl.Color)
		l.SetColor(ids[i], pl.Color)
		l.SetVisible(ids[i], pl.Visible)
		l.SetMonteCarlo(ids[i], pl.MonteCarlo)
		if pl.Soloed {
			l.ToggleSolo(ids[i])
		}
	}
	for _, pb := range p.Bands {
		if pb.Upper < 0 || pb.Upper >= len(ids) || pb.Lower < 0 || pb.Lower >= len(ids) || ids[pb.Upper] == 0 || ids[pb.Lower] == 0 {
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

type presetDoc struct {
	Switcher string    `json:"switcher"`
	Lines    []lineDoc `json:"lines"`
	Bands    []bandDoc `json:"bands"`
	From     float64   `json:"from"`
	To       float64   `json:"to"`
}

type lineDoc struct {
	Asset      model.AssetID `json:"asset"`
	Pinned     bool          `json:"pinned"`
	Projection string        `json:"projection"`
	Color      uint32        `json:"color"` // 0xRRGGBB
	Visible    bool          `json:"visible"`
	Soloed     bool          `json:"soloed"`
	// MonteCarlo is absent from presets saved before the bands existed, which decode with it off.
	MonteCarlo bool `json:"monteCarlo,omitempty"`
}

type bandDoc struct {
	Upper        int    `json:"upper"`
	Lower        int    `json:"lower"`
	Color        uint32 `json:"color"` // 0xRRGGBB
	Hatch        bool   `json:"hatch"`
	TodayDollars bool   `json:"todayDollars"`
	Visible      bool   `json:"visible"`
}

func packColor(c Color) uint32 { return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B) }

// EncodePreset returns the body of the preset in PresetFormat. The name is not part of the body.
func EncodePreset(p Preset) ([]byte, error) {
	d := presetDoc{Switcher: p.Switcher, Lines: []lineDoc{}, Bands: []bandDoc{}, From: p.From, To: p.To}
	for _, l := range p.Lines {
		d.Lines = append(d.Lines, lineDoc{l.Asset, l.Pinned, l.Projection, packColor(l.Color), l.Visible, l.Soloed, l.MonteCarlo})
	}
	for _, b := range p.Bands {
		d.Bands = append(d.Bands, bandDoc{b.Upper, b.Lower, packColor(b.Color), b.Hatch, b.TodayDollars, b.Visible})
	}
	return json.Marshal(d)
}

// DecodePreset rebuilds the preset named name from a body written in the given format. It returns an error
// matching ErrUnknownPresetFormat for a format this build does not know.
func DecodePreset(name string, format int, body []byte) (Preset, error) {
	if format != PresetFormat {
		return Preset{}, fmt.Errorf("%w: %d", ErrUnknownPresetFormat, format)
	}
	var d presetDoc
	if err := json.Unmarshal(body, &d); err != nil {
		return Preset{}, fmt.Errorf("graph: preset %q: %w", name, err)
	}
	p := Preset{Name: name, Switcher: d.Switcher, From: d.From, To: d.To}
	for _, l := range d.Lines {
		p.Lines = append(p.Lines, PresetLine{l.Asset, l.Pinned, l.Projection, RGB(l.Color), l.Visible, l.Soloed, l.MonteCarlo})
	}
	for _, b := range d.Bands {
		p.Bands = append(p.Bands, PresetBand{b.Upper, b.Lower, RGB(b.Color), b.Hatch, b.TodayDollars, b.Visible})
	}
	return p, nil
}
