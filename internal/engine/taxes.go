// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
)

// Taxes are the household's, not an account's: one calendar year's income, contributions and withdrawals across all accounts are taxed together by the rules package. The household's wages are attributed to a single tax.Earner, since budget items carry no member. A wages paycheck carries its share of the year's projected income tax on the household's paychecks alone, and the Social Security, Medicare and additional Medicare tax its wages incur cumulatively through the year; a vest keeps its own rates. On 15 April of the next year the difference between the year's tax and what was withheld is paid from, or refunded to, the household's reconciliation account.

// reconciliationDay is the day of April on which the previous year's tax is settled.
const reconciliationDay = 15

// ReconciliationAccount is the account tax is reconciled against on the date: the household's choice if it is open on the date, else the first checking account open on the date, "" if there is none.
func ReconciliationAccount(rec *model.Record, on model.Date) model.AccountID {
	if id := rec.Household.Checking; id != "" {
		if a := rec.Account(id); a == nil || !a.ClosedOn(on) {
			return id
		}
	}
	for _, a := range rec.Accounts {
		if a.Type == model.TypeChecking && !a.ClosedOn(on) {
			return a.ID
		}
	}
	return ""
}

// TaxYear is the household's tax for one calendar year, the inputs it was computed from, what was withheld and how it is settled.
type TaxYear struct {
	Year int
	// Input is what the rules package computed Result from.
	Input  tax.Input
	Result tax.Result
	// Wages are the gross wages paid by income items and VestWages the value of the year's RSU vests.
	Wages, VestWages float64
	// K401, IRA and HSA are the year's pre-tax contributions before the limits apply.
	K401, IRA, HSA float64
	// Ordinary is the income that is not wages: other-ordinary income items, interest and taxable withdrawals that are not early.
	Ordinary float64
	// Interest is the interest and growth earned on savings and checking accounts.
	Interest float64
	// EarlyWithdrawal is the withdrawals from tax-deferred accounts before 59 and a half.
	EarlyWithdrawal float64
	// ShortTermGains and LongTermGains are the capital gains the year's sales in taxable accounts realized, on lots held a year or less and longer; a loss is negative. GainsTax is the part of Result.Total they add.
	ShortTermGains, LongTermGains, GainsTax float64
	// WithheldPaychecks and WithheldVests are the tax withheld from the year's paychecks and vests, those before the projection's start included.
	WithheldPaychecks, WithheldVests float64
	// Due is the year's total tax less everything withheld; positive is paid on the reconciliation date and negative refunded.
	Due float64
	// ReconciliationDate is the date the difference is settled and ReconciliationAccount where, "" if the record has no checking account.
	ReconciliationDate    model.Date
	ReconciliationAccount model.AccountID
	Diagnostics
}

// Withheld is the tax withheld from the year's paychecks and vests.
func (t *TaxYear) Withheld() float64 { return t.WithheldPaychecks + t.WithheldVests }

// TaxYearOf returns the household's tax for the calendar year under the projection, "" for reality.
func TaxYearOf(rec *model.Record, projection string, year int) (*TaxYear, error) {
	view, err := rec.For(projection)
	if err != nil {
		return nil, err
	}
	var target model.AssetID
	switch id := ReconciliationAccount(view, model.D(year+1, time.April, reconciliationDay)); {
	case id != "" && view.Account(id) != nil:
		target = model.DefaultAssetID(id)
	case len(view.Accounts) > 0:
		target = model.DefaultAssetID(view.Accounts[0].ID)
	default:
		return nil, fmt.Errorf("engine: the record has no accounts")
	}
	r, err := prepare(rec, projection, target, model.D(year+1, time.April, reconciliationDay))
	if err != nil {
		return nil, err
	}
	out := r.tax.result(year)
	out.Diagnostics = r.diag
	return out, nil
}

// paycheck is one wages payment of an income item.
type paycheck struct {
	item  string
	date  model.Date
	gross float64
}

type paycheckKey struct {
	item string
	date model.Date
}

// vestPay is one vest of the year.
type vestPay struct {
	gross, withheld float64
}

