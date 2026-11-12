// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
)

func TestParseGoalInputs(t *testing.T) {
	if age, pct, problem := parseGoalInputs(" 95 ", "90"); problem != "" || age != 95 || pct != 90 {
		t.Errorf("defaults: %d %v %q", age, pct, problem)
	}
	for _, c := range [][2]string{{"", "90"}, {"0", "90"}, {"121", "90"}, {"9.5", "90"}, {"95", ""}, {"95", "101"}, {"95", "-1"}, {"95", "NaN"}} {
		if _, _, problem := parseGoalInputs(c[0], c[1]); problem == "" {
			t.Errorf("%q, %q were accepted", c[0], c[1])
		}
	}
}

func TestDefaultGoalTarget(t *testing.T) {
	if got := defaultGoalTarget(demodata.Family()); got != demodata.NetWorth {
		t.Errorf("default target %q", got)
	}
}

func TestGoalTargetResolvesInProjectionView(t *testing.T) {
	rec := demodata.Family()
	acc := model.ProjAccount{ID: "villa", Name: "Villa", Type: model.TypeProperty, Tax: model.Taxable, Opening: 500, From: model.D(2027, 1, 1)}
	if err := rec.AddProjection(&model.Projection{Name: "own", Accounts: []model.ProjAccount{acc}}); err != nil {
		t.Fatal(err)
	}
	id := model.DefaultAssetID("villa")
	if rec.Asset(id) != nil {
		t.Fatal("the base record has the projection's own asset")
	}
	if effective(rec, "own").Asset(id) == nil {
		t.Error("the projection's view lacks its own asset")
	}
}

func TestGoalJobFinishes(t *testing.T) {
	rec := demodata.Family()
	j := startGoalJob(rec, demodata.ProjRetire60, demodata.NetWorth, 95, 90, 20)
	for deadline := time.Now().Add(time.Minute); !isDone(j) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	done, res, err := j.state()
	if !done || err != nil || res.Evaluated < 1 {
		t.Errorf("job: done %v, %+v, %v", done, res, err)
	}
}

func TestMonteCarloCacheComputesOnceAndReports(t *testing.T) {
	c := newMCCache(demodata.Family())
	if st, _ := c.get(demodata.NetWorth, demodata.ProjBase); st != mcComputing {
		t.Fatalf("first get is %v", st)
	}
	if !c.busy() {
		t.Error("the job is not pending")
	}
	var st mcState
	var sr *mcSeries
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if st, sr = c.get(demodata.NetWorth, demodata.ProjBase); st != mcComputing {
			break
		}
	}
	if st != mcReady || sr == nil || !sr.hasSuccess || sr.success < 0 || sr.success > 100 || len(sr.p50) != len(sr.dates) || len(sr.dates) < 100 {
		t.Fatalf("state %v series %+v", st, sr)
	}
	if c.busy() || !c.takeFresh() || c.takeFresh() {
		t.Error("pending and fresh flags are wrong after the job")
	}
	if sr.p10[len(sr.p10)-1] > sr.p90[len(sr.p90)-1] {
		t.Error("the bands are out of order")
	}
}
