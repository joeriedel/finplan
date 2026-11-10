// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
)

// isDefaultAsset reports whether the asset is the default asset of an account, which follows its account and is not edited on the Assets tab.
func isDefaultAsset(id model.AssetID) bool {
	return strings.HasPrefix(string(id), string(model.DefaultAssetID("")))
}

// assetRow is what the Assets tab tree shows of one asset: its balance today and its members.
type assetRow struct {
	Asset *model.Asset
	// Balance is the asset's balance on the record's today under reality.
	Balance float64
	// Err says why the asset could not be resolved (a cycle), else "".
	Err     string
	Members []memberRow
}

// memberRow is what the tree and the member list show of one member of an asset.
type memberRow struct {
	Member model.Member
	// Label names the member; Detail says what a rule member matches.
	Label, Detail string
	// Balance is the member's balance today: for a rule, the sum of the accounts it matches.
	Balance float64
	// Problem says why the member is left out of the asset's total, else "": it would count an account twice.
	Problem string
	// Child holds the members of an asset member; it is nil for other members and for an asset already being shown above it.
	Child *assetRow
}

// todayBalance returns the balance of the asset on the record's today under reality, 0 if the engine fails.
func todayBalance(rec *model.Record, id model.AssetID) float64 {
	res, err := engine.Balances(rec, "", id, []model.Date{rec.Today})
	if err != nil {
		return 0
	}
	return res.Samples[0].Amount
}

// accountName returns the name of the account, else its id.
func accountName(rec *model.Record, id model.AccountID) string {
	if a := rec.Account(id); a != nil {
		return a.Name
	}
	return string(id)
}

// ruleLabel describes a rule: what it selects by.
func ruleLabel(r model.Rule) string {
	var parts []string
	if len(r.Types) > 0 {
		parts = append(parts, "types "+joinStrings(r.Types))
	}
	if len(r.Tax) > 0 {
		parts = append(parts, "tax "+joinStrings(r.Tax))
	}
	if len(parts) == 0 {
		return "Rule: all accounts"
	}
	return "Rule: " + strings.Join(parts, ", ")
}

func joinStrings[T ~string](vs []T) string {
	s := make([]string, len(vs))
	for i, v := range vs {
		s[i] = string(v)
	}
	return strings.Join(s, ", ")
}

// memberLabel names a member.
func memberLabel(rec *model.Record, m model.Member) string {
	switch {
	case m.Asset != "":
		return assetLabel(rec, m.Asset)
	case m.Rule != nil:
		return ruleLabel(*m.Rule)
	}
	return accountName(rec, m.Account)
}

// overlapProblem says why the overlap left a member out, naming the accounts it would have counted twice and the member that counts them.
func overlapProblem(rec *model.Record, a *model.Asset, o model.Overlap) string {
	names := make([]string, len(o.Duplicates))
	for i, id := range o.Duplicates {
		names[i] = accountName(rec, id)
	}
	against := fmt.Sprintf("member %d", o.Against)
	if i := slices.IndexFunc(a.Members, func(m model.Member) bool { return m.Seq == o.Against }); i >= 0 {
		against = memberLabel(rec, a.Members[i])
	}
	return fmt.Sprintf("Left out of the total: %s already counted by %s. Remove one of the two.", strings.Join(names, ", "), against)
}

// buildAssets returns a row for each asset that is not an account's default asset, in record order, with the members nested.
func buildAssets(rec *model.Record) []*assetRow {
	resolved := map[model.AssetID]*model.Resolution{}
	var rows []*assetRow
	for _, a := range rec.Assets {
		if !isDefaultAsset(a.ID) {
			rows = append(rows, buildAsset(rec, a, resolved, nil))
		}
	}
	return rows
}

