// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// FactKind says what a fact is.
type FactKind string

// Fact kinds.
const (
	FactGrant FactKind = "grant"
	FactVest  FactKind = "vest"
	FactPrice FactKind = "price"
)

// FactValue is the contents of a fact: exactly one of the three is set. Each carries the fact's ID, which the store sets when it reads a fact and ignores when it writes one.
type FactValue struct {
	Grant *model.Grant
	Vest  *model.RecordedVest
	Price *model.PriceObservation
}

// Kind returns the kind of fact the value holds, or "" if it holds none or several.
func (v FactValue) Kind() FactKind {
	switch {
	case v.Grant != nil && v.Vest == nil && v.Price == nil:
		return FactGrant
	case v.Vest != nil && v.Grant == nil && v.Price == nil:
		return FactVest
	case v.Price != nil && v.Grant == nil && v.Vest == nil:
		return FactPrice
	}
	return ""
}

// Fact is a stored grant, recorded vest or price observation with its current, corrected values. Facts are append-only like entries: a correction supersedes a fact and a void takes it out of the record, and the original and every correction stay in its history.
type Fact struct {
	ID   int64
	Kind FactKind
	FactValue
	// Void is set for a voided fact, which only Fact and FactsWithVoided return.
	Void bool
}

// FactRevision is one version of a fact: the original, then each correction. Note is empty for the original, and Void is set for a correction that voided the fact.
type FactRevision struct {
	FactValue
	MadeAt time.Time
	Note   string
	Void   bool
}

type grantDoc struct {
	Account      model.AccountID `json:"account"`
	Symbol       string          `json:"symbol"`
	Date         model.Date      `json:"date"`
	Shares       int             `json:"shares"`
	Price        float64         `json:"price"`
	Value        float64         `json:"value"`
	Calendar     string          `json:"calendar"`
	FirstVest    model.Date      `json:"first_vest"`
	YearPercents []float64       `json:"year_percents"`
	CliffMonths  int             `json:"cliff_months"`
	VestPercents []float64       `json:"vest_percents"`
	Federal      float64         `json:"federal"`
	State        float64         `json:"state"`
	FICA         float64         `json:"fica"`
	Brokerage    model.AccountID `json:"brokerage"`
}

type vestDoc struct {
	Grant    int64      `json:"grant"`
	Planned  int        `json:"planned"`
	Date     model.Date `json:"date"`
	Shares   int        `json:"shares"`
	Price    float64    `json:"price"`
	Federal  float64    `json:"federal"`
	State    float64    `json:"state"`
	FICA     float64    `json:"fica"`
	Withheld *float64   `json:"withheld"`
	TxEntry  int64      `json:"tx_entry"`
}

type priceDoc struct {
	Symbol string     `json:"symbol"`
	Date   model.Date `json:"date"`
	Price  float64    `json:"price"`
}

func encodeFact(v FactValue) ([]byte, error) {
	switch v.Kind() {
	case FactGrant:
		g := v.Grant
		return json.Marshal(grantDoc{g.Account, g.Symbol, g.Date, g.Shares, g.Price, g.Value, g.Calendar, g.FirstVest, g.YearPercents,
			g.CliffMonths, g.VestPercents, g.Rates.Federal, g.Rates.State, g.Rates.FICA, g.Brokerage})
	case FactVest:
		x := v.Vest
		return json.Marshal(vestDoc{x.Grant, x.Planned, x.Date, x.Shares, x.Price, x.Rates.Federal, x.Rates.State, x.Rates.FICA, x.Withheld, x.TxEntry})
	case FactPrice:
		p := v.Price
		return json.Marshal(priceDoc{p.Symbol, p.Date, p.Price})
	}
	return nil, fmt.Errorf("%w: a fact holds exactly one of a grant, a vest and a price", ErrInvalidEntry)
}

func decodeFact(kind FactKind, id int64, body []byte) (FactValue, error) {
	var err error
	switch kind {
	case FactGrant:
		var d grantDoc
		if err = json.Unmarshal(body, &d); err == nil {
			return FactValue{Grant: &model.Grant{ID: id, Account: d.Account, Symbol: d.Symbol, Date: d.Date, Shares: d.Shares, Price: d.Price, Value: d.Value,
				Calendar: d.Calendar, FirstVest: d.FirstVest, YearPercents: d.YearPercents, CliffMonths: d.CliffMonths, VestPercents: d.VestPercents,
				Rates: model.Withholding{Federal: d.Federal, State: d.State, FICA: d.FICA}, Brokerage: d.Brokerage}}, nil
		}
	case FactVest:
		var d vestDoc
		if err = json.Unmarshal(body, &d); err == nil {
			return FactValue{Vest: &model.RecordedVest{ID: id, Grant: d.Grant, Planned: d.Planned, Date: d.Date, Shares: d.Shares, Price: d.Price,
				Rates: model.Withholding{Federal: d.Federal, State: d.State, FICA: d.FICA}, Withheld: d.Withheld, TxEntry: d.TxEntry}}, nil
		}
	case FactPrice:
		var d priceDoc
		if err = json.Unmarshal(body, &d); err == nil {
			return FactValue{Price: &model.PriceObservation{ID: id, Symbol: d.Symbol, Date: d.Date, Price: d.Price}}, nil
		}
	default:
		err = errors.New("unknown kind")
	}
	return FactValue{}, fmt.Errorf("store: %s fact %d: %w", kind, id, err)
}

