// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/joeriedel/finplan/internal/demodata"
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
			Mode     string
			Schedule []struct {
				From  string
				End   *string
				Value float64
			}
			Item *struct {
				ID, Name, Kind string
				Amount         float64
				Growth         float64
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

// snapshot is everything a write command may change: the record's documents, its entries and the presets.
type snapshot struct {
	Projections []*model.Projection
	Accounts    []*model.Account
	Assets      []*model.Asset
	Budget      model.Budget
	Entries     map[model.AccountID][]store.Entry
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
	entries := map[model.AccountID][]store.Entry{}
	for _, a := range rec.Accounts {
		if entries[a.ID], err = st.Entries(a.ID); err != nil {
			t.Fatal(err)
		}
	}
	return snapshot{rec.Projections, rec.Accounts, rec.Assets, rec.Budget, entries, rows}
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
	write(t, path, "", "mod", "add", "Early", "--target", "global", "--property", "inflation", "--mode", "scale", "--value", "1.2")
	w = write(t, path, "", "projection", "set-inflation", "Early", "0.03")
	if m := w.Projection.Modifications; len(m) != 2 || m[0].Mode == "scale" || m[0].Schedule[0].Value != 0.03 || m[1].Mode != "scale" {
		t.Errorf("set-inflation beside a scale: %+v", m)
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
		"asset new", "asset rename", "asset delete", "asset add-member", "asset remove-member",
		"asset add-default", "asset remove-default", "asset set-param", "account new", "account edit",
		"entry add-snapshot", "entry add-transaction", "entry correct", "entry void",
		"budget add", "budget edit", "budget end",
	} {
		if !writes[name] {
			t.Errorf("help does not list the write command %q", name)
		}
	}
	if !types["number"] || !types["int"] {
		t.Errorf("flag types %v", types)
	}
}

// breakdownLine is the projected amount of one cause in the breakdown of the target over (from, to], under the projection if not empty.
func breakdownLine(t *testing.T, path, target, projection, from, to, cause string) float64 {
	t.Helper()
	args := []string{"--db", path, "breakdown", target, "--from", from, "--to", to}
	if projection != "" {
		args = append(args, "--projection", projection)
	}
	var bd struct {
		Lines []struct {
			Cause     string
			Projected float64
		}
	}
	cliJSON(t, &bd, args...)
	for _, l := range bd.Lines {
		if l.Cause == cause {
			return l.Projected
		}
	}
	return 0
}

func closeTo(got, want float64) bool { return math.Abs(got-want) <= 1e-6*math.Max(1, math.Abs(want)) }

// TestCLICutDiningScenario cuts dining by 30% from 2026 with a scale of 0.7, then changes the base amount, and the projected amount follows it.
func TestCLICutDiningScenario(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Cut dining")
	// Dining is 900 on the 6th, indexed at 2.5% from the demo's today; the card's expenses over Jan..Jun 2027 are dining and others.
	from, to := "2026-12-31", "2027-06-30"
	dining := func(amount float64) float64 {
		sum := 0.0
		for m := time.January; m <= time.June; m++ {
			sum += amount * math.Pow(1.025, float64(model.D(2027, m, 6)-demodata.FamilyToday)/model.DaysPerYear)
		}
		return sum
	}
	cut := func() float64 {
		return breakdownLine(t, path, "card", "Cut dining", from, to, "expense") - breakdownLine(t, path, "card", "", from, to, "expense")
	}
	if got := cut(); got != 0 {
		t.Fatalf("an empty projection changes the card's expenses by %v", got)
	}

	args := []string{"mod", "add", "Cut dining", "--target", "item:dining", "--property", "amount", "--mode", "scale", "--value", "0.7", "--from", "2026-01-01"}
	dry := write(t, path, "", append(args, "--dry-run")...)
	if !dry.DryRun || len(dry.Projection.Modifications) != 1 || cut() != 0 {
		t.Fatalf("dry run: %+v", dry)
	}
	w := write(t, path, "", args...)
	m := w.Projection.Modifications[0]
	if m.Mode != "scale" || m.Property != "amount" || m.Target.ID != "dining" || m.Schedule[0].Value != 0.7 || m.Schedule[0].From != "2026-01-01" {
		t.Errorf("stored %+v", m)
	}
	// Projected expenses are negative: the card pays 30% less of dining.
	if got, want := cut(), 0.3*dining(900); !closeTo(got, want) {
		t.Errorf("30%% less dining over six months: got %v, want %v", got, want)
	}

	data(t, path, "", "budget", "edit", "dining", "--amount", "1200")
	if got, want := cut(), 0.3*dining(1200); !closeTo(got, want) {
		t.Errorf("after the base changes to 1200: got %v, want %v", got, want)
	}

	// The mode is shown, and edits.
	var show struct {
		Modifications []struct{ Mode string }
	}
	cliJSON(t, &show, "--db", path, "projection", "show", "Cut dining")
	if show.Modifications[0].Mode != "scale" {
		t.Errorf("projection show: %+v", show)
	}
	w = write(t, path, "", "mod", "edit", "Cut dining", "0", "--mode", "set")
	if !w.Changed || w.Projection.Modifications[0].Mode != "set" || w.Projection.Modifications[0].Schedule[0].Value != 0.7 {
		t.Errorf("mod edit --mode set: %+v", w)
	}
	if w := write(t, path, "", "mod", "edit", "Cut dining", "0", "--mode", "set"); w.Changed {
		t.Error("the same mode changed")
	}
	// As a set, 0.7 is a dining of 0.70 a month.
	if got, want := cut(), dining(1200)-dining(0.7); !closeTo(got, want) {
		t.Errorf("as a set: got %v, want %v", got, want)
	}
	w = write(t, path, "", "mod", "edit", "Cut dining", "0", "--mode", "scale", "--value", "0.5")
	if m := w.Projection.Modifications[0]; m.Mode != "scale" || m.Schedule[0].Value != 0.5 {
		t.Errorf("mod edit --mode scale --value: %+v", m)
	}
	refusedWrite(t, path, "", 1, "invalid", "mod", "add", "Cut dining", "--target", "item:dining", "--property", "amount", "--mode", "scale", "--value", "-0.5")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Cut dining", "--target", "item:dining", "--property", "amount", "--mode", "mix", "--value", "0.5")
}

// TestCLIRaisesScenario adds a salary with 3% yearly raises to a new job.
func TestCLIRaisesScenario(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Job B")
	args := []string{"item", "add", "Job B", "--kind", "income", "--name", "Salary B", "--amount", "5000", "--every", "monthly", "--on", "2027-03-01", "--account", "checking", "--growth", "0.03"}
	dry := write(t, path, "", append(args, "--dry-run")...)
	if !dry.DryRun || dry.Projection.Modifications[0].Item.Growth != 0.03 {
		t.Fatalf("dry run: %+v", dry)
	}
	var shown struct{ Modifications []any }
	cliJSON(t, &shown, "--db", path, "projection", "show", "Job B")
	if len(shown.Modifications) != 0 {
		t.Fatal("a dry run saved the item")
	}
	w := write(t, path, "", args...)
	if got := w.Projection.Modifications[0].Item.Growth; got != 0.03 {
		t.Errorf("stored growth %v", got)
	}

	// Salary B is paid on the 1st from 2027-03-01 and steps up 3% on 2028-03-01 and 2029-03-01, not continuously.
	gross := func(from, to string) float64 {
		return breakdownLine(t, path, "checking", "Job B", from, to, "income") - breakdownLine(t, path, "checking", "", from, to, "income")
	}
	for _, tc := range []struct {
		from, to string
		want     float64
	}{
		{"2027-02-28", "2027-03-01", 5000},
		{"2027-02-28", "2028-02-29", 12 * 5000},
		{"2028-01-31", "2028-02-29", 5000},
		{"2028-02-29", "2028-03-01", 5150},
		{"2028-02-29", "2029-02-28", 12 * 5150},
		{"2029-02-28", "2030-02-28", 12 * 5304.5},
	} {
		if got := gross(tc.from, tc.to); !closeTo(got, tc.want) {
			t.Errorf("salary over (%s, %s]: got %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}

	// Indexed, the raises multiply with inflation (2.5% a year from the demo's today).
	w = write(t, path, "", "item", "edit", "Job B", "salary-b-2", "--indexed")
	if !w.Changed {
		t.Fatal("item edit --indexed changed nothing")
	}
	indexed := func(from, to model.Date) float64 {
		sum := 0.0
		for d := from; d <= to; d++ {
			if _, _, day := d.YMD(); day != 1 || d < model.D(2027, time.March, 1) {
				continue
			}
			raises := 0
			for y := 2028; y <= 2029; y++ {
				if d >= model.D(y, time.March, 1) {
					raises++
				}
			}
			sum += 5000 * math.Pow(1.03, float64(raises)) * math.Pow(1.025, float64(d-demodata.FamilyToday)/model.DaysPerYear)
		}
		return sum
	}
	if got, want := gross("2028-02-29", "2029-02-28"), indexed(model.D(2028, time.March, 1), model.D(2029, time.February, 28)); !closeTo(got, want) {
		t.Errorf("raises with inflation: got %v, want %v", got, want)
	}

	// A growth modification of the item, in either mode.
	write(t, path, "", "mod", "add", "Job B", "--target", "item:salary-b-2", "--property", "growth", "--value", "0.1", "--from", "2029-01-01")
	if got, want := gross("2028-02-29", "2029-02-28"), indexed(model.D(2028, time.March, 1), model.D(2029, time.February, 28)); !closeTo(got, want) {
		t.Errorf("a later rate leaves earlier steps: got %v, want %v", got, want)
	}
	if got := gross("2029-02-28", "2029-03-01"); !closeTo(got, 5000*1.03*1.1*math.Pow(1.025, float64(model.D(2029, time.March, 1)-demodata.FamilyToday)/model.DaysPerYear)) {
		t.Errorf("the 2029 step at the modification's rate: got %v", got)
	}
	w = write(t, path, "", "mod", "add", "Job B", "--target", "item:salary-b-2", "--property", "growth", "--mode", "scale", "--value", "0", "--from", "2029-01-01")
	if m := w.Projection.Modifications[2]; m.Mode != "scale" || m.Property != "growth" {
		t.Errorf("scaled growth modification: %+v", m)
	}
	if got := gross("2029-02-28", "2029-03-01"); !closeTo(got, 5000*1.03*math.Pow(1.025, float64(model.D(2029, time.March, 1)-demodata.FamilyToday)/model.DaysPerYear)) {
		t.Errorf("a scale of zero ends the raises: got %v", got)
	}
	refusedWrite(t, path, "", 1, "invalid", "mod", "add", "Job B", "--target", "item:salary-b-2", "--property", "growth", "--value", "-1")
	refusedWrite(t, path, "", 1, "invalid", "item", "edit", "Job B", "salary-b-2", "--growth", "-1.5")
}

func TestCLIBudgetGrowth(t *testing.T) {
	path := seededFile(t)
	w := data(t, path, "", "budget", "add", "--kind", "income", "--name", "Tutoring", "--amount", "400", "--every", "monthly", "--on", "2027-01-15", "--account", "checking", "--growth", "0.04")
	if w.Item.Growth != 0.04 {
		t.Errorf("add: %+v", w.Item)
	}
	var list []struct {
		ID     string
		Growth float64
	}
	cliJSON(t, &list, "--db", path, "budget", "list")
	if got := list[len(list)-1]; got.ID != "tutoring" || got.Growth != 0.04 {
		t.Errorf("budget list: %+v", got)
	}
	if got := list[0].Growth; got != 0 {
		t.Errorf("an item without raises lists growth %v", got)
	}
	w = data(t, path, "", "budget", "edit", "tutoring", "--growth", "0.05")
	if w.Item.Growth != 0.05 {
		t.Errorf("edit: %+v", w.Item)
	}
	w = data(t, path, "", "budget", "edit", "tutoring", "--amount", "450")
	if w.Item.Growth != 0.05 {
		t.Errorf("an edit of the amount drops the growth: %+v", w.Item)
	}
	w = data(t, path, `{"name":"Gift","kind":"income","amount":500,"growth":0.02,"account":"checking","recurrence":{"kind":"one-time","anchor":"2027-03-01"}}`, "budget", "add")
	if w.Item.Growth != 0.02 {
		t.Errorf("add JSON: %+v", w.Item)
	}
	w = data(t, path, "", "budget", "edit", "tutoring", "--growth", "0")
	if w.Item.Growth != 0 {
		t.Errorf("edit --growth 0: %+v", w.Item)
	}
	refusedWrite(t, path, "", 1, "invalid", "budget", "edit", "tutoring", "--growth", "-1")
}

func TestCLIDefaultBalanceScenario(t *testing.T) {
	path := seededFile(t)
	write(t, path, "", "projection", "new", "Defaults", "--parent", "Base")
	balance := func(proj, target, date string) (float64, string) {
		t.Helper()
		args := []string{"--db", path, "balance", target, "--from", date, "--to", date}
		if proj != "" {
			args = append(args, "--projection", proj)
		}
		var b balanceOut
		cliJSON(t, &b, args...)
		return b.Samples[0].Value, b.Samples[0].Status
	}
	// The Home has no default and no actual data before 2024-12-15.
	const inside, outside, withData = "2024-03-01", "2024-09-01", "2025-06-01"
	baseInside, _ := balance("", "home", inside)
	baseData, dataStatus := balance("", "home", withData)
	if baseInside != 0 || dataStatus != "actual" {
		t.Fatalf("unexpected demo data: %v on %s, %q on %s", baseInside, inside, dataStatus, withData)
	}

	before := dbSnapshot(t, path)
	code, _ := wcli(t, "", "--db", path, "mod", "add", "Defaults", "--target", "account:home", "--property", "default", "--value", "310000", "--from", "2024-01-01", "--end", "2024-06-30", "--dry-run")
	if code != 0 || !reflect.DeepEqual(before, dbSnapshot(t, path)) {
		t.Errorf("--dry-run: exit %d, or it wrote", code)
	}

	w := write(t, path, "", "mod", "add", "Defaults", "--target", "account:home", "--property", "default", "--value", "310000", "--from", "2024-01-01", "--end", "2024-06-30")
	if m := w.Projection.Modifications[0]; m.Property != "default" || m.Schedule[0].End == nil || *m.Schedule[0].End != "2024-06-30" {
		t.Errorf("mod add: %+v", m)
	}
	if v, _ := balance("Defaults", "home", inside); v != 310000 {
		t.Errorf("in the range: %v, want 310000", v)
	}
	if v, _ := balance("Defaults", "home", outside); v != 0 {
		t.Errorf("after the range: %v, want 0", v)
	}
	if v, s := balance("Defaults", "home", withData); v != baseData || s != "actual" {
		t.Errorf("with actual data: %v %q, want %v actual", v, s, baseData)
	}
	if v, _ := balance("", "home", inside); v != 0 {
		t.Errorf("the base projection changed: %v", v)
	}
	if v, _ := balance("Base", "home", inside); v != 0 {
		t.Errorf("the parent changed: %v", v)
	}

	// A scale multiplies the default beneath it.
	write(t, path, "", "mod", "add", "Defaults", "--target", "account:home", "--property", "default", "--mode", "scale", "--value", "0.5", "--from", "2024-04-01", "--end", "2024-04-30")
	if v, _ := balance("Defaults", "home", "2024-04-15"); v != 155000 {
		t.Errorf("scaled: %v, want 155000", v)
	}

	refusedWrite(t, path, "", 1, "invalid", "mod", "add", "Defaults", "--target", "item:salary-a", "--property", "default", "--value", "1")
	refusedWrite(t, path, "", 1, "invalid", "mod", "add", "Defaults", "--target", "global", "--property", "default", "--value", "1")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Defaults", "--target", "account:home", "--property", "default", "--value", "1", "--end", "2024-06-30")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Defaults", "--target", "account:home", "--property", "default", "--value", "1", "--from", "2024-07-01", "--end", "2024-06-30")
	refusedWrite(t, path, "", 2, "usage", "mod", "add", "Defaults", "--target", "account:home", "--property", "default", "--end", "2024-06-30")
}
