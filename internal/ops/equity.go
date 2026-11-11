// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// Vest calendars are documents: they are added, edited and deleted like budget items. Grants, recorded vests and price observations are append-only facts like entries: an add stores a new one, and a correction or void keeps what it replaced in the history.

// editCalendars runs change on a deep copy of the record and commits the copy. It refuses a change that leaves the calendars as they were.
func editCalendars(st *store.Store, rec *model.Record, o Options, change func(next *model.Record) (Result, error)) (*Result, error) {
	next := rec.Clone()
	res, err := change(next)
	if err != nil {
		return nil, err
	}
	if reflect.DeepEqual(rec.Calendars, next.Calendars) {
		return nil, ErrNoChange
	}
	return commit(st, next, res, o)
}

func unknownCalendar(name string) error {
	return refuse(ErrUnknownObject, "there is no vest calendar %q", name)
}

// NewCalendar adds a vest calendar. A name another calendar has is ErrNameTaken, and a calendar that fails VestCalendar.Validate (a month with two rules, a fixed day some year lacks such as 30 February or 31 April, an Nth other than 1 to 4 and last, an override of a date the rules do not generate) is ErrInvalidValue.
func NewCalendar(st *store.Store, rec *model.Record, cal model.VestCalendar, o Options) (*Result, error) {
	cal.Name = strings.TrimSpace(cal.Name)
	return editCalendars(st, rec, o, func(next *model.Record) (Result, error) {
		if cal.Name == "" {
			return Result{}, refuse(ErrEmptyName, "a vest calendar needs a name")
		}
		if next.Calendar(cal.Name) != nil {
			return Result{}, refuse(ErrNameTaken, "there is already a vest calendar named %q", cal.Name)
		}
		if err := cal.Validate(); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		c := cal.Clone()
		next.Calendars = append(next.Calendars, c)
		return Result{Calendar: c}, nil
	})
}

// EditCalendar replaces the rules and overrides of the calendar, keeping its name. The same checks as NewCalendar apply. A grant whose planned vests the new calendar would break (its first vest is no longer a calendar date, or the calendar has another number of vests for it) is ErrInUse.
func EditCalendar(st *store.Store, rec *model.Record, name string, rules []model.VestRule, overrides []model.DateOverride, o Options) (*Result, error) {
	return editCalendars(st, rec, o, func(next *model.Record) (Result, error) {
		c := next.Calendar(name)
		if c == nil {
			return Result{}, unknownCalendar(name)
		}
		c.Rules, c.Overrides = slices.Clone(rules), slices.Clone(overrides)
		if err := c.Validate(); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		for _, a := range next.Accounts {
			for i := range a.Grants {
				if g := &a.Grants[i]; g.Calendar == name {
					if _, err := g.Plan(c); err != nil {
						return Result{}, refuse(ErrInUse, "grant %d would no longer have its planned vests: %v", g.ID, err)
					}
				}
			}
		}
		return Result{Calendar: c}, nil
	})
}

// DeleteCalendar deletes the vest calendar. One that a grant uses is ErrInUse. The result's Calendar is the calendar as it was and Removed its name.
func DeleteCalendar(st *store.Store, rec *model.Record, name string, o Options) (*Result, error) {
	return editCalendars(st, rec, o, func(next *model.Record) (Result, error) {
		i := slices.IndexFunc(next.Calendars, func(c *model.VestCalendar) bool { return c.Name == name })
		if i < 0 {
			return Result{}, unknownCalendar(name)
		}
		for _, a := range next.Accounts {
			for _, g := range a.Grants {
				if g.Calendar == name {
					return Result{}, refuse(ErrInUse, "vest calendar %q is used by grant %d", name, g.ID)
				}
			}
		}
		c := next.Calendars[i]
		next.Calendars = slices.Delete(next.Calendars, i, i+1)
		return Result{Calendar: c, Removed: name}, nil
	})
}

// factError turns the failure of a store call on a fact into the operation's error.
func factError(err error, id int64) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return refuse(ErrUnknownObject, "there is no fact %d", id)
	case errors.Is(err, store.ErrInvalidEntry):
		return refuse(ErrInvalidValue, "%v", err)
	}
	return &SaveError{err}
}

