// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"testing"
)

type tHolding struct {
	Class   string
	Percent float64
}

func allocationOf(t *testing.T, path, account string) []tHolding {
	t.Helper()
	var accts []struct {
		ID         string
		Allocation []tHolding
	}
	cliJSON(t, &accts, "--db", path, "account", "list")
	for _, a := range accts {
		if a.ID == account {
			return a.Allocation
		}
	}
	t.Fatalf("no account %q", account)
	return nil
}

type tSegment struct {
	From  string
	Value float64
}

type tClass struct {
	ID, Name   string
	Return     []tSegment
	Volatility float64
}

type tCorrelation struct {
	A, B  string
	Value float64
}

type tClassList struct {
	Classes      []tClass
	Correlations []tCorrelation
}

func TestCLIClassList(t *testing.T) {
	path := seededFile(t)
	var out tClassList
	cliJSON(t, &out, "--db", path, "class", "list")
	if len(out.Classes) != 6 || out.Classes[0].ID != "us-large-cap" || out.Classes[0].Name != "US large cap" || out.Classes[0].Return[0].Value != 0.10 || out.Classes[0].Volatility != 0.16 {
		t.Errorf("classes: %+v", out.Classes)
	}
	if len(out.Correlations) != 10 {
		t.Errorf("correlations: %+v", out.Correlations)
	}
	if got := allocationOf(t, path, "brokerage"); len(got) != 1 || got[0].Class != "us-large-cap" || got[0].Percent != 100 {
		t.Errorf("brokerage allocation: %+v", got)
	}
	if got := allocationOf(t, path, "checking"); got == nil || len(got) != 0 {
		t.Errorf("checking allocation: %+v, want an empty list", got)
	}
}

func TestCLIClassWrites(t *testing.T) {
	path := seededFile(t)
	if w := write(t, path, "", "class", "add", "Gold", "--return", "0.02", "--volatility", "0.15"); !w.Changed {
		t.Errorf("add: %+v", w)
	}
	write(t, path, "", "class", "set", "gold", "--name", "Bullion", "--return", "0.03")
	write(t, path, "", "class", "correlation", "bullion", "us-large-cap", "--value", "0.2")
	write(t, path, `[{"from":"2027-01-01","value":0.01}]`, "class", "growth", "Bullion")
	var out tClassList
	cliJSON(t, &out, "--db", path, "class", "list")
	i := slices.IndexFunc(out.Classes, func(c tClass) bool { return c.ID == "gold" })
	if i < 0 || out.Classes[i].Name != "Bullion" || out.Classes[i].Volatility != 0.15 || len(out.Classes[i].Return) != 1 || out.Classes[i].Return[0].From != "2027-01-01" || out.Classes[i].Return[0].Value != 0.01 {
		t.Fatalf("classes: %+v", out.Classes)
	}
	if len(out.Correlations) != 11 {
		t.Errorf("correlations: %+v", out.Correlations)
	}
	if w := write(t, path, "", "class", "set", "gold", "--name", "Bullion"); w.Changed {
		t.Errorf("same again: %+v", w)
	}
	refusedWrite(t, path, "", 1, "name_taken", "class", "add", "Bullion")
	refusedWrite(t, path, "", 1, "unknown_target", "class", "set", "nope", "--return", "0.1")
	refusedWrite(t, path, "", 1, "invalid", "class", "correlation", "gold", "cash", "--value", "2")
	refusedWrite(t, path, "", 2, "usage", "class", "set", "gold")
	refusedWrite(t, path, "", 1, "in_use", "class", "remove", "us-large-cap")
	write(t, path, "", "class", "remove", "gold")
	cliJSON(t, &out, "--db", path, "class", "list")
	if len(out.Classes) != 6 || len(out.Correlations) != 10 {
		t.Errorf("after remove: %d classes, %d correlations", len(out.Classes), len(out.Correlations))
	}
}