// buildAsset makes the row of one asset. shown are the assets being shown above it, which it does not nest again, so a cycle ends in a row with an error.
func buildAsset(rec *model.Record, a *model.Asset, resolved map[model.AssetID]*model.Resolution, shown []model.AssetID) *assetRow {
	row := &assetRow{Asset: a, Balance: todayBalance(rec, a.ID)}
	res, ok := resolved[a.ID]
	if !ok {
		var err error
		if res, err = rec.Resolve(a.ID); err != nil {
			row.Err = err.Error()
			res = nil
		}
		resolved[a.ID] = res
	} else if res == nil {
		row.Err = "the asset contains itself"
	}
	shown = append(shown[:len(shown):len(shown)], a.ID)
	members := slices.Clone(a.Members)
	slices.SortStableFunc(members, func(x, y model.Member) int { return x.Seq - y.Seq })
	for _, m := range members {
		mr := memberRow{Member: m, Label: memberLabel(rec, m)}
		switch {
		case m.Asset != "":
			mr.Balance = todayBalance(rec, m.Asset)
			if child := rec.Asset(m.Asset); child != nil && !slices.Contains(shown, m.Asset) {
				mr.Child = buildAsset(rec, child, resolved, shown)
			}
		case m.Rule != nil:
			var names []string
			for _, acct := range rec.Accounts {
				if m.Rule.Matches(acct) {
					names = append(names, acct.Name)
					mr.Balance += todayBalance(rec, model.DefaultAssetID(acct.ID))
				}
			}
			mr.Detail = "matches " + strings.Join(names, ", ")
			if len(names) == 0 {
				mr.Detail = "matches no account"
			}
		default:
			mr.Balance = todayBalance(rec, model.DefaultAssetID(m.Account))
		}
		if res != nil {
			if o, bad := res.Invalid(a.ID, m.Seq); bad {
				mr.Problem = overlapProblem(rec, a, o)
			}
		}
		row.Members = append(row.Members, mr)
	}
	return row
}

// chartSeg is a run of chart points of one status; it ends on the first point of the next run, so the runs join.
type chartSeg struct {
	Status engine.Status
	Pts    []chartPt
}

type chartPt struct {
	Date   model.Date
	Amount float64
}

// assetChart is the data of the small chart of one asset: its value under the base projection, drawn in runs of actual, projected and default values, and the coverage of its actual data.
type assetChart struct {
	From, To model.Date
	Segs     []chartSeg
	// Coverage is the union of the data spans of the accounts under the asset; LastActual is the end of the last span.
	Coverage   []model.Span
	LastActual model.Date
	HasActual  bool
	Min, Max   float64
	Today      model.Date
	Err        string
}

const (
	chartBackYears    = 1
	chartForwardYears = 10
	chartSamples      = 160
)

// buildChart returns the chart of the asset under the record's first projection, the base one, from a year before its first actual data (a year before today without any) to ten years after today. A failure to resolve is in Err.
func buildChart(rec *model.Record, id model.AssetID) *assetChart {
	c := &assetChart{Today: rec.Today}
	res, err := rec.Resolve(id)
	if err != nil {
		c.Err = err.Error()
		return c
	}
	c.Coverage = res.Coverage()
	c.LastActual, c.HasActual = res.LastActual()
	c.From = rec.Today.Add(-365 * chartBackYears)
	if len(c.Coverage) > 0 {
		c.From = min(c.From, c.Coverage[0].From.Add(-30))
	}
	c.To = rec.Today.Add(365 * chartForwardYears)
	proj := ""
	if len(rec.Projections) > 0 {
		proj = rec.Projections[0].Name
	}
	grid := graph.Grid{Step: max(1, int(c.To-c.From)/chartSamples), First: c.From, Last: c.To}
	bal, err := engine.Balances(rec, proj, id, grid.Times())
	if err != nil {
		c.Err = err.Error()
		return c
	}
	for i, s := range bal.Samples {
		if i == 0 || s.Status != c.Segs[len(c.Segs)-1].Status {
			if i > 0 {
				last := &c.Segs[len(c.Segs)-1]
				last.Pts = append(last.Pts, chartPt{s.Date, s.Amount})
			}
			c.Segs = append(c.Segs, chartSeg{Status: s.Status})
		}
		last := &c.Segs[len(c.Segs)-1]
		last.Pts = append(last.Pts, chartPt{s.Date, s.Amount})
		if i == 0 {
			c.Min, c.Max = s.Amount, s.Amount
		}
		c.Min, c.Max = min(c.Min, s.Amount), max(c.Max, s.Amount)
	}
	return c
}
