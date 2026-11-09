// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package undo

import "testing"

func TestEmpty(t *testing.T) {
	var h History[int]
	if h.CanUndo() || h.CanRedo() {
		t.Error("an empty history can move")
	}
	if _, ok := h.Undo(); ok {
		t.Error("undid nothing")
	}
	if _, ok := h.Redo(); ok {
		t.Error("redid nothing")
	}
	if _, ok := h.PeekUndo(); ok {
		t.Error("peeked nothing")
	}
}

func TestUndoRedoOrderAndLabels(t *testing.T) {
	var h History[int]
	h.Push("one", 1)
	h.Push("two", 2)
	if s, ok := h.PeekUndo(); !ok || s.Label != "two" || s.Data != 2 {
		t.Errorf("peek undo: %v %v", s, ok)
	}
	if s, ok := h.Undo(); !ok || s.Label != "two" {
		t.Errorf("undo: %v %v", s, ok)
	}
	if s, ok := h.PeekRedo(); !ok || s.Label != "two" {
		t.Errorf("peek redo: %v %v", s, ok)
	}
	if s, _ := h.PeekUndo(); s.Label != "one" {
		t.Errorf("after undo the latest is %q", s.Label)
	}
	h.Undo()
	if h.CanUndo() || !h.CanRedo() {
		t.Error("everything undone: can undo or cannot redo")
	}
	if s, ok := h.Redo(); !ok || s.Label != "one" {
		t.Errorf("redo: %v %v", s, ok)
	}
	if s, ok := h.Redo(); !ok || s.Label != "two" {
		t.Errorf("redo: %v %v", s, ok)
	}
	if h.CanRedo() || !h.CanUndo() {
		t.Error("everything redone: can redo or cannot undo")
	}
}

func TestPeekDoesNotMove(t *testing.T) {
	var h History[int]
	h.Push("a", 1)
	h.PeekUndo()
	h.PeekRedo()
	if !h.CanUndo() || h.CanRedo() {
		t.Error("peeking moved the history")
	}
}

func TestPushClearsRedo(t *testing.T) {
	var h History[int]
	h.Push("a", 1)
	h.Undo()
	h.Push("b", 2)
	if h.CanRedo() {
		t.Error("a push left the redo side")
	}
	if s, _ := h.PeekUndo(); s.Label != "b" {
		t.Errorf("latest is %q", s.Label)
	}
}

func TestClear(t *testing.T) {
	var h History[int]
	h.Push("a", 1)
	h.Push("b", 2)
	h.Undo()
	h.Clear()
	if h.CanUndo() || h.CanRedo() {
		t.Error("clear left steps")
	}
}
