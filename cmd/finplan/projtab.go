// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

const (
	dateLayout = "2006-01-02"
	// dateHint is how dates are written to the user; dateLayout is the Go layout that parses them.
	dateHint = "YYYY-MM-DD"
	// dragMod names the drag and drop payload of a modification row; the index of the row is in projTab.dragFrom.
	dragMod = "projection modification"
	// colError is the colour of messages about input that was refused.
	colError = 0xE06C6C
)

// projTab is the Projections tab: the projection list on the left, the selected projection's header and
// modifications on the right. Every change goes through app.edit, which makes it with internal/ops.
type projTab struct {
	a *app

	// sel is the selected projection; empty selects Reality, which is not editable.
	sel    string
	search string
	// rec is the record the buffers below belong to; they are dropped when it changes.
	rec *model.Record
	// bufs holds the text of fields the user is typing in or that were refused, by field id (the modification fields include the row); a field without one shows the record's value.
	bufs map[string]string
	// errs holds the reason each field's input was refused, by the same ids as bufs; the empty id holds a refusal that belongs to no field.
	errs map[string]string

	focusName  bool
	openDelete bool
	deleting   string
	dragFrom   int
	overwrites []*model.Overwrite
	// missing holds, per modification of the selected projection, the target that does not exist, or "".
	missing []string
	// starts is the effective start of each account under the selected projection; startsErr is why there are none.
	starts    []engine.AccountStart
	startsErr error
	startsFor string
	// tl is the timeline editor under the modifications table.
	tl timelineState
	// itemTl is the timeline editor of the amount or growth of a budget item that a modification adds.
	itemTl timelineState

	// The Add modification form.
	addKind  model.ObjectKind
	addObj   string
	addProp  string
	addMode  model.Mode
	addValue string
	addStart string
	addEnd   string
	// addNew switches the form to a new budget item, which addItem holds.
	addNew  bool
	addItem itemForm
	// addGrant is the form of a focal grant, which the Add form offers as a kind of its own.
	addGrant focalForm
	// addShock and addRebalance are the forms of a shock and a rebalance, which the Add form offers as kinds of their own.
	addShock     shockForm
	addRebalance rebalanceForm
	// addAcct is the Add account form.
	addAcct acctForm
	// addSetting is the Add start setting form.
	addSetting settingForm
}

func (t *projTab) reset() {
	t.bufs, t.errs, t.overwrites, t.missing, t.startsFor = map[string]string{}, map[string]string{}, nil, nil, ""
}

// setErr records the error that refused an edit of field id; the edit path runs on a projTab that was never reset.
func (t *projTab) setErr(id, msg string) {
	if t.errs == nil {
		t.errs = map[string]string{}
	}
	t.errs[id] = msg
}

func (t *projTab) draw() {
	a := t.a
	if t.rec != a.rec {
		t.rec = a.rec
		t.reset()
	}
	if t.sel != "" && a.rec.Projection(t.sel) == nil {
		t.sel = ""
	}
	imgui.PushStyleColorVec4(imgui.ColChildBg, rgba(colPanel, 1))
	open := imgui.BeginChildStrV("##projlist", imgui.NewVec2(imgui.FontSize()*18, 0), imgui.ChildFlagsBorders, 0)
	imgui.PopStyleColor()
	if open {
		t.list()
	}
	imgui.EndChild()
	imgui.SameLine()
	if imgui.BeginChildStrV("##projdetail", imgui.NewVec2(0, 0), 0, 0) {
		t.detail()
	}
	imgui.EndChild()
	t.deletePopup()
}

// ---- The list.

func (t *projTab) list() {
	a := t.a
	imgui.SetNextItemWidth(-1)
	imgui.InputTextWithHint("##search", "Search projections", &t.search, 0, nil)
	if imgui.Button("New") {
		t.newProjection()
	}
	imgui.SameLine()
	imgui.BeginDisabledV(t.sel == "")
	if imgui.Button("Clone") {
		t.cloneProjection()
	}
	imgui.SameLine()
	if imgui.Button("Rename") {
		t.focusName = true
	}
	imgui.SameLine()
	if imgui.Button("Delete") {
		t.requestDelete()
	}
	imgui.EndDisabled()
	imgui.Separator()

	if imgui.SelectableBoolV("Reality##reality", t.sel == "", 0, imgui.NewVec2(0, 0)) {
		t.selectProjection("")
	}
	for _, p := range a.rec.Projections {
		if p.Parent == "" || a.rec.Projection(p.Parent) == nil {
			t.node(p)
		}
	}
}

// node draws the projection and, below it, the projections that build on it, skipping what the search leaves out.
func (t *projTab) node(p *model.Projection) {
	if !projectionShown(t.a.rec, p.Name, t.search) {
		return
	}
	var kids []*model.Projection
	for _, c := range t.a.rec.Children(p.Name) {
		if projectionShown(t.a.rec, c.Name, t.search) {
			kids = append(kids, c)
		}
	}
	flags := imgui.TreeNodeFlagsOpenOnArrow | imgui.TreeNodeFlagsSpanAvailWidth | imgui.TreeNodeFlagsDefaultOpen
	if len(kids) == 0 {
		flags |= imgui.TreeNodeFlagsLeaf
	}
	if p.Name == t.sel {
		flags |= imgui.TreeNodeFlagsSelected
	}
	if t.search != "" {
		imgui.SetNextItemOpen(true)
	}
	open := imgui.TreeNodeExStrV(p.Name+"##"+p.Name, flags)
	if imgui.IsItemClicked() && !imgui.IsItemToggledOpen() {
		t.selectProjection(p.Name)
	}
	if open {
		for _, c := range kids {
			t.node(c)
		}
		imgui.TreePop()
	}
}

// projectionShown reports whether the projection, or one that builds on it, has the search text in its name, ignoring case.
func projectionShown(rec *model.Record, name, search string) bool {
	return projectionShownDepth(rec, name, search, 0)
}

func projectionShownDepth(rec *model.Record, name, search string, depth int) bool {
	if strings.Contains(strings.ToLower(name), strings.ToLower(strings.TrimSpace(search))) {
		return true
	}
	if depth > len(rec.Projections) {
		return false
	}
	for _, c := range rec.Children(name) {
		if projectionShownDepth(rec, c.Name, search, depth+1) {
			return true
		}
	}
	return false
}

func (t *projTab) selectProjection(name string) {
	t.sel = name
	t.reset()
}

func (t *projTab) newProjection() {
	name, parent := t.a.rec.UniqueProjectionName("New projection"), t.sel
	if t.a.edit("add projection", func() (*ops.Result, error) {
		return ops.NewProjection(t.a.st, t.a.rec, name, parent, ops.Options{})
	}) {
		t.selectProjection(name)
	}
}

func (t *projTab) cloneProjection() {
	from := t.sel
	name := t.a.rec.UniqueProjectionName(from + " copy")
	if t.a.edit("clone projection", func() (*ops.Result, error) {
		return ops.CloneProjection(t.a.st, t.a.rec, from, name, ops.Options{})
	}) {
		t.selectProjection(name)
	}
}

