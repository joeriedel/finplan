// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// The retirement settings of a projection: when wages stop, the order the household then draws on its accounts and the federal bracket its yearly Roth conversion fills to. A setting a projection leaves empty is inherited from the projections it builds on.

// inheritedRetirement returns the settings the projection inherits from the projections it builds on, the nearest one that sets each winning: no date and no bracket (nil), and the default order, if none does.
func inheritedRetirement(rec *model.Record, p *model.Projection) (retire *model.Date, order []model.Bucket, bracket *float64) {
	order = model.DefaultWithdrawalOrder()
	chain, err := rec.Chain(p.Name)
	if err != nil || len(chain) == 0 {
		return
	}
	for _, q := range chain[:len(chain)-1] {
		if q.RetireOn != nil {
			retire = q.RetireOn
		}
		if q.WithdrawalOrder != nil {
			order = q.WithdrawalOrder
		}
		if q.RothBracket != nil {
			bracket = q.RothBracket
		}
	}
	return
}

// bucketText names a bucket for the user.
func bucketText(b model.Bucket) string {
	switch b {
	case model.BucketTaxable:
		return "Taxable"
	case model.BucketDeferred:
		return "Tax-deferred"
	}
	return "Roth"
}

// orderText is the withdrawal order as the user reads it, "Taxable, then Tax-deferred, then Roth".
func orderText(order []model.Bucket) string {
	names := make([]string, len(order))
	for i, b := range order {
		names[i] = bucketText(b)
	}
	return strings.Join(names, ", then ")
}

// withdrawalOrders are the six orders the user can pick.
func withdrawalOrders() [][]model.Bucket {
	var out [][]model.Bucket
	for _, a := range model.DefaultWithdrawalOrder() {
		for _, b := range model.DefaultWithdrawalOrder() {
			for _, c := range model.DefaultWithdrawalOrder() {
				if o := []model.Bucket{a, b, c}; model.ValidWithdrawalOrder(o) {
					out = append(out, o)
				}
			}
		}
	}
	return out
}

// parseRetireOn reads the retirement date field: empty inherits.
func parseRetireOn(text string) (*model.Date, error) {
	if text == "" {
		return nil, nil
	}
	d, err := parseDate(text)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// parseRothBracket reads the Roth conversion bracket field as a percentage, 22 or 22%: empty inherits.
func parseRothBracket(text string) (*float64, error) {
	if text == "" {
		return nil, nil
	}
	v, err := parseValue(model.PropInflation, model.ModeSet, text)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// retirement draws the projection's three retirement settings, a row each below the inflation.
func (t *projTab) retirement(p *model.Projection, w float32) {
	retire, order, bracket := inheritedRetirement(t.a.rec, p)

	t.label("Retire on")
	hint := "inherit: none"
	if retire != nil {
		hint = "inherit: " + retire.String()
	}
	text := ""
	if p.RetireOn != nil {
		text = p.RetireOn.String()
	}
	if input, done := t.field("retire", hint, text, w); done {
		t.commit("retire", func() {
			on, err := parseRetireOn(strings.TrimSpace(input))
			if err != nil {
				t.setErr("retire", err.Error())
				return
			}
			t.a.edit("set retirement date", func() (*ops.Result, error) {
				return ops.SetRetirement(t.a.st, t.a.rec, p.Name, on, ops.Options{})
			})
		})
	}
	if imgui.IsItemHovered() {
		tooltipText("Wages and the saving into accounts stop on this date, and expenses are then paid from the accounts in the order below.")
	}

	t.label("Draw from")
	shown := "Inherit: " + orderText(order)
	if p.WithdrawalOrder != nil {
		shown = orderText(p.WithdrawalOrder)
	}
	imgui.SetNextItemWidth(w)
	if imgui.BeginComboV("##order", shown, 0) {
		if imgui.SelectableBoolV("Inherit: "+orderText(order), p.WithdrawalOrder == nil, 0, imgui.NewVec2(0, 0)) && p.WithdrawalOrder != nil {
			t.setOrder(p.Name, nil)
		}
		for _, o := range withdrawalOrders() {
			if imgui.SelectableBoolV(orderText(o), slices.Equal(o, p.WithdrawalOrder), 0, imgui.NewVec2(0, 0)) && !slices.Equal(o, p.WithdrawalOrder) {
				t.setOrder(p.Name, o)
			}
		}
		imgui.EndCombo()
	}

	t.label("Roth conversion")
	hint = "inherit: none"
	if bracket != nil {
		hint = "inherit: " + formatValue(model.PropInflation, model.ModeSet, *bracket)
	}
	text = ""
	if p.RothBracket != nil {
		text = formatValue(model.PropInflation, model.ModeSet, *p.RothBracket)
	}
	if input, done := t.field("bracket", hint, text, w); done {
		t.commit("bracket", func() {
			rate, err := parseRothBracket(strings.TrimSpace(input))
			if err != nil {
				t.setErr("bracket", err.Error())
				return
			}
			if rate != nil && !t.a.rec.Tax.HasBracketRate(*rate) {
				t.setErr("bracket", "The tax tables have no federal bracket at that rate.")
				return
			}
			t.a.edit("set Roth conversion bracket", func() (*ops.Result, error) {
				return ops.SetRothBracket(t.a.st, t.a.rec, p.Name, rate, ops.Options{})
			})
		})
	}
	if imgui.IsItemHovered() {
		tooltipText("Each year, tax-deferred money is moved to a Roth IRA up to the top of this federal bracket, after the year's other income, and taxed as income.")
	}
}

func (t *projTab) setOrder(proj string, order []model.Bucket) {
	t.a.edit("set withdrawal order", func() (*ops.Result, error) {
		return ops.SetWithdrawalOrder(t.a.st, t.a.rec, proj, order, ops.Options{})
	})
}