// yearInputs are a year's amounts that do not depend on balances.
type yearInputs struct {
	paychecks    []paycheck
	wages        float64
	ordinary     float64 // other-ordinary income items and taxable withdrawals that are not early
	early        float64
	vests        float64
	vestWithheld float64
	k401, ira    float64
	hsa          float64
	// moved is the amount of each contribution to a limited account that the year's limit lets through, refused the part above it per account; matches is the employer match deposited on each paycheck that earns one.
	moved      map[paycheckKey]float64
	refused    map[model.AccountID]float64
	limit      float64
	matches    map[paycheckKey]float64
	contrib529 map[model.AccountID]float64
	// withheld is the tax withheld from each paycheck; paycheckWithheld their sum.
	withheld         map[paycheckKey]float64
	paycheckWithheld float64
}

// householdTax computes the household's tax for each year of one query.
type householdTax struct {
	rec     *model.Record
	items   []model.BudgetItem
	amounts map[string]model.Schedule
	growths map[string]model.Schedule
	infl    model.Schedule
	plans   []*equityPlan
	// hypothetical maps an item touching a hypothetical account to the date its flows count after.
	hypothetical map[string]model.Date
	diag         *Diagnostics
	years        map[int]*yearInputs
	results      map[int]*TaxYear
	reported     map[string]bool
	// savers are the savings and checking paths whose interest is taxed.
	savers []*path
	// lots are the paths of taxable investment accounts, whose sales realize gains.
	lots []*path
}

func newHouseholdTax(rec *model.Record, items []model.BudgetItem, amounts, growths map[string]model.Schedule, infl model.Schedule, plans []*equityPlan, hypothetical map[string]model.Date, diag *Diagnostics) *householdTax {
	return &householdTax{
		rec: rec, items: items, amounts: amounts, growths: growths, infl: infl, plans: plans, hypothetical: hypothetical, diag: diag,
		years: map[int]*yearInputs{}, results: map[int]*TaxYear{}, reported: map[string]bool{},
	}
}

func (h *householdTax) note(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if !h.reported[s] {
		h.reported[s] = true
		h.diag.Notes = append(h.diag.Notes, s)
	}
}

// inflation is the rate that indexes the tables past their last year.
func (h *householdTax) inflation(year int) float64 {
	v, _ := h.infl.At(model.D(year, time.January, 1))
	return v
}

// dates returns the dates of the item in the year that count for tax, whatever the projection's start: the schedule stands for the recorded part of the year too.
func (h *householdTax) dates(it model.BudgetItem, year int) []model.Date {
	after, upTo := model.D(year, time.January, 1).Add(-1), model.D(year, time.December, 31)
	if c, ok := h.hypothetical[it.ID]; ok {
		after = max(after, c)
	}
	if it.Start != nil {
		after = max(after, it.Start.Add(-1))
	}
	if it.End != nil {
		upTo = min(upTo, *it.End)
	}
	for _, id := range []model.AccountID{it.Account, it.From, it.To} {
		if a := h.rec.Account(id); a != nil && a.Closed != nil {
			upTo = min(upTo, *a.Closed)
		}
	}
	return it.Dates(after, upTo)
}

// earlyOn reports whether a withdrawal on d is before the household's earner is 59 and a half. Without an earner none is.
func (h *householdTax) earlyOn(d model.Date) bool {
	for _, m := range h.rec.Household.Members {
		if m.Role != model.RoleEarner {
			continue
		}
		by, bm, bd := m.Birth.YMD()
		y, mo, dd := d.YMD()
		months := (y-by)*12 + int(mo) - int(bm)
		if dd < bd {
			months--
		}
		return months < 59*12+6
	}
	return false
}