// requestDelete asks for confirmation, or says why the projection cannot be deleted. A dry run of the delete finds out.
func (t *projTab) requestDelete() {
	if _, err := ops.DeleteProjection(t.a.st, t.a.rec, t.sel, ops.Options{DryRun: true}); errors.Is(err, ops.ErrHasChildren) {
		t.a.notify(fmt.Sprintf("%q cannot be deleted while %q builds on it. Delete it or change what it builds on first.", t.sel, t.a.rec.Children(t.sel)[0].Name))
		return
	}
	t.deleting, t.openDelete = t.sel, true
}

func (t *projTab) deletePopup() {
	if t.openDelete {
		imgui.OpenPopupStrV("delete projection", 0)
		t.openDelete = false
	}
	if !imgui.BeginPopupModalV("delete projection", nil, imgui.WindowFlagsAlwaysAutoResize) {
		return
	}
	imgui.PushTextWrapPos()
	imgui.TextUnformatted(fmt.Sprintf("Delete the projection %q? Undo brings it back.", t.deleting))
	imgui.PopTextWrapPos()
	if imgui.Button("Delete") {
		name := t.deleting
		if p := t.a.rec.Projection(name); p != nil {
			parent := p.Parent
			if t.a.edit("delete projection", func() (*ops.Result, error) {
				return ops.DeleteProjection(t.a.st, t.a.rec, name, ops.Options{})
			}) {
				t.selectProjection(parent)
			}
		}
		imgui.CloseCurrentPopup()
	}
	imgui.SameLine()
	if imgui.Button("Cancel") {
		imgui.CloseCurrentPopup()
	}
	imgui.EndPopup()
}

// ---- The detail.

func (t *projTab) detail() {
	rec := t.a.rec
	p := rec.Projection(t.sel)
	if p == nil {
		wrappedText("Reality is the record as it stands, with no projection. It cannot be edited. Select a projection, or add one with New.")
		return
	}
	if e := t.errs[""]; e != "" {
		errorText("Not changed: " + e)
	}
	t.header(p)
	imgui.Separator()
	t.accounts(p)
	imgui.Separator()
	t.settings(p)
	imgui.Separator()
	t.mods(p)
}

func (t *projTab) label(text string) {
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(text)
	imgui.SameLineV(imgui.FontSize()*9, -1)
}

// field draws a text field showing value and returns the text the user typed once they finish editing it.
func (t *projTab) field(id, hint, value string, width float32) (string, bool) {
	text := value
	if b, ok := t.bufs[id]; ok {
		text = b
	}
	shown := text
	imgui.SetNextItemWidth(width)
	imgui.InputTextWithHint("##"+id, hint, &text, 0, nil)
	if text != shown {
		t.bufs[id] = text
	}
	done := imgui.IsItemDeactivatedAfterEdit()
	if done && text == value {
		t.dropBuf(id)
		done = false
	}
	if imgui.IsItemDeactivated() && !done {
		t.dropBuf(id)
	}
	if e := t.errs[id]; e != "" {
		if width < 0 {
			// A field that fills a table cell has no room beside it, so the message goes below, wrapped to the cell.
			imgui.PushTextWrapPos()
			errorText("Not changed: " + e)
			imgui.PopTextWrapPos()
		} else {
			imgui.SameLine()
			errorText("Not changed: " + e)
		}
	}
	return text, done
}

// commit runs set, the edit a finished field asks for, and drops the field's typed text if the edit changed nothing, so
// the field shows the record's value again. A refused input keeps its text.
func (t *projTab) commit(id string, set func()) {
	t.a.unchanged = false
	set()
	if t.a.unchanged {
		t.dropBuf(id)
	}
}

// dropBuf drops the field's typed text and the error that refused it.
func (t *projTab) dropBuf(id string) {
	delete(t.bufs, id)
	delete(t.errs, id)
}

func (t *projTab) header(p *model.Projection) {
	rec := t.a.rec
	w := imgui.FontSize() * 16

	t.label("Name")
	if t.focusName {
		imgui.SetKeyboardFocusHere()
		t.focusName = false
	}
	if text, done := t.field("name", "", p.Name, w); done {
		t.rename(strings.TrimSpace(text))
	}

	t.label("Builds on")
	imgui.SetNextItemWidth(w)
	if imgui.BeginComboV("##parent", orNone(p.Parent), 0) {
		if imgui.SelectableBoolV("(none)", p.Parent == "", 0, imgui.NewVec2(0, 0)) && p.Parent != "" {
			t.setParent(p.Name, "")
		}
		below := descendants(rec, p.Name)
		for _, c := range rec.Projections {
			if c.Name != p.Name && !below[c.Name] && imgui.SelectableBoolV(c.Name, p.Parent == c.Name, 0, imgui.NewVec2(0, 0)) && p.Parent != c.Name {
				t.setParent(p.Name, c.Name)
			}
		}
		imgui.EndCombo()
	}

	t.label("Start date")
	start := ""
	if p.Start != nil {
		start = p.Start.String()
	}
	if text, done := t.field("start", "inherit", start, w); done {
		t.commit("start", func() { t.setStart(p.Name, strings.TrimSpace(text)) })
	}

	t.label("Override history")
	imgui.SetNextItemWidth(w)
	if imgui.BeginComboV("##override", overrideText(p.OverrideHistory), 0) {
		for _, v := range []*bool{nil, model.Ptr(true), model.Ptr(false)} {
			if imgui.SelectableBoolV(overrideText(v), false, 0, imgui.NewVec2(0, 0)) {
				t.a.edit("set override history", func() (*ops.Result, error) {
					return ops.SetOverrideHistory(t.a.st, t.a.rec, p.Name, v, ops.Options{})
				})
			}
		}
		imgui.EndCombo()
	}

	t.label("Inflation")
	t.inflation(p, w)

	if imgui.Button("Pin to Graph") {
		t.showOnGraph(p.Name)
	}
}

func (t *projTab) setStart(proj, text string) {
	var d *model.Date
	if text != "" {
		v, err := parseDate(text)
		if err != nil {
			t.setErr("start", err.Error())
			return
		}
		d = &v
	}
	t.a.edit("set start date", func() (*ops.Result, error) { return ops.SetStart(t.a.st, t.a.rec, proj, d, ops.Options{}) })
}

func orNone(name string) string {
	if name == "" {
		return "(none)"
	}
	return name
}

func overrideText(v *bool) string {
	switch {
	case v == nil:
		return "Inherit"
	case *v:
		return "On"
	}
	return "Off"
}

// descendants returns the names of the projections that build on the named one, directly or not.
func descendants(rec *model.Record, name string) map[string]bool {
	out := map[string]bool{}
	for grew := true; grew; {
		grew = false
		for _, p := range rec.Projections {
			if !out[p.Name] && (p.Parent == name || out[p.Parent]) && p.Name != name {
				out[p.Name], grew = true, true
			}
		}
	}
	return out
}

