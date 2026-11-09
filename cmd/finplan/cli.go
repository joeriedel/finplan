// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// The command line interface: finplan <command> prints one JSON document on stdout, success or error. Commands are data (see commands), and both the parser and the help output are generated from that data. Read commands never write the database; write commands (see cliwrite.go) change it only through internal/ops.

// Exit codes.
const (
	exitFailure = 1
	exitUsage   = 2
)

// cliError is a failure reported as {"error":{"code","message"}}.
type cliError struct {
	Code    string
	Message string
	Exit    int
}

func (e *cliError) Error() string { return e.Message }

func usageErr(format string, a ...any) error {
	return &cliError{Code: "usage", Message: fmt.Sprintf(format, a...), Exit: exitUsage}
}

func failErr(code, format string, a ...any) error {
	return &cliError{Code: code, Message: fmt.Sprintf(format, a...), Exit: exitFailure}
}

// argDef describes a positional argument.
type argDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// Flag types.
const (
	typeBool   = "bool"
	typeString = "string"
	typeDate   = "date"
	typeEnum   = "enum"
	typeNumber = "number"
	typeInt    = "int"
)

// flagDef describes a flag. A date flag takes YYYY-MM-DD or YYYY-MM; the month means its first day, or its last day when EndOfMonth is set.
type flagDef struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Default     string   `json:"default"`
	Required    bool     `json:"required"`
	Choices     []string `json:"choices,omitempty"`
	Description string   `json:"description"`
	EndOfMonth  bool     `json:"-"`
}

// command is one CLI command: everything the parser and the help output know about it.
type command struct {
	Name    string    `json:"name"`
	Summary string    `json:"summary"`
	Args    []argDef  `json:"args"`
	Flags   []flagDef `json:"flags"`
	Output  string    `json:"output"`
	// Write marks a command that changes the database; it takes --dry-run. Input, if not empty, describes the JSON the command reads from --json-file or stdin.
	Write bool   `json:"write"`
	Input string `json:"input,omitempty"`

	run func(c *call) (any, error)
}

// globalFlags are accepted by every command, before or after the command name.
var globalFlags = []flagDef{
	{Name: "demo", Type: typeBool, Default: "false", Description: "use a throwaway database holding an invented family; cannot be combined with --db"},
	{Name: "db", Type: typeString, Description: "path of the database file to use instead of your own; cannot be combined with --demo"},
}

// call is one command's invocation.
type call struct {
	cmd    *command
	args   []string
	vals   map[string]string
	set    map[string]bool
	global struct {
		demo bool
		db   string
	}
	// commands is the command table, for help.
	commands []*command
	stdin    io.Reader

	st      *store.Store
	rec     *model.Record
	cleanup []func()
}

func (c *call) str(name string) string { return c.vals[name] }

// date returns the date flag's value; a missing optional flag gives false.
func (c *call) date(name string) (model.Date, bool, error) {
	v := c.vals[name]
	if v == "" {
		if c.set[name] {
			return 0, false, usageErr("--%s: empty date", name)
		}
		return 0, false, nil
	}
	var fd *flagDef
	for i := range c.cmd.Flags {
		if c.cmd.Flags[i].Name == name {
			fd = &c.cmd.Flags[i]
		}
	}
	d, err := parseCLIDate(v, fd != nil && fd.EndOfMonth)
	if err != nil {
		return 0, false, usageErr("--%s: %v", name, err)
	}
	return d, true, nil
}

// parseCLIDate reads YYYY-MM-DD, or YYYY-MM as the month's first day or, with endOfMonth, its last.
func parseCLIDate(s string, endOfMonth bool) (model.Date, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return model.D(t.Year(), t.Month(), t.Day()), nil
	}
	if t, err := time.Parse("2006-01", s); err == nil {
		if endOfMonth {
			return model.D(t.Year(), t.Month()+1, 0), nil
		}
		return model.D(t.Year(), t.Month(), 1), nil
	}
	return 0, fmt.Errorf("%q is not a date: use YYYY-MM-DD or YYYY-MM", s)
}