// year returns the year's inputs, computing the withholding of its paychecks on first use.
func (h *householdTax) year(y int) *yearInputs {
	if in, ok := h.years[y]; ok {
		return in
	}
	in := &yearInputs{contrib529: map[model.AccountID]float64{}, withheld: map[paycheckKey]float64{}, moved: map[paycheckKey]float64{}, refused: map[model.AccountID]float64{}, matches: map[paycheckKey]float64{}}
	var contribs []contribution
	h.years[y] = in
	unsheltered := func(id model.AccountID) bool {
		a := h.rec.Account(id)
		return a == nil || !a.Tax.Sheltered()
	}
	for _, it := range h.items {
		switch it.Kind {
		case model.ItemIncome:
			if it.Income() == model.IncomeTaxFree {
				continue
			}
			for _, d := range h.dates(it, y) {
				amt, ok := amountOn(h.rec, it, d, h.amounts, h.growths, h.infl)
				if !ok {
					continue
				}
				if it.Income() == model.IncomeWages {
					in.paychecks = append(in.paychecks, paycheck{it.ID, d, amt})
					in.wages += amt
				} else {
					in.ordinary += amt
				}
			}
		case model.ItemTransfer:
			src, dst := h.rec.Account(it.From), h.rec.Account(it.To)
			for _, d := range h.dates(it, y) {
				amt, ok := amountOn(h.rec, it, d, h.amounts, h.growths, h.infl)
				if !ok {
					continue
				}
				if dst != nil && unsheltered(it.From) {
					switch {
					case limited(dst):
						contribs = append(contribs, contribution{paycheckKey{it.ID, d}, dst.ID, amt})
					case dst.Type == model.TypeIRA && dst.Tax == model.TaxDeferred:
						in.ira += amt
					case dst.Type == model.TypeHSA:
						in.hsa += amt
					case dst.Type == model.Type529:
						in.contrib529[dst.ID] += amt
					}
				}
				if src != nil && src.Tax == model.TaxDeferred && unsheltered(it.To) {
					if h.earlyOn(d) {
						in.early += amt
					} else {
						in.ordinary += amt
					}
				}
			}
		}
	}
	h.limit(y, in, contribs)
	sort.SliceStable(in.paychecks, func(i, j int) bool { return in.paychecks[i].date < in.paychecks[j].date })
	for _, v := range h.vests(y) {
		in.vests += v.gross
		in.vestWithheld += v.withheld
	}
	h.withhold(y, in)
	return in
}

// limited reports whether contributions into the account count against the 401(k) limit.
func limited(a *model.Account) bool {
	return (a.Type == model.Type401k || a.Type == model.TypeTSP) && a.Tax == model.TaxDeferred
}

// contribution is one occurrence of a transfer item into a limited account.
type contribution struct {
	key   paycheckKey
	dst   model.AccountID
	gross float64
}

// limit caps the year's contributions into each limited account at the year's limit, earliest first, and works out the employer match of each paycheck: the share of its item's match percent of the smaller of the paycheck's match ceiling and the contributions into the match account that no earlier paycheck has matched.
func (h *householdTax) limit(y int, in *yearInputs, contribs []contribution) {
	limit, err := h.rec.Tax.Limit401k(y, h.inflation(y))
	if err != nil {
		h.note("tax %d: %v", y, err)
	}
	if err != nil || limit <= 0 {
		limit = math.Inf(1)
	}
	in.limit = limit
	sort.SliceStable(contribs, func(i, j int) bool { return contribs[i].key.date < contribs[j].key.date })
	total := map[model.AccountID]float64{}
	for _, c := range contribs {
		moved := min(c.gross, max(limit-total[c.dst], 0))
		total[c.dst] += moved
		in.moved[c.key] = moved
		in.refused[c.dst] += c.gross - moved
		in.k401 += moved
	}
	sorted := slices.Clone(in.paychecks)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].date < sorted[j].date })
	matched := map[model.AccountID]float64{}
	for _, pc := range sorted {
		i := slices.IndexFunc(h.items, func(it model.BudgetItem) bool { return it.ID == pc.item })
		it := h.items[i]
		if it.MatchAccount == "" || it.MatchPercent <= 0 || it.MatchUpTo <= 0 {
			continue
		}
		have := 0.0
		for _, c := range contribs {
			if c.dst == it.MatchAccount && c.key.date <= pc.date {
				have += in.moved[c.key]
			}
		}
		eligible := min(it.MatchUpTo/100*pc.gross, max(have-matched[it.MatchAccount], 0))
		matched[it.MatchAccount] += eligible
		if m := it.MatchPercent / 100 * eligible; m != 0 {
			in.matches[paycheckKey{pc.item, pc.date}] = m
		}
	}
}

