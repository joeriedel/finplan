// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
)

// realityLabel is the projection picker's entry for the record as it is, without a projection.
const realityLabel = "Reality"

// taxRow is one line of the Taxes view. A row with an empty Value is a heading; Note is shown after the value.
type taxRow struct {
	Label, Value, Note string
}

// taxView is the Taxes view's answer for one projection and year: the rows to show, or the reason the engine could not compute the year.
type taxView struct {
	Rows []taxRow
	// Notes are the engine's diagnostics about the year.
	Notes []string
	Err   string
}

type taxKey struct {
	projection string
	year       int
}

// taxState is the Taxes view's selection and its memo of engine answers, which are dropped whenever the record is replaced (every edit, undo and reload installs a new one).
type taxState struct {
	rec        *model.Record
	projection string
	year       int
	// chosen is whether the projection has been picked or defaulted yet; "" is a valid choice, reality.
	chosen bool
	views  map[taxKey]taxView
}

// defaultTaxProjection is the first projection of the record, else reality.
func defaultTaxProjection(rec *model.Record) string {
	if len(rec.Projections) > 0 {
		return rec.Projections[0].Name
	}
	return ""
}

// defaultTaxYear is the calendar year of the record's today.
func defaultTaxYear(rec *model.Record) int {
	y, _, _ := rec.Today.YMD()
	return y
}

// taxViewOf computes the rows of the Taxes view for the projection ("" for reality) and the calendar year.
func taxViewOf(rec *model.Record, projection string, year int) taxView {
	ty, err := engine.TaxYearOf(rec, projection, year)
	if err != nil {
		return taxView{Err: err.Error()}
	}
	v := taxView{Rows: taxRows(rec, ty)}
	v.Notes = append(v.Notes, ty.Notes...)
	for _, s := range ty.Skipped {
		v.Notes = append(v.Notes, skippedSentence(s))
	}
	return v
}

// skippedSentence describes a modification the engine did not apply.
func skippedSentence(s engine.Skipped) string {
	return fmt.Sprintf("Modification %d of %s was skipped: %s %s %s: %s", s.Index+1, s.Projection, s.Target.Kind, s.Target.ID, s.Property, s.Reason)
}

// showHousehold selects the Accounts tab and scrolls its household editor into view.
func (a *app) showHousehold() {
	a.forceTab = "Accounts"
	a.accts.scrollHousehold = true
}

// renameProjection makes the selection follow its projection to a new name.
func (s *taxState) renameProjection(from, to string) {
	if s.chosen && s.projection == from {
		s.projection = to
	}
}

// taxRows lists the year's figures in the order the CLI's tax year command reports them. Pre-tax deductions are what the tax rules deducted after the contribution limits, so they can be less than the contributions.
func taxRows(rec *model.Record, ty *engine.TaxYear) []taxRow {
	r := ty.Result
	income := ty.Wages + ty.VestWages + ty.Ordinary + ty.EarlyWithdrawal
	deducted := income - r.AGI
	state := "State tax"
	if ty.Input.State != "" {
		state = "State tax (" + ty.Input.State + ")"
	}
	rows := []taxRow{
		{Label: "Income"},
		{"Wages", formatDollars(ty.Wages), ""},
		{"RSU vests", formatDollars(ty.VestWages), ""},
		{"Other income", formatDollars(ty.Ordinary), "interest " + formatDollars(ty.Interest)},
	}
	if ty.EarlyWithdrawal != 0 {
		rows = append(rows, taxRow{"Early withdrawals", formatDollars(ty.EarlyWithdrawal), ""})
	}
	rows = append(rows,
		taxRow{"Pre-tax deductions", formatDollars(deducted), "401k " + formatDollars(ty.K401) + ", IRA " + formatDollars(ty.IRA) + ", HSA " + formatDollars(ty.HSA) + " contributed"},
		taxRow{"Adjusted gross income", formatDollars(r.AGI), ""},
		taxRow{Label: "Tax"},
		taxRow{"Federal taxable income", formatDollars(r.FederalTaxable), ""},
		taxRow{"Federal tax", formatDollars(r.FederalTax), "before credits"},
		taxRow{"Credits", formatDollars(r.Credits), ""},
		taxRow{state + " taxable income", formatDollars(r.StateTaxable), ""},
		taxRow{state, formatDollars(r.StateTax), ""},
		taxRow{"FICA", formatDollars(r.SocialSecurity + r.Medicare + r.AdditionalMedicare),
			"Social Security " + formatDollars(r.SocialSecurity) + ", Medicare " + formatDollars(r.Medicare+r.AdditionalMedicare)},
	)
	if r.NIIT != 0 {
		rows = append(rows, taxRow{"Net investment income tax", formatDollars(r.NIIT), ""})
	}
	if r.Penalty != 0 {
		rows = append(rows, taxRow{"Early withdrawal penalty", formatDollars(r.Penalty), ""})
	}
	settle := "owed"
	if ty.Due < 0 {
		settle = "refunded"
	}
	note := settle + " on " + ty.ReconciliationDate.String()
	if ty.ReconciliationAccount != "" {
		note += " in " + accountName(rec, ty.ReconciliationAccount)
	}
	return append(rows,
		taxRow{"Total tax", formatDollars(r.Total), ""},
		taxRow{Label: "Settlement"},
		taxRow{"Withheld", formatDollars(ty.Withheld()), "paychecks " + formatDollars(ty.WithheldPaychecks) + ", vests " + formatDollars(ty.WithheldVests)},
		taxRow{"Owed or refunded", formatDollars(ty.Due), note},
	)
}

