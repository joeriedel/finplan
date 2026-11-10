// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"slices"
	"testing"

	"github.com/joeriedel/finplan/internal/model"
)

func TestAssetKind(t *testing.T) {
	cases := []struct {
		id   model.AssetID
		want iconKind
	}{
		{model.DefaultAssetID("checking"), iconAccount},
		{"all-cash", iconAsset},
		{"emergency-fund", iconAsset},
	}
	for _, c := range cases {
		if got := assetKind(c.id); got != c.want {
			t.Errorf("assetKind(%q) = %v, want %v", c.id, got, c.want)
		}
	}
	if iconAccount.String() != "account" || iconAsset.String() != "asset" || iconNone.String() != "" {
		t.Errorf("names %q %q %q", iconAccount, iconAsset, iconNone)
	}
}

func TestMemberKind(t *testing.T) {
	cases := []struct {
		m    model.Member
		want iconKind
	}{
		{model.AccountMember("checking"), iconAccount},
		{model.AssetMember("all-cash"), iconAsset},
		{model.AssetMember(model.DefaultAssetID("checking")), iconAccount},
		{model.Member{Rule: &model.Rule{}}, iconNone},
	}
	for _, c := range cases {
		if got := memberKind(c.m); got != c.want {
			t.Errorf("memberKind(%+v) = %v, want %v", c.m, got, c.want)
		}
	}
}

func TestIconSegments(t *testing.T) {
	for _, size := range []float32{12, 14, 17.5, 40} {
		for _, k := range []iconKind{iconAccount, iconAsset} {
			segs := iconSegments(k, size)
			if len(segs) == 0 {
				t.Fatalf("%v has no segments", k)
			}
			for _, s := range segs {
				for _, v := range []float32{s.ax, s.ay, s.bx, s.by} {
					if v < 0 || v > size {
						t.Errorf("%v at size %v: segment %+v leaves the box", k, size, s)
					}
				}
			}
		}
		if slices.Equal(iconSegments(iconAccount, size), iconSegments(iconAsset, size)) {
			t.Errorf("size %v: the account and asset icons are the same", size)
		}
	}
	if len(iconSegments(iconNone, 14)) != 0 {
		t.Error("iconNone has an icon")
	}
}

func TestTreeTip(t *testing.T) {
	if got := textClipped(100, 100); got {
		t.Error("text that fits is clipped")
	}
	if got := textClipped(100.5, 100); !got {
		t.Error("text wider than the room is not clipped")
	}
	cases := []struct {
		clipped bool
		problem string
		want    string
	}{
		{false, "", ""},
		{false, "why", "why"},
		{true, "", "Rule: all accounts"},
		{true, "why", "Rule: all accounts\n\nwhy"},
	}
	for _, c := range cases {
		if got := treeTip("Rule: all accounts", c.clipped, c.problem); got != c.want {
			t.Errorf("treeTip(clipped %v, %q) = %q, want %q", c.clipped, c.problem, got, c.want)
		}
	}
}

func TestPadFor(t *testing.T) {
	// Widths round up per string, as ImGui's do: a space is 3.2 wide but measures 4, three measure 10.
	width := func(s string) float32 { return float32(math.Ceil(float64(len(s)) * 3.2)) }
	for _, slot := range []float32{0, 1, 10, 16, 17} {
		pad := padFor(slot, width)
		if width(pad) < slot {
			t.Errorf("slot %v: pad of %d spaces is only %v wide", slot, len(pad), width(pad))
		}
		if len(pad) > 0 && width(pad[1:]) >= slot {
			t.Errorf("slot %v: pad of %d spaces is not the fewest", slot, len(pad))
		}
	}
}
