// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestParseDate(t *testing.T) {
	d, err := parseDate("2027-03-09")
	if err != nil || d != model.D(2027, 3, 9) {
		t.Errorf("%v %v", d, err)
	}
	for _, bad := range []string{"", "2027-13-01", "3/9/2027", "soon"} {
		if _, err := parseDate(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseAndFormatValue(t *testing.T) {
	for _, tc := range []struct {
		prop, in string
		mode     model.Mode
		want     float64
		ok       bool
		out      string
	}{
		{model.PropGrowth, "4", "", 0.04, true, "4%"},
		{model.PropInflation, " 2.5% ", "", 0.025, true, "2.5%"},
		{model.PropGrowth, "-1.25", "", -0.0125, true, "-1.25%"},
		{model.PropAmount, "1500", "", 1500, true, "1500"},
		{model.PropAmount, "12.5", "", 12.5, true, "12.5"},
		{model.PropGrowth, "", "", 0, false, ""},
		{model.PropAmount, "ten", "", 0, false, ""},
		{model.PropAmount, "NaN", "", 0, false, ""},
		{model.PropAmount, "-30", model.ModeScale, 0.7, true, "-30%"},
		{model.PropGrowth, "10%", model.ModeScale, 1.1, true, "10%"},
		{model.PropAmount, "-100", model.ModeScale, 0, true, "-100%"},
		{model.PropAmount, "x", model.ModeScale, 0, false, ""},
	} {
		got, err := parseValue(tc.prop, tc.mode, tc.in)
		if (err == nil) != tc.ok || (tc.ok && (got < tc.want-1e-12 || got > tc.want+1e-12)) {
			t.Errorf("parseValue(%s, %q) = %v, %v", tc.prop, tc.in, got, err)
			continue
		}
		if tc.ok {
			if s := formatValue(tc.prop, tc.mode, got); s != tc.out {
				t.Errorf("formatValue(%s, %v) = %q, want %q", tc.prop, got, s, tc.out)
			}
		}
	}
	if s := formatValue(model.PropGrowth, model.ModeSet, 0.07); s != "7%" {
		t.Errorf("0.07 formats as %q", s)
	}
}

func TestConstantOfAndDescribe(t *testing.T) {
	d := model.D(2027, 1, 1)
	if v, from, ok := constantOf(model.Constant(0.5)); !ok || v != 0.5 || from != nil {
		t.Errorf("constant: %v %v %v", v, from, ok)
	}
	if v, from, ok := constantOf(scheduleFrom(2, &d)); !ok || v != 2 || from == nil || *from != d {
		t.Errorf("from: %v %v %v", v, from, ok)
	}
	multi := model.MustSchedule(model.Segment{From: d, Value: 1}, model.Segment{From: d.Add(10), Value: 2})
	ended := model.MustSchedule(model.Segment{From: d, End: model.Ptr(d.Add(5)), Value: 1})
	if _, _, ok := constantOf(multi); ok {
		t.Error("multi-segment schedule is editable")
	}
	if _, _, ok := constantOf(ended); ok {
		t.Error("ended segment is editable")
	}
	for s, want := range map[string]model.Schedule{
		"always":                    model.Constant(1),
		"from 2027-01-01":           scheduleFrom(1, &d),
		"2027-01-01 to 2027-01-06":  ended,
		"2 periods from 2027-01-01": multi,
		"never":                     {},
	} {
		if got := describeWhen(want); got != s {
			t.Errorf("describeWhen = %q, want %q", got, s)
		}
	}
}

func TestNewMod(t *testing.T) {
	m, err := newMod(model.KindGlobal, "", model.PropInflation, "", "3", "", "")
	if err != nil || m.Target.Kind != model.KindGlobal || m.Property != model.PropInflation || m.Values.Segments()[0].Value != 0.03 {
		t.Errorf("global: %+v %v", m, err)
	}
	if _, err := newMod(model.KindAccount, "x", model.PropGrowth, "", "3", "never", ""); err == nil {
		t.Error("bad date accepted")
	}
}

func TestNewScaleMod(t *testing.T) {
	m, err := newMod(model.KindBudgetItem, "dining", model.PropAmount, model.ModeScale, "-30", "2026-01-01", "")
	if err != nil || m.Mode != model.ModeScale || m.Property != model.PropAmount {
		t.Fatalf("scale: %+v %v", m, err)
	}
	if segs := m.Values.Segments(); len(segs) != 1 || segs[0].Value != 0.7 || segs[0].From != model.D(2026, 1, 1) {
		t.Errorf("values: %+v", segs)
	}
}

func TestProjectionShownAndDescendants(t *testing.T) {
	rec := demodata.Family()
	if !projectionShown(rec, demodata.ProjBase, "") || !projectionShown(rec, demodata.ProjBase, "withdraw") {
		t.Error("a parent of a match is hidden")
	}
	if projectionShown(rec, demodata.ProjNewJob, "withdraw") {
		t.Error("a non-match is shown")
	}
	if !projectionShown(rec, demodata.ProjWithdraw, "WITHDRAW") {
		t.Error("search is case sensitive")
	}
	below := descendants(rec, demodata.ProjBase)
	if !below[demodata.ProjNewJob] || !below[demodata.ProjWithdraw] || below[demodata.ProjBase] {
		t.Errorf("descendants %v", below)
	}
}

func TestObjectsFor(t *testing.T) {
	rec := demodata.Family()
	for _, c := range objectsFor(rec, model.KindAsset, demodata.ProjBase, 0) {
		if strings.HasPrefix(c.id, string(model.DefaultAssetID(""))) {
			t.Errorf("default asset %q listed", c.id)
		}
	}
	objs := func(k model.ObjectKind) int { return len(objectsFor(rec, k, demodata.ProjBase, 0)) }
	if objs(model.KindAsset) == 0 || objs(model.KindAccount) != len(rec.Accounts) || objs(model.KindGlobal) != 0 || objs(model.KindBudgetItem) != len(rec.Budget.Items) {
		t.Error("wrong object lists")
	}
}

func TestFieldErrorsArePerField(t *testing.T) {
	pt := &projTab{}
	pt.reset()
	pt.bufs["start"], pt.bufs["inflation"] = "soon", "lots"
	pt.errs["start"], pt.errs["inflation"] = "bad date", "bad value"

	pt.dropBuf("inflation")
	if pt.errs["start"] != "bad date" || pt.bufs["start"] != "soon" {
		t.Errorf("restoring one field disturbed another: %q %q", pt.errs["start"], pt.bufs["start"])
	}
	if _, ok := pt.errs["inflation"]; ok {
		t.Error("restored field kept its error")
	}
	if _, ok := pt.bufs["inflation"]; ok {
		t.Error("restored field kept its text")
	}

	pt.dropBuf("start")
	if len(pt.errs) != 0 || len(pt.bufs) != 0 {
		t.Errorf("dropping the buffer left %v %v", pt.errs, pt.bufs)
	}
}

func TestModeValuesConvertOnAFlip(t *testing.T) {
	rec := demodata.Family()
	item := rec.Budget.Items[0]
	end := model.D(2030, 1, 1)
	for _, tc := range []struct {
		mod  model.Modification
		mode model.Mode
		want float64
	}{
		{model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: item.ID}, Property: model.PropAmount, Values: model.Constant(900)}, model.ModeScale, 1},
		{model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: item.ID}, Property: model.PropAmount, Mode: model.ModeScale, Values: model.Constant(0.5)}, model.ModeSet, item.Amount * 0.5},
		{model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: item.ID}, Property: model.PropGrowth, Mode: model.ModeScale, Values: model.Constant(2)}, model.ModeSet, item.Growth * 2},
		{model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(rec.Accounts[0].ID)}, Property: model.PropGrowth, Mode: model.ModeScale, Values: model.Constant(2)}, model.ModeSet, rec.Accounts[0].StatedRate * 2},
	} {
		r := rec.Clone()
		if err := r.AddProjection(&model.Projection{Name: "p", Mods: []model.Modification{tc.mod}}); err != nil {
			t.Fatal(err)
		}
		got := modeValues(r, "p", 0, tc.mode)
		if got == nil || len(got.Segments()) != 1 || got.Segments()[0].Value != tc.want {
			t.Errorf("%+v to %s: %+v, want %v", tc.mod, tc.mode, got, tc.want)
		}
		if same := modeValues(r, "p", 0, cmp.Or(tc.mod.Mode, model.ModeSet)); same != nil {
			t.Errorf("an unchanged mode converted: %+v", same)
		}
	}

	// Segment starts and ends survive.
	m := model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Values: model.MustSchedule(model.Segment{From: model.D(2028, 1, 1), End: &end, Value: 0.05})}
	r := rec.Clone()
	if err := r.AddProjection(&model.Projection{Name: "p", Mods: []model.Modification{m}}); err != nil {
		t.Fatal(err)
	}
	segs := modeValues(r, "p", 0, model.ModeScale).Segments()
	if len(segs) != 1 || segs[0].From != model.D(2028, 1, 1) || segs[0].End == nil || *segs[0].End != end || segs[0].Value != 1 {
		t.Errorf("segments %+v", segs)
	}
}

