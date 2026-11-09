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

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

const (
	dateLayout = "2006-01-02"
	// dragMod names the drag and drop payload of a modification row; the index of the row is in projTab.dragFrom.
	dragMod = "projection modification"
	// colError is the colour of messages about input that was refused.
	colError = 0xE06C6C
)

// projTab is the Projections tab: the projection list on the left, the selected projection's header and
// modifications on the right. Every change goes through app.edit.
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

	// The Add modification form.
	addKind  model.ObjectKind
	addObj   string
	addProp  string
	addValue string
	addStart string
	addErr   string
}

func (t *projTab) reset() {
	t.bufs, t.errs, t.overwrites = map[string]string{}, map[string]string{}, nil
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
	if t.a.edit("add projection", nil, func(rec *model.Record) error {
		return rec.AddProjection(&model.Projection{Name: name, Parent: parent})
	}) {
		t.selectProjection(name)
	}
}

func (t *projTab) cloneProjection() {
	from := t.sel
	name := t.a.rec.UniqueProjectionName(from + " copy")
	if t.a.edit("clone projection", nil, func(rec *model.Record) error {
		_, err := rec.CloneProjection(from, name)
		return err
	}) {
		t.selectProjection(name)
	}
}

// requestDelete asks for confirmation, or says why the projection cannot be deleted.
func (t *projTab) requestDelete() {
	if kids := t.a.rec.Children(t.sel); len(kids) > 0 {
		t.a.notify(fmt.Sprintf("%q cannot be deleted while %q builds on it. Delete it or change what it builds on first.", t.sel, kids[0].Name))
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
	imgui.TextWrapped(fmt.Sprintf("Delete the projection %q? Undo brings it back.", t.deleting))
	if imgui.Button("Delete") {
		name := t.deleting
		if p := t.a.rec.Projection(name); p != nil {
			parent := p.Parent
			if t.a.edit("delete projection", nil, func(rec *model.Record) error { return rec.RemoveProjection(name) }) {
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
		imgui.TextWrapped("Reality is the record as it stands, with no projection. It cannot be edited. Select a projection, or add one with New.")
		return
	}
	if e := t.errs[""]; e != "" {
		imgui.TextColored(rgba(colError, 1), "Not changed: "+e)
	}
	t.header(p)
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
		imgui.SameLine()
		imgui.TextColored(rgba(colError, 1), "Not changed: "+e)
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
				t.a.editProjection(p.Name, "set override history", func(p *model.Projection) error { p.OverrideHistory = v; return nil })
			}
		}
		imgui.EndCombo()
	}

	t.label("Inflation")
	t.inflation(p, w)

	if imgui.Button("Show on graph") {
		t.showOnGraph(p.Name)
	}
}

func (t *projTab) setStart(proj, text string) {
	var d *model.Date
	if text != "" {
		v, err := parseDate(text)
		if err != nil {
			t.errs["start"] = err.Error()
			return
		}
		d = &v
	}
	t.a.editProjection(proj, "set start date", func(p *model.Projection) error { p.Start = d; return nil })
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
	switch {
	case to == from:
		t.dropBuf("name")
	case to == "":
		t.errs["name"] = "A projection needs a name."
	case t.a.rec.Projection(to) != nil:
		t.errs["name"] = fmt.Sprintf("There is already a projection named %q.", to)
	default:
		if t.a.edit("rename projection", &renameOp{from, to}, func(rec *model.Record) error { return rec.RenameProjection(from, to) }) {
			t.sel = to
		}
	}
}

func (t *projTab) setParent(name, parent string) {
	err := t.a.rec.EditableCopy().SetParent(name, parent)
	if errors.Is(err, model.ErrCycle) {
		t.errs[""] = fmt.Sprintf("%q cannot build on %q: that would make a cycle.", name, parent)
		return
	}
	t.a.edit("change parent", nil, func(rec *model.Record) error { return rec.SetParent(name, parent) })
}

