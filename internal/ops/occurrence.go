// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// OccurrenceChange is the values of an occurrence to set; nil leaves one as it is.
type OccurrenceChange struct {
	Status   *model.OccurrenceStatus
	Posted   *float64
	Expected *float64
}

// occurrencePeriod returns the period of the base budget item that starts on start, which must be the one containing asOf or the one before it.
func occurrencePeriod(rec *model.Record, id string, start, asOf model.Date) (model.BudgetItem, error) {
	i, err := baseItem(rec, id)
	if err != nil {
		return model.BudgetItem{}, err
	}
	it := rec.Budget.Items[i]
	cur, ok := it.PeriodAt(asOf)
	if ok && cur.Start == start {
		return it, nil
	}
	if ok {
		if prev, ok := it.PeriodBefore(cur); ok && prev.Start == start {
			return it, nil
		}
	}
	return it, refuse(ErrInvalidValue, "%s is not the start of the current period of budget item %q on %s, nor of the one before it", start, id, asOf)
}

// SetOccurrence records what is known of the period of the base budget item that starts on period: its status, the amount posted and the amount expected. It stores a new occurrence fact, or corrects the existing one with the note; values the change leaves nil stay as they were. Only the period containing asOf and the one before it can be set; an unknown item, an invalid status and a negative amount are refused. A change that leaves the occurrence as it is is ErrNoChange. The result has the reloaded Record unless the run was a dry run.
func SetOccurrence(st *store.Store, rec *model.Record, item string, period, asOf model.Date, ch OccurrenceChange, note string, o Options) (*Result, error) {
	if _, err := occurrencePeriod(rec, item, period, asOf); err != nil {
		return nil, err
	}
	occ := model.Occurrence{Item: item, Period: period, Status: model.StatusAuto}
	existing, found := rec.Occurrences.Get(item, period)
	if found {
		occ = existing.Clone()
	}
	if ch.Status != nil {
		if !ch.Status.Valid() {
			return nil, refuse(ErrInvalidValue, "the status %q must be auto, posted or pending", *ch.Status)
		}
		occ.Status = *ch.Status
	}
	for _, a := range []struct {
		name string
		in   *float64
		out  **float64
	}{{"posted", ch.Posted, &occ.Posted}, {"expected", ch.Expected, &occ.Expected}} {
		if a.in == nil {
			continue
		}
		if !finite(*a.in) || *a.in < 0 {
			return nil, refuse(ErrInvalidValue, "the amount %s %v must not be negative", a.name, *a.in)
		}
		v := *a.in
		*a.out = &v
	}
	if occ.Status == "" {
		occ.Status = model.StatusAuto
	}
	if found && sameOccurrence(occ, existing) || !found && occ.Status == model.StatusAuto && occ.Posted == nil && occ.Expected == nil {
		return nil, ErrNoChange
	}
	res := &Result{Occurrence: &occ}
	if o.DryRun {
		next := rec.Clone()
		next.Occurrences.Add(occ)
		res.Record = next
		if found {
			if err := st.TryCorrectFact(existing.ID, store.FactValue{Occurrence: &occ}, note); err != nil {
				return nil, factError(err, existing.ID)
			}
		}
		return res, nil
	}
	if found {
		return correctFact(st, existing.ID, store.FactValue{Occurrence: &occ}, note, res, o)
	}
	err := st.Write(nil, func(f store.FactTx) error {
		id, err := f.Add(store.FactValue{Occurrence: &occ})
		occ.ID = id
		return err
	})
	if err != nil {
		return nil, &SaveError{err}
	}
	return withReloaded(st, res, o)
}

func sameOccurrence(a, b model.Occurrence) bool {
	same := func(x, y *float64) bool { return x == nil && y == nil || x != nil && y != nil && *x == *y }
	return a.Status == b.Status && same(a.Posted, b.Posted) && same(a.Expected, b.Expected)
}

// ClearOccurrence voids the occurrence of the period that starts on period with the note, so that the period is as if nothing was recorded; SetOccurrence stores a new one. The same periods as for SetOccurrence can be cleared. A period with no occurrence is ErrNoChange.
func ClearOccurrence(st *store.Store, rec *model.Record, item string, period, asOf model.Date, note string, o Options) (*Result, error) {
	if _, err := occurrencePeriod(rec, item, period, asOf); err != nil {
		return nil, err
	}
	existing, found := rec.Occurrences.Get(item, period)
	if !found {
		return nil, ErrNoChange
	}
	res, err := voidFact(st, existing.ID, note, &Result{Occurrence: &existing}, o)
	if err == nil && o.DryRun {
		next := rec.Clone()
		next.Occurrences.Remove(item, period)
		res.Record = next
	}
	return res, err
}
