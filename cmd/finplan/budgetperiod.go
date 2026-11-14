// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"slices"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// periodColumns are the columns of the This period table, with the share of its width each takes, sized to read at 1280 pixels wide. How far over or under budget a period is shows in the Standing column and in Recent periods, not in a column of its own.
var periodColumns = []struct {
	header string
	weight float32
}{{"Item", 1.8}, {"Period", 2.2}, {"Timing", 0.8}, {"Budgeted", 2.4}, {"Expected", 1.1}, {"Posted", 1.1}, {"Status", 1.1}, {"Remaining", 1}, {"Standing", 2.6}, {"Recent periods", 2}, {"", 1.6}}

// periodsShown is how many periods of each item the This period view keeps variance for: the current one and the five before it.
const periodsShown = 6

// periodState is the This period view's memo of the engine's answer, which is dropped whenever the record is replaced (every edit, undo and reload installs a new one) or the date shown changes, and the status picked and the amounts typed for each item, keyed by item id.
type periodState struct {
	rec  *model.Record
	date model.Date
	rows []engine.ItemPeriods
	// standing is each line's status on the date, by item id, and unaccounted the month's unaccounted money.
	standing    map[string]engine.LineStatus
	unaccounted []model.UnaccountedMonth
	err         string
	status      map[string]model.OccurrenceStatus
	posted      map[string]string
	expect      map[string]string
	// at is the date typed in the date field, nil while the view follows the Graph's cursor; dateText is the field and dateErr what is wrong with it.
	at       *model.Date
	dateText string
	dateErr  string
	// sel is the line whose transactions are listed: an item id, or "unaccounted:" and an account id.
	sel string
}

// shownDate is the date the view shows: the typed one, else the Graph cursor's.
func (s *periodState) shownDate(a *app) model.Date {
	if s.at != nil {
		return *s.at
	}
	return a.graph.cur.effective()
}

var periodStatusNames = map[model.OccurrenceStatus]string{model.StatusAuto: "Auto", model.StatusPosted: "Posted", model.StatusPending: "Pending"}

// load computes the rows and the standing of each line on the date, and starts the fields of each row from what is recorded.
func (s *periodState) load(rec *model.Record, date model.Date) {
	s.rec, s.date, s.rows, s.standing, s.unaccounted, s.err = rec, date, nil, nil, nil, ""
	s.status, s.posted, s.expect = map[string]model.OccurrenceStatus{}, map[string]string{}, map[string]string{}
	rows, err := engine.Periods(rec, "", date, periodsShown)
	if err != nil {
		s.err = err.Error()
		return
	}
	view, err := engine.StatusAsOf(rec, date)
	if err != nil {
		s.err = err.Error()
		return
	}
	s.rows, s.unaccounted, s.standing = rows, view.Unaccounted, map[string]engine.LineStatus{}
	for _, l := range view.Lines {
		s.standing[l.Item.ID] = l
	}
	for _, r := range rows {
		v := r.Current()
		s.status[r.Item.ID] = v.Status
		if v.Override.Posted != nil {
			s.posted[r.Item.ID] = trimFloat(*v.Override.Posted)
		}
		if v.Override.Expected != nil {
			s.expect[r.Item.ID] = trimFloat(*v.Override.Expected)
		}
	}
}

// parseAmount reads a typed amount; blank is not set.
func parseAmount(text string) (v *float64, err error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	x, err := parseValue(model.PropAmount, model.ModeSet, text)
	if err != nil {
		return nil, err
	}
	if x < 0 {
		return nil, errors.New("an amount cannot be negative")
	}
	return &x, nil
}

// periodChange is what the typed and picked values change in the item's current period, and why they cannot be set, if so.
func (s *periodState) periodChange(r engine.ItemPeriods) (ops.OccurrenceChange, string) {
	id, v := r.Item.ID, r.Current()
	var ch ops.OccurrenceChange
	if st := s.status[id]; st != v.Status {
		ch.Status = &st
	}
	for _, f := range []struct {
		name string
		text string
		cur  *float64
		out  **float64
	}{{"posted", s.posted[id], v.Override.Posted, &ch.Posted}, {"expected", s.expect[id], v.Override.Expected, &ch.Expected}} {
		x, err := parseAmount(f.text)
		if err != nil {
			return ch, f.name + ": " + err.Error()
		}
		if x != nil && (f.cur == nil || *f.cur != *x) {
			*f.out = x
		}
	}
	if ch == (ops.OccurrenceChange{}) {
		return ch, "nothing is changed"
	}
	return ch, ""
}

