// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"
)

// shotOptions is screenshot mode: draw a tab for a number of frames, save the last as a PNG and exit.
type shotOptions struct {
	out    string
	tab    string
	frames int
	width  int
	height int
	// sel is the name of the projection, asset or account to select; empty selects nothing.
	sel string
	// open is the picker to show open, one of openNames, else empty.
	open string
	// openID is the id of that picker.
	openID string
	// checkIDs asks for the widget ID conflict scan after the screenshot is saved.
	checkIDs bool
}

// openNames are the pickers --open can show, each with the tab it needs and the id the code that draws it asks takeOpen about.
var openNames = []struct{ name, tab, id string }{
	{"add-line", "Graph", "add-line"},
	{"target", "Projections", "addsetasset"},
	{"member", "Assets", "memberacct"},
}

// openRequest is the id of the picker the screenshot asks to be shown open; the tab that draws it opens it on the first frame it does and clears the request.
var openRequest string

// openComboPopup opens the popup of the combo labelled label in the current ID scope: ImGui opens it under the name "##ComboPopup" in the scope of the combo's id.
func openComboPopup(label string) {
	imgui.PushIDStr(label)
	imgui.OpenPopupStr("##ComboPopup")
	imgui.PopID()
}

// shotScroll is the picker opened for a screenshot, whose list is scrolled to where the accounts end and the assets begin.
var shotScroll string

// scrollToAssets scrolls the list of the picker id, in the item just drawn, to centre the first asset, so that a screenshot shows both icons; seen says whether that item has gone by. It does nothing outside a screenshot.
func scrollToAssets(id string, k iconKind, seen *bool) {
	if shotScroll != id || *seen || k != iconAsset {
		return
	}
	*seen = true
	imgui.SetScrollHereYV(0.5)
}

// takeOpen reports whether id is the picker asked for, once.
func takeOpen(id string) bool {
	if openRequest != id {
		return false
	}
	openRequest, shotScroll = "", id
	return true
}

// newShotOptions checks the screenshot flags and resolves the tab name to its spelling in tabs.
func newShotOptions(out, tab string, frames int, size, sel, open string) (*shotOptions, error) {
	o := &shotOptions{out: out, frames: frames, sel: sel}
	for _, name := range tabs {
		if strings.EqualFold(name, tab) {
			o.tab = name
		}
	}
	if o.tab == "" {
		return nil, fmt.Errorf("unknown tab %q; the tabs are %s", tab, strings.Join(tabs, ", "))
	}
	if frames < 1 {
		return nil, fmt.Errorf("--frames must be at least 1")
	}
	w, h, ok := strings.Cut(strings.ToLower(size), "x")
	var err1, err2 error
	o.width, err1 = strconv.Atoi(w)
	o.height, err2 = strconv.Atoi(h)
	if !ok || err1 != nil || err2 != nil || o.width < 1 || o.height < 1 {
		return nil, fmt.Errorf("--size %q is not WxH, such as 1280x800", size)
	}
	if sel != "" && o.tab != "Projections" && o.tab != "Assets" && o.tab != "Accounts" {
		return nil, fmt.Errorf("--select works only with the Projections, Assets and Accounts tabs")
	}
	if open != "" {
		for _, n := range openNames {
			if strings.EqualFold(n.name, open) {
				o.open, o.openID = n.name, n.id
				if o.tab != n.tab {
					return nil, fmt.Errorf("--open %s works only with the %s tab", n.name, n.tab)
				}
			}
		}
		if o.open == "" {
			names := make([]string, len(openNames))
			for i, n := range openNames {
				names[i] = n.name
			}
			return nil, fmt.Errorf("unknown picker %q for --open; the pickers are %s", open, strings.Join(names, ", "))
		}
		if o.open != "add-line" && sel == "" {
			return nil, fmt.Errorf("--open %s needs --select", o.open)
		}
	}
	return o, nil
}

// selectItem selects the projection, asset or account named by --select in the chosen tab.
func (o *shotOptions) selectItem(a *app) error {
	if o.sel == "" {
		return nil
	}
	switch o.tab {
	case "Projections":
		if a.rec.Projection(o.sel) != nil {
			a.proj.sel = o.sel
			return nil
		}
	case "Assets":
		for _, as := range a.rec.Assets {
			if as.Name == o.sel && !isDefaultAsset(as.ID) {
				a.assets.sel = as.ID
				return nil
			}
		}
	case "Accounts":
		for _, ac := range a.rec.Accounts {
			if ac.Name == o.sel {
				a.accts.sel = ac.ID
				return nil
			}
		}
	}
	return fmt.Errorf("the %s tab has nothing named %q", o.tab, o.sel)
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// setDefaultVideoDriver picks SDL's offscreen driver when no display is available and the user has not chosen one.
func setDefaultVideoDriver() {
	if os.Getenv("SDL_VIDEO_DRIVER") == "" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		os.Setenv("SDL_VIDEO_DRIVER", "offscreen")
	}
}
