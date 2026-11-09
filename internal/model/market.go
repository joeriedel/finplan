// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

// MarketClass is a source of returns (treasuries, large cap, ...). The placeholder engine does not use classes.
type MarketClass struct {
	ID     string
	Name   string
	Return float64
}

// Shock is a one-off change to a class on a date: Change is a fraction, -0.3 for a 30% drop.
// The placeholder engine does not execute shocks.
type Shock struct {
	Class  string
	Date   Date
	Change float64
}

// Weight is one class's share of an allocation.
type Weight struct {
	Class string
	Share float64
}

// Rebalance is a dated change of an account's allocation. The placeholder engine does not execute rebalances.
type Rebalance struct {
	Account    AccountID
	Date       Date
	Allocation []Weight
}
