// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/tax"
)

// formatCurrent is the document format version this build writes. Every document row carries its own
// version, and decodeDoc dispatches on it, so a later format adds a case that reads the old shape and
// converts it. Format 2 added the modification mode and the budget item growth to format 1's shape; a
// format 1 document has neither and reads as set and zero, and the bump makes an older build refuse a
// document whose scale modifications it would otherwise silently ignore. Format 3 added the accounts a
// projection defines; a format 1 or 2 document has none and reads as an empty list. Format 4 added the close date of an account; an older document has none and reads as an open account, and the bump makes an older build refuse a document whose closed accounts it would otherwise show as open. Format 5 stores a budget item's amount and growth as segment arrays and adds the count and unit of a custom recurrence; an older document has numbers there, which read as a one-segment schedule (a zero growth as none), and the bump makes an older build refuse a document whose schedules it would otherwise misread. Format 6 adds the income type of a budget item, the household and the tax tables; a budget item of an older document has no income type, which an income item reads as wages, and a record with no tax tables is given the shipped ones when it is loaded. Format 7 stores a market class's return as a segment array with its volatility, the correlations of the classes and the allocation of an account; an older class document has a number as its return, which reads as a one-segment schedule, and the bump makes an older build refuse a document whose allocations it would otherwise ignore. Format 8 adds the yearly rebalance setting of an account; an older document has none and reads as off, and the bump makes an older build refuse a document whose setting it would otherwise drop. Format 9 adds the terms of a mortgage account and the employer match of a wages budget item; an older document has none and reads as a mortgage without terms and an item without a match, and the bump makes an older build refuse a document whose terms and matches it would otherwise drop. Format 10 adds the Social Security and FERS inputs of a household member and the benefit taxation thresholds of the federal tax tables; an older member has none and reads as without benefits, an older table borrows the shipped thresholds, and the bump makes an older build refuse a household whose benefits it would otherwise drop. Format 11 adds the retirement date, withdrawal order and Roth conversion bracket of a projection; an older projection has none and inherits them, and the bump makes an older build refuse a document whose retirement it would otherwise drop. Format 12 adds the beneficiary and contribution basis of a 529 account; an older account has neither and reads as without a beneficiary and with a basis equal to its balance, and the bump makes an older build refuse a document whose beneficiaries it would otherwise drop. Format 13 adds the volatility of a stock as a document of its own; an older record has none and every stock reads with the default, and the bump makes an older build refuse a record whose volatilities it would otherwise drop.
const formatCurrent = 13

// Document kinds.
const (
	kindRecord       = "record"
	kindAccount      = "account"
	kindAsset        = "asset"
	kindBudget       = "budget"
	kindClass        = "class"
	kindCorrelations = "correlations"
	kindGlobal       = "global"
	kindProjection   = "projection"
	kindCalendar     = "calendar"
	kindStock        = "stock"
	kindStockVol     = "stockvol"
	kindHousehold    = "household"
	kindTaxFederal   = "taxfed"
	kindTaxState     = "taxstate"
)

// The DTOs below are the stored shape of formats 1 to 7. They are separate from the model types so that
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
	StatedRate float64            `json:"stated_rate"`
	Default    []intervalDoc      `json:"default"`
	Closed     *model.Date        `json:"closed,omitempty"`
	Allocation []holdingDoc       `json:"allocation,omitempty"`
	// RebalanceYearly is the account's yearly rebalance setting.
	RebalanceYearly bool `json:"rebalance_yearly,omitempty"`
	// OriginalAmount, TermMonths, FirstPayment and PaidFrom are a mortgage's terms.
	OriginalAmount float64         `json:"original_amount,omitempty"`
	TermMonths     int             `json:"term_months,omitempty"`
	FirstPayment   *model.Date     `json:"first_payment,omitempty"`
	PaidFrom       model.AccountID `json:"paid_from,omitempty"`
	// Beneficiary and Basis are a 529's beneficiary and contribution basis.
	Beneficiary string   `json:"beneficiary,omitempty"`
	Basis       *float64 `json:"basis,omitempty"`
}

type holdingDoc struct {
	Class   string  `json:"class"`
	Percent float64 `json:"percent"`
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
	Count  int                  `json:"count,omitempty"`
	Unit   model.RecurrenceUnit `json:"unit,omitempty"`
}

// numOrSegs is a schedule that is stored as segments in format 5 and as a single number before; a number reads as one segment from MinDate.
type numOrSegs []segDoc

func (n *numOrSegs) UnmarshalJSON(b []byte) error {
	var v float64
	if err := json.Unmarshal(b, &v); err == nil {
		*n = numOrSegs{{From: model.MinDate, Value: v}}
		return nil
	}
	return json.Unmarshal(b, (*[]segDoc)(n))
}

