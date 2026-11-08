// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package backend is the SDL3 + OpenGL3 platform layer for Dear ImGui. It wraps the upstream
// imgui_impl_sdl3 and imgui_impl_opengl3 backends; imgui core itself comes from cimgui-go.
package backend

// #cgo CPPFLAGS: -DIMGUI_USE_WCHAR32 -I${SRCDIR}
// #cgo pkg-config: sdl3
// #include <stdlib.h>
// #include "glue.h"
import "C"

import (
	"errors"
	"unsafe"

	// Linked for the imgui core symbols the backend files call; also keeps the backend on the same ImGui context.
	_ "github.com/AllenDang/cimgui-go/imgui"
)

// Event flags returned by WaitEvents.
const (
	EventInput = C.FINPLAN_EVENT_INPUT
	EventQuit  = C.FINPLAN_EVENT_QUIT
)

func lastError() error {
	return errors.New(C.GoString(C.FinplanBackend_Error()))
}

// Init creates the window and GL context and initializes the ImGui backends.
// The ImGui context must already exist. Call from the thread that owns the window.
func Init(title string, width, height int) error {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	if !C.FinplanBackend_Init(cTitle, C.int(width), C.int(height)) {
		err := lastError()
		C.FinplanBackend_Shutdown()
		return err
	}
	return nil
}

// Shutdown releases the backends, the GL context and the window.
func Shutdown() {
	C.FinplanBackend_Shutdown()
}

// WaitEvents blocks for up to timeoutMs milliseconds (0 polls) and feeds pending events to ImGui.
func WaitEvents(timeoutMs int) int {
	return int(C.FinplanBackend_WaitEvents(C.int(timeoutMs)))
}

// UIScale is the factor to apply to ImGui sizes and font size.
func UIScale() float32 {
	return float32(C.FinplanBackend_UIScale())
}

// NewFrame starts a backend frame; call before imgui.NewFrame.
func NewFrame() {
	C.FinplanBackend_NewFrame()
}

// Render draws the current ImGui draw data and swaps buffers; call after imgui.Render.
func Render() {
	C.FinplanBackend_Render()
}
