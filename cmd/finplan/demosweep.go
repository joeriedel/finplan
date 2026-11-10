// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	demoPrefix = "finplan-demo-"
	// demoOwnerFile holds the PID of the process a demo folder belongs to.
	demoOwnerFile = "owner.pid"
)

// sweepDemos removes the demo folders in tmp left behind by processes that no longer run, for example after a kill. A folder without a readable owner file is left alone, as its process may still run.
func sweepDemos(tmp string) {
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), demoPrefix) {
			continue
		}
		dir := filepath.Join(tmp, e.Name())
		if pid, ok := demoOwner(dir); ok && !pidAlive(pid) {
			os.RemoveAll(dir)
		}
	}
}

func demoOwner(dir string) (int, bool) {
	b, err := os.ReadFile(filepath.Join(dir, demoOwnerFile))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil && pid > 0
}

// pidAlive reports whether a process with the PID exists; a process owned by another user counts as alive.
func pidAlive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
