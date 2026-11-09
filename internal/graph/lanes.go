// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

// LaneAt returns the index of the data lane under y, or -1. Lane i spans barH from top+i*pitch; n lanes exist.
func LaneAt(y, top, pitch, barH float32, n int) int {
	if pitch <= 0 || y < top {
		return -1
	}
	i := int((y - top) / pitch)
	if i >= n || y-top-float32(i)*pitch > barH {
		return -1
	}
	return i
}
