// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

type dataOp func(st *store.Store, rec *model.Record, o Options) (*Result, error)

// sameDocs checks that two records hold the same accounts, assets and budget.
func sameDocs(t *testing.T, what string, got, want *model.Record) {
	t.Helper()
	if !reflect.DeepEqual(got.Accounts, want.Accounts) || !reflect.DeepEqual(got.Assets, want.Assets) || !reflect.DeepEqual(got.Budget, want.Budget) {
		t.Errorf("%s: the accounts, assets or budget differ", what)
	}
}

// dataDryRun checks that op as a dry run changes neither the record it is given nor the store, and that the real run agrees with it. It returns the store and the real result.
func dataDryRun(t *testing.T, op dataOp) (*store.Store, *Result) {
	t.Helper()
	st, rec := seed(t)
	before := rec.Clone()
	dry, err := op(st, rec, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	sameDocs(t, "the input after a dry run", rec, before)
	sameDocs(t, "the store after a dry run", reload(t, st), before)
	real, err := op(st, rec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	sameDocs(t, "the input after the real run", rec, before)
	sameDocs(t, "dry run and real run", dry.Record, real.Record)
	if dry.Removed != real.Removed {
		t.Error("the dry run removed another thing than the real run")
	}
	if reflect.DeepEqual(reload(t, st).Assets, before.Assets) && reflect.DeepEqual(reload(t, st).Accounts, before.Accounts) && reflect.DeepEqual(reload(t, st).Budget, before.Budget) {
		t.Error("the real run saved nothing")
	}
	return st, real
}

// dataRefused checks that op fails with want and saves and changes nothing.
func dataRefused(t *testing.T, want error, op func(st *store.Store, rec *model.Record) (*Result, error)) {
	t.Helper()
	st, rec := seed(t)
	before := rec.Clone()
	res, err := op(st, rec)
	if !errors.Is(err, want) || res != nil {
		t.Fatalf("got %v, %v; want %v", res, err, want)
	}
	sameDocs(t, "the input", rec, before)
	sameDocs(t, "the store", reload(t, st), before)
}

func seqs(rec *model.Record) []int {
	var out []int
	for _, a := range rec.Assets {
		for _, m := range a.Members {
			out = append(out, m.Seq)
		}
	}
	slices.Sort(out)
	return out
}

func consecutive(t *testing.T, rec *model.Record) {
	t.Helper()
	for i, s := range seqs(rec) {
		if s != i+1 {
			t.Fatalf("member seqs are not 1, 2, ...: %v", seqs(rec))
		}
	}
}

func TestNewAsset(t *testing.T) {
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return NewAsset(st, rec, " Rainy day fund ", "", o)
	})
	a := reload(t, st).Asset("rainy-day-fund")
	if a == nil || a.Name != "Rainy day fund" || len(a.Members) != 0 || res.Asset.ID != "rainy-day-fund" {
		t.Fatalf("asset %+v, result %+v", a, res.Asset)
	}
	if _, err := NewAsset(st, reload(t, st), " rainy DAY fund", "other-id", Options{}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("name of an asset in other case: %v", err)
	}
	// A made id steps around an id that is taken; a given id is used as it is.
	res, err := NewAsset(st, reload(t, st), "Rainy-day fund", "", Options{})
	if err != nil || res.Asset.ID != "rainy-day-fund-2" {
		t.Errorf("second asset: %v, %v", res, err)
	}
	if res, err := NewAsset(st, reload(t, st), "Mine", "my-id", Options{}); err != nil || res.Asset.ID != "my-id" {
		t.Errorf("given id: %v, %v", res, err)
	}

	for _, c := range []struct {
		name, id string
		want     error
	}{
		{" ", "", ErrInvalidValue},
		{"X", "acct:x", ErrInvalidValue},
		{"X", "net-worth", ErrNameTaken},
		{"X", "checking", ErrNameTaken},
		{"net worth", "", ErrNameTaken},
		{" checking ", "", ErrNameTaken},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return NewAsset(st, rec, c.name, c.id, Options{})
		})
	}
	// A made id steps around an account id.
	st2, rec := seed(t)
	if res, err := NewAsset(st2, rec, "Checking pool", "checking-pool", Options{}); err != nil || res.Asset.ID != "checking-pool" {
		t.Errorf("made id: %v, %v", res, err)
	}
	if _, err := NewAsset(st2, rec, "Checking", "", Options{}); !errors.Is(err, ErrNameTaken) || !strings.Contains(err.Error(), "Checking") {
		t.Errorf("name of an account: %v", err)
	}
}

