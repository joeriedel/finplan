// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strconv"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// dragInterval names the drag and drop payload of a default balance interval row; the index of the row is in assetsTab.dragFrom.
const dragInterval = "default balance interval"

// defaults draws the asset's default balance intervals in the order they apply, each editable in place, removable and draggable, and the Add form.
func (t *assetsTab) defaults(asset *model.Asset) {
	em := imgui.FontSize()
	id := asset.ID
	imgui.Spacing()
	imgui.SeparatorText("Default balance")
	imgui.TextWrapped("The balance used on dates when no account under the asset has recorded data. Intervals apply in the order listed: where they overlap, the later one wins. Drag a number to reorder.")
	if len(asset.Default.Intervals) == 0 {
		imgui.TextDisabled("None.")
	}
	for i, iv := range asset.Default.Intervals {
		imgui.PushIDStr("interval" + strconv.Itoa(i))
		imgui.SelectableBoolV(strconv.Itoa(i+1)+"##drag", false, 0, imgui.NewVec2(em*1.5, 0))
		if imgui.BeginDragDropSource() {
			t.dragFrom = i
			imgui.SetDragDropPayload(dragInterval, 0, 0)
			imgui.TextUnformatted("Move default balance " + strconv.Itoa(i+1))
			imgui.EndDragDropSource()
		}
		if imgui.BeginDragDropTarget() {
			if pl := imgui.AcceptDragDropPayload(dragInterval); pl != nil && pl.CData != nil && t.dragFrom != i {
				from := t.dragFrom
				t.a.edit("reorder default balances", func() (*ops.Result, error) {
					return ops.MoveDefault(t.a.st, t.a.rec, id, from, i, ops.Options{})
				})
			}
			imgui.EndDragDropTarget()
		}
		from, to, value := intervalTexts(iv)
		base := string(id) + "iv" + strconv.Itoa(i)
		var done string
		var texts [3]string
		for k, f := range []struct{ name, hint, text string }{
			{"from", "always", from}, {"to", "no end", to}, {"value", "balance", value},
		} {
			imgui.SameLine()
			var d bool
			if texts[k], d = t.field(base+f.name, f.hint, f.text, em*8); d {
				done = base + f.name
			}
		}
		imgui.SameLine()
		if imgui.Button("x") {
			t.a.edit("remove default balance", func() (*ops.Result, error) {
				return ops.RemoveDefault(t.a.st, t.a.rec, id, i, ops.Options{})
			})
		}
		imgui.PopID()
		if done != "" {
			t.editInterval(id, i, base, done, texts)
		}
	}

	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Add")
	imgui.SameLine()
	imgui.SetNextItemWidth(em * 8)
	imgui.InputTextWithHint("##ivfrom", "always", &t.ivFrom, 0, nil)
	imgui.SameLine()
	imgui.SetNextItemWidth(em * 8)
	imgui.InputTextWithHint("##ivto", "no end", &t.ivTo, 0, nil)
	imgui.SameLine()
	imgui.SetNextItemWidth(em * 8)
	imgui.InputTextWithHint("##ivvalue", "balance", &t.ivValue, 0, nil)
	imgui.TextDisabled("Dates are " + dateHint + ".")
	formButton("Add##interval", addIntervalProblem(t.a.st, t.a.rec, id, t.ivFrom, t.ivTo, t.ivValue), allBlank(t.ivFrom, t.ivTo, t.ivValue), func() {
		iv, _ := interval(t.ivFrom, t.ivTo, t.ivValue)
		if t.a.edit("add default balance", func() (*ops.Result, error) {
			return ops.AddDefault(t.a.st, t.a.rec, id, iv, ops.Options{})
		}) {
			t.ivFrom, t.ivTo, t.ivValue = "", "", ""
		}
	})
}

// editInterval replaces interval i of the asset by the one the texts of its three fields describe; a refusal is shown by the field done, which finished editing.
func (t *assetsTab) editInterval(id model.AssetID, i int, base, done string, texts [3]string) {
	iv, err := interval(texts[0], texts[1], texts[2])
	if err != nil {
		t.errs[done] = err.Error()
		return
	}
	if why := editIntervalProblem(t.a.st, t.a.rec, id, i, texts[0], texts[1], texts[2]); why != "" {
		t.errs[done] = why
		return
	}
	t.a.unchanged = false
	if !t.a.edit("change default balance", func() (*ops.Result, error) {
		return ops.EditDefault(t.a.st, t.a.rec, id, i, iv, ops.Options{})
	}) && t.a.unchanged {
		for _, f := range []string{"from", "to", "value"} {
			t.dropBuf(base + f)
		}
	}
}

// dropBuf drops the field's typed text and the error that refused it.
func (t *assetsTab) dropBuf(id string) {
	delete(t.bufs, id)
	delete(t.errs, id)
}

