// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package backend

import "testing"

// TestLayoutMatchesCimguiGo guards the cgo flags: if they differ from the ones cimgui-go's prebuilt cimgui.a was
// compiled with, ImGuiIO/ImGuiStyle sizes differ and ImGui's layout assert aborts the process.
func TestLayoutMatchesCimguiGo(t *testing.T) {
	CheckVersion()
}
