// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/AllenDang/cimgui-go/implot"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// newAssetProblem says why an asset of that name cannot be added, or "" if it can.
func newAssetProblem(st *store.Store, rec *model.Record, name string) string {
	if strings.TrimSpace(name) == "" {
		return "enter a name"
	}
	_, err := ops.NewAsset(st, rec, name, "", ops.Options{DryRun: true})
	return reasonOf(err)
}

// renameAssetProblem says why the asset cannot be renamed to name, or "" if it can; the same name is no problem.
func renameAssetProblem(st *store.Store, rec *model.Record, id model.AssetID, name string) string {
	_, err := ops.RenameAsset(st, rec, id, name, ops.Options{DryRun: true})
	if errors.Is(err, ops.ErrNoChange) {
		return ""
	}
	return reasonOf(err)
}

// deleteAssetProblem says why the asset cannot be deleted, or "" if it can: what ops.DeleteAsset refuses.
func deleteAssetProblem(st *store.Store, rec *model.Record, id model.AssetID) string {
	_, err := ops.DeleteAsset(st, rec, id, ops.Options{DryRun: true})
	return reasonOf(err)
}

// Kinds of member the Add member form makes.
const (
	memberAccount = "account"
	memberAsset   = "asset"
	memberRule    = "rule"
)

var memberKinds = []string{memberAccount, memberAsset, memberRule}

// memberForm is the Add member form: the kind of member and what it names. A rule with no types and no tax treatments matches every account.
type memberForm struct {
	kind    string
	account model.AccountID
	asset   model.AssetID
	types   []model.AccountType
	tax     []model.TaxTreatment
}

// member reads the form, and says what is missing from it if anything.
func (f memberForm) member() (model.Member, string) {
	switch f.kind {
	case memberAccount:
		if f.account == "" {
			return model.Member{}, "pick an account"
		}
		return model.AccountMember(f.account), ""
	case memberAsset:
		if f.asset == "" {
			return model.Member{}, "pick an asset"
		}
		return model.AssetMember(f.asset), ""
	case memberRule:
		return model.RuleMember(model.Rule{Types: slices.Clone(f.types), Tax: slices.Clone(f.tax)}), ""
	}
	return model.Member{}, "pick what kind of member to add"
}

// memberProblem says why the member the form describes cannot be added to the asset, or "" if it can: what ops.AddMember refuses. A member that overlaps another is not refused; the tree shows it in red.
func memberProblem(st *store.Store, rec *model.Record, asset model.AssetID, f memberForm) string {
	m, missing := f.member()
	if missing != "" {
		return missing
	}
	_, err := ops.AddMember(st, rec, asset, m, ops.Options{DryRun: true})
	return reasonOf(err)
}

// toggle adds v to the list, or removes it if it is there.
func toggle[T comparable](vs []T, v T) []T {
	if i := slices.Index(vs, v); i >= 0 {
		return slices.Delete(slices.Clone(vs), i, i+1)
	}
	return append(slices.Clone(vs), v)
}

// assetsTab is the Assets tab: the tree of assets with their members and balances on the left, and the selected asset on the right.
type assetsTab struct {
	a *app
	// rec is the record the rows and buffers belong to; they are dropped when it changes.
	rec  *model.Record
	rows []*assetRow
	sel  model.AssetID
	// chart is the chart of the asset chartFor.
	chart    *assetChart
	chartFor model.AssetID
	// delFor and delReason cache deleteAssetProblem for the selected asset, which reads the database.
	delFor    model.AssetID
	delReason string
	delKnown  bool
	fieldEditor

	newName string
	add     memberForm
}

func (t *assetsTab) reset() {
	t.rows = buildAssets(t.rec)
	t.bufs, t.errs, t.delKnown, t.chart = map[string]string{}, map[string]string{}, false, nil
}

