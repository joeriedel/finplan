// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// budgetTab shows and edits the base budget, which every projection builds on.
type budgetTab struct {
	a *app
	// fields draws and keeps the typed text and errors of the item fields, which are the Projections tab's.
	fields *projTab
	rec    *model.Record
	// open holds the ids of the items whose fields are shown for editing.
	open map[string]bool
	// endOn is the date typed for each item's End action.
	endOn map[string]string
	// tl is the timeline editor of the amount or growth of one item, which its target and property name.
	tl timelineState
	// addItem is the Add form.
	addItem itemForm
}

func newBudgetTab(a *app) *budgetTab {
	t := &budgetTab{a: a, fields: &projTab{a: a}, open: map[string]bool{}, endOn: map[string]string{}, addItem: newItemForm()}
	t.fields.reset()
	return t
}

// itemPrefix is the id prefix of the fields of the base item with the id.
func itemPrefix(id string) string { return "base" + id + "." }

// setField changes one field of the base item through ops.EditBudgetItem: kind, name, amount, growth, every, date, day, day2, month, count, unit, end, account, from, to, or indexed (text "true" or "false"). An input that cannot be read, or that ops refuses, changes nothing.
func (t *budgetTab) setField(id, field, text string) bool {
	fid := itemPrefix(id) + field
	old, ok := t.a.rec.Budget.Item(id)
	if !ok {
		t.fields.setErr(fid, "the item no longer exists")
		return false
	}
	f := formOf(old, t.a.rec.Today)
	f.set(field, text)
	if field == "every" && (f.every == model.OneTime || f.every == model.Biweekly) && strings.TrimSpace(f.date) == "" {
		f.date = t.a.rec.Today.String()
	}
	it, err := f.item(t.a.rec.Today)
	if err != nil {
		t.fields.setErr(fid, err.Error())
		return false
	}
	keepUnedited(&it, old, f, field)
	return t.a.edit("change budget item", func() (*ops.Result, error) {
		return ops.EditBudgetItem(t.a.st, t.a.rec, id, it, ops.Options{})
	})
}

// endProblem says why the base item cannot end on the date as typed, or "" if it can.
func (t *budgetTab) endProblem(id, text string) string {
	d, err := parseDate(strings.TrimSpace(text))
	if err != nil {
		return err.Error()
	}
	_, err = ops.EndBudgetItem(t.a.st, t.a.rec, id, d, ops.Options{DryRun: true})
	return reasonOf(err)
}

// endItem makes the typed date the last one of the base item, through ops.EndBudgetItem, and drops the typed text. It reports whether it did.
func (t *budgetTab) endItem(id, text string) bool {
	if t.endProblem(id, text) != "" {
		return false
	}
	d, _ := parseDate(strings.TrimSpace(text))
	if !t.a.edit("end budget item", func() (*ops.Result, error) {
		return ops.EndBudgetItem(t.a.st, t.a.rec, id, d, ops.Options{})
	}) {
		return false
	}
	delete(t.endOn, id)
	return true
}

// deleteItem deletes the base item through ops.DeleteBudgetItem. A projection modification of it is left pointing at nothing. It reports whether it did.
func (t *budgetTab) deleteItem(id string) bool {
	if !t.a.edit("delete budget item", func() (*ops.Result, error) {
		return ops.DeleteBudgetItem(t.a.st, t.a.rec, id, ops.Options{})
	}) {
		return false
	}
	delete(t.open, id)
	delete(t.endOn, id)
	if t.tl.target.ID == id {
		t.tl = timelineState{}
	}
	return true
}

// addNewItem adds the item the Add form describes to the base budget and clears the form. It reports whether it did.
func (t *budgetTab) addNewItem() bool {
	if itemProblem(t.a.rec, t.addItem) != "" {
		return false
	}
	it, _ := t.addItem.item(t.a.rec.Today)
	if !t.a.edit("add budget item", func() (*ops.Result, error) {
		return ops.AddBudgetItem(t.a.st, t.a.rec, it, ops.Options{})
	}) {
		return false
	}
	t.addItem = itemForm{kind: t.addItem.kind, every: t.addItem.every, unit: t.addItem.unit}
	return true
}

