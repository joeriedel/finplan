// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"errors"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/model"
)

func hasAll(model.AssetID, string) bool { return true }

func TestPresetRoundTrip(t *testing.T) {
	l := NewLines("Base")
	a := l.Add("cash", false, "", RGB(0x112233))
	b := l.Add("cash", true, "Withdraw", RGB(0x112233)) // lightened by Add as a duplicate
	c := l.Add("home", false, "", RGB(0x445566))
	l.SetColor(c, RGB(0xabcdef))
	l.SetVisible(c, false)
	l.ToggleSolo(a)
	l.SetMonteCarlo(c, true)
	l.Select(b)
	ab := l.AddBand(b, a, RGB(0x010203))
	l.SetBandHatch(ab, true)
	l.SetBandTodayDollars(ab, true)
	ac := l.AddBand(a, c, RGB(0x040506))
	l.SetBandVisible(ac, false)

	p := Capture("mine", l, 100, 200)
	if len(p.Lines) != 3 || len(p.Bands) != 2 || !p.Lines[0].Soloed || !p.Lines[1].Pinned || p.Bands[0].Upper != 1 || p.Bands[0].Lower != 0 {
		t.Fatalf("unexpected capture: %+v", p)
	}
	r := p.Restore(hasAll, []string{"Base", "Withdraw"})
	if _, ok := r.Selected(); ok {
		t.Error("selection restored")
	}
	if got := Capture("mine", r, 100, 200); !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, p)
	}
	if r.Switcher() != "Base" || r.SoloCount() != 1 {
		t.Error("switcher or solo lost")
	}
	if got := r.All(); got[0].MonteCarlo || got[1].MonteCarlo || !got[2].MonteCarlo {
		t.Errorf("Monte Carlo flags after restore: %+v", got)
	}
	body, err := EncodePreset(p)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := DecodePreset("mine", PresetFormat, body); err != nil || !reflect.DeepEqual(back, p) {
		t.Errorf("encode and decode: %v\n got %+v\nwant %+v", err, back, p)
	}
	// The restored list is independent of the original.
	l.Remove(a)
	if len(r.All()) != 3 || len(r.Bands()) != 2 {
		t.Error("restored list shares state")
	}
}

func TestPresetRestoreSkipsBadBands(t *testing.T) {
	p := Preset{Switcher: "Base", Lines: []PresetLine{{Asset: "cash", Visible: true}}, Bands: []PresetBand{{0, 0, Color{}, false, false, true}, {0, 5, Color{}, false, false, true}}}
	if got := p.Restore(hasAll, []string{"Base", "Withdraw"}).Bands(); len(got) != 0 {
		t.Errorf("bands: %+v", got)
	}
}

func TestPresetEncodeRoundTrip(t *testing.T) {
	l := NewLines("Base")
	a := l.Add("cash", false, "", RGB(0x112233))
	b := l.Add("home", true, "Withdraw", RGB(0xfedcba))
	l.ToggleSolo(b)
	l.SetVisible(a, false)
	band := l.AddBand(b, a, RGB(0x010203))
	l.SetBandHatch(band, true)
	want := Capture("mine", l, 100.5, 200.25)

	body, err := EncodePreset(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePreset("mine", PresetFormat, body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, want)
	}

	if _, err := DecodePreset("mine", PresetFormat+1, body); !errors.Is(err, ErrUnknownPresetFormat) {
		t.Errorf("unknown format: %v", err)
	}
	if _, err := DecodePreset("mine", PresetFormat, []byte("{")); err == nil {
		t.Error("garbage body accepted")
	}
}

func TestPresetRestoreSwitcher(t *testing.T) {
	p := Preset{Switcher: "Gone"}
	for _, c := range []struct {
		projections []string
		want        string
	}{{[]string{"A", "Gone"}, "Gone"}, {[]string{"A", "B"}, "A"}, {nil, ""}} {
		if got := p.Restore(hasAll, c.projections).Switcher(); got != c.want {
			t.Errorf("projections %v: switcher %q, want %q", c.projections, got, c.want)
		}
	}
}

