// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"math"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
)

func TestLinesAddRemoveRecolour(t *testing.T) {
	l := NewLines("Base")
	a := l.Add("cash", false, "", ColorCash)
	b := l.Add("cash", false, "", ColorCash)
	c := l.Add("cash", true, "Other", ColorCash)
	la, _ := l.Get(a)
	lb, _ := l.Get(b)
	lc, _ := l.Get(c)
	if la.Color != ColorCash {
		t.Errorf("first line colour %v, want the base", la.Color)
	}
	if lb.Color == la.Color || lc.Color == lb.Color || lc.Color.R < lb.Color.R {
		t.Errorf("duplicates should get progressively lighter: %v %v %v", la.Color, lb.Color, lc.Color)
	}
	if !la.Visible {
		t.Error("new line hidden")
	}
	l.SetVisible(a, false)
	l.SetColor(a, RGB(0x123456))
	if la, _ = l.Get(a); la.Visible || la.Color != RGB(0x123456) {
		t.Errorf("after edits: %+v", la)
	}
	l.Remove(b)
	if _, ok := l.Get(b); ok || len(l.All()) != 2 {
		t.Error("remove failed")
	}
	if d := l.Add("x", false, "", ColorTotal); d == b {
		t.Error("ids reused")
	}
	l.Remove(999) // unknown id is ignored
	if la.Pinned || lc.Projection != "Other" || !lc.Pinned {
		t.Errorf("pin state: %+v %+v", la, lc)
	}
	// An unpinned line forgets a projection passed with it.
	if ln, _ := l.Get(l.Add("y", false, "Stray", ColorTotal)); ln.Projection != "" {
		t.Errorf("unpinned projection %q", ln.Projection)
	}
}

func TestSwitcherAndPins(t *testing.T) {
	l := NewLines("Base")
	follow := l.Add("a", false, "", ColorCash)
	fixed := l.Add("b", true, "Alt", ColorCash)
	eff := func(id LineID) string { p, _ := l.Effective(id); return p }
	if eff(follow) != "Base" || eff(fixed) != "Alt" {
		t.Fatalf("initial: %q %q", eff(follow), eff(fixed))
	}
	l.SetSwitcher("New job")
	if eff(follow) != "New job" || eff(fixed) != "Alt" {
		t.Errorf("after switch: %q %q", eff(follow), eff(fixed))
	}
	// Pinning captures the projection the switcher shows now and stays put afterwards.
	l.TogglePin(follow)
	l.SetSwitcher("Base")
	if eff(follow) != "New job" {
		t.Errorf("pinned line followed the switcher: %q", eff(follow))
	}
	// Unpinning returns the line to the switcher.
	l.TogglePin(follow)
	if ln, _ := l.Get(follow); ln.Pinned || eff(follow) != "Base" {
		t.Errorf("unpinned: %+v %q", ln, eff(follow))
	}
	if _, ok := l.Effective(77); ok {
		t.Error("effective of unknown line")
	}
	l.TogglePin(77)
}

func TestSelection(t *testing.T) {
	l := NewLines("")
	a, b := l.Add("a", false, "", ColorCash), l.Add("b", false, "", ColorCash)
	if _, ok := l.Selected(); ok {
		t.Error("selection at start")
	}
	l.Select(a)
	l.Select(b)
	if id, ok := l.Selected(); !ok || id != b {
		t.Errorf("selected %v %v", id, ok)
	}
	l.Remove(a)
	if _, ok := l.Selected(); !ok {
		t.Error("removing another line cleared the selection")
	}
	l.Remove(b)
	if _, ok := l.Selected(); ok {
		t.Error("selection survived removal")
	}
	l.Select(a)
	if _, ok := l.Selected(); ok {
		t.Error("selected a removed line")
	}
}

