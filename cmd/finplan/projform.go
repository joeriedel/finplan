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
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// propEndsOn is the property a budget item modification has in this tab when it ends the item: an amount of zero from a date, which ops.EndItem stores.
const propEndsOn = "ends on"

// recurrenceChoice is a repeat option of the item forms: the name the picker shows and what it means in plain words.
type recurrenceChoice struct {
	kind model.RecurrenceKind
	name string
	hint string
}

var (
	formItemKinds   = []model.ItemKind{model.ItemIncome, model.ItemExpense, model.ItemTransfer}
	formRecurrences = []recurrenceChoice{
		{model.Weekly, "Weekly", "every week"},
		{model.Biweekly, "Biweekly", "every 2 weeks"},
		{model.Semimonthly, "Semimonthly", "twice a month, on two days you choose, e.g. the 1st and 15th"},
		{model.Monthly, "Monthly", "once a month; a day from 29 to 31 falls on the last day of a shorter month"},
		{model.Quarterly, "Quarterly", "every 3 months"},
		{model.Annual, "Annually", "once a year, on a month and day"},
		{model.OneTime, "One-time", "once, on one date"},
		{model.Custom, "Custom", "every N days, weeks, months or years"},
	}
	formUnits = []model.RecurrenceUnit{model.UnitDays, model.UnitWeeks, model.UnitMonths, model.UnitYears}
)

// recurrenceChoiceOf is the form's choice for the kind; the zero choice if there is none.
func recurrenceChoiceOf(k model.RecurrenceKind) recurrenceChoice {
	for _, c := range formRecurrences {
		if c.kind == k {
			return c
		}
	}
	return recurrenceChoice{}
}

// outsideAccount is the account of a transfer that stands for everything outside the record.
const outsideAccount model.AccountID = "outside"

// reasonOf says in plain words why ops refused; "" for no error. The refusals of ops carry sentences of their own, except the ones that name no more than their kind.
func reasonOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ops.ErrNoObject):
		return "pick an object"
	case errors.Is(err, ops.ErrUnknownProjection):
		return "the projection no longer exists"
	}
	return err.Error()
}

// modProblem says why the modification the Add form describes cannot be added to the projection, or "" if it can. What the form cannot parse comes first; the rest is what ops.AddMod and ops.EndItem refuse.
func modProblem(rec *model.Record, proj string, kind model.ObjectKind, obj, prop string, mode model.Mode, value, start, end string) string {
	if kind != model.KindGlobal && obj == "" {
		return "pick an object"
	}
	if prop == "" {
		return "pick a property"
	}
	m, err := newMod(kind, obj, prop, mode, value, start, end)
	if err != nil {
		return err.Error()
	}
	return reasonOf(ops.CheckMod(rec, proj, m))
}

// itemForm is a budget item as the user edits it: every field is the text or choice as entered. For a one-time or biweekly item date is the day it happens or starts; for any other it is the optional day it starts from, today if empty. amountSched and growthSched say that the amount or growth changes over time, so that amount and growth show its value today and are not to be edited as plain numbers.
type itemForm struct {
	kind                  model.ItemKind
	name, amount          string
	growth                string
	every                 model.RecurrenceKind
	date, day, month, end string
	day2, count           string
	unit                  model.RecurrenceUnit
	account, from, to     model.AccountID
	indexed               bool
	// income is the income type as stored: empty for an income item that has none, which is wages.
	income model.IncomeType
	// matchPercent and matchUpTo are the texts of the employer match of a wages income item, and matchAccount the account it goes into.
	matchPercent, matchUpTo string
	matchAccount            model.AccountID
	amountSched             bool
	growthSched             bool
}

// isPlain reports whether the schedule is a single number that always holds, or empty: what a plain number field can show and write without losing anything.
func isPlain(s model.Schedule) bool {
	if len(s.Segments()) == 0 {
		return true
	}
	_, from, ok := constantOf(s)
	return ok && from == nil
}

func newItemForm() itemForm {
	return itemForm{kind: model.ItemExpense, every: model.OneTime}
}

// valueNow is the value the schedule has on today, or its first value if it has none then; 0 for an empty schedule. The forms show it for an item whose amount or growth changes over time.
func valueNow(s model.Schedule, today model.Date) float64 {
	if v, ok := s.At(today); ok {
		return v
	}
	if segs := s.Segments(); len(segs) > 0 {
		return segs[0].Value
	}
	return 0
}

