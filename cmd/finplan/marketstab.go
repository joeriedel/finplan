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

// marketsTab is the Markets tab: the market classes with their return, volatility and correlations on the left; the selected class's projection overrides and the accounts exposed to it on the right.
type marketsTab struct {
	a *app
	// rec is the record the exposure and the buffers belong to; they are dropped when it changes.
	rec      *model.Record
	exposure []engine.ClassExposure
	sel      string
	fieldEditor
}

func (t *marketsTab) reset() {
	t.bufs, t.errs = map[string]string{}, map[string]string{}
	var err error
	if t.exposure, err = engine.Exposure(t.rec); err != nil {
		t.exposure = nil
	}
}

func (t *marketsTab) draw() {
	a := t.a
	if t.rec != a.rec {
		t.rec = a.rec
		t.reset()
	}
	if a.rec.Class(t.sel) == nil {
		t.sel = ""
		if len(a.rec.Classes) > 0 {
			t.sel = a.rec.Classes[0].ID
		}
	}
	em := imgui.FontSize()
	imgui.PushStyleColorVec4(imgui.ColChildBg, rgba(colPanel, 1))
	open := imgui.BeginChildStrV("##marketlist", imgui.NewVec2(em*55, 0), imgui.ChildFlagsBorders, 0)
	imgui.PopStyleColor()
	if open {
		t.classes()
		imgui.Spacing()
		t.correlations()
	}
	imgui.EndChild()
	imgui.SameLine()
	if imgui.BeginChildStrV("##marketdetail", imgui.NewVec2(0, 0), 0, 0) {
		if cl := a.rec.Class(t.sel); cl != nil {
			t.detail(cl)
		} else {
			disabledText("No market classes.")
		}
	}
	imgui.EndChild()
}

