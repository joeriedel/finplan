# Vendored Dear ImGui files

These files are third-party code and carry no finplan header:

- `imgui_impl_sdl3.cpp`, `imgui_impl_sdl3.h`
- `imgui_impl_opengl3.cpp`, `imgui_impl_opengl3.h`, `imgui_impl_opengl3_loader.h`
- `imgui.h`, `imconfig.h`

Source: Dear ImGui (https://github.com/ocornut/imgui) commit `11bd4e7731dbd8745a9eccacdaac837dd0d2a38b` (version 1.93.0 WIP, `IMGUI_VERSION_NUM` 19292). That is the commit recorded in `cwrappers/VERSION.txt` of github.com/AllenDang/cimgui-go v1.6.1 as `cimgui/imgui HEAD`.

The backend files and `imgui.h` are byte-identical to upstream at that commit. `imconfig.h` is copied from cimgui-go v1.6.1 (`cwrappers/imgui/imconfig.h`); it differs from upstream by routing `IM_ASSERT` to cimgui-go's Go assert handler, and its content does not decide the struct layout.

The prebuilt `cimgui.a` is also compiled with `IMGUI_DISABLE_OBSOLETE_FUNCTIONS=1`, set in cimgui-go's `lib/CMakeLists.txt` (not in `imconfig.h`, and not in cimgui-go's `cflags.go`). `imgui.h` removes `ImGuiIO`/`ImGuiStyle` fields under that define, so the cgo `CPPFLAGS` in `backend.go` must carry the same layout-affecting defines as the library: `-DIMGUI_USE_WCHAR32` and `-DIMGUI_DISABLE_OBSOLETE_FUNCTIONS=1`. Recheck them against `lib/CMakeLists.txt` on every cimgui-go bump; `TestLayoutMatchesCimguiGo` (`IMGUI_CHECKVERSION`) fails when they drift.

imgui core is not compiled here: it comes from cimgui-go's `cimgui.a`. `imgui.h` and `imconfig.h` are only the headers matching that library. When bumping cimgui-go, refresh all of these files from the commit in its `cwrappers/VERSION.txt`.
