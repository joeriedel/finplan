// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/model"
)

// collegeRecord has a checking account with 10,000 and a 529 account with 3,000 that earn nothing, the 529 with the contribution basis given (nil for unset).
func collegeRecord(t *testing.T, basis *float64) *model.Record {
	t.Helper()
	rec := model.NewRecord(d(2026, time.January, 1))
	rec.Household.Members = []model.HouseholdMember{{Name: "Kid", Birth: d(2008, time.April, 9), Role: model.RoleDependent}}
	chk := &model.Account{ID: "chk", Type: model.TypeChecking, Tax: model.Taxable}
	chk.AddSnapshot(d(2026, time.January, 1), 10000)
	c529 := &model.Account{ID: "c529", Name: "Kid 529", Type: model.Type529, Tax: model.Tax529, Beneficiary: "Kid", Basis: basis}
	c529.AddSnapshot(d(2026, time.January, 1), 3000)
	must(t, rec.AddAccount(chk))
	must(t, rec.AddAccount(c529))
	return rec
}

func once(y int, m time.Month, day int) model.Recurrence {
	return model.Recurrence{Kind: model.OneTime, Anchor: d(y, m, day)}
}

func TestCollegeCostsDrawThe529ThenChecking(t *testing.T) {
	rec := collegeRecord(t, nil)
	rec.Budget.Items = []model.BudgetItem{
		{ID: "fall", Name: "Fall bill", Kind: model.ItemExpense, Amount: model.Constant(2000), Recurrence: once(2026, time.March, 1), Account: "c529"},
		{ID: "spring", Name: "Spring bill", Kind: model.ItemExpense, Amount: model.Constant(2000), Recurrence: once(2026, time.September, 1), Account: "c529"},
	}
	for _, c := range []struct {
		on        model.Date
		c529, chk float64
	}{
		{d(2026, time.March, 2), 1000, 10000},
		{d(2026, time.September, 2), 0, 9000},
	} {
		if got := sample(t, rec, "", "acct:c529", c.on).Amount; !near(got, c.c529) {
			t.Errorf("529 on %s: %v, want %v", c.on, got, c.c529)
		}
		if got := sample(t, rec, "", "acct:chk", c.on).Amount; !near(got, c.chk) {
			t.Errorf("checking on %s: %v, want %v", c.on, got, c.chk)
		}
	}
	res, err := Balances(rec, "", "acct:chk", []model.Date{d(2026, time.September, 2)})
	must(t, err)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "Kid 529 ran out") || !strings.Contains(res.Notes[0], "Spring bill") {
		t.Errorf("notes %q", res.Notes)
	}
	ty := taxYear(t, rec, "", 2026)
	if ty.NonQualified529 != 0 || ty.Result.Penalty != 0 {
		t.Errorf("college costs are qualified: %v earnings, %v penalty", ty.NonQualified529, ty.Result.Penalty)
	}
}

func TestCollegeCostCountsGrowthOfThe529(t *testing.T) {
	rec := collegeRecord(t, nil)
	rec.Account("c529").StatedRate = 0.10
	rec.Budget.Items = []model.BudgetItem{
		{ID: "bill", Name: "Bill", Kind: model.ItemExpense, Amount: model.Constant(3300), Recurrence: once(2026, time.July, 1), Account: "c529"},
	}
	grown := sample(t, rec, "", "acct:c529", d(2026, time.June, 30)).Amount
	if grown <= 3000 || grown >= 3300 {
		t.Fatalf("balance before the bill %v", grown)
	}
	if got := sample(t, rec, "", "acct:c529", d(2026, time.July, 2)).Amount; got < -0.001 || got > 1 {
		t.Errorf("529 after the bill: %v, want it empty", got)
	}
	// The balance on the day of the bill is a day's growth (under 0.1% here) above the day before.
	if got := sample(t, rec, "", "acct:chk", d(2026, time.July, 2)).Amount; got < 10000-3300+grown || got > 10000-3300+grown*1.001 {
		t.Errorf("checking pays what the grown balance leaves: %v, balance %v", got, grown)
	}
}

func transferOut(amount float64) []model.BudgetItem {
	return []model.BudgetItem{{ID: "out", Kind: model.ItemTransfer, Amount: model.Constant(amount), Recurrence: once(2026, time.June, 1), From: "c529", To: "chk"}}
}

func TestNonQualifiedWithdrawalTaxesTheEarningsAndAddsThePenalty(t *testing.T) {
	basis := 2000.0
	rec := collegeRecord(t, &basis)
	rec.Budget.Items = transferOut(1500)
	ty := taxYear(t, rec, "", 2026)
	// A third of the balance is earnings.
	if !near(ty.NonQualified529, 500) || !near(ty.Input.NonQualified529, 500) {
		t.Fatalf("earnings %v, input %v, want 500", ty.NonQualified529, ty.Input.NonQualified529)
	}
	if !near(ty.Result.Penalty, 50) {
		t.Errorf("penalty %v, want 50", ty.Result.Penalty)
	}
	if ty.Result.AGI < 500 {
		t.Errorf("AGI %v lacks the earnings", ty.Result.AGI)
	}
	bd, err := Breakdown(rec, "", "acct:chk", d(2026, time.January, 1), d(2026, time.December, 31))
	must(t, err)
	if l := line(bd, CauseWithdrawal); !near(l.Projected, 1500) {
		t.Errorf("withdrawal line %+v", l)
	}
}