// formOf returns the form that shows the item as it is on today.
func formOf(it model.BudgetItem, today model.Date) itemForm {
	r := it.Recurrence
	f := itemForm{kind: it.Kind, name: it.Name, amount: trimFloat(valueNow(it.Amount, today)), every: r.Kind, unit: r.Unit, account: it.Account, from: it.From, to: it.To, indexed: it.Indexed, income: it.IncomeType, matchAccount: it.MatchAccount, amountSched: !isPlain(it.Amount), growthSched: !isPlain(it.Growth)}
	switch {
	case r.Kind == model.OneTime || r.Kind == model.Biweekly:
		f.date = r.Anchor.String()
	case it.Start != nil:
		f.date = it.Start.String()
	}
	if it.MatchPercent != 0 {
		f.matchPercent = trimFloat(it.MatchPercent)
	}
	if it.MatchUpTo != 0 {
		f.matchUpTo = trimFloat(it.MatchUpTo)
	}
	if g := valueNow(it.Growth, today); g != 0 {
		f.growth = formatValue(model.PropGrowth, model.ModeSet, g)
	}
	if r.Day != 0 {
		f.day = strconv.Itoa(r.Day)
	}
	if r.Day2 != 0 {
		f.day2 = strconv.Itoa(r.Day2)
	}
	if r.Count != 0 {
		f.count = strconv.Itoa(r.Count)
	}
	if r.Month != 0 {
		f.month = strconv.Itoa(int(r.Month))
	}
	if it.End != nil {
		f.end = it.End.String()
	}
	return f
}

// set changes one field: kind, name, amount, growth, every, date, day, day2, month, count, unit, end, account, from, to, income (an income type) or indexed (text "true" or "false").
func (f *itemForm) set(field, text string) {
	switch field {
	case "kind":
		f.setKind(model.ItemKind(text))
	case "indexed":
		f.indexed = text == "true"
	case "income":
		f.income = model.IncomeType(text)
	case "matchpercent":
		f.matchPercent = text
	case "matchupto":
		f.matchUpTo = text
	case "matchaccount":
		f.matchAccount = model.AccountID(text)
	case "name":
		f.name = text
	case "amount":
		f.amount = text
	case "growth":
		f.growth = text
	case "every":
		f.every = model.RecurrenceKind(text)
		f.seed()
	case "date":
		f.date = text
	case "day":
		f.day = text
	case "day2":
		f.day2 = text
	case "month":
		f.month = text
	case "count":
		f.count = text
	case "unit":
		f.unit = model.RecurrenceUnit(text)
	case "end":
		f.end = text
	case "account":
		f.account = model.AccountID(text)
	case "from":
		f.from = model.AccountID(text)
	case "to":
		f.to = model.AccountID(text)
	}
}

// setKind switches the kind and carries the accounts over so that the form stays complete: an income or expense takes the account of a transfer that is in the record, and a transfer takes the account of an income or expense on the side the money leaves it, with the outside on the other.
func (f *itemForm) setKind(kind model.ItemKind) {
	was := f.kind
	f.kind = kind
	switch kind {
	case model.ItemIncome, model.ItemExpense:
		if f.account == "" {
			f.account = f.to
			if f.account == "" || f.account == outsideAccount {
				f.account = f.from
			}
			if f.account == outsideAccount {
				f.account = ""
			}
		}
	case model.ItemTransfer:
		if f.from == "" && f.to == "" && f.account != "" {
			f.from, f.to = f.account, outsideAccount
			if was == model.ItemIncome {
				f.from, f.to = outsideAccount, f.account
			}
		}
	}
}