// checkRates refuses a rate outside 0 to 1, or rates that add up to 100% or more.
func checkRates(w model.Withholding) error {
	for _, r := range []float64{w.Federal, w.State, w.FICA} {
		if math.IsNaN(r) || r < 0 || r >= 1 {
			return refuse(ErrInvalidValue, "a withholding rate is a fraction from 0 up to 1: %v is not", r)
		}
	}
	if w.Sum() >= 1 {
		return refuse(ErrInvalidValue, "withholding rates add up to %v: they must be under 100%%", w.Sum())
	}
	return nil
}

// checkGrant refuses a grant that cannot be: an account that is not an open equity account, a symbol or numbers that are not valid, an unknown calendar, a first vest the calendar does not have, percents that do not add up to 100, a brokerage that is not a brokerage account, shares fewer than the grant has already vested.
func checkGrant(rec *model.Record, g model.Grant) error {
	a := rec.Account(g.Account)
	if a == nil {
		return unknownAccount(g.Account)
	}
	if a.Type != model.TypeEquity {
		return refuse(ErrInvalidValue, "account %q is a %s account: a grant needs an equity account", g.Account, a.Type)
	}
	if err := checkOpenOn(a, g.Date); err != nil {
		return err
	}
	if g.Symbol == "" {
		return refuse(ErrInvalidValue, "a grant needs a stock symbol")
	}
	if !finite(g.Price) || g.Price <= 0 {
		return refuse(ErrInvalidValue, "the grant price must be positive")
	}
	if g.Shares < 1 {
		return refuse(ErrInvalidValue, "a grant needs at least one share")
	}
	cal := rec.Calendar(g.Calendar)
	if cal == nil {
		return unknownCalendar(g.Calendar)
	}
	if _, err := g.Plan(cal); err != nil {
		return refuse(ErrInvalidValue, "%v", err)
	}
	b := rec.Account(g.Brokerage)
	if b == nil {
		return unknownAccount(g.Brokerage)
	}
	if b.Type != model.TypeBrokerage {
		return refuse(ErrInvalidValue, "account %q is a %s account: proceeds go to a brokerage account", g.Brokerage, b.Type)
	}
	if err := checkRates(g.Rates); err != nil {
		return err
	}
	if v := a.VestedBy(g.ID, model.MaxDate); g.ID != 0 && g.Shares < v {
		return refuse(ErrInvalidValue, "grant %d has already vested %d shares: it cannot have %d", g.ID, v, g.Shares)
	}
	return nil
}

// checkGrantCorrection refuses a correction of a grant with recorded vests that would leave them without their planned vests: one that moves the grant date after a vest, drops a planned vest a vest fulfils, or changes the dates or percents of the plan. It is called with the corrected grant, which checkGrant has accepted.
func checkGrantCorrection(rec *model.Record, g model.Grant) error {
	old, a := rec.Grant(g.ID)
	if old == nil {
		return nil
	}
	newPlan, err := g.Plan(rec.Calendar(g.Calendar))
	if err != nil {
		return refuse(ErrInvalidValue, "%v", err)
	}
	var oldPlan []model.PlannedVest
	if cal := rec.Calendar(old.Calendar); cal != nil {
		oldPlan, _ = old.Plan(cal)
	}
	for _, v := range a.Vests {
		if v.Grant != g.ID {
			continue
		}
		if v.Date < g.Date {
			return refuse(ErrInUse, "grant %d has recorded vest %d on %v: its date cannot move after it", g.ID, v.ID, v.Date)
		}
		if v.Planned > len(newPlan) {
			return refuse(ErrInUse, "grant %d has recorded vest %d for planned vest %d: the corrected plan has only %d", g.ID, v.ID, v.Planned, len(newPlan))
		}
		if len(oldPlan) != len(newPlan) {
			return refuse(ErrInUse, "grant %d has recorded vest %d: the plan of vests cannot change; void the vests first", g.ID, v.ID)
		}
		for i := range oldPlan {
			if oldPlan[i].Date != newPlan[i].Date || oldPlan[i].Percent != newPlan[i].Percent {
				return refuse(ErrInUse, "grant %d has recorded vest %d: the plan of vests cannot change; void the vests first", g.ID, v.ID)
			}
		}
	}
	return nil
}

