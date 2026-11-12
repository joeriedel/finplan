// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

// span is a closed range of dates.
type span struct{ from, to Date }

// spans returns the dates the schedule covers as sorted, separate ranges; touching segments are one range.
func (s Schedule) spans() []span {
	var out []span
	for i, g := range s.segs {
		to := MaxDate
		if g.End != nil {
			to = *g.End
		} else if i+1 < len(s.segs) {
			to = s.segs[i+1].From - 1
		}
		if n := len(out); n > 0 && out[n-1].to < MaxDate && g.From <= out[n-1].to+1 {
			out[n-1].to = max(out[n-1].to, to)
			continue
		}
		out = append(out, span{g.From, to})
	}
	return out
}

// Covers reports whether the schedule has a value on every date o has one.
func (s Schedule) Covers(o Schedule) bool {
	mine := s.spans()
	for _, w := range o.spans() {
		ok := false
		for _, m := range mine {
			if m.from <= w.from && w.to <= m.to {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// Overwrite says which later modification replaces the value of an earlier one.
type Overwrite struct {
	// Projection and Index name the overwriting modification: Projection.Mods[Index].
	Projection string
	Index      int
	// Partial is set when the modification still counts for some accounts it covers.
	Partial bool
}

type modRef struct {
	proj  string
	index int
	mod   Modification
}

// OverwrittenMods reports, for each of the projection's own modifications (by index), the later modification that
// fully replaces it for some account it covers, or nil. Modifications apply in the order of the projection's
// chain, root first, each in list order, so later means later in that order. Only amount, growth, default and inflation
// modifications can be overwritten; an item addition, or a target that does not exist, is never overwritten.
func (r *Record) OverwrittenMods(name string) ([]*Overwrite, error) {
	chain, err := r.Chain(name)
	if err != nil {
		return nil, err
	}
	var order []modRef
	for _, p := range chain {
		for i, m := range p.Mods {
			order = append(order, modRef{p.Name, i, m})
		}
	}
	own := chain[len(chain)-1]
	out := make([]*Overwrite, len(own.Mods))
	res := map[AssetID]*Resolution{}
	accounts := func(m Modification) []AccountID {
		id := AssetID(m.Target.ID)
		if m.Target.Kind == KindAccount {
			id = DefaultAssetID(AccountID(m.Target.ID))
		}
		if _, ok := res[id]; !ok {
			res[id], _ = r.Resolve(id)
		}
		if res[id] == nil {
			return nil
		}
		return res[id].Accounts
	}
	for k, early := range order {
		if early.proj != own.Name {
			continue
		}
		var ow *Overwrite
		if early.mod.Property == PropAddGrant {
			continue
		}
		if early.mod.Property == PropDefault {
			// A default is read per target, not per account under it, so only a modification of the same target replaces it.
			for _, late := range order[k+1:] {
				if !late.mod.IsScale() && late.mod.Target == early.mod.Target && late.mod.Property == PropDefault && late.mod.Values.Covers(early.mod.Values) {
					ow = &Overwrite{Projection: late.proj, Index: late.index}
				}
			}
			out[early.index] = ow
			continue
		}
		switch early.mod.Target.Kind {
		case KindBudgetItem, KindGlobal, KindStock, KindClass:
			if early.mod.Property == PropAddItem {
				continue
			}
			for _, late := range order[k+1:] {
				if !late.mod.IsScale() && late.mod.Target == early.mod.Target && late.mod.Property == early.mod.Property && late.mod.Values.Covers(early.mod.Values) {
					ow = &Overwrite{Projection: late.proj, Index: late.index}
				}
			}
		case KindAccount, KindAsset:
			covered := accounts(early.mod)
			done := 0
			for _, acct := range covered {
				var by *modRef
				for j := k + 1; j < len(order); j++ {
					late := order[j]
					if late.mod.IsScale() || late.mod.Property != early.mod.Property || late.mod.Target.Kind == KindBudgetItem || late.mod.Target.Kind == KindGlobal || late.mod.Target.Kind == KindStock || late.mod.Target.Kind == KindClass || !late.mod.Values.Covers(early.mod.Values) {
						continue
					}
					for _, a := range accounts(late.mod) {
						if a == acct {
							by = &late
						}
					}
				}
				if by == nil {
					continue
				}
				done++
				if ow == nil {
					ow = &Overwrite{Projection: by.proj, Index: by.index}
				}
			}
			if ow != nil {
				ow.Partial = done < len(covered)
			}
		}
		out[early.index] = ow
	}
	return out, nil
}