func (t *projTab) rename(to string) {
	from := t.sel
	_, err := ops.RenameProjection(t.a.st, t.a.rec, from, to, ops.Options{DryRun: true})
	switch {
	case errors.Is(err, ops.ErrNoChange):
		t.dropBuf("name")
	case errors.Is(err, ops.ErrEmptyName):
		t.setErr("name", "A projection needs a name.")
	case errors.Is(err, ops.ErrNameTaken):
		t.setErr("name", fmt.Sprintf("There is already a projection named %q.", to))
	default:
		if t.a.edit("rename projection", func() (*ops.Result, error) {
			return ops.RenameProjection(t.a.st, t.a.rec, from, to, ops.Options{})
		}) {
			t.sel = strings.TrimSpace(to)
		}
	}
}

func (t *projTab) setParent(name, parent string) {
	_, err := ops.SetParent(t.a.st, t.a.rec, name, parent, ops.Options{DryRun: true})
	if errors.Is(err, model.ErrCycle) {
		t.setErr("", fmt.Sprintf("%q cannot build on %q: that would make a cycle.", name, parent))
		return
	}
	t.a.edit("change parent", func() (*ops.Result, error) {
		return ops.SetParent(t.a.st, t.a.rec, name, parent, ops.Options{})
	})
}

// inflation draws the projection's inflation: a percentage when it has one constant inflation modification or none. A
// single modification that varies over time opens the timeline editor; with several, or a scale one, each is edited in the modifications table.
func (t *projTab) inflation(p *model.Projection, w float32) {
	idx := slices.IndexFunc(p.Mods, isInflation)
	text := ""
	if idx >= 0 {
		v, from, ok := constantOf(p.Mods[idx].Values)
		single := slices.IndexFunc(p.Mods[idx+1:], isInflation) < 0 && !slices.ContainsFunc(p.Mods, func(m model.Modification) bool { return isInflation(m) && m.IsScale() })
		if !single {
			imgui.AlignTextToFramePadding()
			disabledText("varies; edit each in the modifications table")
			return
		}
		if !ok || from != nil {
			imgui.AlignTextToFramePadding()
			disabledText("varies")
			imgui.SameLine()
			if imgui.SmallButton("Timeline##inflation") {
				t.openTimeline(p.Name, idx, p.Mods[idx])
			}
			if imgui.IsItemHovered() {
				tooltipText("Edit inflation on a timeline, under the modifications table.")
			}
			return
		}
		text = formatValue(model.PropInflation, model.ModeSet, v)
	}
	if input, done := t.field("inflation", "inherit", text, w); done {
		t.commit("inflation", func() { t.setInflation(p.Name, strings.TrimSpace(input)) })
	}
}

// setInflation sets the projection's one constant inflation modification to the percentage in text, adding it if needed,
// or removes it if text is empty.
func (t *projTab) setInflation(proj, text string) {
	var v float64
	if text != "" {
		var err error
		if v, err = parseValue(model.PropInflation, model.ModeSet, text); err != nil {
			t.setErr("inflation", err.Error())
			return
		}
	}
	var val *float64
	if text != "" {
		val = &v
	}
	// SetInflation removes or inserts a modification, which moves the others; find it as ops does, before the edit.
	var mods []model.Modification
	if p := t.a.rec.Projection(proj); p != nil {
		mods = p.Mods
	}
	idx := slices.IndexFunc(mods, func(m model.Modification) bool {
		return isInflation(m) && !m.IsScale()
	})
	if t.a.edit("set inflation", func() (*ops.Result, error) { return ops.SetInflation(t.a.st, t.a.rec, proj, val, ops.Options{}) }) {
		switch {
		case val == nil && idx >= 0:
			t.removed(proj, idx)
		case val != nil && idx < 0:
			at := slices.IndexFunc(mods, func(m model.Modification) bool { return isInflation(m) && m.IsScale() })
			if at < 0 {
				at = len(mods)
			}
			t.inserted(proj, at)
		}
	}
}

func isInflation(m model.Modification) bool {
	return m.Target.Kind == model.KindGlobal && m.Property == model.PropInflation
}

// showOnGraph pins a line under the projection for each asset the Graph tab draws now, and switches to that tab.
func (t *projTab) showOnGraph(proj string) {
	g := t.a.graph
	var assets []model.AssetID
	for _, ln := range g.lines.All() {
		if g.lines.Drawn(ln.ID) && !slices.Contains(assets, ln.Asset) {
			assets = append(assets, ln.Asset)
		}
	}
	for _, id := range assets {
		if slices.ContainsFunc(g.lines.All(), func(ln graph.Line) bool { return ln.Pinned && ln.Asset == id && ln.Projection == proj }) {
			continue
		}
		g.lines.Add(id, true, proj, graph.AssetColor(g.rec, id))
	}
	g.fitY = true
	t.a.forceTab = "Graph"
}

// ---- The modifications.

func (t *projTab) mods(p *model.Projection) {
	rec := t.a.rec
	if t.overwrites == nil {
		t.overwrites, _ = rec.OverwrittenMods(p.Name)
	}
	if t.missing == nil {
		t.missing, _ = rec.MissingTargets(p.Name)
	}
	imgui.TextUnformatted("Modifications, applied in this order; a later one wins where it covers the same thing.")
	flags := imgui.TableFlagsRowBg | imgui.TableFlagsBordersInnerV | imgui.TableFlagsSizingStretchProp
	if imgui.BeginTableV("##mods", 7, flags, imgui.NewVec2(0, 0), 0) {
		imgui.TableSetupColumnV("#", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*2.5, 0)
		imgui.TableSetupColumnV("From", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*9, 0)
		imgui.TableSetupColumnV("Object", 0, 2, 0)
		imgui.TableSetupColumnV("Property", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*6, 0)
		imgui.TableSetupColumnV("How", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*6, 0)
		imgui.TableSetupColumnV("Value", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*9, 0)
		imgui.TableSetupColumnV("##remove", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*2, 0)
		imgui.TableHeadersRow()
		for i := range p.Mods {
			t.modRow(p, i)
		}
		imgui.EndTable()
	}
	if len(p.Mods) == 0 {
		disabledText("No modifications yet.")
	}
	t.marketRows(p)
	t.timelineEditor(p)
	t.itemTimelineEditor(p)
	t.addForm(p)
}

