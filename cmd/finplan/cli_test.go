// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/joeriedel/finplan/internal/demodata"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// cli runs a command line and returns its exit code and stdout.
func cli(t *testing.T, args ...string) (int, []byte) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runCLI(args, strings.NewReader(""), &out, &errOut)
	if errOut.Len() > 0 {
		t.Errorf("finplan %v wrote to stderr: %s", args, errOut.String())
	}
	return code, out.Bytes()
}

// cliJSON runs a command that must succeed and decodes its output into v.
func cliJSON(t *testing.T, v any, args ...string) {
	t.Helper()
	code, out := cli(t, args...)
	if code != 0 {
		t.Fatalf("finplan %v exited %d: %s", args, code, out)
	}
	if err := json.Unmarshal(out, v); err != nil {
		t.Fatalf("finplan %v: output is not the expected JSON: %v\n%s", args, err, out)
	}
}

// cliFail runs a command that must fail with the exit code and returns the error's code and message.
func cliFail(t *testing.T, wantExit int, args ...string) (string, string) {
	t.Helper()
	code, out := cli(t, args...)
	if code != wantExit {
		t.Fatalf("finplan %v exited %d, want %d: %s", args, code, wantExit, out)
	}
	var e struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(out, &e); err != nil || e.Error.Code == "" {
		t.Fatalf("finplan %v: output is not a JSON error: %s", args, out)
	}
	return e.Error.Code, e.Error.Message
}

// anyJSON decodes loosely, for checking keys and nulls.
func anyJSON(t *testing.T, args ...string) any {
	t.Helper()
	var v any
	cliJSON(t, &v, args...)
	return v
}

func TestCLIAccounts(t *testing.T) {
	var accts []struct{ ID, Name, Type, Tax, Owner string }
	cliJSON(t, &accts, "--demo", "account", "list")
	i := slices.IndexFunc(accts, func(a struct{ ID, Name, Type, Tax, Owner string }) bool { return a.ID == "401k" })
	if i < 0 || accts[i].Name != "401k" || accts[i].Tax != "tax-deferred" {
		t.Fatalf("accounts = %+v", accts)
	}

	var entries []map[string]any
	cliJSON(t, &entries, "account", "entries", "401k", "--demo")
	if len(entries) == 0 {
		t.Fatal("no entries")
	}
	var sawSnapshot, sawTx bool
	for _, e := range entries {
		for _, k := range []string{"id", "kind", "date", "amount", "tx_kind", "counter", "memo"} {
			if _, ok := e[k]; !ok {
				t.Fatalf("entry lacks %q: %v", k, e)
			}
		}
		switch e["kind"] {
		case "snapshot":
			sawSnapshot = true
			if e["tx_kind"] != nil || e["counter"] != nil || e["memo"] != nil {
				t.Errorf("snapshot has transaction fields: %v", e)
			}
		case "transaction":
			sawTx = true
		}
	}
	if !sawSnapshot || !sawTx {
		t.Errorf("snapshot %v, transaction %v", sawSnapshot, sawTx)
	}

	var hist struct {
		ID        int64
		Revisions []map[string]any
	}
	id := int64(entries[0]["id"].(float64))
	cliJSON(t, &hist, "--demo", "entry", "history", fmt.Sprint(id))
	if hist.ID != id || len(hist.Revisions) != 1 || hist.Revisions[0]["void"] != false {
		t.Errorf("history = %+v", hist)
	}
	for _, k := range []string{"date", "amount", "tx_kind", "counter", "memo", "made_at", "note", "void"} {
		if _, ok := hist.Revisions[0][k]; !ok {
			t.Errorf("revision lacks %q", k)
		}
	}
	if !strings.Contains(hist.Revisions[0]["made_at"].(string), "T") {
		t.Errorf("made_at = %v", hist.Revisions[0]["made_at"])
	}
	if code, msg := cliFail(t, 1, "--demo", "entry", "history", "99999999"); code != "not_found" {
		t.Errorf("unknown entry: %s %s", code, msg)
	}
	if code, _ := cliFail(t, 2, "--demo", "entry", "history", "abc"); code != "usage" {
		t.Errorf("bad entry id: %s", code)
	}
	if code, _ := cliFail(t, 1, "--demo", "account", "entries", "nope"); code != "not_found" {
		t.Errorf("unknown account: %s", code)
	}
}

