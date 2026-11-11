// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// priceForm is the Add price form: the date and price as typed.
type priceForm struct{ date, price string }

// read reads the date and the price of the form.
func (f priceForm) read() (model.Date, float64, error) {
	d, err := parseDate(strings.TrimSpace(f.date))
	if err != nil {
		return 0, 0, err
	}
	p, err := parseValue(model.PropAmount, model.ModeSet, f.price)
	if err != nil || p <= 0 {
		return 0, 0, errors.New("enter the price of one share, a positive number")
	}
	return d, p, nil
}

// priceProblem says why the price the form describes cannot be added for the stock, or "" if it can.
func priceProblem(st *store.Store, rec *model.Record, symbol string, f priceForm) string {
	d, p, err := f.read()
	if err != nil {
		return err.Error()
	}
	_, err = ops.AddPrice(st, rec, symbol, d, p, ops.Options{DryRun: true})
	return reasonOf(err)
}

// priceRows lists the stock's price observations newest first.
func priceRows(rec *model.Record, symbol string) []model.PriceObservation {
	rows := rec.Prices.Symbol(symbol)
	slices.Reverse(rows)
	return rows
}

// priceDraft is the text of the correct form of a price observation.
type priceDraft struct{ date, price, note string }

// draftOfPrice is the text of the correct form of a price observation.
func draftOfPrice(p model.PriceObservation) priceDraft {
	return priceDraft{date: p.Date.String(), price: trimFloat(p.Price)}
}

// priceChange is the change that turns the observation into what the draft holds: only the values that differ.
func priceChange(p model.PriceObservation, d priceDraft) (ops.PriceChange, error) {
	var ch ops.PriceChange
	date, price, err := priceForm{d.date, d.price}.read()
	if err != nil {
		return ch, err
	}
	if date != p.Date {
		ch.Date = &date
	}
	if price != p.Price {
		ch.Price = &price
	}
	return ch, nil
}

// priceCorrectProblem says why the observation cannot be corrected to what the draft holds, or "" if it can.
func priceCorrectProblem(st *store.Store, rec *model.Record, p model.PriceObservation, d priceDraft) string {
	ch, err := priceChange(p, d)
	if err != nil {
		return err.Error()
	}
	_, err = ops.CorrectPrice(st, rec, p.ID, ch, d.note, ops.Options{DryRun: true})
	return noChangeReason(err)
}

// pricesSection draws the price observations of the account's stocks: the Add price form and the list, newest first, with correct and void.
func (t *acctTab) pricesSection(acct *model.Account) {
	u, a := &t.eq, t.a
	em := imgui.FontSize()
	imgui.Spacing()
	if !u.section("Prices", "prices") {
		return
	}
	syms := accountSymbols(acct)
	if len(syms) == 0 {
		imgui.TextDisabled("Prices are kept for the stock of a grant: add a grant first.")
		return
	}
	if !slices.Contains(syms, u.sym) {
		u.sym, u.selPrice = syms[0], 0
	}
	if len(syms) > 1 {
		t.label("Stock")
		if k := pick("psym", u.sym, em*10, syms, slices.Index(syms, u.sym)); k >= 0 {
			u.sym, u.selPrice = syms[k], 0
		}
	}
	imgui.TextWrapped("The latest price of " + u.sym + " values the unvested shares. Each recorded vest also records the price it vested at.")
	t.label("Date")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##pdate", dateHint, &u.price.date, 0, nil)
	t.label("Price")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##pprice", "per share", &u.price.price, 0, nil)
	t.label("")
	formButton("Add price", priceProblem(a.st, a.rec, u.sym, u.price), allBlank(u.price.date, u.price.price), func() {
		d, p, _ := u.price.read()
		if a.addEntry("add price", func() (*ops.Result, error) {
			return ops.AddPrice(a.st, a.rec, u.sym, d, p, ops.Options{})
		}) {
			u.price = priceForm{}
		}
	})

	rows := priceRows(a.rec, u.sym)
	if len(rows) == 0 {
		imgui.TextDisabled("No prices yet.")
		return
	}
	if imgui.BeginTableV("##prices", 2, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		imgui.TableSetupColumnV("Date", 0, 1, 0)
		imgui.TableSetupColumnV("Price", 0, 1, 0)
		imgui.TableHeadersRow()
		for _, p := range rows {
			imgui.TableNextRowV(0, 0)
			imgui.TableSetColumnIndex(0)
			if imgui.SelectableBoolV(p.Date.String()+"##price"+strconv.FormatInt(p.ID, 10), u.selPrice == p.ID, imgui.SelectableFlagsSpanAllColumns, imgui.NewVec2(0, 0)) {
				u.selPrice, u.pdraft = p.ID, draftOfPrice(p)
			}
			imgui.TableSetColumnIndex(1)
			imgui.TextUnformatted(formatDollars(p.Price))
		}
		imgui.EndTable()
	}
	if i := slices.IndexFunc(rows, func(p model.PriceObservation) bool { return p.ID == u.selPrice }); i >= 0 {
		t.pricePanel(rows[i])
	} else {
		u.selPrice = 0
	}
}

// pricePanel draws the correct form and the void button of the selected price observation.
func (t *acctTab) pricePanel(p model.PriceObservation) {
	u, a := &t.eq, t.a
	em := imgui.FontSize()
	d := &u.pdraft
	imgui.Spacing()
	imgui.TextUnformatted(fmt.Sprintf("Correct or void the price of %s on %s", p.Symbol, p.Date))
	t.label("Date")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##cpdate", dateHint, &d.date, 0, nil)
	t.label("Price")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##cpprice", "", &d.price, 0, nil)
	t.label("Note")
	imgui.SetNextItemWidth(em * 24)
	imgui.InputTextWithHint("##cpnote", "why; kept in the history", &d.note, 0, nil)
	t.label("")
	actionButton("Correct##price", priceCorrectProblem(a.st, a.rec, p, *d), func() {
		ch, _ := priceChange(p, *d)
		a.addEntry("correct price", func() (*ops.Result, error) {
			return ops.CorrectPrice(a.st, a.rec, p.ID, ch, d.note, ops.Options{})
		})
	})
	imgui.SameLine()
	actionButton("Void##price", "", func() {
		a.addEntry("void price", func() (*ops.Result, error) {
			return ops.VoidPrice(a.st, a.rec, p.ID, d.note, ops.Options{})
		})
	})
}
