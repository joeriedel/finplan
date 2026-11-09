// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

#include "glue.h"

#include "imgui.h"
#include "imgui_impl_opengl3.h"
#include "imgui_impl_sdl3.h"
#include <SDL3/SDL.h>
#include <mutex>
#include <string>

typedef void (*PFN_glViewport)(int x, int y, int width, int height);
typedef void (*PFN_glClearColor)(float r, float g, float b, float a);
typedef void (*PFN_glClear)(unsigned int mask);

static const unsigned int GL_COLOR_BUFFER_BIT_ = 0x00004000;

static SDL_Window*      g_Window = nullptr;
static SDL_GLContext    g_GLContext = nullptr;
static PFN_glViewport   g_glViewport = nullptr;
static PFN_glClearColor g_glClearColor = nullptr;
static PFN_glClear      g_glClear = nullptr;

// The save dialog's callback may run on another thread, so its result waits in a slot guarded by g_SaveMutex.
static std::mutex       g_SaveMutex;
static bool             g_SaveOpen = false;
static int              g_SaveResult = FINPLAN_SAVE_NONE;
static std::string      g_SaveText;
static Uint32           g_SaveEvent = 0;

void FinplanBackend_CheckVersion(void)
{
    IMGUI_CHECKVERSION();
}

bool FinplanBackend_Init(const char* title, int width, int height)
{
    // No video driver hints: SDL picks Wayland when a Wayland session is available.
    if (!SDL_Init(SDL_INIT_VIDEO))
        return false;

    SDL_GL_SetAttribute(SDL_GL_CONTEXT_FLAGS, 0);
    SDL_GL_SetAttribute(SDL_GL_CONTEXT_PROFILE_MASK, SDL_GL_CONTEXT_PROFILE_CORE);
    SDL_GL_SetAttribute(SDL_GL_CONTEXT_MAJOR_VERSION, 3);
    SDL_GL_SetAttribute(SDL_GL_CONTEXT_MINOR_VERSION, 3);
    SDL_GL_SetAttribute(SDL_GL_DOUBLEBUFFER, 1);

    g_Window = SDL_CreateWindow(title, width, height, SDL_WINDOW_OPENGL | SDL_WINDOW_RESIZABLE | SDL_WINDOW_HIGH_PIXEL_DENSITY);
    if (g_Window == nullptr)
        return false;

    g_GLContext = SDL_GL_CreateContext(g_Window);
    if (g_GLContext == nullptr)
        return false;
    SDL_GL_MakeCurrent(g_Window, g_GLContext);
    SDL_GL_SetSwapInterval(1);

    g_glViewport = (PFN_glViewport)SDL_GL_GetProcAddress("glViewport");
    g_glClearColor = (PFN_glClearColor)SDL_GL_GetProcAddress("glClearColor");
    g_glClear = (PFN_glClear)SDL_GL_GetProcAddress("glClear");
    if (g_glViewport == nullptr || g_glClearColor == nullptr || g_glClear == nullptr)
        return false;

    g_SaveEvent = SDL_RegisterEvents(1);

    SDL_ShowWindow(g_Window);

    ImGui::GetIO().IniFilename = nullptr;
    if (!ImGui_ImplSDL3_InitForOpenGL(g_Window, g_GLContext))
        return false;
    if (!ImGui_ImplOpenGL3_Init("#version 330 core"))
        return false;
    return true;
}

const char* FinplanBackend_Error(void)
{
    return SDL_GetError();
}

void FinplanBackend_Shutdown(void)
{
    if (ImGui::GetIO().BackendRendererUserData != nullptr)
        ImGui_ImplOpenGL3_Shutdown();
    if (ImGui::GetIO().BackendPlatformUserData != nullptr)
        ImGui_ImplSDL3_Shutdown();
    if (g_GLContext != nullptr)
        SDL_GL_DestroyContext(g_GLContext);
    if (g_Window != nullptr)
        SDL_DestroyWindow(g_Window);
    g_GLContext = nullptr;
    g_Window = nullptr;
    SDL_Quit();
}

