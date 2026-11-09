// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"math"

	"github.com/joeriedel/finplan/internal/model"
)

// Cause names why a balance changed.
type Cause string

// Causes. Unexplained is the part of an actual change that snapshots show and transactions do not.
// Default is a change that comes from default balances, including a hand-off between defaults and
// actual data.
const (
	CauseIncome       Cause = "income"
	CauseExpense      Cause = "expense"
	CauseTransfer     Cause = "transfer"
	CauseContribution Cause = "contribution"
	CauseWithdrawal   Cause = "withdrawal"
	CauseTax          Cause = "tax"
	CauseGrowth       Cause = "growth"
	CauseUnexplained  Cause = "unexplained"
	CauseDefault      Cause = "default"
)

// residualTolerance is the size below which a leftover is float rounding, not a change to report.
const residualTolerance = 1e-6

var causeOrder = []Cause{
	CauseIncome, CauseExpense, CauseTransfer, CauseContribution, CauseWithdrawal,
	CauseTax, CauseGrowth, CauseUnexplained, CauseDefault,
}

// Line is the change attributed to one cause, split by where it came from: Actual is read back from
// the record (and defaults), Projected is extrapolated.
type Line struct {
	Cause     Cause
	Actual    float64
	Projected float64
}

// BreakdownResult answers Breakdown. Start and End are the asset's values at From and To.
type BreakdownResult struct {
	Target   model.AssetID
	From, To model.Date
	Start    float64
	End      float64
	Lines    []Line // in a fixed cause order, causes with no change omitted
	Diagnostics
}

// Total is the sum of every line; it equals End - Start.
func (b *BreakdownResult) Total() float64 {
	sum := 0.0
	for _, l := range b.Lines {
		sum += l.Actual + l.Projected
	}
	return sum
}

// Breakdown splits the target asset's change over (from, to] by cause. Transfers between two accounts
// both under the asset cancel; a transfer to or from an account outside it shows as a transfer,
// contribution or withdrawal.
func Breakdown(rec *model.Record, projection string, target model.AssetID, from, to model.Date) (*BreakdownResult, error) {
	if to < from {
		return nil, fmt.Errorf("engine: breakdown interval ends before it starts: %v..%v", from, to)
	}
	r, err := prepare(rec, projection, target, to)
	if err != nil {
		return nil, err
	}
	acc := &accum{inside: map[model.AccountID]bool{}, lines: map[Cause]*Line{}}
	for _, id := range r.res.Accounts {
		acc.inside[id] = true
	}
	for _, id := range r.res.Accounts {
		r.paths[id].breakdown(rec, from, to, acc)
	}
	s, e := r.eval(from), r.eval(to)
	out := &BreakdownResult{Target: target, From: from, To: to, Start: s.Amount, End: e.Amount, Diagnostics: r.diag}

	// Transfers that cancel only if both sides were recorded; a one-sided record is unexplained.
	if math.Abs(acc.cancelled) > residualTolerance {
		acc.add(CauseUnexplained, acc.cancelled, false)
	}
	// Summed in cause order: float addition order must not depend on map iteration.
	sum := 0.0
	for _, c := range causeOrder {
		if l, ok := acc.lines[c]; ok {
			sum += l.Actual + l.Projected
		}
	}
	// What remains comes from asset-level defaults, which no account's changes explain.
	if rest := e.Amount - s.Amount - sum; math.Abs(rest) > residualTolerance {
		acc.add(CauseDefault, rest, false)
	}
	for _, c := range causeOrder {
		if l, ok := acc.lines[c]; ok && (l.Actual != 0 || l.Projected != 0) {
			out.Lines = append(out.Lines, *l)
		}
	}
	return out, nil
}

type accum struct {
	inside    map[model.AccountID]bool
	lines     map[Cause]*Line
	cancelled float64 // signed sum of transfers between accounts under the asset
}

func (a *accum) add(c Cause, amount float64, projected bool) {
	l := a.lines[c]
	if l == nil {
		l = &Line{Cause: c}
		a.lines[c] = l
	}
	if projected {
		l.Projected += amount
	} else {
		l.Actual += amount
	}
}

// addFlow records a flow, dropping it if its counter-account is under the asset: the other side's
// record cancels it.
func (a *accum) addFlow(c Cause, amount float64, counter model.AccountID, projected bool) {
	if counter != "" && a.inside[counter] {
		a.cancelled += amount
		return
	}
	a.add(c, amount, projected)
}

func (p *path) breakdown(rec *model.Record, from, to model.Date, acc *accum) {
	if a, b := min(from, p.start), min(to, p.start); b > a {
		p.breakdownReadBack(rec, a, b, acc)
	}
	if to > p.start {
		lo := max(from, p.start)
		for _, c := range p.changes {
			if c.date > lo && c.date <= to {
				acc.addFlow(c.cause, c.amount, c.counter, true)
			}
		}
		acc.add(CauseGrowth, p.growthAt(to)-p.growthAt(lo), true)
	}
}

// breakdownReadBack explains the change over (a, b], all before start. Inside the account's real data
// span it is its transactions and the unexplained remainder at snapshots; any difference left (dates
// outside the span, which use defaults) is a default change.
func (p *path) breakdownReadBack(rec *model.Record, a, b model.Date, acc *accum) {
	explained := 0.0
	if sp, ok := p.acct.Span(); ok {
		lo, hi := max(a, sp.From), min(b, min(sp.To, p.start))
		if hi > lo {
			for _, t := range p.acct.TransactionsIn(lo, hi) {
				explained += t.Amount
				switch t.Kind {
				case model.TxIncome:
					acc.addFlow(CauseIncome, t.Amount, "", false)
				case model.TxExpense:
					acc.addFlow(CauseExpense, t.Amount, "", false)
				default:
					src, dst := p.acct.ID, t.Counter
					if t.Amount > 0 {
						src, dst = dst, src
					}
					acc.addFlow(transferCause(rec, src, dst), t.Amount, t.Counter, false)
				}
			}
			u := p.acct.UnexplainedIn(lo, hi)
			explained += u
			acc.add(CauseUnexplained, u, false)
		}
	}
	if rest := p.readBack(b) - p.readBack(a) - explained; math.Abs(rest) > residualTolerance {
		acc.add(CauseDefault, rest, false)
	}
}
