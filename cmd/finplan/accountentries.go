// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

var txKinds = []model.TxKind{model.TxIncome, model.TxExpense, model.TxTransfer}

// entryRow is one line of the entries table.
type entryRow struct {
	ID     int64
	Date   model.Date
	Kind   string
	Amount float64
	// Counter is the name of the counter account, blank if the entry has none.
	Counter string
	Memo    string
	Void    bool
}

// entryKindLabel names the kind of an entry: snapshot, or the kind of the transaction.
func entryKindLabel(e store.Entry) string {
	return entryKindName(e.Kind, e.TxKind)
}

// counterName is the name of the account, else its id if the record no longer has it, and blank for no account.
func counterName(rec *model.Record, id model.AccountID) string {
	if id == "" {
		return ""
	}
	if a := rec.Account(id); a != nil {
		return a.Name
	}
	return string(id)
}

// entryRows lists the account's entries newest first for the table.
func entryRows(st *store.Store, rec *model.Record, id model.AccountID, withVoided bool) ([]entryRow, error) {
	es, err := ops.ListEntries(st, id, withVoided)
	if err != nil {
		return nil, err
	}
	rows := make([]entryRow, len(es))
	for i, e := range es {
		rows[i] = entryRow{e.ID, e.Date, entryKindLabel(e), e.Amount, counterName(rec, e.Counter), e.Memo, e.Void}
	}
	return rows, nil
}

// historyRow is one revision of an entry for the history view.
type historyRow struct {
	Kind    string
	Date    model.Date
	Amount  float64
	Counter string
	Memo    string
	MadeAt  time.Time
	Note    string
	Void    bool
}

// historyRows lists every revision of the entry, the original first.
func historyRows(st *store.Store, rec *model.Record, e store.Entry) ([]historyRow, error) {
	revs, err := ops.EntryHistory(st, e.ID)
	if err != nil {
		return nil, err
	}
	rows := make([]historyRow, len(revs))
	for i, r := range revs {
		rows[i] = historyRow{entryKindName(e.Kind, r.TxKind), r.Date, r.Amount, counterName(rec, r.Counter), r.Memo, r.MadeAt.Local(), r.Note, r.Void}
	}
	return rows, nil
}

// counterChoices lists the accounts a transaction of self can name as its counter account: the open ones other than self, and keep, which a correction leaves chosen even if it has since been closed.
func counterChoices(rec *model.Record, self, keep model.AccountID) []*model.Account {
	var out []*model.Account
	for _, a := range rec.Accounts {
		if a.ID != self && (!a.IsClosed() || a.ID == keep) {
			out = append(out, a)
		}
	}
	return out
}

// entryDraft is the text and choices of an entry form.
type entryDraft struct {
	date, amount, memo, note string
	kind                     model.TxKind
	counter                  model.AccountID
}

// draftOf prefills a draft from the current values of the entry.
func draftOf(e store.Entry) entryDraft {
	return entryDraft{date: e.Date.String(), amount: trimFloat(e.Amount), memo: e.Memo, kind: e.TxKind, counter: e.Counter}
}

// counterFor is the counter account the draft saves: only a transfer has one.
func (d entryDraft) counterFor() model.AccountID {
	if d.kind != model.TxTransfer {
		return ""
	}
	return d.counter
}

// draftValues reads the date and amount the draft holds.
func (d entryDraft) draftValues() (model.Date, float64, error) {
	date, err := parseDate(strings.TrimSpace(d.date))
	if err != nil {
		return 0, 0, err
	}
	amount, err := parseValue(model.PropAmount, model.ModeSet, d.amount)
	return date, amount, err
}

// txProblem says why the transaction the form describes cannot be recorded for the account, or "" if it can.
func txProblem(st *store.Store, rec *model.Record, id model.AccountID, d entryDraft) string {
	date, amount, err := d.draftValues()
	if err != nil {
		return err.Error()
	}
	if d.kind == "" {
		return "pick a kind"
	}
	t := model.Transaction{Date: date, Amount: amount, Kind: d.kind, Counter: d.counterFor(), Memo: d.memo}
	_, err = ops.AddTransaction(st, rec, id, t, ops.Options{DryRun: true})
	return reasonOf(err)
}