func (t *projTab) modRow(p *model.Projection, i int) {
	rec := t.a.rec
	m := p.Mods[i]
	var ow *model.Overwrite
	if i < len(t.overwrites) {
		ow = t.overwrites[i]
	}
	gone := ""
	if i < len(t.missing) {
		gone = t.missing[i]
	}
	tip := func() {
		if !imgui.IsItemHovered() {
			return
		}
		if gone != "" {
			tooltipText("The " + gone + " no longer exists, so the engine skips this modification. Remove it, or retarget it.")
		} else if ow != nil {
			tooltipText(overwriteText(ow))
		}
	}
	imgui.PushIDInt(int32(i))
	defer imgui.PopID()
	imgui.TableNextRowV(0, 0)
	if gone != "" {
		imgui.PushStyleColorVec4(imgui.ColText, rgba(colError, 1))
		defer imgui.PopStyleColor()
	} else if ow != nil {
		imgui.PushStyleColorVec4(imgui.ColText, *imgui.StyleColorVec4(imgui.ColTextDisabled))
		defer imgui.PopStyleColor()
	}

	imgui.TableSetColumnIndex(0)
	imgui.SelectableBoolV(strconv.Itoa(i+1)+"##drag", false, 0, imgui.NewVec2(0, 0))
	if imgui.BeginDragDropSource() {
		t.dragFrom = i
		imgui.SetDragDropPayload(dragMod, 0, 0)
		imgui.TextUnformatted("Move modification " + strconv.Itoa(i+1))
		imgui.EndDragDropSource()
	}
	if imgui.BeginDragDropTarget() {
		if pl := imgui.AcceptDragDropPayload(dragMod); pl != nil && pl.CData != nil && t.dragFrom != i {
			from := t.dragFrom
			if t.a.edit("reorder modifications", func() (*ops.Result, error) {
				return ops.MoveMod(t.a.st, t.a.rec, p.Name, from, i, ops.Options{})
			}) {
				t.moved(p.Name, from, i)
			}
		}
		imgui.EndDragDropTarget()
	}
	tip()

	v, from, editable := constantOf(m.Values)
	adds := m.Property == model.PropAddItem && m.Item != nil
	grants := m.Property == model.PropAddGrant && m.Grant != nil
	editable = editable && !adds && !grants
	_, ends := ops.EndedOn(m)
	imgui.TableSetColumnIndex(1)
	if editable {
		start := ""
		if from != nil {
			start = from.String()
		}
		if text, done := t.field("from"+strconv.Itoa(i), "always", start, -1); done {
			t.commit("from"+strconv.Itoa(i), func() { t.setModStart(p.Name, i, strings.TrimSpace(text)) })
		}
	} else if grants {
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(m.Grant.Date.String())
	} else {
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(describeWhen(m.Values))
	}
	tip()

	imgui.TableSetColumnIndex(2)
	switch {
	case adds:
		t.itemFields("item"+strconv.Itoa(i), formOf(*m.Item, rec.Today), false, func(field, text string) { t.setItemField(p.Name, i, field, text) }, func(field string) { t.openItemTimeline(p.Name, i, field) })
	case editable:
		kind, obj, prop := m.Target.Kind, m.Target.ID, rowProperty(m)
		if k, changed := kindCombo("kind", kind, imgui.FontSize()*7, objectKinds); changed {
			obj, prop = firstObject(rec, k, p.Name, i), propertiesFor(k)[0]
			t.setModTarget(p.Name, i, k, obj, prop)
		} else if kind != model.KindGlobal {
			imgui.SameLine()
			if o, changed := t.objectCombo("object", kind, p.Name, i, obj, -1); changed {
				t.setModTarget(p.Name, i, kind, o, prop)
			}
		}
	case grants:
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(objectText(effective(rec, p.Name), m.Target) + ": " + focalSummary(*m.Grant))
	default:
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(objectText(effective(rec, p.Name), m.Target))
	}
	tip()

	imgui.TableSetColumnIndex(3)
	switch {
	case adds:
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted("adds item")
	case grants:
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted("adds grant")
	case !adds && !grants:
		if pr, changed := propertyCombo("property", m.Target.Kind, rowProperty(m), -1); changed {
			t.setModTarget(p.Name, i, m.Target.Kind, m.Target.ID, pr)
		}
	default:
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(m.Property)
	}
	tip()

	imgui.TableSetColumnIndex(4)
	switch {
	case adds, grants:
	case !ends:
		cur := cmp.Or(m.Mode, model.ModeSet)
		if k := pick("mode", string(cur), -1, modeNames, slices.Index(modeNames, string(cur))); k >= 0 {
			t.setModMode(p.Name, i, model.Mode(modeNames[k]))
		}
	default:
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(string(cmp.Or(m.Mode, model.ModeSet)))
	}
	tip()

	imgui.TableSetColumnIndex(5)
	switch {
	case editable && ends:
		imgui.AlignTextToFramePadding()
		disabledText("pays nothing")
	case editable:
		if text, done := t.field("value"+strconv.Itoa(i), "", formatValue(m.Property, m.Mode, v), -1); done {
			t.commit("value"+strconv.Itoa(i), func() { t.setModValue(p.Name, i, strings.TrimSpace(text)) })
		}
	case adds, grants:
		imgui.AlignTextToFramePadding()
		disabledText("see object")
	default:
		if imgui.SmallButton("Timeline") {
			t.openTimeline(p.Name, i, m)
		}
		if imgui.IsItemHovered() {
			tooltipText("This value changes over time. Edit it on a timeline under the table.")
		}
	}
	if editable {
		tip()
	}

	imgui.TableSetColumnIndex(6)
	if imgui.SmallButton("x") {
		if t.a.edit("remove modification", func() (*ops.Result, error) {
			return ops.RemoveMod(t.a.st, t.a.rec, p.Name, i, ops.Options{})
		}) {
			t.removed(p.Name, i)
		}
	}
	if imgui.IsItemHovered() {
		tooltipText("Remove this modification")
	}
}

func (t *projTab) setModStart(proj string, i int, text string) {
	var from *model.Date
	if text != "" {
		d, err := parseDate(text)
		if err != nil {
			t.setErr("from"+strconv.Itoa(i), err.Error())
			return
		}
		from = &d
	}
	if _, ends := ops.EndedOn(t.a.rec.Projection(proj).Mods[i]); ends && from == nil {
		t.setErr("from"+strconv.Itoa(i), "enter the date it ends on as "+dateHint)
		return
	}
	v, _, _ := constantOf(t.a.rec.Projection(proj).Mods[i].Values)
	t.a.edit("change modification", func() (*ops.Result, error) {
		return ops.SetModValues(t.a.st, t.a.rec, proj, i, scheduleFrom(v, from), ops.Options{})
	})
}

func (t *projTab) setModValue(proj string, i int, text string) {
	m := t.a.rec.Projection(proj).Mods[i]
	v, err := parseValue(m.Property, m.Mode, text)
	if err != nil {
		t.setErr("value"+strconv.Itoa(i), err.Error())
		return
	}
	_, from, _ := constantOf(t.a.rec.Projection(proj).Mods[i].Values)
	t.a.edit("change modification", func() (*ops.Result, error) {
		return ops.SetModValues(t.a.st, t.a.rec, proj, i, scheduleFrom(v, from), ops.Options{})
	})
}

// modeNames are the modes a modification offers, in the order the pickers list them.
var modeNames = []string{string(model.ModeSet), string(model.ModeScale)}

// setModMode changes how modification i of the projection combines with what lies beneath it, and converts its values to the new mode so the result stays near what it was.
func (t *projTab) setModMode(proj string, i int, mode model.Mode) {
	values := modeValues(t.a.rec, proj, i, mode)
	t.a.edit("change modification", func() (*ops.Result, error) {
		return ops.SetModMode(t.a.st, t.a.rec, proj, i, mode, values, ops.Options{})
	})
}

