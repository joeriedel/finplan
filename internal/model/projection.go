// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

// ObjectKind says what a modification targets.
type ObjectKind string

// Object kinds.
const (
	KindBudgetItem ObjectKind = "budget item"
	KindAccount    ObjectKind = "account"
	KindAsset      ObjectKind = "asset"
	KindGlobal     ObjectKind = "global"
)

// ObjectRef names the object of a modification.
type ObjectRef struct {
	Kind ObjectKind
	ID   string
}

// Mode says how a setting a projection gives to an asset ranks against settings on other assets that
// cover the same account.
type Mode int

// Setting modes. ModeDefault is the zero value, so stored projections without a mode load as default.
const (
	// ModeDefault settings lose to more specific ones: the setting closest to the account wins.
	ModeDefault Mode = iota
	// ModeOverride settings beat every default and every nested override for the accounts under the
	// asset: the outermost override wins.
	ModeOverride
)

// Modification changes a property of an object on a timetable: the schedule of values says what the
// property is on each date. PropAddItem instead adds Item to the budget, with Target naming the item.
type Modification struct {
	Target   ObjectRef
	Property string
	Values   Schedule
	Item     *BudgetItem
	Mode     Mode
}

// AssetSetting gives one asset its own projection start date and override_history setting. Mode
// applies to both.
type AssetSetting struct {
	Asset           AssetID
	Start           *Date
	OverrideHistory *bool
	Mode            Mode
}

// Projection is a named set of hypothetical changes over the base, optionally building on one parent.
// It stores only inputs. A nil Start or OverrideHistory inherits from the parent chain, and then from
// the record's Today and false.
type Projection struct {
	Name            string
	Parent          string
	Start           *Date
	OverrideHistory *bool
	AssetSettings   []AssetSetting
	Mods            []Modification
	// Shocks and Rebalances are stored but not executed by the placeholder engine.
	Shocks     []Shock
	Rebalances []Rebalance
}