// view returns the memoized Taxes view of the selected projection and year, computing it on first use.
func (s *taxState) view(rec *model.Record) taxView {
	if s.rec != rec {
		s.rec, s.views = rec, map[taxKey]taxView{}
		if s.projection != "" && rec.Projection(s.projection) == nil {
			s.chosen = false
		}
	}
	if !s.chosen {
		s.projection, s.chosen = defaultTaxProjection(rec), true
	}
	if s.year == 0 {
		s.year = defaultTaxYear(rec)
	}
	k := taxKey{s.projection, s.year}
	v, ok := s.views[k]
	if !ok {
		v = taxViewOf(rec, s.projection, s.year)
		s.views[k] = v
	}
	return v
}

// taxes draws the Taxes view.
func (t *budgetTab) taxes() {
	a, s := t.a, &t.tax
	v := s.view(a.rec)
	em := imgui.FontSize()

	labels := []string{realityLabel}
	sel := 0
	for _, p := range a.rec.Projections {
		if p.Name == s.projection {
			sel = len(labels)
		}
		labels = append(labels, p.Name)
	}
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Projection")
	imgui.SameLine()
	preview := labels[sel]
	if k := pick("taxproj", preview, em*16, labels, sel); k >= 0 {
		s.projection = ""
		if k > 0 {
			s.projection = labels[k]
		}
	}
	imgui.SameLine()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Year")
	imgui.SameLine()
	if imgui.Button("-##taxyear") {
		s.year--
	}
	imgui.SameLine()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(strconv.Itoa(s.year))
	imgui.SameLine()
	if imgui.Button("+##taxyear") {
		s.year++
	}
	imgui.SameLine()
	if imgui.Button("This year##taxyear") {
		s.year = defaultTaxYear(a.rec)
	}
	imgui.SameLine()
	if imgui.Button("Household...##taxhousehold") {
		a.showHousehold()
	}
	tooltipOn("Open the household editor on the Accounts tab")

	if v.Err != "" {
		imgui.Spacing()
		wrappedText("The tax for " + strconv.Itoa(s.year) + " cannot be computed: " + v.Err)
		return
	}
	imgui.Spacing()
	if imgui.BeginTableV("##taxrows", 3, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		imgui.TableSetupColumnV("", 0, 3, 0)
		imgui.TableSetupColumnV("", 0, 2, 0)
		imgui.TableSetupColumnV("", 0, 6, 0)
		for _, r := range v.Rows {
			if r.Value == "" {
				groupHeaderRow(r.Label)
				continue
			}
			imgui.TableNextRowV(0, 0)
			imgui.TableSetColumnIndex(0)
			imgui.TextUnformatted(r.Label)
			imgui.TableSetColumnIndex(1)
			imgui.TextUnformatted(r.Value)
			imgui.TableSetColumnIndex(2)
			disabledText(r.Note)
		}
		imgui.EndTable()
	}
	for _, n := range v.Notes {
		wrappedText(n)
	}
}
