// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"strconv"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// The write commands for a projection's per-asset start settings. They print what the other projection writes print.

const settingIndexNote = "the setting's index, as projection show numbers asset_settings from 0"

var overrideChoices = []string{"true", "false"}

var assetSettingCommands = []*command{
	{
		Name: "asset-setting add", Summary: "give an asset its own projection start date and/or override_history in a projection; settings apply in list order and the last applied wins",
		Args: []argDef{{Name: "projection", Description: "projection to change", Required: true}},
		Flags: []flagDef{
			{Name: "asset", Type: typeString, Required: true, Description: "asset id or name; an account names its default asset"},
			{Name: "start", Type: typeDate, Description: "the asset's projection start date: " + dateNote},
			{Name: "override-history", Type: typeEnum, Choices: overrideChoices, Description: "whether projected values replace recorded history from the start"},
			{Name: "at", Type: typeInt, Description: "index to insert at; at the end when omitted"},
		},
		Output: writeOut,
		run:    cmdAssetSettingAdd,
	},
	{
		Name: "asset-setting edit", Summary: "change an asset setting of a projection; flags given override its current values",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "index", Description: settingIndexNote, Required: true},
		},
		Flags: []flagDef{
			{Name: "asset", Type: typeString, Description: "asset id or name"},
			{Name: "start", Type: typeString, Description: "the asset's projection start date: " + dateNote + "; none removes it, so the asset inherits"},
			{Name: "override-history", Type: typeEnum, Choices: append([]string{"none"}, overrideChoices...), Description: "true, false, or none to inherit"},
		},
		Output: writeOut,
		run:    cmdAssetSettingEdit,
	},
	{
		Name: "asset-setting move", Summary: "move an asset setting so that it ends up at another index",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "from", Description: "index it is at", Required: true},
			{Name: "to", Description: "index it ends up at", Required: true},
		},
		Output: writeOut,
		run:    cmdAssetSettingMove,
	},
	{
		Name: "asset-setting remove", Summary: "remove an asset setting from a projection",
		Args: []argDef{
			{Name: "projection", Description: "projection to change", Required: true},
			{Name: "index", Description: settingIndexNote, Required: true},
		},
		Output: writeOut,
		run:    cmdAssetSettingRemove,
	},
}

// settingIndex reads the positional argument at pos as an index into the projection's asset settings.
func (c *call) settingIndex(pos int, p *model.Projection) (int, error) {
	i, err := strconv.Atoi(c.args[pos])
	if err != nil {
		return 0, usageErr("<%s>: %q is not a whole number", c.cmd.Args[pos].Name, c.args[pos])
	}
	if i < 0 || i >= len(p.AssetSettings) {
		return 0, failErr("invalid", "<%s>: index %d is out of range: the projection has %d asset settings", c.cmd.Args[pos].Name, i, len(p.AssetSettings))
	}
	return i, nil
}

// settingAsset reads --asset for the projection, whose own and inherited accounts count. A name nothing has is given back as the id, which the operation refuses as an unknown target.
func (c *call) settingAsset(p *model.Projection) (model.AssetID, error) {
	view, err := c.rec.For(p.Name)
	if err != nil {
		return "", failErr("invalid", "%v", err)
	}
	cand, err := resolveTarget(view, c.str("asset"), false)
	if err != nil {
		if ce := (*cliError)(nil); errors.As(err, &ce) && ce.Code == "ambiguous" {
			return "", err
		}
		return model.AssetID(c.str("asset")), nil
	}
	return cand.asset, nil
}

func cmdAssetSettingAdd(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	asset, err := c.settingAsset(p)
	if err != nil {
		return nil, err
	}
	s := model.AssetSetting{Asset: asset}
	if d, ok, err := c.date("start"); err != nil {
		return nil, err
	} else if ok {
		s.Start = &d
	}
	if v := c.str("override-history"); v != "" {
		s.OverrideHistory = model.Ptr(v == "true")
	}
	at, atSet, err := c.integer("at")
	if err != nil {
		return nil, err
	}
	if atSet && (at < 0 || at > len(p.AssetSettings)) {
		return nil, failErr("invalid", "--at %d is out of range: the projection has %d asset settings", at, len(p.AssetSettings))
	}
	var res *ops.Result
	if atSet {
		res, err = ops.InsertAssetSetting(c.st, c.rec, p.Name, at, s, c.options())
	} else {
		res, err = ops.AddAssetSetting(c.st, c.rec, p.Name, s, c.options())
	}
	return c.finish(p.Name, res, err)
}

func cmdAssetSettingEdit(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	i, err := c.settingIndex(1, p)
	if err != nil {
		return nil, err
	}
	s := p.AssetSettings[i]
	if c.str("asset") != "" {
		if s.Asset, err = c.settingAsset(p); err != nil {
			return nil, err
		}
	}
	if v := c.str("start"); v == "none" {
		s.Start = nil
	} else if v != "" {
		d, err := parseCLIDate(v, false)
		if err != nil {
			return nil, usageErr("--start: %v", err)
		}
		s.Start = &d
	}
	switch v := c.str("override-history"); v {
	case "none":
		s.OverrideHistory = nil
	case "true", "false":
		s.OverrideHistory = model.Ptr(v == "true")
	}
	res, err := ops.SetAssetSetting(c.st, c.rec, p.Name, i, s, c.options())
	return c.finish(p.Name, res, err)
}

func cmdAssetSettingMove(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	from, err := c.settingIndex(1, p)
	if err != nil {
		return nil, err
	}
	to, err := c.settingIndex(2, p)
	if err != nil {
		return nil, err
	}
	res, err := ops.MoveAssetSetting(c.st, c.rec, p.Name, from, to, c.options())
	return c.finish(p.Name, res, err)
}

func cmdAssetSettingRemove(c *call) (any, error) {
	p, err := c.projectionArg()
	if err != nil {
		return nil, err
	}
	i, err := c.settingIndex(1, p)
	if err != nil {
		return nil, err
	}
	res, err := ops.RemoveAssetSetting(c.st, c.rec, p.Name, i, c.options())
	return c.finish(p.Name, res, err)
}
