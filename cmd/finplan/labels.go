// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
	"github.com/joeriedel/finplan/internal/tax"
)

// accountTypeNames, taxNames and kindNames are the display names of the stored values; the stored values themselves never change.
var (
	accountTypeNames = map[model.AccountType]string{
		model.TypeChecking: "Checking", model.TypeSavings: "Savings", model.TypeCreditCard: "Credit card", model.TypeBrokerage: "Brokerage",
		model.Type401k: "401(k)", model.TypeTSP: "TSP", model.TypeIRA: "IRA", model.TypeRothIRA: "Roth IRA", model.TypeHSA: "HSA", model.Type529: "529",
		model.TypeMortgage: "Mortgage", model.TypeLoan: "Loan", model.TypeEquity: "Equity", model.TypeProperty: "Property",
	}
	incomeTypeNames = map[model.IncomeType]string{model.IncomeWages: "Wages", model.IncomeOrdinary: "Other income", model.IncomeTaxFree: "Tax-free"}
	taxNames        = map[model.TaxTreatment]string{model.Taxable: "Taxable", model.TaxDeferred: "Tax-deferred", model.TaxFree: "Tax-free", model.Tax529: "529"}
	kindNames       = map[model.ItemKind]string{model.ItemIncome: "Income", model.ItemExpense: "Expense", model.ItemTransfer: "Transfer"}
	txKindNames     = map[model.TxKind]string{model.TxIncome: "Income", model.TxExpense: "Expense", model.TxTransfer: "Transfer"}
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

// incomeTypeName is the display name of an income type, or the stored value when it has none.
func incomeTypeName(t model.IncomeType) string {
	if n, ok := incomeTypeNames[t]; ok {
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

// txKindName is the display name of a transaction kind, or the stored value when it has none.
func txKindName(k model.TxKind) string {
	if n, ok := txKindNames[k]; ok {
		return n
	}
	return string(k)
}

// entryKindName is the display name of an entry's kind: Snapshot, or the name of the transaction's kind.
func entryKindName(k store.EntryKind, tx model.TxKind) string {
	if k == store.KindSnapshot {
		return "Snapshot"
	}
	return txKindName(tx)
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

// filingNames and roleNames are the display names of the filing statuses and household roles.
var (
	filingNames = map[tax.FilingStatus]string{tax.MarriedJoint: "Married filing jointly", tax.Single: "Single", tax.HeadOfHousehold: "Head of household", tax.MarriedSeparate: "Married filing separately"}
	roleNames   = map[model.Role]string{model.RoleEarner: "Earner", model.RoleSpouse: "Spouse", model.RoleDependent: "Dependent"}
)

// filingName is the display name of a filing status, or the stored value when it has none.
func filingName(s tax.FilingStatus) string {
	if n, ok := filingNames[s]; ok {
		return n
	}
	return string(s)
}

// roleName is the display name of a household role, or the stored value when it has none.
func roleName(r model.Role) string {
	if n, ok := roleNames[r]; ok {
		return n
	}
	return string(r)
}

// filingLabels and roleLabels are the display names of filingStatuses and householdRoles, in the same order.
var filingLabels, roleLabels = func() (filing, roles []string) {
	for _, s := range tax.FilingStatuses {
		filing = append(filing, filingName(s))
	}
	for _, r := range model.Roles {
		roles = append(roles, roleName(r))
	}
	return
}()

// incomeTypeLabels are the display names of incomeTypes, in the same order.
var incomeTypeLabels = func() (names []string) {
	for _, t := range model.IncomeTypes {
		names = append(names, incomeTypeName(t))
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