// GrantInput is a new grant. Enter either Shares or Value: a Value buys the whole shares of Value over Price. Rates nil takes model.DefaultWithholding. VestPercents nil takes the percents generated from YearPercents, CliffMonths and the calendar.
type GrantInput struct {
	Account      model.AccountID
	Symbol       string
	Date         model.Date
	Shares       int
	Value        float64
	Price        float64
	Calendar     string
	FirstVest    model.Date
	YearPercents []float64
	CliffMonths  int
	VestPercents []float64
	Rates        *model.Withholding
	Brokerage    model.AccountID
}

// AddGrant records a grant of shares to an equity account. See GrantInput and checkGrant for what is refused; a grant with both or neither of Shares and Value is ErrInvalidValue. The result's Grant has its ID unless the run was a dry run.
func AddGrant(st *store.Store, rec *model.Record, in GrantInput, o Options) (*Result, error) {
	g := model.Grant{Account: in.Account, Symbol: strings.ToUpper(strings.TrimSpace(in.Symbol)), Date: in.Date, Shares: in.Shares, Value: in.Value,
		Price: in.Price, Calendar: in.Calendar, FirstVest: in.FirstVest, YearPercents: slices.Clone(in.YearPercents), CliffMonths: in.CliffMonths,
		VestPercents: slices.Clone(in.VestPercents), Rates: model.DefaultWithholding, Brokerage: in.Brokerage}
	if in.Rates != nil {
		g.Rates = *in.Rates
	}
	if (in.Shares != 0) == (in.Value != 0) {
		return nil, refuse(ErrInvalidValue, "enter either the number of shares or the dollar value of the grant")
	}
	if in.Value != 0 {
		if !finite(in.Value) || in.Value <= 0 || !finite(in.Price) || in.Price <= 0 {
			return nil, refuse(ErrInvalidValue, "the value and the grant price must be positive")
		}
		g.Shares = model.SharesFor(in.Value, in.Price)
	}
	next := rec.Clone()
	cal := next.Calendar(g.Calendar)
	if cal == nil {
		return nil, unknownCalendar(g.Calendar)
	}
	if g.VestPercents == nil {
		p, err := model.GeneratePercents(cal, g.FirstVest, g.YearPercents, g.CliffMonths)
		if err != nil {
			return nil, refuse(ErrInvalidValue, "%v", err)
		}
		g.VestPercents = p
	}
	if err := checkGrant(next, g); err != nil {
		return nil, err
	}
	next.Account(g.Account).Grants = append(next.Account(g.Account).Grants, g)
	res := &Result{Record: next, Grant: &g}
	if o.DryRun {
		return res, nil
	}
	err := st.Write(nil, func(f store.FactTx) error {
		id, err := f.Add(store.FactValue{Grant: &g})
		g.ID = id
		return err
	})
	if err != nil {
		return nil, &SaveError{err}
	}
	return withReloaded(st, res, o)
}

// GrantChange is the values of a grant to change; nil leaves one as it is. A Value sets the shares to the whole shares of it over the price, and cannot be given with Shares. Changing the calendar, first vest, year percents or cliff regenerates the vest percents unless VestPercents is given too.
type GrantChange struct {
	Symbol       *string
	Date         *model.Date
	Shares       *int
	Value        *float64
	Price        *float64
	Calendar     *string
	FirstVest    *model.Date
	YearPercents []float64
	CliffMonths  *int
	VestPercents []float64
	Rates        *model.Withholding
	Brokerage    *model.AccountID
}

// storedFact returns the fact of the kind, voided or not, and ErrUnknownObject if there is none.
func storedFact(st *store.Store, id int64, kind store.FactKind) (store.Fact, error) {
	f, err := st.Fact(id)
	if err != nil {
		return f, factError(err, id)
	}
	if f.Kind != kind {
		return f, refuse(ErrUnknownObject, "fact %d is a %s, not a %s", id, f.Kind, kind)
	}
	return f, nil
}