// signedDollars is a variance as a signed amount of dollars.
func signedDollars(v float64) string {
	s := formatDollars(v)
	if v > 0 && s != "0" {
		return "+" + s
	}
	return s
}

// overUnder says how far the period is over or under its budget.
func overUnder(v float64) string {
	s := formatDollars(max(v, -v))
	switch {
	case s == "0":
		return "on budget"
	case v > 0:
		return s + " over"
	}
	return s + " under"
}

// setPeriod records the values the user typed for the item's current period through ops.SetOccurrence. Occurrences are facts, which are never undone, only corrected or cleared.
func (t *budgetTab) setPeriod(r engine.ItemPeriods, ch ops.OccurrenceChange) bool {
	start := r.Current().Period.Start
	return t.a.addEntry("set budget period", func() (*ops.Result, error) {
		return ops.SetOccurrence(t.a.st, t.a.rec, r.Item.ID, start, t.a.rec.Today, ch, "", ops.Options{})
	})
}

// clearPeriod voids what was recorded for the item's current period through ops.ClearOccurrence.
func (t *budgetTab) clearPeriod(r engine.ItemPeriods) bool {
	start := r.Current().Period.Start
	return t.a.addEntry("clear budget period", func() (*ops.Result, error) {
		return ops.ClearOccurrence(t.a.st, t.a.rec, r.Item.ID, start, t.a.rec.Today, "", ops.Options{})
	})
}

// standingText says in words where the line stands on the date.
func standingText(l engine.LineStatus) string {
	switch l.State {
	case engine.StateMatched:
		return "matched " + formatDollars(l.Amount) + ", " + overUnder(l.Variance)
	case engine.StateEstimated:
		return "no transactions; assumed " + formatDollars(l.View.Posted)
	case engine.StateOnPace, engine.StateHot, engine.StateLight:
		return string(l.State) + ": " + formatDollars(l.Amount) + " of " + formatDollars(l.Amount-l.Variance) + " due so far"
	}
	return string(l.State)
}

// standingColor is the color of the standing: missing in red, not yet due in grey, the rest in the text color.
func standingColor(l engine.LineStatus) (uint32, bool) {
	switch l.State {
	case engine.StateMissing:
		return colError, true
	case engine.StateNotYetDue, engine.StateEstimated:
		return colMuted, true
	}
	return 0, false
}

// periodDate draws the date field: the date shown follows the Graph's cursor until one is typed, and Follow cursor returns to it.
func (t *budgetTab) periodDate() {
	s := &t.period
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted("Date")
	imgui.SameLine()
	imgui.SetNextItemWidth(imgui.FontSize() * 8)
	imgui.InputTextWithHint("##perioddate", s.shownDate(t.a).String(), &s.dateText, 0, nil)
	tooltipOn("A date to show the periods on, written " + dateHint + "; until one is set, the view follows the Graph's cursor")
	imgui.SameLine()
	if imgui.Button("Show##perioddate") {
		d, err := parseDate(strings.TrimSpace(s.dateText))
		if err != nil {
			s.dateErr = err.Error()
		} else {
			s.at, s.dateErr = &d, ""
		}
	}
	imgui.SameLine()
	imgui.BeginDisabledV(s.at == nil)
	if imgui.Button("Follow cursor##perioddate") {
		s.at, s.dateText, s.dateErr = nil, "", ""
	}
	imgui.EndDisabled()
	if s.dateErr != "" {
		imgui.SameLine()
		errorText(s.dateErr)
	}
}

// standingCell draws the standing of the line, which selects the line to list its transactions when clicked.
func (s *periodState) standingCell(l engine.LineStatus) {
	if c, ok := standingColor(l); ok {
		imgui.PushStyleColorVec4(imgui.ColText, rgba(c, 1))
		defer imgui.PopStyleColor()
	}
	text := standingText(l)
	clipped := textClipped(imgui.CalcTextSize(text).X, imgui.ContentRegionAvail().X)
	if imgui.SelectableBoolV(text+"##standing", s.sel == l.Item.ID, 0, imgui.NewVec2(0, 0)) {
		s.sel = toggled(s.sel, l.Item.ID)
	}
	tooltipOn(fullTextTip(text, clipped, "List the transactions matched to this line in this period"))
}