func TestLighten(t *testing.T) {
	c := RGB(0x102030)
	if c.Lighten(0) != c || c.Lighten(1) != (Color{255, 255, 255}) || c.Lighten(2) != (Color{255, 255, 255}) {
		t.Errorf("lighten bounds: %v %v", c.Lighten(0), c.Lighten(1))
	}
	if l := c.Lighten(0.5); l.R != 136 || l.G != 144 || l.B != 152 {
		t.Errorf("half: %v", l)
	}
	if r, g, b := (Color{255, 0, 51}).Floats(); r != 1 || g != 0 || math.Abs(float64(b)-0.2) > 1e-6 {
		t.Errorf("floats %v %v %v", r, g, b)
	}
}

func TestTypeColors(t *testing.T) {
	for typ, want := range map[model.AccountType]Color{
		model.TypeChecking: ColorCash, model.TypeSavings: ColorSavings, model.Type529: ColorSavings,
		model.TypeCreditCard: ColorDebt, model.TypeMortgage: ColorDebt, model.TypeLoan: ColorDebt,
		model.TypeBrokerage: ColorBrokerage, model.Type401k: ColorRetirement, model.TypeIRA: ColorRetirement,
		model.TypeRothIRA: ColorRetirement, model.TypeHSA: ColorRetirement, model.TypeTSP: ColorTSP,
		model.TypeProperty: ColorTotal, model.TypeEquity: ColorTotal,
	} {
		if got := TypeColor(typ); got != want {
			t.Errorf("%s: got %v, want %v", typ, got, want)
		}
	}
}

