// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"math"
	"reflect"
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
	a.Default.Intervals = []Interval{{From: MinDate, To: Ptr(day(time.January, 9)), Value: 40000}}
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
	a.Default.Intervals = []Interval{{From: MinDate, To: Ptr(day(time.December, 31)), Value: 40000}}
	if got := balance(t, r, DefaultAssetID("a"), day(time.December, 20)); got.Amount != 40000 || got.Actual {
		t.Errorf("default interval: got %+v", got)
	}
}

func TestNestedDefaultResolution(t *testing.T) {
	known, other := day(time.March, 1), day(time.March, 1).Add(100)
	a := acct("a", TypeChecking, 0, 0)
	a.Default.Intervals = []Interval{{From: MinDate, To: nil, Value: 10}}
	b := acct("b", TypeSavings, known, 5) // actual only on one date
	b.Default.Intervals = []Interval{{From: MinDate, To: nil, Value: 20}}
	c := acct("c", TypeSavings, 0, 0)
	c.Default.Intervals = []Interval{{From: MinDate, To: nil, Value: 7}}
	r := newRecord(t, a, b, c)
	addAsset(t, r, "x", AccountMember("a"), AccountMember("b"))
	addAsset(t, r, "y", AssetMember("x"), AccountMember("c"))
	r.Asset("y").Default.Intervals = []Interval{{From: MinDate, To: nil, Value: 999}}

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
	r.Asset("x").Default.Intervals = []Interval{{From: MinDate, To: nil, Value: 100}}
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

func TestApplyOrder(t *testing.T) {
	r := newRecord(t, acct("a", TypeChecking, 0, 0), acct("b", TypeSavings, 0, 0))
	addAsset(t, r, "inner", AccountMember("a"))
	addAsset(t, r, "outer", AssetMember("inner"), AccountMember("b"))
	res, err := r.Resolve("outer")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res.AssetsAround("a"), []AssetID{DefaultAssetID("a"), "inner", "outer"}; !slices.Equal(got, want) {
		t.Errorf("assets around a: got %v, want %v", got, want)
	}
	if got, want := res.AssetsAround("b"), []AssetID{DefaultAssetID("b"), "outer"}; !slices.Equal(got, want) {
		t.Errorf("assets around b: got %v, want %v", got, want)
	}
	set := func(asset AssetID, v float64) Setting[float64] { return Setting[float64]{Asset: asset, Value: v} }
	covers := func(id AssetID) bool { return res.Covers(id, "a") }
	// The last covering setting wins, whatever the assets' nesting.
	got, from, found := Apply(0, []Setting[float64]{set("inner", 1), set("elsewhere", 5), set("outer", 2)}, covers, Replace)
	if !found || got != 2 || from != "outer" {
		t.Errorf("got %v from %v found %v, want 2 from outer", got, from, found)
	}
	got, from, _ = Apply(0, []Setting[float64]{set("outer", 2), set("inner", 1)}, covers, Replace)
	if got != 1 || from != "inner" {
		t.Errorf("reversed: got %v from %v, want 1 from inner", got, from)
	}
	// Nothing covering leaves the base.
	got, _, found = Apply(7, []Setting[float64]{set("elsewhere", 5)}, covers, Replace)
	if found || got != 7 {
		t.Errorf("not covering: got %v found %v", got, found)
	}
}

func TestAssetsAroundTiesFollowScopeOrder(t *testing.T) {
	r := newRecord(t, acct("shared", TypeChecking, 0, 0))
	addAsset(t, r, "sub", AccountMember("shared"))
	addAsset(t, r, "left", AssetMember("sub"))
	// "right" overlaps "left" on the shared account and is marked invalid, yet its settings still apply.
	addAsset(t, r, "right", AccountMember("shared"))
	addAsset(t, r, "top", AssetMember("left"), AssetMember("right"))
	res, err := r.Resolve("top")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res.AssetsAround("shared"), []AssetID{DefaultAssetID("shared"), "sub", "left", "right", "top"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAssetsAroundListsDefaultAssetOnce(t *testing.T) {
	r := newRecord(t, acct("a", TypeChecking, 0, 0))
	addAsset(t, r, "x", AccountMember("a"))
	// The default asset overlaps x on the account and is marked invalid, yet it stays in scope.
	addAsset(t, r, "top", AssetMember("x"), AssetMember(DefaultAssetID("a")))
	res, err := r.Resolve("top")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := res.AssetsAround("a"), []AssetID{DefaultAssetID("a"), "x", "top"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSchedule(t *testing.T) {
	s := MustSchedule(Segment{From: MinDate, Value: 0.04}, Segment{From: day(time.January, 1), Value: 0.06}, Segment{From: day(time.June, 1), Value: 0.02})
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
	split := MustSchedule(Segment{From: MinDate, Value: 0.10}, Segment{From: day(time.July, 1), Value: 0.10})
	a, b := split.Compound(day(time.January, 1), day(time.December, 1)), c.Compound(day(time.January, 1), day(time.December, 1))
	if d := a - b; d > 1e-12 || d < -1e-12 {
		t.Errorf("split vs whole: %v vs %v", a, b)
	}
}

func TestScheduleEnds(t *testing.T) {
	var (
		jan, mar, jun, sep = day(time.January, 1), day(time.March, 1), day(time.June, 1), day(time.September, 1)
		dec                = day(time.December, 1)
	)
	check := func(name string, s Schedule, cases map[Date]float64, none ...Date) {
		t.Helper()
		for d, want := range cases {
			if got, ok := s.At(d); !ok || got != want {
				t.Errorf("%s At(%v): got %v %v, want %v", name, d, got, ok, want)
			}
		}
		for _, d := range none {
			if _, ok := s.At(d); ok {
				t.Errorf("%s At(%v): has a value, want none", name, d)
			}
		}
	}
	// A segment with an End stops there (inclusive) and the schedule has no value after it.
	ended := MustSchedule(Segment{From: jan, End: Ptr(mar.Add(-1)), Value: 5})
	check("ended", ended, map[Date]float64{jan: 5, mar.Add(-1): 5}, jan.Add(-1), mar, dec)
	// A gap between segments is uncovered; the last segment without an End runs on.
	gap := MustSchedule(Segment{From: jan, End: Ptr(mar.Add(-1)), Value: 1}, Segment{From: jun, Value: 2})
	check("gap", gap, map[Date]float64{jan: 1, jun: 2, dec: 2}, mar, sep.Add(-100))
	// An End on the day before the next From touches it and is normalized away.
	touch, err := NewSchedule(Segment{From: jan, End: Ptr(mar.Add(-1)), Value: 1}, Segment{From: mar, Value: 2})
	if err != nil {
		t.Fatalf("touching segments: %v", err)
	}
	if got := touch.Segments(); got[0].End != nil {
		t.Errorf("touching End not normalized: %v", *got[0].End)
	}
	check("touch", touch, map[Date]float64{jan: 1, mar.Add(-1): 1, mar: 2})
	// A one-day segment is valid, and so is an End well before the next From.
	oneDay := MustSchedule(Segment{From: jan, End: Ptr(jan), Value: 4}, Segment{From: mar, Value: 2})
	check("one day", oneDay, map[Date]float64{jan: 4, mar: 2}, jan.Add(1), mar.Add(-1))
	if got := oneDay.Segments(); got[0].End == nil || *got[0].End != jan {
		t.Errorf("one-day End lost: %v", got[0].End)
	}
	for name, segs := range map[string][]Segment{
		"overlap":   {{From: jan, End: Ptr(jun), Value: 1}, {From: mar, Value: 2}},
		"unsorted":  {{From: mar, Value: 2}, {From: jan, Value: 1}},
		"duplicate": {{From: jan, Value: 1}, {From: jan, Value: 2}},
		"reversed":  {{From: mar, End: Ptr(mar.Add(-1)), Value: 1}},
		"on next":   {{From: jan, End: Ptr(mar), Value: 1}, {From: mar, Value: 2}},
	} {
		if _, err := NewSchedule(segs...); err == nil {
			t.Errorf("%s schedule accepted", name)
		}
	}
	// Segments returns a copy.
	cp := gap.Segments()
	cp[0].Value = 99
	*cp[0].End = dec
	check("copy", gap, map[Date]float64{jan: 1}, mar)

	// Overlay is per date: an ended upper segment reverts to the lower value after it.
	base := Constant(0.01)
	check("revert", base.Overlay(MustSchedule(Segment{From: mar, End: Ptr(jun.Add(-1)), Value: 0.05})),
		map[Date]float64{jan: 0.01, mar: 0.05, jun.Add(-1): 0.05, jun: 0.01, dec: 0.01})
	// The lower schedule's own gaps stay uncovered.
	lowerGap := MustSchedule(Segment{From: jan, End: Ptr(mar.Add(-1)), Value: 1}, Segment{From: sep, Value: 3})
	over := lowerGap.Overlay(MustSchedule(Segment{From: jun, End: Ptr(dec.Add(-1)), Value: 7}))
	check("lower gaps", over, map[Date]float64{jan: 1, jun: 7, sep: 7, dec: 3}, mar, jun.Add(-1))
	// An open-ended upper schedule replaces everything from its start on.
	open := lowerGap.Overlay(MustSchedule(Segment{From: jun, Value: 7}))
	check("open", open, map[Date]float64{jan: 1, jun: 7, dec: 7}, mar)
	if got := len(open.Segments()); got != 2 {
		t.Errorf("open overlay segments: got %d, want 2", got)
	}
	// Uncovered dates compound as a factor of 1.
	if got := ended.Compound(mar, dec); got != 1 {
		t.Errorf("compound over uncovered dates: got %v", got)
	}
	if got, want := base.Overlay(MustSchedule(Segment{From: mar, End: Ptr(jun.Add(-1)), Value: 0})).Compound(jan, dec), base.Compound(jan, mar)*base.Compound(jun, dec); math.Abs(got-want) > 1e-12 {
		t.Errorf("compound with an ended zero: got %v, want %v", got, want)
	}
}

func TestScheduleEndAtMaxDate(t *testing.T) {
	from, to := day(time.January, 1), day(time.January, 1).Add(365)
	s := MustSchedule(Segment{From: MinDate, End: Ptr(MaxDate), Value: 0.05})
	if got := s.Segments(); got[0].End != nil {
		t.Errorf("End == MaxDate not normalized: %v", *got[0].End)
	}
	// A schedule holding an End of MaxDate by other means must not wrap either.
	raw := Schedule{segs: []Segment{{From: MinDate, End: Ptr(MaxDate), Value: 0.05}}}
	open := Constant(0.05)
	for name, sc := range map[string]Schedule{"normalized": s, "raw": raw} {
		if got, want := sc.Compound(from, to), open.Compound(from, to); got != want {
			t.Errorf("%s compound: got %v, want %v", name, got, want)
		}
		o := Constant(0.01).Overlay(sc)
		if v, ok := o.At(to); !ok || v != 0.05 {
			t.Errorf("%s overlay At: got %v %v, want 0.05", name, v, ok)
		}
		if got := len(o.Segments()); got != 1 {
			t.Errorf("%s overlay segments: got %d, want 1", name, got)
		}
	}
}

func TestOpenEndedDefaultInterval(t *testing.T) {
	a := acct("a", TypeChecking, 0, 0)
	a.Default.Intervals = []Interval{{From: day(time.March, 1), Value: 77}}
	r := newRecord(t, a)
	for _, c := range []struct {
		date Date
		want float64
	}{{day(time.February, 28), 0}, {day(time.March, 1), 77}, {D(2090, time.January, 1), 77}} {
		if got := balance(t, r, DefaultAssetID("a"), c.date).Amount; got != c.want {
			t.Errorf("%v: got %v, want %v", c.date, got, c.want)
		}
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

func TestFailedAddAssetConsumesNoSeq(t *testing.T) {
	rule := RuleMember(Rule{Types: []AccountType{TypeIRA}})
	build := func(failFirst bool) *Record {
		r := newRecord(t, &Account{ID: "a"})
		if failFirst {
			err := r.AddAsset(&Asset{ID: "y", Members: []Member{rule, AssetMember("missing")}})
			if err == nil {
				t.Fatal("AddAsset with a missing member asset succeeded")
			}
		}
		if err := r.AddAccount(&Account{ID: "b"}); err != nil {
			t.Fatal(err)
		}
		addAsset(t, r, "x", rule)
		if err := r.AddMember("x", AccountMember("b")); err != nil {
			t.Fatal(err)
		}
		return r
	}
	got, want := build(true), build(false)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("record after a failed AddAsset differs from one built without it")
	}
	if m := got.Asset("x").Members; m[1].Seq != 4 {
		t.Errorf("seq after failed AddAsset = %d, want 4", m[1].Seq)
	}
}
