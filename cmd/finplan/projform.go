// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// propEndsOn is the property a budget item modification has in this tab when it ends the item: an amount of zero from a date, which ops.EndItem stores.
const propEndsOn = "ends on"

var (
	formItemKinds   = []model.ItemKind{model.ItemIncome, model.ItemExpense, model.ItemTransfer}
	formRecurrences = []model.RecurrenceKind{model.OneTime, model.Monthly, model.Biweekly, model.Annual}
)

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
func modProblem(rec *model.Record, proj string, kind model.ObjectKind, obj, prop string, mode model.Mode, value, start string) string {
	if kind != model.KindGlobal && obj == "" {
		return "pick an object"
	}
	if prop == "" {
		return "pick a property"
	}
	m, err := newMod(kind, obj, prop, mode, value, start)
	if err != nil {
		return err.Error()
	}
	return reasonOf(ops.CheckMod(rec, proj, m))
}

// itemForm is a budget item as the user edits it: every field is the text or choice as entered. For a one-time or biweekly item date is the day it happens or starts; for a monthly or annual item it is the optional day it starts from.
type itemForm struct {
	kind                  model.ItemKind
	name, amount          string
	growth                string
	every                 model.RecurrenceKind
	date, day, month, end string
	account, from, to     model.AccountID
}

func newItemForm() itemForm {
	return itemForm{kind: model.ItemExpense, every: model.OneTime}
}

// formOf returns the form that shows the item.
func formOf(it model.BudgetItem) itemForm {
	r := it.Recurrence
	f := itemForm{kind: it.Kind, name: it.Name, amount: trimFloat(it.Amount), every: r.Kind, account: it.Account, from: it.From, to: it.To}
	switch {
	case r.Kind == model.OneTime || r.Kind == model.Biweekly:
		f.date = r.Anchor.String()
	case it.Start != nil:
		f.date = it.Start.String()
	}
	if it.Growth != 0 {
		f.growth = formatValue(model.PropGrowth, model.ModeSet, it.Growth)
	}
	if r.Day != 0 {
		f.day = strconv.Itoa(r.Day)
	}
	if r.Month != 0 {
		f.month = strconv.Itoa(int(r.Month))
	}
	if it.End != nil {
		f.end = it.End.String()
	}
	return f
}

// set changes one field: kind, name, amount, growth, every, date, day, month, end, account, from or to.
func (f *itemForm) set(field, text string) {
	switch field {
	case "kind":
		f.setKind(model.ItemKind(text))
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
	case "month":
		f.month = text
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

// seed fills the day and month a monthly or annual item needs from the date the form has, so that switching to such a schedule does not start from nothing. An annual item with no date to take the month from gets January.
func (f *itemForm) seed() {
	if f.every != model.Monthly && f.every != model.Annual {
		return
	}
	d, err := parseDate(strings.TrimSpace(f.date))
	if err == nil && strings.TrimSpace(f.day) == "" {
		_, _, day := d.YMD()
		f.day = strconv.Itoa(day)
	}
	if f.every == model.Annual && strings.TrimSpace(f.month) == "" {
		f.month = "1"
		if err == nil {
			_, m, _ := d.YMD()
			f.month = strconv.Itoa(int(m))
		}
	}
}

// item builds the budget item the form describes. It fails on what it cannot read and on a missing account; ops checks the rest.
func (f itemForm) item() (model.BudgetItem, error) {
	it := model.BudgetItem{Name: f.name, Kind: f.kind, Account: f.account, From: f.from, To: f.to}
	if strings.TrimSpace(f.amount) == "" {
		return it, errors.New("enter an amount")
	}
	amount, err := parseValue(model.PropAmount, model.ModeSet, f.amount)
	if err != nil {
		return it, err
	}
	it.Amount = amount
	if strings.TrimSpace(f.growth) != "" {
		if it.Growth, err = parseValue(model.PropGrowth, model.ModeSet, f.growth); err != nil {
			return it, err
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
	if (f.every == model.Monthly || f.every == model.Annual) && strings.TrimSpace(f.date) != "" {
		d, err := parseDate(strings.TrimSpace(f.date))
		if err != nil {
			return it, err
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

func needDate(s, what string) (model.Date, error) {
	if strings.TrimSpace(s) == "" {
		return 0, fmt.Errorf("enter %s as %s", what, dateLayout)
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
	it, err := f.item()
	if err != nil {
		return err.Error()
	}
	return reasonOf(ops.CheckItem(rec, it))
}

// itemFieldID is the id of a text field of the budget item added by modification i.
func itemFieldID(i int, field string) string { return "item" + strconv.Itoa(i) + field }

// setItemField changes one field of the budget item that modification i of the projection adds, through ops.EditItem.
func (t *projTab) setItemField(proj string, i int, field, text string) {
	id := itemFieldID(i, field)
	old := t.a.rec.Projection(proj).Mods[i].Item
	f := formOf(*old)
	f.set(field, text)
	if field == "every" && (f.every == model.OneTime || f.every == model.Biweekly) && strings.TrimSpace(f.date) == "" {
		f.date = t.a.rec.Today.String()
	}
	it, err := f.item()
	if err != nil {
		t.setErr(id, err.Error())
		return
	}
	it.Indexed = old.Indexed
	// A biweekly item may start later than its anchor, which the form does not show; editing another field keeps that start, while editing the date (the anchor) moves it. A one-time item's start follows its date.
	if old.Recurrence.Kind == model.Biweekly && f.every == model.Biweekly && field != "date" {
		it.Start = old.Start
	}
	t.a.edit("change budget item", func() (*ops.Result, error) {
		return ops.EditItem(t.a.st, t.a.rec, proj, old.ID, it, ops.Options{})
	})
}

// addNewItem adds the budget item the Add form describes to the projection and clears the form. It reports whether it did.
func (t *projTab) addNewItem(proj string) bool {
	if itemProblem(t.a.rec, t.addItem) != "" {
		return false
	}
	it, _ := t.addItem.item()
	if !t.a.edit("add budget item", func() (*ops.Result, error) {
		return ops.AddItem(t.a.st, t.a.rec, proj, it, ops.Options{})
	}) {
		return false
	}
	t.addItem = itemForm{kind: t.addItem.kind, every: t.addItem.every}
	return true
}

// setModTarget changes the object and property of modification i of the projection, keeping its values, through ops.SetMod. A property of propEndsOn makes the modification what ops.EndItem stores, from the date the modification had or else the record's today; leaving it makes an amount of zero from the beginning.
func (t *projTab) setModTarget(proj string, i int, kind model.ObjectKind, obj, prop string) {
	rec := t.a.rec
	m := rec.Projection(proj).Mods[i]
	endDate, ended := ops.EndedOn(m)
	_, from, _ := constantOf(m.Values)
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
