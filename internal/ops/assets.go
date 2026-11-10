// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"slices"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// accountOfDefault returns the account an account's default asset ("acct:<id>") belongs to.
func accountOfDefault(id model.AssetID) (model.AccountID, bool) {
	s, ok := strings.CutPrefix(string(id), string(model.DefaultAssetID("")))
	return model.AccountID(s), ok
}

// newID returns the id of a new account or asset: given if not empty, else made from name. Accounts and assets share one namespace of ids, because a command names either by id, and the accounts that projections define count as taken; a given id that is taken is ErrNameTaken, a made one gets -2, -3 and so on added.
func newID(rec *model.Record, given, name string) (string, error) {
	taken := func(id string) bool {
		if rec.Account(model.AccountID(id)) != nil || rec.Asset(model.AssetID(id)) != nil {
			return true
		}
		return slices.ContainsFunc(rec.Projections, func(p *model.Projection) bool {
			return slices.ContainsFunc(p.Accounts, func(a model.ProjAccount) bool { return string(a.ID) == id })
		})
	}
	if given = strings.TrimSpace(given); given != "" {
		if taken(given) {
			return "", refuse(ErrNameTaken, "there is already an account or asset %q", given)
		}
		return given, nil
	}
	base := slug(name)
	id := base
	for n := 2; taken(id); n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id, nil
}

func unknownAsset(id model.AssetID) error {
	return refuse(ErrUnknownObject, "there is no asset %q", id)
}

func unknownAccount(id model.AccountID) error {
	return refuse(ErrUnknownObject, "there is no account %q", id)
}

// editableAsset returns the asset of next, and refuses an account's default asset, which follows its account.
func editableAsset(next *model.Record, id model.AssetID) (*model.Asset, error) {
	a := next.Asset(id)
	if a == nil {
		return nil, unknownAsset(id)
	}
	if acct, ok := accountOfDefault(id); ok {
		return nil, refuse(ErrInvalidValue, "asset %q is the default asset of account %q: edit the account instead", id, acct)
	}
	return a, nil
}

// freeAssetName refuses a name that another asset or any account already has, ignoring case and surrounding space: accounts count (an account's default asset is checked as its account) because they appear beside their default assets in the tree and pickers. The asset except is not compared, so an asset can change the case of its own name.
func freeAssetName(rec *model.Record, name string, except model.AssetID) error {
	for _, a := range rec.Assets {
		if _, isDefault := accountOfDefault(a.ID); !isDefault && a.ID != except && strings.EqualFold(strings.TrimSpace(a.Name), name) {
			return refuse(ErrNameTaken, "there is already an asset named %q", a.Name)
		}
	}
	for _, a := range rec.Accounts {
		if strings.EqualFold(strings.TrimSpace(a.Name), name) {
			return refuse(ErrNameTaken, "there is already an account named %q", a.Name)
		}
	}
	return nil
}

// NewAsset adds an aggregate asset with no members. A name that another asset or an account has is ErrNameTaken. An empty id is made from the name; an id that an account or asset has is ErrNameTaken, and one that starts with "acct:" is reserved for the default assets of accounts.
func NewAsset(st *store.Store, rec *model.Record, name, id string, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return Result{}, refuse(ErrInvalidValue, "an asset needs a name")
		}
		if _, reserved := accountOfDefault(model.AssetID(strings.TrimSpace(id))); reserved {
			return Result{}, refuse(ErrInvalidValue, "asset ids that start with %q are reserved for the default assets of accounts", model.DefaultAssetID(""))
		}
		if err := freeAssetName(next, name, ""); err != nil {
			return Result{}, err
		}
		aid, err := newID(next, id, name)
		if err != nil {
			return Result{}, err
		}
		a := &model.Asset{ID: model.AssetID(aid), Name: name}
		if err := next.AddAsset(a); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		return Result{Asset: a}, nil
	})
}

// RenameAsset changes the name of an asset; a name that another asset or an account has is ErrNameTaken. An account's default asset is renamed with its account.
func RenameAsset(st *store.Store, rec *model.Record, id model.AssetID, name string, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a, err := editableAsset(next, id)
		if err != nil {
			return Result{}, err
		}
		if name = strings.TrimSpace(name); name == "" {
			return Result{}, refuse(ErrInvalidValue, "an asset needs a name")
		}
		if err := freeAssetName(next, name, id); err != nil {
			return Result{}, err
		}
		a.Name = name
		return Result{Asset: a}, nil
	})
}

// DeleteAsset deletes an asset with its members. It is ErrInUse while another asset has it as a member or a projection names it in an asset setting or a modification, or a modification names an account it contains, and ErrInvalidValue for an account's default asset. The result's Asset is the asset as it was.
func DeleteAsset(st *store.Store, rec *model.Record, id model.AssetID, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a, err := editableAsset(next, id)
		if err != nil {
			return Result{}, err
		}
		for _, other := range next.Assets {
			if slices.ContainsFunc(other.Members, func(m model.Member) bool { return m.Asset == id }) {
				return Result{}, refuse(ErrInUse, "asset %q is a member of asset %q", id, other.ID)
			}
		}
		var under []model.AccountID
		if res, err := next.Resolve(id); err == nil {
			under = res.Accounts
		}
		for _, p := range next.Projections {
			used := slices.ContainsFunc(p.AssetSettings, func(s model.AssetSetting) bool { return s.Asset == id }) ||
				slices.ContainsFunc(p.Mods, func(m model.Modification) bool {
					return m.Target.Kind == model.KindAsset && m.Target.ID == string(id)
				})
			if used {
				return Result{}, refuse(ErrInUse, "projection %q has a setting or modification for asset %q", p.Name, id)
			}
			for _, m := range p.Mods {
				if m.Target.Kind == model.KindAccount && slices.Contains(under, model.AccountID(m.Target.ID)) {
					return Result{}, refuse(ErrInUse, "projection %q has a modification for account %q, which asset %q contains", p.Name, m.Target.ID, id)
				}
			}
		}
		if err := next.RemoveAsset(id); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		return Result{Asset: a, Removed: string(id)}, nil
	})
}

