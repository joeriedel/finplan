// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// allocTexts is the text of each class's percent field for the account: its allocation's percents, blank for a class it does not hold.
func allocTexts(acct *model.Account) map[string]string {
	texts := map[string]string{}
	for _, h := range acct.Allocation {
		texts[h.Class] = trimFloat(h.Percent)
	}
	return texts
}

// allocFromTexts reads the percent fields as an allocation in the record's class order; a blank field is a class not held.
func allocFromTexts(rec *model.Record, texts map[string]string) ([]model.Holding, error) {
	var out []model.Holding
	for _, c := range rec.Classes {
		s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(texts[c.ID]), "%"))
		if s == "" {
			continue
		}
		p, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(p) || math.IsInf(p, 0) {
			return nil, fmt.Errorf("%q is not a percent of %s", s, c.Name)
		}
		out = append(out, model.Holding{Class: c.ID, Percent: p})
	}
	return out, nil
}

// allocProblem says why the percents typed cannot be the account's allocation, or "" if they can.
func allocProblem(rec *model.Record, acct *model.Account, texts map[string]string) string {
	alloc, err := allocFromTexts(rec, texts)
	if err != nil {
		return err.Error()
	}
	if len(alloc) == 0 {
		return "enter the percent of each class to hold, such as 60 and 40, or clear the allocation"
	}
	if err := rec.CheckAllocation(acct.Type, alloc); err != nil {
		return err.Error()
	}
	return ""
}

// allocSum is the sum of the percents typed, ignoring fields that are not numbers.
func allocSum(rec *model.Record, texts map[string]string) float64 {
	sum := 0.0
	for _, c := range rec.Classes {
		if p, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(texts[c.ID]), "%")), 64); err == nil {
			sum += p
		}
	}
	return sum
}

// allocation draws the allocation editor of an investment account: a percent per class, the suggested default, clearing it and the yearly rebalance.
func (t *acctTab) allocation(acct *model.Account) {
	a, em := t.a, imgui.FontSize()
	if t.alloc == nil || t.allocFor != acct.ID {
		t.alloc, t.allocFor = allocTexts(acct), acct.ID
	}
	imgui.Spacing()
	imgui.SeparatorText("Allocation")
	if len(acct.Allocation) == 0 {
		disabledText("No allocation: the account grows at its rate per year. With one it grows by class instead.")
	} else {
		disabledText("The account grows by class, each at its own return; money paid in is split by these percents.")
	}
	for _, c := range a.rec.Classes {
		t.label(c.Name)
		text := t.alloc[c.ID]
		imgui.SetNextItemWidth(em * 8)
		if imgui.InputTextWithHint("##alloc"+c.ID, "percent, e.g. 60", &text, 0, nil) {
			t.alloc[c.ID] = text
		}
	}
	t.label("Sum")
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(trimFloat(allocSum(a.rec, t.alloc)) + "%")
	formButton("Set allocation##allocset", allocProblem(a.rec, acct, t.alloc), allocUntouched(a.rec, acct, t.alloc), func() {
		alloc, _ := allocFromTexts(a.rec, t.alloc)
		a.edit("set allocation", func() (*ops.Result, error) {
			return ops.SetAllocation(a.st, a.rec, acct.ID, alloc, false, ops.Options{})
		})
	})
	if model.DefaultAllocation(acct.Type) != nil {
		imgui.SameLine()
		if imgui.Button("Use default##allocdefault") {
			a.edit("use default allocation", func() (*ops.Result, error) {
				return ops.SetAllocation(a.st, a.rec, acct.ID, nil, true, ops.Options{})
			})
		}
	}
	if len(acct.Allocation) > 0 {
		imgui.SameLine()
		if imgui.Button("Clear##allocclear") {
			a.edit("clear allocation", func() (*ops.Result, error) {
				return ops.SetAllocation(a.st, a.rec, acct.ID, nil, false, ops.Options{})
			})
		}
	}
	yearly := acct.RebalanceYearly
	if imgui.Checkbox("Rebalance to the allocation every 1 January##rebalanceyearly", &yearly) {
		a.edit("set yearly rebalance", func() (*ops.Result, error) {
			return ops.SetRebalanceYearly(a.st, a.rec, acct.ID, yearly, ops.Options{})
		})
	}
}
