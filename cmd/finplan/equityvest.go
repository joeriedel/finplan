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

// tieWindowDays is how far from a vest's date a deposit can be and still be offered to tie the vest to.
const tieWindowDays = 14

// vestForm is the Record vest form of one planned vest: every field is the text or choice as entered. The rates are percentages.
type vestForm struct {
	grant               int64
	planned             int
	date, shares, price string
	fed, state, fica    string
	// tie says the vest is tied to the existing deposit tx rather than recording one.
	tie bool
	tx  int64
}

// newVestForm prefills the form from the planned vest r of the grant g: its date and shares, the stock's latest price on that date and the grant's rates.
func newVestForm(rec *model.Record, g *model.Grant, r ops.ScheduleRow) *vestForm {
	f := &vestForm{grant: g.ID, planned: r.Index, date: r.Date.String(), shares: strconv.Itoa(r.Shares)}
	if px, ok := rec.Prices.At(g.Symbol, r.Date); ok {
		f.price = trimFloat(px)
	}
	f.fed, f.state, f.fica = ratesText(g.Rates)
	return f
}

// readVest reads the date, shares, price and rates of a vest as typed.
func readVest(date, shares, price, fed, state, fica string) (model.RecordedVest, error) {
	var v model.RecordedVest
	var err error
	if v.Date, err = parseDate(strings.TrimSpace(date)); err != nil {
		return v, err
	}
	if v.Shares, err = parseCount("the shares", shares, 1); err != nil {
		return v, err
	}
	if v.Price, err = parseValue(model.PropAmount, model.ModeSet, price); err != nil || v.Price <= 0 {
		return v, errors.New("enter the stock's price on the vest date, a positive number")
	}
	v.Rates, err = parseRates(fed, state, fica)
	return v, err
}

// vestFigures are the value of a vest, V, what is withheld, W, and the proceeds, N = V - W.
type vestFigures struct{ V, W, N float64 }

// figuresOf is the value, withholding and net of a recorded vest.
func figuresOf(v model.RecordedVest) vestFigures {
	return vestFigures{v.Gross(), v.WithheldAmount(), v.Net()}
}

// figures reads the form's vest figures.
func (f vestForm) figures() (vestFigures, error) {
	v, err := readVest(f.date, f.shares, f.price, f.fed, f.state, f.fica)
	return figuresOf(v), err
}

// input reads the form as the vest to record.
func (f vestForm) input() (ops.VestInput, error) {
	v, err := readVest(f.date, f.shares, f.price, f.fed, f.state, f.fica)
	if err != nil {
		return ops.VestInput{}, err
	}
	in := ops.VestInput{Grant: f.grant, Planned: f.planned, Date: &v.Date, Shares: &v.Shares, Price: v.Price, Rates: &v.Rates}
	if f.tie {
		if f.tx == 0 {
			return in, errors.New("pick the deposit to tie the vest to")
		}
		in.TxEntry = f.tx
	}
	return in, nil
}

// vestProblem says why the vest the form describes cannot be recorded, or "" if it can.
func vestProblem(st *store.Store, rec *model.Record, f vestForm) string {
	in, err := f.input()
	if err != nil {
		return err.Error()
	}
	_, err = ops.RecordVest(st, rec, in, ops.Options{DryRun: true})
	return reasonOf(err)
}

// unmatchedDeposits lists the brokerage account's entries a vest dated date can be tied to: transactions that put money into it, that no vest is already tied to, within tieWindowDays of the date, the nearest first.
func unmatchedDeposits(st *store.Store, rec *model.Record, brokerage model.AccountID, date model.Date) ([]store.Entry, error) {
	es, err := ops.ListEntries(st, brokerage, false)
	if err != nil {
		return nil, err
	}
	tied := map[int64]bool{}
	for _, a := range rec.Accounts {
		for _, v := range a.Vests {
			tied[v.TxEntry] = true
		}
	}
	dist := func(e store.Entry) int { return max(int(e.Date-date), int(date-e.Date)) }
	var out []store.Entry
	for _, e := range es {
		if e.Kind == store.KindTransaction && e.Amount > 0 && !tied[e.ID] && dist(e) <= tieWindowDays {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b store.Entry) int {
		if d := dist(a) - dist(b); d != 0 {
			return d
		}
		return int(a.ID - b.ID)
	})
	return out, nil
}