// correctFact stores the correction (a dry run in a transaction that is rolled back) and reloads the record.
func correctFact(st *store.Store, id int64, v store.FactValue, note string, res *Result, o Options) (*Result, error) {
	var err error
	if o.DryRun {
		err = st.TryCorrectFact(id, v, note)
	} else {
		err = st.CorrectFact(id, v, note)
	}
	if err != nil {
		return nil, factError(err, id)
	}
	return withReloaded(st, res, o)
}

func voidFact(st *store.Store, id int64, note string, res *Result, o Options) (*Result, error) {
	var err error
	if o.DryRun {
		err = st.TryVoidFact(id, note)
	} else {
		err = st.VoidFact(id, note)
	}
	if err != nil {
		return nil, factError(err, id)
	}
	return withReloaded(st, res, o)
}

// CorrectGrant stores a correction of the grant with the changed values, and a note; the original stays in its history. The checks of AddGrant apply, and shares fewer than the grant has vested are refused. A correction that changes nothing is ErrNoChange. Correcting a voided grant restores it. The result has the reloaded Record unless the run was a dry run.
func CorrectGrant(st *store.Store, rec *model.Record, id int64, ch GrantChange, note string, o Options) (*Result, error) {
	f, err := storedFact(st, id, store.FactGrant)
	if err != nil {
		return nil, err
	}
	g := f.Grant.Clone()
	if ch.Shares != nil && ch.Value != nil {
		return nil, refuse(ErrInvalidValue, "change either the number of shares or the dollar value of the grant, not both")
	}
	regenerate := ch.Calendar != nil || ch.FirstVest != nil || ch.YearPercents != nil || ch.CliffMonths != nil
	if ch.Symbol != nil {
		g.Symbol = strings.ToUpper(strings.TrimSpace(*ch.Symbol))
	}
	if ch.Date != nil {
		g.Date = *ch.Date
	}
	if ch.Price != nil {
		g.Price = *ch.Price
	}
	if ch.Shares != nil {
		g.Shares, g.Value = *ch.Shares, 0
	}
	if ch.Value != nil {
		if !finite(*ch.Value) || *ch.Value <= 0 || !finite(g.Price) || g.Price <= 0 {
			return nil, refuse(ErrInvalidValue, "the value and the grant price must be positive")
		}
		g.Value, g.Shares = *ch.Value, model.SharesFor(*ch.Value, g.Price)
	} else if ch.Price != nil && g.Value != 0 && ch.Shares == nil {
		g.Shares = model.SharesFor(g.Value, g.Price)
	}
	if ch.Calendar != nil {
		g.Calendar = *ch.Calendar
	}
	if ch.FirstVest != nil {
		g.FirstVest = *ch.FirstVest
	}
	if ch.YearPercents != nil {
		g.YearPercents = slices.Clone(ch.YearPercents)
	}
	if ch.CliffMonths != nil {
		g.CliffMonths = *ch.CliffMonths
	}
	if ch.Rates != nil {
		g.Rates = *ch.Rates
	}
	if ch.Brokerage != nil {
		g.Brokerage = *ch.Brokerage
	}
	if ch.VestPercents != nil {
		g.VestPercents = slices.Clone(ch.VestPercents)
	} else if regenerate {
		cal := rec.Calendar(g.Calendar)
		if cal == nil {
			return nil, unknownCalendar(g.Calendar)
		}
		if g.VestPercents, err = model.GeneratePercents(cal, g.FirstVest, g.YearPercents, g.CliffMonths); err != nil {
			return nil, refuse(ErrInvalidValue, "%v", err)
		}
	}
	if err := checkGrant(rec, g); err != nil {
		return nil, err
	}
	if err := checkGrantCorrection(rec, g); err != nil {
		return nil, err
	}
	if reflect.DeepEqual(g, *f.Grant) && !f.Void {
		return nil, ErrNoChange
	}
	return correctFact(st, id, store.FactValue{Grant: &g}, note, &Result{Grant: &g}, o)
}