type budgetItemDoc struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Kind       model.ItemKind  `json:"kind"`
	Amount     numOrSegs       `json:"amount"`
	Recurrence recurrenceDoc   `json:"recurrence"`
	Start      *model.Date     `json:"start"`
	End        *model.Date     `json:"end"`
	Indexed    bool            `json:"indexed"`
	Growth     numOrSegs       `json:"growth,omitempty"`
	Account    model.AccountID `json:"account"`
	From       model.AccountID `json:"from"`
	To         model.AccountID `json:"to"`
	// IncomeType is empty for an income item written before format 6, which reads as wages, and for the other kinds.
	IncomeType model.IncomeType `json:"income_type,omitempty"`
	// MatchPercent, MatchUpTo and MatchAccount are the employer match of a wages item.
	MatchPercent float64         `json:"match_percent,omitempty"`
	MatchUpTo    float64         `json:"match_up_to,omitempty"`
	MatchAccount model.AccountID `json:"match_account,omitempty"`
}

type budgetDoc struct {
	Items []budgetItemDoc `json:"items"`
}

type householdMemberDoc struct {
	Name  string     `json:"name"`
	Birth model.Date `json:"birth"`
	Role  model.Role `json:"role"`

	SSBenefit  float64 `json:"ss_benefit,omitempty"`
	SSClaimAge int     `json:"ss_claim_age,omitempty"`

	FERSHigh3         float64        `json:"fers_high3,omitempty"`
	FERSYears         int            `json:"fers_years,omitempty"`
	FERSSeparationAge int            `json:"fers_separation_age,omitempty"`
	FERSStartAge      int            `json:"fers_start_age,omitempty"`
	FERSSurvivor      model.Survivor `json:"fers_survivor,omitempty"`
}

type householdDoc struct {
	Members  []householdMemberDoc `json:"members"`
	Filing   tax.FilingStatus     `json:"filing"`
	State    string               `json:"state"`
	Checking model.AccountID      `json:"checking,omitempty"`
}

// The tax tables are stored in the shape of the tax package's types, whose JSON is also what the tax table commands read and print, so the two cannot differ.

func householdToDoc(h model.Household) householdDoc {
	return householdDoc{mapSlice(h.Members, func(m model.HouseholdMember) householdMemberDoc {
		return householdMemberDoc{m.Name, m.Birth, m.Role, m.SSBenefit, m.SSClaimAge, m.FERSHigh3, m.FERSYears, m.FERSSeparationAge, m.FERSStartAge, m.FERSSurvivor}
	}), h.Filing, h.State, h.Checking}
}

func docHousehold(d householdDoc) model.Household {
	return model.Household{Members: mapSlice(d.Members, func(m householdMemberDoc) model.HouseholdMember {
		return model.HouseholdMember{Name: m.Name, Birth: m.Birth, Role: m.Role, SSBenefit: m.SSBenefit, SSClaimAge: m.SSClaimAge,
			FERSHigh3: m.FERSHigh3, FERSYears: m.FERSYears, FERSSeparationAge: m.FERSSeparationAge, FERSStartAge: m.FERSStartAge, FERSSurvivor: m.FERSSurvivor}
	}), Filing: d.Filing, State: d.State, Checking: d.Checking}
}

type classDoc struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Return     numOrSegs `json:"return,omitempty"`
	Volatility float64   `json:"volatility"`
}

type correlationDoc struct {
	A     string  `json:"a"`
	B     string  `json:"b"`
	Value float64 `json:"value"`
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
	Grant    *focalGrantDoc `json:"grant,omitempty"`
}

type focalGrantDoc struct {
	Symbol          string          `json:"symbol"`
	Value           float64         `json:"value"`
	Date            model.Date      `json:"date"`
	Yearly          bool            `json:"yearly"`
	Last            *model.Date     `json:"last"`
	Calendar        string          `json:"calendar"`
	YearPercents    []float64       `json:"year_percents"`
	CliffMonths     int             `json:"cliff_months"`
	FirstVestMonths int             `json:"first_vest_months"`
	Federal         *float64        `json:"federal"`
	State           *float64        `json:"state"`
	FICA            *float64        `json:"fica"`
	Brokerage       model.AccountID `json:"brokerage"`
}

func focalGrantToDoc(g model.FocalGrant) focalGrantDoc {
	d := focalGrantDoc{g.Symbol, g.Value, g.Date, g.Yearly, g.Last, g.Calendar, g.YearPercents, g.CliffMonths, g.FirstVestMonths, nil, nil, nil, g.Brokerage}
	if g.Rates != nil {
		d.Federal, d.State, d.FICA = &g.Rates.Federal, &g.Rates.State, &g.Rates.FICA
	}
	return d
}

