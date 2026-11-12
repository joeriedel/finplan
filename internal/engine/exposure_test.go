// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestExposureAddsUpToBalances(t *testing.T) {
	for name, rec := range map[string]*model.Record{"fixture": marketFixture(t), "demo": demodata.Family()} {
		exp, err := Exposure(rec)
		if err != nil {
			t.Fatal(err)
		}
		if len(exp) != len(rec.Classes) {
			t.Fatalf("%s: %d classes, want %d", name, len(exp), len(rec.Classes))
		}
		perAccount := map[model.AccountID]float64{}
		for _, c := range exp {
			sum := 0.0
			for _, x := range c.Accounts {
				sum += x.Amount
				perAccount[x.Account.ID] += x.Amount
			}
			if !within(sum, c.Total, 1e-6) {
				t.Errorf("%s: class %s lists %v, total %v", name, c.Class.ID, sum, c.Total)
			}
		}
		allocated := 0
		for _, a := range rec.Accounts {
			if len(a.Allocation) == 0 {
				if _, ok := perAccount[a.ID]; ok {
					t.Errorf("%s: unallocated account %s is exposed", name, a.ID)
				}
				continue
			}
			allocated++
			res, err := Balances(rec, "", model.DefaultAssetID(a.ID), []model.Date{rec.Today})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := perAccount[a.ID], res.Samples[0].Amount; !within(got, want, 1e-6) {
				t.Errorf("%s: account %s exposures sum to %v, balance %v", name, a.ID, got, want)
			}
		}
		if allocated == 0 {
			t.Errorf("%s: no allocated accounts to check", name)
		}
	}
}

func TestExposureOfFixture(t *testing.T) {
	exp, err := Exposure(marketFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if c := exp[0]; c.Class.ID != model.ClassLargeCap || len(c.Accounts) != 2 || !within(c.Total, 1200000, 1) {
		t.Errorf("large cap: %+v", c)
	}
}

func TestExposureIsTheEnginesClassBalances(t *testing.T) {
	snap, today := d(2026, time.January, 1), d(2026, time.November, 12)
	rec := model.NewRecord(today)
	rec.Classes = model.DefaultClasses()
	a := &model.Account{ID: "a", Type: model.TypeBrokerage, Tax: model.Taxable, Allocation: []model.Holding{{Class: model.ClassLargeCap, Percent: 60}, {Class: model.ClassBonds, Percent: 40}}}
	a.AddSnapshot(snap, 100000)
	must(t, rec.AddAccount(a))
	days := today - snap
	lc, bonds := 60000*grow(0.10, days), 40000*grow(0.045, days)
	exp, err := Exposure(rec)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]AccountExposure{}
	for _, c := range exp {
		if len(c.Accounts) == 1 {
			got[c.Class.ID] = c.Accounts[0]
		}
	}
	if x := got[model.ClassLargeCap]; !within(x.Amount, lc, 1e-4) || x.Percent != 60 {
		t.Errorf("large cap = %+v, want amount %v at target 60", x, lc)
	}
	if x := got[model.ClassBonds]; !within(x.Amount, bonds, 1e-4) || x.Percent != 40 {
		t.Errorf("bonds = %+v, want amount %v at target 40", x, bonds)
	}
	if bal := balanceOf(t, rec, "", "a", today); !within(lc+bonds, bal, 1e-4) {
		t.Errorf("class amounts %v, balance %v", lc+bonds, bal)
	}
	if x := got[model.ClassLargeCap]; within(x.Amount, x.Balance*0.6, 1) {
		t.Errorf("large cap amount %v is balance times percent", x.Amount)
	}
}

func TestExposureSkipsClosedAccounts(t *testing.T) {
	rec := marketFixture(t)
	closed := d(2025, time.December, 1)
	rec.Account("k").Closed = &closed
	exp, err := Exposure(rec)
	if err != nil {
		t.Fatal(err)
	}
	if c := exp[0]; c.Class.ID != model.ClassLargeCap || len(c.Accounts) != 1 || c.Accounts[0].Account.ID != "a" {
		t.Errorf("large cap with k closed: %+v", c)
	}
	rec.Account("a").Closed = &closed
	if exp, err = Exposure(rec); err != nil {
		t.Fatal(err)
	}
	for _, c := range exp {
		if len(c.Accounts) != 0 || c.Total != 0 {
			t.Errorf("class %s still lists closed accounts: %+v", c.Class.ID, c)
		}
	}
}
