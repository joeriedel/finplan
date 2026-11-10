// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// dragSetting names the drag and drop payload of a start setting row; the index of the row is in projTab.dragFrom.
const dragSetting = "projection start setting"

// overrideNames are the choices of an override_history cell, in the order of overrideValue.
var overrideNames = []string{"inherit", "yes", "no"}

// overrideValue is the override_history a choice of overrideNames stands for.
func overrideValue(choice int) *bool {
	if choice == 0 {
		return nil
	}
	return model.Ptr(choice == 1)
}

// overrideChoice is the index in overrideNames of v.
func overrideChoice(v *bool) int {
	switch {
	case v == nil:
		return 0
	case *v:
		return 1
	}
	return 2
}

// settingForm is the Add start setting form.
type settingForm struct {
	asset    model.AssetID
	start    string
	override int
}

// setting returns the setting the form describes, or the reason it is not one yet.
func (f settingForm) setting() (model.AssetSetting, string) {
	s := model.AssetSetting{Asset: f.asset, OverrideHistory: overrideValue(f.override)}
	if f.asset == "" {
		return s, "choose an asset"
	}
	if text := strings.TrimSpace(f.start); text != "" {
		d, err := parseDate(text)
		if err != nil {
			return s, err.Error()
		}
		s.Start = &d
	}
	if s.Start == nil && s.OverrideHistory == nil {
		return s, "give a start date or an override choice"
	}
	return s, ""
}

// settings draws the projection's per-asset start settings, which apply in this order with the last one winning, and under them the start each account ends up with.
func (t *projTab) settings(p *model.Projection) {
	imgui.SeparatorText("Start settings")
	imgui.TextWrapped("Give an asset its own projection start date or override_history. The settings apply in this order and the last one that reaches an account wins; an account without one inherits the projection's.")
	flags := imgui.TableFlagsRowBg | imgui.TableFlagsBordersInnerV | imgui.TableFlagsSizingStretchProp
	em := imgui.FontSize()
	if len(p.AssetSettings) > 0 && imgui.BeginTableV("##settings", 5, flags, imgui.NewVec2(0, 0), 0) {
		imgui.TableSetupColumnV("#", imgui.TableColumnFlagsWidthFixed, em*2.5, 0)
		imgui.TableSetupColumnV("Asset", 0, 2, 0)
		imgui.TableSetupColumnV("Starts on", imgui.TableColumnFlagsWidthFixed, em*9, 0)
		imgui.TableSetupColumnV("Override history", imgui.TableColumnFlagsWidthFixed, em*8, 0)
		imgui.TableSetupColumnV("##remove", imgui.TableColumnFlagsWidthFixed, em*2, 0)
		imgui.TableHeadersRow()
		for i := range p.AssetSettings {
			t.settingRow(p, i)
		}
		imgui.EndTable()
	}
	if len(p.AssetSettings) == 0 {
		imgui.TextDisabled("No start settings yet.")
	}
	t.addSettingForm(p)
	t.effectiveStarts(p)
}

// assetLabel is the name the tab shows for the asset, its id when the projection does not see it.
func assetLabel(view *model.Record, id model.AssetID) string {
	if a := view.Asset(id); a != nil {
		return a.Name
	}
	return string(id)
}

// assetChoices lists the assets a start setting of the projection can name, with their labels.
func assetChoices(view *model.Record) (ids []model.AssetID, labels []string) {
	for _, a := range view.Assets {
		ids = append(ids, a.ID)
		labels = append(labels, a.Name)
	}
	return ids, labels
}

// assetKinds classifies each of the asset ids.
func assetKinds(ids []model.AssetID) []iconKind {
	kinds := make([]iconKind, len(ids))
	for i, id := range ids {
		kinds[i] = assetKind(id)
	}
	return kinds
}

func (t *projTab) settingRow(p *model.Projection, i int) {
	s := p.AssetSettings[i]
	view := effective(t.a.rec, p.Name)
	id := strconv.Itoa(i)
	imgui.PushIDStr("setting" + id)
	defer imgui.PopID()
	imgui.TableNextRowV(0, 0)

	imgui.TableSetColumnIndex(0)
	imgui.SelectableBoolV(strconv.Itoa(i+1)+"##drag", false, 0, imgui.NewVec2(0, 0))
	if imgui.BeginDragDropSource() {
		t.dragFrom = i
		imgui.SetDragDropPayload(dragSetting, 0, 0)
		imgui.TextUnformatted("Move start setting " + strconv.Itoa(i+1))
		imgui.EndDragDropSource()
	}
	if imgui.BeginDragDropTarget() {
		if pl := imgui.AcceptDragDropPayload(dragSetting); pl != nil && pl.CData != nil && t.dragFrom != i {
			from := t.dragFrom
			t.a.edit("reorder start settings", func() (*ops.Result, error) {
				return ops.MoveAssetSetting(t.a.st, t.a.rec, p.Name, from, i, ops.Options{})
			})
		}
		imgui.EndDragDropTarget()
	}

	imgui.TableSetColumnIndex(1)
	ids, labels := assetChoices(view)
	sel := -1
	for k, a := range ids {
		if a == s.Asset {
			sel = k
		}
	}
	if k := pickKinds("asset", assetLabel(view, s.Asset), assetKind(s.Asset), -1, labels, assetKinds(ids), sel); k >= 0 {
		t.changeSetting(p.Name, i, func(c *model.AssetSetting) { c.Asset = ids[k] })
	}

	imgui.TableSetColumnIndex(2)
	start := ""
	if s.Start != nil {
		start = s.Start.String()
	}
	if text, done := t.field("setstart"+id, "inherit", start, -1); done {
		t.commit("setstart"+id, func() {
			text = strings.TrimSpace(text)
			var d *model.Date
			if text != "" {
				v, err := parseDate(text)
				if err != nil {
					t.setErr("setstart"+id, err.Error())
					return
				}
				d = &v
			}
			t.changeSetting(p.Name, i, func(c *model.AssetSetting) { c.Start = d })
		})
	}

	imgui.TableSetColumnIndex(3)
	cur := overrideChoice(s.OverrideHistory)
	if k := pick("override", overrideNames[cur], -1, overrideNames, cur); k >= 0 {
		t.changeSetting(p.Name, i, func(c *model.AssetSetting) { c.OverrideHistory = overrideValue(k) })
	}

	imgui.TableSetColumnIndex(4)
	if imgui.SmallButton("x") {
		t.a.edit("remove start setting", func() (*ops.Result, error) {
			return ops.RemoveAssetSetting(t.a.st, t.a.rec, p.Name, i, ops.Options{})
		})
	}
	if imgui.IsItemHovered() {
		imgui.SetTooltip("Remove this start setting")
	}
}