func TestRenameAsset(t *testing.T) {
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return RenameAsset(st, rec, demodata.Liquid, " Cash and brokerage ", o)
	})
	if got := reload(t, st).Asset(demodata.Liquid).Name; got != "Cash and brokerage" || res.Asset.Name != got {
		t.Errorf("name %q", got)
	}
	if _, err := RenameAsset(st, reload(t, st), demodata.Liquid, "Cash and brokerage", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same name: %v", err)
	}
	// An asset can change the case of its own name.
	if res, err := RenameAsset(st, reload(t, st), demodata.Liquid, "CASH and brokerage", Options{}); err != nil || res.Asset.Name != "CASH and brokerage" {
		t.Errorf("own name in other case: %v, %v", res, err)
	}
	for _, c := range []struct {
		id   model.AssetID
		name string
		want error
	}{
		{"nope", "x", ErrUnknownObject},
		{"acct:checking", "x", ErrInvalidValue},
		{demodata.Liquid, "  ", ErrInvalidValue},
		{demodata.Liquid, " all CASH ", ErrNameTaken},
		{demodata.Liquid, "savings", ErrNameTaken},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return RenameAsset(st, rec, c.id, c.name, Options{})
		})
	}
}

func TestDeleteAsset(t *testing.T) {
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return DeleteAsset(st, rec, demodata.Retirement, o)
	})
	rec := reload(t, st)
	if rec.Asset(demodata.Retirement) != nil || res.Removed != string(demodata.Retirement) || res.Asset == nil || len(res.Asset.Members) != 3 {
		t.Errorf("after the delete: %v, result %+v", rec.Asset(demodata.Retirement), res)
	}
	for _, id := range []model.AccountID{demodata.K401, demodata.TSP, demodata.Roth} {
		if rec.Account(id) == nil {
			t.Errorf("the delete removed account %s the asset wrapped", id)
		}
	}
	consecutive(t, rec)
	// The assets after the deleted one are still found.
	if _, err := rec.Resolve(demodata.NetWorth); err != nil {
		t.Error(err)
	}

	dataRefused(t, ErrInvalidValue, func(st *store.Store, rec *model.Record) (*Result, error) {
		return DeleteAsset(st, rec, "acct:checking", Options{})
	})
	dataRefused(t, ErrUnknownObject, func(st *store.Store, rec *model.Record) (*Result, error) {
		return DeleteAsset(st, rec, "nope", Options{})
	})
}

func TestAddMember(t *testing.T) {
	// Savings is also in All cash: the overlap is not refused, the later member is invalid.
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return AddMember(st, rec, demodata.Liquid, model.AccountMember(demodata.Savings), o)
	})
	rec := reload(t, st)
	r, err := rec.Resolve(demodata.Liquid)
	if err != nil {
		t.Fatal(err)
	}
	last := res.Asset.Members[len(res.Asset.Members)-1]
	if o, ok := r.Invalid(demodata.Liquid, last.Seq); !ok || o.Duplicates[0] != demodata.Savings {
		t.Errorf("overlap %+v, %v", o, ok)
	}

	rule := model.RuleMember(model.Rule{Types: []model.AccountType{model.TypeMortgage}, Tax: []model.TaxTreatment{model.Taxable}})
	if res, err := AddMember(st, rec, demodata.AllCash, rule, Options{}); err != nil || res.Asset.Members[len(res.Asset.Members)-1].Rule == nil {
		t.Errorf("rule member: %v, %v", res, err)
	}
	if res, err := AddMember(st, reload(t, st), demodata.AllCash, model.AssetMember(demodata.Colleges), Options{}); err != nil || len(res.Asset.Members) != 3 {
		t.Errorf("asset member: %v, %v", res, err)
	}
	consecutive(t, reload(t, st))

	for _, c := range []struct {
		asset model.AssetID
		m     model.Member
		want  error
	}{
		{demodata.Liquid, model.AccountMember("nope"), ErrUnknownObject},
		{demodata.Liquid, model.AssetMember("nope"), ErrUnknownObject},
		{"nope", model.AccountMember(demodata.Card), ErrUnknownObject},
		{"acct:checking", model.AccountMember(demodata.Card), ErrInvalidValue},
		{demodata.Liquid, model.RuleMember(model.Rule{Types: []model.AccountType{"piggy bank"}}), ErrInvalidValue},
		{demodata.Liquid, model.RuleMember(model.Rule{Tax: []model.TaxTreatment{"pretax"}}), ErrInvalidValue},
		{demodata.Liquid, model.Member{}, ErrInvalidValue},
		{demodata.Liquid, model.AssetMember(demodata.Liquid), model.ErrCycle},
		{demodata.AllCash, model.AssetMember(demodata.Liquid), model.ErrCycle},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddMember(st, rec, c.asset, c.m, Options{})
		})
	}
}

func TestRemoveMember(t *testing.T) {
	seqOf := func(rec *model.Record, acct model.AccountID) int {
		for _, m := range rec.Asset(demodata.Liquid).Members {
			if m.Account == acct {
				return m.Seq
			}
		}
		t.Fatal("no such member")
		return 0
	}
	_, rec := seed(t)
	seq := seqOf(rec, demodata.Checking)
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return RemoveMember(st, rec, demodata.Liquid, seq, o)
	})
	got := reload(t, st)
	if len(got.Asset(demodata.Liquid).Members) != 2 || len(res.Asset.Members) != 2 {
		t.Errorf("members %+v", got.Asset(demodata.Liquid).Members)
	}
	consecutive(t, got)
	r, err := got.Resolve(demodata.Liquid)
	if err != nil || len(r.Overlaps) != 0 {
		t.Errorf("overlaps after removing the invalid member: %v, %v", r, err)
	}

	for _, c := range []struct {
		asset model.AssetID
		seq   int
		want  error
	}{
		{demodata.Liquid, 9999, ErrUnknownObject},
		{"nope", 1, ErrUnknownObject},
		{"acct:checking", 1, ErrInvalidValue},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return RemoveMember(st, rec, c.asset, c.seq, Options{})
		})
	}
}

