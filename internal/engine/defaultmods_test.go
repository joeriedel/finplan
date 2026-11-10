// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

func defaultMod(kind model.ObjectKind, id string, mode model.Mode, from, to model.Date, v float64) model.Modification {
	return model.Modification{
		Target:   model.ObjectRef{Kind: kind, ID: id},
		Property: model.PropDefault,
		Mode:     mode,
		Values:   model.MustSchedule(model.Segment{From: from, End: &to, Value: v}),
	}
}

// The fixture's bare account has a default of 500 and no data, so before today it reads its default.
func TestDefaultModificationOfAnAccount(t *testing.T) {
	rec := fixture(t)
	bare := model.DefaultAssetID("bare")
	mar, may := d(2025, time.March, 1), d(2025, time.May, 31)
	must(t, rec.AddProjection(&model.Projection{Name: "set", Mods: []model.Modification{
		defaultMod(model.KindAccount, "bare", model.ModeSet, mar, may, 800),
	}}))
	must(t, rec.AddProjection(&model.Projection{Name: "scaled", Parent: "set", Mods: []model.Modification{
		defaultMod(model.KindAccount, "bare", model.ModeScale, d(2025, time.April, 1), d(2025, time.April, 30), 0.5),
		// Only the base default lies beneath this one.
		defaultMod(model.KindAccount, "bare", model.ModeScale, d(2025, time.July, 1), d(2025, time.July, 31), 2),
	}}))
	for _, tc := range []struct {
		proj string
		date model.Date
		want float64
	}{
		{"", d(2025, time.April, 15), 500},
		{"set", d(2025, time.February, 28), 500},
		{"set", d(2025, time.April, 15), 800},
		{"set", may, 800},
		{"set", d(2025, time.June, 1), 500},
		{"scaled", d(2025, time.March, 15), 800},
		{"scaled", d(2025, time.April, 15), 400},
		{"scaled", d(2025, time.July, 15), 1000},
	} {
		if got := sample(t, rec, tc.proj, bare, tc.date).Amount; !near(got, tc.want) {
			t.Errorf("%q on %v: got %v, want %v", tc.proj, tc.date, got, tc.want)
		}
	}
	if iv := rec.Account("bare").Default.Intervals; len(iv) != 1 || iv[0].Value != 500 {
		t.Errorf("the record's default changed: %+v", iv)
	}
}

func TestDefaultModificationNeverBeatsActualData(t *testing.T) {
	rec := fixture(t)
	day := d(2025, time.December, 1) // chk has a snapshot of 1000
	all := d(2025, time.January, 1)
	must(t, rec.AddProjection(&model.Projection{Name: "p", Mods: []model.Modification{
		defaultMod(model.KindAccount, "chk", model.ModeSet, all, d(2026, time.December, 31), 77),
		defaultMod(model.KindAsset, "pair", model.ModeSet, all, d(2026, time.December, 31), 55),
	}}))
	if got := sample(t, rec, "p", model.DefaultAssetID("chk"), day).Amount; !near(got, 1000) {
		t.Errorf("account with data: got %v, want 1000", got)
	}
	if got, want := sample(t, rec, "p", "pair", day).Amount, sample(t, rec, "", "pair", day).Amount; !near(got, want) {
		t.Errorf("asset with data: got %v, want %v", got, want)
	}
}

func TestDefaultModificationOfAnAsset(t *testing.T) {
	rec := fixture(t)
	must(t, rec.AddAsset(&model.Asset{ID: "spare", Members: []model.Member{model.AccountMember("bare")}}))
	day := d(2025, time.April, 1)
	must(t, rec.AddProjection(&model.Projection{Name: "p", Mods: []model.Modification{
		defaultMod(model.KindAsset, "spare", model.ModeSet, d(2025, time.March, 1), d(2025, time.May, 31), 900),
	}}))
	if got := sample(t, rec, "p", "spare", day).Amount; !near(got, 900) {
		t.Errorf("asset default: got %v, want 900", got)
	}
	if got := sample(t, rec, "p", "spare", d(2025, time.July, 1)).Amount; !near(got, 500) {
		t.Errorf("outside the range: got %v, want its member's 500", got)
	}
	if got := sample(t, rec, "", "spare", day).Amount; !near(got, 500) {
		t.Errorf("base: got %v, want 500", got)
	}
	// The default asset of an account shares the account's default.
	must(t, rec.AddProjection(&model.Projection{Name: "q", Mods: []model.Modification{
		defaultMod(model.KindAsset, string(model.DefaultAssetID("bare")), model.ModeSet, d(2025, time.March, 1), d(2025, time.May, 31), 700),
	}}))
	if got := sample(t, rec, "q", "spare", day).Amount; !near(got, 700) {
		t.Errorf("default asset target: got %v, want 700", got)
	}
}

