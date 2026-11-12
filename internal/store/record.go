// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
)

// encodeRecord turns everything in the record except the entries into document rows.
func encodeRecord(r *model.Record) ([]docRow, error) {
	var rows []docRow
	add := func(kind, key string, ordinal int, v any) error {
		body, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("store: encode %s %q: %w", kind, key, err)
		}
		rows = append(rows, docRow{kind, key, ordinal, formatCurrent, body})
		return nil
	}
	if err := add(kindRecord, "", 0, recordDoc{Today: r.Today}); err != nil {
		return nil, err
	}
	if err := add(kindBudget, "", 0, budgetDoc{Items: mapSlice(r.Budget.Items, itemDoc)}); err != nil {
		return nil, err
	}
	if err := add(kindHousehold, "", 0, householdToDoc(r.Household)); err != nil {
		return nil, err
	}
	for i, f := range r.Tax.Federal {
		if err := add(kindTaxFederal, strconv.Itoa(f.Year), i, f); err != nil {
			return nil, err
		}
	}
	for i, st := range r.Tax.State {
		if err := add(kindTaxState, st.Name+"/"+strconv.Itoa(st.Year), i, st); err != nil {
			return nil, err
		}
	}
	for i, a := range r.Accounts {
		d := accountDoc{a.ID, a.Name, a.Type, a.Tax, a.StatedRate, defaultDoc(a.Default), a.Closed, mapSlice(a.Allocation, func(h model.Holding) holdingDoc { return holdingDoc{h.Class, h.Percent} }), a.RebalanceYearly, a.OriginalAmount, a.TermMonths, a.FirstPayment, a.PaidFrom, a.Beneficiary, a.Basis}
		if err := add(kindAccount, string(a.ID), i, d); err != nil {
			return nil, err
		}
	}
	for i, a := range r.Assets {
		d := assetDoc{a.ID, a.Name, mapSlice(a.Members, memberToDoc), defaultDoc(a.Default), paramsDoc(a.Params)}
		if err := add(kindAsset, string(a.ID), i, d); err != nil {
			return nil, err
		}
	}
	for i, c := range r.Classes {
		if err := add(kindClass, c.ID, i, classDoc{c.ID, c.Name, numOrSegs(scheduleDoc(c.Return)), c.Volatility}); err != nil {
			return nil, err
		}
	}
	if len(r.Correlations) > 0 {
		if err := add(kindCorrelations, "", 0, mapSlice(r.Correlations, func(c model.Correlation) correlationDoc { return correlationDoc{c.A, c.B, c.Value} })); err != nil {
			return nil, err
		}
	}
	for name, s := range r.Globals {
		if err := add(kindGlobal, name, 0, scheduleDoc(s)); err != nil {
			return nil, err
		}
	}
	for i, c := range r.Calendars {
		if err := add(kindCalendar, c.Name, i, calendarToDoc(c)); err != nil {
			return nil, err
		}
	}
	symbols := make([]string, 0, len(r.StockGrowth))
	for s := range r.StockGrowth {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	for i, s := range symbols {
		if err := add(kindStock, s, i, scheduleDoc(r.StockGrowth[s])); err != nil {
			return nil, err
		}
	}
	vols := make([]string, 0, len(r.StockVolatility))
	for s := range r.StockVolatility {
		vols = append(vols, s)
	}
	sort.Strings(vols)
	for i, s := range vols {
		if err := add(kindStockVol, s, i, r.StockVolatility[s]); err != nil {
			return nil, err
		}
	}
	for i, p := range r.Projections {
		if err := add(kindProjection, p.Name, i, projectionToDoc(p)); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// decodeRecord rebuilds a record from document rows (in ordinal order) and the accounts' current
// entries. The record is built only through the model's Add methods, so its indexes and member
// sequence are the model's own; an error means the stored shape cannot be reproduced exactly.
func decodeRecord(rows []docRow, entries map[model.AccountID][]Entry) (*model.Record, error) {
	var (
		today     model.Date
		accts     []accountDoc
		assets    []assetDoc
		budget    budgetDoc
		classes   []classDoc
		corrs     []correlationDoc
		globals   = map[string][]segDoc{}
		projs     []projectionDoc
		cals      []calendarDoc
		stocks    = map[string][]segDoc{}
		stockVols = map[string]float64{}

		household *householdDoc
		taxes     tax.Tables
	)
	for _, row := range rows {
		var err error
		switch row.kind {
		case kindRecord:
			var d recordDoc
			err = decodeDoc(row, &d)
			today = d.Today
		case kindBudget:
			err = decodeDoc(row, &budget)
			if row.format < 6 {
				for i := range budget.Items {
					upgradeIncome(&budget.Items[i])
				}
			}
		case kindAccount:
			var d accountDoc
			err = decodeDoc(row, &d)
			accts = append(accts, d)
		case kindAsset:
			var d assetDoc
			err = decodeDoc(row, &d)
			assets = append(assets, d)
		case kindClass:
			var d classDoc
			err = decodeDoc(row, &d)
			classes = append(classes, d)
		case kindCorrelations:
			err = decodeDoc(row, &corrs)
		case kindGlobal:
			var d []segDoc
			err = decodeDoc(row, &d)
			globals[row.key] = d
		case kindCalendar:
			var d calendarDoc
			err = decodeDoc(row, &d)
			cals = append(cals, d)
		case kindStock:
			var d []segDoc
			err = decodeDoc(row, &d)
			stocks[row.key] = d
		case kindStockVol:
			var d float64
			err = decodeDoc(row, &d)
			stockVols[row.key] = d
		case kindHousehold:
			var d householdDoc
			err = decodeDoc(row, &d)
			household = &d
		case kindTaxFederal:
			var d tax.Federal
			err = decodeDoc(row, &d)
			if err == nil {
				err = d.Validate()
				if err != nil {
					err = fmt.Errorf("store: %s %q: %w", row.kind, row.key, err)
				}
			}
			taxes.Federal = append(taxes.Federal, d)
		case kindTaxState:
			var d tax.State
			err = decodeDoc(row, &d)
			if err == nil {
				err = d.Validate()
				if err != nil {
					err = fmt.Errorf("store: %s %q: %w", row.kind, row.key, err)
				}
			}
			taxes.State = append(taxes.State, d)
		case kindProjection:
			var d projectionDoc
			err = decodeDoc(row, &d)
			if row.format < 6 {
				for _, m := range d.Mods {
					if m.Item != nil {
						upgradeIncome(m.Item)
					}
				}
			}
			projs = append(projs, d)
		default:
			return nil, fmt.Errorf("store: unknown document kind %q", row.kind)
		}
		if err != nil {
			return nil, err
		}
	}

	rec := model.NewRecord(today)
	if household != nil {
		rec.Household = docHousehold(*household)
	}
	if taxes.Federal != nil || taxes.State != nil {
		rec.Tax = taxes
	}
	if budget.Items != nil {
		rec.Budget.Items = make([]model.BudgetItem, 0, len(budget.Items))
	}
	for _, d := range budget.Items {
		it, err := docItem(d)
		if err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		rec.Budget.Items = append(rec.Budget.Items, it)
	}
	for _, c := range classes {
		ret, err := docSchedule(c.Return)
		if err != nil {
			return nil, fmt.Errorf("store: class %q: %w", c.ID, err)
		}
		rec.Classes = append(rec.Classes, model.MarketClass{ID: c.ID, Name: c.Name, Return: ret, Volatility: c.Volatility})
	}
	rec.Correlations = mapSlice(corrs, func(c correlationDoc) model.Correlation { return model.Correlation{A: c.A, B: c.B, Value: c.Value} })
	for name, segs := range globals {
		s, err := docSchedule(segs)
		if err != nil {
			return nil, fmt.Errorf("store: global %q: %w", name, err)
		}
		rec.Globals[name] = s
	}
	if err := restoreAccountsAndAssets(rec, accts, assets, entries); err != nil {
		return nil, err
	}
	for sym, segs := range stocks {
		s, err := docSchedule(segs)
		if err != nil {
			return nil, fmt.Errorf("store: stock %q: %w", sym, err)
		}
		rec.StockGrowth[sym] = s
	}
	for sym, v := range stockVols {
		rec.StockVolatility[sym] = v
	}
	for _, d := range cals {
		c := docCalendar(d)
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		rec.Calendars = append(rec.Calendars, c)
	}
	for _, d := range projs {
		p, err := docProjection(d)
		if err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		if err := rec.AddProjection(p); err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
	}
	return rec, nil
}

// restoreAccountsAndAssets replays accounts, assets and members into rec so that the account and
// asset order and every Member.Seq come out as stored. All members, including each default asset's
// account member, are replayed in Seq order. AddAccount creates the default asset and numbers its
// account member, so ensure calls it when that member's turn comes; assets are created in stored
// order, so the assets before it are created first. Anything that does not fit is an error.
func restoreAccountsAndAssets(rec *model.Record, accts []accountDoc, assets []assetDoc, entries map[model.AccountID][]Entry) error {
	pos := map[model.AssetID]int{}
	for i, a := range assets {
		if _, dup := pos[a.ID]; dup {
			return fmt.Errorf("store: asset %q stored twice", a.ID)
		}
		pos[a.ID] = i
	}
	isDefault := map[model.AssetID]bool{}
	for _, a := range accts {
		id := model.DefaultAssetID(a.ID)
		if _, ok := pos[id]; !ok {
			return fmt.Errorf("store: account %q has no default asset", a.ID)
		}
		isDefault[id] = true
	}
	prefix := string(model.DefaultAssetID(""))
	for _, a := range assets {
		if strings.HasPrefix(string(a.ID), prefix) && !isDefault[a.ID] {
			return fmt.Errorf("store: default asset %q has no account", a.ID)
		}
	}

	type pending struct {
		asset int
		m     memberDoc
		acct  bool // the default asset's account member, added by AddAccount
	}
	var todo []pending
	for i, a := range assets {
		for j, m := range a.Members {
			todo = append(todo, pending{i, m, j == 0 && isDefault[a.ID]})
		}
	}
	sort.SliceStable(todo, func(i, j int) bool { return todo[i].m.Seq < todo[j].m.Seq })

	created, nextAcct, next := 0, 0, 1
	ensure := func(upTo int) error {
		for ; created <= upTo; created++ {
			ad := assets[created]
			if isDefault[ad.ID] {
				if nextAcct >= len(accts) || model.DefaultAssetID(accts[nextAcct].ID) != ad.ID {
					return fmt.Errorf("store: default asset %q is out of order with the accounts", ad.ID)
				}
				ac := accts[nextAcct]
				if len(ad.Members) == 0 || ad.Members[0].Account != ac.ID || ad.Members[0].Asset != "" ||
					ad.Members[0].Rule != nil || ad.Members[0].Seq != next {
					return fmt.Errorf("store: default asset %q: its account member is not reproducible (seq %d expected)", ad.ID, next)
				}
				acct, err := buildAccount(ac, entries[ac.ID])
				if err != nil {
					return err
				}
				if err := rec.AddAccount(acct); err != nil {
					return fmt.Errorf("store: %w", err)
				}
				nextAcct++
				next++
			} else if err := rec.AddAsset(&model.Asset{ID: ad.ID}); err != nil {
				return fmt.Errorf("store: %w", err)
			}
			a := rec.Asset(ad.ID)
			a.Name = ad.Name
			a.Default = docDefault(ad.Default)
			var err error
			if a.Params, err = docParams(ad.Params); err != nil {
				return fmt.Errorf("store: asset %q: %w", ad.ID, err)
			}
		}
		return nil
	}
	for _, p := range todo {
		if err := ensure(p.asset); err != nil {
			return err
		}
		if p.acct {
			continue // ensure called AddAccount, which numbered the member
		}
		for _, ref := range []model.AssetID{p.m.Asset, model.DefaultAssetID(p.m.Account)} {
			if p.m.Account == "" && ref != p.m.Asset {
				continue
			}
			if j, ok := pos[ref]; ok {
				if err := ensure(j); err != nil {
					return err
				}
			}
		}
		if p.m.Seq != next {
			return fmt.Errorf("store: asset %q: member seq %d cannot be reproduced (expected %d)", assets[p.asset].ID, p.m.Seq, next)
		}
		if err := rec.AddMember(assets[p.asset].ID, docMember(p.m)); err != nil {
			return fmt.Errorf("store: %w", err)
		}
		next++
	}
	return ensure(len(assets) - 1)
}

func buildAccount(d accountDoc, entries []Entry) (*model.Account, error) {
	a := &model.Account{ID: d.ID, Name: d.Name, Type: d.Type, Tax: d.Tax,
		StatedRate: d.StatedRate, Default: docDefault(d.Default), Closed: d.Closed,
		Allocation:      mapSlice(d.Allocation, func(h holdingDoc) model.Holding { return model.Holding{Class: h.Class, Percent: h.Percent} }),
		RebalanceYearly: d.RebalanceYearly,
		OriginalAmount:  d.OriginalAmount, TermMonths: d.TermMonths, FirstPayment: d.FirstPayment, PaidFrom: d.PaidFrom,
		Beneficiary: d.Beneficiary, Basis: d.Basis}
	for _, e := range entries {
		switch e.Kind {
		case KindSnapshot:
			a.AddSnapshot(e.Date, e.Amount)
		case KindTransaction:
			t := model.Transaction{Date: e.Date, Amount: e.Amount, Kind: e.TxKind, Counter: e.Counter, Memo: e.Memo}
			if err := a.AddTransaction(t); err != nil {
				return nil, fmt.Errorf("store: %w", err)
			}
		default:
			return nil, fmt.Errorf("store: entry %d has unknown kind %q", e.ID, e.Kind)
		}
	}
	return a, nil
}

// Load returns the stored record with every correction applied and voided entries left out. An empty database loads as an empty
// record. Load also marks the point in the correction history that the returned record reflects; Save's
// stale-record check is relative to the most recent Load on this Store.
func (s *Store) Load() (*model.Record, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT kind, key, ordinal, format, body FROM documents ORDER BY ordinal, kind, key`)
	if err != nil {
		return nil, err
	}
	var docs []docRow
	for rows.Next() {
		var d docRow
		if err := rows.Scan(&d.kind, &d.key, &d.ordinal, &d.format, &d.body); err != nil {
			rows.Close()
			return nil, err
		}
		docs = append(docs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	all, err := queryEntries(tx, false, ``)
	if err != nil {
		return nil, err
	}
	byAccount := map[model.AccountID][]Entry{}
	for _, e := range all {
		byAccount[e.Account] = append(byAccount[e.Account], e)
	}
	var mark int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM corrections`).Scan(&mark); err != nil {
		return nil, err
	}
	r, err := decodeRecord(docs, byAccount)
	if err != nil {
		return nil, err
	}
	defaulted := r.Today == 0
	if defaulted {
		// A stored zero (or no record document) means the date was never set: it follows the clock, and a save writes zero back.
		y, m, d := s.now().Date()
		r.Today = model.D(y, m, d)
	}
	facts, err := queryFacts(tx, false, ``)
	if err != nil {
		return nil, err
	}
	if err := attachFacts(r, facts); err != nil {
		return nil, err
	}
	linkTies(r, all)
	s.mu.Lock()
	s.loaded, s.loadMark = true, mark
	s.todayDefaulted = defaulted
	if defaulted {
		s.defaultedToday = r.Today
	}
	s.mu.Unlock()
	return r, nil
}

// linkTies sets Entry on the transaction of each tied vest's deposit, which is the one with the values of the entry the vest names. Of identical transactions the first not yet linked is the one.
func linkTies(r *model.Record, entries []Entry) {
	byID := map[int64]Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	for _, a := range r.Accounts {
		for _, v := range a.Vests {
			e, ok := byID[v.TxEntry]
			if !ok || e.Kind != KindTransaction {
				continue
			}
			b := r.Account(e.Account)
			if b == nil {
				continue
			}
			for i := range b.Transactions {
				t := &b.Transactions[i]
				if t.Entry == 0 && t.Date == e.Date && t.Amount == e.Amount && t.Kind == e.TxKind && t.Counter == e.Counter && t.Memo == e.Memo {
					t.Entry = e.ID
					break
				}
			}
		}
	}
}

// attachFacts puts the stored grants, vests and price observations into the record. A grant of an account the record lacks, or a vest of a grant it lacks, is an error.
func attachFacts(r *model.Record, facts []Fact) error {
	for _, f := range facts {
		switch f.Kind {
		case FactPrice:
			r.Prices.Add(*f.Price)
		case FactGrant:
			a := r.Account(f.Grant.Account)
			if a == nil {
				return fmt.Errorf("store: grant %d is of unknown account %q", f.ID, f.Grant.Account)
			}
			a.Grants = append(a.Grants, *f.Grant)
		}
	}
	for _, f := range facts {
		if f.Kind != FactVest {
			continue
		}
		if _, a := r.Grant(f.Vest.Grant); a != nil {
			a.Vests = append(a.Vests, *f.Vest)
		} else {
			return fmt.Errorf("store: vest %d is of grant %d, which is not stored or is voided", f.ID, f.Vest.Grant)
		}
	}
	return nil
}

// changedSinceLoad returns, for entries voided or corrected after the most recent Load (all corrected entries
// if there has been none), one Entry per revision: the original values and each correction's.
func (s *Store) changedSinceLoad(tx *sql.Tx) ([]Entry, error) {
	s.mu.Lock()
	mark := s.loadMark
	if !s.loaded {
		mark = 0
	}
	s.mu.Unlock()
	changed, err := queryEntries(tx, true, `e.id IN (SELECT entry FROM corrections WHERE id > ?)`, mark)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range changed {
		rows, err := tx.Query(`SELECT date, amount, tx_kind, counter, memo FROM entries WHERE id = ?
			UNION ALL SELECT date, amount, tx_kind, counter, memo FROM corrections WHERE entry = ?`, e.ID, e.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			r := e
			if err := rows.Scan(&r.Date, &r.Amount, &r.TxKind, &r.Counter, &r.Memo); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CheckSave runs the check Save makes before it writes: the record must encode, and Load must be able to rebuild it from what Save would write. It writes nothing.
func CheckSave(r *model.Record) error {
	_, err := checkRestorable(r)
	return err
}

// checkRestorable encodes the record and refuses one that Load could not rebuild.
func checkRestorable(r *model.Record) ([]docRow, error) {
	docs, err := encodeRecord(r)
	if err != nil {
		return nil, err
	}
	if _, err := decodeRecord(docs, nil); err != nil {
		return nil, fmt.Errorf("store: record cannot be restored: %w", err)
	}
	return docs, nil
}

// Save writes the record in one transaction. Documents are replaced; entries in the model that are
// not stored yet are appended. Voided entries count as absent, but a model entry equal to any revision of an entry voided or corrected since the most recent Load on this Store is a stale copy and fails the same way. If a stored entry (with its corrections applied) is missing from or
// changed in the model, or an account with stored entries is gone, Save writes nothing and returns
// an error matching ErrRecordChanged.
func (s *Store) Save(r *model.Record) error { return s.save(r, nil) }

func (s *Store) save(r *model.Record, then func(FactTx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if r != nil {
		if err := s.saveRecord(tx, r); err != nil {
			return err
		}
	}
	if then != nil {
		if err := then(FactTx{s, tx}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) saveRecord(tx *sql.Tx, r *model.Record) error {
	docs, err := checkRestorable(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	keepUnset := s.todayDefaulted && r.Today == s.defaultedToday
	s.mu.Unlock()
	if _, err := tx.Exec(`DELETE FROM documents`); err != nil {
		return err
	}
	for _, d := range docs {
		if d.kind == kindRecord && keepUnset {
			if d.body, err = json.Marshal(recordDoc{}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO documents (kind, key, ordinal, format, body) VALUES (?, ?, ?, ?, ?)`,
			d.kind, d.key, d.ordinal, d.format, d.body); err != nil {
			return fmt.Errorf("store: save %s %q: %w", d.kind, d.key, err)
		}
	}
	all, err := queryEntries(tx, false, ``)
	if err != nil {
		return err
	}
	byAccount := map[model.AccountID][]Entry{}
	for _, e := range all {
		byAccount[e.Account] = append(byAccount[e.Account], e)
	}
	changedEntries, err := s.changedSinceLoad(tx)
	if err != nil {
		return err
	}
	changed := map[model.AccountID][]Entry{}
	for _, e := range changedEntries {
		changed[e.Account] = append(changed[e.Account], e)
	}
	for _, a := range r.Accounts {
		if err := s.appendNew(tx, a, byAccount[a.ID], changed[a.ID]); err != nil {
			return err
		}
		delete(byAccount, a.ID)
	}
	for id := range byAccount {
		return fmt.Errorf("%w: account %q is absent from the record", ErrRecordChanged, id)
	}
	return s.syncFacts(tx, r)
}