// entryChange is the change that turns the entry into what the draft holds: only the values that differ. A snapshot has a date and a balance only.
func entryChange(e store.Entry, d entryDraft) (ops.EntryChange, error) {
	var ch ops.EntryChange
	date, amount, err := d.draftValues()
	if err != nil {
		return ch, err
	}
	if date != e.Date {
		ch.Date = &date
	}
	if amount != e.Amount {
		ch.Amount = &amount
	}
	if e.Kind == store.KindTransaction {
		if d.kind != e.TxKind {
			ch.TxKind = &d.kind
		}
		if c := d.counterFor(); c != e.Counter {
			ch.Counter = &c
		}
		if d.memo != e.Memo {
			ch.Memo = &d.memo
		}
	}
	return ch, nil
}

// correctProblem says why the entry cannot be corrected to what the draft holds, or "" if it can.
func correctProblem(st *store.Store, rec *model.Record, e store.Entry, d entryDraft) string {
	ch, err := entryChange(e, d)
	if err != nil {
		return err.Error()
	}
	_, err = ops.CorrectEntry(st, rec, e.ID, ch, d.note, ops.Options{DryRun: true})
	if errors.Is(err, ops.ErrNoChange) {
		return "nothing is changed"
	}
	return reasonOf(err)
}

// voidProblem says why the entry cannot be voided, or "" if it can.
func voidProblem(st *store.Store, rec *model.Record, id int64, note string) string {
	_, err := ops.VoidEntry(st, rec, id, note, ops.Options{DryRun: true})
	return reasonOf(err)
}

// restoreProblem says why the voided entry cannot be restored, or "" if it can: a correction that changes no value.
func restoreProblem(st *store.Store, rec *model.Record, id int64, note string) string {
	_, err := ops.CorrectEntry(st, rec, id, ops.EntryChange{}, note, ops.Options{DryRun: true})
	return reasonOf(err)
}

// problemCache keeps the answer of a problem function that reads the database until its key changes.
type problemCache struct {
	key, reason string
	known       bool
}

func (c *problemCache) get(key string, problem func() string) string {
	if !c.known || c.key != key {
		c.key, c.reason, c.known = key, problem(), true
	}
	return c.reason
}

// entriesUI is the state of the entries section of the selected account.
type entriesUI struct {
	// rec, acct and voided say what rows were read for.
	rec    *model.Record
	acct   model.AccountID
	voided bool
	rows   []entryRow
	// hidden counts the voided entries left out of rows.
	hidden int
	err    error

	newTx entryDraft
	// sel is the selected entry, with the draft and history read for it.
	sel      int64
	entry    store.Entry
	draft    entryDraft
	history  []historyRow
	histErr  error
	selRec   *model.Record
	problems [3]problemCache
}

// entries reads the rows of the account when they are stale, draws the add-transaction form and the button that folds the ledger out.
func (t *acctTab) entries(acct *model.Account) {
	u, a := &t.ent, t.a
	if u.newTx.kind == "" {
		u.newTx.kind = model.TxExpense
	}
	if u.rec != a.rec || u.acct != acct.ID {
		if u.acct != acct.ID {
			u.sel, u.newTx = 0, entryDraft{kind: model.TxExpense}
		}
		u.rec, u.acct = a.rec, acct.ID
		t.loadRows(acct.ID)
	}
	t.txForm(acct)

	imgui.Spacing()
	if imgui.Button(entriesLabel(len(u.rows), t.ledger) + "##ledger") {
		t.ledger = !t.ledger
	}
}

// entriesLabel is the text of the button that folds the ledger out: the number of entries listed and which way it opens.
func entriesLabel(n int, open bool) string {
	if open {
		return fmt.Sprintf("Entries (%d) <<", n)
	}
	return fmt.Sprintf("Entries (%d) >>", n)
}

// ledgerPanel draws the Show voided checkbox, the entries table and the selected entry's correct, void and history.
func (t *acctTab) ledgerPanel(acct *model.Account) {
	u := &t.ent
	imgui.SeparatorText("Entries")
	if imgui.Checkbox("Show voided", &u.voided) {
		t.loadRows(acct.ID)
	}
	if u.err != nil {
		errorText("Could not read the entries: " + u.err.Error())
		return
	}
	if len(u.rows) == 0 {
		disabledText(emptyEntries(u.hidden))
	} else {
		t.entryTable(acct)
	}
	if u.sel != 0 {
		t.entryPanel(acct)
	}
}

