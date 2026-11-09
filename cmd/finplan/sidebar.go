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
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Lines")
	imgui.SameLineV(imgui.ContentRegionAvail().X-imgui.FontSize()*4.5+imgui.CursorPos().X, 0)
	if imgui.Button("+ Add") {
		g.search, g.addAsset, g.addProj = "", "", ""
		imgui.OpenPopupStrV("add line", 0)
	}
	g.addPopup()
	imgui.Separator()

	for _, ln := range g.lines.All() {
		g.row(ln)
	}
}

// row draws one line: visibility checkbox, colour swatch, asset name and projection, with pin and
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
	sw := fh * 0.7
	imgui.SetCursorScreenPos(imgui.NewVec2(p.X+4*s+fh+6*s, p.Y+(h-sw)/2))
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
	textX := p.X + 4*s + fh + 6*s + sw + 8*s
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
