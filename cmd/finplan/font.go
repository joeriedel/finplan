// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	_ "embed"
	"unsafe"

	"github.com/AllenDang/cimgui-go/imgui"
)

// fontSize is the base font size in pixels at a display scale of 1.
const fontSize = 13

// Inter Regular, from the official rsms/inter release 4.1, under the SIL Open Font License (fonts/OFL.txt).
// Its default figures are proportional: ImGui does no OpenType feature shaping, so the tabular-figure
// feature is unavailable.
//
//go:embed fonts/Inter-Regular.ttf
var interTTF string

// loadFont makes Inter the default font. The embedded data lives in read-only memory that neither moves
// nor is freed, so the atlas may point at it without owning it.
func loadFont() {
	cfg := imgui.NewFontConfig()
	defer cfg.Destroy()
	cfg.SetFontDataOwnedByAtlas(false)
	data := unsafe.StringData(interTTF)
	imgui.CurrentIO().Fonts().AddFontFromMemoryTTFV(uintptr(unsafe.Pointer(data)), int32(len(interTTF)), fontSize, cfg, nil)
	imgui.CurrentStyle().SetFontSizeBase(fontSize)
}