// classes draws the classes table: the name selects a class, the return and volatility are edited in place.
func (t *marketsTab) classes() {
	em := imgui.FontSize()
	imgui.SeparatorText("Market classes")
	if len(t.rec.Classes) == 0 {
		disabledText("No market classes yet.")
		return
	}
	if !imgui.BeginTableV("##classes", 3, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	imgui.TableSetupColumnV("Class", 0, 2, 0)
	imgui.TableSetupColumnV("Expected return per year", imgui.TableColumnFlagsWidthFixed, em*14, 0)
	imgui.TableSetupColumnV("Volatility", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableHeadersRow()
	for _, c := range t.rec.Classes {
		imgui.PushIDStr("class" + c.ID)
		imgui.TableNextRowV(0, 0)
		imgui.TableSetColumnIndex(0)
		if imgui.SelectableBoolV(c.Name, t.sel == c.ID, 0, imgui.NewVec2(0, 0)) {
			t.sel = c.ID
		}
		imgui.TableSetColumnIndex(1)
		if rate, ok := classRate(c); ok {
			if s, done := t.field("ret"+c.ID, "percent, e.g. 7", formatPercentText(rate), em*8); done {
				t.editClass(c.ID, "ret", s)
			}
		} else {
			imgui.AlignTextToFramePadding()
			disabledText("varies, " + describeWhen(c.Return))
		}
		imgui.TableSetColumnIndex(2)
		if s, done := t.field("vol"+c.ID, "percent, e.g. 15", formatPercentText(c.Volatility), em*8); done {
			t.editClass(c.ID, "vol", s)
		}
		imgui.PopID()
	}
}

// classRate is the constant rate of the class's return, which the table edits; ok is false for a return that is not one rate for all dates.
func classRate(c model.MarketClass) (rate float64, ok bool) {
	v, from, ok := constantOf(c.Return)
	return v, ok && from == nil
}

// classChange reads the text typed into the return or volatility ("ret" or "vol") field as a change of the class.
func classChange(field, text string) (ops.ClassChange, error) {
	v, err := parsePercentText(text)
	if err != nil {
		return ops.ClassChange{}, err
	}
	if field == "ret" {
		return ops.ClassChange{Return: &v}, nil
	}
	return ops.ClassChange{Volatility: &v}, nil
}

// editClass changes the return or volatility of the class through ops.EditClass; a refusal is shown by the field.
func (t *marketsTab) editClass(id, field, text string) {
	fid := field + id
	ch, err := classChange(field, text)
	if err != nil {
		t.errs[fid] = err.Error()
		return
	}
	if !t.a.edit("change market class", func() (*ops.Result, error) {
		return ops.EditClass(t.a.st, t.a.rec, id, ch, ops.Options{})
	}) {
		if t.a.unchanged {
			delete(t.bufs, fid)
			delete(t.errs, fid)
		}
	}
}

// correlations draws the matrix of the classes' correlations: the cells above the diagonal are edited in place, those below show the same pairs.
func (t *marketsTab) correlations() {
	em := imgui.FontSize()
	cs := t.rec.Classes
	imgui.SeparatorText("Correlations")
	if len(cs) < 2 {
		disabledText("Correlations need two market classes.")
		return
	}
	disabledText("Type a number from -1 to 1; 0 means uncorrelated.")
	if !imgui.BeginTableV("##correlations", int32(len(cs)+1), imgui.TableFlagsBordersInner|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	imgui.TableSetupColumnV("##corrname", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	for _, c := range cs {
		imgui.TableSetupColumn(c.Name)
	}
	imgui.TableHeadersRow()
	for i, row := range cs {
		imgui.TableNextRowV(0, 0)
		imgui.TableSetColumnIndex(0)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(row.Name)
		for j, col := range cs {
			imgui.TableSetColumnIndex(int32(j + 1))
			imgui.PushIDStr("corr" + strconv.Itoa(i) + "/" + strconv.Itoa(j))
			v := t.rec.CorrelationOf(row.ID, col.ID)
			switch {
			case i == j:
				imgui.AlignTextToFramePadding()
				disabledText("1")
			case i > j:
				imgui.AlignTextToFramePadding()
				disabledText(trimFloat(v))
			default:
				id := "c" + row.ID + "/" + col.ID
				if s, done := t.field(id, "-1 to 1", trimFloat(v), -1); done {
					t.editCorrelation(id, row.ID, col.ID, s)
				}
			}
			imgui.PopID()
		}
	}
}

// editCorrelation sets the correlation of two classes through ops.SetCorrelation; a refusal is shown by the field.
func (t *marketsTab) editCorrelation(fid, a, b, text string) {
	v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		t.errs[fid] = strconv.Quote(strings.TrimSpace(text)) + " is not a number"
		return
	}
	if !t.a.edit("set correlation", func() (*ops.Result, error) {
		return ops.SetCorrelation(t.a.st, t.a.rec, a, b, v, ops.Options{})
	}) {
		if t.a.unchanged {
			delete(t.bufs, fid)
			delete(t.errs, fid)
		}
	}
}

// override is one line of what a projection does to a class: the projection and a description of its growth modification or shock.
type override struct {
	Projection, Text string
}

// classOverrides lists the growth modifications and shocks that the record's projections hold on the class, projection by projection.
func classOverrides(rec *model.Record, class string) []override {
	var out []override
	for _, p := range rec.Projections {
		for _, m := range p.Mods {
			if m.Target.Kind == model.KindClass && m.Target.ID == class && m.Property == model.PropGrowth {
				out = append(out, override{p.Name, growthModText(m)})
			}
		}
		for _, s := range p.Shocks {
			if s.Class == class {
				out = append(out, override{p.Name, "shock: " + shockSummary(rec, s)})
			}
		}
	}
	return out
}

// growthModText describes a growth modification of a class: the rate set or the scale applied, and when.
func growthModText(m model.Modification) string {
	verb := "growth set"
	if m.IsScale() {
		verb = "growth scaled"
	}
	v, from, ok := constantOf(m.Values)
	if !ok {
		return verb + ", " + describeWhen(m.Values)
	}
	text := verb + " to " + formatValue(model.PropGrowth, m.Mode, v)
	if m.IsScale() {
		text = verb + " by " + formatValue(model.PropGrowth, m.Mode, v)
	}
	if from != nil {
		text += " from " + from.String()
	}
	return text
}

// detail draws the selected class: its overrides in the projections and the accounts exposed to it.
func (t *marketsTab) detail(cl *model.MarketClass) {
	em := imgui.FontSize()
	imgui.PushIDStr("detail" + cl.ID)
	defer imgui.PopID()
	imgui.PushTextWrapPos()
	defer imgui.PopTextWrapPos()
	imgui.SeparatorText(cl.Name)
	t.label("Name")
	if s, done := t.field("name"+cl.ID, "", cl.Name, em*16); done {
		fid := "name" + cl.ID
		name := strings.TrimSpace(s)
		if !t.a.edit("rename market class", func() (*ops.Result, error) {
			return ops.EditClass(t.a.st, t.a.rec, cl.ID, ops.ClassChange{Name: &name}, ops.Options{})
		}) {
			if t.a.unchanged {
				delete(t.bufs, fid)
			}
		}
	}

	imgui.Spacing()
	imgui.SeparatorText("Projection overrides")
	ovs := classOverrides(t.rec, cl.ID)
	if len(ovs) == 0 {
		disabledText("No projection changes this class. They are added on the Projections tab.")
	} else if imgui.BeginTableV("##overrides", 2, imgui.TableFlagsRowBg|imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		imgui.TableSetupColumnV("Projection", imgui.TableColumnFlagsWidthFixed, em*14, 0)
		imgui.TableSetupColumnV("Change", 0, 1, 0)
		imgui.TableHeadersRow()
		for _, o := range ovs {
			imgui.TableNextRowV(0, 0)
			imgui.TableSetColumnIndex(0)
			imgui.TextUnformatted(o.Projection)
			imgui.TableSetColumnIndex(1)
			imgui.TextUnformatted(o.Text)
		}
		imgui.EndTable()
	}

	imgui.Spacing()
	imgui.SeparatorText("Accounts exposed")
	var exp *engine.ClassExposure
	for i := range t.exposure {
		if t.exposure[i].Class.ID == cl.ID {
			exp = &t.exposure[i]
		}
	}
	if exp == nil || len(exp.Accounts) == 0 {
		disabledText(noExposureText(t.rec, cl.ID))
		return
	}
	if !imgui.BeginTableV("##exposure", 5, imgui.TableFlagsRowBg|imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	imgui.TableSetupColumnV("Account", 0, 2, 0)
	imgui.TableSetupColumnV("Balance", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableSetupColumnV("Target", imgui.TableColumnFlagsWidthFixed, em*5, 0)
	imgui.TableSetupColumnV("Share", imgui.TableColumnFlagsWidthFixed, em*5, 0)
	imgui.TableSetupColumnV("Amount", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableHeadersRow()
	cell := func(i int, s string) {
		imgui.TableSetColumnIndex(int32(i))
		imgui.TextUnformatted(s)
	}
	for _, x := range exp.Accounts {
		imgui.TableNextRowV(0, 0)
		cell(0, x.Account.Name)
		cell(1, formatDollars(x.Balance))
		cell(2, trimFloat(x.Percent)+"%")
		cell(3, strconv.FormatFloat(x.Share, 'f', 1, 64)+"%")
		cell(4, formatDollars(x.Amount))
	}
	imgui.TableNextRowV(0, 0)
	cell(0, "Total")
	cell(4, formatDollars(exp.Total))
}

// noExposureText explains why no account is listed for the class: either no allocation holds it, or every account that does is closed or empty today.
func noExposureText(rec *model.Record, classID string) string {
	for _, a := range rec.Accounts {
		for _, h := range a.Allocation {
			if h.Class == classID {
				return "No open account with a balance holds this class."
			}
		}
	}
	return "No account's allocation holds this class. Allocations are set on the Accounts tab."
}
