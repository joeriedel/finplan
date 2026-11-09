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

// Modification changes a property of an object on a timetable: the schedule of values says what the
// property is on each date. PropAddItem instead adds Item to the budget, with Target naming the item.
type Modification struct {
	Target   ObjectRef
	Property string
	Values   Schedule
	Item     *BudgetItem
}

// AssetSetting gives the accounts under one asset their own projection start date and override_history setting.
type AssetSetting struct {
	Asset           AssetID
	Start           *Date
	OverrideHistory *bool
}

// Projection is a named set of hypothetical changes over the base, optionally building on one parent.
// Its AssetSettings and Mods are applied in list order, the last applied winning (see Apply). It stores only inputs. A nil Start or OverrideHistory inherits from the parent chain, and then from the record's Today and false.
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
