// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// formatCurrent is the document format version this build writes. Every document row carries its own
// version, and decodeDoc dispatches on it, so a later format adds a case that reads the old shape and
// converts it. Format 2 added the modification mode and the budget item growth to format 1's shape; a
// format 1 document has neither and reads as set and zero, and the bump makes an older build refuse a
// document whose scale modifications it would otherwise silently ignore. Format 3 added the accounts a
// projection defines; a format 1 or 2 document has none and reads as an empty list. Format 4 added the close date of an account; an older document has none and reads as an open account, and the bump makes an older build refuse a document whose closed accounts it would otherwise show as open.
const formatCurrent = 4

// Document kinds.
const (
	kindRecord     = "record"
	kindAccount    = "account"
	kindAsset      = "asset"
	kindBudget     = "budget"
	kindClass      = "class"
	kindGlobal     = "global"
	kindProjection = "projection"
)

// The DTOs below are the stored shape of formats 1 to 4. They are separate from the model types so that
// renaming a model field cannot change what is on disk.

type recordDoc struct {
	Today model.Date `json:"today"`
}

type segDoc struct {
	From  model.Date  `json:"from"`
	End   *model.Date `json:"end"`
	Value float64     `json:"value"`
}

type intervalDoc struct {
	From  model.Date  `json:"from"`
	To    *model.Date `json:"to"`
	Value float64     `json:"value"`
}

type accountDoc struct {
	ID         model.AccountID    `json:"id"`
	Name       string             `json:"name"`
	Type       model.AccountType  `json:"type"`
	Tax        model.TaxTreatment `json:"tax"`
	Owner      string             `json:"owner"`
	StatedRate float64            `json:"stated_rate"`
	Default    []intervalDoc      `json:"default"`
	Closed     *model.Date        `json:"closed,omitempty"`
}

type ruleDoc struct {
	Types []model.AccountType  `json:"types"`
	Tax   []model.TaxTreatment `json:"tax"`
}

type memberDoc struct {
	Seq     int             `json:"seq"`
	Account model.AccountID `json:"account"`
	Asset   model.AssetID   `json:"asset"`
	Rule    *ruleDoc        `json:"rule"`
}

type assetDoc struct {
	ID      model.AssetID       `json:"id"`
	Name    string              `json:"name"`
	Members []memberDoc         `json:"members"`
	Default []intervalDoc       `json:"default"`
	Params  map[string][]segDoc `json:"params"`
}

type recurrenceDoc struct {
	Kind   model.RecurrenceKind `json:"kind"`
	Anchor model.Date           `json:"anchor"`
	Month  int                  `json:"month"`
	Day    int                  `json:"day"`
	Day2   int                  `json:"day2"`
}

type budgetItemDoc struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Kind       model.ItemKind  `json:"kind"`
	Amount     float64         `json:"amount"`
	Recurrence recurrenceDoc   `json:"recurrence"`
	Start      *model.Date     `json:"start"`
	End        *model.Date     `json:"end"`
	Indexed    bool            `json:"indexed"`
	Growth     float64         `json:"growth,omitempty"`
	Account    model.AccountID `json:"account"`
	From       model.AccountID `json:"from"`
	To         model.AccountID `json:"to"`
}

type budgetDoc struct {
	Items []budgetItemDoc `json:"items"`
}

type classDoc struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Return float64 `json:"return"`
}

type objectRefDoc struct {
	Kind model.ObjectKind `json:"kind"`
	ID   string           `json:"id"`
}

type modDoc struct {
	Target   objectRefDoc   `json:"target"`
	Property string         `json:"property"`
	Mode     model.Mode     `json:"mode,omitempty"`
	Values   []segDoc       `json:"values"`
	Item     *budgetItemDoc `json:"item"`
}

type assetSettingDoc struct {
	Asset           model.AssetID `json:"asset"`
	Start           *model.Date   `json:"start"`
	OverrideHistory *bool         `json:"override_history"`
}

type shockDoc struct {
	Class  string     `json:"class"`
	Date   model.Date `json:"date"`
	Change float64    `json:"change"`
}

type weightDoc struct {
	Class string  `json:"class"`
	Share float64 `json:"share"`
}

