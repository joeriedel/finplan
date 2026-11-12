// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
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
	// open is the state to show, one of openNames, else empty.
	open string
	// openID is the id of the picker that state is, if it is one.
	openID string
	// checkIDs asks for the widget ID conflict scan after the screenshot is saved.
	checkIDs bool
}

// openState is a state --open can show, with the tab it needs, whether it needs --select, and the id the code that draws it asks takeOpen about, which is empty for a state set by applyOpen instead of a picker.
type openState struct {
	name, tab, id string
	needsSel      bool
}

// openNames are the states --open can show.
var openNames = []openState{
	{"add-line", "Graph", "add-line", false},
	{"about", "Graph", "", false},
	{"target", "Projections", "addsetasset", true},
	{"member", "Assets", "memberacct", true},
	{"member-form", "Assets", "", true},
	{"new-item", "Projections", "", true},
	{"focal-grant", "Projections", "", true},
	{"budget-item", "Budget", "", false},
	{"budget-delete", "Budget", "", false},
	{"timeline", "Projections", "", true},
	{"asset-timeline", "Assets", "", true},
	{"ledger", "Accounts", "", true},
	{"equity-grants", "Accounts", "", true},
	{"equity-add-grant", "Accounts", "", true},
	{"equity-schedule", "Accounts", "", true},
	{"equity-vest", "Accounts", "", true},
	{"equity-tie", "Accounts", "", true},
	{"equity-prices", "Accounts", "", true},
	{"equity-calendar", "Accounts", "", true},
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
	if sel != "" && o.tab != "Projections" && o.tab != "Assets" && o.tab != "Accounts" && o.tab != "Graph" {
		return nil, fmt.Errorf("--select works only with the Projections, Assets, Accounts and Graph tabs")
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
			return nil, fmt.Errorf("unknown state %q for --open; the states are %s", open, strings.Join(names, ", "))
		}
		if o.openName().needsSel && sel == "" {
			return nil, fmt.Errorf("--open %s needs --select", o.open)
		}
	}
	return o, nil
}

// openName returns the openNames entry of the open state asked for.
func (o *shotOptions) openName() (n openState) {
	for _, n = range openNames {
		if n.name == o.open {
			return n
		}
	}
	return n
}

// applyOpen puts the app in the state --open asks for that is not a picker: the About dialog (about), the Assets Add member form (member also opens its account picker; member-form leaves it closed), the Projections new budget item form, the Projections Add form on the focal grant kind (focal-grant), the first Budget item's editor (budget-item) or its delete confirmation (budget-delete), the strip editor of the selected projection's first modification with a schedule (timeline), or of the selected asset's growth (asset-timeline), which must have growth, the selected account's ledger panel (ledger), or one of the equity states, which scroll the selected equity account's detail to a section: its grants table (equity-grants), the Add grant form filled in by dollar value (equity-add-grant), the schedule of its first grant with recorded and planned vests (equity-schedule), the Record vest form of that grant's first planned vest with a changed federal rate (equity-vest) or tied to a deposit (equity-tie, with the picker closed), the Prices section (equity-prices) and the vest calendar editor (equity-calendar).
func (o *shotOptions) applyOpen(a *app) error {
	switch o.open {
	case "member", "member-form":
		a.assets.add.kind = memberAccount
	case "about":
		a.openAbout()
	case "ledger":
		a.accts.ledger = true
	case "equity-grants", "equity-add-grant", "equity-schedule", "equity-vest", "equity-tie", "equity-prices", "equity-calendar":
		return o.openEquity(a)
	case "new-item":
		a.proj.addNew = true
	case "focal-grant":
		a.proj.addKind = kindFocalGrant
	case "budget-item":
		for _, g := range budgetGroups(a.rec) {
			if len(g.Rows) > 0 {
				a.budget.open[g.Rows[0].ID] = true
				return nil
			}
		}
		return fmt.Errorf("the budget has no items to open")
	case "budget-delete":
		for _, g := range budgetGroups(a.rec) {
			if len(g.Rows) > 0 {
				a.budget.askDelete(g.Rows[0].ID)
				return nil
			}
		}
		return fmt.Errorf("the budget has no items to delete")
	case "timeline":
		p := a.rec.Projection(a.proj.sel)
		for i, m := range p.Mods {
			if len(m.Values.Segments()) > 0 {
				a.proj.openTimeline(p.Name, i, m)
				a.proj.tl.sel = 0
				return nil
			}
		}
		return fmt.Errorf("projection %q has no modification with a schedule", p.Name)
	case "asset-timeline":
		id := a.assets.sel
		if k, _ := growthKind(a.rec.Asset(id)); k == growthNone {
			return fmt.Errorf("asset %q has no growth to edit", a.rec.Asset(id).Name)
		}
		a.assets.tl = timelineState{open: true, generation: a.generation, sel: 0}
		a.assets.tlAsset = id
	}
	return nil
}