func TestWithdrawalWithoutBasisHasNoEarnings(t *testing.T) {
	rec := collegeRecord(t, nil)
	rec.Budget.Items = transferOut(1500)
	if ty := taxYear(t, rec, "", 2026); ty.NonQualified529 != 0 || ty.Result.Penalty != 0 {
		t.Errorf("earnings %v, penalty %v, want none", ty.NonQualified529, ty.Result.Penalty)
	}
}

func TestBasisFollowsWithdrawalsProRata(t *testing.T) {
	basis := 2400.0
	rec := collegeRecord(t, &basis)
	// Half the balance leaves, taking half the basis; the other half then leaves with 600 of earnings in it.
	rec.Budget.Items = []model.BudgetItem{
		{ID: "one", Kind: model.ItemTransfer, Amount: model.Constant(1500), Recurrence: once(2026, time.June, 1), From: "c529", To: "chk"},
		{ID: "two", Kind: model.ItemTransfer, Amount: model.Constant(1500), Recurrence: once(2026, time.July, 1), From: "c529", To: "chk"},
	}
	ty := taxYear(t, rec, "", 2026)
	if !near(ty.NonQualified529, 600) {
		t.Errorf("earnings %v, want 600", ty.NonQualified529)
	}
}

func TestContributionsAddToTheBasis(t *testing.T) {
	basis := 3000.0
	rec := collegeRecord(t, &basis)
	rec.Account("c529").StatedRate = 0.10
	rec.Budget.Items = []model.BudgetItem{
		{ID: "in", Kind: model.ItemTransfer, Amount: model.Constant(1000), Recurrence: once(2026, time.February, 1), From: "chk", To: "c529"},
		{ID: "out", Kind: model.ItemTransfer, Amount: model.Constant(4000), Recurrence: once(2027, time.January, 1), From: "c529", To: "chk"},
	}
	// Basis 4000 of a balance a little over 4000: only growth is earnings.
	ty := taxYear(t, rec, "", 2027)
	bal := sample(t, rec, "", "acct:c529", d(2026, time.December, 31)).Amount
	if want := bal - 4000; ty.NonQualified529 < want*0.9-1 || ty.NonQualified529 > want+1 {
		t.Errorf("earnings %v, want about %v", ty.NonQualified529, want)
	}
}

func TestTransferBetween529sIsUntaxedAndMovesBasis(t *testing.T) {
	basis := 2000.0
	rec := collegeRecord(t, &basis)
	other := &model.Account{ID: "other", Type: model.Type529, Tax: model.Tax529}
	other.AddSnapshot(d(2026, time.January, 1), 0)
	must(t, rec.AddAccount(other))
	rec.Budget.Items = []model.BudgetItem{
		{ID: "move", Kind: model.ItemTransfer, Amount: model.Constant(3000), Recurrence: once(2026, time.June, 1), From: "c529", To: "other"},
		{ID: "out", Kind: model.ItemTransfer, Amount: model.Constant(3000), Recurrence: once(2026, time.July, 1), From: "other", To: "chk"},
	}
	ty := taxYear(t, rec, "", 2026)
	if !near(ty.NonQualified529, 1000) {
		t.Errorf("earnings %v, want the 1000 the first account held", ty.NonQualified529)
	}
}

func TestShortfallIsNotedOncePerItem(t *testing.T) {
	rec := collegeRecord(t, nil)
	rec.Budget.Items = []model.BudgetItem{{ID: "tuition", Name: "Tuition", Kind: model.ItemExpense, Amount: model.Constant(2000), Recurrence: model.Recurrence{Kind: model.Monthly, Day: 1}, Account: "c529"}}
	res, err := Balances(rec, "", "acct:chk", []model.Date{d(2026, time.December, 1)})
	must(t, err)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "Tuition") {
		t.Errorf("notes %q, want one for the item", res.Notes)
	}
}

func TestShortfallInsideCheckingActualsIsNotBooked(t *testing.T) {
	rec := collegeRecord(t, nil)
	rec.Account("chk").AddSnapshot(d(2026, time.October, 1), 7000)
	rec.Budget.Items = []model.BudgetItem{{ID: "bill", Name: "Bill", Kind: model.ItemExpense, Amount: model.Constant(5000), Recurrence: once(2026, time.August, 1), Account: "c529"}}
	if got := sample(t, rec, "", "acct:c529", d(2026, time.August, 2)).Amount; !near(got, 0) {
		t.Errorf("529 after the bill: %v, want 0", got)
	}
	if got := sample(t, rec, "", "acct:chk", d(2026, time.October, 2)).Amount; !near(got, 7000) {
		t.Errorf("checking after its snapshot: %v, want 7000", got)
	}
}

func TestProjectionOpeningBalanceIsBasis(t *testing.T) {
	rec := collegeRecord(t, nil)
	out := model.BudgetItem{ID: "out", Kind: model.ItemTransfer, Amount: model.Constant(5000), Recurrence: once(2026, time.June, 1), From: "pj529", To: "chk"}
	must(t, rec.AddProjection(&model.Projection{
		Name:     "pj",
		Accounts: []model.ProjAccount{{ID: "pj529", Name: "Projected 529", Type: model.Type529, Tax: model.Tax529, Opening: 10000, From: d(2026, time.March, 1)}},
		Mods:     []model.Modification{{Target: model.ObjectRef{Kind: model.KindBudgetItem, ID: "out"}, Property: model.PropAddItem, Item: &out}},
	}))
	if ty := taxYear(t, rec, "pj", 2026); !near(ty.NonQualified529, 0) {
		t.Errorf("earnings %v, want 0", ty.NonQualified529)
	}
}