func TestAssetColor(t *testing.T) {
	rec := model.NewRecord(model.D(2026, time.January, 1))
	add := func(id string, typ model.AccountType) {
		if err := rec.AddAccount(&model.Account{ID: model.AccountID(id), Name: id, Type: typ}); err != nil {
			t.Fatal(err)
		}
	}
	add("chk", model.TypeChecking)
	add("sav", model.TypeSavings)
	add("k", model.Type401k)
	add("tsp", model.TypeTSP)
	add("card", model.TypeCreditCard)
	add("c1", model.Type529)
	add("c2", model.Type529)
	agg := func(id string, accts ...string) model.AssetID {
		a := &model.Asset{ID: model.AssetID(id)}
		for _, x := range accts {
			a.Members = append(a.Members, model.AccountMember(model.AccountID(x)))
		}
		if err := rec.AddAsset(a); err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	for _, c := range []struct {
		id   model.AssetID
		want Color
	}{
		{model.DefaultAssetID("tsp"), ColorTSP},
		{agg("cash", "chk", "sav"), ColorCash},
		{agg("ret", "k", "tsp"), ColorRetirement},
		{agg("college", "c1", "c2"), ColorSavings},
		{agg("net", "chk", "card", "k"), ColorTotal},
		{agg("empty"), ColorTotal},
		{"missing", ColorTotal},
	} {
		if got := AssetColor(rec, c.id); got != c.want {
			t.Errorf("%s: got %v, want %v", c.id, got, c.want)
		}
	}
}

func TestSampleGridCoarseness(t *testing.T) {
	from := model.D(2026, time.January, 1)
	// Zoomed in on a month the grid is daily; zoomed out over decades it is sparse.
	in := SampleGrid(from, from.Add(30), 1000)
	out := SampleGrid(from, from.Add(30*365), 1000)
	if in.Step != 1 {
		t.Errorf("zoomed in step %d, want 1", in.Step)
	}
	if out.Step < 30 {
		t.Errorf("zoomed out step %d, want sparse", out.Step)
	}
	if out.Count() >= 400 {
		t.Errorf("zoomed out grid has %d samples", out.Count())
	}
	// The view is covered with margin to either side.
	if in.First > from.Add(-10) || in.Last < from.Add(40) {
		t.Errorf("grid %v..%v does not pad the view", in.First, in.Last)
	}
	for _, g := range []Grid{in, out} {
		ts := g.Times()
		if len(ts) != g.Count() || ts[0] != g.First || ts[len(ts)-1] != g.Last {
			t.Errorf("times do not match the grid %+v", g)
		}
		for _, d := range ts {
			if int(d)%g.Step != 0 {
				t.Fatalf("date %v is off the step %d grid", d, g.Step)
			}
		}
	}
}

func TestSampleGridStableWhilePanning(t *testing.T) {
	from := model.D(2026, time.January, 1)
	base := SampleGrid(from, from.Add(365), 1200)
	same := 0
	for shift := 0; shift < 20; shift++ {
		if SampleGrid(from.Add(shift), from.Add(365+shift), 1200) == base {
			same++
		}
	}
	if same < 15 {
		t.Errorf("grid changed on %d of 20 one-day pans", 20-same)
	}
	// Whenever it does change, the dates still lie on the same global step multiples.
	for shift := 0; shift < 2000; shift += 37 {
		g := SampleGrid(from.Add(shift), from.Add(365+shift), 1200)
		if g.Step != base.Step || int(g.First)%g.Step != 0 {
			t.Fatalf("shift %d: %+v", shift, g)
		}
	}
}

func TestSampleGridBounded(t *testing.T) {
	from := model.D(1900, time.January, 1)
	for _, c := range []struct {
		days int
		w    float32
	}{{1, 1}, {10, 4000}, {365, 100}, {40000, 8000}, {2000000, 8000}, {5, 0}} {
		g := SampleGrid(from, from.Add(c.days), c.w)
		if n := g.Count(); n < 1 || n > MaxSamples {
			t.Errorf("days %d width %v: %d samples", c.days, c.w, n)
		}
		if g.First > from || g.Last < from.Add(c.days) {
			t.Errorf("days %d width %v: grid %v..%v misses the view", c.days, c.w, g.First, g.Last)
		}
	}
	// Reversed input is swapped.
	if a, b := SampleGrid(from, from.Add(90), 800), SampleGrid(from.Add(90), from, 800); a != b {
		t.Error("reversed range differs")
	}
}

func TestRangeButtons(t *testing.T) {
	today := model.D(2026, time.November, 8)
	dataStart := model.D(2024, time.October, 31)
	labels := ""
	for _, b := range RangeButtons {
		labels += b.Label + " "
	}
	if labels != "1Y 5Y 10Y 30Y All " {
		t.Errorf("labels %q", labels)
	}
	for _, c := range []struct {
		idx      int
		from, to model.Date
	}{
		{0, model.D(2026, time.October, 8), model.D(2027, time.November, 8)},
		{1, model.D(2026, time.June, 8), model.D(2031, time.November, 8)},
		{3, model.D(2024, time.May, 8), model.D(2056, time.November, 8)},
		{4, dataStart, model.D(2056, time.November, 8)},
	} {
		from, to := RangeButtons[c.idx].Span(today, dataStart)
		if from != c.from || to != c.to {
			t.Errorf("%s: %v..%v, want %v..%v", RangeButtons[c.idx].Label, from, to, c.from, c.to)
		}
	}
	// All with no data before today starts a month back.
	if from, _ := RangeButtons[4].Span(today, today); from != model.D(2026, time.October, 8) {
		t.Errorf("all without data: %v", from)
	}
}

func samples(statuses ...engine.Status) []engine.Sample {
	out := make([]engine.Sample, len(statuses))
	for i, s := range statuses {
		out[i] = engine.Sample{Date: model.Date(100 + 10*i), Amount: float64(i * 100), Status: s}
	}
	return out
}

func TestStatusSpans(t *testing.T) {
	const (
		d, a, p, pp = engine.StatusDefault, engine.StatusActual, engine.StatusProjected, engine.StatusPartlyProjected
	)
	got := StatusSpans(samples(d, a, a, pp, pp, p, p, p))
	want := []Span{{100, 110, d}, {110, 130, a}, {130, 150, pp}, {150, 170, p}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("span %d: got %v, want %v", i, got[i], want[i])
		}
	}
	if StatusSpans(nil) != nil {
		t.Error("spans of nothing")
	}
}