// moved is the amount of the occurrence of the transfer item on d that is moved: the amount, less what the year's 401(k) limit refuses of a contribution to a limited account. A refusal is noted once for the account and year.
func (h *householdTax) moved(it model.BudgetItem, d model.Date, amt float64) float64 {
	y, _, _ := d.YMD()
	in := h.year(y)
	moved, ok := in.moved[paycheckKey{it.ID, d}]
	if !ok {
		return amt
	}
	if moved < amt {
		a := h.rec.Account(it.To)
		h.note("the %d 401(k) limit of %.2f refuses %.2f of the contributions to %s (%s): they are capped at the limit and the rest stays in the paying account", y, in.limit, in.refused[a.ID], a.Name, a.ID)
	}
	return moved
}

// match is the employer match the paycheck of the income item on d earns.
func (h *householdTax) match(item string, d model.Date) float64 {
	y, _, _ := d.YMD()
	return h.year(y).matches[paycheckKey{item, d}]
}

// vests returns the year's vests: those recorded and those planned after their account's start.
func (h *householdTax) vests(y int) []vestPay {
	var out []vestPay
	inYear := func(d model.Date) bool {
		dy, _, _ := d.YMD()
		return dy == y
	}
	starts := map[model.AccountID]model.Date{}
	for _, pl := range h.plans {
		starts[pl.account] = pl.start
	}
	for _, a := range h.rec.Accounts {
		start, planned := starts[a.ID]
		for _, v := range a.Vests {
			if inYear(v.Date) && (!planned || v.Date <= start) {
				out = append(out, vestPay{v.Gross(), v.WithheldAmount()})
			}
		}
	}
	for _, pl := range h.plans {
		for _, ev := range pl.events {
			if !ev.vest || !inYear(ev.date) {
				continue
			}
			price, _ := pl.prices.at(ev.key, ev.date)
			v := float64(ev.shares) * price
			out = append(out, vestPay{v, v * ev.rates.Sum()})
		}
	}
	return out
}

// withhold sets what each paycheck of the year withholds: its share of the income tax on the paychecks alone, in proportion to its gross, and the payroll tax its wages incur on top of the year's earlier paychecks.
func (h *householdTax) withhold(y int, in *yearInputs) {
	if in.wages <= 0 {
		return
	}
	hh := h.rec.Household
	w, err := tax.Compute(h.rec.Tax, tax.Input{
		Year: y, Status: hh.Filing, State: hh.State, DependentAges: hh.DependentAges(y), Inflation: h.inflation(y),
		Earners: []tax.Earner{{Wages: in.wages, K401: in.k401, IRA: in.ira, HSA: in.hsa}},
	})
	if err != nil {
		h.note("tax %d: %v", y, err)
		return
	}
	incomeTax := w.FederalTax - w.Credits + w.StateTax
	// The year's HSA contributions come out of FICA wages in proportion to the wages paid so far.
	payroll := func(wages float64) float64 {
		r, err := tax.Compute(h.rec.Tax, tax.Input{Year: y, Status: hh.Filing, Inflation: h.inflation(y), Earners: []tax.Earner{{Wages: wages, HSA: in.hsa * wages / in.wages}}})
		if err != nil {
			return 0
		}
		return r.SocialSecurity + r.Medicare + r.AdditionalMedicare
	}
	cum := 0.0
	for _, p := range in.paychecks {
		t := incomeTax*p.gross/in.wages + payroll(cum+p.gross) - payroll(cum)
		cum += p.gross
		in.withheld[paycheckKey{p.item, p.date}] = t
		in.paycheckWithheld += t
	}
}

// paycheckTax is the tax withheld from the paycheck of the income item on d.
func (h *householdTax) paycheckTax(item string, d model.Date) float64 {
	y, _, _ := d.YMD()
	return h.year(y).withheld[paycheckKey{item, d}]
}

// interest is what the savings and checking accounts earned in the year until they closed: growth after each account's start and, before it, the part of snapshots that transactions do not explain. An account's contribution is never negative.
func (h *householdTax) interest(y int, paths []*path) float64 {
	from, end := model.D(y-1, time.December, 31), model.D(y, time.December, 31)
	sum := 0.0
	for _, p := range paths {
		to := end
		if p.acct.Closed != nil {
			to = min(to, *p.acct.Closed)
		}
		if to <= from {
			continue
		}
		earned := p.growthAt(to) - p.growthAt(from)
		if p.start > from {
			earned += p.acct.UnexplainedIn(from, min(to, p.start))
		}
		sum += max(0, earned)
	}
	return sum
}