const currentFacts = `SELECT f.id, f.kind, COALESCE(c.body, f.body), COALESCE(c.void, 0)
	FROM facts f LEFT JOIN fact_corrections c ON c.id = (SELECT MAX(id) FROM fact_corrections WHERE fact = f.id)`

// queryFacts returns the facts matching the condition (an SQL expression, which may be empty), in id order. Voided facts are left out unless withVoided is set.
func queryFacts(q querier, withVoided bool, cond string, args ...any) ([]Fact, error) {
	where := ""
	if !withVoided {
		where = "COALESCE(c.void, 0) = 0"
	}
	if cond != "" {
		if where != "" {
			where += " AND "
		}
		where += cond
	}
	if where != "" {
		where = "WHERE " + where
	}
	rows, err := q.Query(currentFacts+" "+where+" ORDER BY f.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type raw struct {
		f    Fact
		body []byte
	}
	var raws []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.f.ID, &r.f.Kind, &r.body, &r.f.Void); err != nil {
			return nil, err
		}
		raws = append(raws, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []Fact
	for _, r := range raws {
		v, err := decodeFact(r.f.Kind, r.f.ID, r.body)
		if err != nil {
			return nil, err
		}
		r.f.FactValue = v
		out = append(out, r.f)
	}
	return out, nil
}

// Facts returns the facts of the kind in the order they were stored, with their current values. Voided facts are left out.
func (s *Store) Facts(kind FactKind) ([]Fact, error) {
	return queryFacts(s.db, false, `f.kind = ?`, string(kind))
}

// FactsWithVoided is Facts with the voided facts included (Void is set).
func (s *Store) FactsWithVoided(kind FactKind) ([]Fact, error) {
	return queryFacts(s.db, true, `f.kind = ?`, string(kind))
}

// Fact returns the fact with its current values, a voided fact included (Void is set). It returns ErrNotFound for an unknown id.
func (s *Store) Fact(id int64) (Fact, error) {
	fs, err := queryFacts(s.db, true, `f.id = ?`, id)
	if err != nil {
		return Fact{}, err
	}
	if len(fs) == 0 {
		return Fact{}, fmt.Errorf("%w: fact %d", ErrNotFound, id)
	}
	return fs[0], nil
}

// CorrectFact stores a correction of the fact: from now on it has the new values, which must be of its kind (ErrInvalidEntry otherwise). The original and earlier corrections stay stored. Correcting a voided fact restores it.
func (s *Store) CorrectFact(id int64, v FactValue, note string) error {
	return s.withTx(true, func(tx *sql.Tx) error { return s.correctFact(tx, id, v, note) })
}

// TryCorrectFact runs CorrectFact, with the same checks, in a transaction that is rolled back.
func (s *Store) TryCorrectFact(id int64, v FactValue, note string) error {
	return s.withTx(false, func(tx *sql.Tx) error { return s.correctFact(tx, id, v, note) })
}

func (s *Store) correctFact(tx *sql.Tx, id int64, v FactValue, note string) error {
	fs, err := queryFacts(tx, true, `f.id = ?`, id)
	if err != nil {
		return err
	}
	if len(fs) == 0 {
		return fmt.Errorf("%w: fact %d", ErrNotFound, id)
	}
	if v.Kind() != fs[0].Kind {
		return fmt.Errorf("%w: fact %d is a %s", ErrInvalidEntry, id, fs[0].Kind)
	}
	body, err := encodeFact(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO fact_corrections (fact, body, note, made_at) VALUES (?, ?, ?, ?)`, id, body, note, s.now().UnixNano())
	return err
}

// VoidFact stores a correction that takes the fact out of the record: Load and Facts leave it out, while the fact and its history stay stored. A later CorrectFact restores it.
func (s *Store) VoidFact(id int64, note string) error {
	return s.withTx(true, func(tx *sql.Tx) error { return s.voidFact(tx, id, note) })
}

// TryVoidFact runs VoidFact, with the same checks, in a transaction that is rolled back.
func (s *Store) TryVoidFact(id int64, note string) error {
	return s.withTx(false, func(tx *sql.Tx) error { return s.voidFact(tx, id, note) })
}

func (s *Store) voidFact(tx *sql.Tx, id int64, note string) error {
	fs, err := queryFacts(tx, true, `f.id = ?`, id)
	if err != nil {
		return err
	}
	if len(fs) == 0 {
		return fmt.Errorf("%w: fact %d", ErrNotFound, id)
	}
	body, err := encodeFact(fs[0].FactValue)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO fact_corrections (fact, body, note, void, made_at) VALUES (?, ?, ?, 1, ?)`, id, body, note, s.now().UnixNano())
	return err
}

