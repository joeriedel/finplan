// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
)

func TestIDScanRecord(t *testing.T) {
	s := newIDScan(100, 50, 10, 10)
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
	s := newIDScan(95, 25, 10, 10)
	if s.cols != 10 || s.rows != 3 {
		t.Errorf("grid = %dx%d, want 10x3", s.cols, s.rows)
	}
	if s.done() {
		t.Error("a new scan is done")
	}
	if err := s.tooLarge(); err != nil {
		t.Errorf("a small grid is too large: %v", err)
	}
	if p := s.point(11); p.X != 10 || p.Y != 10 {
		t.Errorf("point 11 = %v, want (10, 10)", p)
	}
	s = newIDScan(95, 25, 19, 5)
	if s.cols != 6 || s.rows != 6 {
		t.Errorf("grid with steps 19x5 = %dx%d, want 6x6", s.cols, s.rows)
	}
}

func TestIDScanFrameBound(t *testing.T) {
	s := newIDScan(1280, 800, 13, 13)
	if err := s.tooLarge(); err != nil {
		t.Errorf("1280x800 at step 13 is too large: %v", err)
	}
	s = newIDScan(1280, 4000, 13, 13)
	if err := s.tooLarge(); err == nil || !strings.Contains(err.Error(), "frames") {
		t.Errorf("a grid of %d points was not refused: %v", s.points(), err)
	}
	s.frame = idScanMaxFrames + 1
	if !s.done() {
		t.Error("a scan past the frame bound is not done")
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
	s := newIDScan(100, 100, 10, 10)
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

func TestIDScanTimeBound(t *testing.T) {
	start := time.Now()
	if err := checkTime(start, start.Add(idScanMaxTime)); err != nil {
		t.Errorf("a scan at the bound was refused: %v", err)
	}
	if err := checkTime(start, start.Add(idScanMaxTime+time.Second)); err == nil || !strings.Contains(err.Error(), "id scan: exceeded") {
		t.Errorf("a scan past the bound was not refused: %v", err)
	}
}