// params draws the asset's growth setting: none, one rate for all time edited in place, or a schedule, with the Timeline button that edits it on a strip, and Clear.
func (t *assetsTab) params(asset *model.Asset) {
	em := imgui.FontSize()
	id := asset.ID
	imgui.Spacing()
	imgui.SeparatorText("Parameters")
	kind, rate := growthKind(asset)
	if t.tl.open && (t.tl.generation != t.a.generation || t.tlAsset != id || kind == growthNone) {
		t.tl = timelineState{}
	}
	imgui.TextWrapped("Growth: a fraction per year for every account under the asset, applied over the account's stated rate. Settings apply from the innermost asset out, the last applied winning on each date.")
	fid := string(id) + "growth"
	switch kind {
	case growthNone:
		t.label("Growth")
		imgui.SetNextItemWidth(em * 8)
		imgui.InputTextWithHint("##growthnew", "percent, e.g. 4", &t.growthNew, 0, nil)
		imgui.SameLine()
		formButton("Set", growthConstantProblem(t.a.st, t.a.rec, id, t.growthNew), allBlank(t.growthNew), func() {
			if t.a.edit("set growth", func() (*ops.Result, error) {
				return setGrowthConstant(t.a.st, t.a.rec, id, t.growthNew, ops.Options{})
			}) {
				t.growthNew = ""
			}
		})
	case growthConstant:
		t.label("Growth")
		if s, done := t.field(fid, "percent, e.g. 4", formatValue(model.PropGrowth, model.ModeSet, rate), em*8); done {
			if why := growthConstantProblem(t.a.st, t.a.rec, id, s); why != "" {
				t.errs[fid] = why
			} else {
				t.a.unchanged = false
				if !t.a.edit("change growth", func() (*ops.Result, error) {
					return setGrowthConstant(t.a.st, t.a.rec, id, s, ops.Options{})
				}) && t.a.unchanged {
					t.dropBuf(fid)
				}
			}
		}
	default:
		t.label("Growth")
		imgui.TextUnformatted(describeWhen(asset.Params[model.PropGrowth]))
		for _, g := range asset.Params[model.PropGrowth].Segments() {
			disabledText("    " + segmentText(g))
		}
	}
	if kind != growthNone {
		t.label("")
		if imgui.Button("Timeline") {
			t.tl = timelineState{open: true, generation: t.a.generation, sel: -1}
			t.tlAsset = id
		}
		tooltipOn("Edit the growth setting as a schedule on a timeline.")
		imgui.SameLine()
		if imgui.Button("Clear") {
			t.a.edit("clear growth", func() (*ops.Result, error) {
				return ops.SetParam(t.a.st, t.a.rec, id, model.PropGrowth, nil, ops.Options{})
			})
		}
		tooltipOn("Remove the setting: the accounts go back to the settings of the assets around this one, or their stated rate.")
	}
	if t.tl.open {
		s := &timelineStrip{
			a: t.a, st: &t.tl, prop: model.PropGrowth, mode: model.ModeSet,
			field:   t.field,
			setErr:  func(fid, msg string) { t.errs[fid] = msg },
			dropBuf: t.dropBuf,
			commit:  t.growthCommit(id),
		}
		s.draw("Timeline of growth: "+asset.Name, asset.Params[model.PropGrowth], t.a.rec.Today)
	}
}

// growthCommit returns what the timeline strip calls to write an edited schedule as the growth setting of the asset, as one undoable edit.
func (t *assetsTab) growthCommit(id model.AssetID) func(label string, next model.Schedule) {
	return func(label string, next model.Schedule) {
		t.a.edit(label, func() (*ops.Result, error) {
			return ops.SetParam(t.a.st, t.a.rec, id, model.PropGrowth, &next, ops.Options{})
		})
	}
}

// segmentText writes one segment of a growth schedule for the list.
func segmentText(g model.Segment) string {
	from, to := "from the beginning", "no end"
	if g.From != model.MinDate {
		from = "from " + g.From.String()
	}
	if g.End != nil {
		to = "to " + g.End.String()
	}
	return from + ", " + to + ": " + formatValue(model.PropGrowth, model.ModeSet, g.Value)
}

// effectiveGrowth draws, for each account under the asset, the layers of its growth in the order they apply and the effective rate on the record's today. A layer that a later one overwrites today is greyed.
func (t *assetsTab) effectiveGrowth(asset *model.Asset) {
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.SeparatorText("Effective growth")
	growth, err := assetGrowth(t.a.rec, asset.ID)
	if err != nil {
		errorText(err.Error())
		return
	}
	if len(growth) == 0 {
		imgui.TextDisabled("No accounts under this asset.")
		return
	}
	imgui.TextDisabled("Layers apply top to bottom; greyed layers are overwritten today (" + t.a.rec.Today.String() + ").")
	if !imgui.BeginTableV("##effgrowth", 3, imgui.TableFlagsRowBg|imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	imgui.TableSetupColumnV("Account / layer", 0, 3, 0)
	imgui.TableSetupColumnV("When", imgui.TableColumnFlagsWidthFixed, em*14, 0)
	imgui.TableSetupColumnV("Rate today", imgui.TableColumnFlagsWidthFixed, em*8, 0)
	imgui.TableHeadersRow()
	for _, g := range growth {
		imgui.TableNextRowV(0, 0)
		imgui.TableSetColumnIndex(0)
		imgui.TextUnformatted(g.Name)
		imgui.TableSetColumnIndex(2)
		imgui.TextUnformatted(rateText(g.Today, g.HasToday))
		for _, l := range g.Layers {
			imgui.TableNextRowV(0, 0)
			name := "stated rate"
			if l.Asset != "" {
				name = assetLabel(t.a.rec, l.Asset)
			}
			cells := [3]string{"    " + name, describeWhen(l.Schedule), rateText(l.Today, l.HasToday)}
			for c, text := range cells {
				imgui.TableSetColumnIndex(int32(c))
				if l.Applies {
					imgui.TextUnformatted(text)
				} else {
					disabledText(text)
				}
			}
		}
	}
	imgui.EndTable()
}

// rateText writes a growth rate, or says there is none.
func rateText(v float64, ok bool) string {
	if !ok {
		return "none today"
	}
	return formatValue(model.PropGrowth, model.ModeSet, v)
}
