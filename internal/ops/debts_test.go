// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"errors"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestEditMortgageTerms(t *testing.T) {
	st, rec := seed(t)
	m := rec.Account(demodata.Mortgage)
	if !m.Amortized() {
		t.Fatal("the demo mortgage has its terms")
	}
	first := model.D(2027, time.January, 1)
	res, err := EditAccount(st, rec, demodata.Mortgage, AccountChange{
		OriginalAmount: model.Ptr(300000.0), TermMonths: model.Ptr(180), FirstPayment: &first, PaidFrom: model.Ptr(demodata.Savings),
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := reload(t, st).Account(demodata.Mortgage)
	if got.OriginalAmount != 300000 || got.TermMonths != 180 || *got.FirstPayment != first || got.PaidFrom != demodata.Savings {
		t.Errorf("stored terms %+v", got)
	}
	if res.Account.MonthlyPayment() <= 0 {
		t.Error("an amortized mortgage has a payment")
	}

	for _, c := range []struct {
		what string
		ch   AccountChange
		want error
	}{
		{"negative amount", AccountChange{OriginalAmount: model.Ptr(-1.0)}, ErrInvalidValue},
		{"term too long", AccountChange{TermMonths: model.Ptr(1201)}, ErrInvalidValue},
		{"negative term", AccountChange{TermMonths: model.Ptr(-1)}, ErrInvalidValue},
		{"paid from itself", AccountChange{PaidFrom: model.Ptr(demodata.Mortgage)}, ErrInvalidValue},
		{"paid from nothing", AccountChange{PaidFrom: model.Ptr(model.AccountID("nope"))}, ErrUnknownObject},
	} {
		if _, err := EditAccount(st, res.Record, demodata.Mortgage, c.ch, Options{}); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.what, err)
		}
	}
	// Only a mortgage has terms.
	if _, err := EditAccount(st, rec, demodata.Checking, AccountChange{TermMonths: model.Ptr(12)}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("terms on checking: %v", err)
	}
	typ := model.TypeLoan
	if _, err := EditAccount(st, rec, demodata.Mortgage, AccountChange{Type: &typ}, Options{}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("type change with terms set: %v", err)
	}

	res, err = EditAccount(st, res.Record, demodata.Mortgage, AccountChange{ClearTerms: true}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got = reload(t, st).Account(demodata.Mortgage)
	if got.Amortized() || got.OriginalAmount != 0 || got.TermMonths != 0 || got.FirstPayment != nil || got.PaidFrom != "" {
		t.Errorf("cleared terms %+v", got)
	}
	typ = model.TypeLoan
	if _, err := EditAccount(st, res.Record, demodata.Mortgage, AccountChange{Type: &typ}, Options{}); err != nil {
		t.Errorf("type change once cleared: %v", err)
	}
}

func TestEditItemMatch(t *testing.T) {
	st, rec := seed(t)
	edit := func(mutate func(it *model.BudgetItem)) error {
		it, _ := rec.Budget.Item("salary-b")
		mutate(&it)
		_, err := EditBudgetItem(st, rec, "salary-b", it, Options{DryRun: true})
		return err
	}
	for _, c := range []struct {
		what   string
		mutate func(it *model.BudgetItem)
		want   error
	}{
		{"valid", func(it *model.BudgetItem) { it.MatchPercent, it.MatchUpTo, it.MatchAccount = 50, 6, demodata.K401 }, nil},
		{"cleared", func(it *model.BudgetItem) { it.MatchPercent, it.MatchUpTo, it.MatchAccount = 0, 0, "" }, nil},
		{"negative percent", func(it *model.BudgetItem) { it.MatchPercent = -1 }, ErrInvalidValue},
		{"up to above 100", func(it *model.BudgetItem) { it.MatchUpTo = 101 }, ErrInvalidValue},
		{"unknown account", func(it *model.BudgetItem) { it.MatchAccount = "nope" }, ErrUnknownTarget},
		{"not a 401k", func(it *model.BudgetItem) { it.MatchAccount = demodata.Checking }, ErrInvalidValue},
		{"not wages", func(it *model.BudgetItem) { it.IncomeType = model.IncomeOrdinary }, ErrInvalidValue},
		{"not income", func(it *model.BudgetItem) {
			it.Kind, it.Account = model.ItemExpense, demodata.Checking
		}, ErrInvalidValue},
	} {
		err := edit(c.mutate)
		if c.want == nil && err != nil && !errors.Is(err, ErrNoChange) || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.what, err)
		}
	}

	it, _ := rec.Budget.Item("salary-b")
	it.MatchPercent, it.MatchUpTo, it.MatchAccount = 50, 6, demodata.K401
	if _, err := EditBudgetItem(st, rec, "salary-b", it, Options{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := reload(t, st).Budget.Item("salary-b"); got.MatchPercent != 50 || got.MatchUpTo != 6 || got.MatchAccount != demodata.K401 {
		t.Errorf("stored match %+v", got)
	}
}

func TestDeletingAnAccountDropsMatchesAndTerms(t *testing.T) {
	_, rec := seed(t)
	next := rec.Clone()
	out := unlinkAccount(next, demodata.TSP)
	if it, ok := next.Budget.Item("salary-b"); !ok || it.MatchAccount != "" || it.MatchPercent != 0 || it.MatchUpTo != 0 {
		t.Errorf("salary-b kept its match: %+v", it)
	}
	if it, _ := next.Budget.Item("salary-a"); it.MatchAccount != demodata.K401 {
		t.Errorf("salary-a lost its match: %+v", it)
	}
	if affectedKinds(DeletePlan{Affected: out})[AffectedBudgetItem] == 0 {
		t.Errorf("the match is not reported: %+v", out)
	}
	next = rec.Clone()
	out = unlinkAccount(next, demodata.Checking)
	if m := next.Account(demodata.Mortgage); m.Amortized() || m.PaidFrom != "" {
		t.Errorf("the mortgage kept its paying account: %+v", m)
	}
	if affectedKinds(DeletePlan{Affected: out})[AffectedTerms] != 1 {
		t.Errorf("the terms are not reported: %+v", out)
	}
}
