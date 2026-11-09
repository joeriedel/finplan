// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"sort"
)

// Overlap reports a member that was left out of an asset because it would count an account twice.
type Overlap struct {
	// Asset is the asset holding the invalid member; Seq identifies the member in it.
	Asset AssetID
	Seq   int
	// Member describes the invalid member.
	Member string
	// Duplicates are the accounts it would have counted twice.
	Duplicates []AccountID
	// Against is the Seq of the earlier member that already included the first duplicate.
	Against int
}

func (o Overlap) String() string {
	return fmt.Sprintf("asset %q: %s duplicates %v already included by member %d", o.Asset, o.Member, o.Duplicates, o.Against)
}

type memberNode struct {
	Account AccountID
	Asset   AssetID
	Rule    bool        // a rule member; Matched may be empty
	Matched []AccountID // the accounts a rule member matched
}

type node struct {
	members  []memberNode // valid members only, in the order added
	all      []memberNode // every member, valid or not, in the order added
	accounts []AccountID  // every account the valid members include, in record order
	set      map[AccountID]bool
	overlaps []Overlap
}

// Resolution is an asset resolved against the record at one moment: which members count, which are
// invalid, and which accounts lie under it. Resolve it again after the record changes.
type Resolution struct {
	Root AssetID
	// Accounts are the accounts under Root through valid members, in record order.
	Accounts []AccountID
	// Assets are Root and the assets nested under it through valid members, root first.
	Assets []AssetID
	// Overlaps are the invalid members of every asset in Assets.
	Overlaps []Overlap

	rec        *Record
	nodes      map[AssetID]*node
	order      map[AssetID]int
	scope      map[AssetID]int
	scopeList  []AssetID
	accountSet map[AccountID]bool
	nested     map[AssetID]map[AssetID]bool
}

// Resolve checks the asset and everything nested in it, whenever it is used. Overlap never fails
// resolution; a cycle does, with an error matching ErrCycle.
func (r *Record) Resolve(id AssetID) (*Resolution, error) {
	res := &Resolution{
		Root:       id,
		rec:        r,
		nodes:      map[AssetID]*node{},
		order:      map[AssetID]int{},
		scope:      map[AssetID]int{},
		accountSet: map[AccountID]bool{},
		nested:     map[AssetID]map[AssetID]bool{},
	}
	root, err := res.build(id, nil)
	if err != nil {
		return nil, err
	}
	res.collect(id)
	res.collectScope(id)
	res.Accounts = root.accounts
	for _, a := range res.Accounts {
		res.accountSet[a] = true
	}
	return res, nil
}

func (res *Resolution) build(id AssetID, stack []AssetID) (*node, error) {
	if n, ok := res.nodes[id]; ok {
		return n, nil
	}
	if i := slices.Index(stack, id); i >= 0 {
		return nil, &CycleError{Path: append(slices.Clone(stack[i:]), id)}
	}
	a := res.rec.Asset(id)
	if a == nil {
		return nil, fmt.Errorf("model: unknown asset %q", id)
	}
	stack = append(stack[:len(stack):len(stack)], id)
	members := slices.Clone(a.Members)
	slices.SortStableFunc(members, func(x, y Member) int { return cmp.Compare(x.Seq, y.Seq) })

	n := &node{set: map[AccountID]bool{}}
	included := map[AccountID]int{} // account -> Seq of the member that included it
	for _, m := range members {
		var accts []AccountID
		switch {
		case m.Asset != "":
			child, err := res.build(m.Asset, stack)
			if err != nil {
				return nil, err
			}
			accts = child.accounts
		case m.Rule != nil:
			for _, acc := range res.rec.Accounts {
				if m.Rule.Matches(acc) {
					accts = append(accts, acc.ID)
				}
			}
		default:
			if res.rec.Account(m.Account) == nil {
				return nil, fmt.Errorf("model: asset %q: unknown member account %q", id, m.Account)
			}
			accts = []AccountID{m.Account}
		}
		n.all = append(n.all, memberNode{Account: m.Account, Asset: m.Asset})
		var dup []AccountID
		against := 0
		for _, ac := range accts {
			if s, ok := included[ac]; ok {
				dup = append(dup, ac)
				if against == 0 {
					against = s
				}
			}
		}
		if len(dup) > 0 {
			n.overlaps = append(n.overlaps, Overlap{Asset: id, Seq: m.Seq, Member: m.String(), Duplicates: dup, Against: against})
			continue
		}
		for _, ac := range accts {
			included[ac] = m.Seq
			n.set[ac] = true
		}
		mn := memberNode{Account: m.Account, Asset: m.Asset}
		if m.Rule != nil {
			mn.Rule = true
			mn.Matched = accts
		}
		n.members = append(n.members, mn)
	}
	for _, acc := range res.rec.Accounts {
		if n.set[acc.ID] {
			n.accounts = append(n.accounts, acc.ID)
		}
	}
	res.nodes[id] = n
	return n, nil
}