// changeSetting applies change to a copy of setting i of the projection and saves it. change replaces pointers; it never writes through them.
func (t *projTab) changeSetting(proj string, i int, change func(s *model.AssetSetting)) {
	s := t.a.rec.Projection(proj).AssetSettings[i]
	change(&s)
	t.a.edit("change start setting", func() (*ops.Result, error) {
		return ops.SetAssetSetting(t.a.st, t.a.rec, proj, i, s, ops.Options{})
	})
}

func (t *projTab) addSettingForm(p *model.Projection) {
	em := imgui.FontSize()
	f := &t.addSetting
	view := effective(t.a.rec, p.Name)
	ids, labels := assetChoices(view)
	imgui.Spacing()
	t.label("Add start setting")
	imgui.AlignTextToFramePadding()
	imgui.TextDisabled("asset")
	imgui.SameLine()
	preview := "choose"
	sel := -1
	for k, a := range ids {
		if a == f.asset {
			sel, preview = k, labels[k]
		}
	}
	previewKind := iconNone
	if sel >= 0 {
		previewKind = assetKind(f.asset)
	}
	if k := pickKinds("addsetasset", preview, previewKind, em*12, labels, assetKinds(ids), sel); k >= 0 {
		f.asset = ids[k]
	}
	imgui.SameLine()
	imgui.AlignTextToFramePadding()
	imgui.TextDisabled("starts on")
	imgui.SameLine()
	imgui.SetNextItemWidth(em * 8)
	imgui.InputTextWithHint("##addsetstart", dateHint, &f.start, 0, nil)
	imgui.SameLine()
	imgui.AlignTextToFramePadding()
	imgui.TextDisabled("override history")
	imgui.SameLine()
	if k := pick("addsetoverride", overrideNames[f.override], em*7, overrideNames, f.override); k >= 0 {
		f.override = k
	}
	t.label("")
	s, problem := f.setting()
	addButton(problem, func() {
		if t.a.edit("add start setting", func() (*ops.Result, error) {
			return ops.AddAssetSetting(t.a.st, t.a.rec, p.Name, s, ops.Options{})
		}) {
			*f = settingForm{}
		}
	})
}

// effectiveStarts draws where each account of the projection switches from recorded data to projection, as the engine resolves it with the settings above.
func (t *projTab) effectiveStarts(p *model.Projection) {
	if t.startsFor != p.Name {
		t.starts, t.startsErr = engine.AllStarts(t.a.rec, p.Name)
		t.startsFor = p.Name
	}
	imgui.Spacing()
	imgui.TextUnformatted("Effective start of each account")
	if t.startsErr != nil {
		errorText("Cannot work out the starts: " + t.startsErr.Error())
		return
	}
	view := effective(t.a.rec, p.Name)
	em := imgui.FontSize()
	flags := imgui.TableFlagsRowBg | imgui.TableFlagsBordersInnerV | imgui.TableFlagsSizingStretchProp
	if !imgui.BeginTableV("##starts", 5, flags, imgui.NewVec2(0, 0), 0) {
		return
	}
	imgui.TableSetupColumnV("Account", 0, 2, 0)
	imgui.TableSetupColumnV("Projected from", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableSetupColumnV("Last recorded", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableSetupColumnV("Projection start", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableSetupColumnV("Override history", imgui.TableColumnFlagsWidthFixed, em*8, 0)
	imgui.TableHeadersRow()
	for _, s := range t.starts {
		imgui.TableNextRowV(0, 0)
		name := string(s.Account)
		if a := view.Account(s.Account); a != nil {
			name = a.Name
		}
		last, override := "none", "no"
		if s.HasActual {
			last = s.LastActual.String()
		}
		if s.OverrideHistory {
			override = "yes"
		}
		for c, text := range []string{name, s.Start.String(), last, s.ProjectionStart.String(), override} {
			imgui.TableSetColumnIndex(int32(c))
			imgui.TextUnformatted(text)
		}
	}
	imgui.EndTable()
}