// seed fills what the recurrence needs, so that switching to it yields a valid item: the day of a monthly, semimonthly or annual item comes from the date the form has (the 1st with no date), an annual item also gets its month (that of the date, else January) and a day that exists in it, a semimonthly item gets two different days (the 1st and the 15th, keeping a day that is set), and a custom item gets 2 weeks.
func (f *itemForm) seed() {
	d, err := parseDate(strings.TrimSpace(f.date))
	switch f.every {
	case model.Monthly, model.Annual:
		if strings.TrimSpace(f.day) == "" {
			f.day = "1"
			if err == nil {
				_, _, day := d.YMD()
				f.day = strconv.Itoa(day)
			}
		}
		if f.every == model.Annual {
			if strings.TrimSpace(f.month) == "" {
				f.month = "1"
				if err == nil {
					_, m, _ := d.YMD()
					f.month = strconv.Itoa(int(m))
				}
			}
			m, merr := strconv.Atoi(strings.TrimSpace(f.month))
			day, derr := strconv.Atoi(strings.TrimSpace(f.day))
			if merr == nil && derr == nil && m >= 1 && m <= 12 {
				if last := time.Date(2024, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day(); day > last {
					f.day = strconv.Itoa(last)
				}
			}
		}
	case model.Semimonthly:
		day, day2 := strings.TrimSpace(f.day), strings.TrimSpace(f.day2)
		if day == "" {
			f.day = "1"
			if day2 == "1" {
				f.day = "15"
			}
		}
		if day2 == "" || day2 == strings.TrimSpace(f.day) {
			f.day2 = "15"
			if strings.TrimSpace(f.day) == "15" {
				f.day2 = "1"
			}
		}
	case model.Custom:
		if f.unit == "" {
			f.unit = model.UnitWeeks
		}
		if strings.TrimSpace(f.count) == "" {
			f.count = "2"
		}
	}
}

// item builds the budget item the form describes; an item that is not one-time or biweekly and has no date starts today. It fails on what it cannot read and on a missing account; ops checks the rest.
func (f itemForm) item(today model.Date) (model.BudgetItem, error) {
	it := model.BudgetItem{Name: f.name, Kind: f.kind, Account: f.account, From: f.from, To: f.to, Indexed: f.indexed, IncomeType: f.income}
	if strings.TrimSpace(f.amount) == "" {
		return it, errors.New("enter an amount")
	}
	amount, err := parseValue(model.PropAmount, model.ModeSet, f.amount)
	if err != nil {
		return it, err
	}
	it.Amount = model.Constant(amount)
	if strings.TrimSpace(f.growth) != "" {
		g, err := parseValue(model.PropGrowth, model.ModeSet, f.growth)
		if err != nil {
			return it, err
		}
		if g != 0 {
			it.Growth = model.Constant(g)
		}
	}
	switch f.kind {
	case model.ItemIncome, model.ItemExpense:
		if f.account == "" {
			return it, errors.New("pick an account")
		}
	case model.ItemTransfer:
		if f.from == "" || f.to == "" {
			return it, errors.New("pick the accounts the money moves from and to")
		}
	}
	if f.kind == model.ItemIncome && (f.income == "" || f.income == model.IncomeWages) {
		if it.MatchPercent, err = optPercent(f.matchPercent, "the match percent"); err != nil {
			return it, err
		}
		if it.MatchUpTo, err = optPercent(f.matchUpTo, "the percent of pay the match counts up to"); err != nil {
			return it, err
		}
		it.MatchAccount = f.matchAccount
	}
	r := model.Recurrence{Kind: f.every}
	switch f.every {
	case model.OneTime, model.Biweekly:
		if r.Anchor, err = needDate(f.date, "the date"); err != nil {
			return it, err
		}
	case model.Monthly:
		if r.Day, err = needInt(f.day, "the day of the month"); err != nil {
			return it, err
		}
	case model.Semimonthly:
		if r.Day, err = needInt(f.day, "the first day of the month"); err != nil {
			return it, err
		}
		if r.Day2, err = needInt(f.day2, "the second day of the month"); err != nil {
			return it, err
		}
	case model.Custom:
		if r.Count, err = needInt(f.count, "how many"); err != nil {
			return it, err
		}
		if !f.unit.Valid() {
			return it, errors.New("pick days, weeks, months or years")
		}
		r.Unit = f.unit
	case model.Annual:
		m, err := needInt(f.month, "the month")
		if err != nil {
			return it, err
		}
		r.Month = time.Month(m)
		if r.Day, err = needInt(f.day, "the day of the month"); err != nil {
			return it, err
		}
	}
	it.Recurrence = r
	if f.every != model.OneTime && f.every != model.Biweekly {
		d := today
		if strings.TrimSpace(f.date) != "" {
			if d, err = parseDate(strings.TrimSpace(f.date)); err != nil {
				return it, err
			}
		}
		it.Start = &d
	}
	if s := strings.TrimSpace(f.end); s != "" {
		d, err := parseDate(s)
		if err != nil {
			return it, err
		}
		it.End = &d
	}
	return it, nil
}

// optPercent reads a percent as typed, with or without a percent sign; a blank is 0.
func optPercent(s, what string) (float64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("enter %s as a number such as 50", what)
	}
	return v, nil
}

func needDate(s, what string) (model.Date, error) {
	if strings.TrimSpace(s) == "" {
		return 0, fmt.Errorf("enter %s as %s", what, dateHint)
	}
	return parseDate(strings.TrimSpace(s))
}

func needInt(s, what string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("enter %s as a whole number", what)
	}
	return n, nil
}

// itemProblem says why the budget item the form describes cannot be added, or "" if it can.
func itemProblem(rec *model.Record, f itemForm) string {
	it, err := f.item(rec.Today)
	if err != nil {
		return err.Error()
	}
	return reasonOf(ops.CheckItem(rec, it))
}

