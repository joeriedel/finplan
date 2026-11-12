// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"fmt"
	"slices"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// Kinds of Affected.
const (
	AffectedMember       = "member"
	AffectedAssetSetting = "asset setting"
	AffectedModification = "modification"
	AffectedBudgetItem   = "budget item"
	AffectedOrphan       = "orphaned modification"
	AffectedPreset       = "view preset"
	AffectedHousehold    = "household"
)

// Effects of a delete on an Affected object.
const (
	EffectRemoved = "removed"
	EffectDeleted = "deleted"
	// EffectOrphaned is for a modification that stays but targets nothing once its budget item is deleted.
	EffectOrphaned = "orphaned"
)

// Affected is one object a delete of an asset or account changes.
type Affected struct {
	// Kind is one of the Affected constants.
	Kind string `json:"kind"`
	// In is the object that holds it: an asset id, a projection name, "budget" for the base budget, or a view preset name.
	In string `json:"in"`
	// Detail says which member, setting, modification or budget item it is.
	Detail string `json:"detail"`
	// Effect is EffectRemoved for a member, setting, modification or view preset line, and EffectDeleted for a budget item.
	Effect string `json:"effect"`
}

// DeletePlan is what a delete of an asset or account would do besides removing the object itself.
type DeletePlan struct {
	// Affected lists the objects that contain or refer to the deleted one and are changed with it. It is empty when Blocked is set.
	Affected []Affected
	// Blocked is why the delete is refused (ErrInUse); empty when it can go ahead.
	Blocked string
}

// AssetDeletePlan returns what DeleteAsset would change besides the asset: its membership in other assets, the asset settings and modifications of projections that name it, and the saved view presets that have a line of it (one entry per preset). Modifications of accounts the asset contains stay. It is ErrUnknownObject for an asset the record does not have and ErrInvalidValue for an account's default asset. Nothing is changed.
func AssetDeletePlan(st *store.Store, rec *model.Record, id model.AssetID) (DeletePlan, error) {
	next := rec.Clone()
	if _, err := editableAsset(next, id); err != nil {
		return DeletePlan{}, err
	}
	affected, err := withPresets(st, unlinkAsset(next, id, ""), id)
	return DeletePlan{Affected: affected}, err
}

// withPresets appends to affected one entry for each saved view preset that has a line of the assets.
func withPresets(st *store.Store, affected []Affected, assets ...model.AssetID) ([]Affected, error) {
	changes, err := presetsWithout(st, assets...)
	if err != nil {
		return nil, fmt.Errorf("could not read the view presets: %w", err)
	}
	for _, c := range changes {
		detail := plural(c.Lines, "line")
		if c.Bands > 0 {
			detail += ", " + plural(c.Bands, "band")
		}
		affected = append(affected, Affected{AffectedPreset, c.Before.Name, detail, EffectRemoved})
	}
	return affected, nil
}

// AccountDeletePlan returns what DeleteAccount would change besides the account and its default asset: membership in other assets, asset settings and modifications naming the account or its default asset, the saved view presets that have a line of its default asset (one entry per preset), the household's reconciliation account setting, the budget items that move money in or out of it (base and projection-added), and modifications of deleted base items. An account with record entries, or that is the counter account of any entry stored, has Blocked set instead. It is ErrUnknownObject for an account the record does not have. Nothing is changed.
func AccountDeletePlan(st *store.Store, rec *model.Record, id model.AccountID) (DeletePlan, error) {
	next := rec.Clone()
	a := next.Account(id)
	if a == nil {
		return DeletePlan{}, unknownAccount(id)
	}
	blocked, err := accountBlocked(st, next, a)
	if err != nil || blocked != "" {
		return DeletePlan{Blocked: blocked}, err
	}
	affected, err := withPresets(st, unlinkAccount(next, id), model.DefaultAssetID(id))
	return DeletePlan{Affected: affected}, err
}

// BudgetItemDeletePlan returns what DeleteBudgetItem would leave behind: the modifications of projections that target the base item, which stay and point at nothing. It is ErrUnknownObject for an item the budget does not have. Nothing is changed.
func BudgetItemDeletePlan(rec *model.Record, id string) (DeletePlan, error) {
	if _, err := baseItem(rec, id); err != nil {
		return DeletePlan{}, err
	}
	var out []Affected
	for _, p := range rec.Projections {
		for _, m := range p.Mods {
			if m.Target.Kind == model.KindBudgetItem && m.Target.ID == id {
				out = append(out, Affected{AffectedOrphan, p.Name, string(m.Property), EffectOrphaned})
			}
		}
	}
	return DeletePlan{Affected: out}, nil
}