func (res *Resolution) collect(id AssetID) {
	if _, seen := res.order[id]; seen {
		return
	}
	res.order[id] = len(res.Assets)
	res.Assets = append(res.Assets, id)
	n := res.nodes[id]
	res.Overlaps = append(res.Overlaps, n.overlaps...)
	for _, m := range n.members {
		if m.Asset != "" {
			res.collect(m.Asset)
		}
	}
}

// collectScope numbers the assets under the root through every member, valid or not, in the order
// added. Parameter settings on any of them count, so sibling settings for an account that overlap
// are still seen as a conflict.
func (res *Resolution) collectScope(id AssetID) {
	if _, seen := res.scope[id]; seen {
		return
	}
	res.scope[id] = len(res.scope)
	res.scopeList = append(res.scopeList, id)
	for _, m := range res.nodes[id].all {
		if m.Asset != "" {
			res.collectScope(m.Asset)
		}
	}
}

// Scope returns the assets whose parameter settings can apply under the root: the root and every asset
// nested under it through any member, valid or not, in the order added.
func (res *Resolution) Scope() []AssetID { return res.scopeList }

// Invalid returns the overlap that excludes the member of the asset with the given Seq.
func (res *Resolution) Invalid(asset AssetID, seq int) (Overlap, bool) {
	for _, o := range res.Overlaps {
		if o.Asset == asset && o.Seq == seq {
			return o, true
		}
	}
	return Overlap{}, false
}

// DataKind says where an account's value on a date came from.
type DataKind int

// Data kinds.
const (
	DataNone      DataKind = iota // no data: the value is the account's default
	DataActual                    // real data
	DataProjected                 // extrapolated
)

// AccountData is an account's value on one date.
type AccountData struct {
	Amount float64
	Kind   DataKind
}

// Evaluation is an asset's value on one date, with how many of its accounts supplied actual or
// projected data (accounts on defaults count in neither).
type Evaluation struct {
	Amount    float64
	Actual    int
	Projected int
}

// Eval resolves the root asset's value on d, calling f for each account's value on d. The rule
// (docs/design.md, "Resolving an asset's balance"): if any account under an asset has data, the asset
// is the sum of its members; otherwise a default interval of the asset beats its members' defaults;
// otherwise it is the sum of its members. An account member is its data or its default.
func (res *Resolution) Eval(d Date, f func(AccountID) AccountData) Evaluation {
	cache := map[AccountID]AccountData{}
	get := func(id AccountID) AccountData {
		v, ok := cache[id]
		if !ok {
			v = f(id)
			cache[id] = v
		}
		return v
	}
	var amount func(AssetID) float64
	amount = func(id AssetID) float64 {
		n := res.nodes[id]
		hasData := false
		for _, a := range n.accounts {
			if get(a).Kind != DataNone {
				hasData = true
				break
			}
		}
		if !hasData {
			if v, ok := res.rec.Asset(id).Default.Covers(d); ok {
				return v
			}
		}
		sum := 0.0
		for _, m := range n.members {
			switch {
			case m.Asset != "":
				sum += amount(m.Asset)
			case m.Rule:
				for _, a := range m.Matched {
					sum += get(a).Amount
				}
			default:
				sum += get(m.Account).Amount
			}
		}
		return sum
	}
	out := Evaluation{Amount: amount(res.Root)}
	for _, a := range res.Accounts {
		switch get(a).Kind {
		case DataActual:
			out.Actual++
		case DataProjected:
			out.Projected++
		}
	}
	return out
}

// Balance returns the root asset's balance on d from real data and defaults.
func (res *Resolution) Balance(d Date) Balance {
	e := res.Eval(d, func(id AccountID) AccountData {
		a := res.rec.Account(id)
		if v, ok := a.ActualAt(d); ok {
			return AccountData{v, DataActual}
		}
		return AccountData{a.Default.At(d), DataNone}
	})
	return Balance{Amount: e.Amount, Actual: e.Actual > 0}
}