// VoidGrant stores a correction that takes the grant out of the record, with a note; CorrectGrant restores it. A grant with recorded vests is ErrInUse: void them first, because a vest without its grant would leave shares vested from nothing. A grant already voided is ErrNoChange. The result has the reloaded Record unless the run was a dry run.
func VoidGrant(st *store.Store, rec *model.Record, id int64, note string, o Options) (*Result, error) {
	f, err := storedFact(st, id, store.FactGrant)
	if err != nil {
		return nil, err
	}
	if f.Void {
		return nil, ErrNoChange
	}
	if _, a := rec.Grant(id); a != nil {
		for _, v := range a.Vests {
			if v.Grant == id {
				return nil, refuse(ErrInUse, "grant %d has recorded vest %d: void the vests first", id, v.ID)
			}
		}
	}
	return voidFact(st, id, note, &Result{Grant: f.Grant}, o)
}

// VestInput is a vest to record: the planned vest Planned (1-based) of the grant. Date, Shares and Rates nil take the planned vest's date and shares and the grant's rates. Withheld is an entered amount that replaces the rates. TxEntry is the id of an existing entry of the grant's brokerage account that deposited the proceeds; with 0 a transfer of the net proceeds from the equity account into the brokerage account is recorded.
type VestInput struct {
	Grant    int64
	Planned  int
	Date     *model.Date
	Shares   *int
	Price    float64
	Rates    *model.Withholding
	Withheld *float64
	TxEntry  int64
}

// checkVest refuses a vest that cannot be: a grant the record lacks, a vest before the grant date or on a planned vest the grant lacks or another vest already fulfils, a price or shares not positive, rates that are not valid, an entered withheld amount over the vest's value, shares beyond what the grant has left.
func checkVest(rec *model.Record, v model.RecordedVest) error {
	g, a := rec.Grant(v.Grant)
	if g == nil {
		return refuse(ErrUnknownObject, "there is no grant %d", v.Grant)
	}
	if err := checkOpenOn(a, v.Date); err != nil {
		return err
	}
	if v.Date < g.Date {
		return refuse(ErrInvalidValue, "a vest on %v is before its grant on %v", v.Date, g.Date)
	}
	cal := rec.Calendar(g.Calendar)
	if cal == nil {
		return unknownCalendar(g.Calendar)
	}
	plan, err := g.Plan(cal)
	if err != nil {
		return refuse(ErrInvalidValue, "%v", err)
	}
	if v.Planned < 1 || v.Planned > len(plan) {
		return refuse(ErrInvalidValue, "grant %d has planned vests 1 to %d", g.ID, len(plan))
	}
	if v.Shares < 1 {
		return refuse(ErrInvalidValue, "a vest needs at least one share")
	}
	if !finite(v.Price) || v.Price <= 0 {
		return refuse(ErrInvalidValue, "the vest price must be positive")
	}
	if err := checkRates(v.Rates); err != nil {
		return err
	}
	if v.Withheld != nil && (!finite(*v.Withheld) || *v.Withheld < 0 || *v.Withheld > v.Gross()) {
		return refuse(ErrInvalidValue, "the withheld amount must be from 0 to the vest's value %.2f", v.Gross())
	}
	vested := 0
	for _, o := range a.Vests {
		if o.Grant != g.ID || o.ID == v.ID && v.ID != 0 {
			continue
		}
		if o.Planned == v.Planned {
			return refuse(ErrInvalidValue, "planned vest %d of grant %d is already recorded as vest %d: correct that instead", v.Planned, g.ID, o.ID)
		}
		vested += o.Shares
	}
	if v.Shares > g.Shares-vested {
		return refuse(ErrInvalidValue, "grant %d has %d of its %d shares left to vest: %d is too many", g.ID, g.Shares-vested, g.Shares, v.Shares)
	}
	return nil
}

// checkTied refuses a deposit entry that is not a current entry of the brokerage account, or that another vest already names.
func checkTied(st *store.Store, rec *model.Record, brokerage model.AccountID, entry, self int64) error {
	e, err := st.Entry(entry)
	if err != nil {
		return entryError(err, entry)
	}
	if e.Void || e.Kind != store.KindTransaction || e.Account != brokerage {
		return refuse(ErrInvalidValue, "entry %d is not a current transaction of brokerage account %q", entry, brokerage)
	}
	for _, a := range rec.Accounts {
		for _, v := range a.Vests {
			if v.TxEntry == entry && v.ID != self {
				return refuse(ErrInvalidValue, "entry %d is already the deposit of vest %d", entry, v.ID)
			}
		}
	}
	return nil
}