// selectionIn is the selected entry after the rows change: 0, nothing, if it is not among them, as when Show voided is unticked while a voided entry is selected.
func selectionIn(sel int64, rows []entryRow) int64 {
	if slices.ContainsFunc(rows, func(r entryRow) bool { return r.ID == sel }) {
		return sel
	}
	return 0
}

// loadRows reads the rows of the account, clears the selection if its entry is not among them, and counts the voided entries they leave out.
func (t *acctTab) loadRows(id model.AccountID) {
	u, a := &t.ent, t.a
	u.rows, u.err = entryRows(a.st, a.rec, id, u.voided)
	u.sel = selectionIn(u.sel, u.rows)
	u.hidden = 0
	if u.err != nil || u.voided {
		return
	}
	all, err := ops.ListEntries(a.st, id, true)
	if err != nil {
		u.err = err
		return
	}
	u.hidden = len(all) - len(u.rows)
}

// emptyEntries says why an account shows no entries: none recorded, or all of them voided and hidden.
func emptyEntries(hidden int) string {
	if hidden == 0 {
		return "No entries yet."
	}
	return fmt.Sprintf("No entries shown; %d voided. Tick Show voided to see them.", hidden)
}

// txForm draws the form that adds a transaction beside the balance form.
func (t *acctTab) txForm(acct *model.Account) {
	em := imgui.FontSize()
	d := &t.ent.newTx
	imgui.Spacing()
	imgui.SeparatorText("Record a transaction")
	disabledText("The amount is signed from this account's view: negative when money leaves it.")
	if acct.IsClosed() {
		errorText("Closed on " + acct.Closed.String() + ": a transaction cannot be recorded after that date.")
	}
	t.label("Date")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##txdate", dateHint, &d.date, 0, nil)
	t.label("Amount")
	imgui.SetNextItemWidth(em * 10)
	imgui.InputTextWithHint("##txamount", "negative for an expense", &d.amount, 0, nil)
	t.label("Kind")
	d.kind = t.kindPick("txkind", d.kind)
	if d.kind == model.TxTransfer {
		t.label("Transfer account")
		d.counter = t.counterPick("txcounter", acct.ID, d.counter)
	}
	t.label("Memo")
	imgui.SetNextItemWidth(em * 16)
	imgui.InputTextWithHint("##txmemo", "optional", &d.memo, 0, nil)
	t.label("")
	formButton("Add##entry", txProblem(t.a.st, t.a.rec, acct.ID, *d), d.untouched(), func() {
		date, amount, _ := d.draftValues()
		tx := model.Transaction{Date: date, Amount: amount, Kind: d.kind, Counter: d.counterFor(), Memo: d.memo}
		if t.a.addEntry("record transaction", func() (*ops.Result, error) {
			return ops.AddTransaction(t.a.st, t.a.rec, acct.ID, tx, ops.Options{})
		}) {
			*d = entryDraft{kind: d.kind}
		}
	})
}

// kindPick draws the combo of transaction kinds and returns the kind it holds.
func (t *acctTab) kindPick(id string, cur model.TxKind) model.TxKind {
	labels := make([]string, len(txKinds))
	for i, k := range txKinds {
		labels[i] = txKindName(k)
	}
	if k := pick(id, txKindName(cur), imgui.FontSize()*10, labels, slices.Index(txKinds, cur)); k >= 0 {
		return txKinds[k]
	}
	return cur
}

// counterPick draws the combo of counter accounts for a transaction of self and returns the one it holds.
func (t *acctTab) counterPick(id string, self, cur model.AccountID) model.AccountID {
	choices := counterChoices(t.a.rec, self, cur)
	labels := []string{"(none)"}
	selected := 0
	for i, c := range choices {
		labels = append(labels, c.Name)
		if c.ID == cur {
			selected = i + 1
		}
	}
	preview := "(none)"
	if cur != "" {
		preview = counterName(t.a.rec, cur)
	}
	k := pick(id, preview, imgui.FontSize()*16, labels, selected)
	switch {
	case k == 0:
		return ""
	case k > 0:
		return choices[k-1].ID
	}
	return cur
}

