# finplan

finplan is a personal financial planner for one user. It is a Linux desktop app written in Go, drawing its UI with Dear ImGui and ImPlot (via cimgui-go) on SDL3, as a native Wayland client. For now it shows an interactive plot of invented balance data.

The data model, projection engine and planned features are described in [docs/design.md](docs/design.md). The model and a placeholder engine exist as standalone Go packages, `internal/model` and `internal/engine`; the UI does not use them yet.

## Prerequisites

- podman, used to build and test inside a Fedora 43 container (no Go toolchain is needed on the host)
- SDL3 on the host (`libSDL3.so.0`, Fedora package `SDL3`) to run the app

## Build

    scripts/build.sh

This builds the container image on first use (and whenever the `Containerfile` changes) and produces `bin/finplan`.

## Test

    scripts/test.sh

Runs a gofmt check, `go vet` and `go test` in the container.

## Run

    bin/finplan

Drag to pan, scroll to zoom, and click a legend entry to toggle a series.
