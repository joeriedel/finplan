// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"math"
	"slices"

	"github.com/joeriedel/finplan/internal/model"
)

// Cause names why a balance changed.
type Cause string

// Causes. SocialSecurity and FERS are the benefits the household's members receive, which Income would otherwise show. GainsTax is the part of a year's tax that the capital gains realized in taxable accounts add, and Shock the change a market shock made to the holdings of a class (a line with a Class). Grant is the value of new RSU grants added to an equity account and Vest the net proceeds of its vests, which leave it for the deposit account; the withholding of a vest is Tax. Match is the employer's deposit on a paycheck. Interest is what a credit card owing money compounds, or the interest part of a mortgage payment, and Principal the part of a payment that pays the mortgage down; a mortgage payment shows on the paying account as interest and principal and on the mortgage as principal. Unexplained is the part of an actual change that snapshots show and transactions do not.
// Default is a change that comes from default balances, including a hand-off between defaults and
// actual data.
const (
	CauseIncome         Cause = "income"
	CauseSocialSecurity Cause = "social security"
	CauseFERS           Cause = "fers"
	CauseExpense        Cause = "expense"
	CauseTransfer       Cause = "transfer"
	CauseContribution   Cause = "contribution"
	CauseWithdrawal     Cause = "withdrawal"
	CauseMatch          Cause = "match"
	CauseInterest       Cause = "interest"
	CausePrincipal      Cause = "principal"
	CauseTax            Cause = "tax"
	CauseGainsTax       Cause = "gains tax"
	CauseShock          Cause = "shock"
	CauseGrant          Cause = "grant"
	CauseVest           Cause = "vest"
	CauseGrowth         Cause = "growth"
	CauseOpening        Cause = "opening"
	CauseUnexplained    Cause = "unexplained"
	CauseDefault        Cause = "default"
)

// residualTolerance is the size below which a leftover is float rounding, not a change to report.
const residualTolerance = 1e-6

var causeOrder = []Cause{
	CauseIncome, CauseSocialSecurity, CauseFERS, CauseExpense, CauseTransfer, CauseContribution, CauseMatch, CauseWithdrawal, CausePrincipal, CauseInterest,
	CauseGrant, CauseVest, CauseTax, CauseGainsTax, CauseGrowth, CauseShock, CauseOpening, CauseUnexplained, CauseDefault,
}

// Line is the change attributed to one cause, split by where it came from: Actual is read back from
// the record (and defaults), Projected is extrapolated. Class names the market class of a growth or shock line of an account with an allocation, and is empty on every other line.
type Line struct {
	Cause     Cause
	Class     string
	Actual    float64
	Projected float64
}