func TestDefaultIntervals(t *testing.T) {
	iv := model.Interval{From: model.D(2026, 1, 1), To: model.Ptr(model.D(2026, 3, 31)), Value: 1234}
	st, _ := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return AddDefault(st, rec, demodata.NetWorth, iv, o)
	})
	rec := reload(t, st)
	if d := rec.Asset(demodata.NetWorth).Default.Intervals; len(d) != 1 || d[0].Value != 1234 || *d[0].To != *iv.To {
		t.Errorf("default %+v", d)
	}
	if _, err := RemoveDefault(st, rec, demodata.NetWorth, 0, Options{}); err != nil {
		t.Fatal(err)
	}
	if d := reload(t, st).Asset(demodata.NetWorth).Default.Intervals; len(d) != 0 {
		t.Errorf("default after remove %+v", d)
	}

	// An account's default asset takes the account's interval, which is the one resolution reads.
	before := len(rec.Account(demodata.Roth).Default.Intervals)
	res, err := AddDefault(st, rec, "acct:roth", model.Interval{From: model.D(2020, 1, 1), Value: 5}, Options{})
	if err != nil || res.Account == nil || res.Account.ID != demodata.Roth {
		t.Fatalf("account default: %v, %v", res, err)
	}
	got := reload(t, st)
	if n := len(got.Account(demodata.Roth).Default.Intervals); n != before+1 || len(got.Asset("acct:roth").Default.Intervals) != 0 {
		t.Errorf("account intervals %d, was %d; asset intervals %v", n, before, got.Asset("acct:roth").Default.Intervals)
	}
	if _, err := RemoveDefault(st, got, "acct:roth", before, Options{}); err != nil {
		t.Fatal(err)
	}
	if n := len(reload(t, st).Account(demodata.Roth).Default.Intervals); n != before {
		t.Errorf("account intervals after remove: %d", n)
	}

	for _, c := range []struct {
		name string
		op   func(st *store.Store, rec *model.Record) (*Result, error)
		want error
	}{
		{"unknown asset", func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddDefault(st, rec, "nope", iv, Options{})
		}, ErrUnknownObject},
		{"end before start", func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddDefault(st, rec, demodata.NetWorth, model.Interval{From: iv.From, To: model.Ptr(iv.From - 1)}, Options{})
		}, ErrInvalidValue},
		{"not a number", func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddDefault(st, rec, demodata.NetWorth, model.Interval{From: iv.From, Value: math.NaN()}, Options{})
		}, ErrInvalidValue},
		{"index out of range", func(st *store.Store, rec *model.Record) (*Result, error) {
			return RemoveDefault(st, rec, demodata.NetWorth, 0, Options{})
		}, ErrInvalidValue},
		{"negative index", func(st *store.Store, rec *model.Record) (*Result, error) {
			return RemoveDefault(st, rec, "acct:roth", -1, Options{})
		}, ErrInvalidValue},
	} {
		dataRefused(t, c.want, c.op)
	}
}

func TestSetParam(t *testing.T) {
	g := model.MustSchedule(model.Segment{From: model.MinDate, Value: 0.03}, model.Segment{From: model.D(2030, 1, 1), Value: 0.05})
	st, _ := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return SetParam(st, rec, demodata.Retirement, model.PropGrowth, &g, o)
	})
	rec := reload(t, st)
	if got := rec.Asset(demodata.Retirement).Params[model.PropGrowth]; !reflect.DeepEqual(got, g) {
		t.Errorf("param %v", got)
	}
	if _, err := SetParam(st, rec, demodata.Retirement, model.PropGrowth, &g, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same schedule: %v", err)
	}
	if _, err := SetParam(st, rec, "acct:tsp", model.PropGrowth, &g, Options{}); err != nil {
		t.Errorf("param of a default asset: %v", err)
	}
	if _, err := SetParam(st, reload(t, st), demodata.Retirement, model.PropGrowth, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if _, ok := rec.Asset(demodata.Retirement).Params[model.PropGrowth]; ok {
		t.Error("the param was not cleared")
	}
	if _, err := SetParam(st, rec, demodata.Retirement, model.PropGrowth, nil, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("clearing nothing: %v", err)
	}
	bad := model.Constant(math.Inf(1))
	for _, c := range []struct {
		asset model.AssetID
		prop  string
		s     *model.Schedule
		want  error
	}{
		{"nope", model.PropGrowth, &g, ErrUnknownObject},
		{demodata.Retirement, model.PropInflation, &g, ErrInvalidValue},
		{demodata.Retirement, model.PropGrowth, &bad, ErrInvalidValue},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return SetParam(st, rec, c.asset, c.prop, c.s, Options{})
		})
	}
}