// checkMember refuses a member that names nothing the record has, or a rule with a type or tax treatment that does not exist.
func checkMember(rec *model.Record, m model.Member) error {
	switch {
	case m.Account != "" && rec.Account(m.Account) == nil:
		return unknownAccount(m.Account)
	case m.Asset != "" && rec.Asset(m.Asset) == nil:
		return unknownAsset(m.Asset)
	case m.Rule != nil:
		for _, t := range m.Rule.Types {
			if !slices.Contains(model.AccountTypes, t) {
				return refuse(ErrInvalidValue, "unknown account type %q", t)
			}
		}
		for _, t := range m.Rule.Tax {
			if !slices.Contains(model.TaxTreatments, t) {
				return refuse(ErrInvalidValue, "unknown tax treatment %q", t)
			}
		}
	}
	return nil
}

// AddMember adds an account, an asset or a rule to an aggregate asset, after its other members. A member that covers an account an earlier member covers is not refused: Resolve marks it invalid. A member that would make the asset contain itself is refused with an error matching model.ErrCycle.
func AddMember(st *store.Store, rec *model.Record, asset model.AssetID, m model.Member, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a, err := editableAsset(next, asset)
		if err != nil {
			return Result{}, err
		}
		if err := checkMember(next, m); err != nil {
			return Result{}, err
		}
		if err := next.AddMember(asset, m); err != nil {
			if _, cycle := err.(*model.CycleError); cycle {
				return Result{}, err
			}
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		return Result{Asset: a}, nil
	})
}

// RemoveMember removes the member of the asset with the given seq. The seq numbers of the record's members are renumbered afterwards, keeping their order, so the seq of a later member can change.
func RemoveMember(st *store.Store, rec *model.Record, asset model.AssetID, seq int, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a, err := editableAsset(next, asset)
		if err != nil {
			return Result{}, err
		}
		if !slices.ContainsFunc(a.Members, func(m model.Member) bool { return m.Seq == seq }) {
			return Result{}, refuse(ErrUnknownObject, "asset %q has no member with seq %d", asset, seq)
		}
		if err := next.RemoveMember(asset, seq); err != nil {
			return Result{}, refuse(ErrInvalidValue, "%v", err)
		}
		return Result{Asset: a}, nil
	})
}

// defaultBalance returns the default balance an asset's default intervals are kept in. For an account's default asset that is the account's, which is the one the resolver and the engine read for it; the account is returned too.
func defaultBalance(next *model.Record, id model.AssetID) (*model.DefaultBalance, *model.Account, *model.Asset, error) {
	a := next.Asset(id)
	if a == nil {
		return nil, nil, nil, unknownAsset(id)
	}
	if acct, ok := accountOfDefault(id); ok {
		ac := next.Account(acct)
		return &ac.Default, ac, a, nil
	}
	return &a.Default, nil, a, nil
}

// AddDefault adds a default balance interval to an asset, after its others: where intervals overlap, the one added last wins. The interval's value is the balance on dates with no actual data.
func AddDefault(st *store.Store, rec *model.Record, asset model.AssetID, iv model.Interval, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		d, ac, a, err := defaultBalance(next, asset)
		if err != nil {
			return Result{}, err
		}
		if v := iv.Value; v != v || v > maxValue || v < -maxValue {
			return Result{}, refuse(ErrInvalidValue, "%v is not a number within range", v)
		}
		if iv.To != nil && *iv.To < iv.From {
			return Result{}, refuse(ErrInvalidValue, "the interval ends %v, before it starts %v", *iv.To, iv.From)
		}
		iv.To = copyPtr(iv.To)
		d.Intervals = append(d.Intervals, iv)
		return Result{Asset: a, Account: ac}, nil
	})
}

// RemoveDefault removes the default balance interval at index, counted from 0 in the order the asset lists them.
func RemoveDefault(st *store.Store, rec *model.Record, asset model.AssetID, index int, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		d, ac, a, err := defaultBalance(next, asset)
		if err != nil {
			return Result{}, err
		}
		if index < 0 || index >= len(d.Intervals) {
			return Result{}, refuse(ErrInvalidValue, "asset %q has %d default balance intervals: index %d is out of range", asset, len(d.Intervals), index)
		}
		d.Intervals = slices.Delete(d.Intervals, index, index+1)
		return Result{Asset: a, Account: ac}, nil
	})
}

// SetParam sets the asset's parameter property (only model.PropGrowth) to the schedule, replacing its earlier setting; a nil schedule clears the setting. The setting applies to everything the asset wraps. An account's default asset takes settings for its account alone.
func SetParam(st *store.Store, rec *model.Record, asset model.AssetID, property string, values *model.Schedule, o Options) (*Result, error) {
	return editRecord(st, rec, o, func(next *model.Record) (Result, error) {
		a := next.Asset(asset)
		if a == nil {
			return Result{}, unknownAsset(asset)
		}
		if property != model.PropGrowth {
			return Result{}, refuse(ErrInvalidValue, "unknown asset parameter %q: only %s can be set", property, model.PropGrowth)
		}
		if values == nil {
			delete(a.Params, property)
			return Result{Asset: a}, nil
		}
		if err := checkSchedule(*values); err != nil {
			return Result{}, err
		}
		if a.Params == nil {
			a.Params = map[string]model.Schedule{}
		}
		a.Params[property] = *values
		return Result{Asset: a}, nil
	})
}
