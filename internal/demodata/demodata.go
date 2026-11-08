// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package demodata generates invented, deterministic account balances for exercising the UI.
// None of the numbers describe a real person.
package demodata

import (
	"math/rand/v2"
	"time"
)

// Months is the number of monthly samples, 30 years.
const Months = 30 * 12

// Data holds monthly balances. Times are unix seconds, the first of each month at 00:00 UTC.
// Debt is stored as negative values (money owed), so Total is the plain sum of the five
// account series and is the net worth.
type Data struct {
	Times     []int64
	Debt      []float64
	Cash      []float64
	Brokerage []float64
	K401      []float64
	Savings   []float64
	Total     []float64
}

// Generate returns the demo data for the given seed.
func Generate(seed uint64) *Data {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	d := &Data{
		Times:     make([]int64, Months),
		Debt:      make([]float64, Months),
		Cash:      make([]float64, Months),
		Brokerage: make([]float64, Months),
		K401:      make([]float64, Months),
		Savings:   make([]float64, Months),
		Total:     make([]float64, Months),
	}

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	owed := 280000.0
	cash := 12000.0
	brokerage := 25000.0
	k401 := 60000.0
	savings := 18000.0

	for i := 0; i < Months; i++ {
		d.Times[i] = start.AddDate(0, i, 0).Unix()

		owed -= 1100 + float64(i)*2
		if owed < 0 {
			owed = 0
		}
		cash += (rng.Float64() - 0.5) * 1500
		if cash < 2000 {
			cash = 2000
		}
		brokerage = brokerage*(1+monthlyReturn(rng, 0.07, 0.04)) + 600
		k401 = k401*(1+monthlyReturn(rng, 0.065, 0.035)) + 1200
		savings = savings*(1+0.04/12) + 400

		d.Debt[i] = -owed
		d.Cash[i] = cash
		d.Brokerage[i] = brokerage
		d.K401[i] = k401
		d.Savings[i] = savings
		d.Total[i] = d.Debt[i] + d.Cash[i] + d.Brokerage[i] + d.K401[i] + d.Savings[i]
	}
	return d
}

// monthlyReturn draws a noisy monthly return from an annual mean and a monthly spread.
func monthlyReturn(rng *rand.Rand, annualMean, spread float64) float64 {
	return annualMean/12 + (rng.Float64()-0.5)*spread
}