// number returns the number flag's value; a flag that is not set gives false.
func (c *call) number(name string) (float64, bool, error) {
	if !c.set[name] {
		return 0, false, nil
	}
	v, err := parseNumber(c.vals[name])
	if err != nil {
		return 0, false, usageErr("--%s: %v", name, err)
	}
	return v, true, nil
}

func parseNumber(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	return v, nil
}

// integer returns the int flag's value; a flag that is not set gives false.
func (c *call) integer(name string) (int, bool, error) {
	if !c.set[name] {
		return 0, false, nil
	}
	v, err := strconv.Atoi(c.vals[name])
	if err != nil {
		return 0, false, usageErr("--%s: %q is not a whole number", name, c.vals[name])
	}
	return v, true, nil
}

// open returns the store and record the command uses, opening them on first use. A write command opens the database for writing.
func (c *call) open() (*store.Store, *model.Record, error) {
	if c.rec != nil {
		return c.st, c.rec, nil
	}
	var st *store.Store
	var err error
	if c.cmd.Write {
		st, err = c.openWrite()
	} else {
		st, err = c.openReadOnly()
	}
	if err != nil {
		return nil, nil, err
	}
	rec, err := st.Load()
	if err != nil {
		return nil, nil, failErr("database", "load the record: %v", err)
	}
	c.st, c.rec = st, rec
	return st, rec, nil
}

// openDemoStore opens a throwaway demo database, removed when the command ends.
func (c *call) openDemoStore() (*store.Store, error) {
	st, cleanup, err := openDemo()
	if err != nil {
		return nil, failErr("database", "%v", err)
	}
	c.cleanup = append(c.cleanup, cleanup)
	return st, nil
}

// dbPath returns the path of the database --db or the default names.
func (c *call) dbPath() (string, error) {
	if c.global.db != "" {
		return c.global.db, nil
	}
	path, err := databasePath(os.Getenv, os.UserHomeDir)
	if err != nil {
		return "", failErr("database", "%v", err)
	}
	return path, nil
}

// openWrite opens the selected database for writing, migrating it if it is older. It never creates a database.
func (c *call) openWrite() (*store.Store, error) {
	if c.global.demo {
		return c.openDemoStore()
	}
	path, err := c.dbPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, failErr("no_database", "there is no database at %s", path)
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, openErr(err, path)
	}
	c.cleanup = append(c.cleanup, func() { st.Close() })
	return st, nil
}

// openReadOnly opens the selected database without creating, migrating or writing it. A demo database is removed when the command ends.
func (c *call) openReadOnly() (*store.Store, error) {
	if c.global.demo {
		return c.openDemoStore()
	}
	path, err := c.dbPath()
	if err != nil {
		return nil, err
	}
	st, err := store.OpenReadOnly(path)
	if err != nil {
		return nil, openErr(err, path)
	}
	c.cleanup = append(c.cleanup, func() { st.Close() })
	return st, nil
}

// openErr turns the failure to open the database at path into a CLI error.
func openErr(err error, path string) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return failErr("no_database", "there is no database at %s", path)
	case errors.Is(err, store.ErrOldSchema):
		return failErr("old_schema", "the database at %s was written by an older version of finplan: open it with the app first so it can be upgraded", path)
	case errors.Is(err, store.ErrFutureSchema):
		return failErr("future_schema", "the database at %s was written by a newer version of finplan", path)
	default:
		return failErr("database", "%v", err)
	}
}

// isCLI reports whether the arguments name a command, that is, hold something other than flags. The only flag that takes a separate value before the command is --db.
func isCLI(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--db" || a == "-db":
			i++
		case !strings.HasPrefix(a, "-"):
			return true
		}
	}
	return false
}

func isDBFlag(a string) bool { return a == "--db" || a == "-db" }

