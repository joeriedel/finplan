// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/AllenDang/cimgui-go/imgui"
)

func TestIDScanRecord(t *testing.T) {
	s := newIDScan(100, 50)
	s.record(0, 5, imgui.NewVec2(0, 0), "w")
	s.record(7, 1, imgui.NewVec2(0, 0), "w")
	if s.err() != nil {
		t.Fatalf("ids seen once or unset reported: %v", s.err())
	}
	s.record(7, 2, imgui.NewVec2(30, 10), "w")
	err := s.err()
	if err == nil || !strings.Contains(err.Error(), "2 visible items") || !strings.Contains(err.Error(), "(30, 10)") {
		t.Errorf("err = %v, want the count and the position", err)
	}
}

func TestIDScanGrid(t *testing.T) {
	s := newIDScan(95, 25)
	if s.cols != 10 || s.rows != 3 {
		t.Errorf("grid = %dx%d, want 10x3", s.cols, s.rows)
	}
	if s.done() {
		t.Error("a new scan is done")
	}
}

// scanButtons draws two buttons labelled "Add" in a window covering a 100x100 display, with labels the two buttons' IDs, and scans it.
func scanButtons(t *testing.T, first, second string) error {
	t.Helper()
	ctx := imgui.CreateContext()
	defer imgui.DestroyContext()
	io := imgui.CurrentIO()
	io.SetBackendFlags(io.BackendFlags() | imgui.BackendFlagsRendererHasTextures)
	io.SetDisplaySize(imgui.NewVec2(100, 100))
	io.SetDeltaTime(1.0 / 60)
	io.SetIniFilename("")
	s := newIDScan(100, 100)
	for !s.done() {
		s.before(io)
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.NewVec2(0, 0))
		imgui.SetNextWindowSize(imgui.NewVec2(100, 100))
		if imgui.BeginV("scan", nil, imgui.WindowFlagsNoDecoration|imgui.WindowFlagsNoSavedSettings) {
			imgui.Button(first)
			imgui.Button(second)
		}
		imgui.End()
		s.after(ctx)
		imgui.Render()
	}
	return s.err()
}

func TestIDScanFindsSameLabelButtons(t *testing.T) {
	if err := scanButtons(t, "Add", "Add"); err == nil {
		t.Error("two Add buttons in one scope were not reported")
	}
}

func TestIDScanAcceptsDistinctIDs(t *testing.T) {
	if err := scanButtons(t, "Add##one", "Add##two"); err != nil {
		t.Errorf("distinct IDs reported: %v", err)
	}
}
