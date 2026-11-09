// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// AssetID identifies an asset.
type AssetID string

const defaultAssetPrefix = "acct:"

// DefaultAssetID returns the id of the default asset that wraps one account alone.
// Ids with this prefix are reserved for default assets.
func DefaultAssetID(a AccountID) AssetID { return AssetID(defaultAssetPrefix + string(a)) }

func accountOfDefault(id AssetID) (AccountID, bool) {
	s, ok := strings.CutPrefix(string(id), defaultAssetPrefix)
	return AccountID(s), ok
}

// Interval is a default balance that applies from From to To, both inclusive. A nil To leaves it open-ended.
type Interval struct {
	From  Date
	To    *Date
	Value float64
}

// DefaultBalance is a fallback value for dates with no actual data: a base of 0, overridden by intervals.
type DefaultBalance struct {
	Intervals []Interval
}

// Covers returns the value of the interval covering d, and false if none does.
// Where intervals overlap, the one added last wins.
func (b DefaultBalance) Covers(d Date) (float64, bool) {
	for i := len(b.Intervals) - 1; i >= 0; i-- {
		if iv := b.Intervals[i]; d >= iv.From && (iv.To == nil || d <= *iv.To) {
			return iv.Value, true
		}
	}
	return 0, false
}

// At returns the default balance on d: the covering interval's value, else the base 0.
func (b DefaultBalance) At(d Date) float64 {
	v, _ := b.Covers(d)
	return v
}

// Rule selects accounts by attributes. Empty lists match everything, so an empty rule matches every account.
type Rule struct {
	Types []AccountType
	Tax   []TaxTreatment
}

// Matches reports whether the account satisfies the rule.
func (r Rule) Matches(a *Account) bool {
	return (len(r.Types) == 0 || slices.Contains(r.Types, a.Type)) &&
		(len(r.Tax) == 0 || slices.Contains(r.Tax, a.Tax))
}

// Member is one member of an asset: exactly one of an account, an asset or a rule.
// Seq records when it was added, across the whole record, so "later-added" is defined.
type Member struct {
	Seq     int
	Account AccountID
	Asset   AssetID
	Rule    *Rule
}

// AccountMember returns a member naming an account.
func AccountMember(id AccountID) Member { return Member{Account: id} }

// AssetMember returns a member naming an asset.
func AssetMember(id AssetID) Member { return Member{Asset: id} }

// RuleMember returns a member that selects accounts by rule.
func RuleMember(r Rule) Member { return Member{Rule: &r} }

func (m Member) String() string {
	switch {
	case m.Asset != "":
		return fmt.Sprintf("asset %q", m.Asset)
	case m.Rule != nil:
		return fmt.Sprintf("rule %v", *m.Rule)
	}
	return fmt.Sprintf("account %q", m.Account)
}

// Asset wraps members and carries behavior: default balances and parameter overrides.
type Asset struct {
	ID      AssetID
	Name    string
	Members []Member
	Default DefaultBalance
	// Params set parameters of everything the asset wraps, by name (see PropGrowth); they apply in order from the innermost asset out, so an outer asset's Params win.
	Params map[string]Schedule
}

// ErrCycle is matched by every error caused by a cycle.
var ErrCycle = errors.New("cycle")

// CycleError reports a cycle among assets, or among a projection's parents.
type CycleError struct {
	Path []AssetID
}

func (e *CycleError) Error() string {
	parts := make([]string, len(e.Path))
	for i, id := range e.Path {
		parts[i] = string(id)
	}
	return "model: asset cycle: " + strings.Join(parts, " -> ")
}

// Is makes errors.Is(err, ErrCycle) true.
func (e *CycleError) Is(target error) bool { return target == ErrCycle }
