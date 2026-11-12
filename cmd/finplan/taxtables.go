// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
	"github.com/joeriedel/finplan/internal/tax"
)

// taxField is one number of a tax table of type T. with returns a copy of the table with the field set, so a change of one field is one whole-table edit.
type taxField[T any] struct {
	id, label string
	percent   bool
	get       func(t T, st tax.FilingStatus) float64
	with      func(t T, st tax.FilingStatus, v float64) T
}

// taxSpec is what the editor needs to know about tables of type T.
type taxSpec[T any] struct {
	fields       []taxField[T]
	brackets     func(t T, st tax.FilingStatus) []tax.Bracket
	withBrackets func(t T, st tax.FilingStatus, bs []tax.Bracket) T
	validate     func(t T) error
	save         func(st *store.Store, rec *model.Record, t T) (*ops.Result, error)
}

// fedField is a federal field that does not depend on the filing status; p points at it in the table.
func fedField(id, label string, percent bool, p func(f *tax.Federal) *float64) taxField[tax.Federal] {
	return taxField[tax.Federal]{id, label, percent,
		func(f tax.Federal, _ tax.FilingStatus) float64 { return *p(&f) },
		func(f tax.Federal, _ tax.FilingStatus, v float64) tax.Federal {
			f = f.Clone()
			*p(&f) = v
			return f
		}}
}

// fedStatusField is a federal field of the filing status; p points at it in the status's rules.
func fedStatusField(id, label string, p func(s *tax.FedStatus) *float64) taxField[tax.Federal] {
	return taxField[tax.Federal]{id, label, false,
		func(f tax.Federal, st tax.FilingStatus) float64 {
			s := f.Status[st]
			return *p(&s)
		},
		func(f tax.Federal, st tax.FilingStatus, v float64) tax.Federal {
			f = f.Clone()
			if f.Status == nil {
				f.Status = map[tax.FilingStatus]tax.FedStatus{}
			}
			s := f.Status[st]
			*p(&s) = v
			f.Status[st] = s
			return f
		}}
}

// stateField is a state field that does not depend on the filing status; p points at it in the table.
func stateField(id, label string, p func(s *tax.State) *float64) taxField[tax.State] {
	return taxField[tax.State]{id, label, false,
		func(s tax.State, _ tax.FilingStatus) float64 { return *p(&s) },
		func(s tax.State, _ tax.FilingStatus, v float64) tax.State {
			s = s.Clone()
			*p(&s) = v
			return s
		}}
}

var fedSpec = taxSpec[tax.Federal]{
	fields: []taxField[tax.Federal]{
		fedStatusField("fedstd", "Standard deduction", func(s *tax.FedStatus) *float64 { return &s.StandardDeduction }),
		fedStatusField("fedphase", "Child credit phase-out", func(s *tax.FedStatus) *float64 { return &s.ChildCreditPhaseOut }),
		fedStatusField("fedaddmed", "Add'l Medicare threshold", func(s *tax.FedStatus) *float64 { return &s.AdditionalMedicareThreshold }),
		fedStatusField("fedniit", "NIIT threshold", func(s *tax.FedStatus) *float64 { return &s.NIITThreshold }),
		fedField("fedchild", "Child credit", false, func(f *tax.Federal) *float64 { return &f.ChildCredit }),
		fedField("fedother", "Other dependent credit", false, func(f *tax.Federal) *float64 { return &f.OtherDependentCredit }),
		fedField("fedpenalty", "Early withdrawal penalty", true, func(f *tax.Federal) *float64 { return &f.PenaltyRate }),
		fedField("fedssrate", "Social Security rate", true, func(f *tax.Federal) *float64 { return &f.SocialSecurityRate }),
		fedField("fedwagebase", "Social Security wage base", false, func(f *tax.Federal) *float64 { return &f.SocialSecurityWageBase }),
		fedField("fedmedrate", "Medicare rate", true, func(f *tax.Federal) *float64 { return &f.MedicareRate }),
		fedField("fedaddmedrate", "Add'l Medicare rate", true, func(f *tax.Federal) *float64 { return &f.AdditionalMedicareRate }),
		fedField("fedniitrate", "NIIT rate", true, func(f *tax.Federal) *float64 { return &f.NIITRate }),
		fedField("fed401k", "401(k) limit", false, func(f *tax.Federal) *float64 { return &f.Limit401k }),
		fedField("fedira", "IRA limit", false, func(f *tax.Federal) *float64 { return &f.LimitIRA }),
		fedField("fedhsa", "HSA limit", false, func(f *tax.Federal) *float64 { return &f.LimitHSA }),
	},
	brackets: func(f tax.Federal, st tax.FilingStatus) []tax.Bracket { return f.Status[st].Brackets },
	withBrackets: func(f tax.Federal, st tax.FilingStatus, bs []tax.Bracket) tax.Federal {
		f = f.Clone()
		if f.Status == nil {
			f.Status = map[tax.FilingStatus]tax.FedStatus{}
		}
		s := f.Status[st]
		s.Brackets = bs
		f.Status[st] = s
		return f
	},
	validate: tax.Federal.Validate,
	save: func(st *store.Store, rec *model.Record, f tax.Federal) (*ops.Result, error) {
		return ops.SetFederalTable(st, rec, f, ops.Options{})
	},
}

