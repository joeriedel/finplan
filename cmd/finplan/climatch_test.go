// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

type matchTx struct {
	Date    string  `json:"date"`
	Account string  `json:"account"`
	Amount  float64 `json:"amount"`
	Memo    *string `json:"memo"`
}

type matchItem struct {
	Item    string `json:"item"`
	Periods []struct {
		Start         string    `json:"period_start"`
		Missing       bool      `json:"missing"`
		MatchedAmount *float64  `json:"matched_amount"`
		Transactions  []matchTx `json:"transactions"`
	} `json:"periods"`
}

type unaccountedRow struct {
	Account      string    `json:"account"`
	Month        string    `json:"month"`
	Amount       float64   `json:"amount"`
	Transactions []matchTx `json:"transactions"`
}

func matchesOf(t *testing.T, path string, args ...string) []matchItem {
	t.Helper()
	var out []matchItem
	cliJSON(t, &out, append([]string{"--db", path, "budget", "matches"}, args...)...)
	return out
}

func unaccountedOf(t *testing.T, path string, args ...string) []unaccountedRow {
	t.Helper()
	var out []unaccountedRow
	cliJSON(t, &out, append([]string{"--db", path, "budget", "unaccounted"}, args...)...)
	return out
}

func unaccountedTotal(rows []unaccountedRow, memo string) (total float64, n int) {
	for _, r := range rows {
		for _, tx := range r.Transactions {
			if tx.Memo != nil && *tx.Memo == memo {
				total += tx.Amount
				n++
			}
		}
	}
	return total, n
}

// A groceries charge with the memo Groceries matches its item; a pharmacy charge matches none and lands in Unaccounted.
func TestCLIDemoGroceriesMatchAndPharmacyIsUnaccounted(t *testing.T) {
	path := seededFile(t)
	got := matchesOf(t, path, "--item", "groceries", "--from", "2026-09", "--to", "2026-10")
	if len(got) != 1 || got[0].Item != "groceries" || len(got[0].Periods) == 0 {
		t.Fatalf("groceries matches %+v", got)
	}
	n := 0
	for _, p := range got[0].Periods {
		for _, tx := range p.Transactions {
			n++
			if tx.Memo == nil || *tx.Memo != "Groceries" || tx.Account != "card" || tx.Amount >= 0 {
				t.Errorf("groceries matched %+v", tx)
			}
		}
	}
	if n < 40 {
		t.Errorf("only %d groceries transactions matched over two months", n)
	}
	for _, m := range matchesOf(t, path, "--from", "2026-09", "--to", "2026-10") {
		for _, p := range m.Periods {
			for _, tx := range p.Transactions {
				if tx.Memo != nil && *tx.Memo == "Pharmacy" {
					t.Errorf("the pharmacy charge matched %s", m.Item)
				}
			}
		}
	}
	rows := unaccountedOf(t, path, "--from", "2025-01", "--to", "2026-10")
	if total, n := unaccountedTotal(rows, "Pharmacy"); n == 0 || total >= 0 {
		t.Errorf("pharmacy charges in Unaccounted: %d totalling %v", n, total)
	}
	for _, r := range rows {
		if r.Account != "checking" && r.Account != "card" {
			t.Errorf("unaccounted on %s", r.Account)
		}
		sum := 0.0
		for _, tx := range r.Transactions {
			sum += tx.Amount
			if tx.Memo != nil && (*tx.Memo == "Groceries" || *tx.Memo == "Salary (Parent A)") {
				t.Errorf("%s is unaccounted", *tx.Memo)
			}
		}
		if math.Abs(sum-r.Amount) > 0.015 {
			t.Errorf("%s %s: amount %v, transactions sum %v", r.Account, r.Month, r.Amount, sum)
		}
	}
	if only := unaccountedOf(t, path, "--account", "Credit card", "--from", "2026-01", "--to", "2026-10"); len(only) == 0 || only[0].Account != "card" {
		t.Errorf("by account %+v", only)
	}
}

// Adding an item named like an unbudgeted payee re-matches the past transactions: Unaccounted drops by their amount.
func TestCLIAddingAnItemRematchesHistory(t *testing.T) {
	path := seededFile(t)
	rangeArgs := []string{"--from", "2025-01", "--to", "2026-10"}
	before, nBefore := unaccountedTotal(unaccountedOf(t, path, rangeArgs...), "Pharmacy")
	if nBefore == 0 {
		t.Fatal("no pharmacy charges to match")
	}
	var all float64
	for _, r := range unaccountedOf(t, path, rangeArgs...) {
		all += r.Amount
	}
	data(t, path, "", "budget", "add", "--kind", "expense", "--name", "pharmacy", "--amount", "40", "--every", "monthly", "--start", "2025-01-10", "--day", "10", "--account", "checking", "--timing", "spread")
	after, _ := unaccountedTotal(unaccountedOf(t, path, rangeArgs...), "Pharmacy")
	var allAfter float64
	for _, r := range unaccountedOf(t, path, rangeArgs...) {
		allAfter += r.Amount
	}
	// The item is on checking only, so only the checking charges leave Unaccounted; the card's stay.
	if after >= 0 || after <= before {
		t.Errorf("pharmacy unaccounted %v before, %v after", before, after)
	}
	if math.Abs((allAfter-all)+(before-after)) > 0.02 {
		t.Errorf("Unaccounted moved by %v, the matched pharmacy charges are %v", allAfter-all, before-after)
	}
	got := matchesOf(t, path, "--item", "pharmacy", "--from", "2025-01", "--to", "2026-10")
	if len(got) != 1 || len(got[0].Periods) == 0 {
		t.Errorf("the new item matched nothing: %+v", got)
	}
}

