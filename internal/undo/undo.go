// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package undo is a window-free undo and redo history. It stores steps and does not know what they mean: the
// caller decides what undoing a step does, and moves the history only once that has worked.
package undo

// Step is one user action: a label for the menu and whatever the caller needs to undo and redo it.
type Step[T any] struct {
	Label string
	Data  T
}

// History is the steps that can be undone and the steps that can be redone. The zero History is empty.
type History[T any] struct {
	done, undone []Step[T]
}

// Push records a new step. It can no longer be redone what was undone before it.
func (h *History[T]) Push(label string, data T) {
	h.done = append(h.done, Step[T]{label, data})
	h.undone = nil
}

// CanUndo reports whether there is a step to undo.
func (h *History[T]) CanUndo() bool { return len(h.done) > 0 }

// CanRedo reports whether there is a step to redo.
func (h *History[T]) CanRedo() bool { return len(h.undone) > 0 }

// PeekUndo returns the step Undo would move, and false if there is none.
func (h *History[T]) PeekUndo() (Step[T], bool) {
	if len(h.done) == 0 {
		return Step[T]{}, false
	}
	return h.done[len(h.done)-1], true
}

// PeekRedo returns the step Redo would move, and false if there is none.
func (h *History[T]) PeekRedo() (Step[T], bool) {
	if len(h.undone) == 0 {
		return Step[T]{}, false
	}
	return h.undone[len(h.undone)-1], true
}

// Undo moves the latest step to the redo side and returns it, and false if there is none.
func (h *History[T]) Undo() (Step[T], bool) {
	s, ok := h.PeekUndo()
	if ok {
		h.done = h.done[:len(h.done)-1]
		h.undone = append(h.undone, s)
	}
	return s, ok
}

// Redo moves the latest undone step back and returns it, and false if there is none.
func (h *History[T]) Redo() (Step[T], bool) {
	s, ok := h.PeekRedo()
	if ok {
		h.undone = h.undone[:len(h.undone)-1]
		h.done = append(h.done, s)
	}
	return s, ok
}

// Clear forgets every step.
func (h *History[T]) Clear() { h.done, h.undone = nil, nil }
