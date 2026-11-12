// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AllenDang/cimgui-go/imgui"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
)

// goalJob is one search for the earliest retirement date, running in the background on a copy of the record. Its fields below mu are written by the search and read by the UI.
type goalJob struct {
	proj   string
	target model.AssetID
	age    int
	pct    float64
	runs   int
	start  time.Time
	cancel context.CancelFunc

	mu   sync.Mutex
	done bool
	res  engine.EarliestRetirement
	err  error
}

// startGoalJob starts the search under the projection on a copy of rec and returns at once.
func startGoalJob(rec *model.Record, proj string, target model.AssetID, age int, pct float64, runs int) *goalJob {
	ctx, cancel := context.WithCancel(context.Background())
	j := &goalJob{proj: proj, target: target, age: age, pct: pct, runs: runs, start: time.Now(), cancel: cancel}
	snapshot := rec.Clone()
	go func() {
		res, err := engine.FindEarliestRetirement(ctx, snapshot, proj, target, pct, engine.SuccessOptions{SuccessAge: age, MonteCarloOptions: engine.MonteCarloOptions{Runs: runs, Seed: mcSeed}})
		j.mu.Lock()
		j.done, j.res, j.err = true, res, err
		j.mu.Unlock()
	}()
	return j
}

// state returns whether the search is over and, if so, its answer.
func (j *goalJob) state() (done bool, res engine.EarliestRetirement, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done, j.res, j.err
}

// goalPanel is the Earliest retirement date section of the Projections tab: its inputs and the search it last started. A new record drops the search, whose answer is for the old one.
type goalPanel struct {
	target model.AssetID
	picked bool
	age    string
	pct    string
	job    *goalJob
}

// stop cancels the running search and forgets it.
func (g *goalPanel) stop() {
	if g.job != nil {
		g.job.cancel()
		g.job = nil
	}
}

// defaultGoalTarget is the household's Net worth asset: the asset named so, in any case, or "" if there is none.
func defaultGoalTarget(rec *model.Record) model.AssetID {
	for _, a := range rec.Assets {
		if strings.EqualFold(a.Name, "net worth") {
			return a.ID
		}
	}
	return ""
}

// parseGoalInputs reads the success age and the target percent, and says what is wrong with them, or "".
func parseGoalInputs(ageText, pctText string) (age int, pct float64, problem string) {
	age, err := strconv.Atoi(strings.TrimSpace(ageText))
	if err != nil || age < 1 || age > 120 {
		return 0, 0, "the success age must be a whole number from 1 to 120"
	}
	pct, err = strconv.ParseFloat(strings.TrimSpace(pctText), 64)
	if err != nil || !(pct >= 0 && pct <= 100) {
		return 0, 0, "the target success must be a number from 0 to 100"
	}
	return age, pct, ""
}

// earliestRetirement draws the section that searches for the earliest retirement date under the projection that keeps an asset above zero until the household's first earner reaches an age in at least a share of the random runs, and applies the date it finds.
func (t *projTab) earliestRetirement(p *model.Projection, w float32) {
	g := &t.goal
	rec := t.a.rec
	view := effective(rec, p.Name)
	if g.age == "" {
		g.age, g.pct = strconv.Itoa(engine.DefaultSuccessAge), strconv.Itoa(int(engine.DefaultSuccessPercent))
	}
	if !g.picked && view.Asset(g.target) == nil {
		g.target = defaultGoalTarget(rec)
	}

	imgui.Spacing()
	imgui.SeparatorText("Earliest retirement date")
	t.label("Asset")
	name := "(pick an asset)"
	if a := view.Asset(g.target); a != nil {
		name = a.Name
	}
	imgui.SetNextItemWidth(w)
	if imgui.BeginComboV("##goalasset", name, 0) {
		for _, c := range lineCandidates(view, "") {
			if imgui.SelectableBoolV(c.name+"##"+string(c.id), c.id == g.target, 0, imgui.NewVec2(0, 0)) {
				g.target, g.picked = c.id, true
			}
		}
		imgui.EndCombo()
	}
	t.label("Success age")
	imgui.SetNextItemWidth(w)
	imgui.InputTextWithHint("##goalage", "age of the first earner", &g.age, 0, nil)
	if imgui.IsItemHovered() {
		tooltipText("The asset must stay above zero until the household's first earner reaches this age.")
	}
	t.label("Target success")
	imgui.SetNextItemWidth(w)
	imgui.InputTextWithHint("##goalpct", "percent of runs", &g.pct, 0, nil)
	if imgui.IsItemHovered() {
		tooltipText("The share of the random runs, in percent, that must stay above zero. Each date is tried with 1,000 runs and the same seed.")
	}

	age, pct, problem := parseGoalInputs(g.age, g.pct)
	if problem == "" && view.Asset(g.target) == nil {
		problem = "pick the asset to keep above zero"
	}
	running := g.job != nil && !isDone(g.job)
	if problem == "" && running {
		problem = "a search is running"
	}
	t.label("")
	actionButton("Find earliest date##goal", problem, func() {
		g.stop()
		g.job = startGoalJob(rec, p.Name, g.target, age, pct, engine.DefaultRuns)
	})
	if running {
		imgui.SameLine()
		if imgui.Button("Cancel##goal") {
			g.stop()
		}
	}

	j := g.job
	if j == nil || j.proj != p.Name {
		return
	}
	done, res, err := j.state()
	switch {
	case !done:
		wrappedText(fmt.Sprintf("Searching… %d s. Each date takes %d runs, about a dozen dates.", int(time.Since(j.start).Seconds()), j.runs))
	case err != nil:
		errorText("The search failed: " + err.Error())
	case res.Found:
		wrappedText(fmt.Sprintf("Earliest date: %s. %.0f%% of %d runs keep the asset above zero until age %d.", res.Date, res.Success, j.runs, j.age))
		actionButton("Apply##goal", "", func() { t.applyGoalDate(p.Name, res.Date) })
		if imgui.IsItemHovered() {
			tooltipText("Set the retirement date of " + p.Name + " to this date.")
		}
	default:
		wrappedText(fmt.Sprintf("None found: even retiring on %s only %.0f%% of %d runs keep the asset above zero until age %d, short of %.0f%%.", res.Latest, res.Success, j.runs, j.age, j.pct))
	}
}

func isDone(j *goalJob) bool {
	done, _, _ := j.state()
	return done
}

// applyGoalDate sets the projection's retirement date to the one the search found.
func (t *projTab) applyGoalDate(proj string, on model.Date) {
	t.a.edit("set retirement date", func() (*ops.Result, error) {
		return ops.SetRetirement(t.a.st, t.a.rec, proj, &on, ops.Options{})
	})
}