// fullTextTip is the tooltip of a cell: its hint, headed by the whole text when the cell clips it.
func fullTextTip(text string, clipped bool, hint string) string {
	if clipped {
		return text + "\n\n" + hint
	}
	return hint
}

// budgetedText is the budgeted amount of the period, and for a wages paycheck its gross with the deposit it is expected to bring.
func budgetedText(v model.PeriodView) string {
	if v.Gross != 0 {
		return "Gross " + formatDollars(v.Gross) + " · deposit " + formatDollars(v.Budgeted)
	}
	return formatDollars(v.Budgeted)
}

// recentText is the variance of each recent period of the item, oldest first.
func recentText(r engine.ItemPeriods) string {
	var recent []string
	for _, h := range r.Periods {
		recent = append(recent, signedDollars(h.Variance))
	}
	return strings.Join(recent, "  ")
}

// recentTip says in words how far over or under budget each recent period of the item was, oldest first.
func recentTip(r engine.ItemPeriods) string {
	var lines []string
	for _, h := range r.Periods {
		lines = append(lines, h.Period.Start.String()+": "+overUnder(h.Variance))
	}
	return "Over or under budget, oldest first:\n" + strings.Join(lines, "\n")
}

// toggled is key, or nothing if it is already selected.
func toggled(sel, key string) string {
	if sel == key {
		return ""
	}
	return key
}