// Coverage returns the actual-data coverage of the root asset: the union of its accounts' data spans,
// in order. Spans on consecutive days merge; the result may be disjoint.
func (res *Resolution) Coverage() []Span {
	var spans []Span
	for _, id := range res.Accounts {
		if sp, ok := res.rec.Account(id).Span(); ok {
			spans = append(spans, sp)
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].From < spans[j].From })
	var out []Span
	for _, sp := range spans {
		if n := len(out); n > 0 && sp.From <= out[n-1].To.Add(1) {
			out[n-1].To = max(out[n-1].To, sp.To)
			continue
		}
		out = append(out, sp)
	}
	return out
}

// LastActual returns the end of the root asset's coverage, and false if it has no actual data.
func (res *Resolution) LastActual() (Date, bool) {
	cov := res.Coverage()
	if len(cov) == 0 {
		return 0, false
	}
	return cov[len(cov)-1].To, true
}

// Setting is a parameter value set on an asset.
type Setting[T any] struct {
	Asset AssetID
	Value T
}

// Conflict reports sibling settings that disagree for one account: none wraps another, so nothing
// decides between them. Set the value at an outer level to settle it. Chosen is the one used meanwhile.
type Conflict struct {
	Name    string
	Account AccountID
	Assets  []AssetID
	Chosen  AssetID
}

// Resolved is the outcome of Precedence.
type Resolved[T any] struct {
	Value    T
	Found    bool
	From     AssetID
	Conflict *Conflict
}

// Precedence picks the value of parameter name for an account from layers of settings, outermost layer
// first (a projection's settings, then assets' own). Only settings on assets that wrap the account
// under the resolved root count, through any member: an overlapping member is left out of sums but
// its settings are still seen, so they are reported rather than silently ignored. Within a layer the setting on the outermost asset wins; when several
// assets qualify and none wraps another, differing values are reported as a Conflict. Several settings
// on one asset in a layer: the last wins. An asset's default asset (see DefaultAssetID) is the
// innermost asset of its account.
func Precedence[T any](res *Resolution, acct AccountID, name string, layers ...[]Setting[T]) Resolved[T] {
	for _, layer := range layers {
		var cands []Setting[T]
		for _, s := range layer {
			if !res.covers(s.Asset, acct) {
				continue
			}
			if i := slices.IndexFunc(cands, func(c Setting[T]) bool { return c.Asset == s.Asset }); i >= 0 {
				cands[i] = s
			} else {
				cands = append(cands, s)
			}
		}
		var top []Setting[T]
		for _, c := range cands {
			if !slices.ContainsFunc(cands, func(o Setting[T]) bool { return res.wraps(o.Asset, c.Asset) }) {
				top = append(top, c)
			}
		}
		if len(top) == 0 {
			continue
		}
		slices.SortFunc(top, func(x, y Setting[T]) int { return cmp.Compare(res.orderOf(x.Asset), res.orderOf(y.Asset)) })
		out := Resolved[T]{Value: top[0].Value, Found: true, From: top[0].Asset}
		for _, o := range top[1:] {
			if !reflect.DeepEqual(o.Value, top[0].Value) {
				c := &Conflict{Name: name, Account: acct, Chosen: top[0].Asset}
				for _, t := range top {
					c.Assets = append(c.Assets, t.Asset)
				}
				out.Conflict = c
				break
			}
		}
		return out
	}
	return Resolved[T]{}
}

func (res *Resolution) orderOf(id AssetID) int {
	if i, ok := res.scope[id]; ok {
		return i
	}
	return len(res.scope)
}

// covers reports whether the asset, within the resolved scope, wraps the account.
func (res *Resolution) covers(asset AssetID, acct AccountID) bool {
	if a, ok := accountOfDefault(asset); ok {
		return a == acct && res.accountSet[acct]
	}
	_, inScope := res.scope[asset]
	return inScope && res.nodes[asset].set[acct]
}

// wraps reports whether outer wraps inner, where both wrap the same account.
func (res *Resolution) wraps(outer, inner AssetID) bool {
	if _, ok := accountOfDefault(outer); ok {
		return false
	}
	if _, ok := accountOfDefault(inner); ok {
		return true
	}
	return outer != inner && res.within(outer)[inner]
}

func (res *Resolution) within(id AssetID) map[AssetID]bool {
	if s, ok := res.nested[id]; ok {
		return s
	}
	s := map[AssetID]bool{}
	for _, m := range res.nodes[id].all {
		if m.Asset != "" {
			s[m.Asset] = true
			for k := range res.within(m.Asset) {
				s[k] = true
			}
		}
	}
	res.nested[id] = s
	return s
}