// inflation draws the projection's inflation: a percentage when it has one constant inflation modification or none,
// else read-only.
func (t *projTab) inflation(p *model.Projection, w float32) {
	idx := slices.IndexFunc(p.Mods, isInflation)
	text := ""
	if idx >= 0 {
		v, from, ok := constantOf(p.Mods[idx].Values)
		if !ok || from != nil || slices.IndexFunc(p.Mods[idx+1:], isInflation) >= 0 {
			imgui.AlignTextToFramePadding()
			imgui.TextDisabled("varies; the timeline editor comes next")
			return
		}
		text = formatValue(model.PropInflation, v)
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
		if v, err = parseValue(model.PropInflation, text); err != nil {
			t.errs["inflation"] = err.Error()
			return
		}
	}
	t.a.editProjection(proj, "set inflation", func(p *model.Projection) error {
		idx := slices.IndexFunc(p.Mods, isInflation)
		switch {
		case text == "" && idx >= 0:
			return p.RemoveMod(idx)
		case text == "":
		case idx >= 0:
			p.Mods[idx].Values = model.Constant(v)
		default:
			p.Mods = append(p.Mods, model.Modification{Target: model.ObjectRef{Kind: model.KindGlobal}, Property: model.PropInflation, Values: model.Constant(v)})
		}
		return nil
	})
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
	t.a.showGraph = true
}

// ---- The modifications.

func (t *projTab) mods(p *model.Projection) {
	rec := t.a.rec
	if t.overwrites == nil {
		t.overwrites, _ = rec.OverwrittenMods(p.Name)
	}
	imgui.TextUnformatted("Modifications, applied in this order; a later one wins where it covers the same thing.")
	flags := imgui.TableFlagsRowBg | imgui.TableFlagsBordersInnerV | imgui.TableFlagsSizingStretchProp
	if imgui.BeginTableV("##mods", 6, flags, imgui.NewVec2(0, 0), 0) {
		imgui.TableSetupColumnV("#", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*2.5, 0)
		imgui.TableSetupColumnV("From", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*9, 0)
		imgui.TableSetupColumnV("Object", 0, 2, 0)
		imgui.TableSetupColumnV("Property", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*6, 0)
		imgui.TableSetupColumnV("Value", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*9, 0)
		imgui.TableSetupColumnV("##remove", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*2, 0)
		imgui.TableHeadersRow()
		for i := range p.Mods {
			t.modRow(p, i)
		}
		imgui.EndTable()
	}
	if len(p.Mods) == 0 {
		imgui.TextDisabled("No modifications yet.")
	}
	t.addForm(p)
}

func (t *projTab) modRow(p *model.Projection, i int) {
	rec := t.a.rec
	m := p.Mods[i]
	var ow *model.Overwrite
	if i < len(t.overwrites) {
		ow = t.overwrites[i]
	}
	tip := func() {
		if ow != nil && imgui.IsItemHovered() {
			imgui.SetTooltip(overwriteText(ow))
		}
	}
	imgui.PushIDInt(int32(i))
	defer imgui.PopID()
	imgui.TableNextRowV(0, 0)
	if ow != nil {
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
			t.a.editProjection(p.Name, "reorder modifications", func(p *model.Projection) error { return p.MoveMod(from, i) })
		}
		imgui.EndDragDropTarget()
	}
	tip()

	v, from, editable := constantOf(m.Values)
	editable = editable && m.Property != model.PropAddItem
	imgui.TableSetColumnIndex(1)
	if editable {
		start := ""
		if from != nil {
			start = from.String()
		}
		if text, done := t.field("from"+strconv.Itoa(i), "always", start, -1); done {
			t.commit("from"+strconv.Itoa(i), func() { t.setModStart(p.Name, i, strings.TrimSpace(text)) })
		}
	} else {
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(describeWhen(m.Values))
	}
	tip()

	imgui.TableSetColumnIndex(2)
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(objectText(rec, m.Target))
	tip()

	imgui.TableSetColumnIndex(3)
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(m.Property)
	tip()

	imgui.TableSetColumnIndex(4)
	switch {
	case editable:
		if text, done := t.field("value"+strconv.Itoa(i), "", formatValue(m.Property, v), -1); done {
			t.commit("value"+strconv.Itoa(i), func() { t.setModValue(p.Name, i, strings.TrimSpace(text)) })
		}
	case m.Property == model.PropAddItem:
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted("adds an item")
		if imgui.IsItemHovered() {
			imgui.SetTooltip("Budget item additions are edited with the timeline editor, which comes next.")
		}
	default:
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted("varies")
		if imgui.IsItemHovered() {
			imgui.SetTooltip("This value changes over time. The timeline editor, which comes next, edits it.")
		}
	}
	if editable {
		tip()
	}

	imgui.TableSetColumnIndex(5)
	if imgui.SmallButton("x") {
		t.a.editProjection(p.Name, "remove modification", func(p *model.Projection) error { return p.RemoveMod(i) })
	}
	if imgui.IsItemHovered() {
		imgui.SetTooltip("Remove this modification")
	}
}