type rebalanceDoc struct {
	Account    model.AccountID `json:"account"`
	Date       model.Date      `json:"date"`
	Allocation []weightDoc     `json:"allocation"`
}

type projAccountDoc struct {
	ID         model.AccountID    `json:"id"`
	Name       string             `json:"name"`
	Type       model.AccountType  `json:"type"`
	Tax        model.TaxTreatment `json:"tax"`
	StatedRate float64            `json:"stated_rate"`
	Opening    float64            `json:"opening"`
	From       model.Date         `json:"from"`
}

type projectionDoc struct {
	Name            string            `json:"name"`
	Parent          string            `json:"parent"`
	Start           *model.Date       `json:"start"`
	OverrideHistory *bool             `json:"override_history"`
	AssetSettings   []assetSettingDoc `json:"asset_settings"`
	Accounts        []projAccountDoc  `json:"accounts,omitempty"`
	Mods            []modDoc          `json:"mods"`
	Shocks          []shockDoc        `json:"shocks"`
	Rebalances      []rebalanceDoc    `json:"rebalances"`
}

// docRow is one row of the documents table.
type docRow struct {
	kind, key       string
	ordinal, format int
	body            []byte
}

// decodeDoc decodes a document body of the given format version into v, the current shape.
func decodeDoc(row docRow, v any) error {
	switch row.format {
	case 1, 2, 3, 4:
		if err := json.Unmarshal(row.body, v); err != nil {
			return fmt.Errorf("store: %s %q: %w", row.kind, row.key, err)
		}
		return nil
	}
	return fmt.Errorf("%w: %s %q has format %d, this build reads up to %d",
		ErrUnknownFormat, row.kind, row.key, row.format, formatCurrent)
}