func TestDefaultModificationOfAHypotheticalOpening(t *testing.T) {
	rec := villaRecord(t)
	villa := model.DefaultAssetID("villa")
	mar := d(2026, time.March, 1)
	must(t, rec.AddProjection(&model.Projection{Name: "bigger", Parent: "villa", Mods: []model.Modification{
		defaultMod(model.KindAccount, "villa", model.ModeSet, mar, mar, 5000),
	}}))
	must(t, rec.AddProjection(&model.Projection{Name: "halved", Parent: "bigger", Mods: []model.Modification{
		defaultMod(model.KindAccount, "villa", model.ModeScale, mar, mar, 0.5),
	}}))
	// The 1000 opening of villaRecord becomes the default's, plus the 100 funded on the opening day.
	for proj, want := range map[string]float64{"villa": 1100, "bigger": 5100, "halved": 2600} {
		if got := sample(t, rec, proj, villa, mar).Amount; !near(got, want) {
			t.Errorf("%q opening: got %v, want %v", proj, got, want)
		}
	}
}

func TestDefaultModificationReachesTheBreakdown(t *testing.T) {
	rec := fixture(t)
	bare := model.DefaultAssetID("bare")
	must(t, rec.AddProjection(&model.Projection{Name: "p", Mods: []model.Modification{
		defaultMod(model.KindAccount, "bare", model.ModeSet, d(2025, time.March, 1), d(2025, time.May, 31), 800),
	}}))
	bd, err := Breakdown(rec, "p", bare, d(2025, time.February, 1), d(2025, time.April, 1))
	must(t, err)
	if l := line(bd, CauseDefault); !near(l.Actual+l.Projected, 300) {
		t.Errorf("default change: got %+v, want 300", l)
	}
}

// A default modification on an account the projection defines gives nothing before the account opens, and the opening balance is the modification's value on the opening date.
func TestDefaultModificationOfProjectionAccountStartsAtOpening(t *testing.T) {
	villa := model.DefaultAssetID("villa")
	opens := d(2026, time.March, 1)
	for _, tc := range []struct {
		name string
		from model.Date
	}{
		{"always on", d(2000, time.January, 1)},
		{"starts before opening", d(2026, time.January, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := villaRecord(t)
			must(t, rec.AddProjection(&model.Projection{Name: "mod", Parent: "villa", Mods: []model.Modification{
				defaultMod(model.KindAccount, "villa", model.ModeSet, tc.from, d(2030, time.January, 1), 500000),
			}}))
			for _, date := range []model.Date{d(2026, time.February, 28), d(2026, time.January, 15), d(2025, time.June, 1)} {
				if got := sample(t, rec, "mod", villa, date).Amount; !near(got, 0) {
					t.Errorf("villa on %v: got %v, want 0", date, got)
				}
			}
			if got := sample(t, rec, "mod", villa, opens).Amount; !near(got, 500000+100) {
				t.Errorf("villa on %v: got %v, want 500100", opens, got)
			}
			bd, err := Breakdown(rec, "mod", villa, d(2026, time.February, 15), d(2026, time.April, 1))
			must(t, err)
			opening := 0.0
			for _, l := range bd.Lines {
				if l.Cause == CauseOpening {
					opening = l.Actual + l.Projected
				}
			}
			if !near(opening, 500000) {
				t.Errorf("opening line %v, want 500000", opening)
			}
		})
	}
}
