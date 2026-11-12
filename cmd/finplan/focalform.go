// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// kindFocalGrant is the kind the Add form offers for a focal grant: a modification of an equity account that adds a hypothetical grant. It is not an object kind a modification stores.
const kindFocalGrant model.ObjectKind = "focal grant"

// addKinds are the kinds the Add form offers.
var addKinds = append(slices.Clone(objectKinds), kindFocalGrant, kindShock, kindRebalance)

// focalForm is a focal grant as the user enters it: the picks are ids, the rest text as entered. Blank rates mean the default withholding.
type focalForm struct {
	account, symbol, calendar, deposit string
	value, date, last, years           string
	cliff, firstVest                   string
	federal, state, fica               string
	yearly                             bool
}

// untouched reports whether nothing has been picked or typed in the form.
func (f focalForm) untouched() bool {
	return allBlank(f.account, f.symbol, f.calendar, f.deposit, f.value, f.date, f.last, f.years, f.cliff, f.firstVest, f.federal, f.state, f.fica)
}

// parseNumbers reads a comma-separated list of numbers.
func parseNumbers(s string) ([]float64, error) {
	var out []float64
	for _, tok := range strings.Split(s, ",") {
		v, err := parseValue(model.PropAmount, model.ModeSet, tok)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// optionalInt reads a whole number, 0 if the text is blank.
func optionalInt(s, what string) (int, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	return needInt(s, what)
}

// mod builds the modification the form describes: what the form cannot read is an error, and ops checks the rest.
func (f focalForm) mod() (model.Modification, error) {
	g := model.FocalGrant{Symbol: f.symbol, Calendar: f.calendar, Brokerage: model.AccountID(f.deposit), Yearly: f.yearly}
	switch {
	case f.account == "":
		return model.Modification{}, errors.New("pick an equity account")
	case f.symbol == "":
		return model.Modification{}, errors.New("pick a stock")
	case f.calendar == "":
		return model.Modification{}, errors.New("pick a vest calendar")
	case f.deposit == "":
		return model.Modification{}, errors.New("pick the account that net proceeds are deposited in")
	}
	var err error
	if strings.TrimSpace(f.value) == "" {
		return model.Modification{}, errors.New("enter the dollar value of each grant")
	}
	if g.Value, err = parseValue(model.PropAmount, model.ModeSet, f.value); err != nil {
		return model.Modification{}, err
	}
	if g.Date, err = needDate(f.date, "the grant date"); err != nil {
		return model.Modification{}, err
	}
	if s := strings.TrimSpace(f.last); s != "" {
		d, err := parseDate(s)
		if err != nil {
			return model.Modification{}, err
		}
		g.Last = &d
	}
	if strings.TrimSpace(f.years) == "" {
		return model.Modification{}, errors.New("enter the percent vesting in each year, such as 40,30,20,10")
	}
	if g.YearPercents, err = parseNumbers(f.years); err != nil {
		return model.Modification{}, err
	}
	if g.CliffMonths, err = optionalInt(f.cliff, "the cliff months"); err != nil {
		return model.Modification{}, err
	}
	if g.FirstVestMonths, err = optionalInt(f.firstVest, "the months before the first vest"); err != nil {
		return model.Modification{}, err
	}
	rates := model.DefaultWithholding
	for i, text := range []string{f.federal, f.state, f.fica} {
		if strings.TrimSpace(text) == "" {
			continue
		}
		v, err := parseValue(model.PropGrowth, model.ModeSet, text)
		if err != nil {
			return model.Modification{}, err
		}
		*[]*float64{&rates.Federal, &rates.State, &rates.FICA}[i] = v
		g.Rates = &rates
	}
	return model.Modification{Target: model.ObjectRef{Kind: model.KindAccount, ID: f.account}, Property: model.PropAddGrant, Grant: &g}, nil
}

// focalProblem says why the focal grant the form describes cannot be added to the projection, or "" if it can.
func focalProblem(rec *model.Record, proj string, f focalForm) string {
	m, err := f.mod()
	if err != nil {
		return err.Error()
	}
	return reasonOf(ops.CheckMod(rec, proj, m))
}

// focalSummary describes the focal grant in a line for the modification list.
func focalSummary(g model.FocalGrant) string {
	pct := make([]string, len(g.YearPercents))
	for i, p := range g.YearPercents {
		pct[i] = trimFloat(p)
	}
	s := fmt.Sprintf("$%s of %s", trimFloat(g.Value), g.Symbol)
	if g.Yearly {
		s += " every year"
		if g.Last != nil {
			s += " to " + g.Last.String()
		}
	}
	return s + ", vesting " + strings.Join(pct, "/") + " on " + g.Calendar
}

// focalFields draws the Add form for a focal grant of the projection.
func (t *projTab) focalFields(p *model.Projection) {
	rec := effective(t.a.rec, p.Name)
	f := &t.addGrant
	choose := func(id, label string, cur *string, values, names []string) {
		t.label(label)
		preview := "choose"
		if i := slices.Index(values, *cur); i >= 0 {
			preview = names[i]
		}
		if k := pick(id, preview, imgui.FontSize()*24, names, slices.Index(values, *cur)); k >= 0 {
			*cur = values[k]
		}
	}
	text := func(id, label, hint string, v *string) {
		t.label(label)
		imgui.SetNextItemWidth(imgui.FontSize() * 24)
		imgui.InputTextWithHint("##"+id, hint, v, 0, nil)
	}
	var accts, acctNames, deposits, depositNames []string
	for _, a := range rec.Accounts {
		if a.IsClosed() {
			continue
		}
		if a.Type == model.TypeBrokerage {
			deposits, depositNames = append(deposits, string(a.ID)), append(depositNames, a.Name)
		}
		if a.Type == model.TypeEquity {
			accts, acctNames = append(accts, string(a.ID)), append(acctNames, a.Name)
		}
	}
	var cals []string
	for _, c := range rec.Calendars {
		cals = append(cals, c.Name)
	}
	choose("grantacct", "Equity account", &f.account, accts, acctNames)
	symbols := rec.Symbols()
	choose("grantsymbol", "Stock", &f.symbol, symbols, symbols)
	text("grantvalue", "Value of each grant", "dollars, e.g. 60000", &f.value)
	text("grantdate", "Grant date", dateHint, &f.date)
	t.label("Repeats")
	imgui.Checkbox("every year on that month and day##grantyearly", &f.yearly)
	if f.yearly {
		text("grantlast", "Last grant (optional)", "no end; or "+dateHint, &f.last)
	}
	choose("grantcal", "Vest calendar", &f.calendar, cals, cals)
	text("grantyears", "Percent per year", "e.g. 40,30,20,10", &f.years)
	text("grantcliff", "Cliff months (optional)", "0", &f.cliff)
	text("grantfirst", "First vest after (months)", "0: the first calendar date on or after the grant", &f.firstVest)
	text("grantfed", "Federal withholding", "22 (default)", &f.federal)
	text("grantstate", "State withholding", "5.75 (default)", &f.state)
	text("grantfica", "FICA withholding", "7.65 (default)", &f.fica)
	choose("grantdeposit", "Deposit account", &f.deposit, deposits, depositNames)
	formButton("Add##addgrant", focalProblem(t.a.rec, p.Name, *f), f.untouched(), func() { t.addFocalGrant(p.Name) })
}

// addFocalGrant adds the focal grant the Add form describes to the projection and clears the amounts. It reports whether it did.
func (t *projTab) addFocalGrant(proj string) bool {
	if focalProblem(t.a.rec, proj, t.addGrant) != "" {
		return false
	}
	m, _ := t.addGrant.mod()
	if !t.a.edit("add focal grant", func() (*ops.Result, error) {
		return ops.AddMod(t.a.st, t.a.rec, proj, m, ops.Options{})
	}) {
		return false
	}
	t.addGrant.value, t.addGrant.date, t.addGrant.last = "", "", ""
	return true
}
