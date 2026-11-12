// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package benefits

import "testing"

func TestRMDStartAge(t *testing.T) {
	for year, want := range map[int]int{1950: 73, 1959: 73, 1960: 75, 1985: 75} {
		if got := RMDStartAge(year); got != want {
			t.Errorf("born %d: %d, want %d", year, got, want)
		}
	}
}

func TestUniformLifetimeDivisor(t *testing.T) {
	for age, want := range map[int]float64{70: 27.4, 72: 27.4, 73: 26.5, 75: 24.6, 80: 20.2, 90: 12.2, 100: 6.4, 110: 3.5, 120: 2.0, 130: 2.0} {
		if got := UniformLifetimeDivisor(age); got != want {
			t.Errorf("age %d: %v, want %v", age, got, want)
		}
	}
	for age := 73; age < 120; age++ {
		if UniformLifetimeDivisor(age+1) >= UniformLifetimeDivisor(age) {
			t.Errorf("the period does not shrink from %d", age)
		}
	}
}