func TestCLIAssets(t *testing.T) {
	var assets []struct {
		ID      string
		Name    string
		Members []struct {
			Seq     int
			Member  string
			Account *string
			Asset   *string
			Rule    *struct{ Types, Tax []string }
		}
		Invalid []struct {
			Seq        int
			Member     string
			Duplicates []string
			Against    int
		}
		Error *string
	}
	cliJSON(t, &assets, "--demo", "asset", "list")
	byID := map[string]int{}
	for i, a := range assets {
		byID[a.ID] = i
		if a.Error != nil {
			t.Errorf("asset %s: error %s", a.ID, *a.Error)
		}
	}
	liquid := assets[byID[string(demodata.Liquid)]]
	if len(liquid.Invalid) != 1 || len(liquid.Invalid[0].Duplicates) == 0 || !strings.Contains(liquid.Invalid[0].Member, "checking") {
		t.Errorf("liquid invalid = %+v", liquid.Invalid)
	}
	cash := assets[byID[string(demodata.AllCash)]]
	if len(cash.Members) != 1 || cash.Members[0].Rule == nil || len(cash.Invalid) != 0 {
		t.Errorf("all-cash = %+v", cash)
	}
	acct := assets[byID["acct:401k"]]
	if len(acct.Members) != 1 || acct.Members[0].Account == nil || *acct.Members[0].Account != "401k" {
		t.Errorf("default asset = %+v", acct)
	}
}

func TestCLIBudgetAndProjections(t *testing.T) {
	var items []map[string]any
	cliJSON(t, &items, "--demo", "budget", "list")
	if len(items) == 0 {
		t.Fatal("no budget items")
	}
	for _, k := range []string{"id", "name", "kind", "amount", "recurrence", "start", "end", "indexed", "account", "from", "to"} {
		if _, ok := items[0][k]; !ok {
			t.Errorf("budget item lacks %q", k)
		}
	}
	for _, it := range items {
		r := it["recurrence"].(map[string]any)
		if r["kind"] == "monthly" && (r["day"] == nil || r["anchor"] != nil) {
			t.Errorf("monthly recurrence = %v", r)
		}
	}

	var projs []struct {
		Name              string
		Parent            *string
		Start             *string
		OverrideHistory   *bool `json:"override_history"`
		ModificationCount int   `json:"modification_count"`
	}
	cliJSON(t, &projs, "--demo", "projection", "list")
	if len(projs) != 3 || projs[0].Name != "Base" || projs[0].Parent != nil || projs[0].Start == nil || *projs[0].Start != "2026-11-08" {
		t.Errorf("projections = %+v", projs)
	}
	if projs[2].Name != "Withdraw $100k" || *projs[2].Parent != "Base" || projs[2].ModificationCount != 1 {
		t.Errorf("projections = %+v", projs)
	}

	var show struct {
		Name          string
		Parent        string
		AssetSettings []any `json:"asset_settings"`
		Modifications []struct {
			Index    int
			Target   struct{ Kind, ID string }
			Property string
			Schedule []any
			Item     map[string]any
		}
	}
	cliJSON(t, &show, "--demo", "projection", "show", "Withdraw $100k")
	if show.Name != "Withdraw $100k" || show.Parent != "Base" || show.AssetSettings == nil || len(show.Modifications) != 1 {
		t.Fatalf("show = %+v", show)
	}
	m := show.Modifications[0]
	if m.Index != 0 || m.Property != "add_item" || m.Target.ID != "early-401k-withdrawal" || m.Schedule == nil || m.Item == nil {
		t.Fatalf("mod = %+v", m)
	}
	r := m.Item["recurrence"].(map[string]any)
	if m.Item["amount"] != 100000.0 || m.Item["from"] != "401k" || m.Item["to"] != "checking" || r["kind"] != "one-time" || r["anchor"] != "2027-06-01" {
		t.Errorf("item = %v", m.Item)
	}

	var newJob struct {
		Modifications []struct {
			Property string
			Schedule []struct {
				From  *string
				End   *string
				Value float64
			}
		}
	}
	cliJSON(t, &newJob, "--demo", "projection", "show", "New job")
	if len(newJob.Modifications) != 2 || len(newJob.Modifications[1].Schedule) != 3 || newJob.Modifications[1].Schedule[0].From != nil || newJob.Modifications[1].Schedule[0].Value != 0.04 {
		t.Errorf("new job = %+v", newJob)
	}
	if code, _ := cliFail(t, 1, "--demo", "projection", "show", "nope"); code != "not_found" {
		t.Errorf("unknown projection: %s", code)
	}
}

