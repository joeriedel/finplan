// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/joeriedel/finplan/internal/model"
)

// Sampler draws the standard normal shocks of the market classes with the correlations the record gives them.
type Sampler struct {
	ids []string
	vol []float64
	// chol is the lower-triangular Cholesky factor of the correlation matrix, row by row.
	chol [][]float64
}

// NewSampler builds the sampler of the record's market classes, in the record's order. The correlation matrix is dense: a pair with no correlation is 0 and a class is 1 with itself. A matrix that is not positive definite, which inconsistent correlations give, is an error.
func NewSampler(rec *model.Record) (*Sampler, error) {
	n := len(rec.Classes)
	s := &Sampler{ids: make([]string, n), vol: make([]float64, n), chol: make([][]float64, n)}
	for i, c := range rec.Classes {
		s.ids[i], s.vol[i] = c.ID, c.Volatility
	}
	for i := range n {
		s.chol[i] = make([]float64, i+1)
		for j := 0; j <= i; j++ {
			sum := rec.CorrelationOf(s.ids[i], s.ids[j])
			for k := 0; k < j; k++ {
				sum -= s.chol[i][k] * s.chol[j][k]
			}
			if i == j {
				if sum <= 1e-12 {
					return nil, fmt.Errorf("the correlations of the market classes are not consistent: the matrix is not positive definite (at class %q); change the correlations", s.ids[i])
				}
				s.chol[i][i] = math.Sqrt(sum)
			} else {
				s.chol[i][j] = sum / s.chol[j][j]
			}
		}
	}
	return s, nil
}

// Classes returns the class IDs in the order Draw fills its shocks.
func (s *Sampler) Classes() []string { return s.ids }

// Volatility returns the annual volatility of the class at index i.
func (s *Sampler) Volatility(i int) float64 { return s.vol[i] }

// Draw fills z, one entry per class, with correlated standard normal shocks.
func (s *Sampler) Draw(rng *rand.Rand, z []float64) {
	for i := range s.ids {
		z[i] = rng.NormFloat64()
	}
	// z becomes L·z in place, from the last row up, since row i reads only entries up to i.
	for i := len(z) - 1; i >= 0; i-- {
		sum := 0.0
		for j, l := range s.chol[i] {
			sum += l * z[j]
		}
		z[i] = sum
	}
}

// lognormalFactor is the multiplier exp(sigma·z − sigma²/2) of a year's expected growth factor, which has a mean of 1 and is exactly 1 at zero volatility.
func lognormalFactor(sigma, z float64) float64 {
	return math.Exp(sigma*z - sigma*sigma/2)
}