// keepUnedited makes it, built from the form f after editing field, keep what the form cannot show of the item old: an amount or growth that changes over time, a start that is absent, the anchor of a weekly, quarterly or custom item, and the later start of a biweekly item. Editing the date of a biweekly item moves its anchor and start.
func keepUnedited(it *model.BudgetItem, old model.BudgetItem, f itemForm, field string) {
	if field != "amount" {
		it.Amount = old.Amount
	}
	if field != "growth" {
		it.Growth = old.Growth
	}
	if k := old.Recurrence.Kind; k == model.Weekly || k == model.Quarterly || k == model.Custom {
		if f.every == k && field != "every" && field != "date" {
			it.Recurrence.Anchor = old.Recurrence.Anchor
		}
	}
	switch {
	case old.Recurrence.Kind == model.Biweekly && f.every == model.Biweekly && field != "date":
		it.Start = old.Start
	case old.Start == nil && f.every == old.Recurrence.Kind && field != "every" && field != "date":
		it.Start = nil
	case old.Start == nil && strings.TrimSpace(f.date) == "" && (f.every == model.Monthly || f.every == model.Semimonthly || f.every == model.Annual):
		it.Start = nil
	}
}

// itemFieldID is the id of a text field of the budget item added by modification i.
func itemFieldID(i int, field string) string { return "item" + strconv.Itoa(i) + field }

// setItemField changes one field of the budget item that modification i of the projection adds, through ops.EditItem.
func (t *projTab) setItemField(proj string, i int, field, text string) {
	id := itemFieldID(i, field)
	old := t.a.rec.Projection(proj).Mods[i].Item
	f := formOf(*old, t.a.rec.Today)
	f.set(field, text)
	if field == "every" && (f.every == model.OneTime || f.every == model.Biweekly) && strings.TrimSpace(f.date) == "" {
		f.date = t.a.rec.Today.String()
	}
	it, err := f.item(t.a.rec.Today)
	if err != nil {
		t.setErr(id, err.Error())
		return
	}
	it.Indexed = old.Indexed
	keepUnedited(&it, *old, f, field)
	t.a.edit("change budget item", func() (*ops.Result, error) {
		return ops.EditItem(t.a.st, t.a.rec, proj, old.ID, it, ops.Options{})
	})
}

// addNewItem adds the budget item the Add form describes to the projection and clears the form. It reports whether it did.
func (t *projTab) addNewItem(proj string) bool {
	if itemProblem(effective(t.a.rec, proj), t.addItem) != "" {
		return false
	}
	it, _ := t.addItem.item(t.a.rec.Today)
	if !t.a.edit("add budget item", func() (*ops.Result, error) {
		return ops.AddItem(t.a.st, t.a.rec, proj, it, ops.Options{})
	}) {
		return false
	}
	t.addItem = itemForm{kind: t.addItem.kind, every: t.addItem.every, unit: t.addItem.unit}
	return true
}

// setModTarget changes the object and property of modification i of the projection, keeping its values, through ops.SetMod. A property of propEndsOn makes the modification what ops.EndItem stores, from the date its first segment starts or else the record's today; leaving it makes an amount of zero from the beginning.
func (t *projTab) setModTarget(proj string, i int, kind model.ObjectKind, obj, prop string) {
	rec := t.a.rec
	m := rec.Projection(proj).Mods[i]
	endDate, ended := ops.EndedOn(m)
	var from *model.Date
	if segs := m.Values.Segments(); len(segs) > 0 && segs[0].From != model.MinDate {
		from = model.Ptr(segs[0].From)
	}
	m.Target = model.ObjectRef{Kind: kind, ID: obj}
	if prop == propEndsOn {
		d := rec.Today
		switch {
		case ended:
			d = endDate
		case from != nil:
			d = *from
		}
		m = ops.EndMod(obj, d)
	} else {
		m.Property = prop
		if ended {
			m.Values = model.Constant(0)
		}
	}
	t.a.edit("change modification", func() (*ops.Result, error) {
		return ops.SetMod(t.a.st, t.a.rec, proj, i, m, ops.Options{})
	})
}

// rowProperty is the property a modification shows in the list.
func rowProperty(m model.Modification) string {
	if _, ok := ops.EndedOn(m); ok {
		return propEndsOn
	}
	return m.Property
}

// itemName names the budget item, one of the record's or one any projection adds; the id if there is none.
func itemName(rec *model.Record, id string) string {
	if i := slices.IndexFunc(rec.Budget.Items, func(b model.BudgetItem) bool { return b.ID == id }); i >= 0 {
		return rec.Budget.Items[i].Name
	}
	for _, p := range rec.Projections {
		for _, m := range p.Mods {
			if m.Property == model.PropAddItem && m.Item != nil && m.Item.ID == id {
				return m.Item.Name
			}
		}
	}
	return id
}
