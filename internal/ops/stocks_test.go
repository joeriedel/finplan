// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func stockMod(symbol string, mode model.Mode, v float64) model.Modification {
	return model.Modification{Target: model.ObjectRef{Kind: model.KindStock, ID: symbol}, Property: model.PropGrowth, Mode: mode, Values: model.Constant(v)}
}

func focalMod(g model.FocalGrant) model.Modification {
	return model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Equity)}, Property: model.PropAddGrant, Grant: &g}
}

func goodFocal() model.FocalGrant {
	return model.FocalGrant{Symbol: demodata.Symbol, Value: 50000, Date: model.D(2027, time.March, 1), Yearly: true, Calendar: demodata.CalendarName,
		YearPercents: []float64{40, 30, 20, 10}, FirstVestMonths: 1, Brokerage: demodata.Brokerage}
}

func TestStockMods(t *testing.T) {
	st, rec := seed(t)
	for name, m := range map[string]model.Modification{
		"set":   stockMod(demodata.Symbol, "", 0.15),
		"scale": stockMod(demodata.Symbol, model.ModeScale, 0.5),
	} {
		res, err := AddMod(st, rec, demodata.ProjBase, m, Options{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rec = res.Record
	}
	got := reload(t, st).Projection(demodata.ProjBase).Mods
	if len(got) != 2 || got[0].Target.Kind != model.KindStock || got[1].Mode != model.ModeScale {
		t.Errorf("stored mods: %+v", got)
	}
	for name, c := range map[string]struct {
		m    model.Modification
		want error
	}{
		"unknown stock":  {stockMod("NOPE", "", 0.1), ErrUnknownTarget},
		"crash past all": {stockMod(demodata.Symbol, "", -1), ErrInvalidValue},
		"negative scale": {stockMod(demodata.Symbol, model.ModeScale, -0.1), ErrInvalidValue},
		"no symbol":      {stockMod("", "", 0.1), ErrNoObject},
		"wrong property": {model.Modification{Target: model.ObjectRef{Kind: model.KindStock, ID: demodata.Symbol}, Property: model.PropAmount, Values: model.Constant(1)}, ErrInvalidValue},
	} {
		if _, err := AddMod(st, rec, demodata.ProjBase, c.m, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	// Prices known by a symbol alone count: a stock with a price and no grant.
	rec.Prices.Add(model.PriceObservation{ID: 99, Symbol: "BETA", Date: rec.Today, Price: 10})
	if _, err := AddMod(st, rec, demodata.ProjBase, stockMod("BETA", "", 0.1), Options{DryRun: true}); err != nil {
		t.Errorf("a stock with only a price: %v", err)
	}
}

func TestFocalGrantMods(t *testing.T) {
	st, rec := seed(t)
	g := goodFocal()
	if _, err := AddMod(st, rec, demodata.ProjBase, focalMod(g), Options{}); err != nil {
		t.Fatal(err)
	}
	stored := reload(t, st).Projection(demodata.ProjBase).Mods
	if len(stored) != 1 || !reflect.DeepEqual(*stored[0].Grant, g) {
		t.Fatalf("stored %+v, want %+v", stored, g)
	}
	rec = reload(t, st)
	rates := model.Withholding{Federal: 0.24, State: 0.05, FICA: 0.0765}
	last := model.D(2030, time.March, 1)
	for name, change := range map[string]func(*model.FocalGrant){
		"unknown stock":    func(g *model.FocalGrant) { g.Symbol = "NOPE" },
		"unknown calendar": func(g *model.FocalGrant) { g.Calendar = "Nope" },
		"unknown deposit":  func(g *model.FocalGrant) { g.Brokerage = "nope" },
		"equity deposit":   func(g *model.FocalGrant) { g.Brokerage = demodata.Equity },
		"checking deposit": func(g *model.FocalGrant) { g.Brokerage = demodata.Checking },
		"percents":         func(g *model.FocalGrant) { g.YearPercents = []float64{40, 30} },
		"no value":         func(g *model.FocalGrant) { g.Value = 0 },
		"last before":      func(g *model.FocalGrant) { l := g.Date.Add(-1); g.Last = &l },
		"last not yearly":  func(g *model.FocalGrant) { g.Yearly, g.Last = false, &last },
		"bad rates":        func(g *model.FocalGrant) { g.Rates = &model.Withholding{Federal: 0.7, State: 0.3} },
		"cliff too long":   func(g *model.FocalGrant) { g.CliffMonths = 60 },
	} {
		bad := goodFocal()
		change(&bad)
		if _, err := AddMod(st, rec, demodata.ProjBase, focalMod(bad), Options{}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	good := goodFocal()
	good.Last, good.Rates = &last, &rates
	if _, err := AddMod(st, rec, demodata.ProjBase, focalMod(good), Options{DryRun: true}); err != nil {
		t.Errorf("last and rates: %v", err)
	}
	if _, err := AddMod(st, rec, demodata.ProjBase, model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: string(demodata.Equity)}, Property: model.PropAddGrant}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("no grant: %v", err)
	}
	scaled := focalMod(goodFocal())
	scaled.Mode = model.ModeScale
	if _, err := AddMod(st, rec, demodata.ProjBase, scaled, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("scale: %v", err)
	}
	onChecking := focalMod(goodFocal())
	onChecking.Target.ID = string(demodata.Checking)
	if _, err := AddMod(st, rec, demodata.ProjBase, onChecking, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("not an equity account: %v", err)
	}
	onAsset := focalMod(goodFocal())
	onAsset.Target = model.ObjectRef{Kind: model.KindAsset, ID: string(demodata.Retirement)}
	if _, err := AddMod(st, rec, demodata.ProjBase, onAsset, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("on an asset: %v", err)
	}
}

func TestSetStockGrowth(t *testing.T) {
	st, rec := seed(t)
	growth := model.MustSchedule(model.Segment{From: model.MinDate, End: model.Ptr(model.D(2028, time.December, 31)), Value: 0.08}, model.Segment{From: model.D(2029, time.January, 1), Value: 0.03})
	if _, err := SetStockGrowth(st, rec, demodata.Symbol, &growth, Options{DryRun: true}); err != nil || len(reload(t, st).StockGrowth) != 0 {
		t.Fatalf("dry run: %v", err)
	}
	res, err := SetStockGrowth(st, rec, demodata.Symbol, &growth, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).StockGrowthOf(demodata.Symbol); !reflect.DeepEqual(got.Segments(), growth.Segments()) {
		t.Errorf("stored %v, want %v", got.Segments(), growth.Segments())
	}
	if _, err := SetStockGrowth(st, res.Record, demodata.Symbol, &growth, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same again: %v", err)
	}
	if _, err := SetStockGrowth(st, rec, "NOPE", &growth, Options{}); !errors.Is(err, ErrUnknownTarget) {
		t.Errorf("unknown: %v", err)
	}
	bad := model.Constant(-1)
	if _, err := SetStockGrowth(st, res.Record, demodata.Symbol, &bad, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("-100%%: %v", err)
	}
	if _, err := SetStockGrowth(st, res.Record, demodata.Symbol, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, st).StockGrowth; len(got) != 0 {
		t.Errorf("cleared: %v", got)
	}
	if _, err := SetStockGrowth(st, reload(t, st), demodata.Symbol, nil, Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("clear again: %v", err)
	}
}
