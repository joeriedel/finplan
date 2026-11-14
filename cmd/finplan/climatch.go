// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"

	"github.com/joeriedel/finplan/internal/model"
)

// matchDefaultDays is how far back budget matches and budget unaccounted look when --from is omitted.
const matchDefaultDays = 90

const (
	matchTxShape = "{date, account, amount (signed from the account's view), kind, memo}"
	matchOut     = "[{item, name, timing, periods: [{period_start, period_end, missing (true for a lump past due with no matching transaction in an account that has transactions in its period), matched_amount (the lump's transaction or the spread item's sum, as a positive amount; null when nothing matched), transactions: [" + matchTxShape + "]}]}]; periods are oldest first, a period is listed when it has a matched transaction dated in the range or is a missing lump due in it, and items with none are left out unless --item names one. Amounts are rounded to cents"
	unaccOut     = "[{account, month (first day), amount (signed sum from the account's view), transactions: [" + matchTxShape + "]}]; accounts in record order, months oldest first, months with none left out. Only income and expense transactions count; a transfer never does. Amounts are rounded to cents"
	matchNote    = "Matching is derived from the record, never stored: a transaction matches a budget item when it is on one of the item's accounts and either is linked to the item (entry add-transaction --item) or has the item's name, ignoring case, as its memo; a link wins over the memo. A lump item matches at most one transaction per occurrence, within 7 days of the occurrence's date (the nearest, then the closest in amount); a spread item matches every transaction in its period"
)

var matchCommands = []*command{
	{
		Name: "budget matches", Summary: "show which recorded transactions match each budget item, by period, and the lumps that are missing. " + matchNote,
		Flags: []flagDef{
			{Name: "item", Type: typeString, Description: "only this item: id or name"},
			{Name: "from", Type: typeDate, Description: "first day: " + dateNote + "; 90 days before --to when omitted"},
			{Name: "to", Type: typeDate, EndOfMonth: true, Description: "last day, inclusive: " + endNote + "; the record's today when omitted"},
		},
		Output: matchOut,
		run:    cmdBudgetMatches,
	},
	{
		Name: "budget unaccounted", Summary: "show the money recorded in each account that no budget item accounts for, per calendar month: the income and expense transactions that match no item",
		Flags: []flagDef{
			{Name: "account", Type: typeString, Description: "only this account: id or name"},
			{Name: "from", Type: typeDate, Description: "first day: " + dateNote + "; 90 days before --to when omitted"},
			{Name: "to", Type: typeDate, EndOfMonth: true, Description: "last day, inclusive: " + endNote + "; the record's today when omitted"},
		},
		Output: unaccOut,
		run:    cmdBudgetUnaccounted,
	},
}

func init() { commands = append(commands, matchCommands...) }

type jmatchTx struct {
	Date    jdate   `json:"date"`
	Account string  `json:"account"`
	Amount  float64 `json:"amount"`
	Kind    string  `json:"kind"`
	Memo    *string `json:"memo"`
}

func matchTxJSON(account model.AccountID, t model.Transaction) jmatchTx {
	return jmatchTx{jd(t.Date), string(account), cents(t.Amount), string(t.Kind), nullable(t.Memo)}
}

// dateRange reads --from and --to: to defaults to the record's today, from to matchDefaultDays before it.
func (c *call) dateRange(rec *model.Record) (from, to model.Date, err error) {
	to = rec.Today
	if d, ok, err := c.date("to"); err != nil {
		return 0, 0, err
	} else if ok {
		to = d
	}
	from = to.Add(-matchDefaultDays)
	if d, ok, err := c.date("from"); err != nil {
		return 0, 0, err
	} else if ok {
		from = d
	}
	if to < from {
		return 0, 0, usageErr("--to %v is before --from %v", to, from)
	}
	return from, to, nil
}

type jmatchPeriod struct {
	Start         jdate      `json:"period_start"`
	End           jdate      `json:"period_end"`
	Missing       bool       `json:"missing"`
	MatchedAmount *float64   `json:"matched_amount"`
	Transactions  []jmatchTx `json:"transactions"`
}

type jmatchItem struct {
	Item    string         `json:"item"`
	Name    string         `json:"name"`
	Timing  string         `json:"timing"`
	Periods []jmatchPeriod `json:"periods"`
}

func cmdBudgetMatches(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	from, to, err := c.dateRange(rec)
	if err != nil {
		return nil, err
	}
	only := ""
	if c.set["item"] {
		if only, err = c.itemRef(c.str("item")); err != nil {
			return nil, err
		}
		if _, ok := rec.Budget.Item(only); !ok {
			return nil, failErr("not_found", "there is no budget item %q", c.str("item"))
		}
	}
	matches := rec.Matches()
	out := []jmatchItem{}
	for _, it := range rec.Budget.Items {
		if only != "" && it.ID != only {
			continue
		}
		starts := []model.Date{}
		byStart := map[model.Date]*jmatchPeriod{}
		period := func(start model.Date) *jmatchPeriod {
			if jp := byStart[start]; jp != nil {
				return jp
			}
			p, _ := it.PeriodAt(start)
			jp := &jmatchPeriod{Start: jd(start), End: jd(p.End), Transactions: []jmatchTx{}}
			byStart[start] = jp
			starts = append(starts, start)
			return jp
		}
		for _, m := range matches.Item(it.ID) {
			if m.Transaction.Date >= from && m.Transaction.Date <= to {
				jp := period(m.Period)
				jp.Transactions = append(jp.Transactions, matchTxJSON(m.Ref.Account, m.Transaction))
			}
		}
		if it.EffectiveTiming() == model.Lump {
			for _, d := range it.Dates(from.Add(-1), min(to, rec.Today)) {
				if it.Start != nil && d < *it.Start || it.End != nil && d > *it.End {
					continue
				}
				if p, ok := it.PeriodAt(d); ok && p.Start == d {
					if pm := rec.PeriodMatch(it, p, rec.Today); pm != nil && !pm.Matched {
						period(d).Missing = true
					}
				}
			}
		}
		if len(starts) == 0 && only == "" {
			continue
		}
		slices.Sort(starts)
		row := jmatchItem{Item: it.ID, Name: it.Name, Timing: string(it.EffectiveTiming()), Periods: []jmatchPeriod{}}
		for _, s := range starts {
			jp := byStart[s]
			if p, ok := it.PeriodAt(s); ok {
				if pm := rec.PeriodMatch(it, p, rec.Today); pm != nil && pm.Matched {
					jp.MatchedAmount = model.Ptr(cents(pm.Amount))
				}
			}
			row.Periods = append(row.Periods, *jp)
		}
		out = append(out, row)
	}
	return out, nil
}

type junaccounted struct {
	Account      string     `json:"account"`
	Month        jdate      `json:"month"`
	Amount       float64    `json:"amount"`
	Transactions []jmatchTx `json:"transactions"`
}

func cmdBudgetUnaccounted(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	from, to, err := c.dateRange(rec)
	if err != nil {
		return nil, err
	}
	var account model.AccountID
	if c.set["account"] {
		if account, err = c.accountNamed(c.str("account")); err != nil {
			return nil, err
		}
	}
	out := []junaccounted{}
	for _, m := range rec.Unaccounted(account, from, to) {
		row := junaccounted{Account: string(m.Account), Month: jd(m.Month), Amount: cents(m.Amount), Transactions: []jmatchTx{}}
		for _, t := range m.Transactions {
			row.Transactions = append(row.Transactions, matchTxJSON(m.Account, t.Transaction))
		}
		out = append(out, row)
	}
	return out, nil
}