// RecordVest records that a planned vest happened. It records the price observation of the stock on the date (unless one with the same price is there), and either a transfer of the net proceeds into the grant's brokerage account with the equity account as counter, or, with TxEntry, ties the vest to that existing deposit and records no second one. All of it is saved together, or none. See checkVest for what is refused; net proceeds that are not positive are refused without TxEntry. The result's Vest has its ID unless the run was a dry run; Price is the observation recorded or nil, and Entry the deposit recorded or nil.
func RecordVest(st *store.Store, rec *model.Record, in VestInput, o Options) (*Result, error) {
	next := rec.Clone()
	g, acct := next.Grant(in.Grant)
	if g == nil {
		return nil, refuse(ErrUnknownObject, "there is no grant %d", in.Grant)
	}
	cal := next.Calendar(g.Calendar)
	if cal == nil {
		return nil, unknownCalendar(g.Calendar)
	}
	plan, err := g.Plan(cal)
	if err != nil {
		return nil, refuse(ErrInvalidValue, "%v", err)
	}
	if in.Planned < 1 || in.Planned > len(plan) {
		return nil, refuse(ErrInvalidValue, "grant %d has planned vests 1 to %d", g.ID, len(plan))
	}
	p := plan[in.Planned-1]
	v := model.RecordedVest{Grant: g.ID, Planned: p.Index, Date: p.Date, Shares: p.Shares, Price: in.Price, Rates: g.Rates, Withheld: in.Withheld, TxEntry: in.TxEntry}
	if in.Date != nil {
		v.Date = *in.Date
	}
	if in.Shares != nil {
		v.Shares = *in.Shares
	}
	if in.Rates != nil {
		v.Rates = *in.Rates
		if in.Withheld != nil {
			return nil, refuse(ErrInvalidValue, "give either withholding rates or the withheld amount, not both")
		}
	}
	if err := checkVest(next, v); err != nil {
		return nil, err
	}
	res := &Result{Vest: &v}
	var deposit *store.Values
	if in.TxEntry != 0 {
		if err := checkTied(st, next, g.Brokerage, in.TxEntry, 0); err != nil {
			return nil, err
		}
	} else {
		if v.Net() <= 0 {
			return nil, refuse(ErrInvalidValue, "the net proceeds %.2f are not positive: there is nothing to deposit", v.Net())
		}
		d := store.Values{Date: v.Date, Amount: v.Net(), TxKind: model.TxTransfer, Counter: g.Account,
			Memo: fmt.Sprintf("%s vest of grant %d", g.Symbol, g.ID)}
		if err := checkTx(next, g.Brokerage, d); err != nil {
			return nil, err
		}
		if err := next.Account(g.Brokerage).AddTransaction(model.Transaction{Date: d.Date, Amount: d.Amount, Kind: d.TxKind, Counter: d.Counter, Memo: d.Memo}); err != nil {
			return nil, refuse(ErrInvalidValue, "%v", err)
		}
		deposit = &d
		res.Entry = &store.Entry{Account: g.Brokerage, Kind: store.KindTransaction, Values: d}
	}
	obs := model.PriceObservation{Symbol: g.Symbol, Date: v.Date, Price: v.Price}
	if seen := latestPriceOn(next.Prices.Symbol(g.Symbol), v.Date); seen == nil || seen.Price != v.Price {
		next.Prices.Add(obs)
		res.Price = &obs
	}
	acct.Vests = append(acct.Vests, v)
	res.Record = next
	if o.DryRun {
		return res, nil
	}
	err = st.Write(next, func(f store.FactTx) error {
		if deposit != nil {
			id, ok, err := f.NewestEntry(g.Brokerage, store.KindTransaction, *deposit)
			if err != nil || !ok {
				return fmt.Errorf("the deposit was not saved: %v", err)
			}
			v.TxEntry, res.Entry.ID = id, id
		}
		if res.Price != nil {
			id, err := f.Add(store.FactValue{Price: &obs})
			if err != nil {
				return err
			}
			obs.ID = id
		}
		id, err := f.Add(store.FactValue{Vest: &v})
		v.ID = id
		return err
	})
	if err != nil {
		return nil, &SaveError{err}
	}
	return withReloaded(st, res, o)
}

