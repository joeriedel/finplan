// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// frameTimesRunLimit bounds the run of the binary, which pans, zooms and hovers from four views with and without Monte Carlo, and waits for the background work before each.
	frameTimesRunLimit = 12 * time.Minute
	// frameStallBound is the longest a frame may take in the test. The goal is a frame within frameBudget, 16 ms; the bound is wider because the tests of the other packages share the machine's processors, and still far below the stalls of 1 to 15 seconds that computing on the frame made, so that it fails if the engine runs on the UI thread again.
	frameStallBound = 150 * time.Millisecond
)

// The Graph's pan, zoom and hover never stall a frame, from the All, Age 95, 30Y and 5Y views, with and without Monte Carlo.
func TestFrameTimesHaveNoStall(t *testing.T) {
	if testing.Short() {
		t.Skip("the frame-time run takes minutes")
	}
	bin := buildBinary(t)
	tmp := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), frameTimesRunLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--demo", "--screenshot", filepath.Join(tmp, "x.png"), "--tab", "Graph", "--frame-times")
	cmd.Env = smokeEnv(tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("frame-times: %v\n%s", err, out)
	}
	rows := 0
	for _, line := range strings.Split(string(out), "\n") {
		// A row is view, gesture, frames, mean, p95, max, over, stall.
		f := strings.Fields(line)
		if len(f) != 8 {
			continue
		}
		if _, err := strconv.Atoi(f[2]); err != nil {
			continue
		}
		rows++
		worst, err := time.ParseDuration(f[5])
		if err != nil {
			t.Fatalf("the max of %q: %v", line, err)
		}
		if worst > frameStallBound {
			t.Errorf("%s %s: a frame took %v, over %v: %s", f[0], f[1], worst, frameStallBound, line)
		}
	}
	if want := 2 * len(frameViews) * len(frameGestures()); rows != want {
		t.Errorf("%d rows in the table, want %d\n%s", rows, want, out)
	}
}
