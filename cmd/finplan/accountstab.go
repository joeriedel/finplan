// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// newAcctForm is the Add account form: every field is the text or choice as entered.
type newAcctForm struct {
	name, rate string
	typ        model.AccountType
	tax        model.TaxTreatment
}

// set changes one field: name, rate, type or tax.
func (f *newAcctForm) set(field, text string) {
	switch field {
	case "name":
		f.name = text
	case "rate":
		f.rate = text
	case "type":
		f.typ = model.AccountType(text)
	case "tax":
		f.tax = model.TaxTreatment(text)
	}
}

// account reads the form. A blank rate is 0; the rate is a percentage. An equity account has no rate, so the form's rate is ignored for it.
func (f newAcctForm) account() (model.Account, error) {
	a := model.Account{Name: strings.TrimSpace(f.name), Type: f.typ, Tax: f.tax}
	if f.typ == model.TypeEquity {
		return a, nil
	}
	var err error
	a.StatedRate, err = acctRate(f.rate)
	return a, err
}

// newAcctProblem says why the account the form describes cannot be added, or "" if it can.
func newAcctProblem(st *store.Store, rec *model.Record, f newAcctForm) string {
	a, err := f.account()
	if err != nil {
		return err.Error()
	}
	if a.Name == "" {
		return "enter a name"
	}
	if a.Type == "" {
		return "pick a type"
	}
	if a.Tax == "" {
		return "pick a tax treatment"
	}
	_, err = ops.NewAccount(st, rec, a, ops.Options{DryRun: true})
	return reasonOf(err)
}

// accountChange reads the new text of one attribute of an account being edited: name or rate (a percentage), the choice of type or tax, a mortgage's original amount, term in months, first payment date or paying account (the id; blank for none, which for the amount and the term is 0), or a 529's beneficiary (a member's name; blank for none) or basis (blank for unset).
func accountChange(field, text string) (ops.AccountChange, error) {
	var ch ops.AccountChange
	switch field {
	case "name":
		ch.Name = model.Ptr(text)
	case "type":
		ch.Type = model.Ptr(model.AccountType(text))
	case "tax":
		ch.Tax = model.Ptr(model.TaxTreatment(text))
	case "rate":
		v, err := acctRate(text)
		if err != nil {
			return ch, err
		}
		ch.StatedRate = &v
	case "original":
		v := 0.0
		if text != "" {
			var err error
			if v, err = parseValue(model.PropAmount, model.ModeSet, text); err != nil {
				return ch, err
			}
		}
		ch.OriginalAmount = &v
	case "term":
		n := 0
		if text != "" {
			var err error
			if n, err = strconv.Atoi(text); err != nil {
				return ch, fmt.Errorf("%q is not a whole number of months", text)
			}
		}
		ch.TermMonths = &n
	case "firstpayment":
		d, err := parseDate(text)
		if err != nil {
			return ch, fmt.Errorf("%v: enter the date of the first payment, or clear the terms", err)
		}
		ch.FirstPayment = &d
	case "paidfrom":
		ch.PaidFrom = model.Ptr(model.AccountID(text))
	case "beneficiary":
		ch.Beneficiary = model.Ptr(text)
	case "basis":
		if text == "" {
			ch.ClearBasis = true
			break
		}
		v, err := parseValue(model.PropAmount, model.ModeSet, text)
		if err != nil {
			return ch, err
		}
		ch.Basis = &v
	}
	return ch, nil
}

// snapshotProblem says why the balance the form describes cannot be recorded for the account, or "" if it can.
func snapshotProblem(st *store.Store, rec *model.Record, id model.AccountID, date, balance string) string {
	d, err := parseDate(strings.TrimSpace(date))
	if err != nil {
		return err.Error()
	}
	v, err := parseValue(model.PropAmount, model.ModeSet, balance)
	if err != nil {
		return err.Error()
	}
	_, err = ops.AddSnapshot(st, rec, id, d, v, ops.Options{DryRun: true})
	return reasonOf(err)
}

// closeDate reads the close date the form holds: the record's today when it is blank.
func closeDate(rec *model.Record, text string) (model.Date, error) {
	if strings.TrimSpace(text) == "" {
		return rec.Today, nil
	}
	return parseDate(strings.TrimSpace(text))
}