func TestNewAccount(t *testing.T) {
	acct := model.Account{Name: "Kid's Roth", Type: model.TypeRothIRA, Tax: model.TaxFree, StatedRate: 0.05}
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return NewAccount(st, rec, acct, o)
	})
	got := reload(t, st)
	a := got.Account("kid-s-roth")
	if a == nil || a.Name != "Kid's Roth" || a.StatedRate != 0.05 || a.Type != model.TypeRothIRA || res.Account.ID != a.ID {
		t.Fatalf("account %+v", a)
	}
	d := got.Asset("acct:kid-s-roth")
	if d == nil || d.Name != a.Name || len(d.Members) != 1 || res.Asset == nil || res.Asset.ID != d.ID {
		t.Errorf("default asset %+v", d)
	}
	consecutive(t, got)
	if b, err := got.Balance(d.ID, model.D(2027, 1, 1)); err != nil || b.Amount != 0 || b.Actual {
		t.Errorf("balance %+v, %v", b, err)
	}

	// Entries and defaults in the input are not taken over.
	withData := acct
	withData.Name = "Other"
	withData.AddSnapshot(model.D(2026, 1, 1), 5)
	if res, err := NewAccount(st, got, withData, Options{}); err != nil || len(res.Account.Snapshots) != 0 {
		t.Errorf("account with a snapshot: %v, %v", res, err)
	}

	for _, c := range []struct {
		name   string
		change func(a *model.Account)
		want   error
	}{
		{"no name", func(a *model.Account) { a.Name = " " }, ErrInvalidValue},
		{"type", func(a *model.Account) { a.Type = "piggy bank" }, ErrInvalidValue},
		{"tax", func(a *model.Account) { a.Tax = "" }, ErrInvalidValue},
		{"rate", func(a *model.Account) { a.StatedRate = 2 }, ErrInvalidValue},
		{"nan rate", func(a *model.Account) { a.StatedRate = math.NaN() }, ErrInvalidValue},
		{"account id", func(a *model.Account) { a.ID = "checking" }, ErrNameTaken},
		{"asset id", func(a *model.Account) { a.ID = "net-worth" }, ErrNameTaken},
		{"account name", func(a *model.Account) { a.Name = " savings " }, ErrNameTaken},
		{"asset name", func(a *model.Account) { a.Name = "net WORTH" }, ErrNameTaken},
	} {
		a := acct
		c.change(&a)
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return NewAccount(st, rec, a, Options{})
		})
	}
}

func TestEditAccount(t *testing.T) {
	ch := AccountChange{Name: model.Ptr(" Main checking "), Type: model.Ptr(model.TypeSavings), Tax: model.Ptr(model.TaxDeferred), StatedRate: model.Ptr(0.02)}
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return EditAccount(st, rec, demodata.Checking, ch, o)
	})
	got := reload(t, st)
	a := got.Account(demodata.Checking)
	if a.Name != "Main checking" || a.Type != model.TypeSavings || a.Tax != model.TaxDeferred || a.StatedRate != 0.02 || res.Account.Name != a.Name {
		t.Errorf("account %+v", a)
	}
	if got.Asset("acct:checking").Name != "Main checking" {
		t.Error("the default asset was not renamed")
	}
	if len(a.Snapshots) == 0 || len(a.Transactions) == 0 {
		t.Error("the edit lost the record")
	}
	if _, err := EditAccount(st, got, demodata.Checking, ch, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same attributes: %v", err)
	}
	if res, err := EditAccount(st, got, demodata.Checking, AccountChange{Name: model.Ptr("MAIN CHECKING")}, Options{}); err != nil || res.Account.Name != "MAIN CHECKING" {
		t.Errorf("case change of its own name: %v", err)
	}
	for _, c := range []struct {
		id   model.AccountID
		ch   AccountChange
		want error
	}{
		{"nope", ch, ErrUnknownObject},
		{demodata.Checking, AccountChange{Name: model.Ptr(" ")}, ErrInvalidValue},
		{demodata.Checking, AccountChange{Name: model.Ptr("SAVINGS")}, ErrNameTaken},
		{demodata.Checking, AccountChange{Name: model.Ptr("all cash")}, ErrNameTaken},
		{demodata.Checking, AccountChange{Type: model.Ptr(model.AccountType("x"))}, ErrInvalidValue},
		{demodata.Checking, AccountChange{Tax: model.Ptr(model.TaxTreatment("x"))}, ErrInvalidValue},
		{demodata.Checking, AccountChange{StatedRate: model.Ptr(5.0)}, ErrInvalidValue},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return EditAccount(st, rec, c.id, c.ch, Options{})
		})
	}
}

func entryCount(t *testing.T, st *store.Store, acct model.AccountID) int {
	t.Helper()
	es, err := st.Entries(acct)
	if err != nil {
		t.Fatal(err)
	}
	return len(es)
}

