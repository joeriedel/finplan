// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"math"
	"strconv"

	"github.com/joeriedel/finplan/internal/engine"
)

var goalCommands = []*command{
	{
		Name: "goal earliest-retirement", Summary: "search the first-of-the-month dates, from this month to the month the household's first earner reaches the success age, for the earliest retirement date under a projection that keeps an asset or account above zero until that age in at least the target percent of the random runs (as in montecarlo, with the same seed for every date); the search assumes success does not fall as the date moves later, and changes nothing in the database",
		Args: []argDef{{Name: "target", Description: targetArg, Required: true}},
		Flags: []flagDef{
			{Name: "projection", Type: typeString, Required: true, Description: "projection whose retirement date is searched; its own accounts and those it inherits can be targets"},
			{Name: "success-age", Type: typeInt, Default: "95", Description: "the age of the household's first earner up to which a run must stay above zero to count as a success, 1 or more"},
			{Name: "success", Type: typeNumber, Default: "90", Description: "the percent of runs, 0 to 100, that must succeed"},
			{Name: "runs", Type: typeInt, Default: "1000", Description: "the number of random runs per date, at least 1"},
			{Name: "seed", Type: typeInt, Default: "1", Description: "the seed of the random draws, 0 or more"},
		},
		Output: "{target (asset id), projection, success_age, success_target (percent), runs, seed, found (bool), retirement_date (the earliest date meeting the target; null when found is false), success_percent (the percent of runs that succeed on retirement_date; when found is false, on the latest date, latest_date), latest_date, dates_tried}. " + jsonNote,
		run:    cmdGoalEarliestRetirement,
	},
}

func init() { commands = append(commands, goalCommands...) }

func cmdGoalEarliestRetirement(c *call) (any, error) {
	runs, seed, err := c.runsAndSeed()
	if err != nil {
		return nil, err
	}
	age, err := c.successAge()
	if err != nil {
		return nil, err
	}
	want, err := strconv.ParseFloat(c.str("success"), 64)
	if err != nil || math.IsNaN(want) || want < 0 || want > 100 {
		return nil, usageErr("--success: %q is not a number from 0 to 100", c.str("success"))
	}
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	projection, err := c.projection(rec)
	if err != nil {
		return nil, err
	}
	if projection == "" {
		return nil, usageErr("--projection: a projection is required")
	}
	view, err := rec.For(projection)
	if err != nil {
		return nil, failErr("invalid", "%v", err)
	}
	cand, err := resolveTarget(view, c.args[0], false)
	if err != nil {
		return nil, err
	}
	res, err := engine.FindEarliestRetirement(context.Background(), rec, projection, cand.asset, want, engine.SuccessOptions{SuccessAge: age, MonteCarloOptions: engine.MonteCarloOptions{Runs: runs, Seed: seed}})
	if err != nil {
		return nil, failErr("engine", "%v", err)
	}
	out := struct {
		Target     string  `json:"target"`
		Projection string  `json:"projection"`
		SuccessAge int     `json:"success_age"`
		Want       float64 `json:"success_target"`
		Runs       int     `json:"runs"`
		Seed       uint64  `json:"seed"`
		Found      bool    `json:"found"`
		Date       *jdate  `json:"retirement_date"`
		Success    float64 `json:"success_percent"`
		Latest     jdate   `json:"latest_date"`
		Tried      int     `json:"dates_tried"`
	}{string(cand.asset), projection, age, want, runs, seed, res.Found, nil, res.Success, jd(res.Latest), res.Evaluated}
	if res.Found {
		out.Date = jdp(&res.Date)
	}
	return out, nil
}
