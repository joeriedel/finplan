// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import "time"

// versionPoll decides when to look at the database's data version and whether a reading means another
// program wrote. It does no I/O, so the caller supplies the clock and the readings.
type versionPoll struct {
	every time.Duration
	next  time.Time
	seen  int64
}

// newVersionPoll returns a poll that is due every interval, starting from the data version seen.
func newVersionPoll(every time.Duration, seen int64) *versionPoll {
	return &versionPoll{every: every, seen: seen}
}

// due reports whether it is time to read the version, and if so schedules the next reading.
func (p *versionPoll) due(now time.Time) bool {
	if now.Before(p.next) {
		return false
	}
	p.next = now.Add(p.every)
	return true
}

// changed reports whether v differs from the last version seen, and remembers it.
func (p *versionPoll) changed(v int64) bool {
	if v == p.seen {
		return false
	}
	p.seen = v
	return true
}