// modeValues returns the values of modification i of the projection after it changes to mode, keeping each segment's start and end, or nil if its mode does not change. Into scale each value becomes the multiplier 1; into set it becomes the target's base value at the segment's start times the multiplier.
func modeValues(rec *model.Record, proj string, i int, mode model.Mode) *model.Schedule {
	m := rec.Projection(proj).Mods[i]
	if m.IsScale() == (mode == model.ModeScale) {
		return nil
	}
	segs := m.Values.Segments()
	for k := range segs {
		if mode == model.ModeScale {
			segs[k].Value = 1
		} else {
			segs[k].Value *= baseValue(rec, proj, i, segs[k].From)
		}
	}
	s, err := model.NewSchedule(segs...)
	if err != nil {
		return nil
	}
	return &s
}

// baseValue is the value of the property modification i of the projection changes, as the record has it on d: an item's amount or raise rate, an account's stated rate or default balance, an asset's growth setting or default balance, or the global inflation. It is 0 where the record has none.
func baseValue(rec *model.Record, proj string, i int, d model.Date) float64 {
	m := rec.Projection(proj).Mods[i]
	view := effective(rec, proj)
	var v float64
	switch m.Target.Kind {
	case model.KindBudgetItem:
		items, _ := ops.ItemsBefore(rec, proj, i)
		for _, a := range items {
			if a.Item.ID == m.Target.ID {
				v = valueNow(a.Item.Growth, d)
				if m.Property == model.PropAmount {
					v = valueNow(a.Item.Amount, d)
				}
			}
		}
	case model.KindAccount:
		if a := view.Account(model.AccountID(m.Target.ID)); a != nil {
			v = a.StatedRate
			if m.Property == model.PropDefault {
				v = a.Default.At(d)
			}
		}
	case model.KindAsset:
		if a := view.Asset(model.AssetID(m.Target.ID)); a != nil {
			v, _ = a.Params[model.PropGrowth].At(d)
			if m.Property == model.PropDefault {
				v = a.Default.At(d)
				if acct, ok := model.AccountOfDefaultAsset(a.ID); ok {
					v = view.Account(acct).Default.At(d)
				}
			}
		}
	case model.KindStock:
		v, _ = rec.StockGrowthOf(m.Target.ID).At(d)
	case model.KindClass:
		if c := rec.Class(m.Target.ID); c != nil {
			v, _ = c.Return.At(d)
		}
	case model.KindGlobal:
		v, _ = rec.Globals[model.PropInflation].At(d)
	}
	return v
}

func overwriteText(ow *model.Overwrite) string {
	s := fmt.Sprintf("Overwritten by modification %d of %q", ow.Index+1, ow.Projection)
	if ow.Partial {
		return s + ", for some of the accounts it covers."
	}
	return s + "."
}

// objectText names a modification's object for the list.
func objectText(rec *model.Record, o model.ObjectRef) string {
	switch o.Kind {
	case model.KindGlobal:
		return "global"
	case model.KindAccount:
		if a := rec.Account(model.AccountID(o.ID)); a != nil {
			return "account " + a.Name
		}
	case model.KindAsset:
		if a := rec.Asset(model.AssetID(o.ID)); a != nil {
			return "asset " + a.Name
		}
	case model.KindBudgetItem:
		return "budget item " + itemName(rec, o.ID)
	case model.KindStock:
		return "stock " + o.ID
	case model.KindClass:
		if c := rec.Class(o.ID); c != nil {
			return "class " + c.Name
		}
	}
	return string(o.Kind) + " " + o.ID
}

// propertiesFor lists the properties a modification of the kind can change that the engine applies and this tab edits; ending a budget item is one of them.
func propertiesFor(kind model.ObjectKind) []string {
	switch kind {
	case model.KindAccount, model.KindAsset:
		return []string{model.PropGrowth, model.PropDefault}
	case model.KindStock, model.KindClass:
		return []string{model.PropGrowth}
	case model.KindBudgetItem:
		return []string{model.PropAmount, model.PropGrowth, propEndsOn}
	case model.KindGlobal:
		return []string{model.PropInflation}
	}
	return nil
}

type objectChoice struct{ id, name string }

// objectsFor lists what a modification of the kind can aim at when it is at index of the projection's modifications (len for the end of the list); accounts include those the projection's chain defines, and leave out closed ones. Default assets are left out: aiming at the account does the same. A budget item is offered where it exists: the record's, those added by the projection's ancestors, and those its own modifications before index add.
func objectsFor(base *model.Record, kind model.ObjectKind, proj string, index int) []objectChoice {
	rec := effective(base, proj)
	var out []objectChoice
	switch kind {
	case model.KindAccount:
		for _, a := range rec.Accounts {
			if !a.IsClosed() {
				out = append(out, objectChoice{string(a.ID), a.Name})
			}
		}
	case model.KindAsset:
		for _, a := range rec.Assets {
			if !slices.ContainsFunc(rec.Accounts, func(c *model.Account) bool { return model.DefaultAssetID(c.ID) == a.ID }) {
				out = append(out, objectChoice{string(a.ID), a.Name})
			}
		}
	case model.KindStock:
		for _, s := range rec.Symbols() {
			out = append(out, objectChoice{s, s})
		}
	case model.KindClass:
		for _, c := range rec.Classes {
			out = append(out, objectChoice{c.ID, c.Name})
		}
	case model.KindBudgetItem:
		items, _ := ops.ItemsBefore(base, proj, index)
		for _, it := range items {
			name := it.Item.Name
			if it.AddedBy != "" {
				name += " (added by " + it.AddedBy + ")"
			}
			out = append(out, objectChoice{it.Item.ID, name})
		}
	}
	return out
}

// firstObject is the object a modification switched to the kind starts with: the first one it can aim at.
func firstObject(rec *model.Record, kind model.ObjectKind, proj string, index int) string {
	if c := objectsFor(rec, kind, proj, index); len(c) > 0 {
		return c[0].id
	}
	return ""
}

// ---- The pickers, which the rows and the Add form share.

var objectKinds = []model.ObjectKind{model.KindAccount, model.KindAsset, model.KindBudgetItem, model.KindStock, model.KindGlobal}

// pick draws a combo previewing preview with an entry for each label and returns the index of the one chosen, else -1. A width of -1 fills the cell.
func pick(id, preview string, width float32, labels []string, selected int) int {
	return pickKinds(id, preview, iconNone, width, labels, nil, selected)
}

// pickKinds is pick with an icon in front of each label for its kind in kinds, which is nil for none, and in front of the preview, whose kind is previewKind. An entry of iconNone has no icon.
func pickKinds(id, preview string, previewKind iconKind, width float32, labels []string, kinds []iconKind, selected int) int {
	chosen := -1
	dl := imgui.WindowDrawList()
	start := imgui.CursorScreenPos()
	if takeOpen(id) {
		openComboPopup("##" + id)
	}
	imgui.SetNextItemWidth(width)
	frame := imgui.CurrentStyle().FramePadding()
	open := imgui.BeginComboV("##"+id, iconLabel(previewKind, preview), 0)
	// The combo's popup is a window of its own once open, so the preview's icon goes into the list saved before.
	cy := start.Y + frame.Y + imgui.FontSize()/2
	drawKindIcon(dl, start.X+frame.X, cy, previewKind, iconColor())
	if !open {
		hoverIcon(start.X+frame.X, cy, previewKind)
		return chosen
	}
	seenAsset := false
	for i, l := range labels {
		kind := iconNone
		if kinds != nil {
			kind = kinds[i]
		}
		if imgui.SelectableBoolV(iconLabel(kind, l)+"##"+strconv.Itoa(i), i == selected, 0, imgui.NewVec2(0, 0)) && i != selected {
			chosen = i
		}
		itemIcon(selectableTextX(), kind)
		scrollToAssets(id, kind, &seenAsset)
	}
	imgui.EndCombo()
	return chosen
}

