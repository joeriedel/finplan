// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// addTx records an invented transaction of checking through the app and returns its id.
func addTx(t *testing.T, a *app, date model.Date, amount float64, kind model.TxKind, counter model.AccountID) int64 {
	t.Helper()
	var id int64
	ok := a.addEntry("record transaction", func() (*ops.Result, error) {
		res, err := ops.AddTransaction(a.st, a.rec, demodata.Checking, model.Transaction{Date: date, Amount: amount, Kind: kind, Counter: counter, Memo: "invented"}, ops.Options{})
		if err == nil {
			id = res.Entry.ID
		}
		return res, err
	})
	if !ok {
		t.Fatalf("not recorded: %v", a.notices)
	}
	return id
}

func rowByID(rows []entryRow, id int64) (entryRow, bool) {
	for _, r := range rows {
		if r.ID == id {
			return r, true
		}
	}
	return entryRow{}, false
}

func TestEntryRowsNewestFirstWithKindAndCounter(t *testing.T) {
	a := demoApp(t)
	day := model.D(2031, 3, 9)
	income := addTx(t, a, day, 100, model.TxIncome, "")
	transfer := addTx(t, a, day, -50, model.TxTransfer, demodata.Savings)
	if _, err := ops.AddSnapshot(a.st, a.rec, demodata.Checking, day.Add(1), 777, ops.Options{}); err != nil {
		t.Fatal(err)
	}
	rows, err := entryRows(a.st, a.rec, demodata.Checking, false)
	if err != nil || len(rows) < 4 {
		t.Fatalf("%d rows, %v", len(rows), err)
	}
	if rows[0].Kind != "Snapshot" || rows[0].Amount != 777 || rows[0].Counter != "" {
		t.Errorf("first row %+v", rows[0])
	}
	if rows[1].ID != transfer || rows[2].ID != income {
		t.Errorf("same-day order %d, %d; want %d, %d", rows[1].ID, rows[2].ID, transfer, income)
	}
	if rows[1].Kind != "Transfer" || rows[1].Counter != a.rec.Account(demodata.Savings).Name || rows[2].Kind != "Income" || rows[2].Counter != "" {
		t.Errorf("rows %+v, %+v", rows[1], rows[2])
	}
	if !slices.IsSortedFunc(rows, func(x, y entryRow) int { return int(y.Date) - int(x.Date) }) {
		t.Error("rows are not newest first")
	}
}

func TestEntryRowsVoidedAndHistory(t *testing.T) {
	a := demoApp(t)
	id := addTx(t, a, model.D(2031, 3, 9), -42, model.TxExpense, "")
	if p := voidProblem(a.st, a.rec, id, "duplicate"); p != "" {
		t.Fatalf("void: %q", p)
	}
	if !a.addEntry("void entry", func() (*ops.Result, error) { return ops.VoidEntry(a.st, a.rec, id, "duplicate", ops.Options{}) }) {
		t.Fatal(a.notices)
	}
	live, _ := entryRows(a.st, a.rec, demodata.Checking, false)
	all, _ := entryRows(a.st, a.rec, demodata.Checking, true)
	if _, ok := rowByID(live, id); ok {
		t.Error("the voided entry is listed")
	}
	if r, ok := rowByID(all, id); !ok || !r.Void || len(all) != len(live)+1 {
		t.Errorf("with voided: %+v, %v", r, ok)
	}
	if p := voidProblem(a.st, a.rec, id, ""); p == "" {
		t.Error("a voided entry can be voided again")
	}

	e, _ := a.st.Entry(id)
	if p := restoreProblem(a.st, a.rec, id, "mistake"); p != "" {
		t.Fatalf("restore: %q", p)
	}
	hist, err := historyRows(a.st, a.rec, e)
	if err != nil || len(hist) != 2 || hist[0].Amount != -42 || hist[0].Note != "" || !hist[1].Void || hist[1].Note != "duplicate" || hist[1].MadeAt.IsZero() {
		t.Fatalf("history %+v, %v", hist, err)
	}
	if !a.addEntry("restore entry", func() (*ops.Result, error) {
		return ops.CorrectEntry(a.st, a.rec, id, ops.EntryChange{}, "mistake", ops.Options{})
	}) {
		t.Fatal(a.notices)
	}
	live, _ = entryRows(a.st, a.rec, demodata.Checking, false)
	if _, ok := rowByID(live, id); !ok {
		t.Error("the restored entry is not listed")
	}
	e, _ = a.st.Entry(id)
	if hist, _ = historyRows(a.st, a.rec, e); len(hist) != 3 || hist[2].Void || hist[2].Note != "mistake" {
		t.Errorf("history after the restore %+v", hist)
	}
	if s, _ := a.history.PeekUndo(); s.Label == "void entry" || s.Label == "restore entry" {
		t.Errorf("an entry change pushed an undo step: %q", s.Label)
	}
}