// BreakdownResult answers Breakdown. Start and End are the asset's values at From and To.
type BreakdownResult struct {
	Target   model.AssetID
	From, To model.Date
	Start    float64
	End      float64
	Lines    []Line // in a fixed cause order, causes with no change omitted; growth by class follows the other growth line in class order, shocks by class the shock cause's place
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
	acc := &accum{inside: map[model.AccountID]bool{}, lines: map[Cause]*Line{}, classes: map[string]*Line{}, shocks: map[string]*Line{}}
	for _, id := range r.res.Accounts {
		acc.inside[id] = true
	}
	for _, id := range r.res.Accounts {
		r.paths[id].breakdown(r.rec, from, to, acc)
	}
	s, e := r.eval(from), r.eval(to)
	out := &BreakdownResult{Target: target, From: from, To: to, Start: s.Amount, End: e.Amount, Diagnostics: r.diag}

	// Transfers that cancel only if both sides were recorded; a one-sided record is unexplained.
	if math.Abs(acc.cancelled) > residualTolerance {
		acc.add(CauseUnexplained, acc.cancelled, false)
	}
	// Summed in cause order: float addition order must not depend on map iteration.
	classIDs := make([]string, 0, len(acc.classes))
	known := func(id string) bool { return acc.classes[id] != nil || acc.shocks[id] != nil }
	for _, c := range rec.Classes {
		if known(c.ID) {
			classIDs = append(classIDs, c.ID)
		}
	}
	// Classes the record no longer has, for an allocation that names one, follow in ID order.
	var orphans []string
	for _, m := range []map[string]*Line{acc.classes, acc.shocks} {
		for id := range m {
			if rec.Class(id) == nil && !slices.Contains(orphans, id) {
				orphans = append(orphans, id)
			}
		}
	}
	slices.Sort(orphans)
	classIDs = append(classIDs, orphans...)
	sum := 0.0
	for _, c := range causeOrder {
		if l, ok := acc.lines[c]; ok {
			sum += l.Actual + l.Projected
		}
		for _, id := range classIDs {
			if l := acc.byCause(c, id); l != nil {
				sum += l.Projected
			}
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
		for _, id := range classIDs {
			if l := acc.byCause(c, id); l != nil && l.Projected != 0 {
				out.Lines = append(out.Lines, *l)
			}
		}
	}
	return out, nil
}

type accum struct {
	inside    map[model.AccountID]bool
	lines     map[Cause]*Line
	classes   map[string]*Line // growth by class
	shocks    map[string]*Line // shocks by class
	cancelled float64          // signed sum of transfers between accounts under the asset
}

// byCause is the line of the class under a cause that is split by class, nil for any other cause or a class with no line.
func (a *accum) byCause(c Cause, class string) *Line {
	switch c {
	case CauseGrowth:
		return a.classes[class]
	case CauseShock:
		return a.shocks[class]
	}
	return nil
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
		for _, c := range p.shocked {
			if c.date > lo && c.date <= to {
				line := acc.shocks[c.class]
				if line == nil {
					line = &Line{Cause: CauseShock, Class: c.class}
					acc.shocks[c.class] = line
				}
				line.Projected += c.amount
			}
		}
		if p.legs == nil {
			cause := CauseGrowth
			if p.acct.Type == model.TypeCreditCard {
				cause = CauseInterest
			}
			acc.add(cause, p.growthAt(to)-p.growthAt(lo), true)
		}
		for i, l := range p.legs {
			if l.class == "" {
				acc.add(CauseGrowth, p.legGrowthAt(i, to)-p.legGrowthAt(i, lo), true)
				continue
			}
			line := acc.classes[l.class]
			if line == nil {
				line = &Line{Cause: CauseGrowth, Class: l.class}
				acc.classes[l.class] = line
			}
			line.Projected += p.legGrowthAt(i, to) - p.legGrowthAt(i, lo)
		}
	}
}

// breakdownReadBack explains the change over (a, b], all before start. Inside the account's real data
// span it is its transactions and the unexplained remainder at snapshots (for an equity account with grants, its grants, recorded vests and price moves, then what remains); any difference left (dates
// outside the span, which use defaults) is a default change.
func (p *path) breakdownReadBack(rec *model.Record, a, b model.Date, acc *accum) {
	explained := 0.0
	if sp, ok := p.acct.Span(); ok {
		lo, hi := max(a, sp.From), min(b, min(sp.To, p.start))
		if hi > lo {
			for _, t := range p.acct.TransactionsIn(lo, hi) {
				explained += t.Amount
				if eq := vestDeposit(rec, p.acct.ID, t); eq != "" {
					acc.addFlow(CauseTransfer, t.Amount, eq, false)
					continue
				}
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
			total := p.acct.UnexplainedIn(lo, hi)
			explained += total
			u := total
			if p.acct.HoldsGrants() {
				ch := p.acct.EquityChange(lo, hi)
				u -= ch.Grants + ch.Growth
				acc.add(CauseGrant, ch.Grants, false)
				acc.add(CauseGrowth, ch.Growth, false)
				for _, v := range ch.Vests {
					g, _ := rec.Grant(v.Grant)
					acc.addFlow(CauseVest, -v.Net(), g.Brokerage, false)
					acc.add(CauseTax, -v.WithheldAmount(), false)
					u += v.Gross()
				}
				if math.Abs(u) <= residualTolerance {
					u = 0
				}
			}
			acc.add(CauseUnexplained, u, false)
		}
	}
	if rest := p.readBack(b) - p.readBack(a) - explained; math.Abs(rest) > residualTolerance {
		acc.add(CauseDefault, rest, false)
	}
}

// vestDeposit returns the equity account whose recorded vest is tied to the transaction, which paid its net proceeds into the account, or "" if no vest is or the transaction is a transfer, which names its counter itself.
func vestDeposit(rec *model.Record, id model.AccountID, t model.Transaction) model.AccountID {
	if t.Kind == model.TxTransfer || t.Entry == 0 {
		return ""
	}
	for _, a := range rec.Accounts {
		for _, v := range a.Vests {
			if g, _ := rec.Grant(v.Grant); g != nil && v.TxEntry == t.Entry && g.Brokerage == id {
				return a.ID
			}
		}
	}
	return ""
}
