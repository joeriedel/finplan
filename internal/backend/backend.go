// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package backend is the SDL3 + OpenGL3 platform layer for Dear ImGui. It wraps the upstream
// imgui_impl_sdl3 and imgui_impl_opengl3 backends; imgui core itself comes from cimgui-go.
package backend

// #cgo CPPFLAGS: -DIMGUI_USE_WCHAR32 -DIMGUI_DISABLE_OBSOLETE_FUNCTIONS=1 -I${SRCDIR}
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

// CheckVersion verifies that this package's view of the ImGui struct layouts matches cimgui-go's prebuilt library.
// A mismatch aborts the process through ImGui's assert handler.
func CheckVersion() {
	C.FinplanBackend_CheckVersion()
}

// Shutdown releases the backends, the GL context and the window.
func Shutdown() {
	C.FinplanBackend_Shutdown()
}

// WaitEvents blocks for up to timeoutMs milliseconds (0 polls) and feeds pending events to ImGui.
func WaitEvents(timeoutMs int) int {
	return int(C.FinplanBackend_WaitEvents(C.int(timeoutMs)))
}

// SaveResult is the outcome of a save dialog: the chosen Path, or Cancelled, or Err.
type SaveResult struct {
	Path      string
	Cancelled bool
	Err       error
}

// ShowSaveDialog opens a save dialog offering defaultName and a .db filter, and reports false if one is already
// open. The outcome comes from PollSaveDialog; the dialog's completion wakes WaitEvents.
func ShowSaveDialog(defaultName string) bool {
	cName := C.CString(defaultName)
	defer C.free(unsafe.Pointer(cName))
	return bool(C.FinplanBackend_ShowSaveFileDialog(cName))
}

// PollSaveDialog returns the finished dialog's outcome once, or false if there is none waiting.
func PollSaveDialog() (SaveResult, bool) {
	buf := make([]byte, 4096)
	kind := C.FinplanBackend_PollSaveFile((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	text := C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
	switch kind {
	case C.FINPLAN_SAVE_CHOSEN:
		return SaveResult{Path: text}, true
	case C.FINPLAN_SAVE_CANCELLED:
		return SaveResult{Cancelled: true}, true
	case C.FINPLAN_SAVE_ERROR:
		return SaveResult{Err: errors.New(text)}, true
	}
	return SaveResult{}, false
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
