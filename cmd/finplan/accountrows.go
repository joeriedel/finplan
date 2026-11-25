// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// balanceState says how far to trust the Balance of a listRow.
type balanceState int

const (
	// balanceCurrent is a balance worked out for the record shown.
	balanceCurrent balanceState = iota
	// balancePending is the balance the account had for the record before, shown until the one of the record shown is worked out.
	balancePending
	// balanceUnknown is no balance at all yet.
	balanceUnknown
)

// listRow is what the Accounts tab and account list show of one account.
type listRow struct {
	Account *model.Account
	// Balance is the account's balance on the record's today under reality, which is 0 once today is after the account's close date. It is not set in the rows of listAccountRows, and it is only to be shown as current if BalanceState says so.
	Balance      float64
	BalanceState balanceState
	// LastData is the date of the account's latest record entry; HasData is false for an account with no snapshot.
	LastData model.Date
	HasData  bool
	// Stale is set for an open account whose last data lies more than graph.StaleDays before today, the rule of the Graph's stale label.
	Stale bool
}

// listAccountRows returns a row per account in record order without the balances, which take an engine run each; the Accounts tab fills them in from a background cache.
func listAccountRows(rec *model.Record) []listRow {
	rows := make([]listRow, 0, len(rec.Accounts))
	for _, a := range rec.Accounts {
		r := listRow{Account: a}
		if sp, ok := a.Span(); ok {
			r.LastData, r.HasData = sp.To, true
			r.Stale = !a.IsClosed() && graph.Stale(sp.To, rec.Today)
		}
		rows = append(rows, r)
	}
	return rows
}

// listAccounts returns a row per account in record order, with the balances worked out here, as the account list of the CLI shows them.
func listAccounts(rec *model.Record) []listRow {
	rows := listAccountRows(rec)
	for i := range rows {
		rows[i].Balance = todayBalance(rec, model.DefaultAssetID(rows[i].Account.ID))
	}
	return rows
}

// pendingBalances returns the rows with the balance of each account from prev, the balances landed for the record before, as pending; an account without one has an unknown balance.
func pendingBalances(rows []listRow, prev map[model.AccountID]float64) []listRow {
	out := slices.Clone(rows)
	for i := range out {
		if v, ok := prev[out[i].Account.ID]; ok {
			out[i].Balance, out[i].BalanceState = v, balancePending
		} else {
			out[i].Balance, out[i].BalanceState = 0, balanceUnknown
		}
	}
	return out
}

// currentBalances returns the rows with the balance of each account from balance, as current.
func currentBalances(rows []listRow, balance func(model.AccountID) float64) []listRow {
	out := slices.Clone(rows)
	for i := range out {
		out[i].Balance, out[i].BalanceState = balance(out[i].Account.ID), balanceCurrent
	}
	return out
}

// balanceText is the Balance cell of a row: ... while it is not known.
func balanceText(r listRow) string {
	if r.BalanceState == balanceUnknown {
		return "..."
	}
	return graph.FormatMoney(r.Balance)
}

// listGroup is the accounts of one type, or the closed accounts.
type listGroup struct {
	Title string
	Rows  []listRow
}

const closedGroup = "Closed"

// groupAccounts groups rows by account type in the order of model.AccountTypes, keeping the order of rows within a group, with the closed accounts last in a group of their own. Empty groups are left out.
func groupAccounts(rows []listRow) []listGroup {
	var out []listGroup
	for _, t := range model.AccountTypes {
		g := listGroup{Title: accountTypeName(t)}
		for _, r := range rows {
			if !r.Account.IsClosed() && r.Account.Type == t {
				g.Rows = append(g.Rows, r)
			}
		}
		if len(g.Rows) > 0 {
			out = append(out, g)
		}
	}
	closed := listGroup{Title: closedGroup}
	for _, r := range rows {
		if r.Account.IsClosed() {
			closed.Rows = append(closed.Rows, r)
		}
	}
	if len(closed.Rows) > 0 {
		out = append(out, closed)
	}
	return out
}

// groupHeaderParts is the header row of a group: the title and the number of accounts as "529 · 3 · ", the sum of their balances as "$98,927", or ... if the balance of any is not known, and the state of that sum, pending if any balance of the group is.
func groupHeaderParts(g listGroup) (head, sum string, state balanceState) {
	total := 0.0
	for _, r := range g.Rows {
		total += r.Balance
		switch {
		case r.BalanceState == balanceUnknown:
			state = balanceUnknown
		case r.BalanceState == balancePending && state == balanceCurrent:
			state = balancePending
		}
	}
	head = fmt.Sprintf("%s \u00b7 %d \u00b7 ", g.Title, len(g.Rows))
	if state == balanceUnknown {
		return head, "...", state
	}
	return head, graph.FormatMoney(total), state
}

// groupHeaderText is the text of the header row of a group, as "529 · 3 · $98,927".
func groupHeaderText(g listGroup) string {
	head, sum, _ := groupHeaderParts(g)
	return head + sum
}

// listTaxText is the Tax cell of an account in the list: blank for an equity account, which has no tax treatment of its own.
func listTaxText(a *model.Account) string {
	if a.Type == model.TypeEquity {
		return ""
	}
	return taxName(a.Tax)
}
