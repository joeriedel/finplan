// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/AllenDang/cimgui-go/implot"
)

// Slate palette.
const (
	colWindow    = 0x23262E
	colPanel     = 0x2A2E37
	colStrip     = 0x1D2027
	colInput     = 0x1F2229
	colButton    = 0x353B47
	colBorder    = 0x363B46
	colText      = 0xD6DBE4
	colMuted     = 0x9AA3B2
	colAccent    = 0x6EA8FE
	colAccentLit = 0x8AB8FF
	colSelect    = 0x3D6FB6
)

// uiScale is the display scale most recently applied to the style, for sizes drawn by hand.
var uiScale float32 = 1

// rgba converts a 0xRRGGBB colour and an alpha to an ImGui colour.
func rgba(hex uint32, alpha float32) imgui.Vec4 {
	return imgui.NewVec4(float32(hex>>16&0xff)/255, float32(hex>>8&0xff)/255, float32(hex&0xff)/255, alpha)
}

func u32(c imgui.Vec4) uint32 { return imgui.ColorConvertFloat4ToU32(c) }

// applySlate sets the Slate theme on a style that has not been scaled yet.
func applySlate(st *imgui.Style) {
	c := st.Colors()
	set := func(i imgui.Col, hex uint32, alpha float32) { c[i] = rgba(hex, alpha) }
	set(imgui.ColText, colText, 1)
	set(imgui.ColTextDisabled, colMuted, 1)
	set(imgui.ColWindowBg, colWindow, 1)
	set(imgui.ColChildBg, colPanel, 1)
	set(imgui.ColPopupBg, colPanel, 1)
	set(imgui.ColBorder, colBorder, 1)
	set(imgui.ColBorderShadow, 0, 0)
	set(imgui.ColFrameBg, colInput, 1)
	set(imgui.ColFrameBgHovered, 0x262A33, 1)
	set(imgui.ColFrameBgActive, 0x2C313B, 1)
	set(imgui.ColTitleBg, colStrip, 1)
	set(imgui.ColTitleBgActive, colStrip, 1)
	set(imgui.ColTitleBgCollapsed, colStrip, 1)
	set(imgui.ColMenuBarBg, colStrip, 1)
	set(imgui.ColScrollbarBg, colInput, 0.6)
	set(imgui.ColScrollbarGrab, colButton, 1)
	set(imgui.ColScrollbarGrabHovered, 0x4A5162, 1)
	set(imgui.ColScrollbarGrabActive, 0x5A6275, 1)
	set(imgui.ColCheckMark, colAccent, 1)
	set(imgui.ColCheckboxSelectedBg, colInput, 1)
	set(imgui.ColSliderGrab, colAccent, 1)
	set(imgui.ColSliderGrabActive, colAccentLit, 1)
	set(imgui.ColButton, colButton, 1)
	set(imgui.ColButtonHovered, 0x414857, 1)
	set(imgui.ColButtonActive, colSelect, 1)
	set(imgui.ColHeader, colButton, 1)
	set(imgui.ColHeaderHovered, 0x3B4250, 1)
	set(imgui.ColHeaderActive, colSelect, 1)
	set(imgui.ColSeparator, colBorder, 1)
	set(imgui.ColSeparatorHovered, colAccent, 0.7)
	set(imgui.ColSeparatorActive, colAccent, 1)
	set(imgui.ColResizeGrip, colButton, 0.6)
	set(imgui.ColResizeGripHovered, colAccent, 0.6)
	set(imgui.ColResizeGripActive, colAccent, 0.9)
	set(imgui.ColInputTextCursor, colText, 1)
	set(imgui.ColTabHovered, colButton, 1)
	set(imgui.ColTab, colStrip, 1)
	set(imgui.ColTabSelected, colWindow, 1)
	set(imgui.ColTabSelectedOverline, colAccent, 1)
	set(imgui.ColTabDimmed, colStrip, 1)
	set(imgui.ColTabDimmedSelected, colWindow, 1)
	set(imgui.ColTabDimmedSelectedOverline, colAccent, 0.5)
	set(imgui.ColTableHeaderBg, colPanel, 1)
	set(imgui.ColTableBorderStrong, colBorder, 1)
	set(imgui.ColTableBorderLight, colBorder, 0.6)
	set(imgui.ColTableRowBg, 0, 0)
	set(imgui.ColTableRowBgAlt, 0xFFFFFF, 0.03)
	set(imgui.ColTextLink, colAccent, 1)
	set(imgui.ColTextSelectedBg, colSelect, 0.6)
	set(imgui.ColDragDropTarget, colAccent, 0.9)
	set(imgui.ColNavCursor, colAccent, 1)
	set(imgui.ColNavWindowingHighlight, 0xFFFFFF, 0.7)
	set(imgui.ColNavWindowingDimBg, 0, 0.35)
	set(imgui.ColModalWindowDimBg, 0, 0.5)
	st.SetColors(&c)

	st.SetWindowRounding(6)
	st.SetChildRounding(5)
	st.SetPopupRounding(6)
	st.SetFrameRounding(5)
	st.SetGrabRounding(4)
	st.SetTabRounding(5)
	st.SetScrollbarRounding(6)
	st.SetWindowBorderSize(1)
	st.SetChildBorderSize(1)
	st.SetPopupBorderSize(1)
	st.SetFrameBorderSize(0)
	st.SetTabBarOverlineSize(2)
	st.SetWindowPadding(imgui.NewVec2(10, 8))
	st.SetFramePadding(imgui.NewVec2(8, 4))
	st.SetItemSpacing(imgui.NewVec2(8, 6))
}

// applySlatePlot sets the Slate colours on ImPlot's style. They do not scale with the display.
func applySlatePlot() {
	st := implot.GetStyle()
	c := st.Colors()
	set := func(i implot.Col, hex uint32, alpha float32) { c[i] = rgba(hex, alpha) }
	set(implot.ColFrameBg, colWindow, 0)
	set(implot.ColPlotBg, colInput, 1)
	set(implot.ColPlotBorder, colBorder, 1)
	set(implot.ColLegendBg, colPanel, 0.95)
	set(implot.ColLegendBorder, colBorder, 1)
	set(implot.ColLegendText, colText, 1)
	set(implot.ColTitleText, colText, 1)
	set(implot.ColInlayText, colText, 1)
	set(implot.ColAxisText, colMuted, 1)
	set(implot.ColAxisGrid, colMuted, 0.16)
	set(implot.ColAxisTick, colMuted, 0.4)
	set(implot.ColAxisBg, colWindow, 0)
	set(implot.ColAxisBgHovered, colButton, 1)
	set(implot.ColAxisBgActive, colSelect, 1)
	set(implot.ColSelection, colAccent, 0.9)
	set(implot.ColCrosshairs, colMuted, 0.5)
	st.SetColors(&c)
}
