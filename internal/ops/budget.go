// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"slices"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// baseItem returns the index of the record's budget item with the id.
func baseItem(rec *model.Record, id string) (int, error) {
	i := slices.IndexFunc(rec.Budget.Items, func(it model.BudgetItem) bool { return it.ID == id })
	if i < 0 {
		return 0, refuse(ErrUnknownObject, "there is no budget item %q", id)
	}
	return i, nil
}

// AddBudgetItem adds an item to the base budget, which every projection builds on. The same checks as AddItem apply, and the id must be unused by every base item and by every item a projection adds.
func AddBudgetItem(st *store.Store, rec *model.Record, item model.BudgetItem, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		it, err := checkItem(next, item)
		if err != nil {
			return Result{}, err
		}
		if err := assignItemID(&it, itemIDs(next)); err != nil {
			return Result{}, err
		}
		next.Budget.Items = append(next.Budget.Items, it)
		return Result{Item: &it}, nil
	})
}

// EditBudgetItem replaces the base budget item with item, keeping its id; an item with another non-empty ID is refused. The same checks as AddItem apply, but an item whose start is absent may keep it absent. A payroll deduction taken from the item that it no longer fits, because it stopped being wages, recurs otherwise or starts later, becomes an ordinary transfer out of the item's account and is listed in the result's Affected.
func EditBudgetItem(st *store.Store, rec *model.Record, id string, item model.BudgetItem, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		i, err := baseItem(next, id)
		if err != nil {
			return Result{}, err
		}
		if item.ID != "" && item.ID != id {
			return Result{}, refuse(ErrInvalidValue, "the id of budget item %q cannot change", id)
		}
		it, err := checkItemAs(next, item, &next.Budget.Items[i])
		if err != nil {
			return Result{}, err
		}
		it.ID = id
		released := releaseDeductions(next, id, next.Budget.Items[i].Account, &it)
		next.Budget.Items[i] = it
		return Result{Item: &it, Affected: released}, nil
	})
}

// EndBudgetItem makes the last date of the base budget item the given one. An end before the item's start is refused. The payroll deductions taken from a wages item end with it and are listed in the result's Affected.
func EndBudgetItem(st *store.Store, rec *model.Record, id string, end model.Date, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		i, err := baseItem(next, id)
		if err != nil {
			return Result{}, err
		}
		cur := next.Budget.Items[i]
		cur.End = &end
		it, err := checkItemAs(next, cur, &next.Budget.Items[i])
		if err != nil {
			return Result{}, err
		}
		next.Budget.Items[i] = it
		var ended []Affected
		for j, d := range next.Budget.Items {
			if d.Paycheck != id || d.End != nil && *d.End <= end {
				continue
			}
			d.End = &end
			d, err := checkItemAs(next, d, &next.Budget.Items[j])
			if err != nil {
				return Result{}, err
			}
			next.Budget.Items[j] = d
			ended = append(ended, Affected{AffectedBudgetItem, "budget", itemDetail(d) + " payroll deduction", EffectEnded})
		}
		return Result{Item: &it, Affected: ended}, nil
	})
}

// DeleteBudgetItem deletes the base budget item. A modification of it in a projection is not refused: it is left pointing at nothing, which the engine skips and the editors show. A payroll deduction taken from it becomes an ordinary transfer out of the account its paychecks were deposited into, and is listed in the result's Affected. The result's Item is the item as it was and Removed its id.
func DeleteBudgetItem(st *store.Store, rec *model.Record, id string, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		i, err := baseItem(next, id)
		if err != nil {
			return Result{}, err
		}
		it := next.Budget.Items[i]
		released := releaseDeductions(next, id, it.Account, nil)
		next.Budget.Items = slices.Delete(next.Budget.Items, i, i+1)
		return Result{Item: &it, Removed: id, Affected: released}, nil
	})
}
