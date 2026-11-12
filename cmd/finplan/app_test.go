// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

func TestOpenDemo(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.Load()
	if err != nil || !reflect.DeepEqual(rec, demodata.Family()) {
		t.Errorf("demo record: %v", err)
	}
	presets, problems, err := loadPresets(st)
	if err != nil || len(problems) != 0 || len(presets) != len(familyPresets(rec)) || len(presets) == 0 {
		t.Errorf("demo presets: %d, %v, %v", len(presets), problems, err)
	}
	cleanup()
	if left, _ := os.ReadDir(os.Getenv("TMPDIR")); len(left) != 0 {
		t.Errorf("demo files left behind: %v", left)
	}
}

func TestLoadPresetsSkipsUndecodable(t *testing.T) {
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := st.PutPreset("future", graph.PresetFormat+1, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := st.PutPreset("broken", graph.PresetFormat, []byte("{")); err != nil {
		t.Fatal(err)
	}
	presets, problems, err := loadPresets(st)
	if err != nil || len(problems) != 2 || len(presets) != len(familyPresets(demodata.Family())) {
		t.Errorf("presets %d, problems %v, err %v", len(presets), problems, err)
	}
}

func TestPresetsSurviveRestart(t *testing.T) {
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	a, err := newApp(st)
	if err != nil {
		t.Fatal(err)
	}
	a.graph.savePresetAs("mine")
	if len(a.notices) != 0 {
		t.Fatalf("notices: %v", a.notices)
	}
	b, err := newApp(st)
	if err != nil {
		t.Fatal(err)
	}
	if !b.graph.presetNameTaken("mine") {
		t.Error("saved preset not loaded after restart")
	}
}

func TestReloadKeepsView(t *testing.T) {
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	a, err := newApp(st)
	if err != nil {
		t.Fatal(err)
	}
	g := a.graph
	g.applyPreset(1)
	g.xMin, g.xMax = 1000, 2000
	name := g.presets[g.preset].Name
	lines := len(g.lines.All())

	// Another program removes a preset and adds an account.
	if err := st.DeletePreset(g.presets[0].Name); err != nil {
		t.Fatal(err)
	}
	rec := demodata.Family()
	acct := &model.Account{ID: "extra", Name: "Extra"}
	if err := rec.AddAccount(acct); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(rec); err != nil {
		t.Fatal(err)
	}
	a.reload()
	if len(a.notices) != 0 {
		t.Fatalf("notices: %v", a.notices)
	}
	if got := len(g.lines.All()); got != lines || g.xMin != 1000 || g.xMax != 2000 {
		t.Errorf("view changed: %d lines, x %v..%v", got, g.xMin, g.xMax)
	}
	if g.preset < 0 || g.presets[g.preset].Name != name {
		t.Errorf("preset selection lost: %d", g.preset)
	}
	if g.rec.Account("extra") == nil || g.cache.rec != g.rec {
		t.Error("record or cache not replaced")
	}
}

func TestReloadRemapsSelectionAndBands(t *testing.T) {
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	a, err := newApp(st)
	if err != nil {
		t.Fatal(err)
	}
	g := a.graph
	all := g.lines.All()
	g.lines.Select(all[2].ID)
	g.bandUpper, g.bandLower = all[1].ID, all[2].ID
	a.reload()
	now := g.lines.All()
	if len(now) != len(all) {
		t.Fatalf("%d lines, want %d", len(now), len(all))
	}
	if sel, ok := g.lines.Selected(); !ok || sel != now[2].ID {
		t.Errorf("selection %v %v, want %v", sel, ok, now[2].ID)
	}
	if g.bandUpper != now[1].ID || g.bandLower != now[2].ID {
		t.Errorf("band lines %v %v, want %v %v", g.bandUpper, g.bandLower, now[1].ID, now[2].ID)
	}
}

func TestReloadFromEmptyRecordUsesDefaults(t *testing.T) {
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	a, err := newApp(st)
	if err != nil {
		t.Fatal(err)
	}
	g := a.graph
	g.rec = &model.Record{}
	g.lines = graph.NewLines("")
	g.xMin, g.xMax = 0, 0
	a.reload()
	if len(g.lines.All()) == 0 || g.xMax <= g.xMin {
		t.Errorf("%d lines, x %v..%v", len(g.lines.All()), g.xMin, g.xMax)
	}
}

func TestReloadReportsProblemsOnce(t *testing.T) {
	st, cleanup, err := openDemo()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := st.PutPreset("broken", graph.PresetFormat, []byte("{")); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.notices) != 1 {
		t.Fatalf("notices: %v", a.notices)
	}
	a.reload()
	a.reload()
	if len(a.notices) != 1 {
		t.Errorf("notices after reloads: %v", a.notices)
	}
	if err := st.PutPreset("broken2", graph.PresetFormat, []byte("{")); err != nil {
		t.Fatal(err)
	}
	a.reload()
	if len(a.notices) != 2 {
		t.Errorf("a new problem was not reported: %v", a.notices)
	}
}

