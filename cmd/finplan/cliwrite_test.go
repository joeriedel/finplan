// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

type written struct {
	DryRun     bool `json:"dry_run"`
	Changed    bool
	Removed    string
	Projection *struct {
		Name            string
		Parent          *string
		Start           *string
		OverrideHistory *bool `json:"override_history"`
		Modifications   []struct {
			Index    int
			Target   struct{ Kind, ID string }
			Property string
			Schedule []struct {
				From  string
				End   *string
				Value float64
			}
			Item *struct {
				ID, Name, Kind string
				Amount         float64
				Account        *string
			}
		}
	}
	Renamed *struct{ From, To string }
	Presets []string `json:"rewritten_presets"`
}

// wcli runs a command line with the text on stdin.
func wcli(t *testing.T, stdin string, args ...string) (int, []byte) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runCLI(args, strings.NewReader(stdin), &out, &errOut)
	if errOut.Len() > 0 {
		t.Errorf("finplan %v wrote to stderr: %s", args, errOut.String())
	}
	return code, out.Bytes()
}

// write runs a write command on the file that must succeed.
func write(t *testing.T, path, stdin string, args ...string) written {
	t.Helper()
	code, out := wcli(t, stdin, append([]string{"--db", path}, args...)...)
	if code != 0 {
		t.Fatalf("finplan %v exited %d: %s", args, code, out)
	}
	var w written
	if err := json.Unmarshal(out, &w); err != nil {
		t.Fatalf("finplan %v: %v\n%s", args, err, out)
	}
	return w
}

// refusedWrite runs a write command that must fail with the exit code and error code, and checks that it left the database as it was.
func refusedWrite(t *testing.T, path, stdin string, wantExit int, wantCode string, args ...string) {
	t.Helper()
	before := dbSnapshot(t, path)
	code, out := wcli(t, stdin, append([]string{"--db", path}, args...)...)
	var e struct {
		Error struct{ Code, Message string }
	}
	if code != wantExit || json.Unmarshal(out, &e) != nil || e.Error.Code != wantCode || e.Error.Message == "" {
		t.Fatalf("finplan %v exited %d, want %d with code %s: %s", args, code, wantExit, wantCode, out)
	}
	if !reflect.DeepEqual(before, dbSnapshot(t, path)) {
		t.Errorf("finplan %v changed the database", args)
	}
}

type snapshot struct {
	Projections []*model.Projection
	Presets     []store.Preset
}

func dbSnapshot(t *testing.T, path string) snapshot {
	t.Helper()
	st, err := store.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rec, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.Presets()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot{rec.Projections, rows}
}

func lastValue(t *testing.T, path, target, projection, from, to string) float64 {
	t.Helper()
	args := []string{"--db", path, "balance", target, "--from", from, "--to", to}
	if projection != "" {
		args = append(args, "--projection", projection)
	}
	var b balanceOut
	cliJSON(t, &b, args...)
	return b.Samples[len(b.Samples)-1].Value
}