// resolveCommand finds the command named by the leading words of args, skipping the global flags, and returns it with the remaining arguments. The words of a two-word name need not be adjacent to the flags.
func resolveCommand(cmds []*command, args []string) (*command, []string, error) {
	var words, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case isDBFlag(a):
			rest = append(rest, a)
			if i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
		case strings.HasPrefix(a, "-"):
			rest = append(rest, a)
		default:
			words = append(words, a)
			name := strings.Join(words, " ")
			for _, c := range cmds {
				if c.Name == name {
					return c, append(rest, args[i+1:]...), nil
				}
			}
			if !isNoun(cmds, name) {
				return nil, nil, usageErr("unknown command %q: run finplan help", name)
			}
		}
	}
	if len(words) == 0 {
		return nil, nil, usageErr("no command: run finplan help")
	}
	return nil, nil, usageErr("%s needs one of: %s", words[0], strings.Join(subcommands(cmds, words[0]), ", "))
}

func isNoun(cmds []*command, word string) bool {
	return len(subcommands(cmds, word)) > 0
}

func subcommands(cmds []*command, noun string) []string {
	var out []string
	for _, c := range cmds {
		if rest, ok := strings.CutPrefix(c.Name, noun+" "); ok {
			out = append(out, rest)
		}
	}
	return out
}

// parse reads the command's flags and positional arguments, which may be mixed, into a call.
func (cmd *command) parse(args []string) (*call, error) {
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c := &call{cmd: cmd, vals: map[string]string{}, set: map[string]bool{}}
	strs := map[string]*string{}
	bools := map[string]*bool{}
	for _, f := range slices.Concat(globalFlags, cmd.Flags) {
		if f.Type == typeBool {
			bools[f.Name] = fs.Bool(f.Name, f.Default == "true", f.Description)
		} else {
			strs[f.Name] = fs.String(f.Name, f.Default, f.Description)
		}
	}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usageErr("%s: %v", cmd.Name, err)
		}
		if fs.NArg() == 0 {
			break
		}
		c.args = append(c.args, fs.Arg(0))
		args = fs.Args()[1:]
	}
	fs.Visit(func(f *flag.Flag) { c.set[f.Name] = true })
	for name, v := range strs {
		c.vals[name] = *v
	}
	for name, v := range bools {
		c.vals[name] = strconv.FormatBool(*v)
	}
	c.global.demo, c.global.db = *bools["demo"], c.vals["db"]

	if c.global.demo && c.global.db != "" {
		return nil, usageErr("--demo and --db cannot be used together")
	}
	required := 0
	for _, a := range cmd.Args {
		if a.Required {
			required++
		}
	}
	if len(c.args) < required {
		return nil, usageErr("%s: missing argument <%s>", cmd.Name, cmd.Args[len(c.args)].Name)
	}
	if len(c.args) > len(cmd.Args) {
		return nil, usageErr("%s: unexpected argument %q", cmd.Name, c.args[len(cmd.Args)])
	}
	for _, f := range cmd.Flags {
		if c.set[f.Name] && (f.Type == typeNumber || f.Type == typeInt) {
			var err error
			if f.Type == typeNumber {
				_, _, err = c.number(f.Name)
			} else {
				_, _, err = c.integer(f.Name)
			}
			if err != nil {
				return nil, err
			}
		}
		if f.Required && !c.set[f.Name] {
			return nil, usageErr("%s: missing flag --%s", cmd.Name, f.Name)
		}
		if f.Type == typeEnum && (c.set[f.Name] || f.Default != "") && !slices.Contains(f.Choices, c.vals[f.Name]) {
			return nil, usageErr("--%s: %q is not one of %s", f.Name, c.vals[f.Name], strings.Join(f.Choices, ", "))
		}
	}
	return c, nil
}