func (t *assetsTab) draw() {
	a := t.a
	if t.rec != a.rec {
		t.rec = a.rec
		t.reset()
	}
	if t.sel != "" && (a.rec.Asset(t.sel) == nil || isDefaultAsset(t.sel)) {
		t.sel = ""
	}
	imgui.PushStyleColorVec4(imgui.ColChildBg, rgba(colPanel, 1))
	open := imgui.BeginChildStrV("##assetlist", imgui.NewVec2(imgui.FontSize()*40, 0), imgui.ChildFlagsBorders, 0)
	imgui.PopStyleColor()
	if open {
		t.tree()
		t.newForm()
	}
	imgui.EndChild()
	imgui.SameLine()
	if imgui.BeginChildStrV("##assetdetail", imgui.NewVec2(0, 0), 0, 0) {
		if asset := a.rec.Asset(t.sel); asset != nil {
			t.detail(asset)
		} else {
			imgui.TextDisabled("Select an asset to see and change it.")
		}
	}
	imgui.EndChild()
}

// tree draws the assets with their members and balances today. A member left out of its asset's total is in red, with the reason in a tooltip.
func (t *assetsTab) tree() {
	if len(t.rows) == 0 {
		imgui.TextDisabled("No assets yet.")
		return
	}
	if !imgui.BeginTableV("##assets", 2, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	imgui.TableSetupColumnV("Asset", 0, 2, 0)
	imgui.TableSetupColumnV("Balance", imgui.TableColumnFlagsWidthFixed, imgui.FontSize()*7, 0)
	imgui.TableHeadersRow()
	for _, r := range t.rows {
		t.assetNode(r, "", "")
	}
	imgui.EndTable()
}

// assetNode draws an asset and its members. problem is why the asset is left out of the total of the asset it is a member of, else "".
func (t *assetsTab) assetNode(r *assetRow, path, problem string) {
	path += "/" + string(r.Asset.ID)
	imgui.PushIDStr(path)
	defer imgui.PopID()
	imgui.TableNextRowV(0, 0)
	imgui.TableSetColumnIndex(0)
	flags := imgui.TreeNodeFlagsOpenOnArrow | imgui.TreeNodeFlagsSpanFullWidth | imgui.TreeNodeFlagsDefaultOpen
	if t.sel == r.Asset.ID {
		flags |= imgui.TreeNodeFlagsSelected
	}
	if len(r.Members) == 0 {
		flags |= imgui.TreeNodeFlagsLeaf | imgui.TreeNodeFlagsNoTreePushOnOpen
	}
	if problem != "" {
		imgui.PushStyleColorVec4(imgui.ColText, rgba(colError, 1))
	}
	open := imgui.TreeNodeExStrV(r.Asset.Name, flags)
	if imgui.IsItemClicked() && !imgui.IsItemToggledOpen() && !isDefaultAsset(r.Asset.ID) {
		t.sel = r.Asset.ID
	}
	if problem != "" || r.Err != "" {
		tooltipOn(problem + r.Err)
	}
	imgui.TableSetColumnIndex(1)
	if r.Err != "" {
		errorText("error")
	} else {
		imgui.TextUnformatted(graph.FormatMoney(r.Balance))
	}
	if problem != "" {
		imgui.PopStyleColor()
	}
	if !open || len(r.Members) == 0 {
		return
	}
	for _, m := range r.Members {
		if m.Child != nil {
			t.assetNode(m.Child, path, m.Problem)
			continue
		}
		t.memberNode(r, m)
	}
	imgui.TreePop()
}

// memberNode draws a member that holds no members of its own as a leaf.
func (t *assetsTab) memberNode(parent *assetRow, m memberRow) {
	imgui.PushIDStr("m" + strconv.Itoa(m.Member.Seq))
	defer imgui.PopID()
	imgui.TableNextRowV(0, 0)
	imgui.TableSetColumnIndex(0)
	if m.Problem != "" {
		imgui.PushStyleColorVec4(imgui.ColText, rgba(colError, 1))
	}
	label := m.Label
	if m.Detail != "" {
		label += " (" + m.Detail + ")"
	}
	imgui.TreeNodeExStrV(label, imgui.TreeNodeFlagsLeaf|imgui.TreeNodeFlagsNoTreePushOnOpen|imgui.TreeNodeFlagsSpanFullWidth)
	if m.Problem != "" {
		tooltipOn(m.Problem)
	}
	imgui.TableSetColumnIndex(1)
	imgui.TextUnformatted(graph.FormatMoney(m.Balance))
	if m.Problem != "" {
		imgui.PopStyleColor()
	}
}

// newForm draws the New asset form under the tree.
func (t *assetsTab) newForm() {
	imgui.Spacing()
	imgui.SeparatorText("New asset")
	imgui.SetNextItemWidth(imgui.FontSize() * 16)
	imgui.InputTextWithHint("##newasset", "e.g. Emergency fund", &t.newName, 0, nil)
	imgui.SameLine()
	addButton(newAssetProblem(t.a.st, t.a.rec, t.newName), func() {
		var added model.AssetID
		if t.a.edit("add asset", func() (*ops.Result, error) {
			res, err := ops.NewAsset(t.a.st, t.a.rec, t.newName, "", ops.Options{})
			if err == nil {
				added = res.Asset.ID
			}
			return res, err
		}) {
			t.newName, t.sel = "", added
		}
	})
}

// find returns the row of the selected asset.
func (t *assetsTab) find(id model.AssetID) *assetRow {
	for _, r := range t.rows {
		if r.Asset.ID == id {
			return r
		}
	}
	return nil
}

func (t *assetsTab) detail(asset *model.Asset) {
	em := imgui.FontSize()
	id := asset.ID
	imgui.PushIDStr("detail" + string(id))
	defer imgui.PopID()
	imgui.SeparatorText(asset.Name)

	t.label("Name")
	if s, done := t.field(string(id)+"name", "", asset.Name, em*16); done {
		t.rename(id, s)
	}
	if row := t.find(id); row != nil {
		t.label("Balance today")
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(graph.FormatMoney(row.Balance))
		t.members(asset, row)
	}
	t.chartArea(id)
	t.lifecycle(asset)
}

// rename renames the asset through ops.RenameAsset; a refusal is shown by the field.
func (t *assetsTab) rename(id model.AssetID, name string) {
	fid := string(id) + "name"
	if why := renameAssetProblem(t.a.st, t.a.rec, id, name); why != "" {
		t.errs[fid] = why
		return
	}
	t.a.unchanged = false
	if !t.a.edit("rename asset", func() (*ops.Result, error) {
		return ops.RenameAsset(t.a.st, t.a.rec, id, name, ops.Options{})
	}) && t.a.unchanged {
		delete(t.bufs, fid)
		delete(t.errs, fid)
	}
}

// members draws the members of the asset with a remove button each, and the Add member form.
func (t *assetsTab) members(asset *model.Asset, row *assetRow) {
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.SeparatorText("Members")
	if len(row.Members) == 0 {
		imgui.TextDisabled("None.")
	}
	for _, m := range row.Members {
		imgui.PushIDStr("member" + strconv.Itoa(m.Member.Seq))
		imgui.AlignTextToFramePadding()
		label := m.Label
		if m.Detail != "" {
			label += " (" + m.Detail + ")"
		}
		if m.Problem != "" {
			errorText(label)
			tooltipOn(m.Problem)
		} else {
			imgui.TextUnformatted(label)
		}
		imgui.SameLineV(em*30, -1)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(graph.FormatMoney(m.Balance))
		imgui.SameLine()
		if imgui.Button("x") {
			t.a.edit("remove member", func() (*ops.Result, error) {
				return ops.RemoveMember(t.a.st, t.a.rec, asset.ID, m.Member.Seq, ops.Options{})
			})
		}
		imgui.PopID()
		if m.Problem != "" {
			errorText("    " + m.Problem)
		}
	}

	imgui.Spacing()
	f := &t.add
	t.label("Add member")
	if k := pick("memberkind", orChoose(f.kind), em*10, memberKinds, slices.Index(memberKinds, f.kind)); k >= 0 {
		f.kind = memberKinds[k]
	}
	switch f.kind {
	case memberAccount:
		t.label("Account")
		names, ids := make([]string, len(t.a.rec.Accounts)), make([]model.AccountID, len(t.a.rec.Accounts))
		for i, a := range t.a.rec.Accounts {
			names[i], ids[i] = a.Name, a.ID
		}
		if k := pick("memberacct", orChoose(accountName(t.a.rec, f.account)), em*16, names, slices.Index(ids, f.account)); k >= 0 {
			f.account = ids[k]
		}
	case memberAsset:
		t.label("Asset")
		var names []string
		var ids []model.AssetID
		for _, r := range t.rows {
			if r.Asset.ID != asset.ID {
				names, ids = append(names, r.Asset.Name), append(ids, r.Asset.ID)
			}
		}
		if k := pick("memberasset", orChoose(assetLabel(t.a.rec, f.asset)), em*16, names, slices.Index(ids, f.asset)); k >= 0 {
			f.asset = ids[k]
		}
	case memberRule:
		t.label("Account types")
		if imgui.BeginComboV("##ruletypes", orAny(joinStrings(f.types)), 0) {
			for _, ty := range model.AccountTypes {
				on := slices.Contains(f.types, ty)
				if imgui.Checkbox(string(ty), &on) {
					f.types = toggle(f.types, ty)
				}
			}
			imgui.EndCombo()
		}
		t.label("Tax treatment")
		if imgui.BeginComboV("##ruletax", orAny(joinStrings(f.tax)), 0) {
			for _, tx := range model.TaxTreatments {
				on := slices.Contains(f.tax, tx)
				if imgui.Checkbox(string(tx), &on) {
					f.tax = toggle(f.tax, tx)
				}
			}
			imgui.EndCombo()
		}
		imgui.TextDisabled("A rule takes in every account that matches, including accounts added later.")
	}
	t.label("")
	addButton(memberProblem(t.a.st, t.a.rec, asset.ID, *f), func() {
		m, _ := f.member()
		if t.a.edit("add member", func() (*ops.Result, error) {
			return ops.AddMember(t.a.st, t.a.rec, asset.ID, m, ops.Options{})
		}) {
			t.add = memberForm{kind: f.kind}
		}
	})
}

func orChoose(s string) string {
	if s == "" {
		return "choose"
	}
	return s
}

func orAny(s string) string {
	if s == "" {
		return "any"
	}
	return s
}

// chartArea draws the chart of the asset's value with the coverage bar of its actual data under it.
func (t *assetsTab) chartArea(id model.AssetID) {
	imgui.Spacing()
	imgui.SeparatorText("Value over time")
	if t.chart == nil || t.chartFor != id {
		t.chart, t.chartFor = buildChart(t.a.rec, id), id
	}
	c := t.chart
	if c.Err != "" {
		errorText(c.Err)
		return
	}
	imgui.TextDisabled("Solid: actual data. Dashed: projected. Faded: default balances.")
	h := max(imgui.ContentRegionAvail().Y-imgui.FontSize()*14, imgui.FontSize()*12)
	var pos, size imgui.Vec2
	var xMin, xMax float64
	if implot.BeginPlotV("##assetchart", imgui.NewVec2(-1, h), implot.FlagsNoTitle|implot.FlagsNoLegend|implot.FlagsNoMouseText|implot.FlagsNoInputs) {
		implot.SetupAxisV(implot.AxisX1, "", implot.AxisFlagsNoLabel)
		implot.SetupAxisScalePlotScale(implot.AxisX1, implot.ScaleTime)
		implot.SetupAxisV(implot.AxisY1, "", implot.AxisFlagsNoLabel)
		pad := max(c.Max-c.Min, 1) * 0.08
		xMin, xMax = float64(c.From)*secondsPerDay, float64(c.To)*secondsPerDay
		implot.SetupAxisLimitsV(implot.AxisX1, xMin, xMax, implot.CondAlways)
		implot.SetupAxisLimitsV(implot.AxisY1, c.Min-pad, c.Max+pad, implot.CondAlways)
		implot.SetupFinish()
		pos, size = implot.GetPlotPos(), implot.GetPlotSize()
		t.drawChart(c, pos, size, c.Min-pad, c.Max+pad)
		implot.EndPlot()
	}
	t.coverageBar(c, pos, size, xMin, xMax)
}

// drawChart strokes the chart's runs in the Graph's styles: solid for actual data, dashed for projected, faded for default balances and partly projected.
func (t *assetsTab) drawChart(c *assetChart, pos, size imgui.Vec2, yMin, yMax float64) {
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	xOf := func(d model.Date) float32 {
		return pos.X + float32(float64(d-c.From)/float64(c.To-c.From))*size.X
	}
	yOf := func(v float64) float32 { return pos.Y + float32((yMax-v)/(yMax-yMin))*size.Y }
	dl := implot.GetPlotDrawList()
	implot.PushPlotClipRect()
	defer implot.PopPlotClipRect()
	lo, hi := graph.Pt{X: pos.X, Y: pos.Y}, graph.Pt{X: pos.X + size.X, Y: pos.Y + size.Y}
	if x := xOf(c.Today); x >= lo.X && x <= hi.X {
		dl.AddLineArgs(imgui.NewVec2(x, lo.Y), imgui.NewVec2(x, hi.Y), u32(rgba(colAccent, 0.5)), uiScale)
	}
	color := graph.Color{R: 0x6E, G: 0xA8, B: 0xFE}
	for _, s := range c.Segs {
		pts := make([]graph.Pt, len(s.Pts))
		for i, p := range s.Pts {
			pts[i] = graph.Pt{X: xOf(p.Date), Y: yOf(p.Amount)}
		}
		drawPolyline(dl, pts, segmentClass(s.Status), color, lineWidth*uiScale, lo, hi)
	}
}

// coverageBar draws, under the plot and on its time axis, the dates the asset has actual data for as filled segments with gaps between, and marks the last actual date.
func (t *assetsTab) coverageBar(c *assetChart, pos, size imgui.Vec2, xMin, xMax float64) {
	s := uiScale
	barH := imgui.FontSize() * 1.1
	origin := imgui.CursorScreenPos()
	imgui.Dummy(imgui.NewVec2(imgui.ContentRegionAvail().X, barH+6*s))
	if size.X <= 0 || !(xMax > xMin) {
		return
	}
	dl := imgui.WindowDrawList()
	left, right := pos.X, pos.X+size.X
	top := origin.Y + 3*s
	xOf := func(d model.Date) float32 {
		return left + float32((float64(d)*secondsPerDay-xMin)/(xMax-xMin))*size.X
	}
	dl.AddTextVec2(imgui.NewVec2(origin.X+4*s, top), u32(rgba(colMuted, 1)), "Actual data")
	dl.PushClipRectV(imgui.NewVec2(left, top), imgui.NewVec2(right, top+barH), true)
	dl.AddRectFilled(imgui.NewVec2(left, top), imgui.NewVec2(right, top+barH), u32(rgba(colInput, 1)))
	for _, sp := range c.Coverage {
		x0, x1 := max(xOf(sp.From), left), min(max(xOf(sp.To), xOf(sp.From)+2*s), right)
		if x1 > x0 {
			dl.AddRectFilled(imgui.NewVec2(x0, top), imgui.NewVec2(x1, top+barH), u32(rgba(colAccent, 0.9)))
		}
	}
	dl.PopClipRect()
	if x := xOf(c.Today); x >= left && x <= right {
		dl.AddLineArgs(imgui.NewVec2(x, top-2*s), imgui.NewVec2(x, top+barH+2*s), u32(rgba(colAccent, 0.7)), s)
	}
	if c.HasActual {
		if x := xOf(c.LastActual); x >= left && x <= right {
			dl.AddLineArgs(imgui.NewVec2(x, top-3*s), imgui.NewVec2(x, top+barH+3*s), u32(rgba(colText, 1)), 2*s)
		}
	}
	if m := imgui.MousePos(); imgui.IsWindowHovered() && m.X >= left && m.X <= right && m.Y >= top && m.Y <= top+barH {
		msg := "No actual data under this asset."
		if c.HasActual {
			msg = "Last actual data: " + c.LastActual.String()
		}
		tooltipText(msg)
	}
}

// lifecycle draws the Delete button, disabled with the reason beside it.
func (t *assetsTab) lifecycle(asset *model.Asset) {
	imgui.Spacing()
	imgui.SeparatorText("Delete")
	if !t.delKnown || t.delFor != asset.ID {
		t.delFor, t.delReason, t.delKnown = asset.ID, deleteAssetProblem(t.a.st, t.a.rec, asset.ID), true
	}
	actionButton("Delete", t.delReason, func() {
		if t.a.edit("delete asset", func() (*ops.Result, error) {
			return ops.DeleteAsset(t.a.st, t.a.rec, asset.ID, ops.Options{})
		}) {
			t.sel = ""
		}
	})
}
