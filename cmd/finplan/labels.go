// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
)

// accountTypeNames, taxNames and kindNames are the display names of the stored values; the stored values themselves never change.
var (
	accountTypeNames = map[model.AccountType]string{
		model.TypeChecking: "Checking", model.TypeSavings: "Savings", model.TypeCreditCard: "Credit card", model.TypeBrokerage: "Brokerage",
		model.Type401k: "401(k)", model.TypeTSP: "TSP", model.TypeIRA: "IRA", model.TypeRothIRA: "Roth IRA", model.TypeHSA: "HSA", model.Type529: "529",
		model.TypeMortgage: "Mortgage", model.TypeLoan: "Loan", model.TypeEquity: "Equity", model.TypeProperty: "Property",
	}
	taxNames  = map[model.TaxTreatment]string{model.Taxable: "Taxable", model.TaxDeferred: "Tax-deferred", model.TaxFree: "Tax-free", model.Tax529: "529"}
	kindNames = map[model.ItemKind]string{model.ItemIncome: "Income", model.ItemExpense: "Expense", model.ItemTransfer: "Transfer"}
)

// accountTypeName is the display name of an account type, or the stored value when it has none.
func accountTypeName(t model.AccountType) string {
	if n, ok := accountTypeNames[t]; ok {
		return n
	}
	return string(t)
}

// taxName is the display name of a tax treatment, or the stored value when it has none.
func taxName(t model.TaxTreatment) string {
	if n, ok := taxNames[t]; ok {
		return n
	}
	return string(t)
}

// itemKindName is the display name of a budget item kind, or the stored value when it has none.
func itemKindName(k model.ItemKind) string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return string(k)
}

// accountTypeLabels and taxLabels are the display names of accountTypes and taxTreatments, in the same order.
var accountTypeLabels, taxLabels = func() (types, tax []string) {
	for _, t := range model.AccountTypes {
		types = append(types, accountTypeName(t))
	}
	for _, t := range model.TaxTreatments {
		tax = append(tax, taxName(t))
	}
	return
}()

// groupHeaderRow draws a row of the current table that titles the group of rows after it, tinted across all columns.
func groupHeaderRow(title string) {
	imgui.TableNextRowV(0, 0)
	imgui.TableSetBgColor(imgui.TableBgTargetRowBg0, u32(rgba(colMuted, 0.18)))
	imgui.TableSetColumnIndex(0)
	imgui.AlignTextToFramePadding()
	imgui.TextUnformatted(title)
}