func TestHandoff(t *testing.T) {
	const a, p, pp = engine.StatusActual, engine.StatusProjected, engine.StatusPartlyProjected
	if h, ok := Handoff(samples(a, a, p, p)); !ok || h.Date != 120 {
		t.Errorf("actual to projected: %v %v", h, ok)
	}
	if h, ok := Handoff(samples(a, pp, p)); !ok || h.Date != 110 {
		t.Errorf("actual to partly projected: %v %v", h, ok)
	}
	for name, s := range map[string][]engine.Sample{
		"all actual": samples(a, a), "all projected": samples(p, p), "single": samples(a), "none": nil,
		"already projected at the start": samples(pp, p),
	} {
		if _, ok := Handoff(s); ok {
			t.Errorf("%s: handoff reported", name)
		}
	}
}

func TestStaleLabel(t *testing.T) {
	today := model.D(2026, time.November, 8)
	if l, ok := StaleLabel("TSP", model.D(2026, time.June, 30), today); !ok || l != "TSP · last manual entry Jun 30 2026" {
		t.Errorf("stale: %q %v", l, ok)
	}
	if _, ok := StaleLabel("TSP", today.Add(-StaleDays), today); ok {
		t.Error("labelled at the threshold")
	}
	if _, ok := StaleLabel("TSP", today.Add(-StaleDays-1), today); !ok {
		t.Error("not labelled past the threshold")
	}
	if _, ok := StaleLabel("TSP", today, today); ok {
		t.Error("labelled a current line")
	}
}

func TestLastActual(t *testing.T) {
	if _, ok := LastActual(nil); ok {
		t.Error("last actual of nothing")
	}
	got, ok := LastActual([]engine.AccountStart{{HasActual: true, LastActual: 50}, {HasActual: false, LastActual: 900}, {HasActual: true, LastActual: 70}})
	if !ok || got != 70 {
		t.Errorf("got %v %v", got, ok)
	}
}

func TestAt(t *testing.T) {
	s := []engine.Sample{
		{Date: 100, Amount: 1000, Status: engine.StatusActual},
		{Date: 110, Amount: 2000, Status: engine.StatusProjected},
		{Date: 130, Amount: 1000, Status: engine.StatusProjected},
	}
	for _, c := range []struct {
		d      model.Date
		v      float64
		status engine.Status
		ok     bool
	}{
		{100, 1000, engine.StatusActual, true},
		{105, 1500, engine.StatusActual, true}, // interpolated, status of the earlier sample
		{110, 2000, engine.StatusProjected, true},
		{120, 1500, engine.StatusProjected, true},
		{130, 1000, engine.StatusProjected, true},
		{99, 0, 0, false},
		{131, 0, 0, false},
	} {
		v, st, ok := At(s, c.d)
		if ok != c.ok || (ok && (math.Abs(v-c.v) > 1e-9 || st != c.status)) {
			t.Errorf("At(%d) = %v %v %v, want %v %v %v", c.d, v, st, ok, c.v, c.status, c.ok)
		}
	}
	if _, _, ok := At(nil, 5); ok {
		t.Error("lookup in no samples")
	}
	if v, _, ok := At(s[:1], 100); !ok || v != 1000 {
		t.Errorf("single sample: %v %v", v, ok)
	}
}

func TestDataStart(t *testing.T) {
	today := model.D(2026, time.March, 1)
	rec := model.NewRecord(today)
	if got := DataStart(rec); got != today {
		t.Errorf("empty record: %v", got)
	}
	_ = rec.AddAccount(&model.Account{ID: "a", Name: "a", Snapshots: []model.Snapshot{{Date: today.Add(-10)}}})
	_ = rec.AddAccount(&model.Account{ID: "b", Name: "b", Transactions: []model.Transaction{{Date: today.Add(-30)}}})
	if got := DataStart(rec); got != today.Add(-30) {
		t.Errorf("got %v", got)
	}
}

