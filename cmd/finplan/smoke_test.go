// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// buildBinary builds finplan into a temporary folder.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "finplan")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// smokeEnv runs the GUI offscreen with temp files in tmp.
func smokeEnv(tmp string) []string {
	return append(os.Environ(), "SDL_VIDEO_DRIVER=offscreen", "TMPDIR="+tmp)
}

func TestScreenshotSmoke(t *testing.T) {
	bin := buildBinary(t)
	outDir := os.Getenv("FINPLAN_SMOKE_OUT")
	if outDir == "" {
		outDir = t.TempDir()
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()

	cases := []struct{ name, tab, sel string }{
		{"graph", "Graph", ""},
		{"projections", "Projections", ""},
		{"assets", "Assets", ""},
		{"accounts", "Accounts", ""},
		{"budget", "Budget", ""},
		{"markets", "Markets", ""},
		{"accounts-selected", "Accounts", "Checking"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(outDir, c.name+".png")
			os.Remove(path)
			args := []string{"--demo", "--screenshot", path, "--tab", c.tab}
			if c.sel != "" {
				args = append(args, "--select", c.sel)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, args...)
			cmd.Env = smokeEnv(tmp)
			out, err := cmd.CombinedOutput()
			if err != nil || bytes.Contains(out, []byte("panic")) {
				t.Fatalf("finplan %s: %v\n%s", strings.Join(args, " "), err, out)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			img, err := png.Decode(f)
			if err != nil {
				t.Fatal(err)
			}
			if b := img.Bounds(); b.Dx() != 1280 || b.Dy() != 800 {
				t.Errorf("size = %v, want 1280x800", b)
			}
			if !hasTwoColours(img) {
				t.Errorf("the screenshot is a single colour")
			}
		})
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, demoPrefix+"*")); len(left) != 0 {
		t.Errorf("demo folders left behind: %v", left)
	}
}

func TestScreenshotBadArguments(t *testing.T) {
	bin := buildBinary(t)
	for _, args := range [][]string{
		{"--screenshot", filepath.Join(t.TempDir(), "x.png"), "--tab", "Nope"},
		{"--screenshot", filepath.Join(t.TempDir(), "x.png"), "--tab", "Graph", "--select", "Checking"},
		{"--demo", "--screenshot", filepath.Join(t.TempDir(), "x.png"), "--tab", "Assets", "--select", "No such asset"},
		{"--tab", "Assets"},
	} {
		cmd := exec.Command(bin, args...)
		cmd.Env = smokeEnv(t.TempDir())
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Errorf("finplan %s succeeded:\n%s", strings.Join(args, " "), out)
		}
	}
}

// TestDemoSigintLeavesNothing starts the GUI on a demo database and interrupts it.
func TestDemoSigintLeavesNothing(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	cmd := exec.Command(bin, "--demo")
	cmd.Env = smokeEnv(tmp)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cmd.Process.Kill()

	deadline := time.Now().Add(30 * time.Second)
	for {
		if m, _ := filepath.Glob(filepath.Join(tmp, demoPrefix+"*")); len(m) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no demo folder appeared:\n%s", out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Let the window come up; the loop notices the signal within its idle wait.
	time.Sleep(2 * time.Second)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exit after SIGINT: %v\n%s", err, out.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("still running after SIGINT:\n%s", out.String())
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, demoPrefix+"*")); len(left) != 0 {
		t.Errorf("demo folders left behind: %v", left)
	}
}

func hasTwoColours(img image.Image) bool {
	b := img.Bounds()
	r0, g0, b0, a0 := img.At(b.Min.X, b.Min.Y).RGBA()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if r, g, bl, a := img.At(x, y).RGBA(); r != r0 || g != g0 || bl != b0 || a != a0 {
				return true
			}
		}
	}
	return false
}
