// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// checkItem refuses a budget item that is incomplete or inconsistent, and returns it with the fields its kind does not use cleared.
func checkItem(rec *model.Record, it model.BudgetItem) (model.BudgetItem, error) {
	it.Name = strings.TrimSpace(it.Name)
	if it.Name == "" {
		return it, refuse(ErrInvalidValue, "a budget item needs a name")
	}
	if a := it.Amount; a != a || a <= 0 || a > maxValue {
		return it, refuse(ErrInvalidValue, "the amount %v must be above zero and within range", it.Amount)
	}
	switch it.Kind {
	case model.ItemIncome, model.ItemExpense:
		if rec.Account(it.Account) == nil {
			return it, refuse(ErrUnknownTarget, "there is no account %q", it.Account)
		}
		it.From, it.To = "", ""
	case model.ItemTransfer:
		if it.From == "" || it.To == "" || it.From == it.To {
			return it, refuse(ErrInvalidValue, "a transfer needs two different accounts, from and to")
		}
		// An account of a transfer may be outside the record, but not both.
		if rec.Account(it.From) == nil && rec.Account(it.To) == nil {
			return it, refuse(ErrUnknownTarget, "a transfer needs at least one of %q and %q to be an account of the record", it.From, it.To)
		}
		it.Account = ""
	default:
		return it, refuse(ErrInvalidValue, "unknown item kind %q", it.Kind)
	}
	r := it.Recurrence
	start := it.Start
	switch r.Kind {
	case model.OneTime, model.Biweekly:
		if start == nil {
			start = &r.Anchor
		}
	case model.Monthly:
		if r.Day < 1 || r.Day > 31 {
			return it, refuse(ErrInvalidValue, "the day of the month %d must be from 1 to 31", r.Day)
		}
	case model.Annual:
		if r.Month < 1 || r.Month > 12 || r.Day < 1 || r.Day > 31 {
			return it, refuse(ErrInvalidValue, "an annual item needs a month from 1 to 12 and a day from 1 to 31")
		}
	default:
		return it, refuse(ErrInvalidValue, "the recurrence %q must be one-time, monthly, biweekly or annual", r.Kind)
	}
	if it.End != nil && start != nil && *it.End < *start {
		return it, refuse(ErrInvalidValue, "the end %v is before the start %v", *it.End, *start)
	}
	it.Start, it.End = copyPtr(it.Start), copyPtr(it.End)
	return it, nil
}

// itemIDs returns the ids of the record's budget items and of every item added by a modification of any projection.
func itemIDs(rec *model.Record) map[string]bool {
	ids := map[string]bool{}
	for _, it := range rec.Budget.Items {
		ids[it.ID] = true
	}
	for _, p := range rec.Projections {
		addedItems(ids, p.Mods)
	}
	return ids
}

// slug makes an id of the name: lowercase letters and digits, joined by single hyphens.
func slug(name string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(name) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			hyphen = false
		} else {
			hyphen = true
		}
	}
	if b.Len() == 0 {
		return "item"
	}
	return b.String()
}

// AddItem adds a new budget item to the projection, as an add_item modification at the end of its list. An empty ID is generated from the name; an ID that another budget item or added item has is ErrNameTaken. The item must be complete: a name, an amount above zero, an account for income and expense, from and to for a transfer, and a one-time, monthly, biweekly or annual recurrence.
func AddItem(st *store.Store, rec *model.Record, name string, item model.BudgetItem, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		it, err := checkItem(rec, item)
		if err != nil {
			return err
		}
		ids := itemIDs(rec)
		it.ID = strings.TrimSpace(it.ID)
		switch {
		case it.ID == "":
			base := slug(it.Name)
			it.ID = base
			for n := 2; ids[it.ID]; n++ {
				it.ID = base + "-" + strconv.Itoa(n)
			}
		case ids[it.ID]:
			return refuse(ErrNameTaken, "there is already a budget item %q", it.ID)
		}
		p.Mods = append(p.Mods, model.Modification{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: it.ID}, Property: model.PropAddItem, Item: &it})
		return nil
	})
}

// EditItem replaces the budget item that the projection's own add_item modification adds with item, keeping its id; an item with another non-empty ID is refused. The same checks as AddItem apply. An item the projection does not add is ErrUnknownTarget.
func EditItem(st *store.Store, rec *model.Record, name, itemID string, item model.BudgetItem, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		i := slices.IndexFunc(p.Mods, func(m model.Modification) bool {
			return m.Property == model.PropAddItem && m.Item != nil && m.Item.ID == itemID
		})
		if i < 0 {
			return refuse(ErrUnknownTarget, "projection %q does not add a budget item %q", name, itemID)
		}
		if item.ID != "" && item.ID != itemID {
			return refuse(ErrInvalidValue, "the id of budget item %q cannot change", itemID)
		}
		it, err := checkItem(rec, item)
		if err != nil {
			return err
		}
		it.ID = itemID
		p.Mods[i].Item = &it
		return nil
	})
}

// EndItem appends a modification that sets the amount of the budget item to zero from the date on, so that it pays nothing from then; before it the item is as it was. The item may be one of the record's or one added in the chain.
func EndItem(st *store.Store, rec *model.Record, name, itemID string, from model.Date, o Options) (*Result, error) {
	m := model.Modification{
		Target:   model.ObjectRef{Kind: model.KindBudgetItem, ID: itemID},
		Property: model.PropAmount,
		Values:   model.MustSchedule(model.Segment{From: from, Value: 0}),
	}
	return AddMod(st, rec, name, m, o)
}
