// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/joeriedel/finplan/internal/model"
)

// Defaults of the success measure and the goal search.
const (
	DefaultSuccessAge     = 95
	DefaultSuccessPercent = 90.0
)

// Success returns the percent, from 0 to 100, of the result's runs whose value stays above zero at every sample time from from to through, both inclusive. Samples before from, such as the past of a line, do not count. It refuses a result without runs or without a sample in the window.
func Success(res *MonteCarloResult, from, through model.Date) (float64, error) {
	if len(res.Values) == 0 {
		return 0, errors.New("the result has no runs")
	}
	var cols []int
	for t, d := range res.Times {
		if d >= from && d <= through {
			cols = append(cols, t)
		}
	}
	if len(cols) == 0 {
		return 0, fmt.Errorf("no sample time lies between %s and %s", from, through)
	}
	ok := 0
	for _, vals := range res.Values {
		alive := true
		for _, t := range cols {
			if !(vals[t] > 0) {
				alive = false
				break
			}
		}
		if alive {
			ok++
		}
	}
	return 100 * float64(ok) / float64(len(res.Values)), nil
}

// AgeDate returns the date the record's reference person, the household's first earner (the member whose age drawdown goes by), reaches the age. It is false if the household has no earner.
func AgeDate(rec *model.Record, age int) (model.Date, bool) {
	m, ok := firstEarner(rec)
	if !ok {
		return 0, false
	}
	y, mo, d := m.Birth.YMD()
	return model.D(y+age, mo, d), true
}

// MonthStarts returns the first of each month from the month of from through through.
func MonthStarts(from, through model.Date) []model.Date {
	y, m, _ := from.YMD()
	var out []model.Date
	for d := model.D(y, m, 1); d <= through; d = model.D(y, m, 1) {
		out = append(out, d)
		m++
	}
	return out
}

// SuccessOptions say to which age a line must stay above zero. Zero SuccessAge means DefaultSuccessAge.
type SuccessOptions struct {
	SuccessAge int
	MonteCarloOptions
}

// MonteCarloSuccess runs MonteCarlo on a monthly grid from the month of the record's today to the date the household's first earner reaches the success age, and returns the result with Success from today to that date. It refuses a household without an earner.
func MonteCarloSuccess(rec *model.Record, projection string, target model.AssetID, opts SuccessOptions) (*MonteCarloResult, float64, error) {
	if opts.SuccessAge == 0 {
		opts.SuccessAge = DefaultSuccessAge
	}
	end, err := successEnd(rec, opts.SuccessAge)
	if err != nil {
		return nil, 0, err
	}
	res, err := MonteCarlo(rec, projection, target, MonthStarts(rec.Today, end), opts.MonteCarloOptions)
	if err != nil {
		return nil, 0, err
	}
	pct, err := Success(res, rec.Today, end)
	return res, pct, err
}

// successEnd is the date the first earner reaches the success age, which must not be before today.
func successEnd(rec *model.Record, age int) (model.Date, error) {
	end, ok := AgeDate(rec, age)
	if !ok {
		return 0, errors.New("the household has no earner, whose age the success age counts")
	}
	if end < rec.Today {
		return 0, fmt.Errorf("the earner reaches %d on %s, before %s", age, end, rec.Today)
	}
	return end, nil
}

// EarliestRetirement is the answer of FindEarliestRetirement. When Found is false, Date is zero and Success is that of the latest candidate.
type EarliestRetirement struct {
	Found   bool
	Date    model.Date
	Success float64
	// Latest is the last candidate, the first of the month of the success age.
	Latest model.Date
	// Evaluated is the number of candidates run.
	Evaluated int
}

// FindEarliestRetirement returns the earliest first-of-a-month date, from the month of the record's today to the month of the success age, on which the projection's household can retire so that the target stays above zero until the first earner reaches the success age in at least targetPercent of the runs. Each candidate runs MonteCarloSuccess with the same options, hence the same seed, on a copy of the record whose projection has RetireOn set to the candidate; rec is not changed. The search is a bisection and assumes that success does not fall as the retirement date moves later; it runs the latest candidate, then at most log2 of the number of months more. Cancelling ctx stops the search between candidates with its error. The projection must be named: the base has no retirement date to set.
func FindEarliestRetirement(ctx context.Context, rec *model.Record, projection string, target model.AssetID, targetPercent float64, opts SuccessOptions) (EarliestRetirement, error) {
	if rec.Projection(projection) == nil {
		return EarliestRetirement{}, fmt.Errorf("unknown projection %q", projection)
	}
	if targetPercent < 0 || targetPercent > 100 {
		return EarliestRetirement{}, fmt.Errorf("the target success %v must be between 0 and 100", targetPercent)
	}
	if opts.SuccessAge == 0 {
		opts.SuccessAge = DefaultSuccessAge
	}
	end, err := successEnd(rec, opts.SuccessAge)
	if err != nil {
		return EarliestRetirement{}, err
	}
	cands := MonthStarts(rec.Today, end)
	out := EarliestRetirement{Latest: cands[len(cands)-1]}
	eval := func(d model.Date) (float64, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		c := rec.EditableCopy()
		c.Projection(projection).RetireOn = model.Ptr(d)
		_, pct, err := MonteCarloSuccess(c, projection, target, opts)
		out.Evaluated++
		return pct, err
	}
	hi := len(cands) - 1
	pct, err := eval(cands[hi])
	if err != nil {
		return out, err
	}
	out.Success = pct
	if pct < targetPercent {
		return out, nil
	}
	for lo := 0; lo < hi; {
		mid := (lo + hi) / 2
		p, err := eval(cands[mid])
		if err != nil {
			return out, err
		}
		if p >= targetPercent {
			hi, pct = mid, p
		} else {
			lo = mid + 1
		}
	}
	out.Found, out.Date, out.Success = true, cands[hi], pct
	return out, nil
}