type balanceOut struct {
	Target     string
	Projection *string
	From, To   string
	Step       string
	Samples    []struct {
		Date   string
		Value  float64
		Status string
	}
	Diagnostics struct{ Skipped, Overlaps []any }
}

func TestCLIBalance(t *testing.T) {
	var b balanceOut
	cliJSON(t, &b, "--demo", "balance", "401k", "--from", "2027-01", "--to", "2027-03")
	if b.Target != "acct:401k" || b.Projection != nil || b.From != "2027-01-01" || b.To != "2027-03-31" || b.Step != "month" {
		t.Errorf("balance = %+v", b)
	}
	if len(b.Samples) != 3 || b.Samples[0].Date != "2027-01-01" || b.Samples[2].Date != "2027-03-01" {
		t.Errorf("samples = %+v", b.Samples)
	}
	if b.Samples[0].Status != "projected" {
		t.Errorf("status = %q", b.Samples[0].Status)
	}
	if b.Diagnostics.Skipped == nil || b.Diagnostics.Overlaps == nil {
		t.Error("diagnostics arrays are null")
	}

	// The same asset by id, by name and as the account's default asset.
	var viaAsset balanceOut
	cliJSON(t, &viaAsset, "--demo", "balance", "acct:401k", "--from", "2027-01", "--to", "2027-03")
	if viaAsset.Samples[1].Value != b.Samples[1].Value {
		t.Error("asset id and account id give different values")
	}
	cliJSON(t, &viaAsset, "--demo", "balance", "Retirement", "--projection", "Base", "--from", "2027-01-01", "--to", "2027-01-31", "--step", "week")
	if viaAsset.Target != "retirement" || len(viaAsset.Samples) != 5 || viaAsset.Samples[4].Date != "2027-01-29" || *viaAsset.Projection != "Base" {
		t.Errorf("retirement = %+v", viaAsset)
	}

	cliJSON(t, &b, "--demo", "balance", "401k", "--from", "2027-01-31", "--to", "2027-05-31")
	var dates []string
	for _, s := range b.Samples {
		dates = append(dates, s.Date)
	}
	if want := []string{"2027-01-31", "2027-02-28", "2027-03-31", "2027-04-30", "2027-05-31"}; !slices.Equal(dates, want) {
		t.Errorf("month dates = %v", dates)
	}
	cliJSON(t, &b, "--demo", "balance", "401k", "--from", "2024-02-29", "--to", "2027-03-01", "--step", "year")
	dates = nil
	for _, s := range b.Samples {
		dates = append(dates, s.Date)
	}
	if want := []string{"2024-02-29", "2025-02-28", "2026-02-28", "2027-02-28"}; !slices.Equal(dates, want) {
		t.Errorf("year dates = %v", dates)
	}
	cliJSON(t, &b, "--demo", "balance", "401k", "--from", "2027-01-30", "--to", "2027-02-02", "--step", "day")
	if len(b.Samples) != 4 {
		t.Errorf("day samples = %d", len(b.Samples))
	}

	// A mixed asset, where part of it is projected.
	cliJSON(t, &b, "--demo", "balance", "net-worth", "--from", "2026-06", "--to", "2026-12")
	statuses := map[string]bool{}
	for _, s := range b.Samples {
		statuses[s.Status] = true
		if strings.Contains(s.Status, " ") {
			t.Errorf("status %q has a space", s.Status)
		}
	}
	if !statuses["partly_projected"] {
		t.Errorf("statuses = %v", statuses)
	}
}

