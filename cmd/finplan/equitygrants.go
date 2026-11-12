// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// equityUI is the state of the equity sections of the selected equity account: its grants, the vest calendars and the prices.
type equityUI struct {
	// acct is the account the state belongs to; it is dropped when another is selected.
	acct model.AccountID
	// scroll names the section to scroll into view on the next frame it is drawn; set by --open.
	scroll string

	// grantInit says grant has been initialized for acct.
	grant     grantForm
	grantInit bool
	// sel is the selected grant, with its schedule and drafts read for selRec.
	sel      int64
	selRec   *model.Record
	rows     []ops.ScheduleRow
	schedErr error
	pct      []string
	draft    grantDraft
	problems [6]problemCache

	vest    *vestForm
	selVest int64
	vdraft  vestDraft
	// deposits are the unmatched deposits the tie picker lists, read for depKey.
	deposits []store.Entry
	depKey   string
	depErr   error

	confirm confirmDialog

	sym      string
	price    priceForm
	selPrice int64
	pdraft   priceDraft

	cal      string
	calNew   bool
	calDraft calDraft
	calBase  *model.VestCalendar
}

// scrollHere scrolls the item just drawn into view if it heads the section asked for.
func (u *equityUI) scrollHere(name string) {
	if u.scroll == name {
		u.scroll = ""
		imgui.SetScrollHereYV(0)
	}
}

// section draws the collapsible header of a section that is closed until the user opens it, or until the section is the one to scroll to, and reports whether it is open.
func (u *equityUI) section(title, name string) bool {
	if u.scroll == name {
		imgui.SetNextItemOpenV(true, imgui.CondAlways)
	}
	open := imgui.CollapsingHeaderTreeNodeFlags(title + "##section")
	u.scrollHere(name)
	return open
}

// grantRow is one line of the grants table.
type grantRow struct {
	ID               int64
	Symbol           string
	Date             model.Date
	Shares           int
	Price            float64
	Vested, Unvested int
	// UnvestedValue is the unvested shares at the stock's latest price, or at the grant price if it has none.
	UnvestedValue float64
}

// grantRows lists the account's grants in the order they were added.
func grantRows(rec *model.Record, acct *model.Account) []grantRow {
	rows := make([]grantRow, len(acct.Grants))
	for i, g := range acct.Grants {
		vested := acct.VestedBy(g.ID, model.MaxDate)
		px, ok := rec.Prices.At(g.Symbol, model.MaxDate)
		if !ok {
			px = g.Price
		}
		rows[i] = grantRow{g.ID, g.Symbol, g.Date, g.Shares, g.Price, vested, g.Shares - vested, float64(g.Shares-vested) * px}
	}
	return rows
}

// scheduleCells is a schedule row as the cells of the schedule table, except the percent and the action: number, date, shares, status, actual price, net proceeds, date difference and share difference.
func scheduleCells(r ops.ScheduleRow) []string {
	cells := []string{strconv.Itoa(r.Index), r.Date.String(), strconv.Itoa(r.Shares), r.Status, "", "", "", ""}
	if v := r.Vest; v != nil {
		cells[2] = strconv.Itoa(v.Shares)
		cells[4], cells[5] = formatDollars(v.Price), formatDollars(v.Net())
		cells[1] = v.Date.String()
		cells[6], cells[7] = signed(r.DateDiff, "day"), signed(r.ShareDiff, "share")
	}
	return cells
}

// signed writes a difference such as "+3 days", or "-" for none.
func signed(n int, unit string) string {
	if n == 0 {
		return "-"
	}
	s := fmt.Sprintf("%+d %s", n, unit)
	if n != 1 && n != -1 {
		s += "s"
	}
	return s
}

// grantForm is the Add grant form: every field is the text or choice as entered. The rates are percentages.
type grantForm struct {
	byShares            bool
	amount, price, date string
	symbol              string
	calendar            string
	first               model.Date
	years, cliff        string
	brokerage           model.AccountID
	fed, state, fica    string
}

// ratesText writes withholding rates as the percentages the form holds.
func ratesText(w model.Withholding) (fed, state, fica string) {
	return trimFloat(w.Federal * 100), trimFloat(w.State * 100), trimFloat(w.FICA * 100)
}