func kindCombo(id string, cur model.ObjectKind, width float32, kinds []model.ObjectKind) (model.ObjectKind, bool) {
	labels := make([]string, len(kinds))
	for i, k := range kinds {
		labels[i] = string(k)
	}
	if k := pick(id, string(cur), width, labels, slices.Index(kinds, cur)); k >= 0 {
		return kinds[k], true
	}
	return cur, false
}

// objectIconKind is the icon kind of the object of a modification of the kind: accounts and assets have one, budget items and the global object none.
func objectIconKind(kind model.ObjectKind, id string) iconKind {
	switch kind {
	case model.KindAccount:
		return iconAccount
	case model.KindAsset:
		return assetKind(model.AssetID(id))
	}
	return iconNone
}

// objectCombo picks the object of a modification of the kind at index of the projection's modifications.
func (t *projTab) objectCombo(id string, kind model.ObjectKind, proj string, index int, cur string, width float32) (string, bool) {
	choices := objectsFor(t.a.rec, kind, proj, index)
	preview := "choose an object"
	if cur != "" {
		preview = objectText(effective(t.a.rec, proj), model.ObjectRef{Kind: kind, ID: cur})
	}
	labels := make([]string, len(choices))
	kinds := make([]iconKind, len(choices))
	sel := -1
	for i, c := range choices {
		labels[i] = c.name
		kinds[i] = objectIconKind(kind, c.id)
		if c.id == cur {
			sel, preview = i, c.name
		}
	}
	previewKind := iconNone
	if cur != "" {
		previewKind = objectIconKind(kind, cur)
	}
	if k := pickKinds(id, preview, previewKind, width, labels, kinds, sel); k >= 0 {
		return choices[k].id, true
	}
	return cur, false
}

func propertyCombo(id string, kind model.ObjectKind, cur string, width float32) (string, bool) {
	props := propertiesFor(kind)
	if k := pick(id, cur, width, props, slices.Index(props, cur)); k >= 0 {
		return props[k], true
	}
	return cur, false
}

// itemFields draws the fields of a budget item. set is called with the field and its new text or choice: at every keystroke if live, else once the user finishes a text field. id prefixes the field ids. If timeline is not nil, amount and growth get a button that calls it with "amount" or "growth" to edit that property as a schedule; an amount or growth that already changes over time shows its value today instead of a text field.
func (t *projTab) itemFields(id string, f itemForm, live bool, set func(field, text string), timeline func(field string)) {
	em := imgui.FontSize()
	input := func(field, label, hint, value string, width float32) {
		fid := id + field
		imgui.AlignTextToFramePadding()
		disabledText(label)
		imgui.SameLine()
		if live {
			s := value
			imgui.SetNextItemWidth(width)
			if imgui.InputTextWithHint("##"+fid, hint, &s, 0, nil) {
				set(field, s)
			}
			return
		}
		if s, done := t.field(fid, hint, value, width); done {
			t.commit(fid, func() { set(field, strings.TrimSpace(s)) })
		}
	}
	choose := func(field, label, preview string, width float32, values, names []string, cur string) {
		imgui.AlignTextToFramePadding()
		if label != "" {
			disabledText(label)
			imgui.SameLine()
		}
		if k := pick(id+field, preview, width, names, slices.Index(values, cur)); k >= 0 {
			set(field, values[k])
		}
		if e := t.errs[id+field]; e != "" && !live {
			imgui.SameLine()
			errorText("Not changed: " + e)
		}
	}
	// scheduled shows the property as a value today and a button that opens its timeline, or draws input and, in an editor, the button that turns it into a schedule.
	scheduled := func(field, label, hint, value string, width float32, sched bool) {
		if sched {
			imgui.AlignTextToFramePadding()
			disabledText(label)
			imgui.SameLine()
			imgui.TextUnformatted(value + " now, changes over time")
		} else {
			input(field, label, hint, value, width)
		}
		if timeline == nil {
			return
		}
		imgui.SameLine()
		if imgui.SmallButton("timeline##" + id + field) {
			timeline(field)
		}
		tooltipOn("Edit this as a schedule on a timeline: a different " + field + " over different periods.")
	}
	strs := func(n int, at func(i int) string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = at(i)
		}
		return out
	}
	kinds := strs(len(formItemKinds), func(i int) string { return string(formItemKinds[i]) })
	kindLabels := strs(len(formItemKinds), func(i int) string { return itemKindName(formItemKinds[i]) })
	choose("kind", "kind", itemKindName(f.kind), em*7, kinds, kindLabels, string(f.kind))
	imgui.SameLine()
	input("name", "name", "name", f.name, em*14)
	scheduled("amount", "amount", "e.g. 1500", f.amount, em*8, f.amountSched)
	imgui.SameLine()
	every := strs(len(formRecurrences), func(i int) string { return string(formRecurrences[i].kind) })
	labels := strs(len(formRecurrences), func(i int) string { return formRecurrences[i].name + ": " + formRecurrences[i].hint })
	choose("every", "", recurrenceChoiceOf(f.every).name, em*8, every, labels, string(f.every))
	switch f.every {
	case model.OneTime:
		input("date", "on", dateHint, f.date, em*8)
	case model.Biweekly:
		input("date", "from", dateHint, f.date, em*8)
	case model.Weekly, model.Quarterly:
		input("date", "from", "today", f.date, em*8)
	case model.Semimonthly:
		input("day", "on days", "1-31", f.day, em*4)
		imgui.SameLine()
		input("day2", "and", "1-31", f.day2, em*4)
		imgui.SameLine()
		input("date", "from", "today", f.date, em*8)
	case model.Monthly:
		input("day", "on day", "1-31", f.day, em*4)
		imgui.SameLine()
		input("date", "from", "today", f.date, em*8)
	case model.Annual:
		input("month", "in month", "1-12", f.month, em*4)
		imgui.SameLine()
		input("day", "day", "1-31", f.day, em*4)
		imgui.SameLine()
		input("date", "from", "today", f.date, em*8)
	case model.Custom:
		input("count", "every", "N", f.count, em*4)
		imgui.SameLine()
		units := strs(len(formUnits), func(i int) string { return string(formUnits[i]) })
		choose("unit", "unit", string(f.unit), em*8, units, units, string(f.unit))
		imgui.SameLine()
		input("date", "from", "today", f.date, em*8)
	}
	disabledText(recurrenceChoiceOf(f.every).hint)
	input("end", "until", "no end", f.end, em*8)
	imgui.SameLine()
	scheduled("growth", "raise per year", "percent, e.g. 3", f.growth, em*6, f.growthSched)

	rec := effective(t.a.rec, t.sel)
	ids, names := itemAccountChoices(rec)
	account := func(field, label string, cur model.AccountID) {
		// Only a transfer may name the outside; the first entry is no choice.
		from := 2
		if f.kind == model.ItemTransfer {
			from = 1
		}
		choose(field, label, itemAccountPreview(rec, ids, names, cur), em*12, ids[from:], names[from:], string(cur))
	}
	if f.kind == model.ItemTransfer {
		account("from", "from", f.from)
		imgui.SameLine()
		account("to", "to", f.to)
	} else {
		account("account", "account", f.account)
	}
	if f.kind == model.ItemIncome {
		imgui.SameLine()
		incomes := strs(len(model.IncomeTypes), func(i int) string { return string(model.IncomeTypes[i]) })
		choose("income", "taxed as", incomeTypeName((model.BudgetItem{IncomeType: f.income}).Income()), em*12, incomes, incomeTypeLabels, string((model.BudgetItem{IncomeType: f.income}).Income()))
	}
	if f.kind == model.ItemIncome && (f.income == "" || f.income == model.IncomeWages) {
		input("matchpercent", "employer matches", "percent, e.g. 50", f.matchPercent, em*6)
		imgui.SameLine()
		input("matchupto", "of contributions up to", "% of pay", f.matchUpTo, em*6)
		mids, mnames := []string{""}, []string{"no match"}
		for _, a := range rec.Accounts {
			if (a.Type == model.Type401k || a.Type == model.TypeTSP) && !a.IsClosed() {
				mids, mnames = append(mids, string(a.ID)), append(mnames, a.Name)
			}
		}
		imgui.SameLine()
		choose("matchaccount", "into", itemAccountPreview(rec, mids, mnames, f.matchAccount), em*12, mids, mnames, string(f.matchAccount))
	}
}