func TestAddSnapshot(t *testing.T) {
	date := model.D(2026, 11, 8)
	st, rec := seed(t)
	n := entryCount(t, st, demodata.TSP)
	dry, err := AddSnapshot(st, rec, demodata.TSP, date, 150000, Options{DryRun: true})
	if err != nil || dry.Entry.ID != 0 || entryCount(t, st, demodata.TSP) != n {
		t.Fatalf("dry run: %v, %v", dry, err)
	}
	res, err := AddSnapshot(st, rec, demodata.TSP, date, 150000, Options{})
	if err != nil {
		t.Fatal(err)
	}
	es, _ := st.Entries(demodata.TSP)
	last := es[len(es)-1]
	if len(es) != n+1 || last.ID == 0 || res.Entry.ID != last.ID || last.Kind != store.KindSnapshot || last.Date != date || last.Amount != 150000 {
		t.Errorf("entry %+v, result %+v", last, res.Entry)
	}
	b, err := reload(t, st).Balance("acct:tsp", date)
	if err != nil || b.Amount != 150000 || !b.Actual {
		t.Errorf("balance %+v, %v", b, err)
	}

	// A date that has a snapshot is changed by correcting it, never by adding one.
	existing := rec.Account(demodata.TSP).Snapshots[0].Date
	for _, c := range []struct {
		acct model.AccountID
		date model.Date
		bal  float64
		want error
	}{
		{demodata.TSP, existing, 1, ErrInvalidValue},
		{"nope", date, 1, ErrUnknownObject},
		{demodata.TSP, date, math.NaN(), ErrInvalidValue},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddSnapshot(st, rec, c.acct, c.date, c.bal, Options{})
		})
	}
	if _, err := AddSnapshot(st, reload(t, st), demodata.TSP, date, 1, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("snapshot on a date the account got since: %v", err)
	}
}

func TestAddTransaction(t *testing.T) {
	tx := model.Transaction{Date: model.D(2026, 11, 5), Amount: -42.5, Kind: model.TxExpense, Memo: "test"}
	st, rec := seed(t)
	n := entryCount(t, st, demodata.Checking)
	if _, err := AddTransaction(st, rec, demodata.Checking, tx, Options{DryRun: true}); err != nil || entryCount(t, st, demodata.Checking) != n {
		t.Fatalf("dry run: %v", err)
	}
	first, err := AddTransaction(st, rec, demodata.Checking, tx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// An equal transaction is a second payment, not a duplicate to refuse.
	second, err := AddTransaction(st, reload(t, st), demodata.Checking, tx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if entryCount(t, st, demodata.Checking) != n+2 || first.Entry.ID == 0 || second.Entry.ID <= first.Entry.ID {
		t.Errorf("entries %d, ids %d and %d", entryCount(t, st, demodata.Checking), first.Entry.ID, second.Entry.ID)
	}
	e, err := st.Entry(second.Entry.ID)
	if err != nil || e.Kind != store.KindTransaction || e.Amount != -42.5 || e.TxKind != model.TxExpense || e.Memo != "test" {
		t.Errorf("entry %+v, %v", e, err)
	}
	tr := model.Transaction{Date: tx.Date, Amount: -10, Kind: model.TxTransfer, Counter: demodata.Savings}
	if res, err := AddTransaction(st, reload(t, st), demodata.Checking, tr, Options{}); err != nil || res.Entry.Counter != demodata.Savings {
		t.Errorf("transfer: %v, %v", res, err)
	}

	for _, c := range []struct {
		name string
		acct model.AccountID
		tx   model.Transaction
		want error
	}{
		{"unknown account", "nope", tx, ErrUnknownObject},
		{"no counter", demodata.Checking, model.Transaction{Date: tx.Date, Amount: 1, Kind: model.TxTransfer}, ErrInvalidValue},
		{"unknown counter", demodata.Checking, model.Transaction{Date: tx.Date, Amount: 1, Kind: model.TxTransfer, Counter: "nope"}, ErrUnknownObject},
		{"counter is the account", demodata.Checking, model.Transaction{Date: tx.Date, Amount: 1, Kind: model.TxTransfer, Counter: demodata.Checking}, ErrInvalidValue},
		{"kind", demodata.Checking, model.Transaction{Date: tx.Date, Amount: 1, Kind: "gift"}, ErrInvalidValue},
		{"amount", demodata.Checking, model.Transaction{Date: tx.Date, Amount: math.NaN(), Kind: model.TxIncome}, ErrInvalidValue},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddTransaction(st, rec, c.acct, c.tx, Options{})
		})
	}
}

// firstEntry returns the first entry of the kind in the account.
func firstEntry(t *testing.T, st *store.Store, acct model.AccountID, kind store.EntryKind) store.Entry {
	t.Helper()
	es, err := st.Entries(acct)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.Kind == kind {
			return e
		}
	}
	t.Fatal("no entry")
	return store.Entry{}
}

// firstTransaction returns the first transaction of the account with the kind.
func firstTransaction(t *testing.T, st *store.Store, acct model.AccountID, kind model.TxKind) store.Entry {
	t.Helper()
	es, err := st.Entries(acct)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.Kind == store.KindTransaction && e.TxKind == kind {
			return e
		}
	}
	t.Fatalf("no %s transaction", kind)
	return store.Entry{}
}