// result returns the year's tax: the one computed while the query reconciled, else computed from the accounts as they stand.
func (h *householdTax) result(y int) *TaxYear {
	if t, ok := h.results[y]; ok {
		return t
	}
	return h.compute(y, h.savers)
}

// compute returns the year's tax with the interest the paths earned in it.
func (h *householdTax) compute(y int, interest []*path) *TaxYear {
	in := h.year(y)
	hh := h.rec.Household
	t := &TaxYear{
		Year: y, Wages: in.wages, VestWages: in.vests, K401: in.k401, IRA: in.ira, HSA: in.hsa, EarlyWithdrawal: in.early,
		WithheldPaychecks: in.paycheckWithheld, WithheldVests: in.vestWithheld,
		ReconciliationDate: model.D(y+1, time.April, reconciliationDay), ReconciliationAccount: ReconciliationAccount(h.rec, model.D(y+1, time.April, reconciliationDay)),
	}
	t.Interest = h.interest(y, interest)
	t.Ordinary = in.ordinary + t.Interest
	for _, p := range h.lots {
		st, lt := p.gainsIn(y)
		t.ShortTermGains += st
		t.LongTermGains += lt
	}
	t.Input = tax.Input{
		Year: y, Status: hh.Filing, State: hh.State, DependentAges: hh.DependentAges(y), Inflation: h.inflation(y),
		Earners:     []tax.Earner{{Wages: in.wages + in.vests, K401: in.k401, IRA: in.ira, HSA: in.hsa}},
		OtherIncome: t.Ordinary, NetInvestmentIncome: t.Interest, EarlyWithdrawal: in.early,
		ShortTermGains: t.ShortTermGains, LongTermGains: t.LongTermGains,
	}
	for _, a := range h.rec.Accounts {
		if v, ok := in.contrib529[a.ID]; ok {
			t.Input.Contrib529 = append(t.Input.Contrib529, v)
		}
	}
	res, err := tax.Compute(h.rec.Tax, t.Input)
	if err != nil {
		h.note("tax %d: %v", y, err)
	}
	t.Result = res
	if t.ShortTermGains != 0 || t.LongTermGains != 0 {
		without := t.Input
		without.ShortTermGains, without.LongTermGains = 0, 0
		if base, err := tax.Compute(h.rec.Tax, without); err == nil {
			t.GainsTax = res.Total - base.Total
		}
	}
	t.Due = res.Total - t.Withheld()
	return t
}

// reconcile computes the tax of each year whose reconciliation falls within the horizon and books the difference on the reconciliation path, year by year: the interest of a year depends on the balances the earlier reconciliations leave. paths are every path of the query, the reconciliation account's among them, and interest the savings and checking paths.
func (h *householdTax) reconcile(paths, interest []*path, horizon model.Date) {
	h.savers = interest
	lo := horizon
	for _, p := range paths {
		lo = min(lo, p.start)
	}
	first, _, _ := lo.YMD()
	last, _, _ := horizon.YMD()
	for y := first - 1; y < last; y++ {
		date := model.D(y+1, time.April, reconciliationDay)
		if date > horizon {
			break
		}
		t := h.compute(y, interest)
		h.results[y] = t
		var recon *path
		for _, p := range paths {
			if p.acct.ID == t.ReconciliationAccount {
				recon = p
			}
		}
		if recon == nil {
			if t.Due != 0 {
				h.note("there is no open checking account on %s: the tax of %d is not reconciled", date, y)
			}
			continue
		}
		if date > recon.start {
			recon.changes = append(recon.changes, change{date: date, cause: CauseTax, amount: -(t.Due - t.GainsTax), rank: rankTax})
			if t.GainsTax != 0 {
				recon.changes = append(recon.changes, change{date: date, cause: CauseGainsTax, amount: -t.GainsTax, rank: rankTax})
			}
			recon.settle()
		}
	}
}
