// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"reflect"
	"testing"
)

func TestPresetRoundTrip(t *testing.T) {
	l := NewLines("Base")
	a := l.Add("cash", false, "", RGB(0x112233))
	b := l.Add("cash", true, "Withdraw", RGB(0x112233)) // lightened by Add as a duplicate
	c := l.Add("home", false, "", RGB(0x445566))
	l.SetColor(c, RGB(0xabcdef))
	l.SetVisible(c, false)
	l.ToggleSolo(a)
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
	r := p.Restore()
	if _, ok := r.Selected(); ok {
		t.Error("selection restored")
	}
	if got := Capture("mine", r, 100, 200); !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, p)
	}
	if r.Switcher() != "Base" || r.SoloCount() != 1 {
		t.Error("switcher or solo lost")
	}
	// The restored list is independent of the original.
	l.Remove(a)
	if len(r.All()) != 3 || len(r.Bands()) != 2 {
		t.Error("restored list shares state")
	}
}

func TestPresetRestoreSkipsBadBands(t *testing.T) {
	p := Preset{Switcher: "Base", Lines: []PresetLine{{Asset: "cash", Visible: true}}, Bands: []PresetBand{{0, 0, Color{}, false, false, true}, {0, 5, Color{}, false, false, true}}}
	if got := p.Restore().Bands(); len(got) != 0 {
		t.Errorf("bands: %+v", got)
	}
}