// itemAccountPreview is the text the account picker shows for cur: its name in the picker, else the name of an account rec has that the picker does not offer (a closed one), else the raw id.
func itemAccountPreview(rec *model.Record, ids, names []string, cur model.AccountID) string {
	if i := slices.Index(ids, string(cur)); i > 0 {
		return names[i]
	}
	if a := rec.Account(cur); a != nil {
		return a.Name
	}
	if cur != "" {
		return string(cur)
	}
	return names[0]
}

// itemAccountChoices lists what a budget item's account pickers offer: a first entry that is no choice, the outside, and the open accounts of rec, which is the record as a projection sees it.
func itemAccountChoices(rec *model.Record) (ids, names []string) {
	ids, names = []string{"", string(outsideAccount)}, []string{"choose an account", "outside the record"}
	for _, a := range rec.Accounts {
		if !a.IsClosed() {
			ids, names = append(ids, string(a.ID)), append(names, a.Name)
		}
	}
	return ids, names
}

func (t *projTab) addForm(p *model.Projection) {
	rec := t.a.rec
	imgui.Spacing()
	imgui.SeparatorText("Add to this projection")
	modes := []string{"a modification", "a new budget item"}
	mode := 0
	if t.addNew {
		mode = 1
	}
	t.label("Add")
	if k := pick("addmode", modes[mode], imgui.FontSize()*16, modes, mode); k >= 0 {
		t.addNew = k == 1
	}
	if t.addNew {
		if t.addItem.kind == "" {
			t.addItem = newItemForm()
		}
		t.itemFields("newitem", t.addItem, true, t.addItem.set, nil)
		formButton("Add##newitem", itemProblem(effective(rec, p.Name), t.addItem), t.addItem.untouched(), func() { t.addNewItem(p.Name) })
		return
	}

	if t.addKind == "" {
		t.addKind = model.KindAccount
	}
	t.label("Object kind")
	if k, changed := kindCombo("addkind", t.addKind, imgui.FontSize()*16, addKinds); changed {
		t.addKind, t.addObj, t.addProp = k, "", ""
	}
	switch t.addKind {
	case kindFocalGrant:
		t.focalFields(p)
		return
	case kindShock:
		t.shockFields(p)
		return
	case kindRebalance:
		t.rebalanceFields(p)
		return
	}
	if t.addKind != model.KindGlobal {
		t.label("Object")
		t.addObj, _ = t.objectCombo("addobj", t.addKind, p.Name, len(p.Mods), t.addObj, imgui.FontSize()*24)
	}
	if !slices.Contains(propertiesFor(t.addKind), t.addProp) {
		t.addProp = propertiesFor(t.addKind)[0]
	}
	t.label("Property")
	t.addProp, _ = propertyCombo("addprop", t.addKind, t.addProp, imgui.FontSize()*16)

	if t.addProp != propEndsOn {
		if t.addMode == "" {
			t.addMode = model.ModeSet
		}
		t.label("How")
		if k := pick("addhow", string(t.addMode), imgui.FontSize()*16, modeNames, slices.Index(modeNames, string(t.addMode))); k >= 0 {
			t.addMode = model.Mode(modeNames[k])
		}
	}
	if t.addProp == propEndsOn {
		t.label("Date")
		imgui.SetNextItemWidth(imgui.FontSize() * 16)
		imgui.InputTextWithHint("##addstart", dateHint, &t.addStart, 0, nil)
	} else {
		t.label("Value")
		imgui.SetNextItemWidth(imgui.FontSize() * 16)
		imgui.InputTextWithHint("##addvalue", valueHint(t.addProp, t.addMode), &t.addValue, 0, nil)
		t.label("From (optional)")
		imgui.SetNextItemWidth(imgui.FontSize() * 16)
		imgui.InputTextWithHint("##addstart", "always; or "+dateHint, &t.addStart, 0, nil)
		t.label("To (optional)")
		imgui.SetNextItemWidth(imgui.FontSize() * 16)
		imgui.InputTextWithHint("##addend", "no end; or "+dateHint+", needs a start", &t.addEnd, 0, nil)
	}
	formButton("Add##addmod", modProblem(rec, p.Name, t.addKind, t.addObj, t.addProp, t.addMode, t.addValue, t.addStart, t.addEnd), modUntouched(t.addObj, t.addValue, t.addStart, t.addEnd), func() { t.addMod(p.Name) })
}

// actionButton draws a button labelled label, disabled with the reason beside it while problem is not empty.
func actionButton(label, problem string, do func()) { formButton(label, problem, false, do) }

// formButton is actionButton for a form: while untouched the button is disabled but the reason is not shown, since nothing has been typed to be wrong.
func formButton(label, problem string, untouched bool, do func()) {
	imgui.BeginDisabledV(problem != "")
	if imgui.Button(label) {
		do()
	}
	imgui.EndDisabled()
	if problem != "" && !untouched {
		errorBeside("Not yet: " + problem)
	}
}

func valueHint(prop string, mode model.Mode) string {
	if mode == model.ModeScale {
		return "percent change, e.g. -30"
	}
	if prop == model.PropAmount {
		return "amount, e.g. 1500"
	}
	if prop == model.PropDefault {
		return "balance, e.g. 25000"
	}
	return "percent, e.g. 4"
}

