// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/joeriedel/finplan/internal/model"
)

func TestDisplayNames(t *testing.T) {
	wantTypes := []string{"Checking", "Savings", "Credit card", "Brokerage", "401(k)", "TSP", "IRA", "Roth IRA", "HSA", "529", "Mortgage", "Loan", "Equity", "Property"}
	if len(model.AccountTypes) != len(wantTypes) {
		t.Fatalf("%d account types", len(model.AccountTypes))
	}
	for i, ty := range model.AccountTypes {
		if got := accountTypeName(ty); got != wantTypes[i] || accountTypeLabels[i] != wantTypes[i] {
			t.Errorf("%q is %q, want %q", ty, got, wantTypes[i])
		}
	}
	for i, want := range []string{"Taxable", "Tax-deferred", "Tax-free", "529"} {
		if got := taxName(model.TaxTreatments[i]); got != want || taxLabels[i] != want {
			t.Errorf("%q is %q, want %q", model.TaxTreatments[i], got, want)
		}
	}
	for k, want := range map[model.ItemKind]string{model.ItemIncome: "Income", model.ItemExpense: "Expense", model.ItemTransfer: "Transfer"} {
		if got := itemKindName(k); got != want {
			t.Errorf("%q is %q, want %q", k, got, want)
		}
	}
	if accountTypeName("odd") != "odd" || taxName("odd") != "odd" || itemKindName("odd") != "odd" {
		t.Error("an unknown value is not shown as it is")
	}
}

func TestRuleLabel(t *testing.T) {
	for _, c := range []struct {
		r    model.Rule
		want string
	}{
		{model.Rule{}, "Rule: all accounts"},
		{model.Rule{Types: []model.AccountType{model.TypeChecking, model.TypeSavings}, Tax: []model.TaxTreatment{model.Taxable}}, "Rule: accounts of type Checking, Savings with tax treatment Taxable"},
		{model.Rule{Types: []model.AccountType{model.Type401k}}, "Rule: accounts of type 401(k)"},
		{model.Rule{Tax: []model.TaxTreatment{model.TaxDeferred, model.TaxFree}}, "Rule: accounts with tax treatment Tax-deferred, Tax-free"},
	} {
		if got := ruleLabel(c.r); got != c.want {
			t.Errorf("ruleLabel = %q, want %q", got, c.want)
		}
	}
}