func TestCLIWithdrawalScenario(t *testing.T) {
	values := func(projection string) []float64 {
		var b balanceOut
		cliJSON(t, &b, "--demo", "balance", "401k", "--from", "2027-05", "--to", "2027-07", "--projection", projection)
		if len(b.Samples) != 3 || b.Samples[0].Date != "2027-05-01" || b.Samples[2].Date != "2027-07-01" {
			t.Fatalf("samples = %+v", b.Samples)
		}
		return []float64{b.Samples[0].Value, b.Samples[1].Value, b.Samples[2].Value}
	}
	base, withdraw := values("Base"), values("Withdraw $100k")
	if d := base[0] - withdraw[0]; d < -1 || d > 1 {
		t.Errorf("before the withdrawal: base %v, withdraw %v", base[0], withdraw[0])
	}
	if d := base[2] - withdraw[2]; d < 100000-1 {
		t.Errorf("after the withdrawal: base %v, withdraw %v, difference %v", base[2], withdraw[2], d)
	}
}

func TestCLIBreakdownAndStarts(t *testing.T) {
	var bd struct {
		Target     string
		Projection *string
		From, To   string
		Start, End float64
		Total      float64
		Lines      []struct {
			Cause             string
			Actual, Projected float64
		}
		Diagnostics struct{ Skipped, Overlaps []any }
	}
	cliJSON(t, &bd, "--demo", "breakdown", "401k", "--from", "2027-05-31", "--to", "2027-06", "--projection", "Withdraw $100k")
	if bd.Target != "acct:401k" || bd.Projection == nil || bd.From != "2027-05-31" || bd.To != "2027-06-30" || len(bd.Lines) == 0 || bd.Diagnostics.Skipped == nil {
		t.Fatalf("breakdown = %+v", bd)
	}
	if d := bd.End - bd.Start - bd.Total; d < -1 || d > 1 {
		t.Errorf("total %v does not explain %v to %v", bd.Total, bd.Start, bd.End)
	}
	var sawWithdrawal bool
	for _, l := range bd.Lines {
		if l.Cause == "withdrawal" && l.Projected < -99999 {
			sawWithdrawal = true
		}
	}
	if !sawWithdrawal {
		t.Errorf("no withdrawal line: %+v", bd.Lines)
	}

	var starts []struct {
		Account         string
		Start           string
		LastActual      *string `json:"last_actual"`
		HasActual       bool    `json:"has_actual"`
		OverrideHistory bool    `json:"override_history"`
		ProjectionStart string  `json:"projection_start"`
	}
	cliJSON(t, &starts, "--demo", "starts")
	if len(starts) != 12 {
		t.Fatalf("starts for %d accounts", len(starts))
	}
	cliJSON(t, &starts, "--demo", "starts", "retirement", "--projection", "Base")
	if len(starts) == 0 || starts[0].Start == "" || starts[0].ProjectionStart != "2026-11-08" || !starts[0].HasActual || starts[0].LastActual == nil {
		t.Errorf("retirement starts = %+v", starts)
	}
}