// closeProblem says why the account cannot be closed on the date the form holds, or "" if it can.
func closeProblem(st *store.Store, rec *model.Record, id model.AccountID, date string) string {
	d, err := closeDate(rec, date)
	if err != nil {
		return err.Error()
	}
	_, err = ops.CloseAccount(st, rec, id, d, ops.Options{DryRun: true})
	return reasonOf(err)
}

// interval reads a default balance interval from the form: a blank start is the beginning of time and a blank end no end.
func interval(from, to, value string) (model.Interval, error) {
	iv := model.Interval{From: model.MinDate}
	var err error
	if s := strings.TrimSpace(from); s != "" {
		if iv.From, err = parseDate(s); err != nil {
			return iv, err
		}
	}
	if s := strings.TrimSpace(to); s != "" {
		d, err := parseDate(s)
		if err != nil {
			return iv, err
		}
		iv.To = &d
	}
	iv.Value, err = parseValue(model.PropDefault, model.ModeSet, value)
	return iv, err
}

// intervalProblem says why the interval the form describes cannot be added to the account's default balances, or "" if it can.
func intervalProblem(st *store.Store, rec *model.Record, id model.AccountID, from, to, value string) string {
	iv, err := interval(from, to, value)
	if err != nil {
		return err.Error()
	}
	_, err = ops.AddDefault(st, rec, model.DefaultAssetID(id), iv, ops.Options{DryRun: true})
	return reasonOf(err)
}

// describeInterval writes a default balance interval for the list.
func describeInterval(iv model.Interval) string {
	from, to := "always", "no end"
	if iv.From != model.MinDate {
		from = iv.From.String()
	}
	if iv.To != nil {
		to = iv.To.String()
	}
	return fmt.Sprintf("%s to %s: %s", from, to, formatDollars(iv.Value))
}

// fieldEditor keeps the typed text and the refusal of the in-place text fields of a tab, by field id.
type fieldEditor struct {
	bufs, errs map[string]string
}

// acctTab is the Accounts tab: the accounts grouped by type on the left and the selected one on the right.
type acctTab struct {
	a *app
	// rec is the record the rows and buffers belong to; they are dropped when it changes.
	rec  *model.Record
	rows []listRow
	sel  model.AccountID
	del  deleteDialog
	fieldEditor

	add newAcctForm
	// hh is the Add member form of the household editor, and hhCheck keeps its problem.
	hh      householdForm
	// hhDel is the dialog that asks before a member who is a 529 beneficiary is removed, and hhDelName the member.
	hhDel     deleteDialog
	hhDelName string
	hhCheck problemCache
	// fers holds the Add FERS form of each member without a FERS annuity, by name, and fersCheck keeps the problem of each.
	fers                  map[string]fersForm
	fersCheck             map[string]*problemCache
	snapDate, snapBalance string
	ent                   entriesUI
	// eq is the state of the equity sections of an equity account.
	eq equityUI

	// scrollHousehold scrolls the household editor into view the next time it is drawn.
	scrollHousehold bool
	// openBenefits opens the Benefits section of the household members the next time they are drawn.
	openBenefits bool

	// ledger says whether the ledger panel is folded out to the right of the detail.
	ledger  bool
	closeOn string
	// closeCheck keeps the close problem of the account, which reads the database, until the record, account or date changes.
	closeCheck problemCache
	// addCheck, snapCheck and ivCheck keep the problems of the Add account, Record a balance and default balance forms.
	addCheck, snapCheck, ivCheck problemCache
	ivFrom, ivTo, ivValue        string
	// alloc is the text of the allocation editor's percent fields by class id, for the account allocFor; nil until it is drawn.
	alloc    map[string]string
	allocFor model.AccountID
}

func (t *acctTab) reset() {
	t.rows = listAccounts(t.rec)
	t.bufs, t.errs = map[string]string{}, map[string]string{}
	t.alloc = nil
}