func TestCLIWriteProjectionCommands(t *testing.T) {
	path := seededFile(t)

	w := write(t, path, "", "projection", "new", "Early", "--parent", "Base")
	if !w.Changed || w.DryRun || w.Projection.Name != "Early" || *w.Projection.Parent != "Base" {
		t.Errorf("new: %+v", w)
	}
	var list []struct{ Name string }
	cliJSON(t, &list, "--db", path, "projection", "list")
	if !slices.ContainsFunc(list, func(p struct{ Name string }) bool { return p.Name == "Early" }) {
		t.Errorf("a following read does not see the new projection: %v", list)
	}

	w = write(t, path, "", "projection", "clone", "New job", "--as", "Job copy")
	if w.Projection.Name != "Job copy" || len(w.Projection.Modifications) != 2 {
		t.Errorf("clone: %+v", w.Projection)
	}

	w = write(t, path, "", "projection", "rename", "Job copy", "Job two")
	if w.Renamed == nil || *w.Renamed != (struct{ From, To string }{"Job copy", "Job two"}) || w.Projection.Name != "Job two" || w.Presets == nil {
		t.Errorf("rename: %+v", w)
	}
	w = write(t, path, "", "projection", "rename", "Base", "Main")
	if len(w.Presets) == 0 {
		t.Errorf("a preset that names Base was not rewritten: %+v", w)
	}
	if w := write(t, path, "", "projection", "rename", "Job two", "Job two"); w.Changed {
		t.Errorf("rename to the same name changed: %+v", w)
	}
	write(t, path, "", "projection", "rename", "Main", "Base")

	w = write(t, path, "", "projection", "set-parent", "Job two", "Early")
	if *w.Projection.Parent != "Early" {
		t.Errorf("set-parent: %+v", w.Projection)
	}
	w = write(t, path, "", "projection", "set-parent", "Job two")
	if w.Projection.Parent != nil {
		t.Errorf("set-parent to top level: %+v", w.Projection)
	}

	w = write(t, path, "", "projection", "set-start", "Early", "2028-02")
	if w.Projection.Start == nil || *w.Projection.Start != "2028-02-01" {
		t.Errorf("set-start: %+v", w.Projection)
	}
	w = write(t, path, "", "projection", "set-start", "Early", "none")
	if w.Projection.Start != nil {
		t.Errorf("set-start none: %+v", w.Projection)
	}
	w = write(t, path, "", "projection", "set-override-history", "Early", "true")
	if w.Projection.OverrideHistory == nil || !*w.Projection.OverrideHistory {
		t.Errorf("set-override-history: %+v", w.Projection)
	}
	w = write(t, path, "", "projection", "set-override-history", "Early", "none")
	if w.Projection.OverrideHistory != nil {
		t.Errorf("set-override-history none: %+v", w.Projection)
	}
	w = write(t, path, "", "projection", "set-inflation", "Early", "0.035")
	if m := w.Projection.Modifications; len(m) != 1 || m[0].Target.Kind != "global" || m[0].Property != "inflation" || m[0].Schedule[0].Value != 0.035 {
		t.Errorf("set-inflation: %+v", w.Projection.Modifications)
	}
	if w := write(t, path, "", "projection", "set-inflation", "Early", "0.035"); w.Changed {
		t.Error("same inflation changed")
	}
	w = write(t, path, "", "projection", "set-inflation", "Early", "none")
	if len(w.Projection.Modifications) != 0 {
		t.Errorf("set-inflation none: %+v", w.Projection.Modifications)
	}

	w = write(t, path, "", "projection", "delete", "Job two")
	if w.Removed != "Job two" || w.Projection == nil || w.Projection.Name != "Job two" {
		t.Errorf("delete: %+v", w)
	}
	cliJSON(t, &list, "--db", path, "projection", "list")
	if slices.ContainsFunc(list, func(p struct{ Name string }) bool { return p.Name == "Job two" }) {
		t.Error("the deleted projection is still listed")
	}
	refusedWrite(t, path, "", 1, "has_children", "projection", "delete", "Base")
	refusedWrite(t, path, "", 1, "not_found", "projection", "delete", "Nowhere")
	refusedWrite(t, path, "", 1, "name_taken", "projection", "new", "Base")
	refusedWrite(t, path, "", 1, "invalid", "projection", "new", "  ")
	refusedWrite(t, path, "", 1, "not_found", "projection", "new", "X", "--parent", "Nowhere")
	refusedWrite(t, path, "", 2, "usage", "projection", "clone", "Base")
	refusedWrite(t, path, "", 2, "usage", "projection", "set-override-history", "Base", "maybe")
}