// FactHistory returns the fact's original values followed by each correction, including voids, in the order made.
func (s *Store) FactHistory(id int64) ([]FactRevision, error) {
	f, err := s.Fact(id)
	if err != nil {
		return nil, err
	}
	var revs []FactRevision
	read := func(query string) error {
		rows, err := s.db.Query(query, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r FactRevision
			var body []byte
			var at int64
			if err := rows.Scan(&body, &r.Note, &r.Void, &at); err != nil {
				return err
			}
			if r.FactValue, err = decodeFact(f.Kind, id, body); err != nil {
				return err
			}
			r.MadeAt = time.Unix(0, at).UTC()
			revs = append(revs, r)
		}
		return rows.Err()
	}
	if err := read(`SELECT body, '', 0, made_at FROM facts WHERE id = ?`); err != nil {
		return nil, err
	}
	if err := read(`SELECT body, note, void, made_at FROM fact_corrections WHERE fact = ? ORDER BY id`); err != nil {
		return nil, err
	}
	return revs, nil
}

// FactTx adds facts and reads the entries inside the transaction of a Write.
type FactTx struct {
	s  *Store
	tx *sql.Tx
}

// Add stores a new fact and returns its id.
func (f FactTx) Add(v FactValue) (int64, error) {
	body, err := encodeFact(v)
	if err != nil {
		return 0, err
	}
	res, err := f.tx.Exec(`INSERT INTO facts (kind, body, made_at) VALUES (?, ?, ?)`, string(v.Kind()), body, f.s.now().UnixNano())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// NewestEntry returns the id of the newest current entry of the account with these values, which is the entry a Write just appended, and false if there is none.
func (f FactTx) NewestEntry(account model.AccountID, kind EntryKind, v Values) (int64, bool, error) {
	es, err := queryEntries(f.tx, false, `e.account = ?`, string(account))
	if err != nil {
		return 0, false, err
	}
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].Kind == kind && es[i].Values == v {
			return es[i].ID, true, nil
		}
	}
	return 0, false, nil
}

// Write is Save, and then, in the same transaction, runs then, which may add facts: either all of it is stored or none. A nil record saves no record, and a nil then adds no facts.
func (s *Store) Write(r *model.Record, then func(FactTx) error) error {
	return s.save(r, then)
}

// recordFacts returns the grants, vests and prices of the record that have an id, in id order. One without an id is not stored yet and Save leaves it alone: it is added with FactTx.Add.
func recordFacts(r *model.Record) []Fact {
	var out []Fact
	for _, a := range r.Accounts {
		for i := range a.Grants {
			if g := a.Grants[i]; g.ID != 0 {
				out = append(out, Fact{ID: g.ID, Kind: FactGrant, FactValue: FactValue{Grant: &g}})
			}
		}
		for i := range a.Vests {
			if v := a.Vests[i]; v.ID != 0 {
				out = append(out, Fact{ID: v.ID, Kind: FactVest, FactValue: FactValue{Vest: &v}})
			}
		}
	}
	for _, p := range r.Prices.All() {
		if p.ID != 0 {
			out = append(out, Fact{ID: p.ID, Kind: FactPrice, FactValue: FactValue{Price: &p}})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// syncFacts stores the record's facts that are not stored yet, under the ids the record gives them, and reports ErrRecordChanged if a stored fact (with its corrections applied) is missing from or differs in the record, or the record has a fact that is stored but voided. Changes to stored facts go through CorrectFact and VoidFact.
func (s *Store) syncFacts(tx *sql.Tx, r *model.Record) error {
	stored, err := queryFacts(tx, false, ``)
	if err != nil {
		return err
	}
	current := map[int64]Fact{}
	for _, f := range stored {
		current[f.ID] = f
	}
	for _, f := range recordFacts(r) {
		body, err := encodeFact(f.FactValue)
		if err != nil {
			return err
		}
		if c, ok := current[f.ID]; ok {
			old, err := encodeFact(c.FactValue)
			if err != nil {
				return err
			}
			if c.Kind != f.Kind || !bytes.Equal(old, body) {
				return fmt.Errorf("%w: %s %d", ErrRecordChanged, f.Kind, f.ID)
			}
			delete(current, f.ID)
			continue
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM facts WHERE id = ?`, f.ID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: %s %d is voided or corrected since the record was loaded", ErrRecordChanged, f.Kind, f.ID)
		}
		if _, err := tx.Exec(`INSERT INTO facts (id, kind, body, made_at) VALUES (?, ?, ?, ?)`, f.ID, string(f.Kind), body, s.now().UnixNano()); err != nil {
			return err
		}
	}
	for id, f := range current {
		return fmt.Errorf("%w: %s %d is absent from the record", ErrRecordChanged, f.Kind, id)
	}
	return nil
}
