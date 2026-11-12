// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package benefits

// RMDStartAge is the age at which required minimum distributions from tax-deferred accounts begin for someone born in the year: 73, or 75 from 1960 (SECURE 2.0 Act, 26 U.S.C. 401(a)(9)).
func RMDStartAge(birthYear int) int {
	if birthYear >= 1960 {
		return 75
	}
	return 73
}

// uniformLifetime is the IRS Uniform Lifetime Table (Treas. Reg. 1.401(a)(9)-9) from age 72: the distribution period for each age reached in the year.
var uniformLifetime = [...]float64{
	27.4, 26.5, 25.5, 24.6, 23.7, 22.9, 22.0, 21.1, 20.2, 19.4, // 72-81
	18.5, 17.7, 16.8, 16.0, 15.2, 14.4, 13.7, 12.9, 12.2, 11.5, // 82-91
	10.8, 10.1, 9.5, 8.9, 8.4, 7.8, 7.3, 6.8, 6.4, 6.0, // 92-101
	5.6, 5.2, 4.9, 4.6, 4.3, 4.1, 3.9, 3.7, 3.5, 3.4, // 102-111
	3.3, 3.1, 3.0, 2.9, 2.8, 2.7, 2.5, 2.3, 2.0, // 112-120
}

const uniformLifetimeFrom = 72

// UniformLifetimeDivisor is the Uniform Lifetime Table's distribution period for the age reached in the year; an age before 72 uses the table's first, and an age past 120 its last.
func UniformLifetimeDivisor(age int) float64 {
	i := min(max(age-uniformLifetimeFrom, 0), len(uniformLifetime)-1)
	return uniformLifetime[i]
}
