// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"runtime"
	"sync"

	"github.com/joeriedel/finplan/internal/model"
)

// backgroundWorkers is the most goroutines a bgRunner runs jobs on at once. Monte Carlo runs on all the processors, so a few workers are enough to keep the screens' own jobs from queueing behind each other.
var backgroundWorkers = max(1, min(4, runtime.GOMAXPROCS(0)-1))

// bgRunner runs jobs off the UI thread, newest first within a slot: a job queued under a slot replaces the one before it that has not started, so a view that moves faster than the engine answers queues one job per slot, not one per frame. A job checks for itself whether it is still wanted when it starts and when it finishes.
type bgRunner struct {
	mu      sync.Mutex
	slots   map[any]func()
	order   []any
	workers int
	active  int
}

func newBGRunner() *bgRunner {
	return &bgRunner{slots: map[any]func(){}}
}

// submit queues fn under the slot, replacing the job queued under it that has not started.
func (r *bgRunner) submit(slot any, fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, queued := r.slots[slot]; !queued {
		r.order = append(r.order, slot)
	}
	r.slots[slot] = fn
	if r.workers < backgroundWorkers {
		r.workers++
		go r.work()
	}
}

func (r *bgRunner) work() {
	for {
		r.mu.Lock()
		if len(r.order) == 0 {
			r.workers--
			r.mu.Unlock()
			return
		}
		slot := r.order[0]
		r.order = r.order[1:]
		fn := r.slots[slot]
		delete(r.slots, slot)
		r.active++
		r.mu.Unlock()
		fn()
		r.mu.Lock()
		r.active--
		r.mu.Unlock()
	}
}

// busy reports whether a job is queued or running.
func (r *bgRunner) busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.slots) > 0 || r.active > 0
}

// recCopy is a copy of a record that background jobs read, made on the first use so that jobs never read a record the UI goes on editing. get is called on the UI thread.
type recCopy struct {
	src *model.Record
	rec *model.Record
}

func (s *recCopy) get() *model.Record {
	if s.rec == nil {
		s.rec = s.src.Clone()
	}
	return s.rec
}
