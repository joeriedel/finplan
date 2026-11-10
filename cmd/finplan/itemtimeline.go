// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// itemSchedule is the amount or growth schedule of the item, by the field name of the item forms.
func itemSchedule(it model.BudgetItem, field string) model.Schedule {
	if field == "growth" {
		return it.Growth
	}
	return it.Amount
}

// withSchedule returns the item with its amount or growth, by field, replaced by next.
func withSchedule(it model.BudgetItem, field string, next model.Schedule) model.BudgetItem {
	if field == "growth" {
		it.Growth = next
	} else {
		it.Amount = next
	}
	return it
}

// scheduleProp is the property the timeline strip shows for the amount or growth field.
func scheduleProp(field string) string {
	if field == "growth" {
		return model.PropGrowth
	}
	return model.PropAmount
}

// axisStart is the date a timeline strip on the item's schedule starts its axis at when the schedule has no earlier date.
func axisStart(it model.BudgetItem, today model.Date) model.Date {
	if it.Start != nil {
		return *it.Start
	}
	return today
}

// setSchedule writes next as the amount or growth, by field, of the base item through ops.EditBudgetItem, as one undoable edit. It reports whether it did.
func (t *budgetTab) setSchedule(id, field, label string, next model.Schedule) bool {
	old, ok := t.a.rec.Budget.Item(id)
	if !ok {
		return false
	}
	return t.a.edit(label, func() (*ops.Result, error) {
		return ops.EditBudgetItem(t.a.st, t.a.rec, id, withSchedule(old, field, next), ops.Options{})
	})
}

// openTimeline shows the timeline strip for the amount or growth of the base item. A growth that is none starts as a schedule of zero, which is an edit of its own.
func (t *budgetTab) openTimeline(id, field string) {
	it, ok := t.a.rec.Budget.Item(id)
	if !ok {
		return
	}
	if len(itemSchedule(it, field).Segments()) == 0 && !t.setSchedule(id, field, "start schedule", model.Constant(0)) {
		return
	}
	t.tl = timelineState{open: true, generation: t.a.generation, target: model.ObjectRef{ID: id}, property: field, sel: -1}
}

// timelineEditor draws the strip for the base item with the id if it is the one openTimeline chose.
func (t *budgetTab) timelineEditor(id string) {
	st := &t.tl
	if !st.open || st.target.ID != id {
		return
	}
	it, ok := t.a.rec.Budget.Item(id)
	if !ok || st.generation != t.a.generation || len(itemSchedule(it, st.property).Segments()) == 0 {
		*st = timelineState{}
		return
	}
	s := &timelineStrip{
		a: t.a, st: st, prop: scheduleProp(st.property), mode: model.ModeSet,
		field: t.fields.field, setErr: t.fields.setErr, dropBuf: t.fields.dropBuf,
		commit: func(label string, next model.Schedule) { t.setSchedule(id, st.property, label, next) },
	}
	s.draw("Timeline of "+st.property+": "+it.Name, itemSchedule(it, st.property), axisStart(it, t.a.rec.Today))
}

// setItemSchedule writes next as the amount or growth, by field, of the budget item that modification i of the projection adds, through ops.EditItem, as one undoable edit. It reports whether it did.
func (t *projTab) setItemSchedule(proj string, i int, field, label string, next model.Schedule) bool {
	old := t.a.rec.Projection(proj).Mods[i].Item
	return t.a.edit(label, func() (*ops.Result, error) {
		return ops.EditItem(t.a.st, t.a.rec, proj, old.ID, withSchedule(*old, field, next), ops.Options{})
	})
}

// openItemTimeline shows the timeline strip for the amount or growth of the budget item that modification i of the projection adds. A growth that is none starts as a schedule of zero, which is an edit of its own.
func (t *projTab) openItemTimeline(proj string, i int, field string) {
	it := t.a.rec.Projection(proj).Mods[i].Item
	if len(itemSchedule(*it, field).Segments()) == 0 && !t.setItemSchedule(proj, i, field, "start schedule", model.Constant(0)) {
		return
	}
	t.tl = timelineState{}
	t.itemTl = timelineState{open: true, proj: proj, mod: i, generation: t.a.generation, target: model.ObjectRef{ID: it.ID}, property: field, sel: -1}
}

// itemTimelineEditor draws the strip for the item openItemTimeline chose, and closes it when the modification at that index is no longer that item's.
func (t *projTab) itemTimelineEditor(p *model.Projection) {
	st := &t.itemTl
	if !st.open {
		return
	}
	if st.generation != t.a.generation || st.proj != p.Name || st.mod >= len(p.Mods) {
		*st = timelineState{}
		return
	}
	m := p.Mods[st.mod]
	if m.Property != model.PropAddItem || m.Item == nil || m.Item.ID != st.target.ID || len(itemSchedule(*m.Item, st.property).Segments()) == 0 {
		*st = timelineState{}
		return
	}
	s := &timelineStrip{
		a: t.a, st: st, prop: scheduleProp(st.property), mode: model.ModeSet,
		field: t.field, setErr: t.setErr, dropBuf: t.dropBuf,
		commit: func(label string, next model.Schedule) { t.setItemSchedule(st.proj, st.mod, st.property, label, next) },
	}
	s.draw("Timeline of "+st.property+": "+m.Item.Name, itemSchedule(*m.Item, st.property), axisStart(*m.Item, t.a.rec.Today))
}
