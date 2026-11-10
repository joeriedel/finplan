// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSweepDemos(t *testing.T) {
	tmp := t.TempDir()
	child := exec.Command("true")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	dead := child.Process.Pid

	mk := func(name, pid string) string {
		dir := filepath.Join(tmp, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if pid != "" {
			if err := os.WriteFile(filepath.Join(dir, demoOwnerFile), []byte(pid), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	alive := mk("finplan-demo-alive", strconv.Itoa(os.Getpid()))
	gone := mk("finplan-demo-dead", strconv.Itoa(dead))
	ownerless := mk("finplan-demo-ownerless", "")
	junk := mk("finplan-demo-junk", "not a pid")
	other := mk("unrelated", strconv.Itoa(dead))

	sweepDemos(tmp)

	for dir, want := range map[string]bool{alive: true, gone: false, ownerless: true, junk: true, other: true} {
		_, err := os.Stat(dir)
		if (err == nil) != want {
			t.Errorf("%s: exists = %v, want %v", filepath.Base(dir), err == nil, want)
		}
	}
}
