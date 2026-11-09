/*
 * Copyright (c) 2026 Joseph Riedel
 * SPDX-License-Identifier: MIT
 */

// Thin glue between Go and the Dear ImGui SDL3 + OpenGL3 backends.
// The ImGui context must be created (by cimgui-go) before FinplanBackend_Init().

#pragma once

#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

// Flags returned by FinplanBackend_WaitEvents().
#define FINPLAN_EVENT_INPUT 1 // at least one event was processed
#define FINPLAN_EVENT_QUIT  2 // the window was closed or the app was asked to quit

// Results returned by FinplanBackend_PollSaveFile().
#define FINPLAN_SAVE_NONE      0 // no dialog result is waiting
#define FINPLAN_SAVE_CHOSEN    1 // the user chose a file; the path is in the buffer
#define FINPLAN_SAVE_CANCELLED 2 // the user closed the dialog without choosing
#define FINPLAN_SAVE_ERROR     3 // the dialog failed; the message is in the buffer

// Verifies that the struct layouts seen by this package match the ones cimgui.a was compiled with (IMGUI_CHECKVERSION).
// Aborts through IM_ASSERT on a mismatch. Needs no ImGui context, window or display.
void        FinplanBackend_CheckVersion(void);

bool        FinplanBackend_Init(const char* title, int width, int height);
const char* FinplanBackend_Error(void);
void        FinplanBackend_Shutdown(void);

// Waits up to timeout_ms (0 = poll) for an event, then drains the queue into ImGui.
int         FinplanBackend_WaitEvents(int timeout_ms);

// Scale to apply to ImGui sizes and fonts, in addition to the pixel density the backend already handles.
float       FinplanBackend_UIScale(void);

// Opens a native save dialog on the window, offering default_name and a .db filter. Returns false if a dialog is
// already open. The result arrives later through FinplanBackend_PollSaveFile(), and an event is pushed so that
// FinplanBackend_WaitEvents() wakes.
bool        FinplanBackend_ShowSaveFileDialog(const char* default_name);

// Takes the finished dialog's result, if any, copying the path or error message into buffer (NUL-terminated,
// truncated to buffer_size). Returns one of the FINPLAN_SAVE_* values; each result is returned once.
int         FinplanBackend_PollSaveFile(char* buffer, int buffer_size);

void        FinplanBackend_NewFrame(void);
void        FinplanBackend_Render(void);

#ifdef __cplusplus
}
#endif
