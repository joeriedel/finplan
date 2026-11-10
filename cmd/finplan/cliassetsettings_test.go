// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
)

type startRow struct {
	Account         string
	Start           string
	OverrideHistory bool   `json:"override_history"`
	ProjectionStart string `json:"projection_start"`
}

func startOf(t *testing.T, path, account, projection string) startRow {
	t.Helper()
	var rows []startRow
	cliJSON(t, &rows, "--db", path, "starts", account, "--projection", projection)
	if len(rows) != 1 {
		t.Fatalf("starts %s: %+v", account, rows)
	}
	return rows[0]
}

// Scenario A: give the TSP its own start date in a projection and see its effective start.
func TestCLIAssetSettingGivesTSPItsOwnStart(t *testing.T) {
	path := seededFile(t)
	if got := startOf(t, path, "tsp", "Base"); got.ProjectionStart != "2026-11-08" {
		t.Fatalf("before: %+v", got)
	}
	w := write(t, path, "", "asset-setting", "add", "Base", "--asset", "tsp", "--start", "2027-03-01")
	if !w.Changed || len(w.Projection.AssetSettings) != 1 {
		t.Fatalf("add: %+v", w)
	}
	if s := w.Projection.AssetSettings[0]; s.Asset != "acct:tsp" || s.Start == nil || *s.Start != "2027-03-01" || s.OverrideHistory != nil {
		t.Errorf("setting: %+v", s)
	}
	if got := startOf(t, path, "tsp", "Base"); got.ProjectionStart != "2027-03-01" {
		t.Errorf("tsp after: %+v", got)
	}
	if got := startOf(t, path, "401k", "Base"); got.ProjectionStart != "2026-11-08" {
		t.Errorf("401k changed: %+v", got)
	}
	// A child projection inherits it.
	if got := startOf(t, path, "tsp", "New job"); got.ProjectionStart != "2027-03-01" {
		t.Errorf("tsp under New job: %+v", got)
	}
	var shown struct {
		AssetSettings []struct{ Asset string } `json:"asset_settings"`
	}
	cliJSON(t, &shown, "--db", path, "projection", "show", "Base")
	if len(shown.AssetSettings) != 1 || shown.AssetSettings[0].Asset != "acct:tsp" {
		t.Errorf("projection show: %+v", shown)
	}
}

func TestCLIAssetSettingEditMoveRemove(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "asset-setting", "add", "Base", "--asset", "tsp", "--start", "2027-03-01")
	w := write(t, path, "", "asset-setting", "add", "Base", "--asset", "401k", "--override-history", "true", "--at", "0")
	if s := w.Projection.AssetSettings; len(s) != 2 || s[0].Asset != "acct:401k" || s[0].OverrideHistory == nil || !*s[0].OverrideHistory {
		t.Fatalf("add --at: %+v", s)
	}
	w = write(t, path, "", "asset-setting", "edit", "Base", "1", "--start", "2027-06-01", "--override-history", "false")
	if s := w.Projection.AssetSettings[1]; s.Start == nil || *s.Start != "2027-06-01" || s.OverrideHistory == nil || *s.OverrideHistory {
		t.Errorf("edit: %+v", s)
	}
	refusedWrite(t, path, "", 1, "invalid", "asset-setting", "edit", "Base", "1", "--start", "none", "--override-history", "none")
	w = write(t, path, "", "asset-setting", "move", "Base", "0", "1")
	if s := w.Projection.AssetSettings; s[0].Asset != "acct:tsp" || s[1].Asset != "acct:401k" {
		t.Errorf("move: %+v", s)
	}
	w = write(t, path, "", "asset-setting", "remove", "Base", "0")
	if s := w.Projection.AssetSettings; len(s) != 1 || s[0].Asset != "acct:401k" {
		t.Errorf("remove: %+v", s)
	}
	refusedWrite(t, path, "", 1, "invalid", "asset-setting", "remove", "Base", "5")
	refusedWrite(t, path, "", 1, "invalid", "asset-setting", "add", "Base", "--asset", "tsp")
	refusedWrite(t, path, "", 1, "unknown_target", "asset-setting", "add", "Base", "--asset", "nowhere", "--start", "2027-01-01")
}

func TestCLIAssetSettingDryRun(t *testing.T) {
	path := seededFile(t)
	w := write(t, path, "", "asset-setting", "add", "Base", "--asset", "tsp", "--start", "2027-03-01", "--dry-run")
	if !w.DryRun || !w.Changed || len(w.Projection.AssetSettings) != 1 {
		t.Errorf("dry run: %+v", w)
	}
	if got := startOf(t, path, "tsp", "Base"); got.ProjectionStart != "2026-11-08" {
		t.Errorf("a dry run saved: %+v", got)
	}
}

// Scenario B: delete a base budget item a projection modifies; the modification turns missing and is skipped.
func TestCLIBudgetDeleteLeavesModificationMissing(t *testing.T) {
	path := seededFile(t)
	w := write(t, path, "", "budget", "delete", "salary-a", "--dry-run")
	if !w.DryRun || w.Removed != "salary-a" {
		t.Errorf("dry run: %+v", w)
	}
	var skipped struct {
		Diagnostics struct {
			Skipped []struct{ Projection, Reason string }
		}
	}
	balance := []string{"--db", path, "balance", "checking", "--from", "2027-01-31", "--to", "2027-06-30", "--projection", "New job"}
	cliJSON(t, &skipped, balance...)
	if len(skipped.Diagnostics.Skipped) != 0 {
		t.Fatalf("skipped before: %+v", skipped)
	}
	var before struct{ Modifications []struct{ Missing bool } }
	cliJSON(t, &before, "--db", path, "projection", "show", "New job")
	for _, m := range before.Modifications {
		if m.Missing {
			t.Fatalf("missing before: %+v", m)
		}
	}

	w = write(t, path, "", "budget", "delete", "salary-a")
	if !w.Changed || w.Removed != "salary-a" {
		t.Fatalf("delete: %+v", w)
	}
	var shown struct {
		Modifications []struct {
			Index         int
			Target        struct{ ID string }
			Missing       bool
			MissingTarget string `json:"missing_target"`
		}
	}
	cliJSON(t, &shown, "--db", path, "projection", "show", "New job")
	m := shown.Modifications[0]
	if m.Target.ID != "salary-a" || !m.Missing || m.MissingTarget != "budget item salary-a" {
		t.Errorf("mod 0: %+v", m)
	}
	if shown.Modifications[1].Missing {
		t.Errorf("mod 1 is missing: %+v", shown.Modifications[1])
	}
	cliJSON(t, &skipped, balance...)
	if len(skipped.Diagnostics.Skipped) != 1 || skipped.Diagnostics.Skipped[0].Projection != "New job" || skipped.Diagnostics.Skipped[0].Reason != "budget item not found" {
		t.Errorf("skipped after: %+v", skipped)
	}
	refusedWrite(t, path, "", 1, "not_found", "budget", "delete", "salary-a")
}