// openEquity puts the selected equity account's sections in the state the equity open state asks for.
func (o *shotOptions) openEquity(a *app) error {
	acct := a.rec.Account(a.accts.sel)
	if acct.Type != model.TypeEquity {
		return fmt.Errorf("account %q is not an equity account", acct.Name)
	}
	u := &a.accts.eq
	*u = equityUI{acct: acct.ID, grant: newGrantForm(acct), grantInit: true}
	switch o.open {
	case "equity-grants":
		u.scroll = "grants"
	case "equity-prices":
		u.scroll = "prices"
	case "equity-calendar":
		u.scroll = "calendars"
	case "equity-add-grant":
		f := &u.grant
		f.amount, f.price, f.date, f.years = "75000", "210", a.rec.Today.String(), "25/25/25/25"
		choices := firstVestChoices(a.rec, f.calendar, f.date)
		if len(choices) == 0 {
			return fmt.Errorf("the account's grants name no vest calendar with dates")
		}
		f.first = choices[0]
		u.scroll = "addgrant"
	case "equity-schedule", "equity-vest", "equity-tie":
		for _, g := range acct.Grants {
			_, rows, err := ops.GrantSchedule(a.rec, g.ID)
			if err != nil {
				return err
			}
			i := slices.IndexFunc(rows, func(r ops.ScheduleRow) bool { return r.Vest == nil })
			if i <= 0 {
				continue
			}
			u.sel, u.scroll = g.ID, "schedule"
			if o.open != "equity-schedule" {
				u.vest, u.scroll = newVestForm(a.rec, &g, rows[i]), "vest"
				u.vest.fed = "24"
			}
			if o.open == "equity-tie" {
				return o.tieDeposit(a, &g)
			}
			return nil
		}
		return fmt.Errorf("account %q has no grant with both recorded and planned vests", acct.Name)
	}
	return nil
}

// tieDeposit ties the open vest form to the newest deposit of the grant's brokerage account, dated on or after the grant, that no vest is tied to and that the vest can be recorded on, dating the vest to it.
func (o *shotOptions) tieDeposit(a *app, g *model.Grant) error {
	es, err := ops.ListEntries(a.st, g.Brokerage, false)
	if err != nil {
		return err
	}
	for _, e := range es {
		if e.Kind != store.KindTransaction || e.Amount <= 0 || e.Date < g.Date {
			continue
		}
		deps, err := unmatchedDeposits(a.st, a.rec, g.Brokerage, e.Date)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(deps, func(d store.Entry) bool { return d.ID == e.ID }) {
			continue
		}
		f := *a.accts.eq.vest
		f.date, f.tie, f.tx = e.Date.String(), true, e.ID
		if vestProblem(a.st, a.rec, f) == "" {
			*a.accts.eq.vest = f
			return nil
		}
	}
	return fmt.Errorf("the brokerage account has no deposit a vest can be tied to")
}

// showEquityOnGraph shows the projection on the Graph the way the Projections tab's Pin to Graph does, after drawing the equity accounts and the brokerage accounts their grants deposit into too, so that vests show as a step down of one line and a step up of the other.
func (a *app) showEquityOnGraph(proj string) {
	for _, acct := range a.rec.Accounts {
		if acct.Type != model.TypeEquity {
			continue
		}
		ids := []model.AssetID{model.DefaultAssetID(acct.ID)}
		for _, g := range acct.Grants {
			ids = append(ids, model.DefaultAssetID(g.Brokerage))
		}
		for _, id := range ids {
			if !slices.ContainsFunc(a.graph.lines.All(), func(ln graph.Line) bool { return !ln.Pinned && ln.Asset == id }) {
				a.graph.lines.Add(id, false, "", graph.AssetColor(a.rec, id))
			}
		}
	}
	a.proj.showOnGraph(proj)
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
	case "Graph":
		if a.rec.Projection(o.sel) != nil {
			a.showEquityOnGraph(o.sel)
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