// periods draws the This period view: each budget item's period that contains the date, with what has posted, what remains, how far over or under budget it is and where it stands on the date, then the month's unaccounted money.
func (t *budgetTab) periods() {
	a := t.a
	s := &t.period
	t.periodDate()
	if d := s.shownDate(a); s.rec != a.rec || s.date != d {
		s.load(a.rec, d)
	}
	imgui.TextUnformatted("Periods on " + s.date.String() + ". A period runs from one occurrence of an item to the day before the next.")
	if s.date > a.rec.Today {
		disabledText("After today nothing is recorded: each line is judged on what is recorded up to " + a.rec.Today.String() + ", and what falls later is not yet due.")
	}
	disabledText("The projection applies only what remains of each period after its account's last recorded date: the expected amount, or the budgeted one, less what has posted. Grey is an estimate until you set an amount.")
	if s.err != "" {
		errorText(s.err)
		return
	}
	if len(s.rows) == 0 {
		disabledText("No budget item has a period on this date.")
		return
	}
	em := imgui.FontSize()
	defer s.transactions(a.rec)
	if !imgui.BeginTableV("##budgetperiods", int32(len(periodColumns)), imgui.TableFlagsBordersInnerV|imgui.TableFlagsPadOuterX|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	for _, c := range periodColumns {
		imgui.TableSetupColumnV(c.header, 0, c.weight, 0)
	}
	imgui.TableHeadersRow()
	for _, r := range s.rows {
		v := r.Current()
		id := r.Item.ID
		imgui.PushIDStr(id)
		imgui.TableNextRowV(0, 0)
		cell := func(i int, text string) {
			imgui.TableSetColumnIndex(int32(i))
			imgui.AlignTextToFramePadding()
			imgui.TextUnformatted(text)
		}
		cell(0, r.Item.Name)
		cell(1, v.Period.Start.String()+" to "+v.Period.End.String())
		cell(2, string(v.Timing))
		cell(3, budgetedText(v))

		imgui.TableSetColumnIndex(4)
		expected := s.expect[id]
		imgui.SetNextItemWidth(-1)
		if imgui.InputTextWithHint("##expected", formatDollars(v.Budgeted), &expected, 0, nil) {
			s.expect[id] = expected
		}
		tooltipOn("The amount expected for this period only, instead of the budgeted one")

		imgui.TableSetColumnIndex(5)
		posted := s.posted[id]
		hint := formatDollars(v.Posted)
		if v.PostedAuto {
			hint += " est."
		}
		imgui.SetNextItemWidth(-1)
		if imgui.InputTextWithHint("##posted", hint, &posted, 0, nil) {
			s.posted[id] = posted
		}
		tooltipOn("The amount posted in this period so far; it may be more than the budgeted amount")

		imgui.TableSetColumnIndex(6)
		var names, values []string
		for _, st := range model.OccurrenceStatuses {
			names, values = append(names, periodStatusNames[st]), append(values, string(st))
		}
		imgui.SetNextItemWidth(-1)
		if k := pick("status", periodStatusNames[s.status[id]], em*7, names, slices.Index(values, string(s.status[id]))); k >= 0 {
			s.status[id] = model.OccurrenceStatus(values[k])
		}
		tooltipOn("Posted: nothing more is applied. Pending: what remains is still to come, even for a lump whose date has passed")

		cell(7, formatDollars(v.Remaining))

		imgui.TableSetColumnIndex(8)
		imgui.AlignTextToFramePadding()
		if l, ok := s.standing[id]; ok {
			s.standingCell(l)
		}

		recent := recentText(r)
		cell(9, recent)
		if imgui.IsItemHovered() {
			tooltipText(fullTextTip(recent, textClipped(imgui.CalcTextSize(recent).X, imgui.ContentRegionAvail().X), recentTip(r)))
		}

		imgui.TableSetColumnIndex(10)
		ch, problem := s.periodChange(r)
		formButton("Set##period", problem, problem == "nothing is changed", func() { t.setPeriod(r, ch) })
		if v.Overridden {
			imgui.SameLine()
			if imgui.Button("Clear##period") {
				t.clearPeriod(r)
			}
			tooltipOn("Forget what was recorded for this period")
		}
		imgui.PopID()
	}
	s.unaccountedRows(a.rec)
}

// unaccountedKey is the selection of the account's unaccounted money.
func unaccountedKey(id model.AccountID) string { return "unaccounted:" + string(id) }

// unaccountedRows draws a row per account with unaccounted money in the month, and under the table the transactions of the selected line.
func (s *periodState) unaccountedRows(rec *model.Record) {
	for _, u := range s.unaccounted {
		key := unaccountedKey(u.Account)
		imgui.PushIDStr(key)
		imgui.TableNextRowV(0, 0)
		imgui.TableSetColumnIndex(0)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted("Unaccounted")
		imgui.TableSetColumnIndex(1)
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(rec.Account(u.Account).Name + ", " + u.Month.Time().Format("January 2006"))
		imgui.TableSetColumnIndex(8)
		if imgui.SelectableBoolV(signedDollars(u.Amount)+" outside the budget##standing", s.sel == key, 0, imgui.NewVec2(0, 0)) {
			s.sel = toggled(s.sel, key)
		}
		tooltipOn("List the transactions that no budget item accounts for")
		imgui.PopID()
	}
}

// transactions lists the transactions of the selected line.
func (s *periodState) transactions(rec *model.Record) {
	var lines []model.Transaction
	var accounts []model.AccountID
	title := ""
	if l, ok := s.standing[s.sel]; ok {
		title = l.Item.Name + ", " + l.View.Period.Start.String() + " to " + l.View.Period.End.String()
		for _, m := range l.Matches {
			lines, accounts = append(lines, m.Transaction), append(accounts, m.Ref.Account)
		}
	}
	for _, u := range s.unaccounted {
		if unaccountedKey(u.Account) == s.sel {
			title = "Unaccounted in " + rec.Account(u.Account).Name + ", " + u.Month.Time().Format("January 2006")
			for _, tx := range u.Transactions {
				lines, accounts = append(lines, tx.Transaction), append(accounts, tx.Ref.Account)
			}
		}
	}
	if title == "" {
		return
	}
	imgui.Spacing()
	imgui.SeparatorText(title)
	if len(lines) == 0 {
		disabledText("No transactions up to " + s.date.String() + ".")
		return
	}
	if !imgui.BeginTableV("##periodtransactions", 4, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	for i, h := range []string{"Date", "Account", "Amount", "Memo"} {
		imgui.TableSetupColumnV(h, 0, []float32{1, 2, 1, 4}[i], 0)
	}
	imgui.TableHeadersRow()
	for i, tx := range lines {
		imgui.TableNextRowV(0, 0)
		for c, text := range []string{tx.Date.String(), rec.Account(accounts[i]).Name, signedDollars(tx.Amount), tx.Memo} {
			imgui.TableSetColumnIndex(int32(c))
			imgui.TextUnformatted(text)
		}
	}
}