func (t *projTab) setModStart(proj string, i int, text string) {
	var from *model.Date
	if text != "" {
		d, err := parseDate(text)
		if err != nil {
			t.errs["from"+strconv.Itoa(i)] = err.Error()
			return
		}
		from = &d
	}
	t.a.editProjection(proj, "change modification", func(p *model.Projection) error {
		v, _, _ := constantOf(p.Mods[i].Values)
		p.Mods[i].Values = scheduleFrom(v, from)
		return nil
	})
}

func (t *projTab) setModValue(proj string, i int, text string) {
	prop := t.a.rec.Projection(proj).Mods[i].Property
	v, err := parseValue(prop, text)
	if err != nil {
		t.errs["value"+strconv.Itoa(i)] = err.Error()
		return
	}
	t.a.editProjection(proj, "change modification", func(p *model.Projection) error {
		_, from, _ := constantOf(p.Mods[i].Values)
		p.Mods[i].Values = scheduleFrom(v, from)
		return nil
	})
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
		if i := slices.IndexFunc(rec.Budget.Items, func(b model.BudgetItem) bool { return b.ID == o.ID }); i >= 0 {
			return "budget item " + rec.Budget.Items[i].Name
		}
	}
	return string(o.Kind) + " " + o.ID
}

// propertiesFor lists the properties a modification of the kind can change that the engine applies and this tab edits.
func propertiesFor(kind model.ObjectKind) []string {
	switch kind {
	case model.KindAccount, model.KindAsset:
		return []string{model.PropGrowth}
	case model.KindBudgetItem:
		return []string{model.PropAmount}
	case model.KindGlobal:
		return []string{model.PropInflation}
	}
	return nil
}

type objectChoice struct{ id, name string }

// objectsFor lists what a modification of the kind can aim at. Default assets are left out: aiming at the account does the same.
func objectsFor(rec *model.Record, kind model.ObjectKind) []objectChoice {
	var out []objectChoice
	switch kind {
	case model.KindAccount:
		for _, a := range rec.Accounts {
			out = append(out, objectChoice{string(a.ID), a.Name})
		}
	case model.KindAsset:
		for _, a := range rec.Assets {
			if !slices.ContainsFunc(rec.Accounts, func(c *model.Account) bool { return model.DefaultAssetID(c.ID) == a.ID }) {
				out = append(out, objectChoice{string(a.ID), a.Name})
			}
		}
	case model.KindBudgetItem:
		for _, b := range rec.Budget.Items {
			out = append(out, objectChoice{b.ID, b.Name})
		}
	}
	return out
}

