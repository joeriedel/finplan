// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

const followSwitcher = "Follow switcher"

func colorFloats(c graph.Color) [3]float32 {
	r, g, b := c.Floats()
	return [3]float32{r, g, b}
}

func floatsColor(f [3]float32) graph.Color {
	q := func(v float32) uint8 { return uint8(max(0, min(1, v))*255 + 0.5) }
	return graph.Color{R: q(f[0]), G: q(f[1]), B: q(f[2])}
}

// drawPin draws a push pin centred on c, the head up and to the right of the needle.
func drawPin(dl *imgui.DrawList, c imgui.Vec2, size float32, col uint32) {
	head := imgui.NewVec2(c.X+size*0.14, c.Y-size*0.16)
	dl.AddLineArgs(imgui.NewVec2(c.X-size*0.34, c.Y+size*0.36), imgui.NewVec2(head.X-size*0.06, head.Y+size*0.06), col, 1.5*uiScale)
	dl.AddCircleFilled(head, size*0.26, col)
}

func (g *graphTab) sidebar() {
	// A picked preset replaces the lines, so it is applied once the sidebar is drawn.
	pick := g.presetSection()
	imgui.Separator()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Lines")
	imgui.SameLineV(imgui.ContentRegionAvail().X-imgui.FontSize()*4.5+imgui.CursorPos().X, 0)
	if imgui.Button("+ Add") || takeOpen("add-line") {
		g.search, g.addAsset, g.addProj = "", "", ""
		imgui.OpenPopupStrV("add line", 0)
	}
	g.addPopup()
	imgui.Separator()

	if n := g.lines.SoloCount(); n > 0 {
		if imgui.SmallButton("Solo: " + strconv.Itoa(n) + " · Clear") {
			g.lines.ClearSolo()
			g.fitY = true
		}
	}
	for _, ln := range g.lines.All() {
		g.row(ln)
	}

	imgui.Separator()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Bands")
	imgui.SameLineV(imgui.ContentRegionAvail().X-imgui.FontSize()*4.5+imgui.CursorPos().X, 0)
	if imgui.Button("+ Add##band") {
		g.bandUpper, g.bandLower = 0, 0
		g.bandColor, g.bandDefault = [3]float32{}, [3]float32{}
		imgui.OpenPopupStrV("add band", 0)
	}
	g.addBandPopup()
	for _, b := range g.lines.Bands() {
		g.bandRow(b)
	}

	if pick >= 0 {
		g.applyPreset(pick)
	}
}

// presetSection draws the preset dropdown with Save and Save as, and returns the index of the preset picked, or -1.
func (g *graphTab) presetSection() int {
	pick := -1
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("View preset")
	preview := "Choose a preset"
	if g.preset >= 0 {
		preview = g.presets[g.preset].Name
		if g.presetDirty() {
			preview += " *"
		}
	}
	imgui.SetNextItemWidth(-1)
	if imgui.BeginComboV("##preset", preview, 0) {
		for i, p := range g.presets {
			if imgui.SelectableBoolV(p.Name+"##"+strconv.Itoa(i), i == g.preset, 0, imgui.NewVec2(0, 0)) {
				pick = i
			}
		}
		imgui.EndCombo()
	}
	imgui.BeginDisabledV(g.preset < 0)
	if imgui.Button("Save") {
		g.savePreset()
	}
	imgui.EndDisabled()
	imgui.SameLine()
	if imgui.Button("Save as…") {
		g.presetName = ""
		imgui.OpenPopupStrV("save preset as", 0)
	}
	if imgui.BeginPopupV("save preset as", 0) {
		imgui.SetNextItemWidth(imgui.FontSize() * 16)
		imgui.InputTextWithHint("##name", "Preset name", &g.presetName, 0, nil)
		name := strings.TrimSpace(g.presetName)
		imgui.BeginDisabledV(name == "" || g.presetNameTaken(name))
		if imgui.Button("Save") {
			g.savePresetAs(name)
			imgui.CloseCurrentPopup()
		}
		imgui.EndDisabled()
		imgui.SameLine()
		if imgui.Button("Cancel") {
			imgui.CloseCurrentPopup()
		}
		imgui.EndPopup()
	}
	return pick
}