// mapSlice converts a slice element by element, keeping nil nil so that nil and empty round-trip.
func mapSlice[S, D any](in []S, f func(S) D) []D {
	if in == nil {
		return nil
	}
	out := make([]D, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

func scheduleDoc(s model.Schedule) []segDoc {
	return mapSlice(s.Segments(), func(g model.Segment) segDoc { return segDoc{g.From, g.End, g.Value} })
}

func docSchedule(d []segDoc) (model.Schedule, error) {
	if len(d) == 0 {
		return model.Schedule{}, nil
	}
	return model.NewSchedule(mapSlice(d, func(g segDoc) model.Segment { return model.Segment{From: g.From, End: g.End, Value: g.Value} })...)
}

func paramsDoc(p map[string]model.Schedule) map[string][]segDoc {
	if p == nil {
		return nil
	}
	out := make(map[string][]segDoc, len(p))
	for k, s := range p {
		out[k] = scheduleDoc(s)
	}
	return out
}

func docParams(d map[string][]segDoc) (map[string]model.Schedule, error) {
	if d == nil {
		return nil, nil
	}
	out := make(map[string]model.Schedule, len(d))
	for k, segs := range d {
		s, err := docSchedule(segs)
		if err != nil {
			return nil, fmt.Errorf("param %q: %w", k, err)
		}
		out[k] = s
	}
	return out, nil
}

func defaultDoc(b model.DefaultBalance) []intervalDoc {
	return mapSlice(b.Intervals, func(iv model.Interval) intervalDoc { return intervalDoc{iv.From, iv.To, iv.Value} })
}

func docDefault(d []intervalDoc) model.DefaultBalance {
	return model.DefaultBalance{Intervals: mapSlice(d, func(iv intervalDoc) model.Interval {
		return model.Interval{From: iv.From, To: iv.To, Value: iv.Value}
	})}
}

func itemDoc(b model.BudgetItem) budgetItemDoc {
	r := b.Recurrence
	return budgetItemDoc{b.ID, b.Name, b.Kind, b.Amount, recurrenceDoc{r.Kind, r.Anchor, int(r.Month), r.Day, r.Day2},
		b.Start, b.End, b.Indexed, b.Growth, b.Account, b.From, b.To}
}

func docItem(d budgetItemDoc) model.BudgetItem {
	r := d.Recurrence
	return model.BudgetItem{ID: d.ID, Name: d.Name, Kind: d.Kind, Amount: d.Amount,
		Recurrence: model.Recurrence{Kind: r.Kind, Anchor: r.Anchor, Month: time.Month(r.Month), Day: r.Day, Day2: r.Day2},
		Start:      d.Start, End: d.End, Indexed: d.Indexed, Growth: d.Growth, Account: d.Account, From: d.From, To: d.To}
}

func memberToDoc(m model.Member) memberDoc {
	d := memberDoc{Seq: m.Seq, Account: m.Account, Asset: m.Asset}
	if m.Rule != nil {
		d.Rule = &ruleDoc{m.Rule.Types, m.Rule.Tax}
	}
	return d
}

func docMember(d memberDoc) model.Member {
	m := model.Member{Seq: d.Seq, Account: d.Account, Asset: d.Asset}
	if d.Rule != nil {
		m.Rule = &model.Rule{Types: d.Rule.Types, Tax: d.Rule.Tax}
	}
	return m
}

func projectionToDoc(p *model.Projection) projectionDoc {
	return projectionDoc{
		Name: p.Name, Parent: p.Parent, Start: p.Start, OverrideHistory: p.OverrideHistory,
		AssetSettings: mapSlice(p.AssetSettings, func(a model.AssetSetting) assetSettingDoc {
			return assetSettingDoc{a.Asset, a.Start, a.OverrideHistory}
		}),
		Accounts: mapSlice(p.Accounts, func(a model.ProjAccount) projAccountDoc { return projAccountDoc(a) }),
		Mods: mapSlice(p.Mods, func(m model.Modification) modDoc {
			d := modDoc{Target: objectRefDoc{m.Target.Kind, m.Target.ID}, Property: m.Property, Mode: m.Mode, Values: scheduleDoc(m.Values)}
			if m.Item != nil {
				it := itemDoc(*m.Item)
				d.Item = &it
			}
			return d
		}),
		Shocks: mapSlice(p.Shocks, func(s model.Shock) shockDoc { return shockDoc{s.Class, s.Date, s.Change} }),
		Rebalances: mapSlice(p.Rebalances, func(r model.Rebalance) rebalanceDoc {
			return rebalanceDoc{r.Account, r.Date, mapSlice(r.Allocation, func(w model.Weight) weightDoc { return weightDoc{w.Class, w.Share} })}
		}),
	}
}

func docProjection(d projectionDoc) (*model.Projection, error) {
	p := &model.Projection{Name: d.Name, Parent: d.Parent, Start: d.Start, OverrideHistory: d.OverrideHistory}
	p.AssetSettings = mapSlice(d.AssetSettings, func(a assetSettingDoc) model.AssetSetting {
		return model.AssetSetting{Asset: a.Asset, Start: a.Start, OverrideHistory: a.OverrideHistory}
	})
	p.Accounts = mapSlice(d.Accounts, func(a projAccountDoc) model.ProjAccount { return model.ProjAccount(a) })
	var err error
	if d.Mods != nil {
		p.Mods = make([]model.Modification, len(d.Mods))
	}
	for i, m := range d.Mods {
		mod := model.Modification{Target: model.ObjectRef{Kind: m.Target.Kind, ID: m.Target.ID}, Property: m.Property, Mode: m.Mode}
		if m.Mode != "" && m.Mode != model.ModeSet && m.Mode != model.ModeScale {
			return nil, fmt.Errorf("projection %q mod %d: unknown mode %q", d.Name, i, m.Mode)
		}
		if mod.Values, err = docSchedule(m.Values); err != nil {
			return nil, fmt.Errorf("projection %q mod %d: %w", d.Name, i, err)
		}
		if m.Item != nil {
			it := docItem(*m.Item)
			mod.Item = &it
		}
		p.Mods[i] = mod
	}
	p.Shocks = mapSlice(d.Shocks, func(s shockDoc) model.Shock { return model.Shock{Class: s.Class, Date: s.Date, Change: s.Change} })
	p.Rebalances = mapSlice(d.Rebalances, func(r rebalanceDoc) model.Rebalance {
		return model.Rebalance{Account: r.Account, Date: r.Date,
			Allocation: mapSlice(r.Allocation, func(w weightDoc) model.Weight { return model.Weight{Class: w.Class, Share: w.Share} })}
	})
	return p, nil
}