func (t *projTab) addForm(p *model.Projection) {
	rec := t.a.rec
	imgui.Spacing()
	imgui.SeparatorText("Add a modification")
	kinds := []model.ObjectKind{model.KindAccount, model.KindAsset, model.KindBudgetItem, model.KindGlobal}
	if t.addKind == "" {
		t.addKind = model.KindAccount
	}
	t.label("Object kind")
	imgui.SetNextItemWidth(imgui.FontSize() * 16)
	if imgui.BeginComboV("##addkind", string(t.addKind), 0) {
		for _, k := range kinds {
			if imgui.SelectableBoolV(string(k), k == t.addKind, 0, imgui.NewVec2(0, 0)) && k != t.addKind {
				t.addKind, t.addObj, t.addProp = k, "", ""
			}
		}
		imgui.EndCombo()
	}

	choices := objectsFor(rec, t.addKind)
	if t.addKind != model.KindGlobal {
		t.label("Object")
		preview := t.addObj
		for _, c := range choices {
			if c.id == t.addObj {
				preview = c.name
			}
		}
		imgui.SetNextItemWidth(imgui.FontSize() * 16)
		if imgui.BeginComboV("##addobj", preview, 0) {
			for _, c := range choices {
				if imgui.SelectableBoolV(c.name+"##"+c.id, c.id == t.addObj, 0, imgui.NewVec2(0, 0)) {
					t.addObj = c.id
				}
			}
			imgui.EndCombo()
		}
	}

	props := propertiesFor(t.addKind)
	if !slices.Contains(props, t.addProp) {
		t.addProp = props[0]
	}
	t.label("Property")
	imgui.SetNextItemWidth(imgui.FontSize() * 16)
	if imgui.BeginComboV("##addprop", t.addProp, 0) {
		for _, pr := range props {
			if imgui.SelectableBoolV(pr, pr == t.addProp, 0, imgui.NewVec2(0, 0)) {
				t.addProp = pr
			}
		}
		imgui.EndCombo()
	}

	t.label("Value")
	imgui.SetNextItemWidth(imgui.FontSize() * 16)
	imgui.InputTextWithHint("##addvalue", valueHint(t.addProp), &t.addValue, 0, nil)
	t.label("From (optional)")
	imgui.SetNextItemWidth(imgui.FontSize() * 16)
	imgui.InputTextWithHint("##addstart", "always; or "+dateLayout, &t.addStart, 0, nil)

	if imgui.Button("Add") {
		t.addMod(p.Name)
	}
	if t.addErr != "" {
		imgui.SameLine()
		imgui.TextColored(rgba(colError, 1), "Not added: "+t.addErr)
	}
}

func valueHint(prop string) string {
	if prop == model.PropAmount {
		return "amount, e.g. 1500"
	}
	return "percent, e.g. 4"
}

func (t *projTab) addMod(proj string) {
	m, err := newMod(t.addKind, t.addObj, t.addProp, t.addValue, t.addStart)
	if err != nil {
		t.addErr = err.Error()
		return
	}
	if t.a.editProjection(proj, "add modification", func(p *model.Projection) error { p.Mods = append(p.Mods, m); return nil }) {
		t.addValue, t.addStart, t.addErr = "", "", ""
	}
}

// ---- Parsing and formatting.

// newMod builds the modification the Add form describes: the value from the optional start date on.
func newMod(kind model.ObjectKind, obj, prop, value, start string) (model.Modification, error) {
	if kind != model.KindGlobal && obj == "" {
		return model.Modification{}, errors.New("pick an object")
	}
	v, err := parseValue(prop, strings.TrimSpace(value))
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
	return model.Modification{Target: model.ObjectRef{Kind: kind, ID: obj}, Property: prop, Values: scheduleFrom(v, from)}, nil
}

// parseDate reads a date written as YYYY-MM-DD.
func parseDate(s string) (model.Date, error) {
	tm, err := time.Parse(dateLayout, s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a date; write it as %s", s, dateLayout)
	}
	y, m, d := tm.Date()
	return model.D(y, m, d), nil
}

// parseValue reads a property's value: a percentage for rates (the % sign is optional), else a number.
func parseValue(prop, s string) (float64, error) {
	rate := prop != model.PropAmount
	num := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v != v || v > 1e15 || v < -1e15 {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if rate {
		v /= 100
	}
	return v, nil
}

// formatValue is parseValue's inverse.
func formatValue(prop string, v float64) string {
	if prop == model.PropAmount {
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

// describeWhen says when a schedule has values.
func describeWhen(s model.Schedule) string {
	segs := s.Segments()
	switch {
	case len(segs) == 0:
		return "never"
	case len(segs) == 1 && segs[0].End == nil && segs[0].From == model.MinDate:
		return "always"
	case len(segs) == 1 && segs[0].End == nil:
		return "from " + segs[0].From.String()
	case len(segs) == 1:
		return segs[0].From.String() + " to " + segs[0].End.String()
	}
	return fmt.Sprintf("%d periods from %s", len(segs), segs[0].From)
}