// A transaction linked to an item matches it whatever its memo, and a link can be cleared.
func TestCLILinkedTransactionMatches(t *testing.T) {
	path := seededFile(t)
	memoOf := func(item string) (n int) {
		for _, m := range matchesOf(t, path, "--item", item, "--from", "2026-11-01", "--to", "2026-11-08") {
			for _, p := range m.Periods {
				for _, tx := range p.Transactions {
					if tx.Memo != nil && *tx.Memo == "Birthday lunch" {
						n++
					}
				}
			}
		}
		return n
	}
	w := data(t, path, "", "entry", "add-transaction", "card", "--date", "2026-11-05", "--amount", "-61.5", "--kind", "expense", "--memo", "Birthday lunch", "--item", "dining")
	if memoOf("dining") != 1 {
		t.Fatal("the linked transaction does not match dining")
	}
	if total, n := unaccountedTotal(unaccountedOf(t, path, "--from", "2026-11-01", "--to", "2026-11-08"), "Birthday lunch"); n != 0 || total != 0 {
		t.Errorf("the linked transaction is unaccounted: %d, %v", n, total)
	}
	var entries []struct {
		ID   int64   `json:"id"`
		Memo *string `json:"memo"`
		Item *string `json:"item"`
	}
	cliJSON(t, &entries, "--db", path, "account", "entries", "card")
	found := false
	for _, e := range entries {
		if e.Memo != nil && *e.Memo == "Birthday lunch" {
			found = e.Item != nil && *e.Item == "dining"
		}
	}
	if !found {
		t.Errorf("the entry does not show its item: %+v", entries)
	}
	sid := strconv.FormatInt(*w.Entry.ID, 10)
	refusedWrite(t, path, "", 1, "not_found", "entry", "correct", sid, "--item", "no-such-item")
	refusedWrite(t, path, "", 1, "invalid", "entry", "correct", sid, "--item", "salary-a")
	data(t, path, "", "entry", "correct", sid, "--item", "none")
	if memoOf("dining") != 0 {
		t.Error("the cleared link still matches")
	}
	if code, msg := cliFail(t, 2, "--db", path, "entry", "correct", sid); code != "usage" || !strings.Contains(msg, "--item") {
		t.Errorf("correct with nothing: %s %s", code, msg)
	}
}

// A lump with no transaction in an account that has transactions in the period is missing, and the projection still applies it after the cutoff.
func TestCLISkippedBillIsMissing(t *testing.T) {
	path := seededFile(t)
	rows := periodRows(t, path)
	c := rows["childcare"]
	if !c.Missing || c.Posted != 0 || c.Remaining != c.Budgeted || !c.PostedAuto {
		t.Errorf("childcare %+v", c)
	}
	if rows["insurance"].Missing || rows["salary-a"].Missing {
		t.Errorf("a bill that was paid is missing: %+v %+v", rows["insurance"], rows["salary-a"])
	}
	var got []matchItem
	cliJSON(t, &got, "--db", path, "budget", "matches", "--item", "childcare", "--from", "2026-09", "--to", "2026-11")
	last := got[0].Periods[len(got[0].Periods)-1]
	if len(got) != 1 || last.Start != "2026-11-03" || !last.Missing || last.MatchedAmount != nil {
		t.Errorf("childcare matches %+v", got)
	}
	for _, p := range got[0].Periods[:len(got[0].Periods)-1] {
		if p.Missing || p.MatchedAmount == nil {
			t.Errorf("an earlier childcare period %+v", p)
		}
	}
}

// A spread item's posted amount, with no override, is the sum of its matched transactions.
func TestCLISpreadPostedIsTheMatchedSum(t *testing.T) {
	path := seededFile(t)
	d := periodRows(t, path)["dining"]
	if !d.PostedAuto {
		t.Fatalf("dining %+v", d)
	}
	var got []matchItem
	cliJSON(t, &got, "--db", path, "budget", "matches", "--item", "dining", "--from", d.Start, "--to", d.End)
	if len(got) != 1 || len(got[0].Periods) != 1 || got[0].Periods[0].Start != d.Start {
		t.Fatalf("dining matches %+v", got)
	}
	sum := 0.0
	for _, tx := range got[0].Periods[0].Transactions {
		sum -= tx.Amount
	}
	if math.Abs(sum-d.Posted) > 0.02 || got[0].Periods[0].MatchedAmount == nil || math.Abs(*got[0].Periods[0].MatchedAmount-d.Posted) > 0.01 {
		t.Errorf("dining posted %v, matched sum %v", d.Posted, sum)
	}
}

// Over a past year the breakdown has an unaccounted line, and the lines still total the change.
func TestCLIBreakdownHasAnUnaccountedLine(t *testing.T) {
	path := seededFile(t)
	var res struct {
		Start, End, Total float64
		Lines             []struct {
			Cause             string
			Actual, Projected float64
		}
	}
	cliJSON(t, &res, "--db", path, "breakdown", "checking", "--from", "2025-12-31", "--to", "2026-10-31")
	un, sum, expense := 0.0, 0.0, 0.0
	for _, l := range res.Lines {
		sum += l.Actual + l.Projected
		switch l.Cause {
		case "unaccounted":
			un = l.Actual
		case "expense":
			expense = l.Actual
		case "default":
			if math.Abs(l.Actual+l.Projected) > 0.01 {
				t.Errorf("a default line of %v", l.Actual+l.Projected)
			}
		}
	}
	if un >= 0 || expense >= 0 {
		t.Errorf("unaccounted %v, expense %v", un, expense)
	}
	if math.Abs(sum-(res.End-res.Start)) > 0.02 || math.Abs(res.Total-(res.End-res.Start)) > 0.02 {
		t.Errorf("the lines total %v, total %v, the change is %v", sum, res.Total, res.End-res.Start)
	}
}
