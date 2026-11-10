// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
)

// iconKind says whether a thing is an account or an asset; iconNone is neither, such as a rule member.
type iconKind int

const (
	iconNone iconKind = iota
	iconAsset
	iconAccount
)

// String is the plain-language name of the kind, empty for iconNone; the CLI and the icon tooltips use it.
func (k iconKind) String() string {
	switch k {
	case iconAsset:
		return "asset"
	case iconAccount:
		return "account"
	}
	return ""
}

// assetKind classifies an asset id: an account's default asset (id acct:<account>) is the account, every other asset an asset.
func assetKind(id model.AssetID) iconKind {
	if isDefaultAsset(id) {
		return iconAccount
	}
	return iconAsset
}

// memberKind classifies a member of an asset: an account member is an account, an asset member as assetKind says, a rule neither.
func memberKind(m model.Member) iconKind {
	switch {
	case m.Asset != "":
		return assetKind(m.Asset)
	case m.Rule != nil:
		return iconNone
	}
	return iconAccount
}

// iconSeg is a line segment of an icon, in pixels from the top left of its square box.
type iconSeg struct{ ax, ay, bx, by float32 }

// iconSegments returns the line art of the kind's icon in a square box of the given size: a checkbook for an account, a cube for an asset, none for iconNone. Every segment lies inside the box.
func iconSegments(k iconKind, size float32) []iconSeg {
	var unit []iconSeg
	switch k {
	case iconAccount:
		const l, r, t, b = 0.06, 0.94, 0.18, 0.82
		unit = []iconSeg{
			{l, t, r, t}, {r, t, r, b}, {r, b, l, b}, {l, b, l, t}, // the cover
			{0.28, t, 0.28, b},       // the binding
			{0.46, 0.40, 0.80, 0.40}, // check lines
			{0.46, 0.60, 0.80, 0.60},
		}
	case iconAsset:
		unit = []iconSeg{
			{0.5, 0.04, 0.92, 0.27}, {0.92, 0.27, 0.92, 0.73}, {0.92, 0.73, 0.5, 0.96}, // the outline
			{0.5, 0.96, 0.08, 0.73}, {0.08, 0.73, 0.08, 0.27}, {0.08, 0.27, 0.5, 0.04},
			{0.5, 0.5, 0.08, 0.27}, {0.5, 0.5, 0.92, 0.27}, {0.5, 0.5, 0.5, 0.96}, // the edges meeting at the near corner
		}
	}
	out := make([]iconSeg, len(unit))
	for i, s := range unit {
		out[i] = iconSeg{s.ax * size, s.ay * size, s.bx * size, s.by * size}
	}
	return out
}

// iconSize is the side of an icon's box in pixels: about the text height, a whole number of pixels.
func iconSize() float32 { return float32(math.Round(float64(imgui.FontSize()) * 0.9)) }

// iconSlot is the width an icon and the gap after it take in front of a name.
func iconSlot() float32 { return iconSize() + float32(math.Round(float64(imgui.FontSize())*0.3)) }

// iconPad returns the spaces that keep a label's text clear of an icon drawn over its start, none for iconNone.
func iconPad(k iconKind) string {
	if k == iconNone {
		return ""
	}
	return padFor(iconSlot(), func(s string) float32 { return imgui.CalcTextSize(s).X })
}

// padFor returns the fewest spaces whose width, as width measures a string, is at least slot. ImGui rounds a text's width up, so the spaces are measured together, not one by one.
func padFor(slot float32, width func(string) float32) string {
	pad := ""
	for width(pad) < slot {
		pad += " "
	}
	return pad
}

// selectableTextX is where the text of the selectable just drawn starts: a selectable's rectangle reaches half the item spacing left of its text.
func selectableTextX() float32 {
	return imgui.ItemRectMin().X + float32(math.Floor(float64(imgui.CurrentStyle().ItemSpacing().X)*0.5))
}

// iconLabel returns label with room for the kind's icon in front of it.
func iconLabel(k iconKind, label string) string { return iconPad(k) + label }

// iconRect returns the box of an icon with its left edge at x, vertically centred on cy, on whole pixels.
func iconRect(x, cy float32) (min, max imgui.Vec2) {
	size := iconSize()
	x = float32(math.Round(float64(x)))
	y := float32(math.Round(float64(cy - size/2)))
	return imgui.NewVec2(x, y), imgui.NewVec2(x+size, y+size)
}

// drawKindIcon draws the icon with its left edge at x, vertically centred on cy.
func drawKindIcon(dl *imgui.DrawList, x, cy float32, k iconKind, col uint32) {
	min, _ := iconRect(x, cy)
	thick := max(1, 1.25*uiScale)
	for _, s := range iconSegments(k, iconSize()) {
		dl.AddLineArgs(imgui.NewVec2(min.X+s.ax, min.Y+s.ay), imgui.NewVec2(min.X+s.bx, min.Y+s.by), col, thick)
	}
}

// hoverIcon reports whether the mouse is over the icon at x centred on cy, and then shows its name, "Account" or "Asset", in a tooltip.
func hoverIcon(x, cy float32, k iconKind) bool {
	if k == iconNone || !imgui.IsWindowHovered() {
		return false
	}
	min, max := iconRect(x, cy)
	if !imgui.IsMouseHoveringRectV(min, max, true) {
		return false
	}
	name := k.String()
	tooltipText(strings.ToUpper(name[:1]) + name[1:])
	return true
}

// itemIcon draws the icon with its left edge at x, centred on the height of the item just drawn, and shows its tooltip while the mouse is over it, which it reports.
func itemIcon(x float32, k iconKind) bool {
	if k == iconNone {
		return false
	}
	cy := (imgui.ItemRectMin().Y + imgui.ItemRectMax().Y) / 2
	drawKindIcon(imgui.WindowDrawList(), x, cy, k, iconColor())
	return hoverIcon(x, cy, k)
}

// iconColor is the colour icons are drawn in.
func iconColor() uint32 { return u32(rgba(colText, 0.9)) }
