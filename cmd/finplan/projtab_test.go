// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
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
		want     float64
		ok       bool
		out      string
	}{
		{model.PropGrowth, "4", 0.04, true, "4%"},
		{model.PropInflation, " 2.5% ", 0.025, true, "2.5%"},
		{model.PropGrowth, "-1.25", -0.0125, true, "-1.25%"},
		{model.PropAmount, "1500", 1500, true, "1500"},
		{model.PropAmount, "12.5", 12.5, true, "12.5"},
		{model.PropGrowth, "", 0, false, ""},
		{model.PropAmount, "ten", 0, false, ""},
		{model.PropAmount, "NaN", 0, false, ""},
	} {
		got, err := parseValue(tc.prop, tc.in)
		if (err == nil) != tc.ok || (tc.ok && (got < tc.want-1e-12 || got > tc.want+1e-12)) {
			t.Errorf("parseValue(%s, %q) = %v, %v", tc.prop, tc.in, got, err)
			continue
		}
		if tc.ok {
			if s := formatValue(tc.prop, got); s != tc.out {
				t.Errorf("formatValue(%s, %v) = %q, want %q", tc.prop, got, s, tc.out)
			}
		}
	}
	if s := formatValue(model.PropGrowth, 0.07); s != "7%" {
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
	m, err := newMod(model.KindGlobal, "", model.PropInflation, "3", "")
	if err != nil || m.Target.Kind != model.KindGlobal || m.Property != model.PropInflation || m.Values.Segments()[0].Value != 0.03 {
		t.Errorf("global: %+v %v", m, err)
	}
	if _, err := newMod(model.KindAccount, "x", model.PropGrowth, "3", "never"); err == nil {
		t.Error("bad date accepted")
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