func TestRecordHas(t *testing.T) {
	has := recordHas(demodata.Family())
	if !has(demodata.NetWorth, "") || has("nonesuch", "") || has(demodata.NetWorth, "nonesuch") || !has(demodata.NetWorth, demodata.ProjBase) {
		t.Error("recordHas gave the wrong answer")
	}
}

func TestAboutTaglineRotates(t *testing.T) {
	if len(aboutTaglines) != 4 {
		t.Fatalf("%d taglines", len(aboutTaglines))
	}
	a := &app{}
	for n := 0; n < 2*len(aboutTaglines)+1; n++ {
		a.openAbout()
		if want := aboutTaglines[n%len(aboutTaglines)]; !a.about || a.aboutLine != want || aboutTagline(n) != want {
			t.Errorf("open %d shows %q, want %q", n, a.aboutLine, want)
		}
	}
}

func TestShortcutsBlocked(t *testing.T) {
	for _, tc := range []struct {
		notices          int
		textInput, popup bool
		want             bool
	}{
		{0, false, false, false},
		{1, false, false, true},
		{0, true, false, true},
		{0, false, true, true},
	} {
		if got := shortcutsBlocked(tc.notices, tc.textInput, tc.popup); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

func TestPresetModified(t *testing.T) {
	a := demoApp(t)
	g := a.graph
	g.applyPreset(0)
	if g.presetDirty() {
		t.Error("a just applied preset is modified")
	}
	// ImPlot reads the limits back as it set them, give or take rounding.
	g.xMin, g.xMax = g.xMin+0.001, g.xMax-0.001
	if g.presetDirty() {
		t.Error("a rounding read-back marks the preset modified")
	}
	g.xMin += 10 * secondsPerDay
	if !g.presetDirty() {
		t.Error("a panned view is unmodified")
	}
	g.applyPreset(0)

	ln := g.lines.All()[0]
	g.lines.SetVisible(ln.ID, !ln.Visible)
	if !g.presetDirty() {
		t.Error("a hidden line left the preset unmodified")
	}
	g.savePreset()
	if len(a.notices) != 0 {
		t.Fatalf("notices: %v", a.notices)
	}
	if g.presetDirty() {
		t.Error("a saved preset is modified")
	}

	g.preset = -1
	if g.presetDirty() {
		t.Error("no preset is modified")
	}
}

func TestFitYRange(t *testing.T) {
	d := func(day int, v float64) engine.Sample { return engine.Sample{Date: model.Date(day), Amount: v} }
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	big := []engine.Sample{d(0, 1000000), d(10, 2000000), d(20, 3000000)}
	small := []engine.Sample{d(0, 100), d(10, 200), d(20, 300)}

	lo, hi, ok := fitYRange([][]engine.Sample{big, small}, 0, 20)
	if !ok || !near(lo, 100-0.08*(3000000-100)) || !near(hi, 3000000+0.08*(3000000-100)) {
		t.Errorf("all in range: %v %v %v", lo, hi, ok)
	}
	// Only the small line is in range.
	lo, hi, ok = fitYRange([][]engine.Sample{{d(30, 5000000)}, small}, 0, 20)
	if !ok || !near(lo, 100-0.08*200) || !near(hi, 300+0.08*200) {
		t.Errorf("only small lines in range: %v %v %v", lo, hi, ok)
	}
	// A flat line gets a margin of one on each side.
	lo, hi, ok = fitYRange([][]engine.Sample{{d(5, 50), d(6, 50)}}, 0, 20)
	if !ok || !near(lo, 49-0.16) || !near(hi, 51+0.16) {
		t.Errorf("flat: %v %v %v", lo, hi, ok)
	}
	if _, _, ok := fitYRange([][]engine.Sample{big}, 100, 200); ok {
		t.Error("no sample in range fitted")
	}
}