func TestCLIWriteModCommands(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Mods", "--parent", "Base")

	w := write(t, path, "", "mod", "add", "Mods", "--target", "account:checking", "--property", "growth", "--value", "0.02", "--from", "2028-01")
	m := w.Projection.Modifications[0]
	if m.Index != 0 || m.Target.Kind != "account" || m.Target.ID != "checking" || m.Schedule[0].From != "2028-01-01" || m.Schedule[0].Value != 0.02 {
		t.Errorf("mod add: %+v", m)
	}
	w = write(t, path, "", "mod", "add", "Mods", "--target", "item:salary-a", "--property", "amount", "--value", "100", "--at", "0")
	if m := w.Projection.Modifications[0]; m.Target.ID != "salary-a" || m.Schedule[0].Value != 100 {
		t.Errorf("mod add --at: %+v", w.Projection.Modifications)
	}
	// A schedule on stdin.
	sched := `[{"from":"2028-01-01","end":"2028-12-31","value":10},{"from":"2030-01-01","end":null,"value":20}]`
	w = write(t, path, sched, "mod", "add", "Mods", "--target", "global", "--property", "inflation")
	m = w.Projection.Modifications[2]
	if m.Target.Kind != "global" || len(m.Schedule) != 2 || m.Schedule[0].End == nil || *m.Schedule[0].End != "2028-12-31" || m.Schedule[1].Value != 20 {
		t.Errorf("mod add JSON: %+v", m)
	}
	// The same schedule from a file.
	file := filepath.Join(t.TempDir(), "sched.json")
	if err := os.WriteFile(file, []byte(sched), 0o600); err != nil {
		t.Fatal(err)
	}
	w = write(t, path, "", "mod", "add", "Mods", "--target", "asset:Retirement", "--property", "growth", "--json-file", file)
	if m := w.Projection.Modifications[3]; m.Target.Kind != "asset" || len(m.Schedule) != 2 {
		t.Errorf("mod add --json-file: %+v", m)
	}

	w = write(t, path, "", "mod", "edit", "Mods", "0", "--value", "0.03", "--from", "2029-01-01")
	if m := w.Projection.Modifications[0]; m.Schedule[0].From != "2029-01-01" || m.Schedule[0].Value != 0.03 {
		t.Errorf("mod edit: %+v", m)
	}
	w = write(t, path, "", "mod", "move", "Mods", "0", "3")
	if m := w.Projection.Modifications[3]; m.Target.ID != "salary-a" {
		t.Errorf("mod move: %+v", w.Projection.Modifications)
	}
	if w := write(t, path, "", "mod", "move", "Mods", "1", "1"); w.Changed {
		t.Error("a move to the same place changed")
	}
	w = write(t, path, "", "mod", "remove", "Mods", "3")
	if len(w.Projection.Modifications) != 3 {
		t.Errorf("mod remove: %+v", w.Projection.Modifications)
	}

	refusedWrite(t, path, "", 1, "invalid", "mod", "remove", "Mods", "9")
	refusedWrite(t, path, "", 1, "invalid", "mod", "move", "Mods", "0", "9")
	refusedWrite(t, path, "", 1, "invalid", "mod", "edit", "Mods", "9", "--value", "1")
	refusedWrite(t, path, "", 1, "invalid", "mod", "add", "Mods", "--target", "global", "--property", "inflation", "--value", "1", "--at", "9")
	refusedWrite(t, path, "", 1, "invalid", "mod", "add", "Mods", "--target", "global", "--property", "inflation", "--value", "1e16")
	refusedWrite(t, path, "", 1, "invalid", "mod", "add", "Mods", "--target", "global", "--property", "growth", "--value", "1")
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "add", "Mods", "--target", "item:nope", "--property", "amount", "--value", "1")
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "add", "Mods", "--target", "account:nope", "--property", "growth", "--value", "1")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Mods", "--target", "thing:x", "--property", "amount", "--value", "1")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Mods", "--target", "global", "--property", "inflation", "--value", "lots")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Mods", "--target", "global", "--property", "inflation")
	refusedWrite(t, path, "not json", 2, "usage", "mod", "add", "Mods", "--target", "global", "--property", "inflation")
	refusedWrite(t, path, `[{"from":"2030-01-01","value":1},{"from":"2029-01-01","value":2}]`, 2, "usage", "mod", "add", "Mods", "--target", "global", "--property", "inflation")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Mods", "--target", "global", "--property", "inflation", "--value", "1", "--from", "2028-13-01")
	refusedWrite(t, path, "", 1, "invalid", "mod", "edit", "Withdraw $100k", "0", "--value", "1")
}