// entriesTableID is the ID of the entries table, whose scrolling window is named after it.
const entriesTableID = "##entries"

// entryTableHeight is the height of the entries table in a ledger panel with avail left below its heading, in font sizes em: all of it while no entry is selected, else 40% of it so that the selected entry's form and history fit below. It is never under six font sizes, room for the header and a few rows.
func entryTableHeight(avail, em float32, selected bool) float32 {
	if selected {
		avail *= 0.4
	}
	return max(avail, em*6)
}

// entryTable draws the entries newest first, only the visible rows, and selects one on a click. The table fills the room the ledger panel has left, less the selected entry's panel.
func (t *acctTab) entryTable(acct *model.Account) {
	em := imgui.FontSize()
	u := &t.ent
	height := entryTableHeight(imgui.ContentRegionAvail().Y, em, u.sel != 0)
	if !imgui.BeginTableV(entriesTableID, 5, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp|imgui.TableFlagsScrollY, imgui.NewVec2(0, height), 0) {
		return
	}
	defer imgui.EndTable()
	imgui.TableSetupScrollFreeze(0, 1)
	imgui.TableSetupColumnV("Date", imgui.TableColumnFlagsWidthFixed, em*8, 0)
	imgui.TableSetupColumnV("Kind", imgui.TableColumnFlagsWidthFixed, em*6, 0)
	imgui.TableSetupColumnV("Amount", imgui.TableColumnFlagsWidthFixed, em*8, 0)
	imgui.TableSetupColumnV("Transfer account", 0, 1, 0)
	imgui.TableSetupColumnV("Memo", 0, 1, 0)
	imgui.TableHeadersRow()
	clipper := imgui.NewListClipper()
	defer clipper.Destroy()
	clipper.Begin(int32(len(u.rows)))
	for clipper.Step() {
		for i := clipper.DisplayStart(); i < clipper.DisplayEnd(); i++ {
			r := u.rows[i]
			imgui.TableNextRowV(0, 0)
			imgui.TableSetColumnIndex(0)
			label := r.Date.String()
			if r.Void {
				label += " (voided)"
			}
			if imgui.SelectableBoolV(label+"##"+strconv.FormatInt(r.ID, 10), u.sel == r.ID, imgui.SelectableFlagsSpanAllColumns, imgui.NewVec2(0, 0)) {
				u.sel, u.selRec = r.ID, nil
			}
			imgui.TableSetColumnIndex(1)
			imgui.TextUnformatted(r.Kind)
			imgui.TableSetColumnIndex(2)
			imgui.TextUnformatted(formatDollars(r.Amount))
			imgui.TableSetColumnIndex(3)
			imgui.TextUnformatted(r.Counter)
			imgui.TableSetColumnIndex(4)
			imgui.TextUnformatted(r.Memo)
			if r.Memo != "" && imgui.IsItemHoveredV(imgui.HoveredFlagsAllowWhenOverlappedByItem) {
				tooltipText(r.Memo)
			}
		}
	}
}