// bandRow draws one band: visibility checkbox, colour swatch and name, then hatch and today's dollars toggles and a
// remove button.
func (g *graphTab) bandRow(b graph.Band) {
	imgui.PushIDStr("band" + strconv.Itoa(int(b.ID)))
	defer imgui.PopID()

	vis := b.Visible
	if imgui.Checkbox("##visible", &vis) {
		g.lines.SetBandVisible(b.ID, vis)
	}
	imgui.SameLine()
	sw := imgui.FrameHeight() * 0.7
	if imgui.ColorButtonV("##swatch", lineColor(b.Color, 1), imgui.ColorEditFlagsNoTooltip|imgui.ColorEditFlagsNoAlpha, imgui.NewVec2(sw, sw)) {
		g.editColor = colorFloats(b.Color)
		imgui.OpenPopupStrV("recolour", 0)
	}
	if imgui.BeginPopupV("recolour", 0) {
		if imgui.ColorEdit3V("##colour", &g.editColor, imgui.ColorEditFlagsNoInputs|imgui.ColorEditFlagsNoLabel) {
			g.lines.SetBandColor(b.ID, floatsColor(g.editColor))
		}
		imgui.EndPopup()
	}
	imgui.SameLine()
	imgui.PushTextWrapPosV(0)
	imgui.TextUnformatted(g.bandName(b))
	imgui.PopTextWrapPos()

	hatch, today := b.Hatch, b.TodayDollars
	if imgui.Checkbox("Hatch", &hatch) {
		g.lines.SetBandHatch(b.ID, hatch)
	}
	imgui.SameLine()
	if imgui.Checkbox("Today's $", &today) {
		g.lines.SetBandTodayDollars(b.ID, today)
	}
	imgui.SameLine()
	if imgui.SmallButton("Remove") {
		g.lines.RemoveBand(b.ID)
	}
	imgui.Dummy(imgui.NewVec2(0, imgui.CurrentStyle().ItemSpacing().Y))
}

// lineCombo is a combo choosing one of the lines, shown by asset name and effective projection. It reports whether
// the choice changed.
func (g *graphTab) lineCombo(label string, id *graph.LineID) bool {
	preview := "Choose a line"
	if n, p := g.lineName(*id); n != "" {
		preview = n + " (" + p + ")"
	}
	changed := false
	imgui.SetNextItemWidth(imgui.FontSize() * 16)
	if imgui.BeginComboV(label, preview, 0) {
		for _, ln := range g.lines.All() {
			n, p := g.lineName(ln.ID)
			if imgui.SelectableBoolV(n+" ("+p+")##"+strconv.Itoa(int(ln.ID)), ln.ID == *id, 0, imgui.NewVec2(0, 0)) {
				*id, changed = ln.ID, true
			}
		}
		imgui.EndCombo()
	}
	return changed
}

// addBandPopup is the Add band dialog: the upper and lower lines and the colour, which follows the upper line's
// until it is edited.
func (g *graphTab) addBandPopup() {
	if !imgui.BeginPopupV("add band", 0) {
		return
	}
	if g.lineCombo("Upper", &g.bandUpper) {
		if ln, ok := g.lines.Get(g.bandUpper); ok {
			c := colorFloats(ln.Color)
			if g.bandColor == g.bandDefault {
				g.bandColor = c
			}
			g.bandDefault = c
		}
	}
	g.lineCombo("Lower", &g.bandLower)
	imgui.ColorEdit3V("Colour", &g.bandColor, imgui.ColorEditFlagsNoInputs)

	imgui.Separator()
	imgui.BeginDisabledV(g.bandUpper == 0 || g.bandLower == 0 || g.bandUpper == g.bandLower)
	imgui.PushStyleColorVec4(imgui.ColButton, rgba(colSelect, 1))
	if imgui.Button("Add") {
		g.lines.AddBand(g.bandUpper, g.bandLower, floatsColor(g.bandColor))
		imgui.CloseCurrentPopup()
	}
	imgui.PopStyleColor()
	imgui.EndDisabled()
	imgui.SameLine()
	if imgui.Button("Cancel") {
		imgui.CloseCurrentPopup()
	}
	imgui.EndPopup()
}

// rowControls says which of a row's buttons are drawn: all of them while the row is hovered or a button of the row is held down, and else only the pin of a pinned line and the solo of a soloed one.
func rowControls(hovered, pinned, soloed, buttonActive bool) (solo, pin, remove bool) {
	show := hovered || buttonActive
	return show || soloed, show || pinned, show
}