// accountSymbols lists the stocks the account's grants are in, in the order of the grants.
func accountSymbols(acct *model.Account) []string {
	var out []string
	for _, g := range acct.Grants {
		if !slices.Contains(out, g.Symbol) {
			out = append(out, g.Symbol)
		}
	}
	return out
}

// newGrantForm is an empty form with the default rates and the account's stock, if it has one.
func newGrantForm(acct *model.Account) grantForm {
	f := grantForm{}
	f.fed, f.state, f.fica = ratesText(model.DefaultWithholding)
	if s := accountSymbols(acct); len(s) > 0 {
		f.symbol = s[len(s)-1]
	}
	if n := len(acct.Grants); n > 0 {
		f.calendar, f.brokerage = acct.Grants[n-1].Calendar, acct.Grants[n-1].Brokerage
	}
	return f
}

// parseYearPercents reads yearly percents written as 25/25/25/25; commas and spaces separate too.
func parseYearPercents(s string) ([]float64, error) {
	toks := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == ',' || r == ' ' })
	if len(toks) == 0 {
		return nil, errors.New("enter the yearly percents, such as 25/25/25/25")
	}
	out := make([]float64, len(toks))
	for i, tok := range toks {
		v, err := parseValue(model.PropAmount, model.ModeSet, tok)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// parseRates reads three withholding percentages. A rate is kept to six decimals as a fraction, so 7.65 is exactly the default 0.0765.
func parseRates(fed, state, fica string) (model.Withholding, error) {
	var w model.Withholding
	for _, r := range []struct {
		name, text string
		to         *float64
	}{{"federal rate", fed, &w.Federal}, {"state rate", state, &w.State}, {"FICA rate", fica, &w.FICA}} {
		v, err := parseValue(model.PropGrowth, model.ModeSet, r.text)
		if err != nil {
			return w, fmt.Errorf("%s: %v", r.name, err)
		}
		*r.to = math.Round(v*1e6) / 1e6
	}
	return w, nil
}

// parseCount reads a whole number of at least min.
func parseCount(what, s string, min int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < min {
		return 0, fmt.Errorf("%s must be a whole number of %d or more", what, min)
	}
	return n, nil
}

// grantDeriv is what the Add grant form's first fields amount to: the grant price and, when the amount and price read, the shares.
type grantDeriv struct {
	Price     float64
	Shares    int
	HasShares bool
}

// derive reads the amount and the grant price of the form: shares are the whole shares of the value over the price, or the shares as entered.
func (f grantForm) derive() (grantDeriv, error) {
	var d grantDeriv
	price, err := parseValue(model.PropAmount, model.ModeSet, f.price)
	if err != nil || price <= 0 {
		return d, errors.New("enter the grant price, a positive number")
	}
	d.Price = price
	if f.byShares {
		if d.Shares, err = parseCount("the shares", f.amount, 1); err != nil {
			return d, err
		}
	} else {
		value, err := parseValue(model.PropAmount, model.ModeSet, f.amount)
		if err != nil || value <= 0 {
			return d, errors.New("enter the dollar value of the grant, a positive number")
		}
		if d.Shares = model.SharesFor(value, price); d.Shares < 1 {
			return d, errors.New("the value buys less than one share")
		}
	}
	d.HasShares = true
	return d, nil
}

// input reads the form as the grant to add for the account, or says what is missing or cannot be read.
func (f grantForm) input(acct model.AccountID) (ops.GrantInput, error) {
	in := ops.GrantInput{Account: acct, Symbol: strings.TrimSpace(f.symbol), Calendar: f.calendar, FirstVest: f.first, Brokerage: f.brokerage}
	if in.Symbol == "" {
		return in, errors.New("enter the stock's ticker symbol")
	}
	d, err := f.derive()
	if err != nil {
		return in, err
	}
	if f.byShares {
		in.Shares = d.Shares
	} else {
		in.Value, _ = parseValue(model.PropAmount, model.ModeSet, f.amount)
	}
	in.Price = d.Price
	if in.Date, err = parseDate(strings.TrimSpace(f.date)); err != nil {
		return in, err
	}
	if in.Calendar == "" {
		return in, errors.New("pick a vest calendar")
	}
	if in.FirstVest == 0 {
		return in, errors.New("pick the first vest date")
	}
	if in.YearPercents, err = parseYearPercents(f.years); err != nil {
		return in, err
	}
	if strings.TrimSpace(f.cliff) != "" {
		if in.CliffMonths, err = parseCount("the cliff", f.cliff, 0); err != nil {
			return in, err
		}
	}
	if in.Brokerage == "" {
		return in, errors.New("pick the brokerage account the proceeds go to")
	}
	w, err := parseRates(f.fed, f.state, f.fica)
	if err != nil {
		return in, err
	}
	in.Rates = &w
	return in, nil
}

// grantProblem says why the grant the form describes cannot be added to the account, or "" if it can.
func grantProblem(st *store.Store, rec *model.Record, acct model.AccountID, f grantForm) string {
	in, err := f.input(acct)
	if err != nil {
		return err.Error()
	}
	_, err = ops.AddGrant(st, rec, in, ops.Options{DryRun: true})
	return reasonOf(err)
}

// grantPreview is what saving the form would make: the derived shares and the generated schedule.
type grantPreview struct {
	grantDeriv
	Plan []model.PlannedVest
	// PlanErr is why there is no plan, once the calendar, first vest and yearly percents are entered.
	PlanErr string
}

// previewGrant derives the shares and the schedule from what the form holds so far; either is left out while its fields are not readable.
func previewGrant(rec *model.Record, f grantForm) grantPreview {
	var p grantPreview
	p.grantDeriv, _ = f.derive()
	cal := rec.Calendar(f.calendar)
	if cal == nil || f.first == 0 || strings.TrimSpace(f.years) == "" {
		return p
	}
	years, err := parseYearPercents(f.years)
	cliff := 0
	if err == nil && strings.TrimSpace(f.cliff) != "" {
		cliff, err = parseCount("the cliff", f.cliff, 0)
	}
	var pct []float64
	if err == nil {
		pct, err = model.GeneratePercents(cal, f.first, years, cliff)
	}
	if err != nil {
		p.PlanErr = err.Error()
		return p
	}
	g := model.Grant{Shares: p.Shares, Calendar: f.calendar, FirstVest: f.first, YearPercents: years, CliffMonths: cliff, VestPercents: pct}
	if p.Plan, err = g.Plan(cal); err != nil {
		p.PlanErr = err.Error()
	}
	return p
}

// firstVestChoices lists the dates of the calendar to start vesting on: the first twelve from the grant's date, or from today while the date is not entered.
func firstVestChoices(rec *model.Record, calendar, grantDate string) []model.Date {
	cal := rec.Calendar(calendar)
	if cal == nil {
		return nil
	}
	from, err := parseDate(strings.TrimSpace(grantDate))
	if err != nil {
		from = rec.Today
	}
	dates := cal.Dates(from, from.Add(4*366))
	return dates[:min(len(dates), 12)]
}

// grantDraft is the text of the correct form of the selected grant. The rates are percentages.
type grantDraft struct {
	date, shares, price string
	fed, state, fica    string
	note                string
}

// draftOfGrant is the text of the correct form of a grant.
func draftOfGrant(g *model.Grant) grantDraft {
	fed, state, fica := ratesText(g.Rates)
	return grantDraft{date: g.Date.String(), shares: strconv.Itoa(g.Shares), price: trimFloat(g.Price), fed: fed, state: state, fica: fica}
}

// grantChange is the change that turns the grant into what the draft holds: only the values that differ.
func grantChange(g *model.Grant, d grantDraft) (ops.GrantChange, error) {
	var ch ops.GrantChange
	date, err := parseDate(strings.TrimSpace(d.date))
	if err != nil {
		return ch, err
	}
	shares, err := parseCount("the shares", d.shares, 1)
	if err != nil {
		return ch, err
	}
	price, err := parseValue(model.PropAmount, model.ModeSet, d.price)
	if err != nil || price <= 0 {
		return ch, errors.New("the grant price must be a positive number")
	}
	w, err := parseRates(d.fed, d.state, d.fica)
	if err != nil {
		return ch, err
	}
	if date != g.Date {
		ch.Date = &date
	}
	if price != g.Price {
		ch.Price = &price
	}
	if shares != g.Shares {
		ch.Shares = &shares
	}
	if w != g.Rates {
		ch.Rates = &w
	}
	return ch, nil
}

// noChangeText is the reason a correction gives when it would leave everything as it is.
const noChangeText = "nothing is changed"

// noChangeReason is reasonOf for a correction, where an error that leaves everything as it was reads noChangeText.
func noChangeReason(err error) string {
	if errors.Is(err, ops.ErrNoChange) {
		return noChangeText
	}
	return reasonOf(err)
}

// grantCorrectProblem says why the grant cannot be corrected to what the draft holds, or "" if it can.
func grantCorrectProblem(st *store.Store, rec *model.Record, g *model.Grant, d grantDraft) string {
	ch, err := grantChange(g, d)
	if err != nil {
		return err.Error()
	}
	_, err = ops.CorrectGrant(st, rec, g.ID, ch, d.note, ops.Options{DryRun: true})
	return noChangeReason(err)
}

// grantVoidProblem says why the grant cannot be voided, or "" if it can.
func grantVoidProblem(st *store.Store, rec *model.Record, id int64) string {
	_, err := ops.VoidGrant(st, rec, id, "", ops.Options{DryRun: true})
	return reasonOf(err)
}

// percentsProblem says why the percents typed in the schedule cannot be saved as the grant's vest percents, or "" if they can.
func percentsProblem(st *store.Store, rec *model.Record, id int64, texts []string) string {
	p, err := parseVestPercents(texts)
	if err != nil {
		return err.Error()
	}
	_, err = ops.CorrectGrant(st, rec, id, ops.GrantChange{VestPercents: p}, "", ops.Options{DryRun: true})
	return noChangeReason(err)
}

// parseVestPercents reads the percent typed for each planned vest.
func parseVestPercents(texts []string) ([]float64, error) {
	out := make([]float64, len(texts))
	for i, s := range texts {
		v, err := parseValue(model.PropAmount, model.ModeSet, s)
		if err != nil {
			return nil, fmt.Errorf("vest %d: %v", i+1, err)
		}
		out[i] = v
	}
	return out, nil
}

// enter drops the state of another account and sets up the Add grant form once for acct; the form is never reset by what is typed in it.
func (u *equityUI) enter(acct *model.Account) {
	if u.acct != acct.ID {
		*u = equityUI{acct: acct.ID, scroll: u.scroll}
	}
	if !u.grantInit {
		u.grant, u.grantInit = newGrantForm(acct), true
	}
}

// equity draws the equity sections of an equity account's detail: its grants, the vest calendars and the stock prices.
func (t *acctTab) equity(acct *model.Account) {
	u := &t.eq
	u.enter(acct)
	t.grantsSection(acct)
	t.calendarsSection(acct)
	t.pricesSection(acct)
}

// grantsSection draws the Grants section: the table, the selected grant and the Add grant form.
func (t *acctTab) grantsSection(acct *model.Account) {
	u, a := &t.eq, t.a
	imgui.Spacing()
	imgui.SeparatorText("Grants")
	u.scrollHere("grants")
	if u.sel != 0 {
		if g, _ := a.rec.Grant(u.sel); g == nil || g.Account != acct.ID {
			u.sel, u.vest, u.selVest = 0, nil, 0
		}
	}
	rows := grantRows(a.rec, acct)
	if len(rows) == 0 {
		disabledText("No grants yet. Add one below.")
	} else if imgui.BeginTableV("##grants", 7, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		for _, h := range []string{"Stock", "Granted", "Shares", "Grant price", "Vested", "Unvested", "Unvested value"} {
			imgui.TableSetupColumnV(h, 0, 1, 0)
		}
		imgui.TableHeadersRow()
		for _, r := range rows {
			imgui.TableNextRowV(0, 0)
			imgui.TableSetColumnIndex(0)
			if imgui.SelectableBoolV(r.Symbol+"##grant"+strconv.FormatInt(r.ID, 10), u.sel == r.ID, imgui.SelectableFlagsSpanAllColumns, imgui.NewVec2(0, 0)) {
				u.sel, u.selRec, u.vest, u.selVest = r.ID, nil, nil, 0
			}
			for i, s := range []string{r.Date.String(), strconv.Itoa(r.Shares), formatDollars(r.Price), strconv.Itoa(r.Vested), strconv.Itoa(r.Unvested), formatDollars(r.UnvestedValue)} {
				imgui.TableSetColumnIndex(int32(i + 1))
				imgui.TextUnformatted(s)
			}
		}
		imgui.EndTable()
		disabledText("Select a grant to see its schedule and record its vests.")
	}
	if u.sel != 0 {
		t.grantPanel(acct)
	}
	t.addGrantForm(acct)
}

// loadGrant reads the schedule and drafts of the selected grant when the record has changed.
func (t *acctTab) loadGrant(g *model.Grant) {
	u, a := &t.eq, t.a
	if u.selRec == a.rec {
		return
	}
	u.selRec = a.rec
	_, u.rows, u.schedErr = ops.GrantSchedule(a.rec, g.ID)
	u.pct = make([]string, len(u.rows))
	for i, r := range u.rows {
		u.pct[i] = trimFloat(r.Percent)
	}
	u.draft = draftOfGrant(g)
	u.problems = [6]problemCache{}
}

// grantPanel draws the selected grant's schedule, the record vest form and the correct and void buttons.
func (t *acctTab) grantPanel(acct *model.Account) {
	u, a := &t.eq, t.a
	em := imgui.FontSize()
	g, _ := a.rec.Grant(u.sel)
	t.loadGrant(g)
	imgui.Spacing()
	imgui.SeparatorText(fmt.Sprintf("%s grant of %s", g.Symbol, g.Date))
	u.scrollHere("schedule")
	if u.schedErr != nil {
		errorText("Could not plan the vests: " + u.schedErr.Error())
		return
	}
	recorded := slices.ContainsFunc(u.rows, func(r ops.ScheduleRow) bool { return r.Vest != nil })
	if imgui.BeginTableV("##schedule", 10, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		for _, h := range []string{"#", "Date", "Percent", "Shares", "Status", "Price", "Proceeds", "Date diff", "Share diff", ""} {
			imgui.TableSetupColumnV(h, 0, 1, 0)
		}
		imgui.TableHeadersRow()
		for i, r := range u.rows {
			imgui.PushIDInt(int32(i))
			cells := scheduleCells(r)
			imgui.TableNextRowV(0, 0)
			for c, s := range cells[:2] {
				imgui.TableSetColumnIndex(int32(c))
				imgui.TextUnformatted(s)
			}
			imgui.TableSetColumnIndex(2)
			if recorded {
				imgui.TextUnformatted(trimFloat(r.Percent) + "%")
			} else {
				imgui.SetNextItemWidth(em * 5)
				imgui.InputTextWithHint("##pct", "", &u.pct[i], 0, nil)
			}
			for c, s := range cells[2:] {
				imgui.TableSetColumnIndex(int32(c + 3))
				imgui.TextUnformatted(s)
			}
			imgui.TableSetColumnIndex(9)
			if r.Vest == nil {
				if imgui.Button("Record") {
					u.vest, u.selVest = newVestForm(a.rec, g, r), 0
				}
			} else if imgui.Button("Open") {
				u.vest, u.selVest, u.vdraft = nil, r.Vest.ID, draftOfVest(*r.Vest)
			}
			imgui.PopID()
		}
		imgui.EndTable()
	}
	if recorded {
		disabledText("The percents are fixed while a vest is recorded; void the vests to change them.")
	} else {
		key := fmt.Sprintf("%p|%v", a.rec, u.pct)
		formButton("Save percents", u.problems[0].get(key, func() string { return percentsProblem(a.st, a.rec, g.ID, u.pct) }), false, func() {
			p, _ := parseVestPercents(u.pct)
			a.addEntry("change vest percents", func() (*ops.Result, error) {
				return ops.CorrectGrant(a.st, a.rec, g.ID, ops.GrantChange{VestPercents: p}, "", ops.Options{})
			})
		})
		imgui.SameLine()
		disabledText("Each planned vest's percent of the shares; they add up to 100.")
	}
	if u.vest != nil {
		t.vestForm(g)
	}
	if u.selVest != 0 {
		t.vestPanel(g)
	}
	t.grantCorrect(g)
}

// grantCorrect draws the correct form and the void button of the grant.
func (t *acctTab) grantCorrect(g *model.Grant) {
	u, a := &t.eq, t.a
	em := imgui.FontSize()
	d := &u.draft
	imgui.Spacing()
	imgui.SeparatorText("Correct or void the grant")
	t.label("Grant date")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##gdate", dateHint, &d.date, 0, nil)
	t.label("Shares")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##gshares", "", &d.shares, 0, nil)
	t.label("Grant price")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##gprice", "", &d.price, 0, nil)
	t.rateFields("g", &d.fed, &d.state, &d.fica)
	t.label("Note")
	imgui.SetNextItemWidth(em * 24)
	imgui.InputTextWithHint("##gnote", "why; kept in the history", &d.note, 0, nil)
	key := fmt.Sprintf("%p|%+v", a.rec, *d)
	t.label("")
	actionButton("Correct##grant", u.problems[1].get(key, func() string { return grantCorrectProblem(a.st, a.rec, g, *d) }), func() {
		ch, _ := grantChange(g, *d)
		a.addEntry("correct grant", func() (*ops.Result, error) {
			return ops.CorrectGrant(a.st, a.rec, g.ID, ch, d.note, ops.Options{})
		})
	})
	t.label("")
	actionButton("Void##grant", u.problems[2].get(key, func() string { return grantVoidProblem(a.st, a.rec, g.ID) }), func() {
		u.confirm.ask("Void grant?", fmt.Sprintf("Void the %s grant of %s (%d shares)? It leaves the record, along with its planned vests, and the account's balance no longer counts its shares. It has no recorded vests, so nothing else changes. It stays in the history.", g.Symbol, g.Date, g.Shares), func() {
			a.addEntry("void grant", func() (*ops.Result, error) {
				return ops.VoidGrant(a.st, a.rec, g.ID, d.note, ops.Options{})
			})
		})
	})
	u.confirm.draw()
}

// rateFields draws the three withholding rate fields, in percent. id keeps their widget ids apart from other forms'.
func (t *acctTab) rateFields(id string, fed, state, fica *string) {
	em := imgui.FontSize()
	for _, r := range []struct {
		label string
		to    *string
	}{{"Federal rate %", fed}, {"Virginia rate %", state}, {"FICA rate %", fica}} {
		t.label(r.label)
		imgui.SetNextItemWidth(em * 8)
		imgui.InputTextWithHint("##"+id+r.label, "", r.to, 0, nil)
	}
}

// addGrantForm draws the Add grant form with its live preview.
func (t *acctTab) addGrantForm(acct *model.Account) {
	u, a := &t.eq, t.a
	em := imgui.FontSize()
	f := &u.grant
	imgui.Spacing()
	if !u.section("Add grant", "addgrant") {
		return
	}
	t.label("Stock")
	imgui.SetNextItemWidth(em * 8)
	imgui.InputTextWithHint("##gsym", "ticker, e.g. ACME", &f.symbol, 0, nil)
	t.label("Enter")
	choice := 0
	if f.byShares {
		choice = 1
	}
	if k := pick("gbyshares", []string{"the dollar value", "the number of shares"}[choice], em*16, []string{"the dollar value", "the number of shares"}, choice); k >= 0 {
		f.byShares = k == 1
	}
	t.label(map[bool]string{false: "Value", true: "Shares"}[f.byShares])
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##gamount", map[bool]string{false: "dollars, e.g. 100000", true: "whole shares"}[f.byShares], &f.amount, 0, nil)
	t.label("Grant price")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##gpx", "per share, reference only", &f.price, 0, nil)
	t.label("Grant date")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##gdt", dateHint, &f.date, 0, nil)

	t.label("Vest calendar")
	names := make([]string, len(a.rec.Calendars))
	for i, c := range a.rec.Calendars {
		names[i] = c.Name
	}
	preview := "choose"
	if f.calendar != "" {
		preview = f.calendar
	}
	if k := pick("gcal", preview, em*16, names, slices.Index(names, f.calendar)); k >= 0 && names[k] != f.calendar {
		f.calendar, f.first = names[k], 0
	}
	t.label("First vest")
	choices := firstVestChoices(a.rec, f.calendar, f.date)
	labels := make([]string, len(choices))
	for i, d := range choices {
		labels[i] = d.String()
	}
	preview = "choose"
	if f.first != 0 {
		preview = f.first.String()
	}
	if k := pick("gfirst", preview, em*16, labels, slices.IndexFunc(choices, func(d model.Date) bool { return d == f.first })); k >= 0 {
		f.first = choices[k]
	}
	t.label("Yearly percents")
	imgui.SetNextItemWidth(em * 12)
	imgui.InputTextWithHint("##gyears", "e.g. 25/25/25/25", &f.years, 0, nil)
	t.label("Cliff months")
	imgui.SetNextItemWidth(em * 6)
	imgui.InputTextWithHint("##gcliff", "optional", &f.cliff, 0, nil)
	t.label("Deposit account")
	var brokers []*model.Account
	for _, b := range a.rec.Accounts {
		if b.Type == model.TypeBrokerage && (!b.IsClosed() || b.ID == f.brokerage) {
			brokers = append(brokers, b)
		}
	}
	labels = make([]string, len(brokers))
	for i, b := range brokers {
		labels[i] = b.Name
	}
	preview = "choose"
	if f.brokerage != "" {
		preview = counterName(a.rec, f.brokerage)
	}
	if k := pick("gbroker", preview, em*16, labels, slices.IndexFunc(brokers, func(b *model.Account) bool { return b.ID == f.brokerage })); k >= 0 {
		f.brokerage = brokers[k].ID
	}
	t.rateFields("n", &f.fed, &f.state, &f.fica)

	pv := previewGrant(a.rec, *f)
	t.grantPreview(pv)
	t.label("")
	formButton("Add grant", grantProblem(a.st, a.rec, acct.ID, *f), f.untouched(), func() {
		in, _ := f.input(acct.ID)
		var added int64
		if a.addEntry("add grant", func() (*ops.Result, error) {
			res, err := ops.AddGrant(a.st, a.rec, in, ops.Options{})
			if err == nil {
				added = res.Grant.ID
			}
			return res, err
		}) {
			*f = newGrantForm(acct)
			u.sel, u.selRec, u.vest, u.selVest = added, nil, nil, 0
		}
	})
}

// grantPreview draws the shares the form derives and the schedule it generates.
func (t *acctTab) grantPreview(pv grantPreview) {
	imgui.Spacing()
	if pv.HasShares {
		imgui.TextUnformatted(fmt.Sprintf("Shares: %d, at a grant price of %s", pv.Shares, formatDollars(pv.Price)))
	} else {
		disabledText("Shares: enter the value or shares and the grant price.")
	}
	if pv.PlanErr != "" {
		errorText("No schedule yet: " + pv.PlanErr)
		return
	}
	if len(pv.Plan) == 0 {
		disabledText("Schedule: pick the calendar and first vest and enter the yearly percents.")
		return
	}
	if imgui.BeginTableV("##grantpreview", 4, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		for _, h := range []string{"#", "Date", "Percent", "Shares"} {
			imgui.TableSetupColumnV(h, 0, 1, 0)
		}
		imgui.TableHeadersRow()
		for _, p := range pv.Plan {
			imgui.TableNextRowV(0, 0)
			for c, s := range []string{strconv.Itoa(p.Index), p.Date.String(), trimFloat(p.Percent) + "%", planShares(pv, p)} {
				imgui.TableSetColumnIndex(int32(c))
				imgui.TextUnformatted(s)
			}
		}
		imgui.EndTable()
	}
}

// planShares is the shares of a previewed vest, or "-" while the grant's shares are not known.
func planShares(pv grantPreview, p model.PlannedVest) string {
	if !pv.HasShares {
		return "-"
	}
	return strconv.Itoa(p.Shares)
}

// confirmDialog is the modal that asks before a void that changes more than its own fact.
type confirmDialog struct {
	title, text string
	do          func()
	opening     bool
}

// ask opens the dialog; do runs if the user confirms.
func (d *confirmDialog) ask(title, text string, do func()) {
	d.title, d.text, d.do, d.opening = title, text, do, true
}

// draw draws the modal when open.
func (d *confirmDialog) draw() {
	if d.opening {
		imgui.OpenPopupStrV(d.title, 0)
		d.opening = false
	}
	if !imgui.BeginPopupModalV(d.title, nil, imgui.WindowFlagsAlwaysAutoResize) {
		return
	}
	imgui.PushTextWrapPos()
	imgui.TextUnformatted(d.text)
	imgui.PopTextWrapPos()
	imgui.Spacing()
	if imgui.Button("Void") {
		imgui.CloseCurrentPopup()
		d.do()
	}
	imgui.SameLine()
	if imgui.Button("Cancel") {
		imgui.CloseCurrentPopup()
	}
	imgui.EndPopup()
}