// depositLabel describes a deposit for the tie picker.
func depositLabel(rec *model.Record, e store.Entry) string {
	s := e.Date.String() + "  " + formatDollars(e.Amount)
	if c := counterName(rec, e.Counter); c != "" {
		s += "  from " + c
	}
	if e.Memo != "" {
		s += "  " + e.Memo
	}
	return s
}

// vestDraft is the text of the correct form of a recorded vest. The rates are percentages.
type vestDraft struct {
	date, shares, price string
	fed, state, fica    string
	note                string
}

// draftOfVest is the text of the correct form of a recorded vest.
func draftOfVest(v model.RecordedVest) vestDraft {
	fed, state, fica := ratesText(v.Rates)
	return vestDraft{date: v.Date.String(), shares: strconv.Itoa(v.Shares), price: trimFloat(v.Price), fed: fed, state: state, fica: fica}
}

// vestChange is the change that turns the vest into what the draft holds: only the values that differ.
func vestChange(v model.RecordedVest, d vestDraft) (ops.VestChange, error) {
	var ch ops.VestChange
	n, err := readVest(d.date, d.shares, d.price, d.fed, d.state, d.fica)
	if err != nil {
		return ch, err
	}
	if n.Date != v.Date {
		ch.Date = &n.Date
	}
	if n.Shares != v.Shares {
		ch.Shares = &n.Shares
	}
	if n.Price != v.Price {
		ch.Price = &n.Price
	}
	if n.Rates != v.Rates {
		ch.Rates = &n.Rates
	}
	return ch, nil
}

// vestCorrectProblem says why the vest cannot be corrected to what the draft holds, or "" if it can.
func vestCorrectProblem(st *store.Store, rec *model.Record, v model.RecordedVest, d vestDraft) string {
	ch, err := vestChange(v, d)
	if err != nil {
		return err.Error()
	}
	_, err = ops.CorrectVest(st, rec, v.ID, ch, d.note, ops.Options{DryRun: true})
	return noChangeReason(err)
}

// vestVoidProblem says why the vest cannot be voided, or "" if it can.
func vestVoidProblem(st *store.Store, rec *model.Record, id int64) string {
	_, err := ops.VoidVest(st, rec, id, "", ops.Options{DryRun: true})
	return reasonOf(err)
}

// drawFigures shows V, W and N.
func drawFigures(fg vestFigures, err error) {
	if err != nil {
		imgui.TextDisabled("Value, withheld and net: fill in the fields above.")
		return
	}
	imgui.TextUnformatted(fmt.Sprintf("Value V: %s    Withheld W: %s    Net N: %s", formatDollars(fg.V), formatDollars(fg.W), formatDollars(fg.N)))
}

// vestForm draws the Record vest form of the selected grant.
func (t *acctTab) vestForm(g *model.Grant) {
	u, a := &t.eq, t.a
	em := imgui.FontSize()
	f := u.vest
	imgui.Spacing()
	imgui.SeparatorText(fmt.Sprintf("Record vest %d", f.planned))
	u.scrollHere("vest")
	t.label("Date")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##vdate", dateHint, &f.date, 0, nil)
	t.label("Shares")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##vshares", "", &f.shares, 0, nil)
	t.label("Price")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##vprice", "on the vest date", &f.price, 0, nil)
	t.rateFields("v", &f.fed, &f.state, &f.fica)
	t.label("")
	fg, err := f.figures()
	drawFigures(fg, err)

	t.label("Deposit")
	broker := counterName(a.rec, g.Brokerage)
	modes := []string{"Record the net proceeds as a deposit into " + broker, "Tie to a deposit already in " + broker}
	sel := 0
	if f.tie {
		sel = 1
	}
	if k := pick("vtie", modes[sel], em*26, modes, sel); k >= 0 {
		f.tie, f.tx = k == 1, 0
	}
	if f.tie {
		t.label("Existing deposit")
		t.depositPicker(g, f)
	}
	t.label("")
	formButton("Record vest", u.problems[3].get(fmt.Sprintf("%p|%+v", a.rec, *f), func() string { return vestProblem(a.st, a.rec, *f) }), false, func() {
		in, _ := f.input()
		if a.recordVest("record vest", func() (*ops.Result, error) {
			return ops.RecordVest(a.st, a.rec, in, ops.Options{})
		}) {
			u.vest = nil
		}
	})
	imgui.SameLine()
	if imgui.Button("Cancel##vest") {
		u.vest = nil
	}
}