// VestChange is the values of a recorded vest to change; nil leaves one as it is. Rates and Withheld replace each other and cannot be given together. TxEntry re-ties the vest to another deposit.
type VestChange struct {
	Planned  *int
	Date     *model.Date
	Shares   *int
	Price    *float64
	Rates    *model.Withholding
	Withheld *float64
	TxEntry  *int64
}

// CorrectVest stores a correction of the recorded vest with the changed values, and a note; the original stays in its history. The checks of RecordVest apply. A correction does not touch the vest's deposit or price observation: correct those with their own operations. A correction that changes nothing is ErrNoChange. Correcting a voided vest restores it.
func CorrectVest(st *store.Store, rec *model.Record, id int64, ch VestChange, note string, o Options) (*Result, error) {
	f, err := storedFact(st, id, store.FactVest)
	if err != nil {
		return nil, err
	}
	v := f.Vest.Clone()
	if ch.Rates != nil && ch.Withheld != nil {
		return nil, refuse(ErrInvalidValue, "give either withholding rates or the withheld amount, not both")
	}
	if ch.Planned != nil {
		v.Planned = *ch.Planned
	}
	if ch.Date != nil {
		v.Date = *ch.Date
	}
	if ch.Shares != nil {
		v.Shares = *ch.Shares
	}
	if ch.Price != nil {
		v.Price = *ch.Price
	}
	if ch.Rates != nil {
		v.Rates, v.Withheld = *ch.Rates, nil
	}
	if ch.Withheld != nil {
		w := *ch.Withheld
		v.Withheld = &w
	}
	if ch.TxEntry != nil {
		v.TxEntry = *ch.TxEntry
	}
	if err := checkVest(rec, v); err != nil {
		return nil, err
	}
	if g, _ := rec.Grant(v.Grant); ch.TxEntry != nil && g != nil {
		if err := checkTied(st, rec, g.Brokerage, v.TxEntry, id); err != nil {
			return nil, err
		}
	}
	if reflect.DeepEqual(v, *f.Vest) && !f.Void {
		return nil, ErrNoChange
	}
	return correctFact(st, id, store.FactValue{Vest: &v}, note, &Result{Vest: &v}, o)
}

// VoidVest stores a correction that takes the recorded vest out of the record, with a note; CorrectVest restores it. The vest's deposit and price observation stay: void those separately. A vest already voided is ErrNoChange.
func VoidVest(st *store.Store, rec *model.Record, id int64, note string, o Options) (*Result, error) {
	f, err := storedFact(st, id, store.FactVest)
	if err != nil {
		return nil, err
	}
	if f.Void {
		return nil, ErrNoChange
	}
	return voidFact(st, id, note, &Result{Vest: f.Vest}, o)
}

// latestPriceOn returns the last of the observations (in date order) that is dated d, or nil.
func latestPriceOn(obs []model.PriceObservation, d model.Date) *model.PriceObservation {
	for i := len(obs) - 1; i >= 0; i-- {
		if obs[i].Date == d {
			return &obs[i]
		}
	}
	return nil
}

func checkPrice(p model.PriceObservation) error {
	if p.Symbol == "" {
		return refuse(ErrInvalidValue, "a price needs a stock symbol")
	}
	if !finite(p.Price) || p.Price <= 0 {
		return refuse(ErrInvalidValue, "the price must be positive")
	}
	return nil
}

// AddPrice records the price of one share of the stock on a date. A symbol is upper-cased. The latest observation of a date is the one that counts.
func AddPrice(st *store.Store, rec *model.Record, symbol string, date model.Date, price float64, o Options) (*Result, error) {
	p := model.PriceObservation{Symbol: strings.ToUpper(strings.TrimSpace(symbol)), Date: date, Price: price}
	if err := checkPrice(p); err != nil {
		return nil, err
	}
	next := rec.Clone()
	next.Prices.Add(p)
	res := &Result{Record: next, Price: &p}
	if o.DryRun {
		return res, nil
	}
	err := st.Write(nil, func(f store.FactTx) error {
		id, err := f.Add(store.FactValue{Price: &p})
		p.ID = id
		return err
	})
	if err != nil {
		return nil, &SaveError{err}
	}
	return withReloaded(st, res, o)
}