// entryPanel draws the correct form, the void or restore button and the history of the selected entry.
func (t *acctTab) entryPanel(acct *model.Account) {
	em := imgui.FontSize()
	a, u := t.a, &t.ent
	if u.selRec != a.rec {
		e, err := a.st.Entry(u.sel)
		if err != nil || e.Account != acct.ID {
			u.sel = 0
			return
		}
		u.entry, u.draft, u.selRec = e, draftOf(e), a.rec
		u.history, u.histErr = historyRows(a.st, a.rec, e)
		u.problems = [3]problemCache{}
	}
	e, d := u.entry, &u.draft
	key := fmt.Sprintf("%p|%+v", a.rec, *d)

	imgui.Spacing()
	title := fmt.Sprintf("%s of %s", entryKindLabel(e), e.Date)
	if e.Void {
		title += " (voided)"
	}
	imgui.SeparatorText(title)
	if e.Void {
		disabledText("This entry is out of the record. Restoring it puts it back with its last values.")
		t.label("Note")
		imgui.SetNextItemWidth(em * 24)
		imgui.InputTextWithHint("##entnote", "why", &d.note, 0, nil)
		t.label("")
		actionButton("Restore", u.problems[2].get(key, func() string { return restoreProblem(a.st, a.rec, e.ID, d.note) }), func() {
			a.addEntry("restore entry", func() (*ops.Result, error) {
				return ops.CorrectEntry(a.st, a.rec, e.ID, ops.EntryChange{}, d.note, ops.Options{})
			})
		})
	} else {
		t.label("Date")
		imgui.SetNextItemWidth(em * 10)
		imgui.InputTextWithHint("##entdate", dateHint, &d.date, 0, nil)
		t.label("Amount")
		imgui.SetNextItemWidth(em * 10)
		imgui.InputTextWithHint("##entamount", "", &d.amount, 0, nil)
		if e.Kind == store.KindTransaction {
			t.label("Kind")
			d.kind = t.kindPick("entkind", d.kind)
			if d.kind == model.TxTransfer {
				t.label("Transfer account")
				d.counter = t.counterPick("entcounter", acct.ID, d.counter)
			}
			t.label("Memo")
			imgui.SetNextItemWidth(em * 16)
			imgui.InputTextWithHint("##entmemo", "", &d.memo, 0, nil)
		}
		t.label("Note")
		imgui.SetNextItemWidth(em * 24)
		imgui.InputTextWithHint("##entnote", "why; kept in the history", &d.note, 0, nil)
		key = fmt.Sprintf("%p|%+v", a.rec, *d)
		t.label("")
		actionButton("Correct", u.problems[0].get(key, func() string { return correctProblem(a.st, a.rec, e, *d) }), func() {
			ch, _ := entryChange(e, *d)
			a.addEntry("correct entry", func() (*ops.Result, error) {
				return ops.CorrectEntry(a.st, a.rec, e.ID, ch, d.note, ops.Options{})
			})
		})
		t.label("")
		actionButton("Void", u.problems[1].get(key, func() string { return voidProblem(a.st, a.rec, e.ID, d.note) }), func() {
			a.addEntry("void entry", func() (*ops.Result, error) {
				return ops.VoidEntry(a.st, a.rec, e.ID, d.note, ops.Options{})
			})
		})
	}
	t.history(u)
}

// history draws every revision of the selected entry, the original first.
func (t *acctTab) history(u *entriesUI) {
	em := imgui.FontSize()
	imgui.Spacing()
	imgui.SeparatorText("History")
	if u.histErr != nil {
		errorText("Could not read the history: " + u.histErr.Error())
		return
	}
	if !imgui.BeginTableV("##history", 6, imgui.TableFlagsBordersInnerV|imgui.TableFlagsSizingStretchProp, imgui.NewVec2(0, 0), 0) {
		return
	}
	defer imgui.EndTable()
	imgui.TableSetupColumnV("Made", imgui.TableColumnFlagsWidthFixed, em*11, 0)
	imgui.TableSetupColumnV("Date", imgui.TableColumnFlagsWidthFixed, em*8, 0)
	imgui.TableSetupColumnV("Kind", imgui.TableColumnFlagsWidthFixed, em*6, 0)
	imgui.TableSetupColumnV("Amount", imgui.TableColumnFlagsWidthFixed, em*8, 0)
	imgui.TableSetupColumnV("Transfer account and memo", 0, 1, 0)
	imgui.TableSetupColumnV("Note", 0, 1, 0)
	imgui.TableHeadersRow()
	for _, r := range u.history {
		imgui.TableNextRowV(0, 0)
		imgui.TableSetColumnIndex(0)
		imgui.TextUnformatted(r.MadeAt.Format("2006-01-02 15:04"))
		imgui.TableSetColumnIndex(1)
		imgui.TextUnformatted(r.Date.String())
		imgui.TableSetColumnIndex(2)
		imgui.TextUnformatted(r.Kind)
		imgui.TableSetColumnIndex(3)
		imgui.TextUnformatted(formatDollars(r.Amount))
		imgui.TableSetColumnIndex(4)
		imgui.TextUnformatted(strings.TrimSpace(r.Counter + " " + r.Memo))
		imgui.TableSetColumnIndex(5)
		note := r.Note
		if r.Void {
			note = strings.TrimSpace("voided " + note)
		}
		imgui.TextUnformatted(note)
	}
}
