// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package model

// MissingTargets reports, for each of the projection's own modifications (by index), a description of the target
// that does not exist, such as "budget item salary-a", or "" for a modification whose target resolves. The engine
// skips a modification with a missing target. A budget item resolves if the record has it or a modification
// earlier in the chain adds it; an account or asset must be in the projection's view of the record.
func (r *Record) MissingTargets(name string) ([]string, error) {
	chain, err := r.Chain(name)
	if err != nil {
		return nil, err
	}
	view, err := r.For(name)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, it := range r.Budget.Items {
		ids[it.ID] = true
	}
	own := chain[len(chain)-1]
	out := make([]string, len(own.Mods))
	for _, p := range chain {
		for i, m := range p.Mods {
			missing := ""
			switch m.Target.Kind {
			case KindBudgetItem:
				if (m.Property == PropAmount || m.Property == PropGrowth) && !ids[m.Target.ID] {
					missing = "budget item " + m.Target.ID
				}
				if m.Property == PropAddItem && m.Item != nil {
					ids[m.Item.ID] = true
				}
			case KindAccount:
				if view.Account(AccountID(m.Target.ID)) == nil {
					missing = "account " + m.Target.ID
				}
			case KindAsset:
				if view.Asset(AssetID(m.Target.ID)) == nil {
					missing = "asset " + m.Target.ID
				}
			case KindStock:
				if !r.HasStock(m.Target.ID) {
					missing = "stock " + m.Target.ID
				}
			case KindClass:
				if r.Class(m.Target.ID) == nil {
					missing = "class " + m.Target.ID
				}
			}
			if p == own {
				out[i] = missing
			}
		}
	}
	return out, nil
}