int FinplanBackend_WaitEvents(int timeout_ms)
{
    int flags = 0;
    SDL_Event event;
    bool have_event = (timeout_ms > 0) ? SDL_WaitEventTimeout(&event, timeout_ms) : SDL_PollEvent(&event);
    while (have_event)
    {
        ImGui_ImplSDL3_ProcessEvent(&event);
        flags |= FINPLAN_EVENT_INPUT;
        if (event.type == SDL_EVENT_QUIT)
            flags |= FINPLAN_EVENT_QUIT;
        if (event.type == SDL_EVENT_WINDOW_CLOSE_REQUESTED && event.window.windowID == SDL_GetWindowID(g_Window))
            flags |= FINPLAN_EVENT_QUIT;
        have_event = SDL_PollEvent(&event);
    }
    return flags;
}

float FinplanBackend_UIScale(void)
{
    // The display scale combines pixel density and content scale. The backend already maps ImGui points to
    // pixels through the density, so only the content scale part is left to apply to sizes.
    float display_scale = SDL_GetWindowDisplayScale(g_Window);
    float density = SDL_GetWindowPixelDensity(g_Window);
    if (display_scale <= 0.0f || density <= 0.0f)
        return 1.0f;
    return display_scale / density;
}

void FinplanBackend_NewFrame(void)
{
    ImGui_ImplOpenGL3_NewFrame();
    ImGui_ImplSDL3_NewFrame();
}

void FinplanBackend_Render(void)
{
    ImGuiIO& io = ImGui::GetIO();
    g_glViewport(0, 0, (int)(io.DisplaySize.x * io.DisplayFramebufferScale.x), (int)(io.DisplaySize.y * io.DisplayFramebufferScale.y));
    g_glClearColor(0.137f, 0.149f, 0.180f, 1.0f);
    g_glClear(GL_COLOR_BUFFER_BIT_);
    ImGui_ImplOpenGL3_RenderDrawData(ImGui::GetDrawData());
    SDL_GL_SwapWindow(g_Window);
}

static void SDLCALL SaveFileCallback(void* /*userdata*/, const char* const* filelist, int /*filter*/)
{
    {
        std::lock_guard<std::mutex> lock(g_SaveMutex);
        if (filelist == nullptr)
        {
            g_SaveResult = FINPLAN_SAVE_ERROR;
            g_SaveText = SDL_GetError();
        }
        else if (filelist[0] == nullptr)
        {
            g_SaveResult = FINPLAN_SAVE_CANCELLED;
            g_SaveText.clear();
        }
        else
        {
            g_SaveResult = FINPLAN_SAVE_CHOSEN;
            g_SaveText = filelist[0];
        }
        g_SaveOpen = false;
    }
    SDL_Event event;
    SDL_zero(event);
    event.type = g_SaveEvent;
    SDL_PushEvent(&event);
}

bool FinplanBackend_ShowSaveFileDialog(const char* default_name)
{
    {
        std::lock_guard<std::mutex> lock(g_SaveMutex);
        if (g_SaveOpen)
            return false;
        g_SaveOpen = true;
        g_SaveResult = FINPLAN_SAVE_NONE;
    }
    static const SDL_DialogFileFilter filters[] = { { "Database (*.db)", "db" } };
    SDL_ShowSaveFileDialog(SaveFileCallback, nullptr, g_Window, filters, 1, default_name);
    return true;
}

int FinplanBackend_PollSaveFile(char* buffer, int buffer_size)
{
    std::lock_guard<std::mutex> lock(g_SaveMutex);
    int result = g_SaveResult;
    if (result == FINPLAN_SAVE_NONE)
        return FINPLAN_SAVE_NONE;
    if (buffer_size > 0)
        SDL_strlcpy(buffer, g_SaveText.c_str(), (size_t)buffer_size);
    g_SaveResult = FINPLAN_SAVE_NONE;
    return result;
}