func TestCLIWriteItemCommands(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Items", "--parent", "Base")

	w := write(t, path, "", "item", "add", "Items", "--kind", "expense", "--name", "Boat Slip", "--amount", "300", "--every", "monthly", "--on", "2027-03-20", "--account", "checking", "--indexed")
	it := w.Projection.Modifications[0]
	if it.Property != "add_item" || it.Target.ID != "boat-slip" || it.Item == nil || it.Item.Amount != 300 || it.Item.Kind != "expense" || *it.Item.Account != "checking" {
		t.Errorf("item add: %+v", it)
	}
	var items []struct {
		ID string
	}
	cliJSON(t, &items, "--db", path, "budget", "list")
	if slices.ContainsFunc(items, func(i struct{ ID string }) bool { return i.ID == "boat-slip" }) {
		t.Error("an added item is in the record's budget")
	}
	var show struct {
		Modifications []struct {
			Item struct {
				Recurrence struct{ Kind string }
				Day        *int
				Start      string
				Indexed    bool
			}
		}
	}
	cliJSON(t, &show, "--db", path, "projection", "show", "Items")
	got := show.Modifications[0].Item
	if got.Recurrence.Kind != "monthly" || got.Start != "2027-03-20" || !got.Indexed {
		t.Errorf("stored item %+v", got)
	}

	// The item as JSON, with a transfer to an account outside the record.
	in := `{"name":"Gift fund","kind":"transfer","amount":75,"recurrence":{"kind":"annual","month":12,"day":24},"start":"2027-12-24","from":"checking","to":"relatives"}`
	w = write(t, path, in, "item", "add", "Items")
	if it := w.Projection.Modifications[1]; it.Target.ID != "gift-fund" || it.Item.Kind != "transfer" {
		t.Errorf("item add JSON: %+v", it)
	}

	w = write(t, path, "", "item", "edit", "Items", "Boat Slip", "--amount", "350", "--end", "2030-12-31")
	if it := w.Projection.Modifications[0]; it.Item.Amount != 350 || it.Item.ID != "boat-slip" || it.Item.Name != "Boat Slip" {
		t.Errorf("item edit: %+v", it)
	}
	w = write(t, path, "", "item", "edit", "Items", "boat-slip", "--every", "one-time", "--on", "2028-01-15", "--end", "none")
	if w.Projection.Modifications[0].Item.Amount != 350 {
		t.Errorf("item edit recurrence: %+v", w.Projection.Modifications[0])
	}
	w = write(t, path, `{"name":"Boat Slip","kind":"expense","amount":20,"recurrence":{"kind":"monthly","day":2},"account":"checking"}`, "item", "edit", "Items", "boat-slip")
	if w.Projection.Modifications[0].Item.Amount != 20 {
		t.Errorf("item edit JSON: %+v", w.Projection.Modifications[0])
	}

	w = write(t, path, "", "item", "end", "Items", "salary-a", "2028-01-01")
	if m := w.Projection.Modifications[2]; m.Property != "amount" || m.Target.ID != "salary-a" || m.Schedule[0].From != "2028-01-01" || m.Schedule[0].Value != 0 {
		t.Errorf("item end: %+v", m)
	}
	write(t, path, "", "item", "end", "Items", "Boat Slip", "2029-01")

	refusedWrite(t, path, "", 1, "name_taken", "item", "add", "Items", "--kind", "income", "--name", "x", "--amount", "1", "--every", "one-time", "--on", "2027-01-01", "--account", "checking", "--id", "salary-a")
	refusedWrite(t, path, "", 1, "invalid", "item", "add", "Items", "--kind", "income", "--name", "x", "--amount", "0", "--every", "one-time", "--on", "2027-01-01", "--account", "checking")
	refusedWrite(t, path, "", 1, "invalid", "item", "add", "Items", "--kind", "income", "--name", "x", "--amount", "5", "--every", "monthly", "--day", "40", "--on", "2027-01-01", "--account", "checking")
	refusedWrite(t, path, "", 1, "unknown_target", "item", "add", "Items", "--kind", "income", "--name", "x", "--amount", "5", "--every", "one-time", "--on", "2027-01-01", "--account", "nope")
	refusedWrite(t, path, "", 1, "invalid", "item", "add", "Items", "--kind", "income", "--name", "x", "--amount", "5", "--every", "monthly", "--on", "2027-01-01", "--end", "2026-01-01", "--account", "checking")
	refusedWrite(t, path, "", 2, "usage", "item", "add", "Items", "--kind", "income", "--name", "x")
	refusedWrite(t, path, "", 2, "usage", "item", "add", "Items", "--kind", "gift", "--name", "x", "--amount", "5", "--every", "monthly", "--on", "2027-01-01")
	refusedWrite(t, path, "", 2, "usage", "item", "add", "Items")
	refusedWrite(t, path, "", 2, "usage", "item", "add", "Items", "--kind", "income", "--name", "x", "--amount", "many", "--every", "monthly", "--on", "2027-01-01")
	refusedWrite(t, path, "", 1, "unknown_target", "item", "edit", "Items", "salary-a", "--amount", "1")
	refusedWrite(t, path, "", 1, "unknown_target", "item", "end", "Items", "nope", "2028-01-01")
	refusedWrite(t, path, "", 2, "usage", "item", "end", "Items", "salary-a", "soon")
}