func TestCorrectEntry(t *testing.T) {
	st, rec := seed(t)
	sn := firstEntry(t, st, demodata.TSP, store.KindSnapshot)
	tx := firstTransaction(t, st, demodata.Checking, model.TxExpense)

	dry, err := CorrectEntry(st, rec, sn.ID, EntryChange{Amount: model.Ptr(1.0)}, "typo", Options{DryRun: true})
	if err != nil || dry.Entry.Amount != 1 || dry.Entry.Date != sn.Date {
		t.Fatalf("dry run: %+v, %v", dry, err)
	}
	if got, _ := st.Entry(sn.ID); got != sn {
		t.Errorf("a dry run stored a correction: %+v", got)
	}
	if revs, _ := st.History(sn.ID); len(revs) != 1 {
		t.Errorf("a dry run stored a revision: %v", revs)
	}
	res, err := CorrectEntry(st, rec, sn.ID, EntryChange{Amount: model.Ptr(1.0)}, "typo", Options{})
	if err != nil || !reflect.DeepEqual(res.Entry, dry.Entry) {
		t.Fatalf("real run: %+v, %v", res, err)
	}
	revs, _ := st.History(sn.ID)
	if len(revs) != 2 || revs[1].Note != "typo" || revs[0].Amount != sn.Amount {
		t.Errorf("history %+v", revs)
	}

	// Only the values given change.
	res, err = CorrectEntry(st, rec, tx.ID, EntryChange{Memo: model.Ptr("fixed")}, "", Options{})
	if err != nil || res.Entry.Memo != "fixed" || res.Entry.Amount != tx.Amount || res.Entry.TxKind != tx.TxKind || res.Entry.Date != tx.Date {
		t.Errorf("memo correction: %+v, %v", res, err)
	}
	if _, err := CorrectEntry(st, rec, tx.ID, EntryChange{Memo: model.Ptr("fixed")}, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same values: %v", err)
	}

	snaps := rec.Account(demodata.TSP).Snapshots
	for _, c := range []struct {
		name string
		id   int64
		ch   EntryChange
		want error
	}{
		{"unknown entry", 99999, EntryChange{Amount: model.Ptr(1.0)}, ErrUnknownObject},
		{"snapshot onto a used date", sn.ID, EntryChange{Date: &snaps[1].Date}, ErrInvalidValue},
		{"snapshot with a memo", sn.ID, EntryChange{Memo: model.Ptr("x")}, ErrInvalidValue},
		{"snapshot with a kind", sn.ID, EntryChange{TxKind: model.Ptr(model.TxIncome)}, ErrInvalidValue},
		{"amount", tx.ID, EntryChange{Amount: model.Ptr(math.NaN())}, ErrInvalidValue},
		{"kind", tx.ID, EntryChange{TxKind: model.Ptr(model.TxKind("gift"))}, ErrInvalidValue},
		{"transfer without counter", tx.ID, EntryChange{TxKind: model.Ptr(model.TxTransfer)}, ErrInvalidValue},
		{"unknown counter", tx.ID, EntryChange{TxKind: model.Ptr(model.TxTransfer), Counter: model.Ptr(model.AccountID("nope"))}, ErrUnknownObject},
	} {
		for _, dry := range []bool{false, true} {
			before, _ := st.Entry(c.id)
			res, err := CorrectEntry(st, rec, c.id, c.ch, "", Options{DryRun: dry})
			if !errors.Is(err, c.want) || res != nil {
				t.Errorf("%s (dry run %v): %v, %v; want %v", c.name, dry, res, err, c.want)
			}
			if after, _ := st.Entry(c.id); after != before {
				t.Errorf("%s: the entry changed", c.name)
			}
		}
	}
}

func TestVoidEntry(t *testing.T) {
	st, rec := seed(t)
	tx := firstEntry(t, st, demodata.Checking, store.KindTransaction)
	n := entryCount(t, st, demodata.Checking)

	dry, err := VoidEntry(st, rec, tx.ID, "dup", Options{DryRun: true})
	if err != nil || !dry.Entry.Void || entryCount(t, st, demodata.Checking) != n {
		t.Fatalf("dry run: %+v, %v", dry, err)
	}
	if _, err := VoidEntry(st, rec, tx.ID, "dup", Options{}); err != nil {
		t.Fatal(err)
	}
	if entryCount(t, st, demodata.Checking) != n-1 {
		t.Error("the entry is still listed")
	}
	if len(reload(t, st).Account(demodata.Checking).Transactions) != n-1-snapshotCount(t, st, demodata.Checking) {
		t.Error("the record still has the voided transaction")
	}
	revs, _ := st.History(tx.ID)
	if len(revs) != 2 || !revs[1].Void || revs[1].Note != "dup" {
		t.Errorf("history %+v", revs)
	}
	if _, err := VoidEntry(st, rec, tx.ID, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("voiding twice: %v", err)
	}
	if _, err := VoidEntry(st, rec, 99999, "", Options{DryRun: true}); !errors.Is(err, ErrUnknownObject) {
		t.Errorf("unknown entry: %v", err)
	}
	// Correcting a voided entry restores it.
	if _, err := CorrectEntry(st, rec, tx.ID, EntryChange{Memo: model.Ptr("back")}, "", Options{}); err != nil || entryCount(t, st, demodata.Checking) != n {
		t.Errorf("restore: %v", err)
	}
}