func TestPresetRestoreSkipsMissing(t *testing.T) {
	p := Preset{
		Switcher: "Base",
		Lines: []PresetLine{
			{Asset: "cash", Visible: true},
			{Asset: "gone", Visible: true},
			{Asset: "home", Pinned: true, Projection: "Missing", Visible: true},
			{Asset: "home", Pinned: true, Projection: "Withdraw", Visible: true},
		},
		Bands: []PresetBand{
			{0, 1, Color{}, false, false, true}, // uses a missing asset
			{2, 3, Color{}, false, false, true}, // uses a missing projection
			{3, 0, Color{}, true, false, true},  // survives, with new indexes
		},
	}
	has := func(a model.AssetID, proj string) bool {
		return a != "gone" && (proj == "" || proj == "Withdraw")
	}
	l := p.Restore(has, []string{"Base", "Withdraw"})
	all := l.All()
	if len(all) != 2 || all[0].Asset != "cash" || all[1].Projection != "Withdraw" {
		t.Fatalf("lines: %+v", all)
	}
	bands := l.Bands()
	if len(bands) != 1 || !bands[0].Hatch || bands[0].Upper != all[1].ID || bands[0].Lower != all[0].ID {
		t.Fatalf("bands: %+v", bands)
	}
}

func TestRenameProjection(t *testing.T) {
	l := NewLines("Old")
	l.Add("cash", false, "", RGB(1))
	l.Add("cash", true, "Old", RGB(1))
	l.Add("cash", true, "Other", RGB(1))
	p := Capture("mine", l, 1, 2)

	l.RenameProjection("Old", "New")
	got := l.All()
	if l.Switcher() != "New" || got[0].Projection != "" || got[1].Projection != "New" || got[2].Projection != "Other" {
		t.Errorf("lines after rename: %q %+v", l.Switcher(), got)
	}

	q, changed := p.RenameProjection("Old", "New")
	if !changed || q.Switcher != "New" || q.Lines[1].Projection != "New" || q.Lines[2].Projection != "Other" {
		t.Errorf("renamed preset: %v %+v", changed, q)
	}
	if p.Switcher != "Old" || p.Lines[1].Projection != "Old" {
		t.Error("renaming changed the original preset")
	}
	if _, changed := p.RenameProjection("Nope", "New"); changed {
		t.Error("a preset that does not use the name changed")
	}
}

func TestPresetRemoveAssets(t *testing.T) {
	line := func(a model.AssetID) PresetLine { return PresetLine{Asset: a, Visible: true} }
	p := Preset{
		Name:  "view",
		Lines: []PresetLine{line("a"), line("b"), line("c")},
		Bands: []PresetBand{{Upper: 0, Lower: 2}, {Upper: 1, Lower: 2}, {Upper: 0, Lower: 1}, {Upper: 2, Lower: 9}},
	}
	got, lines, bands := p.RemoveAssets("b")
	if lines != 1 || bands != 3 || len(got.Lines) != 2 || got.Lines[0].Asset != "a" || got.Lines[1].Asset != "c" {
		t.Fatalf("got %+v, %d lines, %d bands", got, lines, bands)
	}
	if len(got.Bands) != 1 || got.Bands[0].Upper != 0 || got.Bands[0].Lower != 1 {
		t.Errorf("bands %+v", got.Bands)
	}
	if len(p.Lines) != 3 || len(p.Bands) != 4 {
		t.Error("the preset was changed")
	}
	if same, lines, bands := p.RemoveAssets("z"); lines != 0 || bands != 0 || !reflect.DeepEqual(same, p) {
		t.Errorf("an absent asset changed the preset: %+v", same)
	}
	if none, _, _ := p.RemoveAssets("a", "b", "c"); len(none.Lines) != 0 || len(none.Bands) != 0 {
		t.Errorf("all lines removed: %+v", none)
	}
}

// TestDecodePresetWithoutMonteCarlo reads a body written before lines had the Monte Carlo flag.
func TestDecodePresetWithoutMonteCarlo(t *testing.T) {
	body := `{"switcher":"Base","lines":[{"asset":"cash","pinned":false,"projection":"","color":1122867,"visible":true,"soloed":false}],"bands":[],"from":1,"to":2}`
	p, err := DecodePreset("old", PresetFormat, []byte(body))
	if err != nil || len(p.Lines) != 1 || p.Lines[0].MonteCarlo || p.Lines[0].Asset != "cash" {
		t.Errorf("old body: %v %+v", err, p)
	}
}

func TestSetMonteCarlo(t *testing.T) {
	l := NewLines("Base")
	a := l.Add("cash", false, "", RGB(1))
	l.SetMonteCarlo(a, true)
	l.SetMonteCarlo(99, true)
	if ln, _ := l.Get(a); !ln.MonteCarlo {
		t.Error("Monte Carlo not set")
	}
	l.SetMonteCarlo(a, false)
	if ln, _ := l.Get(a); ln.MonteCarlo {
		t.Error("Monte Carlo not cleared")
	}
}