func (t *budgetTab) draw() {
	a := t.a
	if t.rec != a.rec {
		t.rec = a.rec
		t.fields.reset()
	}
	if !imgui.BeginChildStrV("##budget", imgui.NewVec2(0, 0), 0, 0) {
		imgui.EndChild()
		return
	}
	defer imgui.EndChild()

	em := imgui.FontSize()
	income, expenses, net := budgetTotalsText(a.rec)
	imgui.TextUnformatted("Each month, on " + a.rec.Today.String() + ", in today's dollars:")
	imgui.TextUnformatted("income " + income + "    expenses " + expenses + "    left over " + net)
	imgui.TextDisabled("Transfers are in neither total; a one-time item counts for nothing, and an annual item for a twelfth.")

	imgui.TextDisabled("A dash means no start or end date is set. Open an item with Edit to change it.")
	offsets := []float32{8, 32, 52, 64, 76, 88, 98, 108, 120}
	for _, g := range budgetGroups(a.rec) {
		imgui.Spacing()
		imgui.SeparatorText(g.Title)
		if len(g.Rows) == 0 {
			imgui.TextDisabled("No " + g.Title + " items.")
		} else {
			for i, h := range budgetHeaders {
				if i == 0 {
					imgui.SetCursorPosX(em * offsets[0])
				} else {
					imgui.SameLineV(em*offsets[i], -1)
				}
				imgui.TextDisabled(h)
			}
		}
		for _, r := range g.Rows {
			imgui.PushIDStr(r.ID)
			label := "Edit"
			if t.open[r.ID] {
				label = "Close"
			}
			if imgui.Button(label) {
				t.open[r.ID] = !t.open[r.ID]
			}
			for i, s := range []string{r.Name, r.Schedule, r.Amount, r.Accounts, r.Start, r.End, r.Indexed, r.Growth, r.Monthly} {
				imgui.SameLineV(em*offsets[i], -1)
				imgui.TextUnformatted(s)
			}
			if t.open[r.ID] {
				imgui.Indent()
				t.editor(r.ID)
				imgui.Unindent()
			}
			imgui.PopID()
		}
	}

	imgui.Spacing()
	imgui.SeparatorText("Add a budget item")
	t.addForm()
}

// editor draws the fields of the base item with the id, and its End and Delete actions.
func (t *budgetTab) editor(id string) {
	it, ok := t.a.rec.Budget.Item(id)
	if !ok {
		return
	}
	em := imgui.FontSize()
	prefix := itemPrefix(id)
	t.fields.itemFields(prefix, formOf(it, t.a.rec.Today), false, func(field, text string) { t.setField(id, field, text) }, func(field string) { t.openTimeline(id, field) })
	indexed := it.Indexed
	if imgui.Checkbox("rises with inflation##"+prefix, &indexed) {
		t.setField(id, "indexed", boolText(indexed))
	}

	imgui.AlignTextToFramePadding()
	imgui.TextDisabled("end on")
	imgui.SameLine()
	text := t.endOn[id]
	imgui.SetNextItemWidth(em * 8)
	if imgui.InputTextWithHint("##endon"+id, dateHint, &text, 0, nil) {
		t.endOn[id] = text
	}
	imgui.SameLine()
	actionButton("End", t.endProblem(id, text), func() { t.endItem(id, text) })
	imgui.SameLine()
	if imgui.Button("Delete") {
		t.deleteItem(id)
	}
	t.timelineEditor(id)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (t *budgetTab) addForm() {
	t.fields.itemFields("newitem", t.addItem, true, t.addItem.set, nil)
	indexed := t.addItem.indexed
	if imgui.Checkbox("rises with inflation##newitem", &indexed) {
		t.addItem.set("indexed", boolText(indexed))
	}
	addButton(itemProblem(t.a.rec, t.addItem), func() { t.addNewItem() })
}