func docFocalGrant(d focalGrantDoc) model.FocalGrant {
	g := model.FocalGrant{Symbol: d.Symbol, Value: d.Value, Date: d.Date, Yearly: d.Yearly, Last: d.Last, Calendar: d.Calendar,
		YearPercents: d.YearPercents, CliffMonths: d.CliffMonths, FirstVestMonths: d.FirstVestMonths, Brokerage: d.Brokerage}
	if d.Federal != nil && d.State != nil && d.FICA != nil {
		g.Rates = &model.Withholding{Federal: *d.Federal, State: *d.State, FICA: *d.FICA}
	}
	return g.Clone()
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
	RetireOn        *model.Date       `json:"retire_on,omitempty"`
	WithdrawalOrder []model.Bucket    `json:"withdrawal_order,omitempty"`
	RothBracket     *float64          `json:"roth_bracket,omitempty"`
	AssetSettings   []assetSettingDoc `json:"asset_settings"`
	Accounts        []projAccountDoc  `json:"accounts,omitempty"`
	Mods            []modDoc          `json:"mods"`
	Shocks          []shockDoc        `json:"shocks"`
	Rebalances      []rebalanceDoc    `json:"rebalances"`
}

type vestRuleDoc struct {
	Month   int `json:"month"`
	Day     int `json:"day"`
	Nth     int `json:"nth"`
	Weekday int `json:"weekday"`
}

type overrideDoc struct {
	From model.Date `json:"from"`
	To   model.Date `json:"to"`
}

type calendarDoc struct {
	Name      string        `json:"name"`
	Rules     []vestRuleDoc `json:"rules"`
	Overrides []overrideDoc `json:"overrides"`
}

func calendarToDoc(c *model.VestCalendar) calendarDoc {
	return calendarDoc{c.Name,
		mapSlice(c.Rules, func(r model.VestRule) vestRuleDoc { return vestRuleDoc{int(r.Month), r.Day, r.Nth, int(r.Weekday)} }),
		mapSlice(c.Overrides, func(o model.DateOverride) overrideDoc { return overrideDoc{o.From, o.To} })}
}

func docCalendar(d calendarDoc) *model.VestCalendar {
	return &model.VestCalendar{Name: d.Name,
		Rules: mapSlice(d.Rules, func(r vestRuleDoc) model.VestRule {
			return model.VestRule{Month: time.Month(r.Month), Day: r.Day, Nth: r.Nth, Weekday: time.Weekday(r.Weekday)}
		}),
		Overrides: mapSlice(d.Overrides, func(o overrideDoc) model.DateOverride { return model.DateOverride{From: o.From, To: o.To} })}
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
	case 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13:
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
	return budgetItemDoc{b.ID, b.Name, b.Kind, scheduleDoc(b.Amount), recurrenceDoc{r.Kind, r.Anchor, int(r.Month), r.Day, r.Day2, r.Count, r.Unit},
		b.Start, b.End, b.Indexed, scheduleDoc(b.Growth), b.Account, b.From, b.To, b.IncomeType, b.MatchPercent, b.MatchUpTo, b.MatchAccount}
}

// upgradeIncome gives an income item of a document older than format 6 the income type it was always taxed as.
func upgradeIncome(it *budgetItemDoc) {
	if it.Kind == model.ItemIncome && it.IncomeType == "" {
		it.IncomeType = model.IncomeWages
	}
}

func docItem(d budgetItemDoc) (model.BudgetItem, error) {
	r := d.Recurrence
	amount, err := docSchedule(d.Amount)
	if err != nil {
		return model.BudgetItem{}, fmt.Errorf("budget item %q amount: %w", d.ID, err)
	}
	// A format 1 to 4 growth of zero meant no raises.
	if len(d.Growth) == 1 && d.Growth[0].From == model.MinDate && d.Growth[0].End == nil && d.Growth[0].Value == 0 {
		d.Growth = nil
	}
	growth, err := docSchedule(d.Growth)
	if err != nil {
		return model.BudgetItem{}, fmt.Errorf("budget item %q growth: %w", d.ID, err)
	}
	return model.BudgetItem{ID: d.ID, Name: d.Name, Kind: d.Kind, Amount: amount,
		Recurrence: model.Recurrence{Kind: r.Kind, Anchor: r.Anchor, Month: time.Month(r.Month), Day: r.Day, Day2: r.Day2, Count: r.Count, Unit: r.Unit},
		Start:      d.Start, End: d.End, Indexed: d.Indexed, Growth: growth, Account: d.Account, From: d.From, To: d.To, IncomeType: d.IncomeType,
		MatchPercent: d.MatchPercent, MatchUpTo: d.MatchUpTo, MatchAccount: d.MatchAccount}, nil
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
		RetireOn: p.RetireOn, WithdrawalOrder: p.WithdrawalOrder, RothBracket: p.RothBracket,
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
			if m.Grant != nil {
				g := focalGrantToDoc(m.Grant.Clone())
				d.Grant = &g
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
	p := &model.Projection{Name: d.Name, Parent: d.Parent, Start: d.Start, OverrideHistory: d.OverrideHistory,
		RetireOn: d.RetireOn, WithdrawalOrder: d.WithdrawalOrder, RothBracket: d.RothBracket}
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
			it, err := docItem(*m.Item)
			if err != nil {
				return nil, fmt.Errorf("projection %q mod %d: %w", d.Name, i, err)
			}
			mod.Item = &it
		}
		if m.Grant != nil {
			g := docFocalGrant(*m.Grant)
			mod.Grant = &g
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