// row draws one line: visibility checkbox, solo button, colour swatch, asset name and projection, with pin and
// remove buttons while hovered. Clicking the row selects the line.
func (g *graphTab) row(ln graph.Line) {
	s := uiScale
	imgui.PushIDStr(strconv.Itoa(int(ln.ID)))
	defer imgui.PopID()

	dl := imgui.WindowDrawList()
	p := imgui.CursorScreenPos()
	w := imgui.ContentRegionAvail().X
	fh := imgui.FrameHeight()
	pad := 4 * s
	h := imgui.TextLineHeight()*2 + 2*pad
	end := imgui.NewVec2(p.X+w, p.Y+h)
	selID, selected := g.lines.Selected()
	selected = selected && selID == ln.ID

	imgui.SetNextItemAllowOverlap()
	if imgui.SelectableBoolV("##row", false, imgui.SelectableFlagsAllowOverlap, imgui.NewVec2(w, h)) {
		g.lines.Select(ln.ID)
	}
	// A button held down blocks the window's hover, so the row stays hovered while one is active.
	hovered := imgui.IsWindowHoveredV(imgui.HoveredFlagsAllowWhenBlockedByActiveItem) && imgui.IsMouseHoveringRectV(p, end, true)
	if selected {
		dl.AddRectFilledV(p, end, u32(rgba(colSelect, 0.45)), 4*s, 0)
	}

	// Checkbox and colour swatch, vertically centred.
	vis := ln.Visible
	imgui.SetCursorScreenPos(imgui.NewVec2(p.X+4*s, p.Y+(h-fh)/2))
	if imgui.Checkbox("##visible", &vis) {
		g.lines.SetVisible(ln.ID, vis)
		g.fitY = true
	}

	// The solo, pin and remove buttons are submitted on every frame, so that one pressed stays pressed until its release, and drawn only where rowControls says. Ctrl-click on solo clears every solo.
	soloed := g.lines.Soloed(ln.ID)
	sx := p.X + 4*s + fh + 2*s
	imgui.SetCursorScreenPos(imgui.NewVec2(sx, p.Y+(h-fh)/2))
	soloClicked := imgui.InvisibleButton("##solo", imgui.NewVec2(fh, fh))
	soloHot, soloActive := imgui.IsItemHovered(), imgui.IsItemActive()
	if soloClicked {
		if imgui.IsKeyDown(imgui.ModCtrl) {
			g.lines.ClearSolo()
		} else {
			g.lines.ToggleSolo(ln.ID)
		}
		g.fitY = true
	}
	if soloHot {
		tooltipText("Solo: draw only soloed lines (Ctrl-click clears all)")
	}

	pinX := p.X + w - 2*fh - 4*s
	imgui.SetCursorScreenPos(imgui.NewVec2(pinX, p.Y+(h-fh)/2))
	if imgui.InvisibleButton("##pin", imgui.NewVec2(fh, fh)) {
		g.lines.TogglePin(ln.ID)
	}
	pinHot, pinActive := imgui.IsItemHovered(), imgui.IsItemActive()
	if pinHot {
		tip := "Pin to " + g.lines.Switcher()
		if ln.Pinned {
			tip = "Unpin: follow the switcher"
		}
		tooltipText(tip)
	}

	mcX := pinX - fh - 2*s
	imgui.SetCursorScreenPos(imgui.NewVec2(mcX, p.Y+(h-fh)/2))
	if imgui.InvisibleButton("##montecarlo", imgui.NewVec2(fh, fh)) {
		g.lines.SetMonteCarlo(ln.ID, !ln.MonteCarlo)
		g.fitY = true
	}
	mcHot, mcActive := imgui.IsItemHovered(), imgui.IsItemActive()
	if mcHot {
		tooltipText("Monte Carlo: draw the 10th-90th and 25th-75th percentile bands and the median of many random runs, and the share of runs that stay above zero")
	}

	removeX := p.X + w - fh - 2*s
	imgui.SetCursorScreenPos(imgui.NewVec2(removeX, p.Y+(h-fh)/2))
	if imgui.InvisibleButton("##remove", imgui.NewVec2(fh, fh)) {
		g.lines.Remove(ln.ID)
		g.fitY = true
	}
	removeHot, removeActive := imgui.IsItemHovered(), imgui.IsItemActive()
	if removeHot {
		tooltipText("Remove line")
	}
	showSolo, showPin, showRemove := rowControls(hovered, ln.Pinned, soloed, soloActive || pinActive || removeActive || mcActive)

	if showSolo {
		sc := imgui.NewVec2(sx+fh/2, p.Y+h/2)
		half := fh * 0.4
		sa := float32(0.35)
		if soloHot {
			sa = 0.8
		}
		txtCol := u32(rgba(colText, sa))
		if soloed {
			dl.AddRectFilledV(imgui.NewVec2(sc.X-half, sc.Y-half), imgui.NewVec2(sc.X+half, sc.Y+half), u32(rgba(colAccent, 1)), 3*s, 0)
			txtCol = u32(rgba(colWindow, 1))
		}
		ts := imgui.CalcTextSize("S")
		dl.AddTextVec2(imgui.NewVec2(sc.X-ts.X/2, sc.Y-ts.Y/2), txtCol, "S")
	}

	sw := fh * 0.7
	imgui.SetCursorScreenPos(imgui.NewVec2(p.X+4*s+2*fh+8*s, p.Y+(h-sw)/2))
	if imgui.ColorButtonV("##swatch", lineColor(ln.Color, 1), imgui.ColorEditFlagsNoTooltip|imgui.ColorEditFlagsNoAlpha, imgui.NewVec2(sw, sw)) {
		g.editColor = colorFloats(ln.Color)
		imgui.OpenPopupStrV("recolour", 0)
	}
	if imgui.BeginPopupV("recolour", 0) {
		if imgui.ColorEdit3V("##colour", &g.editColor, imgui.ColorEditFlagsNoInputs|imgui.ColorEditFlagsNoLabel) {
			g.lines.SetColor(ln.ID, floatsColor(g.editColor))
		}
		imgui.EndPopup()
	}

	// Names.
	textX := p.X + 4*s + 2*fh + 8*s + sw + 8*s
	kind := assetKind(ln.Asset)
	nameCY := p.Y + pad + imgui.TextLineHeight()/2
	drawKindIcon(dl, textX, nameCY, kind, iconColor())
	iconHot := hovered && hoverIcon(textX, nameCY, kind)
	// Names stop before the pin button, which can be drawn over their end.
	clipX := mcX - 2*s
	dl.PushClipRectV(imgui.NewVec2(textX, p.Y), imgui.NewVec2(clipX, end.Y), true)
	name := g.assetName(ln.Asset)
	dl.AddTextVec2(imgui.NewVec2(textX+iconSlot(), p.Y+pad), u32(rgba(colText, 1)), name)
	proj, _ := g.lines.Effective(ln.ID)
	line2 := imgui.NewVec2(textX, p.Y+pad+imgui.TextLineHeight())
	projX := float32(0)
	if ln.Pinned {
		projX = 14 * s
		drawPin(dl, imgui.NewVec2(textX+5*s, line2.Y+imgui.TextLineHeight()/2), 11*s, u32(rgba(colMuted, 1)))
		dl.AddTextVec2(imgui.NewVec2(textX+projX, line2.Y), u32(rgba(colMuted, 1)), proj)
	} else {
		dl.AddTextVec2(line2, u32(rgba(colAccentLit, 1)), proj)
	}
	dl.PopClipRect()
	room := clipX - textX
	if clipped := textClipped(iconSlot()+imgui.CalcTextSize(name).X, room) || textClipped(projX+imgui.CalcTextSize(proj).X, room); clipped && hovered && !iconHot && !soloHot && !pinHot && !removeHot && !mcHot &&
		imgui.IsMouseHoveringRectV(imgui.NewVec2(textX, p.Y), imgui.NewVec2(clipX, end.Y), true) {
		tooltipText(name + "\n" + proj)
	}

	if showRemove || ln.MonteCarlo {
		mc := imgui.NewVec2(mcX+fh/2, p.Y+h/2)
		half := fh * 0.4
		ma := float32(0.35)
		if mcHot {
			ma = 0.8
		}
		txtCol := u32(rgba(colText, ma))
		if ln.MonteCarlo {
			dl.AddRectFilledV(imgui.NewVec2(mc.X-half, mc.Y-half), imgui.NewVec2(mc.X+half, mc.Y+half), u32(rgba(colAccent, 1)), 3*s, 0)
			txtCol = u32(rgba(colWindow, 1))
		}
		ts := imgui.CalcTextSize("M")
		dl.AddTextVec2(imgui.NewVec2(mc.X-ts.X/2, mc.Y-ts.Y/2), txtCol, "M")
	}

	if showPin {
		pinAlpha := float32(0.35)
		switch {
		case pinHot:
			pinAlpha = 1
		case ln.Pinned:
			pinAlpha = 0.9
		}
		drawPin(dl, imgui.NewVec2(pinX+fh/2, p.Y+h/2), 13*s, u32(rgba(colText, pinAlpha)))
	}
	if showRemove {
		alpha := float32(0.35)
		if removeHot {
			alpha = 1
		}
		c := imgui.NewVec2(removeX+fh/2, p.Y+h/2)
		r := 4 * s
		col := u32(rgba(colText, alpha))
		dl.AddLineArgs(imgui.NewVec2(c.X-r, c.Y-r), imgui.NewVec2(c.X+r, c.Y+r), col, 1.5*s)
		dl.AddLineArgs(imgui.NewVec2(c.X-r, c.Y+r), imgui.NewVec2(c.X+r, c.Y-r), col, 1.5*s)
	}

	imgui.SetCursorScreenPos(imgui.NewVec2(p.X, p.Y+h))
	imgui.Dummy(imgui.NewVec2(w, imgui.CurrentStyle().ItemSpacing().Y))
}