func TestFormatMoney(t *testing.T) {
	for v, want := range map[float64]string{
		0: "$0", 7: "$7", 999.4: "$999", 1000: "$1,000", 1234567.6: "$1,234,568", -1234: "-$1,234", -0.2: "$0", 1e9: "$1,000,000,000",
	} {
		if got := FormatMoney(v); got != want {
			t.Errorf("FormatMoney(%v) = %q, want %q", v, got, want)
		}
	}
	for v, want := range map[float64]string{
		0: "$0", 800: "$800", 1500: "$1.5K", 250000: "$250K", -40000: "-$40K", 1.25e6: "$1.25M", 3e6: "$3M", 2e9: "$2B", 1234: "$1.23K",
	} {
		if got := FormatMoneyCompact(v); got != want {
			t.Errorf("FormatMoneyCompact(%v) = %q, want %q", v, got, want)
		}
	}
	if got := FormatDate(model.D(2026, time.June, 5)); got != "Jun 5 2026" {
		t.Errorf("date %q", got)
	}
}

func TestNiceTicks(t *testing.T) {
	got := NiceTicks(0, 1000, 5)
	want := []float64{0, 200, 400, 600, 800, 1000}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tick %d: %v, want %v", i, got[i], want[i])
		}
	}
	if g := NiceTicks(-250, 480, 5); g[0] != -200 || g[len(g)-1] != 400 {
		t.Errorf("negative range: %v", g)
	}
	if NiceTicks(5, 5, 5) != nil || NiceTicks(0, 10, 0) != nil {
		t.Error("degenerate ranges should give no ticks")
	}
	if g := NiceTicks(0, 0.0005, 5); len(g) < 3 {
		t.Errorf("small range: %v", g)
	}
}

func TestDash(t *testing.T) {
	segs := Dash([]Pt{{0, 0}, {10, 0}}, 2, 1)
	if len(segs) != 4 || segs[0] != (Seg{Pt{0, 0}, Pt{2, 0}}) || segs[1].A.X != 3 || segs[3].B.X != 10 {
		t.Errorf("straight dashes: %v", segs)
	}
	// The pattern runs on around a corner: a 2-dash that ends 1 into the second leg.
	segs = Dash([]Pt{{0, 0}, {1, 0}, {1, 5}}, 2, 1)
	if len(segs) < 2 || segs[0].A != (Pt{0, 0}) || segs[1].A != (Pt{1, 0}) || segs[1].B != (Pt{1, 1}) {
		t.Errorf("dashes around a corner: %v", segs)
	}
	if Dash(nil, 2, 1) != nil || Dash([]Pt{{0, 0}, {1, 1}}, 0, 1) != nil {
		t.Error("degenerate dashes")
	}
	if len(Dash([]Pt{{0, 0}, {0, 0}}, 2, 1)) != 0 {
		t.Error("zero-length leg dashed")
	}
	// No gap gives a solid line cut into pieces.
	if segs = Dash([]Pt{{0, 0}, {4, 0}}, 2, 0); len(segs) != 2 {
		t.Errorf("zero gap: %v", segs)
	}
}

func TestDistance(t *testing.T) {
	poly := []Pt{{0, 0}, {10, 0}, {10, 10}}
	for _, c := range []struct {
		p    Pt
		want float32
	}{{Pt{5, 3}, 3}, {Pt{12, 5}, 2}, {Pt{-3, -4}, 5}, {Pt{10, 10}, 0}} {
		if got := Distance(c.p, poly); math.Abs(float64(got-c.want)) > 1e-5 {
			t.Errorf("Distance(%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if !math.IsInf(float64(Distance(Pt{}, nil)), 1) {
		t.Error("empty polyline should be infinitely far")
	}
	if got := Distance(Pt{3, 4}, []Pt{{0, 0}}); got != 5 {
		t.Errorf("single point: %v", got)
	}
	if got := Distance(Pt{3, 4}, []Pt{{0, 0}, {0, 0}}); got != 5 {
		t.Errorf("degenerate segment: %v", got)
	}
}