var stateSpec = taxSpec[tax.State]{
	fields: []taxField[tax.State]{
		{"statestd", "Standard deduction", false,
			func(s tax.State, st tax.FilingStatus) float64 { return s.StandardDeduction[st] },
			func(s tax.State, st tax.FilingStatus, v float64) tax.State {
				s = s.Clone()
				switch {
				case v == 0:
					delete(s.StandardDeduction, st)
				case s.StandardDeduction == nil:
					s.StandardDeduction = map[tax.FilingStatus]float64{st: v}
				default:
					s.StandardDeduction[st] = v
				}
				return s
			}},
		stateField("stateexempt", "Exemption per filer", func(s *tax.State) *float64 { return &s.Exemption }),
		stateField("statedepexempt", "Exemption per dependent", func(s *tax.State) *float64 { return &s.DependentExemption }),
		stateField("statecap529", "529 deduction cap", func(s *tax.State) *float64 { return &s.Cap529 }),
	},
	brackets: func(s tax.State, _ tax.FilingStatus) []tax.Bracket { return s.Brackets },
	withBrackets: func(s tax.State, _ tax.FilingStatus, bs []tax.Bracket) tax.State {
		s = s.Clone()
		s.Brackets = bs
		return s
	},
	validate: tax.State.Validate,
	save: func(st *store.Store, rec *model.Record, s tax.State) (*ops.Result, error) {
		return ops.SetStateTable(st, rec, s, ops.Options{})
	},
}

// ---- Text and bracket helpers, which need no window.

// formatPercentText writes a rate as a percent without the sign, "5.75" for 0.0575.
func formatPercentText(rate float64) string {
	return strconv.FormatFloat(math.Round(rate*1e8)/1e6, 'f', -1, 64)
}

// parsePercentText reads a percent typed as "5.75" or "5.75%" as a rate, 0.0575.
func parsePercentText(s string) (float64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	p, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(p) || math.IsInf(p, 0) {
		return 0, fmt.Errorf("%q is not a percent", s)
	}
	return math.Round(p*1e6) / 1e8, nil
}

// formatMoneyText writes an amount for a field: plain digits, so it can be edited.
func formatMoneyText(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// parseMoneyText reads an amount typed with or without a dollar sign and thousands commas.
func parseMoneyText(s string) (float64, error) {
	t := strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%q is not an amount", strings.TrimSpace(s))
	}
	return v, nil
}

// fieldText is the text of a field's value: a percent or an amount.
func fieldText(percent bool, v float64) string {
	if percent {
		return formatPercentText(v)
	}
	return formatMoneyText(v)
}