func snapshotCount(t *testing.T, st *store.Store, acct model.AccountID) int {
	t.Helper()
	es, _ := st.Entries(acct)
	n := 0
	for _, e := range es {
		if e.Kind == store.KindSnapshot {
			n++
		}
	}
	return n
}

func baseItem1() model.BudgetItem {
	return model.BudgetItem{
		Name: "Side Gig", Kind: model.ItemIncome, Amount: model.Constant(800), Account: demodata.Checking,
		Recurrence: model.Recurrence{Kind: model.Monthly, Day: 15}, Start: model.Ptr(model.D(2027, 1, 1)),
	}
}

func TestAddBudgetItem(t *testing.T) {
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return AddBudgetItem(st, rec, baseItem1(), o)
	})
	rec := reload(t, st)
	it, ok := rec.Budget.Item("side-gig")
	if !ok || !sameSchedule(it.Amount, model.Constant(800)) || res.Item.ID != "side-gig" {
		t.Errorf("item %+v, %v", it, ok)
	}
	if res, err := AddBudgetItem(st, rec, baseItem1(), Options{}); err != nil || res.Item.ID != "side-gig-2" {
		t.Errorf("second item: %v, %v", res, err)
	}
	for _, c := range []struct {
		change func(it *model.BudgetItem)
		want   error
	}{
		{func(it *model.BudgetItem) { it.Name = " " }, ErrInvalidValue},
		{func(it *model.BudgetItem) { it.Amount = model.Constant(0) }, ErrInvalidValue},
		{func(it *model.BudgetItem) { it.Account = "nope" }, ErrUnknownTarget},
		{func(it *model.BudgetItem) { it.ID = "salary-a" }, ErrNameTaken},
		{func(it *model.BudgetItem) { it.ID = "early-401k-withdrawal" }, ErrNameTaken},
		{func(it *model.BudgetItem) {
			it.Recurrence = model.Recurrence{Kind: model.Semimonthly, Day: 15, Day2: 15}
		}, ErrInvalidValue},
		{func(it *model.BudgetItem) { it.End = model.Ptr(model.D(2026, 1, 1)) }, ErrInvalidValue},
	} {
		it := baseItem1()
		c.change(&it)
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return AddBudgetItem(st, rec, it, Options{})
		})
	}
}

func TestEditBudgetItem(t *testing.T) {
	_, rec := seed(t)
	groceries, _ := rec.Budget.Item("groceries")
	groceries.Amount = model.Constant(1900)
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return EditBudgetItem(st, rec, "groceries", groceries, o)
	})
	if it, _ := reload(t, st).Budget.Item("groceries"); !sameSchedule(it.Amount, model.Constant(1900)) || !sameSchedule(res.Item.Amount, model.Constant(1900)) {
		t.Errorf("item %+v", it)
	}
	// Order and the other items stay.
	if got := reload(t, st).Budget.Items; len(got) != len(rec.Budget.Items) || slices.IndexFunc(got, func(it model.BudgetItem) bool { return it.ID == "groceries" }) != slices.IndexFunc(rec.Budget.Items, func(it model.BudgetItem) bool { return it.ID == "groceries" }) {
		t.Errorf("items %v", got)
	}
	if _, err := EditBudgetItem(st, reload(t, st), "groceries", groceries, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same item: %v", err)
	}
	// A semimonthly item can be edited, and an item without a start keeps it so.
	salary, _ := rec.Budget.Item("salary-a")
	salary.Amount = model.Constant(5100)
	if _, err := EditBudgetItem(st, reload(t, st), "salary-a", salary, Options{}); err != nil {
		t.Errorf("semimonthly item: %v", err)
	}
	salary.Recurrence.Day2 = 15
	groceries.Recurrence = model.Recurrence{Kind: model.Semimonthly, Day: 1, Day2: 1}
	other := groceries
	other.ID = "dining"
	for _, c := range []struct {
		id   string
		it   model.BudgetItem
		want error
	}{
		{"nope", groceries, ErrUnknownObject},
		{"groceries", groceries, ErrInvalidValue},
		{"salary-a", salary, ErrInvalidValue},
		{"salary-a", other, ErrInvalidValue},
		{"early-401k-withdrawal", groceries, ErrUnknownObject},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return EditBudgetItem(st, rec, c.id, c.it, Options{})
		})
	}
}