// runCLI runs one command and returns the process exit code. Results and errors are JSON on stdout.
func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd, rest, err := resolveCommand(commands, args)
	var c *call
	if err == nil {
		c, err = cmd.parse(rest)
	}
	var result any
	if err == nil {
		c.commands = commands
		c.stdin = stdin
		defer func() {
			for _, f := range slices.Backward(c.cleanup) {
				f()
			}
		}()
		result, err = cmd.run(c)
	}
	if err != nil {
		var ce *cliError
		if !errors.As(err, &ce) {
			ce = &cliError{Code: "failed", Message: err.Error(), Exit: exitFailure}
		}
		writeJSON(stdout, map[string]any{"error": map[string]string{"code": ce.Code, "message": ce.Message}})
		return ce.Exit
	}
	if text, ok := result.(textOutput); ok {
		io.WriteString(stdout, string(text))
		return 0
	}
	writeJSON(stdout, result)
	return 0
}

// textOutput is a result printed as it is, not as JSON.
type textOutput string

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// jdate is a date that marshals as YYYY-MM-DD, or null for the unbounded ends of the date range.
type jdate model.Date

func (d jdate) MarshalJSON() ([]byte, error) {
	if model.Date(d) == model.MinDate || model.Date(d) == model.MaxDate {
		return []byte("null"), nil
	}
	return strconv.AppendQuote(nil, model.Date(d).String()), nil
}

func jd(d model.Date) jdate { return jdate(d) }

// jdp converts an optional date; nil marshals as null.
func jdp(p *model.Date) *jdate {
	if p == nil {
		return nil
	}
	return model.Ptr(jdate(*p))
}

// nullable returns nil for an empty string, so that it marshals as null.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// helpText renders the command table for people.
func helpText(cmds []*command) string {
	var b strings.Builder
	b.WriteString("finplan [--demo | --db <path>] <command> [arguments] [flags]\n")
	b.WriteString("With no command, finplan opens its window. Every command prints one JSON document on stdout;\n")
	b.WriteString("an error prints {\"error\":{\"code\",\"message\"}} and exits 1, or 2 for a usage error.\n")
	b.WriteString("finplan help --json describes the commands for programs.\n\nGlobal flags, accepted by every command:\n")
	for _, f := range globalFlags {
		writeFlag(&b, f)
	}
	for _, c := range cmds {
		b.WriteString("\n" + c.Name)
		for _, a := range c.Args {
			if a.Required {
				b.WriteString(" <" + a.Name + ">")
			} else {
				b.WriteString(" [<" + a.Name + ">]")
			}
		}
		b.WriteString("\n  " + c.Summary + "\n")
		for _, a := range c.Args {
			fmt.Fprintf(&b, "  <%s>  %s\n", a.Name, a.Description)
		}
		for _, f := range c.Flags {
			writeFlag(&b, f)
		}
		b.WriteString("  output: " + c.Output + "\n")
	}
	return b.String()
}

func writeFlag(b *strings.Builder, f flagDef) {
	if f.Type == typeBool {
		fmt.Fprintf(b, "  --%s", f.Name)
	} else {
		fmt.Fprintf(b, "  --%s <%s>", f.Name, f.Type)
	}
	switch {
	case f.Required:
		b.WriteString("  (required)")
	case f.Default != "" && f.Type != typeBool:
		fmt.Fprintf(b, "  (default %s)", f.Default)
	}
	if len(f.Choices) > 0 {
		fmt.Fprintf(b, "  one of %s;", strings.Join(f.Choices, ", "))
	}
	b.WriteString("  " + f.Description + "\n")
}

// cmdHelp is the help command.
func cmdHelp(c *call) (any, error) {
	if c.str("json") == "true" {
		cmds := make([]command, len(c.commands))
		for i, cmd := range c.commands {
			cmds[i] = *cmd
			if cmds[i].Args == nil {
				cmds[i].Args = []argDef{}
			}
			if cmds[i].Flags == nil {
				cmds[i].Flags = []flagDef{}
			}
		}
		return map[string]any{"global_flags": globalFlags, "commands": cmds}, nil
	}
	return textOutput(helpText(c.commands)), nil
}