func TestCLIWriteDryRunSavesNothing(t *testing.T) {
	path := seededFile(t)
	before := dbSnapshot(t, path)
	cmds := [][]string{
		{"projection", "new", "Dry"},
		{"projection", "clone", "New job", "--as", "Dry"},
		{"projection", "rename", "Base", "Main"},
		{"projection", "set-parent", "New job"},
		{"projection", "delete", "Withdraw $100k"},
		{"projection", "set-start", "New job", "2030-01-01"},
		{"projection", "set-override-history", "New job", "true"},
		{"projection", "set-inflation", "New job", "0.05"},
		{"mod", "add", "New job", "--target", "item:salary-a", "--property", "amount", "--value", "1"},
		{"mod", "edit", "New job", "0", "--value", "1"},
		{"mod", "move", "New job", "0", "1"},
		{"mod", "remove", "New job", "0"},
		{"item", "add", "New job", "--kind", "expense", "--name", "Dry", "--amount", "5", "--every", "one-time", "--on", "2028-01-01", "--account", "checking"},
		{"item", "edit", "Withdraw $100k", "early-401k-withdrawal", "--amount", "5"},
		{"item", "end", "New job", "salary-a", "2028-01-01"},
	}
	for _, args := range cmds {
		w := write(t, path, "", append(slices.Clone(args), "--dry-run")...)
		if !w.DryRun || !w.Changed || w.Projection == nil {
			t.Errorf("finplan %v: %+v", args, w)
		}
		if !reflect.DeepEqual(before, dbSnapshot(t, path)) {
			t.Fatalf("finplan %v --dry-run changed the database", args)
		}
	}
	// A dry run says what the real run does.
	dry := write(t, path, "", "item", "end", "New job", "salary-a", "2028-01-01", "--dry-run")
	real := write(t, path, "", "item", "end", "New job", "salary-a", "2028-01-01")
	if !reflect.DeepEqual(dry.Projection, real.Projection) || real.DryRun {
		t.Errorf("dry %+v, real %+v", dry, real)
	}
	if reflect.DeepEqual(before, dbSnapshot(t, path)) {
		t.Error("the real run saved nothing")
	}
}

func TestCLIWriteNeverCreatesDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none.db")
	code, out := wcli(t, "", "--db", path, "projection", "new", "X")
	if code != 1 || !strings.Contains(string(out), `"no_database"`) {
		t.Errorf("exit %d: %s", code, out)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a write command created the database")
	}
	// A demo database is writable and thrown away.
	var w written
	cliJSON(t, &w, "--demo", "projection", "new", "X")
	if !w.Changed {
		t.Errorf("demo: %+v", w)
	}
}

func TestCLIEarlyWithdrawalScenario(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Early", "--parent", "Base")
	write(t, path, "", "item", "add", "Early", "--kind", "transfer", "--name", "Early 401k draw", "--amount", "20000", "--every", "one-time", "--on", "2027-06-01", "--from", "401k", "--to", "checking")
	base := lastValue(t, path, "401k", "Base", "2027-05", "2027-12")
	early := lastValue(t, path, "401k", "Early", "2027-05", "2027-12")
	if early >= base-19000 {
		t.Errorf("401k at year end: %v with the withdrawal, %v without", early, base)
	}
	if before := lastValue(t, path, "401k", "Early", "2027-05", "2027-05"); before != lastValue(t, path, "401k", "Base", "2027-05", "2027-05") {
		t.Error("the withdrawal changed the balance before its date")
	}
}

func TestCLINewJobScenario(t *testing.T) {
	path := seededFile(t)
	base := lastValue(t, path, "checking", "Base", "2027-01", "2027-12")
	write(t, path, "", "projection", "new", "Job B", "--parent", "Base")
	write(t, path, "", "item", "end", "Job B", "salary-a", "2027-03-01")
	ended := lastValue(t, path, "checking", "Job B", "2027-01", "2027-12")
	if ended >= base {
		t.Errorf("checking at year end: %v with the job ended, %v without", ended, base)
	}
	write(t, path, "", "item", "add", "Job B", "--kind", "income", "--name", "Salary B", "--amount", "50000", "--every", "monthly", "--on", "2027-03-15", "--account", "checking")
	hired := lastValue(t, path, "checking", "Job B", "2027-01", "2027-12")
	if hired <= base {
		t.Errorf("checking at year end: %v with the new job, %v without", hired, base)
	}
	var bd struct{ Lines []struct{ Cause string } }
	cliJSON(t, &bd, "--db", path, "breakdown", "checking", "--from", "2027-01", "--to", "2027-12", "--projection", "Job B")
	if len(bd.Lines) == 0 {
		t.Error("the breakdown is empty")
	}
}