// addProjection is the projection whose accounts the Add line dialog offers: the one chosen in it, else the one lines follow.
func (g *graphTab) addProjection() string {
	if g.addProj != "" {
		return g.addProj
	}
	return g.lines.Switcher()
}

// lineCandidate is an asset or account the Add line dialog offers.
type lineCandidate struct {
	id   model.AssetID
	name string
	kind iconKind
}

// lineCandidates lists the assets and accounts of rec, which is the record as a projection sees it, whose names contain search (any case), in record order. Closed accounts are left out; lines already on the Graph stay.
func lineCandidates(rec *model.Record, search string) []lineCandidate {
	needle := strings.ToLower(search)
	closed := map[model.AssetID]bool{}
	for _, a := range rec.Accounts {
		closed[model.DefaultAssetID(a.ID)] = a.IsClosed()
	}
	var out []lineCandidate
	for _, a := range rec.Assets {
		if closed[a.ID] || needle != "" && !strings.Contains(strings.ToLower(a.Name), needle) {
			continue
		}
		out = append(out, lineCandidate{a.ID, a.Name, assetKind(a.ID)})
	}
	return out
}

// addPopup is the Add line dialog: a searchable list of assets and accounts, including the accounts the chosen projection defines, the projection and the colour.
func (g *graphTab) addPopup() {
	if !imgui.BeginPopupV("add line", 0) {
		return
	}
	s := uiScale
	imgui.SetNextItemWidth(imgui.FontSize() * 20)
	imgui.InputTextWithHint("##search", "Search assets and accounts", &g.search, 0, nil)

	if imgui.BeginChildStrV("##candidates", imgui.NewVec2(imgui.FontSize()*20, 180*s), imgui.ChildFlagsBorders, 0) {
		view := effective(g.rec, g.addProjection())
		seenAsset := false
		for _, c := range lineCandidates(view, g.search) {
			if imgui.SelectableBoolV(iconLabel(c.kind, c.name)+"##"+string(c.id), g.addAsset == c.id, 0, imgui.NewVec2(0, 0)) {
				g.addAsset = c.id
				g.addDefault = colorFloats(graph.AssetColor(view, c.id))
				g.addColor = g.addDefault
			}
			itemIcon(selectableTextX(), c.kind)
			scrollToAssets("add-line", c.kind, &seenAsset)
		}
	}
	imgui.EndChild()

	imgui.SetNextItemWidth(imgui.FontSize() * 12)
	if imgui.BeginComboV("Projection", orFollow(g.addProj), 0) {
		if imgui.SelectableBoolV(followSwitcher, g.addProj == "", 0, imgui.NewVec2(0, 0)) {
			g.addProj = ""
		}
		for _, p := range g.rec.Projections {
			if imgui.SelectableBoolV(p.Name, g.addProj == p.Name, 0, imgui.NewVec2(0, 0)) {
				g.addProj = p.Name
			}
		}
		imgui.EndCombo()
	}
	if g.addAsset == "" {
		disabledText("Colour: chosen with the asset")
	} else {
		imgui.ColorEdit3V("Colour", &g.addColor, imgui.ColorEditFlagsNoInputs)
	}

	imgui.Separator()
	imgui.BeginDisabledV(g.addAsset == "")
	imgui.PushStyleColorVec4(imgui.ColButton, rgba(colSelect, 1))
	if imgui.Button("Add") {
		id := g.lines.Add(g.addAsset, g.addProj != "", g.addProj, graph.AssetColor(effective(g.rec, g.addProjection()), g.addAsset))
		if g.addColor != g.addDefault {
			g.lines.SetColor(id, floatsColor(g.addColor))
		}
		g.fitY = true
		imgui.CloseCurrentPopup()
	}
	imgui.PopStyleColor()
	imgui.EndDisabled()
	imgui.SameLine()
	if imgui.Button("Cancel") {
		imgui.CloseCurrentPopup()
	}
	imgui.EndPopup()
}

func orFollow(proj string) string {
	if proj == "" {
		return followSwitcher
	}
	return proj
}