func TestTxProblem(t *testing.T) {
	a := demoApp(t)
	ok := entryDraft{date: "2031-03-09", amount: "-5", kind: model.TxExpense}
	if p := txProblem(a.st, a.rec, demodata.Checking, ok); p != "" {
		t.Errorf("valid: %q", p)
	}
	for name, c := range map[string]struct {
		mod  func(d *entryDraft)
		want string
	}{
		"date":     {func(d *entryDraft) { d.date = "soon" }, "date"},
		"amount":   {func(d *entryDraft) { d.amount = "" }, "not a number"},
		"kind":     {func(d *entryDraft) { d.kind = "" }, "kind"},
		"transfer": {func(d *entryDraft) { d.kind = model.TxTransfer }, "counter"},
		"self":     {func(d *entryDraft) { d.kind, d.counter = model.TxTransfer, demodata.Checking }, "other than"},
	} {
		d := ok
		c.mod(&d)
		if p := txProblem(a.st, a.rec, demodata.Checking, d); !strings.Contains(p, c.want) {
			t.Errorf("%s: %q", name, p)
		}
	}
}

// A counter account is kept for a transfer only: any other kind saves none, for a new transaction and for a correction.
func TestCounterOnlyForTransfers(t *testing.T) {
	d := entryDraft{kind: model.TxTransfer, counter: demodata.Savings}
	if got := d.counterFor(); got != demodata.Savings {
		t.Errorf("transfer counter %q", got)
	}
	for _, k := range []model.TxKind{model.TxIncome, model.TxExpense} {
		d.kind = k
		if got := d.counterFor(); got != "" {
			t.Errorf("%s keeps the counter %q", k, got)
		}
	}

	a := demoApp(t)
	id := addTx(t, a, model.D(2031, 3, 9), -42, model.TxTransfer, demodata.Savings)
	e, _ := a.st.Entry(id)
	d = draftOf(e)
	d.kind = model.TxExpense
	ch, err := entryChange(e, d)
	if err != nil || ch.TxKind == nil || ch.Counter == nil || *ch.Counter != "" {
		t.Fatalf("change %+v, %v", ch, err)
	}
	if p := correctProblem(a.st, a.rec, e, d); p != "" {
		t.Fatalf("correct: %q", p)
	}
	if _, err := ops.CorrectEntry(a.st, a.rec, e.ID, ch, "not a transfer", ops.Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.st.Entry(id); got.TxKind != model.TxExpense || got.Counter != "" {
		t.Errorf("corrected entry %+v", got)
	}

	if p := txProblem(a.st, a.rec, demodata.Checking, entryDraft{date: "2031-03-10", amount: "-5", kind: model.TxExpense, counter: demodata.Checking}); p != "" {
		t.Errorf("expense with a leftover counter: %q", p)
	}
}

func TestCorrectProblemAndChange(t *testing.T) {
	a := demoApp(t)
	id := addTx(t, a, model.D(2031, 3, 9), -42, model.TxExpense, "")
	e, _ := a.st.Entry(id)
	d := draftOf(e)
	if p := correctProblem(a.st, a.rec, e, d); !strings.Contains(p, "nothing") {
		t.Errorf("unchanged: %q", p)
	}
	d.amount = "-45"
	d.note = "misread"
	ch, err := entryChange(e, d)
	if err != nil || ch.Amount == nil || *ch.Amount != -45 || ch.Date != nil || ch.TxKind != nil || ch.Counter != nil || ch.Memo != nil {
		t.Fatalf("change %+v, %v", ch, err)
	}
	if p := correctProblem(a.st, a.rec, e, d); p != "" {
		t.Fatalf("correct: %q", p)
	}
	d.kind = model.TxTransfer
	if p := correctProblem(a.st, a.rec, e, d); !strings.Contains(p, "counter") {
		t.Errorf("transfer without counter: %q", p)
	}
	d.amount = "x"
	if p := correctProblem(a.st, a.rec, e, d); !strings.Contains(p, "not a number") {
		t.Errorf("bad amount: %q", p)
	}
	d.amount = "-45"
	d.kind = model.TxExpense
	if !a.addEntry("correct entry", func() (*ops.Result, error) { return ops.CorrectEntry(a.st, a.rec, id, ch, d.note, ops.Options{}) }) {
		t.Fatal(a.notices)
	}
	if rows, _ := entryRows(a.st, a.rec, demodata.Checking, false); rowOf2(rows, id).Amount != -45 {
		t.Error("the table does not show the corrected amount")
	}
	e, _ = a.st.Entry(id)
	hist, _ := historyRows(a.st, a.rec, e)
	if len(hist) != 2 || hist[0].Amount != -42 || hist[1].Amount != -45 || hist[1].Note != "misread" {
		t.Errorf("history %+v", hist)
	}

	// A snapshot is a date and a balance only.
	es, _ := a.st.Entries(demodata.Checking)
	for _, se := range es {
		if se.Kind != store.KindSnapshot {
			continue
		}
		sd := draftOf(se)
		sd.memo, sd.kind = "ignored", model.TxIncome
		if ch, _ := entryChange(se, sd); ch.Memo != nil || ch.TxKind != nil {
			t.Errorf("snapshot change %+v", ch)
		}
		break
	}
}

func rowOf2(rows []entryRow, id int64) entryRow {
	r, _ := rowByID(rows, id)
	return r
}

func TestCounterChoicesExcludeSelfAndClosed(t *testing.T) {
	a := demoApp(t)
	rec := a.rec.Clone()
	closed := model.D(2020, 1, 1)
	rec.Account(demodata.Savings).Closed = &closed
	for _, c := range counterChoices(rec, demodata.Checking, "") {
		if c.ID == demodata.Checking || c.ID == demodata.Savings {
			t.Errorf("offered %q", c.ID)
		}
	}
	if !slices.ContainsFunc(counterChoices(rec, demodata.Checking, demodata.Savings), func(c *model.Account) bool { return c.ID == demodata.Savings }) {
		t.Error("the kept counter account is not offered")
	}
	if len(counterChoices(rec, demodata.Checking, "")) == 0 {
		t.Error("no choices")
	}
}

func TestEmptyEntries(t *testing.T) {
	if got := emptyEntries(0); got != "No entries yet." {
		t.Errorf("none recorded: %q", got)
	}
	if got := emptyEntries(2); !strings.Contains(got, "2 voided") || !strings.Contains(got, "Show voided") {
		t.Errorf("all voided: %q", got)
	}
}

func TestEntriesLabel(t *testing.T) {
	if got := entriesLabel(470, false); got != "Entries (470) >>" {
		t.Errorf("closed label = %q", got)
	}
	if got := entriesLabel(0, true); got != "Entries (0) <<" {
		t.Errorf("open label = %q", got)
	}
}

func TestShotOptionsLedger(t *testing.T) {
	o, err := newShotOptions("x.png", "Accounts", 10, "1280x800", "Checking", "ledger")
	if err != nil || o.open != "ledger" {
		t.Fatalf("ledger with Accounts and --select: %+v, %v", o, err)
	}
	if _, err := newShotOptions("x.png", "Accounts", 10, "1280x800", "", "ledger"); err == nil {
		t.Error("ledger without --select was accepted")
	}
	if _, err := newShotOptions("x.png", "Graph", 10, "1280x800", "", "ledger"); err == nil {
		t.Error("ledger on the Graph tab was accepted")
	}
}

func TestEntryTableHeight(t *testing.T) {
	const em = 13
	for _, tc := range []struct {
		avail    float32
		selected bool
		want     float32
	}{
		{500, false, 500},
		{500, true, 200},
		{100, true, em * 6},
		{40, false, em * 6},
	} {
		if got := entryTableHeight(tc.avail, em, tc.selected); got != tc.want {
			t.Errorf("entryTableHeight(%v, selected %v) = %v, want %v", tc.avail, tc.selected, got, tc.want)
		}
	}
}

func TestSelectionIn(t *testing.T) {
	rows := []entryRow{{ID: 4}, {ID: 7}}
	for _, tc := range []struct{ sel, want int64 }{{0, 0}, {7, 7}, {5, 0}} {
		if got := selectionIn(tc.sel, rows); got != tc.want {
			t.Errorf("selection %d: %d, want %d", tc.sel, got, tc.want)
		}
	}
	if got := selectionIn(7, nil); got != 0 {
		t.Errorf("no rows: %d", got)
	}
}