// accountBlocked returns why the account cannot be deleted: it has record entries (grants included), is the brokerage account of a grant, stored or in the record, or is the counter account of a stored entry (voided included). It is empty for an account that can go.
func accountBlocked(st *store.Store, rec *model.Record, a *model.Account) (string, error) {
	stored, err := st.HasEntries(a.ID)
	if err != nil {
		return "", fmt.Errorf("could not read the entries of account %q: %w", a.ID, err)
	}
	if stored || len(a.Snapshots) > 0 || len(a.Transactions) > 0 || len(a.Grants) > 0 {
		return fmt.Sprintf("account %q has record entries: close it instead", a.ID), nil
	}
	for _, o := range rec.Accounts {
		for _, g := range o.Grants {
			if g.Brokerage == a.ID {
				return fmt.Sprintf("account %q is the brokerage account of grant %d of account %q: close it instead", a.ID, g.ID, o.ID), nil
			}
		}
	}
	naming, err := st.EntriesNaming(a.ID)
	if err != nil {
		return "", fmt.Errorf("could not read the entries that name account %q: %w", a.ID, err)
	}
	if len(naming) > 0 {
		return fmt.Sprintf("account %q is the counter account of an entry of account %q: close it instead", a.ID, naming[0].Account), nil
	}
	return "", nil
}

// unlinkAsset removes from next every member naming the asset (or, when acct is set, naming that account) in other assets, and every asset setting and modification of a projection naming the asset. It returns what it removed.
func unlinkAsset(next *model.Record, asset model.AssetID, acct model.AccountID) []Affected {
	var out []Affected
	names := func(m model.Member) bool { return m.Asset == asset || acct != "" && m.Account == acct }
	for _, other := range next.Assets {
		if other.ID == asset {
			continue
		}
		for {
			i := slices.IndexFunc(other.Members, names)
			if i < 0 {
				break
			}
			m := other.Members[i]
			detail := fmt.Sprintf("asset %q", m.Asset)
			if m.Account != "" {
				detail = fmt.Sprintf("account %q", m.Account)
			}
			// the member exists in an asset that is not a default asset, so this cannot fail
			_ = next.RemoveMember(other.ID, m.Seq)
			out = append(out, Affected{AffectedMember, string(other.ID), detail, EffectRemoved})
		}
	}
	for _, p := range next.Projections {
		p.AssetSettings = slices.DeleteFunc(p.AssetSettings, func(s model.AssetSetting) bool {
			if s.Asset != asset {
				return false
			}
			out = append(out, Affected{AffectedAssetSetting, p.Name, fmt.Sprintf("asset %q", asset), EffectRemoved})
			return true
		})
		out = append(out, removeMods(p, func(m model.Modification) string {
			if m.Target.Kind == model.KindAsset && m.Target.ID == string(asset) {
				return fmt.Sprintf("%s of asset %q", m.Property, asset)
			}
			return ""
		})...)
	}
	return out
}

// unlinkAccount is unlinkAsset for the account's default asset and the account itself, plus the household's reconciliation account if it is the account, the budget items that move money in or out of the account and the modifications that target the account or those items.
func unlinkAccount(next *model.Record, id model.AccountID) []Affected {
	out := unlinkAsset(next, model.DefaultAssetID(id), id)
	uses := func(it model.BudgetItem) bool { return it.Account == id || it.From == id || it.To == id }
	gone := map[string]bool{}
	next.Budget.Items = slices.DeleteFunc(next.Budget.Items, func(it model.BudgetItem) bool {
		if !uses(it) {
			return false
		}
		gone[it.ID] = true
		out = append(out, Affected{AffectedBudgetItem, "budget", itemDetail(it), EffectDeleted})
		return true
	})
	for _, p := range next.Projections {
		p.Mods = slices.DeleteFunc(p.Mods, func(m model.Modification) bool {
			if m.Item == nil || !uses(*m.Item) {
				return false
			}
			gone[m.Item.ID] = true
			out = append(out, Affected{AffectedBudgetItem, p.Name, itemDetail(*m.Item), EffectDeleted})
			return true
		})
		out = append(out, removeMods(p, func(m model.Modification) string {
			if m.Target.Kind == model.KindAccount && m.Target.ID == string(id) {
				return fmt.Sprintf("%s of account %q", m.Property, id)
			}
			return ""
		})...)
	}
	if next.Household.Checking == id {
		next.Household.Checking = ""
		out = append(out, Affected{AffectedHousehold, "household", "reconciliation account", EffectRemoved})
	}
	for _, p := range next.Projections {
		out = append(out, removeMods(p, func(m model.Modification) string {
			if m.Target.Kind == model.KindBudgetItem && gone[m.Target.ID] {
				return fmt.Sprintf("%s of budget item %q", m.Property, m.Target.ID)
			}
			return ""
		})...)
	}
	return out
}

func itemDetail(it model.BudgetItem) string {
	if it.Name != "" {
		return fmt.Sprintf("%q (%s)", it.Name, it.ID)
	}
	return fmt.Sprintf("%q", it.ID)
}

// removeMods removes the modifications of p for which detail is not empty, and returns them as Affected.
func removeMods(p *model.Projection, detail func(model.Modification) string) []Affected {
	var out []Affected
	p.Mods = slices.DeleteFunc(p.Mods, func(m model.Modification) bool {
		d := detail(m)
		if d == "" {
			return false
		}
		out = append(out, Affected{AffectedModification, p.Name, d, EffectRemoved})
		return true
	})
	return out
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
