// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
	"time"
)

func TestVersionPollDue(t *testing.T) {
	p := newVersionPoll(time.Second, 7)
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	if !p.due(t0) {
		t.Error("first check not due")
	}
	if p.due(t0.Add(500 * time.Millisecond)) {
		t.Error("due again within the interval")
	}
	if !p.due(t0.Add(time.Second)) {
		t.Error("not due after the interval")
	}
	if p.due(t0.Add(1500 * time.Millisecond)) {
		t.Error("interval not counted from the last check")
	}
}

func TestVersionPollChanged(t *testing.T) {
	p := newVersionPoll(time.Second, 7)
	if p.changed(7) {
		t.Error("same version reported as changed")
	}
	if !p.changed(9) {
		t.Error("new version not reported")
	}
	if p.changed(9) {
		t.Error("a change was reported twice")
	}
}
