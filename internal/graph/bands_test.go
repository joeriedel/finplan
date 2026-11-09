// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

func bandFixture() (*Lines, LineID, LineID, LineID) {
	l := NewLines("Base")
	a := l.Add("cash", false, "", RGB(0x112233))
	b := l.Add("cash", true, "Other", RGB(0x112233))
	c := l.Add("home", false, "", RGB(0x445566))
	return l, a, b, c
}

func TestAddBandRules(t *testing.T) {
	l, a, b, _ := bandFixture()
	if l.AddBand(a, a, Color{}) != 0 || l.AddBand(a, 99, Color{}) != 0 || l.AddBand(99, a, Color{}) != 0 {
		t.Fatal("invalid band accepted")
	}
	id := l.AddBand(a, b, RGB(1))
	if id == 0 {
		t.Fatal("valid band rejected")
	}
	got, _ := l.GetBand(id)
	if want := (Band{ID: id, Upper: a, Lower: b, Color: RGB(1), Visible: true}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	l.RemoveBand(id)
	if id2 := l.AddBand(a, b, RGB(1)); id2 == id {
		t.Fatal("band id reused")
	}
}

func TestBandDrawn(t *testing.T) {
	l, a, b, c := bandFixture()
	id := l.AddBand(a, b, Color{})
	if !l.BandDrawn(id) {
		t.Fatal("want drawn")
	}
	l.SetVisible(b, false)
	if l.BandDrawn(id) {
		t.Fatal("hidden lower line should hide the band")
	}
	l.SetVisible(b, true)
	l.SetBandVisible(id, false)
	if l.BandDrawn(id) {
		t.Fatal("hidden band drawn")
	}
	l.SetBandVisible(id, true)
	l.ToggleSolo(a)
	if l.BandDrawn(id) {
		t.Fatal("solo of one edge should hide the band")
	}
	l.ToggleSolo(b)
	if !l.BandDrawn(id) {
		t.Fatal("both edges soloed should show the band")
	}
	l.ToggleSolo(c)
	l.SetVisible(a, false)
	if !l.BandDrawn(id) {
		t.Fatal("solo overrides line visibility")
	}
	if l.BandDrawn(99) {
		t.Fatal("unknown band drawn")
	}
}

func TestRemoveLineDropsBands(t *testing.T) {
	l, a, b, c := bandFixture()
	ab := l.AddBand(a, b, Color{})
	bc := l.AddBand(b, c, Color{})
	ac := l.AddBand(a, c, Color{})
	if got := l.BandsUsing(b); !reflect.DeepEqual(got, []BandID{ab, bc}) {
		t.Fatalf("BandsUsing: %v", got)
	}
	l.Remove(b)
	if got := l.Bands(); len(got) != 1 || got[0].ID != ac {
		t.Fatalf("bands after remove: %+v", got)
	}
}

func TestBandName(t *testing.T) {
	if got, want := BandName("401k", "Base", "401k", "Withdraw $100k"), "401k: Base − Withdraw $100k"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := BandName("Cash", "Base", "Home", "New job"), "Cash (Base) − Home (New job)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGapAndTodayDollars(t *testing.T) {
	if Gap(1500, 400) != 1100 || Gap(400, 1500) != -1100 {
		t.Error("gap")
	}
	infl := model.Constant(0.025)
	today := model.D(2026, time.November, 8)
	for _, tc := range []struct {
		name string
		v    float64
		d    model.Date
		want float64
	}{
		{"today", 1000, today, 1000},
		{"a year later", 1025, today.Add(365), 1025 / math.Pow(1.025, 365/model.DaysPerYear)},
		{"a year earlier", 1000, today.Add(-365), 1000 * math.Pow(1.025, 365/model.DaysPerYear)},
	} {
		got := ToTodayDollars(tc.v, tc.d, today, infl)
		if math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := ToTodayDollars(1000*math.Pow(1.025, 731/model.DaysPerYear), today.Add(731), today, infl); math.Abs(got-1000) > 0.5 {
		t.Errorf("two years later: got %v", got)
	}
}

func sameSeg(a, b Seg) bool {
	near := func(p, q Pt) bool { return math.Abs(float64(p.X-q.X)) < 1e-3 && math.Abs(float64(p.Y-q.Y)) < 1e-3 }
	return near(a.A, b.A) && near(a.B, b.B) || near(a.A, b.B) && near(a.B, b.A)
}

func TestHatchRectangle(t *testing.T) {
	top := []Pt{{0, 0}, {10, 0}}
	bottom := []Pt{{0, 10}, {10, 10}}
	got := HatchStripes(top, bottom, 5, Pt{-1e6, -1e6}, Pt{1e6, 1e6})
	want := []Seg{{Pt{0, 5}, Pt{5, 0}}, {Pt{0, 10}, Pt{10, 0}}, {Pt{5, 10}, Pt{10, 5}}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if !sameSeg(got[i], want[i]) {
			t.Errorf("stripe %d: got %v, want %v", i, got[i], want[i])
		}
	}
	if HatchStripes(top, bottom, 0, Pt{-1e6, -1e6}, Pt{1e6, 1e6}) != nil {
		t.Error("zero spacing")
	}
}

func TestHatchCrossing(t *testing.T) {
	a := []Pt{{0, 0}, {10, 10}}
	b := []Pt{{0, 10}, {10, 0}}
	got := HatchStripes(a, b, 2, Pt{-1e6, -1e6}, Pt{1e6, 1e6})
	if len(got) == 0 {
		t.Fatal("no stripes")
	}
	for _, s := range got {
		for _, p := range []Pt{s.A, s.B} {
			lo, hi := math.Min(float64(p.X), 10-float64(p.X)), math.Max(float64(p.X), 10-float64(p.X))
			if float64(p.Y) < lo-1e-3 || float64(p.Y) > hi+1e-3 {
				t.Errorf("stripe %v leaves the band at %v", s, p)
			}
		}
	}
	polys := BandPolys(a, b)
	if len(polys) != 2 {
		t.Fatalf("want 2 polygons split at the crossing, got %v", polys)
	}
	for _, poly := range polys {
		if len(poly) != 3 {
			t.Errorf("want triangles, got %v", poly)
		}
	}
}

func TestHatchBoundedByRect(t *testing.T) {
	var top, bottom []Pt
	for x := float32(-1000); x <= 2000; x += 10 {
		top = append(top, Pt{x, -1e6})
		bottom = append(bottom, Pt{x, 1e6})
	}
	lo, hi := Pt{0, 0}, Pt{100, 50}
	got := HatchStripes(top, bottom, 5, lo, hi)
	// 11 strips of 10 px overlap the rectangle, each crossed by at most (strip width + rectangle height) / spacing + 1
	// stripes; the unclipped band has 300 strips reaching +-1e6.
	const maxStripes = 11 * ((10+50)/5 + 1)
	if len(got) == 0 || len(got) > maxStripes {
		t.Fatalf("got %d stripes, want 1..%d, bounded by the rectangle", len(got), maxStripes)
	}
	for _, s := range got {
		for _, p := range []Pt{s.A, s.B} {
			if p.X < lo.X-1e-3 || p.X > hi.X+1e-3 || p.Y < lo.Y-1e-3 || p.Y > hi.Y+1e-3 {
				t.Errorf("stripe %v leaves the rectangle at %v", s, p)
			}
		}
	}
}