func TestCLIAccountAllocation(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "account", "allocation", "brokerage", "--set", "us-large-cap=60,US bonds=40")
	if got := allocationOf(t, path, "brokerage"); len(got) != 2 || got[0] != (tHolding{"us-large-cap", 60}) || got[1] != (tHolding{"us-bonds", 40}) {
		t.Errorf("set: %+v", got)
	}
	refusedWrite(t, path, "", 1, "invalid", "account", "allocation", "brokerage", "--set", "us-large-cap=60")
	refusedWrite(t, path, "", 1, "invalid", "account", "allocation", "checking", "--set", "us-large-cap=100")
	refusedWrite(t, path, "", 1, "unknown_target", "account", "allocation", "brokerage", "--set", "gold=100")
	refusedWrite(t, path, "", 2, "usage", "account", "allocation", "brokerage", "--set", "us-large-cap")
	refusedWrite(t, path, "", 2, "usage", "account", "allocation", "brokerage")
	refusedWrite(t, path, "", 2, "usage", "account", "allocation", "brokerage", "--default", "--clear")
	write(t, path, "", "account", "allocation", "brokerage", "--clear")
	if got := allocationOf(t, path, "brokerage"); len(got) != 0 {
		t.Errorf("clear: %+v", got)
	}
	write(t, path, "", "account", "allocation", "401k", "--default")
	if got := allocationOf(t, path, "401k"); len(got) != 2 || got[0] != (tHolding{"us-large-cap", 60}) {
		t.Errorf("default: %+v", got)
	}
	if w := write(t, path, "", "account", "allocation", "401k", "--default"); w.Changed {
		t.Errorf("same again: %+v", w)
	}
}

// The scenarios of the class engine, through the command line: an allocated account grows at the blend of its classes, a projection that lowers large cap lowers it, and the breakdown shows growth by class.
func TestCLIClassGrowthScenarios(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "account", "allocation", "brokerage", "--set", "us-large-cap=60,us-bonds=40")
	base := lastValue(t, path, "brokerage", "Base", "2030-01", "2030-12")
	write(t, path, "", "projection", "new", "Weak stocks", "--parent", "Base")
	write(t, path, "", "mod", "add", "Weak stocks", "--target", "class:us-large-cap", "--property", "growth", "--value", "0.05")
	var show tProjectionShow
	cliJSON(t, &show, "--db", path, "projection", "show", "Weak stocks")
	if m := show.Modifications[0]; m.Target.Kind != "class" || m.Target.ID != "us-large-cap" || m.Property != "growth" || m.Schedule[0].Value != 0.05 {
		t.Errorf("stored modification: %+v", m)
	}
	if weak := lastValue(t, path, "brokerage", "Weak stocks", "2030-01", "2030-12"); !(weak < base) {
		t.Errorf("brokerage with large cap at 5%%: %v, base %v", weak, base)
	}
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "add", "Weak stocks", "--target", "class:nope", "--property", "growth", "--value", "0.1")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Weak stocks", "--target", "class:", "--property", "growth", "--value", "0.1")

	var bd struct {
		Lines []struct {
			Cause     string
			Class     *string
			Projected float64
		}
		Start, End float64
	}
	cliJSON(t, &bd, "--db", path, "breakdown", "brokerage", "--from", "2030-01-01", "--to", "2030-12")
	var classes []string
	sum := 0.0
	for _, l := range bd.Lines {
		if l.Cause == "growth" {
			if l.Class == nil {
				t.Errorf("a growth line of the allocated account has no class: %+v", l)
				continue
			}
			classes = append(classes, *l.Class)
			sum += l.Projected
		}
	}
	if !slices.Equal(classes, []string{"us-large-cap", "us-bonds"}) || !(sum > 0) {
		t.Errorf("growth lines by class: %v summing to %v", classes, sum)
	}

	// An account with no allocation keeps one growth line.
	cliJSON(t, &bd, "--db", path, "breakdown", "savings", "--from", "2030-01-01", "--to", "2030-12")
	for _, l := range bd.Lines {
		if l.Class != nil {
			t.Errorf("savings has no allocation but a line has class %q", *l.Class)
		}
	}
}