func (t *acctTab) draw() {
	a := t.a
	if t.rec != a.rec {
		t.rec = a.rec
		t.reset()
	}
	if t.sel != "" && a.rec.Account(t.sel) == nil {
		t.sel = ""
	}
	acct := a.rec.Account(t.sel)
	showLedger := t.ledger && acct != nil && acct.Type != model.TypeEquity
	em := imgui.FontSize()
	listWidth, detailWidth := em*40, float32(0)
	if showLedger {
		avail := imgui.ContentRegionAvail().X - 2*imgui.CurrentStyle().ItemSpacing().X
		listWidth, detailWidth = ledgerWidths(avail, em)
	}
	imgui.PushStyleColorVec4(imgui.ColChildBg, rgba(colPanel, 1))
	open := imgui.BeginChildStrV("##acctlist", imgui.NewVec2(listWidth, 0), imgui.ChildFlagsBorders, 0)
	imgui.PopStyleColor()
	if open {
		t.list(!showLedger)
		t.addForm()
		t.household()
	}
	imgui.EndChild()
	imgui.SameLine()
	if imgui.BeginChildStrV("##acctdetail", imgui.NewVec2(detailWidth, 0), 0, 0) {
		if acct != nil {
			t.detail(acct)
		} else {
			disabledText("Select an account to see and change it.")
		}
	}
	imgui.EndChild()
	if showLedger {
		imgui.SameLine()
		if imgui.BeginChildStrV("##acctledger", imgui.NewVec2(0, 0), imgui.ChildFlagsBorders, 0) {
			t.ledgerPanel(acct)
		}
		imgui.EndChild()
	}
}

// ledgerWidths splits avail, the width for the list, detail and ledger panels, in font sizes em, when the ledger is open. The ledger gets at least ledgerMinEm, enough for the history table's fixed columns and its stretch columns; the detail gives up width first, down to detailMinEm, then the list, down to listMinEm.
func ledgerWidths(avail, em float32) (list, detail float32) {
	list, detail = em*40, em*32
	if short := em*ledgerMinEm - (avail - list - detail); short > 0 {
		cut := min(short, detail-em*detailMinEm)
		detail -= cut
		list -= min(short-cut, list-em*listMinEm)
	}
	return list, detail
}

const (
	ledgerMinEm = 40
	detailMinEm = 27
	listMinEm   = 28
)

// List column widths in font sizes. listNameEm is the room the Account column keeps for the demo's longest name, "529 youngest".
const (
	listTaxEm     = 6
	listBalanceEm = 6
	listLastEm    = 9
	listNameEm    = 9
)

// accountNameWidth is the width the Account column of the list gets in a list panel listWidth wide with padding pad on each side. The Tax column is left out while the ledger is open, which leaves the account names room that the narrow list would not.
func accountNameWidth(listWidth, pad, em float32, withTax bool) float32 {
	fixed := listBalanceEm + listLastEm
	if withTax {
		fixed += listTaxEm
	}
	return listWidth - 2*pad - em*float32(fixed)
}

// groupHasHeader reports whether the list titles the group: one account stands on its own, two or more are grouped under their title.
func groupHasHeader(g listGroup) bool { return len(g.Rows) > 1 }

// addFormStackedBelowEm is the list width, in font sizes, that the Add account form's two rows of fields need side by side: the longer row is about 481 px at 13 px, its labels, fields and spacing.
const addFormStackedBelowEm = 37

// addFormStacked reports whether the Add account form puts each field on its own line because avail, the width inside the list, is too narrow for its rows.
func addFormStacked(avail, em float32) bool {
	return avail < em*addFormStackedBelowEm
}