func TestCLIHelp(t *testing.T) {
	var h struct {
		GlobalFlags []flagDef `json:"global_flags"`
		Commands    []command `json:"commands"`
	}
	cliJSON(t, &h, "help", "--json")
	if len(h.Commands) != len(commands) || len(h.GlobalFlags) != len(globalFlags) {
		t.Fatalf("help lists %d commands, %d global flags", len(h.Commands), len(h.GlobalFlags))
	}
	for i, c := range commands {
		got := h.Commands[i]
		if got.Name != c.Name || got.Summary != c.Summary || got.Output != c.Output || len(got.Args) != len(c.Args) || len(got.Flags) != len(c.Flags) {
			t.Errorf("help for %q differs from its definition: %+v", c.Name, got)
		}
		if c.Summary == "" || c.Output == "" || c.run == nil {
			t.Errorf("command %q is incomplete", c.Name)
		}
	}
	var raw struct {
		Commands []map[string]any `json:"commands"`
	}
	cliJSON(t, &raw, "help", "--json")
	for _, c := range raw.Commands {
		for _, k := range []string{"args", "flags"} {
			if c[k] == nil {
				t.Errorf("help for %v has null %s", c["name"], k)
			}
		}
	}
	code, text := cli(t, "help")
	if code != 0 {
		t.Fatalf("help exited %d", code)
	}
	for _, c := range commands {
		if !strings.Contains(string(text), "\n"+c.Name) {
			t.Errorf("help text lacks %q", c.Name)
		}
	}
	if !strings.Contains(string(text), "partly_projected") {
		t.Error("help text does not mention partly_projected")
	}
	if !strings.Contains(string(text), "--demo") || !strings.Contains(string(text), "--db") {
		t.Error("help text lacks the global flags")
	}
}

func TestCLIErrors(t *testing.T) {
	for _, c := range []struct {
		args     []string
		exit     int
		code     string
		contains string
	}{
		{[]string{"frobnicate"}, 2, "usage", "unknown command"},
		{[]string{"account"}, 2, "usage", "list"},
		{[]string{"account", "frobnicate"}, 2, "usage", "unknown command"},
		{[]string{"--demo", "--db", "x.db", "account", "list"}, 2, "usage", "together"},
		{[]string{"account", "list", "--demo", "--db=x.db"}, 2, "usage", "together"},
		{[]string{"--demo", "account", "list", "extra"}, 2, "usage", "unexpected"},
		{[]string{"--demo", "balance"}, 2, "usage", "missing"},
		{[]string{"--demo", "balance", "401k", "--from", "2027-01"}, 2, "usage", "--to"},
		{[]string{"--demo", "balance", "401k", "--from", "2027-13", "--to", "2027-12"}, 2, "usage", "not a date"},
		{[]string{"--demo", "balance", "401k", "--from", "2027-02-30", "--to", "2027-12"}, 2, "usage", "not a date"},
		{[]string{"--demo", "balance", "401k", "--from", "2027-06", "--to", "2027-01"}, 2, "usage", "before"},
		{[]string{"--demo", "balance", "401k", "--from", "", "--to", "2027-01"}, 2, "usage", "--from"},
		{[]string{"--demo", "balance", "401k", "--from", "2027-01", "--to", ""}, 2, "usage", "--to"},
		{[]string{"--demo", "balance", "401k", "--from", "2027-01", "--to", "2027-02", "--step", "decade"}, 2, "usage", "day, week"},
		{[]string{"--demo", "balance", "401k", "--from", "2027-01", "--to", "2027-02", "--bogus"}, 2, "usage", "bogus"},
		{[]string{"--demo", "balance", "nobody", "--from", "2027-01", "--to", "2027-02"}, 1, "not_found", "nobody"},
		{[]string{"--demo", "balance", "401k", "--from", "2027-01", "--to", "2027-02", "--projection", "Nope"}, 1, "not_found", "Nope"},
		{[]string{"--demo", "starts", "--projection", "Nope"}, 1, "not_found", "Nope"},
	} {
		code, msg := cliFail(t, c.exit, c.args...)
		if code != c.code || !strings.Contains(msg, c.contains) {
			t.Errorf("finplan %v: %s %q, want %s containing %q", c.args, code, msg, c.code, c.contains)
		}
	}
}