// addMod adds the modification the Add form describes, unless modProblem finds one. It reports whether it did.
func (t *projTab) addMod(proj string) bool {
	if modProblem(t.a.rec, proj, t.addKind, t.addObj, t.addProp, t.addMode, t.addValue, t.addStart, t.addEnd) != "" {
		return false
	}
	m, _ := newMod(t.addKind, t.addObj, t.addProp, t.addMode, t.addValue, t.addStart, t.addEnd)
	if !t.a.edit("add modification", func() (*ops.Result, error) {
		if from, ends := ops.EndedOn(m); ends && t.addProp == propEndsOn {
			return ops.EndItem(t.a.st, t.a.rec, proj, t.addObj, from, ops.Options{})
		}
		return ops.AddMod(t.a.st, t.a.rec, proj, m, ops.Options{})
	}) {
		return false
	}
	t.addValue, t.addStart, t.addEnd = "", "", ""
	return true
}

// ---- Parsing and formatting.

// newMod builds the modification the Add form describes: the value from the optional start date to the optional end date (inclusive, and needing a start), or for propEndsOn what ops.EndItem stores, from the date in start. ops validates it.
func newMod(kind model.ObjectKind, obj, prop string, mode model.Mode, value, start, end string) (model.Modification, error) {
	if prop == propEndsOn {
		d, err := needDate(start, "the date it ends on")
		if err != nil {
			return model.Modification{}, err
		}
		return ops.EndMod(obj, d), nil
	}
	if strings.TrimSpace(value) == "" {
		return model.Modification{}, errors.New("enter a value")
	}
	v, err := parseValue(prop, mode, strings.TrimSpace(value))
	if err != nil {
		return model.Modification{}, err
	}
	var from *model.Date
	if s := strings.TrimSpace(start); s != "" {
		d, err := parseDate(s)
		if err != nil {
			return model.Modification{}, err
		}
		from = &d
	}
	values := scheduleFrom(v, from)
	if s := strings.TrimSpace(end); s != "" {
		if from == nil {
			return model.Modification{}, errors.New("an end date needs a start date")
		}
		d, err := parseDate(s)
		if err != nil {
			return model.Modification{}, err
		}
		if values, err = model.NewSchedule(model.Segment{From: *from, End: &d, Value: v}); err != nil {
			return model.Modification{}, err
		}
	}
	return model.Modification{Target: model.ObjectRef{Kind: kind, ID: obj}, Property: prop, Mode: mode, Values: values}, nil
}

// parseDate reads a date written as YYYY-MM-DD.
func parseDate(s string) (model.Date, error) {
	tm, err := time.Parse(dateLayout, s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a date; write it as %s", s, dateHint)
	}
	y, m, d := tm.Date()
	return model.D(y, m, d), nil
}

// parseValue reads a property's value: a percentage for rates (the % sign is optional), else a number (an amount or a default balance). In scale mode the value is the percentage change, so -30 reads as the multiplier 0.7.
func parseValue(prop string, mode model.Mode, s string) (float64, error) {
	rate := prop != model.PropAmount && prop != model.PropDefault
	num := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v != v || v > 1e15 || v < -1e15 {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	switch {
	case mode == model.ModeScale:
		v = math.Round((1+v/100)*1e9) / 1e9
	case rate:
		v /= 100
	}
	return v, nil
}

// formatValue is parseValue's inverse.
func formatValue(prop string, mode model.Mode, v float64) string {
	if mode == model.ModeScale {
		return trimFloat((v-1)*100) + "%"
	}
	if prop == model.PropAmount || prop == model.PropDefault {
		return trimFloat(v)
	}
	return trimFloat(v*100) + "%"
}

// trimFloat writes v with at most four decimals and no trailing zeros.
func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		return "0"
	}
	return s
}

// constantOf reports the value and start of a schedule of one segment without an end, the only kind this tab edits. The
// start is nil if the segment starts at the beginning of time.
func constantOf(s model.Schedule) (v float64, from *model.Date, ok bool) {
	segs := s.Segments()
	if len(segs) != 1 || segs[0].End != nil {
		return 0, nil, false
	}
	if segs[0].From != model.MinDate {
		from = model.Ptr(segs[0].From)
	}
	return segs[0].Value, from, true
}

// scheduleFrom returns the schedule that holds v from the date on, or always if from is nil.
func scheduleFrom(v float64, from *model.Date) model.Schedule {
	if from == nil {
		return model.Constant(v)
	}
	return model.MustSchedule(model.Segment{From: *from, Value: v})
}

// describeWhen says when a schedule has values; a start at the beginning of time reads "the beginning", never "-inf".
func describeWhen(s model.Schedule) string {
	segs := s.Segments()
	if len(segs) == 0 {
		return "never"
	}
	from := "the beginning"
	if segs[0].From != model.MinDate {
		from = segs[0].From.String()
	}
	switch {
	case len(segs) == 1 && segs[0].End == nil && segs[0].From == model.MinDate:
		return "always"
	case len(segs) == 1 && segs[0].End == nil:
		return "from " + from
	case len(segs) == 1 && segs[0].From == model.MinDate:
		return "until " + segs[0].End.String()
	case len(segs) == 1:
		return from + " to " + segs[0].End.String()
	}
	return fmt.Sprintf("%d periods from %s", len(segs), from)
}

// disabledText draws msg in the disabled colour as plain text; TextDisabled takes a format string, which a rate such as "4%" would corrupt.
func disabledText(msg string) {
	imgui.PushStyleColorVec4(imgui.ColText, *imgui.StyleColorVec4(imgui.ColTextDisabled))
	imgui.TextUnformatted(msg)
	imgui.PopStyleColor()
}

// wrappedText draws msg wrapped at the right edge of the window as plain text; TextWrapped takes a format string.
func wrappedText(msg string) {
	imgui.PushTextWrapPos()
	imgui.TextUnformatted(msg)
	imgui.PopTextWrapPos()
}

// bulletText draws a bullet and msg as plain text; BulletText takes a format string, and msg quotes user input.
func bulletText(msg string) {
	imgui.Bullet()
	imgui.TextUnformatted(msg)
}

// errorText draws msg in the error colour as plain text; msg quotes user input, so it must never be a format string.
func errorText(msg string) {
	imgui.PushStyleColorVec4(imgui.ColText, rgba(colError, 1))
	imgui.TextUnformatted(msg)
	imgui.PopStyleColor()
}

// errorBeside shows msg after the previous item when it fits on that line, otherwise under it, where a wrapping panel can wrap it across its full width.
func errorBeside(msg string) {
	spacing := imgui.CurrentStyle().ItemSpacing().X
	room := imgui.CursorScreenPos().X + imgui.ContentRegionAvail().X - imgui.ItemRectMax().X - spacing
	if fitsBeside(room, imgui.CalcTextSize(msg).X) {
		imgui.SameLine()
	}
	errorText(msg)
}

// fitsBeside reports whether text, width textW, fits in room, the width left on the line of the previous item.
func fitsBeside(room, textW float32) bool {
	return textW <= room
}

// tooltipText shows msg as a tooltip without treating it as a format string.
func tooltipText(msg string) {
	if imgui.BeginTooltip() {
		imgui.TextUnformatted(msg)
		imgui.EndTooltip()
	}
}
