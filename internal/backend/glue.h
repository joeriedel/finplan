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

bool        FinplanBackend_Init(const char* title, int width, int height);
const char* FinplanBackend_Error(void);
void        FinplanBackend_Shutdown(void);

// Waits up to timeout_ms (0 = poll) for an event, then drains the queue into ImGui.
int         FinplanBackend_WaitEvents(int timeout_ms);

// Scale to apply to ImGui sizes and fonts, in addition to the pixel density the backend already handles.
float       FinplanBackend_UIScale(void);

void        FinplanBackend_NewFrame(void);
void        FinplanBackend_Render(void);

#ifdef __cplusplus
}
#endif