// list draws the accounts by group, with balance and the date of the last data; a stale account is marked in red. withTax says whether the tax treatment is shown.
func (t *acctTab) list(withTax bool) {
	em := imgui.FontSize()
	groups := groupAccounts(t.rows)
	if len(groups) == 0 {
		disabledText("No accounts yet.")
		return
	}
	cols, id := int32(3), "##accountsnarrow"
	if withTax {
		cols, id = 4, "##accounts"
	}
	if !imgui.BeginTableV(id, cols, imgui.TableFlagsBordersInnerV|imgui.TableFlagsPadOuterX|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	imgui.TableSetupColumnV("Account", 0, 2, 0)
	if withTax {
		imgui.TableSetupColumnV("Tax", imgui.TableColumnFlagsWidthFixed, em*listTaxEm, 0)
	}
	imgui.TableSetupColumnV("Balance", imgui.TableColumnFlagsWidthFixed, em*listBalanceEm, 0)
	imgui.TableSetupColumnV("Last data", imgui.TableColumnFlagsWidthFixed, em*listLastEm, 0)
	imgui.TableHeadersRow()
	for _, g := range groups {
		header := groupHasHeader(g)
		if header {
			groupHeaderRow(groupHeaderText(g))
		}
		for _, r := range g.Rows {
			t.listRow(r, withTax, header)
		}
	}
	imgui.EndTable()
}

// listRow draws one account; indented puts it under the header of its group.
func (t *acctTab) listRow(r listRow, withTax, indented bool) {
	imgui.PushIDStr("acct" + string(r.Account.ID))
	defer imgui.PopID()
	imgui.TableNextRowV(0, 0)
	imgui.TableSetColumnIndex(0)
	if indented {
		imgui.Indent()
	}
	if imgui.SelectableBoolV(r.Account.Name, t.sel == r.Account.ID, imgui.SelectableFlagsSpanAllColumns, imgui.NewVec2(0, 0)) {
		t.sel = r.Account.ID
	}
	if indented {
		imgui.Unindent()
	}
	text := func(i int, s string) {
		imgui.TableSetColumnIndex(int32(i))
		imgui.TextUnformatted(s)
	}
	col := 1
	if withTax {
		text(col, listTaxText(r.Account))
		col++
	}
	text(col, graph.FormatMoney(r.Balance))
	imgui.TableSetColumnIndex(int32(col + 1))
	switch {
	case r.Account.IsClosed():
		disabledText("closed " + r.Account.Closed.String())
	case !r.HasData:
		disabledText("no data")
	case r.Stale:
		errorText(r.LastData.String() + " stale")
	default:
		imgui.TextUnformatted(r.LastData.String())
	}
}

// field draws a text field showing value and returns the text the user typed once they finish editing it.
func (t *fieldEditor) field(id, hint, value string, width float32) (string, bool) {
	text := value
	if b, ok := t.bufs[id]; ok {
		text = b
	}
	shown := text
	imgui.SetNextItemWidth(width)
	imgui.InputTextWithHint("##"+id, hint, &text, 0, nil)
	if text != shown {
		t.bufs[id] = text
	}
	done := imgui.IsItemDeactivatedAfterEdit()
	if done && text == value {
		delete(t.bufs, id)
		delete(t.errs, id)
		done = false
	}
	if imgui.IsItemDeactivated() && !done {
		delete(t.bufs, id)
		delete(t.errs, id)
	}
	if e := t.errs[id]; e != "" {
		errorBeside("Not changed: " + e)
	}
	return text, done
}

// editAttr changes one attribute of the account through ops.EditAccount; a refusal is shown by the field.
func (t *acctTab) editAttr(id model.AccountID, field, text string) {
	fid := string(id) + field
	ch, err := accountChange(field, strings.TrimSpace(text))
	if err != nil {
		t.errs[fid] = err.Error()
		return
	}
	t.a.unchanged = false
	if !t.a.edit("change account", func() (*ops.Result, error) {
		return ops.EditAccount(t.a.st, t.a.rec, id, ch, ops.Options{})
	}) && t.a.unchanged {
		delete(t.bufs, fid)
		delete(t.errs, fid)
	}
}

func (t *fieldEditor) label(text string) {
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(text)
	imgui.SameLineV(imgui.FontSize()*9, -1)
}

func (t *acctTab) detail(acct *model.Account) {
	em := imgui.FontSize()
	id := acct.ID
	imgui.PushIDStr("detail" + string(id))
	defer imgui.PopID()
	imgui.PushTextWrapPos()
	defer imgui.PopTextWrapPos()
	imgui.SeparatorText(acct.Name)

	t.label("Name")
	if s, done := t.field(string(id)+"name", "", acct.Name, em*16); done {
		t.editAttr(id, "name", s)
	}
	t.label("Type")
	if k := pick("type", accountTypeName(acct.Type), em*16, accountTypeLabels, slices.Index(accountTypes, string(acct.Type))); k >= 0 {
		t.editAttr(id, "type", accountTypes[k])
	}
	t.label("Tax treatment")
	if k := pick("tax", taxName(acct.Tax), em*16, taxLabels, slices.Index(taxTreatments, string(acct.Tax))); k >= 0 {
		t.editAttr(id, "tax", taxTreatments[k])
	}
	if acct.Type == model.TypeEquity {
		t.equity(acct)
		t.lifecycle(acct)
		return
	}
	t.label("Rate per year")
	if s, done := t.field(string(id)+"rate", "percent, e.g. 3", formatValue(model.PropGrowth, model.ModeSet, acct.StatedRate), em*8); done {
		t.editAttr(id, "rate", s)
	}

	if acct.Type == model.TypeMortgage {
		t.mortgage(acct)
	}
	if acct.Type == model.Type529 {
		t.college(acct)
	}
	if model.Investable(acct.Type) {
		t.allocation(acct)
	}
	t.snapshots(acct)
	t.entries(acct)
	t.defaults(acct)
	t.lifecycle(acct)
}

// snapshots draws the Record a balance form. A balance is a record entry: it is appended and has no undo; correcting one is done on the entries below.
func (t *acctTab) snapshots(acct *model.Account) {
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.SeparatorText("Record a balance")
	if sp, ok := acct.Span(); ok {
		disabledText("Record entries run from " + sp.From.String() + " to " + sp.To.String() + ".")
	} else {
		disabledText("No balance recorded yet: the Graph shows the account once it has one.")
	}
	if acct.IsClosed() {
		errorText("Closed on " + acct.Closed.String() + ": a balance cannot be recorded after that date.")
	}
	t.label("Date")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##snapdate", dateHint, &t.snapDate, 0, nil)
	t.label("Balance")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##snapbal", "negative for a debt", &t.snapBalance, 0, nil)
	t.label("")
	formButton("Add##snapshot", t.snapCheck.get(problemKey(t.a.rec, string(acct.ID), [2]string{t.snapDate, t.snapBalance}), func() string {
		return snapshotProblem(t.a.st, t.a.rec, acct.ID, t.snapDate, t.snapBalance)
	}), allBlank(t.snapDate, t.snapBalance), func() {
		d, _ := parseDate(strings.TrimSpace(t.snapDate))
		v, _ := parseValue(model.PropAmount, model.ModeSet, t.snapBalance)
		if t.a.addEntry("record balance", func() (*ops.Result, error) {
			return ops.AddSnapshot(t.a.st, t.a.rec, acct.ID, d, v, ops.Options{})
		}) {
			t.snapDate, t.snapBalance = "", ""
		}
	})
}

// defaults draws the default balance intervals, the balances used on dates with no recorded data.
func (t *acctTab) defaults(acct *model.Account) {
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.SeparatorText("Default balances")
	wrappedText("The balance used on dates with no recorded data. Where intervals overlap, the one added last wins.")
	for i, iv := range acct.Default.Intervals {
		imgui.PushIDInt(int32(i))
		imgui.AlignTextToFramePadding()
		imgui.TextUnformatted(describeInterval(iv))
		imgui.SameLine()
		if imgui.Button("x") {
			t.a.edit("remove default balance", func() (*ops.Result, error) {
				return ops.RemoveDefault(t.a.st, t.a.rec, model.DefaultAssetID(acct.ID), i, ops.Options{})
			})
		}
		imgui.PopID()
	}
	if len(acct.Default.Intervals) == 0 {
		disabledText("None.")
	}
	t.label("From")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##ivfrom", "always; or "+dateHint, &t.ivFrom, 0, nil)
	t.label("To")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##ivto", "no end; or "+dateHint, &t.ivTo, 0, nil)
	t.label("Balance")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##ivvalue", "e.g. 25000", &t.ivValue, 0, nil)
	t.label("")
	formButton("Add##interval", t.ivCheck.get(problemKey(t.a.rec, string(acct.ID), [3]string{t.ivFrom, t.ivTo, t.ivValue}), func() string {
		return intervalProblem(t.a.st, t.a.rec, acct.ID, t.ivFrom, t.ivTo, t.ivValue)
	}), allBlank(t.ivFrom, t.ivTo, t.ivValue), func() {
		iv, _ := interval(t.ivFrom, t.ivTo, t.ivValue)
		if t.a.edit("add default balance", func() (*ops.Result, error) {
			return ops.AddDefault(t.a.st, t.a.rec, model.DefaultAssetID(acct.ID), iv, ops.Options{})
		}) {
			t.ivFrom, t.ivTo, t.ivValue = "", "", ""
		}
	})
}

// lifecycle draws closing, reopening and deleting the account.
func (t *acctTab) lifecycle(acct *model.Account) {
	em := imgui.FontSize()
	closeReason := t.closeCheck.get(fmt.Sprintf("%p|%s|%s", t.a.rec, acct.ID, t.closeOn), func() string {
		return closeProblem(t.a.st, t.a.rec, acct.ID, t.closeOn)
	})
	imgui.Spacing()
	imgui.SeparatorText("Close or delete")
	if acct.IsClosed() {
		wrappedText("Closed on " + acct.Closed.String() + ". It keeps its history, reads 0 after that date, and is left out of the pickers for new lines and objects.")
		actionButton("Reopen", "", func() {
			t.a.edit("reopen account", func() (*ops.Result, error) {
				return ops.ReopenAccount(t.a.st, t.a.rec, acct.ID, ops.Options{})
			})
		})
	} else {
		wrappedText("Closing keeps the history and shows the account as 0 after its close date. Only an account without record entries can be deleted; deleting it also deletes the budget items that use it.")
		t.label("Closes on")
		imgui.SetNextItemWidth(em * 10)
		imgui.InputTextWithHint("##closeon", "today; or "+dateHint, &t.closeOn, 0, nil)
		t.label("")
		actionButton("Close", closeReason, func() {
			d, _ := closeDate(t.a.rec, t.closeOn)
			if t.a.edit("close account", func() (*ops.Result, error) {
				return ops.CloseAccount(t.a.st, t.a.rec, acct.ID, d, ops.Options{})
			}) {
				t.closeOn = ""
			}
		})
	}
	imgui.Spacing()
	actionButton("Delete", "", func() {
		if plan, err := ops.AccountDeletePlan(t.a.st, t.a.rec, acct.ID); err != nil {
			t.a.notifyFailed("delete account", err)
		} else {
			t.del.ask(fmt.Sprintf("account %q", acct.Name), plan)
		}
	})
	switch t.del.draw(!acct.IsClosed(), closeReason) {
	case deleteGo:
		if t.a.edit("delete account", func() (*ops.Result, error) {
			return ops.DeleteAccount(t.a.st, t.a.rec, acct.ID, ops.Options{})
		}) {
			t.sel = ""
		}
	case deleteClose:
		d, err := closeDate(t.a.rec, t.closeOn)
		if err != nil {
			return
		}
		if t.a.edit("close account", func() (*ops.Result, error) {
			return ops.CloseAccount(t.a.st, t.a.rec, acct.ID, d, ops.Options{})
		}) {
			t.closeOn = ""
		}
	}
}

// addForm draws the Add account form under the list.
func (t *acctTab) addForm() {
	em := imgui.FontSize()
	f := &t.add
	imgui.Spacing()
	imgui.SeparatorText("Add account")
	input := func(field, label, hint, value string, width float32) {
		imgui.AlignTextToFramePadding()
		disabledText(label)
		imgui.SameLine()
		s := value
		imgui.SetNextItemWidth(width)
		if imgui.InputTextWithHint("##newacct"+field, hint, &s, 0, nil) {
			f.set(field, s)
		}
	}
	choose := func(field, label, cur, curName string, values, names []string) {
		imgui.AlignTextToFramePadding()
		disabledText(label)
		imgui.SameLine()
		preview := "choose"
		if cur != "" {
			preview = curName
		}
		if k := pick("newacct"+field, preview, em*8, names, slices.Index(values, cur)); k >= 0 {
			f.set(field, values[k])
		}
	}
	rowAvail := imgui.ContentRegionAvail().X
	sameLine := func() {
		if !addFormStacked(rowAvail, em) {
			imgui.SameLine()
		}
	}
	input("name", "name", "e.g. TSP", f.name, em*12)
	sameLine()
	choose("type", "type", string(f.typ), accountTypeName(f.typ), accountTypes, accountTypeLabels)
	sameLine()
	choose("tax", "tax", string(f.tax), taxName(f.tax), taxTreatments, taxLabels)
	if f.typ != model.TypeEquity {
		input("rate", "rate per year", "percent, e.g. 3", f.rate, em*6)
	}
	formButton("Add", t.addCheck.get(problemKey(t.a.rec, "", *f), func() string { return newAcctProblem(t.a.st, t.a.rec, *f) }), f.untouched(), func() {
		acct, _ := f.account()
		var added model.AccountID
		if t.a.edit("add account", func() (*ops.Result, error) {
			res, err := ops.NewAccount(t.a.st, t.a.rec, acct, ops.Options{})
			if err == nil {
				added = res.Account.ID
			}
			return res, err
		}) {
			t.add, t.sel = newAcctForm{}, added
		}
	})
}