func TestDefaultBalanceProperty(t *testing.T) {
	for _, k := range []model.ObjectKind{model.KindAccount, model.KindAsset} {
		if !slices.Contains(propertiesFor(k), model.PropDefault) {
			t.Errorf("%s: no default balance property", k)
		}
	}
	if slices.Contains(propertiesFor(model.KindBudgetItem), model.PropDefault) || slices.Contains(propertiesFor(model.KindGlobal), model.PropDefault) {
		t.Error("default balance offered for an item or the global")
	}
	v, err := parseValue(model.PropDefault, model.ModeSet, "-2500.5")
	if err != nil || v != -2500.5 {
		t.Errorf("parse set = %v %v, want money as typed", v, err)
	}
	if got := formatValue(model.PropDefault, model.ModeSet, 25000); got != "25000" {
		t.Errorf("format set = %q", got)
	}
	if v, _ := parseValue(model.PropDefault, model.ModeScale, "-20"); v != 0.8 {
		t.Errorf("parse scale = %v, want 0.8", v)
	}
	if got := formatValue(model.PropDefault, model.ModeScale, 0.8); got != "-20%" {
		t.Errorf("format scale = %q", got)
	}
	m, err := newMod(model.KindAccount, "x", model.PropDefault, "", "9000", "2025-01-01", "2025-06-30")
	if err != nil {
		t.Fatal(err)
	}
	if segs := m.Values.Segments(); len(segs) != 1 || segs[0].Value != 9000 || segs[0].End == nil || *segs[0].End != model.D(2025, 6, 30) {
		t.Errorf("ranged default: %+v", segs)
	}
	if _, err := newMod(model.KindAccount, "x", model.PropDefault, "", "9000", "", "2025-06-30"); err == nil {
		t.Error("an end without a start accepted")
	}
	if _, err := newMod(model.KindAccount, "x", model.PropDefault, "", "9000", "2025-07-01", "2025-06-30"); err == nil {
		t.Error("an end before the start accepted")
	}
}
