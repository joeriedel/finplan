// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/graph"
)

func TestRowControls(t *testing.T) {
	for _, c := range []struct {
		name                            string
		hovered, pinned, soloed, active bool
		wantSolo, wantPin, wantRemove   bool
	}{
		{"idle", false, false, false, false, false, false, false},
		{"hovered", true, false, false, false, true, true, true},
		{"pinned line at rest", false, true, false, false, false, true, false},
		{"soloed line at rest", false, false, true, false, true, false, false},
		{"button held with the pointer gone", false, false, false, true, true, true, true},
		{"pinned line, remove held", false, true, false, true, true, true, true},
	} {
		solo, pin, remove := rowControls(c.hovered, c.pinned, c.soloed, c.active)
		if solo != c.wantSolo || pin != c.wantPin || remove != c.wantRemove {
			t.Errorf("%s: solo %v pin %v remove %v, want %v %v %v", c.name, solo, pin, remove, c.wantSolo, c.wantPin, c.wantRemove)
		}
	}
}

// The actions the buttons call: the pin pins an unpinned line to the switcher's projection and unpins a pinned one; the X removes the line.
func TestSidebarButtonActions(t *testing.T) {
	a := demoApp(t)
	l := a.graph.lines
	l.SetSwitcher(demodata.ProjNewJob)
	id := l.Add(demodata.NetWorth, false, "", graph.RGB(1))

	l.TogglePin(id)
	if got, _ := l.Get(id); !got.Pinned || got.Projection != demodata.ProjNewJob {
		t.Errorf("pinned line %+v", got)
	}
	l.SetSwitcher(demodata.ProjBase)
	if proj, _ := l.Effective(id); proj != demodata.ProjNewJob {
		t.Errorf("a pinned line follows the switcher: %q", proj)
	}
	l.TogglePin(id)
	if proj, _ := l.Effective(id); proj != demodata.ProjBase {
		t.Errorf("an unpinned line does not follow the switcher: %q", proj)
	}

	n := len(l.All())
	l.Remove(id)
	if _, ok := l.Get(id); ok || len(l.All()) != n-1 {
		t.Error("the line was not removed")
	}
}
