// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// checkItem refuses a budget item that is incomplete or inconsistent, and returns it with the fields its kind does not use cleared.
func checkItem(rec *model.Record, it model.BudgetItem) (model.BudgetItem, error) {
	return checkItemAs(rec, it, nil)
}

// checkItemAs is checkItem for an item that replaces existing, if not nil. An item whose existing start is absent, as in a legacy item, may keep it absent.
func checkItemAs(rec *model.Record, it model.BudgetItem, existing *model.BudgetItem) (model.BudgetItem, error) {
	it.Name = strings.TrimSpace(it.Name)
	if it.Name == "" {
		return it, refuse(ErrInvalidValue, "a budget item needs a name")
	}
	if len(it.Amount.Segments()) == 0 {
		return it, refuse(ErrInvalidValue, "a budget item needs an amount")
	}
	for _, g := range it.Amount.Segments() {
		if a := g.Value; a != a || a <= 0 || a > maxValue {
			return it, refuse(ErrInvalidValue, "the amount %v must be above zero and within range", a)
		}
	}
	for _, s := range it.Growth.Segments() {
		if g := s.Value; g != g || g <= -1 || g > maxValue {
			return it, refuse(ErrInvalidValue, "the growth %v must be above -1 and within range", g)
		}
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
	// A legacy item may stay without a start if its recurrence is unchanged or needs no phase; otherwise an anchor phases it.
	legacy := existing != nil && existing.Start == nil
	keepAbsent := legacy
	if legacy && r != existing.Recurrence {
		switch r.Kind {
		case model.Monthly, model.Semimonthly, model.Annual:
		default:
			keepAbsent = false
		}
	}
	start := it.Start
	if start == nil && !keepAbsent && r.Anchor != 0 && (r.Kind == model.OneTime || r.Kind == model.Biweekly) {
		start = &r.Anchor
	}
	if start == nil && !keepAbsent && !(legacy && r.Anchor != 0) {
		return it, refuse(ErrInvalidValue, "a budget item needs a start date")
	}
	if r.Kind == model.OneTime && start != nil && r.Anchor != 0 && *start != r.Anchor {
		return it, refuse(ErrInvalidValue, "a one-time item happens on one date: its start %v and its date %v differ", *start, r.Anchor)
	}
	switch r.Kind {
	case model.OneTime, model.Weekly, model.Biweekly, model.Quarterly:
	case model.Monthly:
		if r.Day < 1 || r.Day > 31 {
			return it, refuse(ErrInvalidValue, "the day of the month %d must be from 1 to 31", r.Day)
		}
	case model.Semimonthly:
		if r.Day < 1 || r.Day > 31 || r.Day2 < 1 || r.Day2 > 31 || r.Day == r.Day2 {
			return it, refuse(ErrInvalidValue, "a semimonthly item needs two different days of the month, each from 1 to 31")
		}
	case model.Annual:
		if r.Month < 1 || r.Month > 12 || r.Day < 1 || r.Day > time.Date(2024, time.Month(r.Month)+1, 0, 0, 0, 0, 0, time.UTC).Day() {
			return it, refuse(ErrInvalidValue, "an annual item needs a month from 1 to 12 and a day in that month")
		}
	case model.Custom:
		if r.Count < 1 || !r.Unit.Valid() {
			return it, refuse(ErrInvalidValue, "a custom recurrence needs a count of at least 1 and a unit of days, weeks, months or years")
		}
	default:
		return it, refuse(ErrInvalidValue, "the recurrence %q must be one-time, weekly, biweekly, semimonthly, monthly, quarterly, annual or custom", r.Kind)
	}
	if r.Kind != model.Custom {
		it.Recurrence.Count, it.Recurrence.Unit = 0, ""
	}
	effective := start
	if effective == nil && r.Anchor != 0 {
		effective = &r.Anchor
	}
	if it.End != nil && effective != nil && *it.End < *effective {
		return it, refuse(ErrInvalidValue, "the end %v is before the start %v", *it.End, *effective)
	}
	it.Start = start
	it.Start, it.End = copyPtr(it.Start), copyPtr(it.End)
	return it, nil
}

// CheckItem returns the reason AddItem would refuse the item, nil if it would not. It is the check AddItem and EditItem make, for editors that show why an input cannot be added. Pass rec.For(projection) to check an item of a projection that defines accounts.
func CheckItem(rec *model.Record, item model.BudgetItem) error {
	_, err := checkItem(rec, item)
	return err
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

// assignItemID gives the item an id made from its name if it has none, and refuses an id that ids already has.
func assignItemID(it *model.BudgetItem, ids map[string]bool) error {
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
	return nil
}

// AddItem adds a new budget item to the projection, as an add_item modification at the end of its list. An empty ID is generated from the name; an ID that another budget item or added item has is ErrNameTaken. The item must be complete: a name, an amount above zero in every segment, a start date, an account for income and expense, from and to for a transfer, and a recurrence with what it needs.
func AddItem(st *store.Store, rec *model.Record, name string, item model.BudgetItem, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		view, err := viewOf(rec, p)
		if err != nil {
			return err
		}
		it, err := checkItem(view, item)
		if err != nil {
			return err
		}
		if err := assignItemID(&it, itemIDs(rec)); err != nil {
			return err
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
		view, err := viewOf(rec, p)
		if err != nil {
			return err
		}
		it, err := checkItemAs(view, item, p.Mods[i].Item)
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
	return AddMod(st, rec, name, EndMod(itemID, from), o)
}

// EndMod is the modification EndItem adds.
func EndMod(itemID string, from model.Date) model.Modification {
	return model.Modification{
		Target:   model.ObjectRef{Kind: model.KindBudgetItem, ID: itemID},
		Property: model.PropAmount,
		Values:   model.MustSchedule(model.Segment{From: from, Value: 0}),
	}
}

// EndedOn reports whether m is exactly what EndItem stores, and the date it ends the item from.
func EndedOn(m model.Modification) (model.Date, bool) {
	segs := m.Values.Segments()
	if m.IsScale() || m.Target.Kind != model.KindBudgetItem || m.Property != model.PropAmount || len(segs) != 1 || segs[0].Value != 0 || segs[0].End != nil || segs[0].From == model.MinDate {
		return 0, false
	}
	return segs[0].From, true
}