// PriceChange is the values of a price observation to change; nil leaves one as it is.
type PriceChange struct {
	Symbol *string
	Date   *model.Date
	Price  *float64
}

// CorrectPrice stores a correction of the price observation, with a note; the original stays in its history. A correction that changes nothing is ErrNoChange. Correcting a voided observation restores it.
func CorrectPrice(st *store.Store, rec *model.Record, id int64, ch PriceChange, note string, o Options) (*Result, error) {
	f, err := storedFact(st, id, store.FactPrice)
	if err != nil {
		return nil, err
	}
	p := *f.Price
	if ch.Symbol != nil {
		p.Symbol = strings.ToUpper(strings.TrimSpace(*ch.Symbol))
	}
	if ch.Date != nil {
		p.Date = *ch.Date
	}
	if ch.Price != nil {
		p.Price = *ch.Price
	}
	if err := checkPrice(p); err != nil {
		return nil, err
	}
	if p == *f.Price && !f.Void {
		return nil, ErrNoChange
	}
	return correctFact(st, id, store.FactValue{Price: &p}, note, &Result{Price: &p}, o)
}

// VoidPrice stores a correction that takes the price observation out of the record, with a note; CorrectPrice restores it. An observation already voided is ErrNoChange.
func VoidPrice(st *store.Store, rec *model.Record, id int64, note string, o Options) (*Result, error) {
	f, err := storedFact(st, id, store.FactPrice)
	if err != nil {
		return nil, err
	}
	if f.Void {
		return nil, ErrNoChange
	}
	return voidFact(st, id, note, &Result{Price: f.Price}, o)
}

// FactHistory returns the grant's, vest's or price observation's original values followed by each correction and void in the order made. An unknown id, or one of another kind, is ErrUnknownObject.
func FactHistory(st *store.Store, id int64, kind store.FactKind) ([]store.FactRevision, error) {
	if _, err := storedFact(st, id, kind); err != nil {
		return nil, err
	}
	revs, err := st.FactHistory(id)
	if err != nil {
		return nil, factError(err, id)
	}
	return revs, nil
}

// Planned vest statuses.
const (
	VestPlanned  = "planned"
	VestRecorded = "recorded"
)

// ScheduleRow is a planned vest of a grant and, if it happened, the vest that fulfils it. DateDiff and ShareDiff are the recorded vest's date (in days) and shares less the planned ones.
type ScheduleRow struct {
	model.PlannedVest
	Status    string
	Vest      *model.RecordedVest
	DateDiff  int
	ShareDiff int
}

// GrantSchedule returns the grant's planned vests, each with its status and, if recorded, how the recorded vest differs. An unknown grant is ErrUnknownObject; a calendar that no longer yields the grant's vests is ErrInvalidValue.
func GrantSchedule(rec *model.Record, id int64) (*model.Grant, []ScheduleRow, error) {
	g, a := rec.Grant(id)
	if g == nil {
		return nil, nil, refuse(ErrUnknownObject, "there is no grant %d", id)
	}
	cal := rec.Calendar(g.Calendar)
	if cal == nil {
		return nil, nil, unknownCalendar(g.Calendar)
	}
	plan, err := g.Plan(cal)
	if err != nil {
		return nil, nil, refuse(ErrInvalidValue, "%v", err)
	}
	rows := make([]ScheduleRow, len(plan))
	for i, p := range plan {
		rows[i] = ScheduleRow{PlannedVest: p, Status: VestPlanned}
		for j := range a.Vests {
			if v := &a.Vests[j]; v.Grant == id && v.Planned == p.Index {
				rows[i].Status, rows[i].Vest = VestRecorded, v
				rows[i].DateDiff, rows[i].ShareDiff = int(v.Date-p.Date), v.Shares-p.Shares
			}
		}
	}
	return g, rows, nil
}
