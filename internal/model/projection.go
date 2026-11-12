// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import "slices"

// ObjectKind says what a modification targets.
type ObjectKind string

// Object kinds.
const (
	KindBudgetItem ObjectKind = "budget item"
	KindAccount    ObjectKind = "account"
	KindAsset      ObjectKind = "asset"
	KindGlobal     ObjectKind = "global"
	KindStock      ObjectKind = "stock" // the object is a stock symbol
	KindClass      ObjectKind = "class" // the object is a market class id
)

// ObjectRef names the object of a modification.
type ObjectRef struct {
	Kind ObjectKind
	ID   string
}

// Mode says how a modification's values combine with what lies beneath them.
type Mode string

// Modification modes. The empty mode means ModeSet.
const (
	// ModeSet replaces the value beneath with the modification's.
	ModeSet Mode = "set"
	// ModeScale multiplies the value beneath by the modification's, so it follows whatever lies beneath.
	ModeScale Mode = "scale"
)

// Modification changes a property of an object on a timetable: the schedule of values says what the
// property is on each date, or by how much to multiply it when Mode is ModeScale. PropAddItem instead adds Item to the budget, with Target naming the item, and PropAddGrant adds Grant to the equity account Target names.
type Modification struct {
	Target   ObjectRef
	Property string
	Mode     Mode
	Values   Schedule
	Item     *BudgetItem
	Grant    *FocalGrant
}

// IsScale reports whether the modification multiplies the value beneath it.
func (m Modification) IsScale() bool { return m.Mode == ModeScale }

// Over returns s with the modification's values applied: written over it, or multiplied into it.
func (m Modification) Over(s Schedule) Schedule {
	if m.IsScale() {
		return s.Scale(m.Values)
	}
	return s.Overlay(m.Values)
}

// AssetSetting gives the accounts under one asset their own projection start date and override_history setting.
type AssetSetting struct {
	Asset           AssetID
	Start           *Date
	OverrideHistory *bool
}

// Bucket is a group of accounts a retired household draws on, by tax treatment.
type Bucket string

// The drawdown buckets: taxable savings and brokerage accounts, tax-deferred 401k, TSP and IRA accounts, and Roth IRA accounts.
const (
	BucketTaxable  Bucket = "taxable"
	BucketDeferred Bucket = "tax-deferred"
	BucketRoth     Bucket = "roth"
)

// DefaultWithdrawalOrder is the order a retired household draws on its buckets when no projection says otherwise.
func DefaultWithdrawalOrder() []Bucket {
	return []Bucket{BucketTaxable, BucketDeferred, BucketRoth}
}

// ValidWithdrawalOrder reports whether order names each bucket exactly once.
func ValidWithdrawalOrder(order []Bucket) bool {
	if len(order) != 3 {
		return false
	}
	for _, b := range DefaultWithdrawalOrder() {
		if !slices.Contains(order, b) {
			return false
		}
	}
	return true
}

// Projection is a named set of hypothetical changes over the base, optionally building on one parent.
// Its AssetSettings and Mods are applied in list order, the last applied winning (see Apply). It stores only inputs. A nil Start or OverrideHistory inherits from the parent chain, and then from the record's Today and false.
type Projection struct {
	Name            string
	Parent          string
	Start           *Date
	OverrideHistory *bool
	// RetireOn is the date wages stop and the household starts to live off its accounts; WithdrawalOrder is the order it draws on its buckets; RothBracket is the federal bracket rate (0.22 for 22%) each year's Roth conversion fills to. Nil inherits from the parent chain, and then none, taxable then tax-deferred then Roth, and none.
	RetireOn        *Date
	WithdrawalOrder []Bucket
	RothBracket     *float64
	AssetSettings   []AssetSetting
	Mods            []Modification
	// Accounts are hypothetical accounts that exist only under this projection and the projections built on it.
	Accounts []ProjAccount
	// Shocks and Rebalances are executed by the engine on their dates, those of the parent chain first.
	Shocks     []Shock
	Rebalances []Rebalance
}