// depositPicker draws the picker of the unmatched deposits near the vest's date, for the tie.
func (t *acctTab) depositPicker(g *model.Grant, f *vestForm) {
	u, a := &t.eq, t.a
	key := fmt.Sprintf("%p|%s|%s", a.rec, g.Brokerage, f.date)
	if u.depKey != key {
		u.depKey, u.deposits, u.depErr = key, nil, nil
		if d, err := parseDate(strings.TrimSpace(f.date)); err == nil {
			u.deposits, u.depErr = unmatchedDeposits(a.st, a.rec, g.Brokerage, d)
		}
	}
	if u.depErr != nil {
		errorText("Could not read the deposits: " + u.depErr.Error())
		return
	}
	labels := make([]string, len(u.deposits))
	sel := -1
	for i, e := range u.deposits {
		labels[i] = depositLabel(a.rec, e)
		if e.ID == f.tx {
			sel = i
		}
	}
	if sel < 0 {
		f.tx = 0
	}
	if len(labels) == 0 {
		imgui.TextDisabled(fmt.Sprintf("No deposit of %s is within %d days of the vest date that no vest is tied to.", counterName(a.rec, g.Brokerage), tieWindowDays))
		return
	}
	preview := "choose"
	if sel >= 0 {
		preview = labels[sel]
	}
	if k := pick("vdeposit", preview, imgui.FontSize()*30, labels, sel); k >= 0 {
		f.tx = u.deposits[k].ID
	}
}

// vestPanel draws the correct form and the void button of the selected recorded vest.
func (t *acctTab) vestPanel(g *model.Grant) {
	u, a := &t.eq, t.a
	em := imgui.FontSize()
	v, _ := a.rec.Vest(u.selVest)
	if v == nil || v.Grant != g.ID {
		u.selVest = 0
		return
	}
	d := &u.vdraft
	imgui.Spacing()
	imgui.SeparatorText(fmt.Sprintf("Vest %d, %s", v.Planned, v.Date))
	t.label("Date")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##cvdate", dateHint, &d.date, 0, nil)
	t.label("Shares")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##cvshares", "", &d.shares, 0, nil)
	t.label("Price")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##cvprice", "", &d.price, 0, nil)
	t.rateFields("cv", &d.fed, &d.state, &d.fica)
	t.label("")
	cur, err := readVest(d.date, d.shares, d.price, d.fed, d.state, d.fica)
	if v.Withheld != nil && cur.Rates == v.Rates {
		cur.Withheld = v.Withheld
	}
	drawFigures(figuresOf(cur), err)
	t.label("Deposit")
	imgui.TextUnformatted(fmt.Sprintf("entry %d of %s", v.TxEntry, counterName(a.rec, g.Brokerage)))
	t.label("Note")
	imgui.SetNextItemWidth(em * 24)
	imgui.InputTextWithHint("##cvnote", "why; kept in the history", &d.note, 0, nil)
	key := fmt.Sprintf("%p|%+v", a.rec, *d)
	t.label("")
	actionButton("Correct##vest", u.problems[4].get(key, func() string { return vestCorrectProblem(a.st, a.rec, *v, *d) }), func() {
		ch, _ := vestChange(*v, *d)
		a.addEntry("correct vest", func() (*ops.Result, error) {
			return ops.CorrectVest(a.st, a.rec, v.ID, ch, d.note, ops.Options{})
		})
	})
	t.label("")
	actionButton("Void##vest", u.problems[5].get(key, func() string { return vestVoidProblem(a.st, a.rec, v.ID) }), func() {
		id := v.ID
		u.confirm.ask("Void vest?", fmt.Sprintf("Void vest %d of %s, %d shares on %s? It leaves the record and its shares count as unvested again. Its deposit (entry %d of %s) and the price observation stay in the record: void those separately, on the account's entries and in the Prices section. It stays in the history.", v.Planned, g.Symbol, v.Shares, v.Date, v.TxEntry, counterName(a.rec, g.Brokerage)), func() {
			if a.addEntry("void vest", func() (*ops.Result, error) {
				return ops.VoidVest(a.st, a.rec, id, d.note, ops.Options{})
			}) {
				u.selVest = 0
			}
		})
	})
}