func TestCLIAmbiguousName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twin.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := demodata.Family()
	for _, id := range []model.AssetID{"twin-a", "twin-b"} {
		if err := rec.AddAsset(&model.Asset{ID: id, Name: "Twin", Members: []model.Member{model.AccountMember(demodata.Checking)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Save(rec); err != nil {
		t.Fatal(err)
	}
	st.Close()

	code, msg := cliFail(t, 1, "--db", path, "balance", "Twin", "--from", "2027-01", "--to", "2027-02")
	if code != "ambiguous" || !strings.Contains(msg, "twin-a") || !strings.Contains(msg, "twin-b") {
		t.Errorf("ambiguous: %s %s", code, msg)
	}
	var b balanceOut
	cliJSON(t, &b, "--db", path, "balance", "twin-b", "--from", "2027-01", "--to", "2027-02")
	if b.Target != "twin-b" {
		t.Errorf("target = %q", b.Target)
	}
}

// readCommands is every read command, each with arguments that work on the demo family.
var readCommands = [][]string{
	{"account", "list"},
	{"account", "entries", "checking"},
	{"entry", "history", "1"},
	{"asset", "list"},
	{"asset", "params", "retirement"},
	{"budget", "list"},
	{"projection", "list"},
	{"projection", "show", "New job"},
	{"balance", "net-worth", "--from", "2027-01", "--to", "2027-12", "--projection", "New job"},
	{"breakdown", "net-worth", "--from", "2027-01", "--to", "2027-12"},
	{"starts"},
	{"help", "--json"},
}

func userVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func seededFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seed.db")
	st, err := seedDemo(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	return path
}

func TestCLIReadsLeaveDatabaseAlone(t *testing.T) {
	path := seededFile(t)
	version := userVersion(t, path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range readCommands {
		cliJSON(t, new(any), append([]string{"--db", path}, args...)...)
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
			t.Fatalf("finplan %v changed the database file (%v)", args, err)
		}
	}
	if got := userVersion(t, path); got != version {
		t.Errorf("user_version %d, was %d", got, version)
	}
}

func TestCLIDefaultDatabase(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	if code, _ := cliFail(t, 1, "account", "list"); code != "no_database" {
		t.Errorf("code = %s", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "finplan")); err == nil {
		t.Error("a read created the data folder")
	}
	if err := os.MkdirAll(filepath.Join(dir, "finplan"), 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := seedDemo(filepath.Join(dir, "finplan", "finplan.db"))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	var accts []any
	cliJSON(t, &accts, "account", "list")
	if len(accts) != 12 {
		t.Errorf("%d accounts", len(accts))
	}
}

func TestCLIMissingDatabaseIsNotCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.db")
	for _, args := range readCommands {
		if args[0] == "help" {
			continue
		}
		if code, _ := cliFail(t, 1, append([]string{"--db", path}, args...)...); code != "no_database" {
			t.Errorf("finplan %v: code %s", args, code)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("reading a missing database created %v", entries)
	}
}

func TestCLIOldSchema(t *testing.T) {
	path := seededFile(t)
	version := userVersion(t, path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version-1)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	code, msg := cliFail(t, 1, "--db", path, "account", "list")
	if code != "old_schema" || !strings.Contains(msg, "open it with the app first") {
		t.Errorf("old schema: %s %s", code, msg)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("the old database was changed")
	}
	if got := userVersion(t, path); got != version-1 {
		t.Errorf("user_version %d, want %d", got, version-1)
	}

	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version+1)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if code, _ := cliFail(t, 1, "--db", path, "account", "list"); code != "future_schema" {
		t.Errorf("future schema: %s", code)
	}
}

func TestCLIDemoIsRemoved(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	cliJSON(t, new(any), "--demo", "account", "list")
	cliFail(t, 1, "--demo", "entry", "history", "99999999")
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Errorf("the demo database was left behind: %v", entries)
	}
}

func TestIsCLI(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--demo"}, false},
		{[]string{"--db", "x"}, false},
		{[]string{"--demo", "account"}, true},
		{[]string{"--db", "x", "help"}, true},
		{[]string{"frobnicate"}, true},
	} {
		if got := isCLI(c.args); got != c.want {
			t.Errorf("isCLI(%v) = %v", c.args, got)
		}
	}
}
