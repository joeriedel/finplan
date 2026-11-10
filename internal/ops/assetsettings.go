// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package ops

import (
	"fmt"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// checkAssetSetting refuses a setting that sets nothing (ErrInvalidValue) or names an asset that the projection's view of rec does not have (ErrUnknownTarget). p is the projection with the edit applied. It returns the setting with its pointers copied.
func checkAssetSetting(rec *model.Record, p *model.Projection, s model.AssetSetting) (model.AssetSetting, error) {
	if s.Start == nil && s.OverrideHistory == nil {
		return s, refuse(ErrInvalidValue, "an asset setting needs a start date or an override_history")
	}
	view, err := viewOf(rec, p)
	if err != nil {
		return s, refuse(ErrInvalidValue, "%v", err)
	}
	if view.Asset(s.Asset) == nil {
		return s, refuse(ErrUnknownTarget, "there is no asset %q", s.Asset)
	}
	s.Start, s.OverrideHistory = copyPtr(s.Start), copyPtr(s.OverrideHistory)
	return s, nil
}

func settingIndexError(p *model.Projection, index int) error {
	return fmt.Errorf("projection %q: asset setting index %d out of range", p.Name, index)
}

// AddAssetSetting appends the setting to the projection's asset settings, which apply in list order with the last applied winning. It refuses a setting with neither a start nor an override_history (ErrInvalidValue) and an asset that the projection does not see (ErrUnknownTarget).
func AddAssetSetting(st *store.Store, rec *model.Record, name string, s model.AssetSetting, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return insertAssetSetting(rec, p, len(p.AssetSettings), s) })
}

// InsertAssetSetting is AddAssetSetting for a setting that ends up at index; index len adds it at the end.
func InsertAssetSetting(st *store.Store, rec *model.Record, name string, index int, s model.AssetSetting, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return insertAssetSetting(rec, p, index, s) })
}

func insertAssetSetting(rec *model.Record, p *model.Projection, index int, s model.AssetSetting) error {
	s, err := checkAssetSetting(rec, p, s)
	if err != nil {
		return err
	}
	return p.InsertAssetSetting(index, s)
}

// SetAssetSetting replaces the projection's asset setting at index with s, which is checked as AddAssetSetting checks one.
func SetAssetSetting(st *store.Store, rec *model.Record, name string, index int, s model.AssetSetting, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error {
		if index < 0 || index >= len(p.AssetSettings) {
			return settingIndexError(p, index)
		}
		s, err := checkAssetSetting(rec, p, s)
		if err != nil {
			return err
		}
		p.AssetSettings[index] = s
		return nil
	})
}

// MoveAssetSetting moves the projection's asset setting at index from so that it ends up at index to.
func MoveAssetSetting(st *store.Store, rec *model.Record, name string, from, to int, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return p.MoveAssetSetting(from, to) })
}

// RemoveAssetSetting deletes the projection's asset setting at index.
func RemoveAssetSetting(st *store.Store, rec *model.Record, name string, index int, o Options) (*Result, error) {
	return editProjection(st, rec, o, name, func(p *model.Projection) error { return p.RemoveAssetSetting(index) })
}