func TestEndBudgetItem(t *testing.T) {
	end := model.D(2027, 6, 30)
	st, res := dataDryRun(t, func(st *store.Store, rec *model.Record, o Options) (*Result, error) {
		return EndBudgetItem(st, rec, "salary-a", end, o)
	})
	if it, _ := reload(t, st).Budget.Item("salary-a"); it.End == nil || *it.End != end || res.Item.End == nil || it.Recurrence.Kind != model.Semimonthly {
		t.Errorf("item %+v", it)
	}
	if _, err := EndBudgetItem(st, reload(t, st), "salary-a", end, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same end: %v", err)
	}
	// Ending before the start is refused; an item with a start date shows it.
	st, rec := seed(t)
	if _, err := AddBudgetItem(st, rec, baseItem1(), Options{}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id   string
		end  model.Date
		want error
	}{
		{"nope", end, ErrUnknownObject},
		{"early-401k-withdrawal", end, ErrUnknownObject},
	} {
		dataRefused(t, c.want, func(st *store.Store, rec *model.Record) (*Result, error) {
			return EndBudgetItem(st, rec, c.id, c.end, Options{})
		})
	}
	if _, err := EndBudgetItem(st, reload(t, st), "side-gig", model.D(2026, 12, 31), Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("end before start: %v", err)
	}
	if it, _ := reload(t, st).Budget.Item("side-gig"); it.End != nil {
		t.Error("a refused end was saved")
	}
}

func TestEditAndMoveDefault(t *testing.T) {
	a := model.Interval{From: model.D(2016, 1, 1), To: model.Ptr(model.D(2021, 6, 30)), Value: 200000}
	b := model.Interval{From: model.D(2018, 1, 1), Value: 50}
	st, rec := seed(t)
	for _, iv := range []model.Interval{a, b} {
		if _, err := AddDefault(st, rec, demodata.Retirement, iv, Options{}); err != nil {
			t.Fatal(err)
		}
		rec = reload(t, st)
	}
	edited := model.Interval{From: model.D(2016, 1, 1), To: model.Ptr(model.D(2020, 12, 31)), Value: 210000}
	if _, err := EditDefault(st, rec, demodata.Retirement, 0, edited, Options{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	sameDocs(t, "the store after a dry run", reload(t, st), rec)

	res, err := EditDefault(st, rec, demodata.Retirement, 0, edited, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	got := rec.Asset(demodata.Retirement).Default.Intervals
	if len(got) != 2 || !reflect.DeepEqual(got[0], edited) || !reflect.DeepEqual(got[1], b) || res.Asset == nil {
		t.Errorf("after edit: %+v", got)
	}
	if _, err := EditDefault(st, rec, demodata.Retirement, 0, edited, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("unchanged edit: %v", err)
	}

	// Moving the open-ended interval first lets the other win where they overlap.
	d := model.D(2019, 6, 1)
	if v, _ := rec.Asset(demodata.Retirement).Default.Covers(d); v != 50 {
		t.Fatalf("before the move: %v", v)
	}
	if _, err := MoveDefault(st, rec, demodata.Retirement, 1, 0, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	if v, _ := rec.Asset(demodata.Retirement).Default.Covers(d); v != 210000 {
		t.Errorf("after the move: %v", v)
	}
	if _, err := MoveDefault(st, rec, demodata.Retirement, 1, 1, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("move in place: %v", err)
	}

	// An account's default asset edits the account's intervals.
	if _, err := AddDefault(st, rec, "acct:roth", b, Options{}); err != nil {
		t.Fatal(err)
	}
	rec = reload(t, st)
	i := len(rec.Account(demodata.Roth).Default.Intervals) - 1
	if _, err := EditDefault(st, rec, "acct:roth", i, model.Interval{From: b.From, Value: 7}, Options{}); err != nil {
		t.Fatal(err)
	}
	if v := reload(t, st).Account(demodata.Roth).Default.Intervals[i].Value; v != 7 {
		t.Errorf("account interval value %v", v)
	}

	for _, c := range []struct {
		name string
		op   func(st *store.Store, rec *model.Record) (*Result, error)
		want error
	}{
		{"edit unknown asset", func(st *store.Store, rec *model.Record) (*Result, error) {
			return EditDefault(st, rec, "nope", 0, a, Options{})
		}, ErrUnknownObject},
		{"edit out of range", func(st *store.Store, rec *model.Record) (*Result, error) {
			return EditDefault(st, rec, demodata.NetWorth, 0, a, Options{})
		}, ErrInvalidValue},
		{"move out of range", func(st *store.Store, rec *model.Record) (*Result, error) {
			return MoveDefault(st, rec, demodata.NetWorth, 0, 1, Options{})
		}, ErrInvalidValue},
		{"move unknown asset", func(st *store.Store, rec *model.Record) (*Result, error) {
			return MoveDefault(st, rec, "nope", 0, 0, Options{})
		}, ErrUnknownObject},
	} {
		dataRefused(t, c.want, c.op)
	}
	st3, rec3 := seed(t)
	if _, err := AddDefault(st3, rec3, demodata.NetWorth, a, Options{}); err != nil {
		t.Fatal(err)
	}
	bad := model.Interval{From: a.From, To: model.Ptr(a.From - 1)}
	if _, err := EditDefault(st3, reload(t, st3), demodata.NetWorth, 0, bad, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("end before start: %v", err)
	}
}