func TestCLIModOfParentsItemScenario(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Smaller", "--parent", "Withdraw $100k")
	full := lastValue(t, path, "401k", "Withdraw $100k", "2027-05", "2027-12")
	// By name as well as by id.
	w := write(t, path, "", "mod", "add", "Smaller", "--target", "item:Early 401k withdrawal", "--property", "amount", "--value", "1000", "--from", "2027-06-01")
	if w.Projection.Modifications[0].Target.ID != "early-401k-withdrawal" {
		t.Errorf("target %+v", w.Projection.Modifications[0].Target)
	}
	if got := lastValue(t, path, "401k", "Smaller", "2027-05", "2027-12"); got < full+90000 {
		t.Errorf("401k at year end: %v with the smaller withdrawal, %v with the full one", got, full)
	}
	var b balanceOut
	cliJSON(t, &b, "--db", path, "balance", "401k", "--from", "2027-05", "--to", "2027-12", "--projection", "Smaller")
	if len(b.Diagnostics.Skipped) != 0 {
		t.Errorf("skipped %v", b.Diagnostics.Skipped)
	}
}

func TestCLIRefusedEditsLeaveFileAlone(t *testing.T) {
	path := seededFile(t)
	refusedWrite(t, path, "", 1, "cycle", "projection", "set-parent", "Base", "New job")
	refusedWrite(t, path, "", 1, "cycle", "projection", "set-parent", "Base", "Base")
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "add", "New job", "--target", "item:no-such-item", "--property", "amount", "--value", "5")
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "add", "New job", "--target", "item:early-401k-withdrawal", "--property", "amount", "--value", "5")
	refusedWrite(t, path, "", 2, "usage", "projection", "set-start", "Base", "2027-13-45")
	refusedWrite(t, path, "", 2, "usage", "item", "end", "New job", "salary-a", "2027-02-30")
	// A target added only later in the list is not there yet.
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "add", "Withdraw $100k", "--target", "item:early-401k-withdrawal", "--property", "amount", "--value", "5", "--at", "0")
	// Nothing may be left pointing at an item that goes away.
	write(t, path, "", "projection", "new", "Child", "--parent", "Withdraw $100k")
	write(t, path, "", "mod", "add", "Child", "--target", "item:early-401k-withdrawal", "--property", "amount", "--value", "5")
	refusedWrite(t, path, "", 1, "unknown_target", "mod", "remove", "Withdraw $100k", "0")
	refusedWrite(t, path, "", 1, "unknown_target", "projection", "set-parent", "Child", "Base")
	// A one-time item needs its date.
	refusedWrite(t, path, `{"name":"Gift","kind":"income","amount":500,"account":"checking","recurrence":{"kind":"one-time"}}`, 1, "invalid", "item", "add", "Base")
}

func TestCLIWriteHelp(t *testing.T) {
	var help struct {
		Commands []struct {
			Name  string
			Write bool
			Input string
			Flags []struct{ Name, Type string }
		}
	}
	cliJSON(t, &help, "help", "--json")
	writes := map[string]bool{}
	types := map[string]bool{}
	for _, c := range help.Commands {
		if !c.Write {
			continue
		}
		writes[c.Name] = true
		hasDry := false
		for _, f := range c.Flags {
			hasDry = hasDry || f.Name == "dry-run"
			types[f.Type] = true
		}
		if !hasDry {
			t.Errorf("%s has no --dry-run", c.Name)
		}
	}
	for _, name := range []string{
		"projection new", "projection clone", "projection rename", "projection set-parent", "projection delete",
		"projection set-start", "projection set-override-history", "projection set-inflation",
		"mod add", "mod edit", "mod move", "mod remove", "item add", "item edit", "item end",
	} {
		if !writes[name] {
			t.Errorf("help does not list the write command %q", name)
		}
	}
	if !types["number"] || !types["int"] {
		t.Errorf("flag types %v", types)
	}
}