// fieldShown is the value as shown beside the field, for the shipped value.
func fieldShown(percent bool, v float64) string {
	if percent {
		return formatPercentText(v) + "%"
	}
	return formatDollars(v)
}

// parseFieldText reads the text typed into a field.
func parseFieldText(percent bool, s string) (float64, error) {
	if percent {
		return parsePercentText(s)
	}
	return parseMoneyText(s)
}

// bracketsText lists brackets as "$0 at 2%, $3,000 at 3%".
func bracketsText(bs []tax.Bracket) string {
	parts := make([]string, len(bs))
	for i, b := range bs {
		parts[i] = formatDollars(b.Over) + " at " + formatPercentText(b.Rate) + "%"
	}
	return strings.Join(parts, ", ")
}

// bracketsWith returns a copy of bs with the threshold or rate of bracket i set.
func bracketsWith(bs []tax.Bracket, i int, over, rate float64) []tax.Bracket {
	bs = slices.Clone(bs)
	bs[i] = tax.Bracket{Over: over, Rate: rate}
	return bs
}

// bracketsWithout returns a copy of bs without bracket i.
func bracketsWithout(bs []tax.Bracket, i int) []tax.Bracket {
	return slices.Delete(slices.Clone(bs), i, i+1)
}

// bracketsAdded returns a copy of bs with the bracket added in the order of the thresholds.
func bracketsAdded(bs []tax.Bracket, b tax.Bracket) []tax.Bracket {
	bs = append(slices.Clone(bs), b)
	slices.SortStableFunc(bs, func(x, y tax.Bracket) int {
		switch {
		case x.Over < y.Over:
			return -1
		case x.Over > y.Over:
			return 1
		}
		return 0
	})
	return bs
}

// taxTableSel is the table the editor shows: the federal table or a state's, in a year, with the filing status the per-status fields are for.
type taxTableSel struct {
	federal bool
	state   string
	year    int
	status  tax.FilingStatus
}

// taxTableYears lists the years the selected table has.
func taxTableYears(rec *model.Record, s taxTableSel) []int {
	var years []int
	if s.federal {
		for _, f := range rec.Tax.Federal {
			years = append(years, f.Year)
		}
		return years
	}
	for _, t := range rec.Tax.State {
		if t.Name == s.state {
			years = append(years, t.Year)
		}
	}
	return years
}

