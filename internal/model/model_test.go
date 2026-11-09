// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func day(m time.Month, d int) Date { return D(2026, m, d) }

func newRecord(t *testing.T, accts ...*Account) *Record {
	t.Helper()
	r := NewRecord(day(time.January, 1))
	for _, a := range accts {
		if err := r.AddAccount(a); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func addAsset(t *testing.T, r *Record, id AssetID, members ...Member) {
	t.Helper()
	if err := r.AddAsset(&Asset{ID: id, Members: members}); err != nil {
		t.Fatal(err)
	}
}

func acct(id AccountID, typ AccountType, snapshotOn Date, balance float64) *Account {
	a := &Account{ID: id, Type: typ, Tax: Taxable}
	if snapshotOn != 0 {
		a.AddSnapshot(snapshotOn, balance)
	}
	return a
}

func balance(t *testing.T, r *Record, id AssetID, d Date) Balance {
	t.Helper()
	b, err := r.Balance(id, d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLaterOverlappingMemberIsInvalid(t *testing.T) {
	d := day(time.March, 1)
	r := newRecord(t, acct("chk", TypeChecking, d, 100), acct("sav", TypeSavings, d, 50), acct("brk", TypeBrokerage, d, 1000))
	cash := RuleMember(Rule{Types: []AccountType{TypeChecking, TypeSavings}})
	addAsset(t, r, "cash", cash)

	// Checking is added after "cash" already includes it.
	addAsset(t, r, "a", AssetMember("cash"), AccountMember("chk"), AccountMember("brk"))
	res, err := r.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Overlaps) != 1 {
		t.Fatalf("overlaps: got %v, want 1", res.Overlaps)
	}
	o := res.Overlaps[0]
	if o.Asset != "a" || !slices.Equal(o.Duplicates, []AccountID{"chk"}) || o.Seq <= o.Against {
		t.Errorf("overlap: got %+v", o)
	}
	if _, ok := res.Invalid("a", o.Seq); !ok {
		t.Error("Invalid does not find the overlapping member")
	}
	if got := balance(t, r, "a", d).Amount; got != 1150 {
		t.Errorf("balance: got %v, want 1150 (checking counted once)", got)
	}

	// Added the other way around, the rule member is the later one and is the invalid one.
	addAsset(t, r, "b", AccountMember("chk"), AssetMember("cash"))
	res, _ = r.Resolve("b")
	if len(res.Overlaps) != 1 || res.Overlaps[0].Member != AssetMember("cash").String() {
		t.Fatalf("overlaps: got %v", res.Overlaps)
	}
	if got := balance(t, r, "b", d).Amount; got != 100 {
		t.Errorf("balance: got %v, want 100", got)
	}
}

func TestOverlapIsCheckedWhenUsed(t *testing.T) {
	d := day(time.March, 1)
	r := newRecord(t, acct("chk", TypeChecking, d, 100), acct("brk", TypeBrokerage, d, 1000))
	addAsset(t, r, "invest", AccountMember("brk"))
	addAsset(t, r, "top", AccountMember("chk"), AssetMember("invest"))
	if res, _ := r.Resolve("top"); len(res.Overlaps) != 0 {
		t.Fatalf("unexpected overlap: %v", res.Overlaps)
	}
	if got := balance(t, r, "top", d).Amount; got != 1100 {
		t.Fatalf("balance: got %v, want 1100", got)
	}
	// Editing the wrapped asset, not "top", creates the overlap.
	if err := r.AddMember("invest", AccountMember("chk")); err != nil {
		t.Fatal(err)
	}
	res, _ := r.Resolve("top")
	if len(res.Overlaps) != 1 || res.Overlaps[0].Asset != "top" {
		t.Fatalf("overlaps: got %v", res.Overlaps)
	}
	if got := balance(t, r, "top", d).Amount; got != 100 {
		t.Errorf("balance: got %v, want 100 (the later member is left out)", got)
	}
}

func TestDefaultVersusActual(t *testing.T) {
	a := acct("a", TypeChecking, 0, 0)
	a.Default.Intervals = []Interval{{From: MinDate, To: day(time.January, 9), Value: 40000}}
	a.AddSnapshot(day(time.January, 1), 100)
	a.AddSnapshot(day(time.January, 10), 200)
	if err := a.AddTransaction(Transaction{Date: day(time.January, 5), Amount: 50, Kind: TxIncome}); err != nil {
		t.Fatal(err)
	}
	r := newRecord(t, a)
	for _, c := range []struct {
		date   Date
		amount float64
		actual bool
	}{
		{D(2025, time.December, 31), 40000, false}, // before the span: the interval applies
		{day(time.January, 1), 100, true},          // actual beats the 40000 default
		{day(time.January, 7), 150, true},          // gap filled from transactions, not defaults
		{day(time.January, 10), 200, true},         // snapshot
		{day(time.January, 12), 0, false},          // after the span: the base default
	} {
		got := balance(t, r, DefaultAssetID("a"), c.date)
		if got.Amount != c.amount || got.Actual != c.actual {
			t.Errorf("%v: got %+v, want %v actual=%v", c.date, got, c.amount, c.actual)
		}
	}
	// An interval that does cover a date with no actual data supplies the value.
	a.Default.Intervals = []Interval{{From: MinDate, To: day(time.December, 31), Value: 40000}}
	if got := balance(t, r, DefaultAssetID("a"), day(time.December, 20)); got.Amount != 40000 || got.Actual {
		t.Errorf("default interval: got %+v", got)
	}
}

func TestNestedDefaultResolution(t *testing.T) {
	known, other := day(time.March, 1), day(time.March, 1).Add(100)
	a := acct("a", TypeChecking, 0, 0)
	a.Default.Intervals = []Interval{{From: MinDate, To: MaxDate, Value: 10}}
	b := acct("b", TypeSavings, known, 5) // actual only on one date
	b.Default.Intervals = []Interval{{From: MinDate, To: MaxDate, Value: 20}}
	c := acct("c", TypeSavings, 0, 0)
	c.Default.Intervals = []Interval{{From: MinDate, To: MaxDate, Value: 7}}
	r := newRecord(t, a, b, c)
	addAsset(t, r, "x", AccountMember("a"), AccountMember("b"))
	addAsset(t, r, "y", AssetMember("x"), AccountMember("c"))
	r.Asset("y").Default.Intervals = []Interval{{From: MinDate, To: MaxDate, Value: 999}}

	// Some account has actual data: the asset is the sum of its members, each resolved on its own.
	// x has actual data (b), so it is a(default 10) + b(actual 5), and c is its default 7.
	if got := balance(t, r, "y", known); got.Amount != 22 || !got.Actual {
		t.Errorf("with actual data: got %+v, want 22 actual", got)
	}
	// No actual data anywhere: y's own default interval wins over its members' defaults.
	if got := balance(t, r, "y", other); got.Amount != 999 || got.Actual {
		t.Errorf("y default: got %+v, want 999 default", got)
	}
	// Without y's interval, x's interval beats a's and b's defaults, plus c's default.
	r.Asset("y").Default.Intervals = nil
	r.Asset("x").Default.Intervals = []Interval{{From: MinDate, To: MaxDate, Value: 100}}
	if got := balance(t, r, "y", other); got.Amount != 107 {
		t.Errorf("x default: got %+v, want 107", got)
	}
	// With no interval at any level, the members' defaults add up (the 0 base never beats them).
	r.Asset("x").Default.Intervals = nil
	if got := balance(t, r, "y", other); got.Amount != 10+20+7 {
		t.Errorf("members' defaults: got %+v, want 37", got)
	}
}

func TestCoverageUnion(t *testing.T) {
	mk := func(id AccountID, from, to Date) *Account {
		a := acct(id, TypeSavings, from, 1)
		a.AddSnapshot(to, 2)
		return a
	}
	r := newRecord(t,
		mk("a", day(time.January, 1), day(time.February, 1)),
		mk("b", day(time.June, 1), day(time.July, 1)),
		mk("c", day(time.January, 15), day(time.March, 1)),
		acct("none", TypeSavings, 0, 0),
	)
	addAsset(t, r, "all", AccountMember("a"), AccountMember("b"), AccountMember("c"), AccountMember("none"))
	res, err := r.Resolve("all")
	if err != nil {
		t.Fatal(err)
	}
	want := []Span{{day(time.January, 1), day(time.March, 1)}, {day(time.June, 1), day(time.July, 1)}}
	if got := res.Coverage(); !slices.Equal(got, want) {
		t.Errorf("coverage: got %v, want %v", got, want)
	}
	if last, ok := res.LastActual(); !ok || last != day(time.July, 1) {
		t.Errorf("last actual: got %v %v", last, ok)
	}
	addAsset(t, r, "empty", AccountMember("none"))
	res, _ = r.Resolve("empty")
	if _, ok := res.LastActual(); ok {
		t.Error("an asset with no data has no last actual date")
	}
}

func TestCycleRejected(t *testing.T) {
	r := newRecord(t, acct("a", TypeChecking, 0, 0))
	addAsset(t, r, "x", AccountMember("a"))
	addAsset(t, r, "y", AssetMember("x"))
	err := r.AddMember("x", AssetMember("y"))
	if !errors.Is(err, ErrCycle) {
		t.Fatalf("got %v, want a cycle error", err)
	}
	var ce *CycleError
	if !errors.As(err, &ce) || len(ce.Path) != 3 {
		t.Errorf("cycle path: got %v", ce)
	}
	if err := r.AddMember("x", AssetMember("x")); !errors.Is(err, ErrCycle) {
		t.Errorf("self member: got %v", err)
	}
	if len(r.Asset("x").Members) != 1 {
		t.Error("a rejected member was added")
	}
	// A cycle already in the data is reported on resolution.
	r.Asset("x").Members = append(r.Asset("x").Members, Member{Seq: 99, Asset: "y"})
	if _, err := r.Resolve("y"); !errors.Is(err, ErrCycle) {
		t.Errorf("resolve: got %v, want a cycle error", err)
	}
}

func TestPrecedence(t *testing.T) {
	r := newRecord(t, acct("a", TypeChecking, 0, 0), acct("b", TypeSavings, 0, 0))
	addAsset(t, r, "inner", AccountMember("a"))
	addAsset(t, r, "outer", AssetMember("inner"), AccountMember("b"))
	res, err := r.Resolve("outer")
	if err != nil {
		t.Fatal(err)
	}
	set := func(asset AssetID, v float64) Setting[float64] { return Setting[float64]{Asset: asset, Value: v} }
	over := func(asset AssetID, v float64) Setting[float64] {
		return Setting[float64]{Asset: asset, Value: v, Mode: ModeOverride}
	}

	// Among defaults the most specific wins, whatever order the settings are listed in.
	got := Precedence(res, "a", "growth", []Setting[float64]{set("inner", 1), set("outer", 2), set(DefaultAssetID("a"), 3)})
	if !got.Found || got.Value != 3 || got.From != DefaultAssetID("a") || got.Conflict != nil {
		t.Errorf("most specific default wins: got %+v", got)
	}
	got = Precedence(res, "a", "growth", []Setting[float64]{set("outer", 2), set("inner", 1)})
	if got.Value != 1 || got.From != "inner" {
		t.Errorf("inner default beats outer default: got %+v", got)
	}
	// An override beats defaults nested inside it; the outermost override beats inner overrides too.
	got = Precedence(res, "a", "growth", []Setting[float64]{over("outer", 2), set("inner", 1), set(DefaultAssetID("a"), 3)})
	if got.Value != 2 || got.From != "outer" {
		t.Errorf("override beats nested defaults: got %+v", got)
	}
	got = Precedence(res, "a", "growth", []Setting[float64]{over("inner", 1), over("outer", 2), over(DefaultAssetID("a"), 3)})
	if got.Value != 2 || got.From != "outer" || got.Conflict != nil {
		t.Errorf("outermost override wins: got %+v", got)
	}
	// An inner override beats an outer default.
	got = Precedence(res, "a", "growth", []Setting[float64]{set("outer", 2), over("inner", 1)})
	if got.Value != 1 || got.From != "inner" {
		t.Errorf("inner override beats outer default: got %+v", got)
	}
	// An earlier layer (the projection) beats every later layer, even one marked override.
	got = Precedence(res, "a", "growth", []Setting[float64]{set("inner", 9)}, []Setting[float64]{over("outer", 2)})
	if got.Value != 9 || got.From != "inner" {
		t.Errorf("layer order: got %+v", got)
	}
	// The account's default asset is innermost; it applies only when nothing wraps it with a setting.
	got = Precedence(res, "a", "growth", []Setting[float64]{set(DefaultAssetID("a"), 3)})
	if got.Value != 3 {
		t.Errorf("default asset: got %+v", got)
	}
	// Assets that do not wrap the account, or lie outside the resolved scope, do not count.
	got = Precedence(res, "b", "growth", []Setting[float64]{set("inner", 1), set("elsewhere", 5)})
	if got.Found {
		t.Errorf("not wrapping: got %+v", got)
	}
}

func TestSiblingConflictIsReported(t *testing.T) {
	r := newRecord(t, acct("shared", TypeChecking, 0, 0), acct("solo", TypeSavings, 0, 0))
	addAsset(t, r, "left", AccountMember("shared"), AccountMember("solo"))
	// "right" overlaps "left" on the shared account: it is marked invalid, yet its setting is seen.
	addAsset(t, r, "right", AccountMember("shared"))
	addAsset(t, r, "top", AssetMember("left"), AssetMember("right"))
	res, err := r.Resolve("top")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Overlaps) != 1 {
		t.Fatalf("overlaps: got %v", res.Overlaps)
	}
	layer := []Setting[Schedule]{{Asset: "left", Value: Constant(0.04)}, {Asset: "right", Value: Constant(0.05)}}
	got := Precedence(res, "shared", "growth", layer)
	if got.Conflict == nil {
		t.Fatal("no conflict reported")
	}
	if got.Conflict.Account != "shared" || got.Conflict.Name != "growth" || got.Conflict.Chosen != "left" ||
		!slices.Equal(got.Conflict.Assets, []AssetID{"left", "right"}) {
		t.Errorf("conflict: got %+v", got.Conflict)
	}
	if got.Value.Segments[0].Value != 0.04 {
		t.Errorf("value while in conflict: got %+v, want the earlier-added sibling's", got.Value)
	}
	// The account only one sibling covers is not in conflict, and equal values are not either.
	if got := Precedence(res, "solo", "growth", layer); got.Conflict != nil || got.From != "left" {
		t.Errorf("solo: got %+v", got)
	}
	same := []Setting[Schedule]{{Asset: "left", Value: Constant(0.04)}, {Asset: "right", Value: Constant(0.04)}}
	if got := Precedence(res, "shared", "growth", same); got.Conflict != nil {
		t.Errorf("equal values: got conflict %+v", got.Conflict)
	}
	// An override at an outer level settles it; a default there does not, as the siblings are more specific.
	settled := append(layer, Setting[Schedule]{Asset: "top", Value: Constant(0.07), Mode: ModeOverride})
	if got := Precedence(res, "shared", "growth", settled); got.Conflict != nil || got.From != "top" {
		t.Errorf("settled: got %+v", got)
	}
	unsettled := append(layer[:2:2], Setting[Schedule]{Asset: "top", Value: Constant(0.07)})
	if got := Precedence(res, "shared", "growth", unsettled); got.Conflict == nil || got.From != "left" {
		t.Errorf("outer default: got %+v", got)
	}
}

func TestSchedule(t *testing.T) {
	s := NewSchedule(Segment{day(time.January, 1), 0.06}, Segment{MinDate, 0.04}, Segment{day(time.June, 1), 0.02})
	for _, c := range []struct {
		d    Date
		want float64
	}{{day(time.January, 1).Add(-1), 0.04}, {day(time.January, 1), 0.06}, {day(time.May, 31), 0.06}, {day(time.June, 1), 0.02}} {
		if got, ok := s.At(c.d); !ok || got != c.want {
			t.Errorf("At(%v): got %v %v, want %v", c.d, got, ok, c.want)
		}
	}
	if _, ok := (Schedule{}).At(day(time.January, 1)); ok {
		t.Error("empty schedule has a value")
	}
	if got, ok := Constant(3).At(MinDate); !ok || got != 3 {
		t.Errorf("constant: got %v %v", got, ok)
	}
	// Compounding over a year at 0.04 and across a boundary splits the elapsed time.
	c := Constant(0.10)
	if got := c.Compound(day(time.January, 1), day(time.January, 1)); got != 1 {
		t.Errorf("no time: got %v", got)
	}
	split := NewSchedule(Segment{MinDate, 0.10}, Segment{day(time.July, 1), 0.10})
	a, b := split.Compound(day(time.January, 1), day(time.December, 1)), c.Compound(day(time.January, 1), day(time.December, 1))
	if d := a - b; d > 1e-12 || d < -1e-12 {
		t.Errorf("split vs whole: %v vs %v", a, b)
	}
}

func TestRecurrence(t *testing.T) {
	var (
		jan1  = day(time.January, 1)
		year  = D(2027, time.January, 1)
		count = func(r Recurrence) int { return len(r.Between(jan1.Add(-1), year.Add(-1))) }
	)
	if got := count(Recurrence{Kind: Biweekly, Anchor: jan1.Add(-30)}); got != 26 {
		t.Errorf("biweekly: got %d, want 26", got)
	}
	if got := count(Recurrence{Kind: Monthly, Day: 31}); got != 12 {
		t.Errorf("monthly: got %d", got)
	}
	if got := count(Recurrence{Kind: Semimonthly, Day: 15, Day2: 31}); got != 24 {
		t.Errorf("semimonthly: got %d", got)
	}
	if got := count(Recurrence{Kind: Annual, Month: time.June, Day: 15}); got != 1 {
		t.Errorf("annual: got %d", got)
	}
	if got := count(Recurrence{Kind: OneTime, Anchor: day(time.May, 5)}); got != 1 {
		t.Errorf("one-time: got %d", got)
	}
	got := Recurrence{Kind: Monthly, Day: 31}.Between(day(time.January, 31), day(time.March, 31))
	if !slices.Equal(got, []Date{day(time.February, 28), day(time.March, 31)}) {
		t.Errorf("month ends: got %v", got)
	}
}

func TestProjectionChain(t *testing.T) {
	r := NewRecord(day(time.January, 1))
	for _, p := range []*Projection{{Name: "base"}, {Name: "mid", Parent: "base"}, {Name: "leaf", Parent: "mid"}, {Name: "x", Parent: "y"}, {Name: "y", Parent: "x"}} {
		if err := r.AddProjection(p); err != nil {
			t.Fatal(err)
		}
	}
	chain, err := r.Chain("leaf")
	if err != nil || len(chain) != 3 || chain[0].Name != "base" || chain[2].Name != "leaf" {
		t.Fatalf("chain: got %v %v", chain, err)
	}
	if _, err := r.Chain("x"); !errors.Is(err, ErrCycle) {
		t.Errorf("cycle: got %v", err)
	}
	if _, err := r.Chain("nope"); err == nil {
		t.Error("unknown projection accepted")
	}
}

func TestRuleMatchingNothingContributesZero(t *testing.T) {
	d := day(time.March, 1)
	r := newRecord(t, acct("chk", TypeChecking, d, 100))
	none := RuleMember(Rule{Types: []AccountType{TypeBrokerage}})
	addAsset(t, r, "mixed", AccountMember("chk"), none)
	addAsset(t, r, "alone", none)
	if got := balance(t, r, "mixed", d).Amount; got != 100 {
		t.Errorf("mixed: got %v, want 100", got)
	}
	if got := balance(t, r, "alone", d).Amount; got != 0 {
		t.Errorf("alone: got %v, want 0", got)
	}
}
