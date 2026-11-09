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
	if imgui.Button("+ Add") {
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
	hovered := imgui.IsWindowHovered() && imgui.IsMouseHoveringRectV(p, end, true)
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

	// Solo button: faint while the row is hovered, lit while soloed. Ctrl-click clears every solo.
	soloed := g.lines.Soloed(ln.ID)
	sx := p.X + 4*s + fh + 2*s
	imgui.SetCursorScreenPos(imgui.NewVec2(sx, p.Y+(h-fh)/2))
	soloClicked := imgui.InvisibleButton("##solo", imgui.NewVec2(fh, fh))
	soloHot := imgui.IsItemHovered()
	if soloClicked {
		if imgui.IsKeyDown(imgui.ModCtrl) {
			g.lines.ClearSolo()
		} else {
			g.lines.ToggleSolo(ln.ID)
		}
		g.fitY = true
	}
	if soloed || hovered {
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
	if soloHot {
		imgui.SetTooltip("Solo: draw only soloed lines (Ctrl-click clears all)")
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
	dl.AddTextVec2(imgui.NewVec2(textX, p.Y+pad), u32(rgba(colText, 1)), g.assetName(ln.Asset))
	proj, _ := g.lines.Effective(ln.ID)
	line2 := imgui.NewVec2(textX, p.Y+pad+imgui.TextLineHeight())
	if ln.Pinned {
		drawPin(dl, imgui.NewVec2(textX+5*s, line2.Y+imgui.TextLineHeight()/2), 11*s, u32(rgba(colMuted, 1)))
		dl.AddTextVec2(imgui.NewVec2(textX+14*s, line2.Y), u32(rgba(colMuted, 1)), proj)
	} else {
		dl.AddTextVec2(line2, u32(rgba(colAccentLit, 1)), proj)
	}

	// Pin and remove buttons.
	if hovered || ln.Pinned {
		pinAlpha := float32(0.35)
		if ln.Pinned {
			pinAlpha = 0.9
		}
		bx := p.X + w - 2*fh - 4*s
		imgui.SetCursorScreenPos(imgui.NewVec2(bx, p.Y+(h-fh)/2))
		if imgui.InvisibleButton("##pin", imgui.NewVec2(fh, fh)) {
			g.lines.TogglePin(ln.ID)
		}
		if imgui.IsItemHovered() {
			pinAlpha = 1
			tip := "Pin to " + g.lines.Switcher()
			if ln.Pinned {
				tip = "Unpin: follow the switcher"
			}
			imgui.SetTooltip(tip)
		}
		if hovered || ln.Pinned {
			drawPin(dl, imgui.NewVec2(bx+fh/2, p.Y+h/2), 13*s, u32(rgba(colText, pinAlpha)))
		}
	}
	if hovered {
		bx := p.X + w - fh - 2*s
		imgui.SetCursorScreenPos(imgui.NewVec2(bx, p.Y+(h-fh)/2))
		if imgui.InvisibleButton("##remove", imgui.NewVec2(fh, fh)) {
			g.lines.Remove(ln.ID)
			g.fitY = true
		}
		alpha := float32(0.35)
		if imgui.IsItemHovered() {
			alpha = 1
			imgui.SetTooltip("Remove line")
		}
		c := imgui.NewVec2(bx+fh/2, p.Y+h/2)
		r := 4 * s
		col := u32(rgba(colText, alpha))
		dl.AddLineArgs(imgui.NewVec2(c.X-r, c.Y-r), imgui.NewVec2(c.X+r, c.Y+r), col, 1.5*s)
		dl.AddLineArgs(imgui.NewVec2(c.X-r, c.Y+r), imgui.NewVec2(c.X+r, c.Y-r), col, 1.5*s)
	}

	imgui.SetCursorScreenPos(imgui.NewVec2(p.X, p.Y+h))
	imgui.Dummy(imgui.NewVec2(w, imgui.CurrentStyle().ItemSpacing().Y))
}

// addPopup is the Add line dialog: a searchable list of assets and accounts, the projection and the colour.
func (g *graphTab) addPopup() {
	if !imgui.BeginPopupV("add line", 0) {
		return
	}
	s := uiScale
	imgui.SetNextItemWidth(imgui.FontSize() * 20)
	imgui.InputTextWithHint("##search", "Search assets and accounts", &g.search, 0, nil)

	if imgui.BeginChildStrV("##candidates", imgui.NewVec2(imgui.FontSize()*20, 180*s), imgui.ChildFlagsBorders, 0) {
		needle := strings.ToLower(g.search)
		accounts := map[model.AssetID]bool{}
		for _, a := range g.rec.Accounts {
			accounts[model.DefaultAssetID(a.ID)] = true
		}
		for _, a := range g.rec.Assets {
			if needle != "" && !strings.Contains(strings.ToLower(a.Name), needle) {
				continue
			}
			if imgui.SelectableBoolV(a.Name+"##"+string(a.ID), g.addAsset == a.ID, 0, imgui.NewVec2(0, 0)) {
				g.addAsset = a.ID
				g.addDefault = colorFloats(graph.AssetColor(g.rec, a.ID))
				g.addColor = g.addDefault
			}
			kind := "asset"
			if accounts[a.ID] {
				kind = "account"
			}
			imgui.SameLineV(imgui.WindowWidth()-imgui.CalcTextSize(kind).X-imgui.CurrentStyle().WindowPadding().X*1.5, 0)
			imgui.PushStyleColorVec4(imgui.ColText, rgba(colMuted, 1))
			imgui.TextUnformatted(kind)
			imgui.PopStyleColor()
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
	imgui.ColorEdit3V("Colour", &g.addColor, imgui.ColorEditFlagsNoInputs)

	imgui.Separator()
	imgui.BeginDisabledV(g.addAsset == "")
	imgui.PushStyleColorVec4(imgui.ColButton, rgba(colSelect, 1))
	if imgui.Button("Add") {
		id := g.lines.Add(g.addAsset, g.addProj != "", g.addProj, graph.AssetColor(g.rec, g.addAsset))
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
