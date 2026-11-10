// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// listRow is what the Accounts tab and account list show of one account.
type listRow struct {
	Account *model.Account
	// Balance is the account's balance on the record's today under reality, which is 0 once today is after the account's close date.
	Balance float64
	// LastData is the date of the account's latest record entry; HasData is false for an account with no snapshot.
	LastData model.Date
	HasData  bool
	// Stale is set for an open account whose last data lies more than graph.StaleDays before today, the rule of the Graph's stale label.
	Stale bool
}

// listAccounts returns a row per account in record order.
func listAccounts(rec *model.Record) []listRow {
	rows := make([]listRow, 0, len(rec.Accounts))
	for _, a := range rec.Accounts {
		r := listRow{Account: a}
		if res, err := engine.Balances(rec, "", model.DefaultAssetID(a.ID), []model.Date{rec.Today}); err == nil {
			r.Balance = res.Samples[0].Amount
		}
		if sp, ok := a.Span(); ok {
			r.LastData, r.HasData = sp.To, true
			r.Stale = !a.IsClosed() && graph.Stale(sp.To, rec.Today)
		}
		rows = append(rows, r)
	}
	return rows
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
		g := listGroup{Title: string(t)}
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
