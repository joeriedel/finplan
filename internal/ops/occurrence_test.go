// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

var (
	nov3  = model.D(2026, time.November, 3)
	nov6  = model.D(2026, time.November, 6)
	oct6  = model.D(2026, time.October, 6)
	today = model.D(2026, time.November, 8)
)

func TestSetOccurrenceStoresCorrectsAndSurvivesReloadAndOtherEdits(t *testing.T) {
	st, rec := seed(t)
	if o, ok := rec.Occurrences.Get("groceries", nov3); !ok || o.ID == 0 || o.Posted == nil || *o.Posted != 2050 {
		t.Fatalf("the demo's groceries occurrence loaded as %+v, %v", o, ok)
	}
	before := rec.Occurrences.All()

	pending := model.StatusPending
	res, err := SetOccurrence(st, rec, "dining", nov6, today, OccurrenceChange{Status: &pending, Expected: model.Ptr(1000.0)}, "", Options{DryRun: true})
	if err != nil || res.Occurrence.ID != 0 {
		t.Fatalf("dry run: %+v, %v", res, err)
	}
	if got, err := st.Load(); err != nil || !reflect.DeepEqual(got.Occurrences.All(), before) {
		t.Fatalf("a dry run changed the stored occurrences: %v", err)
	}

	res, err = SetOccurrence(st, rec, "dining", nov6, today, OccurrenceChange{Status: &pending, Expected: model.Ptr(1000.0)}, "", Options{})
	if err != nil || res.Occurrence.ID == 0 {
		t.Fatalf("set: %+v, %v", res, err)
	}
	rec = res.Record
	id := res.Occurrence.ID
	if o, ok := rec.Occurrences.Get("dining", nov6); !ok || o.Status != pending || o.Expected == nil || *o.Expected != 1000 || o.Posted != nil {
		t.Fatalf("stored %+v, %v", o, ok)
	}

	// The same values change nothing; a posted amount is added to what is there.
	if _, err := SetOccurrence(st, rec, "dining", nov6, today, OccurrenceChange{Expected: model.Ptr(1000.0)}, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("same values: %v", err)
	}
	res, err = SetOccurrence(st, rec, "dining", nov6, today, OccurrenceChange{Posted: model.Ptr(300.0)}, "paid some", Options{})
	if err != nil || res.Occurrence.ID != id {
		t.Fatalf("correct: %+v, %v", res, err)
	}
	rec = res.Record
	if o, _ := rec.Occurrences.Get("dining", nov6); o.Status != pending || *o.Expected != 1000 || *o.Posted != 300 {
		t.Errorf("corrected %+v", o)
	}

	// An edit of the budget saves the whole record: the occurrences stay.
	it, _ := rec.Budget.Item("dining")
	it.Timing = model.Lump
	if res, err = EditBudgetItem(st, rec, "dining", it, Options{}); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := loaded.Budget.Item("dining"); got.Timing != model.Lump {
		t.Errorf("timing %q", got.Timing)
	}
	if o, ok := loaded.Occurrences.Get("dining", nov6); !ok || o.ID != id || *o.Posted != 300 {
		t.Errorf("after reload %+v, %v", o, ok)
	}

	revs, err := FactHistory(st, id, store.FactOccurrence)
	if err != nil || len(revs) != 2 || revs[1].Note != "paid some" || revs[0].Occurrence.Posted != nil {
		t.Errorf("history %+v, %v", revs, err)
	}

	res, err = ClearOccurrence(st, loaded, "dining", nov6, today, "mistake", Options{})
	if err != nil || res.Occurrence.ID != id {
		t.Fatalf("clear: %+v, %v", res, err)
	}
	if _, ok := res.Record.Occurrences.Get("dining", nov6); ok {
		t.Error("a cleared occurrence is still in the record")
	}
	if _, err := ClearOccurrence(st, res.Record, "dining", nov6, today, "", Options{}); !errors.Is(err, ErrNoChange) {
		t.Errorf("clear again: %v", err)
	}
	if revs, err = FactHistory(st, id, store.FactOccurrence); err != nil || len(revs) != 3 || !revs[2].Void {
		t.Errorf("history after clear %+v, %v", revs, err)
	}
}

func TestSetOccurrenceRefusals(t *testing.T) {
	st, rec := seed(t)
	posted := model.StatusPosted
	bad := model.OccurrenceStatus("done")
	for _, c := range []struct {
		name   string
		item   string
		period model.Date
		ch     OccurrenceChange
		want   error
	}{
		{"unknown item", "nothing", nov6, OccurrenceChange{Status: &posted}, ErrUnknownObject},
		{"two periods back", "dining", model.D(2026, time.September, 6), OccurrenceChange{Status: &posted}, ErrInvalidValue},
		{"next period", "dining", model.D(2026, time.December, 6), OccurrenceChange{Status: &posted}, ErrInvalidValue},
		{"not a period start", "dining", model.D(2026, time.November, 7), OccurrenceChange{Status: &posted}, ErrInvalidValue},
		{"negative posted", "dining", nov6, OccurrenceChange{Posted: model.Ptr(-1.0)}, ErrInvalidValue},
		{"negative expected", "dining", nov6, OccurrenceChange{Expected: model.Ptr(-1.0)}, ErrInvalidValue},
		{"bad status", "dining", nov6, OccurrenceChange{Status: &bad}, ErrInvalidValue},
	} {
		if _, err := SetOccurrence(st, rec, c.item, c.period, today, c.ch, "", Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// The period before the current one can be set.
	if _, err := SetOccurrence(st, rec, "dining", oct6, today, OccurrenceChange{Status: &posted}, "", Options{}); err != nil {
		t.Errorf("previous period: %v", err)
	}
}

func TestBudgetItemTimingIsChecked(t *testing.T) {
	st, rec := seed(t)
	it, _ := rec.Budget.Item("dining")
	it.Timing = "weekly"
	if _, err := EditBudgetItem(st, rec, "dining", it, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("bad timing: %v", err)
	}
}
