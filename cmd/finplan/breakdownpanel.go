// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// breakdownPanelID is the ID of the breakdown panel's child window.
const breakdownPanelID = "##breakdown"

// bkUI is the state of the breakdown panel's header fields and what its first frame opens.
type bkUI struct {
	fromText, toText string
	editFrom, editTo bool
	err              string
	openFirst        bool
}

// breakdownPanelHeight is the height of the breakdown panel below the plot, with avail the height of the area it shares with the plot.
func breakdownPanelHeight(avail float32) float32 {
	return min(imgui.FontSize()*13, avail*0.4)
}

// breakdownPanel draws the panel: what changed the line the breakdown follows over the interval, by cause.
func (g *graphTab) breakdownPanel(height float32) {
	imgui.PushStyleColorVec4(imgui.ColChildBg, rgba(colPanel, 1))
	open := imgui.BeginChildStrV(breakdownPanelID, imgui.NewVec2(0, height), imgui.ChildFlagsBorders, 0)
	imgui.PopStyleColor()
	if open {
		g.breakdownBody()
	}
	imgui.EndChild()
}

func (g *graphTab) breakdownBody() {
	ln, ok := followedLine(g.lines)
	if !ok {
		muted("Draw a line to see what changed it.")
		return
	}
	proj, _ := g.lines.Effective(ln.ID)
	from, to := g.iv.window(g.cursor)
	g.breakdownHeader(ln.ID, from, to)
	k := bkKey{proj, ln.Asset, from, to}
	entry := g.bkCache.get(k)
	if entry.err != nil {
		muted("The breakdown failed: " + entry.err.Error())
		return
	}
	rows := breakdownRows(g.rec, entry.res)
	if len(rows) == 0 {
		muted("Nothing changed over this interval.")
		return
	}
	em := imgui.FontSize()
	if !imgui.BeginTableV("##bktable", 4, imgui.TableFlagsRowBg|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	imgui.TableSetupColumnV("Cause", 0, 1, 0)
	imgui.TableSetupColumnV("Actual", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableSetupColumnV("Projected", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableSetupColumnV("Total", imgui.TableColumnFlagsWidthFixed, em*9, 0)
	imgui.TableHeadersRow()
	expandable := len(g.bkCache.memberAccounts(proj, ln.Asset)) > 1
	var sumA, sumP float64
	for _, r := range rows {
		sumA, sumP = sumA+r.actual, sumP+r.projected
		imgui.TableNextRow()
		imgui.TableNextColumn()
		flags := imgui.TreeNodeFlagsSpanFullWidth
		if !expandable {
			flags |= imgui.TreeNodeFlagsLeaf | imgui.TreeNodeFlagsNoTreePushOnOpen
		} else if g.bk.openFirst {
			imgui.SetNextItemOpenV(true, imgui.CondOnce)
			g.bk.openFirst = false
		}
		open := imgui.TreeNodeExStrV(r.label+"##"+string(r.key.cause)+"/"+r.key.class, flags)
		moneyCells(r.actual, r.projected, r.total())
		if expandable && open {
			shares := memberShares(g.bkCache.members(k, entry), r.key)
			for _, s := range shares {
				imgui.TableNextRow()
				imgui.TableNextColumn()
				imgui.TextUnformatted(s.name)
				moneyCells(s.actual, s.projected, s.actual+s.projected)
			}
			if len(shares) == 0 {
				imgui.TableNextRow()
				imgui.TableNextColumn()
				muted("No member account has a share.")
			}
			imgui.TreePop()
		}
	}
	imgui.TableNextRow()
	imgui.TableNextColumn()
	imgui.TextUnformatted("Change")
	moneyCells(sumA, sumP, sumA+sumP)
	imgui.EndTable()
}

// breakdownHeader draws the panel's two header rows: the line dropdown and the interval's date fields and clear button, then the quick choices and a note on what the panel covers.
func (g *graphTab) breakdownHeader(line graph.LineID, from, to model.Date) {
	em := imgui.FontSize()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Breakdown of")
	imgui.SameLine()
	id := line
	if g.lineCombo("##bkline", &id) {
		g.lines.Select(id)
	}
	imgui.SameLineV(0, em)
	imgui.TextUnformatted("From")
	imgui.SameLine()
	g.bkDateField("##bkfrom", &g.bk.fromText, &g.bk.editFrom, firstDay(from), false)
	imgui.SameLine()
	imgui.TextUnformatted("to")
	imgui.SameLine()
	g.bkDateField("##bkto", &g.bk.toText, &g.bk.editTo, to, true)
	imgui.SameLine()
	imgui.BeginDisabledV(!g.iv.set)
	if imgui.Button("x##bkclear") {
		g.iv.clear()
	}
	imgui.EndDisabled()
	if imgui.IsItemHovered() && imgui.BeginTooltip() {
		imgui.TextUnformatted("Clear the interval (Esc)")
		imgui.EndTooltip()
	}
	if g.bk.err != "" {
		imgui.SameLine()
		errorText(g.bk.err)
	}

	if imgui.Button("Year at cursor") {
		g.iv.setWindow(yearAt(g.cursor))
	}
	imgui.SameLine()
	if imgui.Button("This year") {
		g.iv.setWindow(yearAt(g.rec.Today))
	}
	imgui.SameLine()
	if imgui.Button("Since start") {
		g.iv.setWindow(sinceStart(g.dataStart, g.rec.Today))
	}
	imgui.SameLine()
	if imgui.Button("Next 10 years") {
		g.iv.setWindow(nextTenYears(g.rec.Today))
	}
	imgui.SameLine()
	if g.iv.set {
		muted("Shift-drag on the plot or edit the dates to change it. The window is " + graph.FormatDate(firstDay(from)) + " through " + graph.FormatDate(to) + ".")
	} else {
		muted("The 12 months ending at the cursor; Shift-drag on the plot to set an interval.")
	}
}

// bkDateField is a text field for one end of the interval, written as the first day (the start) or the last day (the end) the breakdown covers. It shows the interval's date unless being edited, and applies the text when the edit ends.
func (g *graphTab) bkDateField(id string, text *string, editing *bool, shown model.Date, end bool) {
	if !*editing {
		*text = shown.String()
	}
	imgui.SetNextItemWidth(imgui.FontSize() * 8)
	imgui.InputTextWithHint(id, dateHint, text, 0, nil)
	*editing = imgui.IsItemActive()
	if !imgui.IsItemDeactivatedAfterEdit() {
		return
	}
	d, err := parseDate(*text)
	if err != nil {
		g.bk.err = err.Error()
		return
	}
	g.bk.err = ""
	if !end {
		d--
	}
	g.iv.setEdge(g.cursor, end, d)
}

func muted(s string) {
	imgui.PushStyleColorVec4(imgui.ColText, rgba(colMuted, 1))
	imgui.TextUnformatted(s)
	imgui.PopStyleColor()
}

// moneyCells fills the table's three amount columns, right aligned.
func moneyCells(actual, projected, total float64) {
	for _, v := range []float64{actual, projected, total} {
		imgui.TableNextColumn()
		s := formatDollars(v)
		imgui.SetCursorPosX(imgui.CursorPosX() + max(0, imgui.ContentRegionAvail().X-imgui.CalcTextSize(s).X))
		imgui.TextUnformatted(s)
	}
}