// resolved fills in what the selection lacks or no longer has: the household's state table, the year of the Taxes view or the nearest one the table has, and the household's filing status.
func (s taxTableSel) resolved(rec *model.Record, viewYear int) taxTableSel {
	if !s.federal && !rec.Tax.HasState(s.state) {
		switch names := stateNames(rec); {
		case rec.Tax.HasState(rec.Household.State):
			s.state = rec.Household.State
		case len(names) > 0:
			s.state = names[0]
		default:
			s.federal = true
		}
	}
	if years := taxTableYears(rec, s); !slices.Contains(years, s.year) {
		s.year = 0
		for _, y := range years {
			if s.year == 0 || abs(y-viewYear) < abs(s.year-viewYear) {
				s.year = y
			}
		}
	}
	if !s.status.Valid() {
		s.status = rec.Household.Filing
		if !s.status.Valid() {
			s.status = tax.MarriedJoint
		}
	}
	return s
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---- The editor.

// taxTablesUI is the tax tables editor of the Taxes view.
type taxTablesUI struct {
	fieldEditor
	// rec is the record the typed texts belong to; they are dropped when it changes.
	rec  *model.Record
	show bool
	sel  taxTableSel
	// shipped are the tables the program ships, which Revert restores.
	shipped   tax.Tables
	haveShip  bool
	addOver   string
	addRate   string
	newName   string
	newFrom   string
	copyCheck problemCache
}

// shippedTables returns the shipped tables, read on first use.
func (u *taxTablesUI) shippedTables() tax.Tables {
	if !u.haveShip {
		u.shipped, u.haveShip = tax.Defaults(), true
	}
	return u.shipped
}

// taxTables draws the tax tables editor, when it is shown.
func (t *budgetTab) taxTables() {
	a, u := t.a, &t.tables
	if !u.show {
		return
	}
	if u.rec != a.rec {
		u.rec = a.rec
		u.bufs, u.errs = map[string]string{}, map[string]string{}
	}
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.SeparatorText("Tax tables")
	wrappedText("The brackets, deductions and limits the tax is computed from. A value that differs from the one the program ships shows it, with Revert.")

	u.sel = u.sel.resolved(a.rec, t.tax.year)
	sel := u.sel
	names := stateNames(a.rec)
	labels := append([]string{"Federal"}, names...)
	cur, preview := 0, "Federal"
	if !sel.federal {
		cur, preview = 1+slices.Index(names, sel.state), sel.state
	}
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Table")
	imgui.SameLine()
	if k := pick("taxtbltable", preview, em*12, labels, cur); k >= 0 {
		u.sel.federal, u.sel.state = k == 0, ""
		if k > 0 {
			u.sel.state = names[k-1]
		}
		u.sel = u.sel.resolved(a.rec, t.tax.year)
		sel = u.sel
	}
	imgui.SameLine()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Year")
	imgui.SameLine()
	years := taxTableYears(a.rec, sel)
	yearLabels := make([]string, len(years))
	for i, y := range years {
		yearLabels[i] = strconv.Itoa(y)
	}
	if k := pick("taxtblyear", strconv.Itoa(sel.year), em*8, yearLabels, slices.Index(years, sel.year)); k >= 0 {
		u.sel.year = years[k]
		sel = u.sel
	}
	imgui.SameLine()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Filing status")
	imgui.SameLine()
	if k := pick("taxtblstatus", filingName(sel.status), em*16, filingLabels, slices.Index(filingStatuses, string(sel.status))); k >= 0 {
		u.sel.status = tax.FilingStatus(filingStatuses[k])
		sel = u.sel
	}
	tooltipOn("The filing status picks the brackets and the per-status fields shown")

	key := fmt.Sprintf("%t/%s/%d/%s/", sel.federal, sel.state, sel.year, sel.status)
	if sel.federal {
		if f, ok := a.rec.Tax.FederalYear(sel.year); ok {
			shipped, has := u.shippedTables().FederalYear(sel.year)
			tableEditor(u, a, fedSpec, key, f, &shipped, has, sel.status, func() (*ops.Result, error) {
				return ops.RevertFederalTable(a.st, a.rec, sel.year, ops.Options{})
			})
		}
	} else if s, ok := a.rec.Tax.StateYear(sel.state, sel.year); ok {
		shipped, has := u.shippedTables().StateYear(sel.state, sel.year)
		if sel.state != a.rec.Household.State {
			imgui.SameLine()
			if imgui.Button("Use for household##taxtbluse") {
				a.edit("change household", func() (*ops.Result, error) {
					return ops.SetHousehold(a.st, a.rec, ops.HouseholdChange{State: model.Ptr(sel.state)}, ops.Options{})
				})
			}
			tooltipOn("Tax the household by this state's table")
		}
		tableEditor(u, a, stateSpec, key, s, &shipped, has, sel.status, func() (*ops.Result, error) {
			return ops.RevertStateTable(a.st, a.rec, sel.state, sel.year, ops.Options{})
		})
	} else {
		disabledText("There is no table for this year.")
	}
	u.newState(a, names)
}

// tableEditor draws the fields and brackets of the table t, with the shipped table to compare to when has.
func tableEditor[T any](u *taxTablesUI, a *app, spec taxSpec[T], key string, t T, shipped *T, has bool, st tax.FilingStatus, revert func() (*ops.Result, error)) {
	if has && !reflect.DeepEqual(t, *shipped) {
		imgui.SameLine()
		if imgui.Button("Revert table##taxtblrevert") {
			a.edit("revert tax table", revert)
		}
		tooltipOn("Restore every value of this table to the one the program ships")
	}
	imgui.Spacing()
	for _, f := range spec.fields {
		drawTaxField(u, a, spec, key, t, shipped, has, st, f)
	}
	drawBrackets(u, a, spec, key, t, shipped, has, st)
}

// saveTable makes the edit that replaces the table, showing a refusal beside the field with the id.
func saveTable[T any](u *taxTablesUI, a *app, spec taxSpec[T], id string, next T) {
	if err := spec.validate(next); err != nil {
		u.errs[id] = err.Error()
		return
	}
	if !a.edit("edit tax table", func() (*ops.Result, error) { return spec.save(a.st, a.rec, next) }) && a.unchanged {
		delete(u.bufs, id)
		delete(u.errs, id)
	}
}

// drawTaxField draws one number field, its shipped value and Revert when they differ.
func drawTaxField[T any](u *taxTablesUI, a *app, spec taxSpec[T], key string, t T, shipped *T, has bool, st tax.FilingStatus, f taxField[T]) {
	em := imgui.FontSize()
	id := key + f.id
	imgui.PushIDStr(id)
	defer imgui.PopID()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(f.label)
	imgui.SameLineV(em*16, -1)
	v := f.get(t, st)
	if s, done := u.field(id, "", fieldText(f.percent, v), em*10); done {
		if nv, err := parseFieldText(f.percent, s); err != nil {
			u.errs[id] = err.Error()
		} else {
			saveTable(u, a, spec, id, f.with(t, st, nv))
		}
	}
	if f.percent {
		imgui.SameLine()
		imgui.TextUnformatted("%")
	}
	if !has {
		return
	}
	if sv := f.get(*shipped, st); sv != v {
		imgui.SameLine()
		disabledText("shipped " + fieldShown(f.percent, sv))
		imgui.SameLine()
		if imgui.Button("Revert##field") {
			saveTable(u, a, spec, id, f.with(t, st, sv))
		}
	}
}

// drawBrackets draws the brackets of the filing status as rows, the shipped list and Revert when it differs, and the Add row.
func drawBrackets[T any](u *taxTablesUI, a *app, spec taxSpec[T], key string, t T, shipped *T, has bool, st tax.FilingStatus) {
	em := imgui.FontSize()
	bs := spec.brackets(t, st)
	imgui.Spacing()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Brackets: each rate applies to the income over its threshold")
	if has {
		if sbs := spec.brackets(*shipped, st); !slices.Equal(bs, sbs) {
			imgui.PushIDStr(key + "bracketsrevert")
			wrappedText("shipped " + bracketsText(sbs))
			if imgui.Button("Revert##brackets") {
				saveTable(u, a, spec, key+"brackets", spec.withBrackets(t, st, slices.Clone(sbs)))
			}
			imgui.PopID()
		}
	}
	for i, b := range bs {
		n := strconv.Itoa(i)
		oid, rid := key+"over"+n, key+"rate"+n
		imgui.PushIDStr(key + "bracket" + n)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted("Over")
		imgui.SameLine()
		x := imgui.CursorPosX()
		if i == 0 {
			imgui.TextUnformatted("0")
			imgui.SameLineV(x+em*10, 0)
			imgui.Dummy(imgui.NewVec2(0, 0))
		} else if s, done := u.field(oid, "", formatMoneyText(b.Over), em*10); done {
			if v, err := parseMoneyText(s); err != nil {
				u.errs[oid] = err.Error()
			} else {
				saveTable(u, a, spec, oid, spec.withBrackets(t, st, bracketsWith(bs, i, v, b.Rate)))
			}
		}
		imgui.SameLine()
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted("Rate")
		imgui.SameLine()
		if s, done := u.field(rid, "", formatPercentText(b.Rate), em*8); done {
			if v, err := parsePercentText(s); err != nil {
				u.errs[rid] = err.Error()
			} else {
				saveTable(u, a, spec, rid, spec.withBrackets(t, st, bracketsWith(bs, i, b.Over, v)))
			}
		}
		imgui.SameLine()
		imgui.TextUnformatted("%")
		if i > 0 {
			imgui.SameLine()
			if imgui.Button("x##bracketremove") {
				saveTable(u, a, spec, oid, spec.withBrackets(t, st, bracketsWithout(bs, i)))
			}
		}
		imgui.PopID()
	}

	imgui.AlignTextToFramePadding()
	disabledText("add")
	imgui.SameLine()
	s := u.addOver
	imgui.SetNextItemWidth(em * 10)
	if imgui.InputTextWithHint("##taxtbladdover", "over, e.g. 50000", &s, 0, nil) {
		u.addOver = s
	}
	imgui.SameLine()
	s = u.addRate
	imgui.SetNextItemWidth(em * 8)
	if imgui.InputTextWithHint("##taxtbladdrate", "percent", &s, 0, nil) {
		u.addRate = s
	}
	next, problem := addedBracket(spec, t, st, u.addOver, u.addRate)
	formButton("Add##taxtbladd", problem, allBlank(u.addOver, u.addRate), func() {
		if a.edit("add tax bracket", func() (*ops.Result, error) { return spec.save(a.st, a.rec, next) }) {
			u.addOver, u.addRate = "", ""
		}
	})
}

// addedBracket returns the table with the bracket the texts describe added to the filing status's brackets, or the reason it cannot be.
func addedBracket[T any](spec taxSpec[T], t T, st tax.FilingStatus, over, rate string) (T, string) {
	o, err := parseMoneyText(over)
	if err != nil {
		return t, err.Error()
	}
	r, err := parsePercentText(rate)
	if err != nil {
		return t, err.Error()
	}
	next := spec.withBrackets(t, st, bracketsAdded(spec.brackets(t, st), tax.Bracket{Over: o, Rate: r}))
	if err := spec.validate(next); err != nil {
		return t, err.Error()
	}
	return next, ""
}

// newState draws the form that adds a state table as a copy of an existing one.
func (u *taxTablesUI) newState(a *app, names []string) {
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("New state table")
	imgui.SameLine()
	s := u.newName
	imgui.SetNextItemWidth(em * 10)
	if imgui.InputTextWithHint("##taxtblnewname", "name", &s, 0, nil) {
		u.newName = s
	}
	imgui.SameLine()
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("copy of")
	imgui.SameLine()
	if !slices.Contains(names, u.newFrom) {
		u.newFrom = ""
		if slices.Contains(names, a.rec.Household.State) {
			u.newFrom = a.rec.Household.State
		} else if len(names) > 0 {
			u.newFrom = names[0]
		}
	}
	if k := pick("taxtblnewfrom", u.newFrom, em*12, names, slices.Index(names, u.newFrom)); k >= 0 {
		u.newFrom = names[k]
	}
	name, from := strings.TrimSpace(u.newName), u.newFrom
	problem := u.copyCheck.get(problemKey(a.rec, "", [2]string{from, u.newName}), func() string {
		if name == "" {
			return "enter a name"
		}
		_, err := ops.CopyStateTables(a.st, a.rec, from, u.newName, ops.Options{DryRun: true})
		return reasonOf(err)
	})
	formButton("Create##taxtblnew", problem, allBlank(u.newName), func() {
		if a.edit("add state tax table", func() (*ops.Result, error) {
			return ops.CopyStateTables(a.st, a.rec, from, u.newName, ops.Options{})
		}) {
			u.sel = taxTableSel{state: u.newName, status: u.sel.status}
			u.newName = ""
		}
	})
	disabledText("Copies every year of the table. Edit it above, then pick it as the household's state.")
}
