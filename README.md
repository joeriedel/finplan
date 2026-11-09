# finplan

finplan is a personal financial planner for one user. It is a Linux desktop app written in Go, drawing its UI with Dear ImGui and ImPlot (via cimgui-go) on SDL3, as a native Wayland client. It shows a tabbed application shell with a working Graph tab and the first half of the Projections tab over the record in its database (an invented family record with `--demo`); the other tabs are placeholders. The UI uses a dark theme and the bundled Inter font.

The data model, projection engine and planned features are described in [docs/design.md](docs/design.md). The model and a placeholder engine exist as standalone Go packages, `internal/model` and `internal/engine` and the Graph tab draws from them. A storage layer, `internal/store`, saves and loads the record to SQLite, and the app uses it.

## Prerequisites

- podman, used to build and test inside a Fedora 43 container (no Go toolchain is needed on the host)
- SDL3 on the host (`libSDL3.so.0`, Fedora package `SDL3`) to run the app

## Build

    scripts/build.sh

This builds the container image on first use (and whenever the `Containerfile` changes) and produces `bin/finplan`.

## Test

    scripts/test.sh

Runs a gofmt check, `go vet` and `go test` in the container.

## Run

    bin/finplan [--demo]

With `--demo` the app runs against a temporary database seeded from the invented family data and the family view presets; it is removed on exit and the window title is "finplan (demo)". Without it the app uses `$XDG_DATA_HOME/finplan/finplan.db` (only when `XDG_DATA_HOME` is an absolute path) or else `~/.local/share/finplan/finplan.db`; the folder is created with mode 0700. An empty database shows a hint on the Graph tab.

File > Backup... opens a save dialog (`.db` filter, default name `finplan-backup-YYYY-MM-DD.db`) and writes a consistent copy of the database. It refuses to overwrite the open database itself.

Drag to pan and scroll to zoom the plot. The toolbar has a Projection dropdown and range buttons (1Y, 5Y, 10Y, 30Y, All). In the line list, checkboxes toggle a line, the S button solos it (only soloed lines are drawn; a "Solo: N · Clear" chip or Ctrl-click on any S button clears all solos, and the checkboxes are left as they were), "+ Add" adds one, and rows can be pinned to a projection, recoloured and removed. The Bands section shades the gap between two lines (optionally hatched, with the gap at the cursor in the tooltip, also in today's dollars), and the View preset section saves and restores the lines, bands and date range (presets are saved in the database and persist across runs; the demo database holds two example presets). Changes made to the database by another process are picked up: the app checks the database's data version once a second and reloads when it changes. Click a line, its row or its data lane under the plot to select it, and hover over the plot for the crosshair and tooltip, with value tags in the y-axis gutter and the date tag below the plot.

The Projections tab lists the projections as a tree with Reality first, with search, New, Clone, Rename and Delete, and edits a projection's parent, start date, override history, inflation and modifications (add, remove, edit a constant value or start, drag to reorder; modifications overwritten by later ones are greyed with a tooltip). Edits are saved to the database at once. Edit > Undo and Redo (Ctrl+Z, Ctrl+Shift+Z or Ctrl+Y) step through them until the database is reloaded. Modifications with several segments and budget item additions are shown but not yet editable.

## Command line

    bin/finplan [--demo | --db <path>] <command> [arguments] [flags]

With no command (`finplan` or `finplan --demo`) the app opens its window as above. With a command, the same binary reads the database and prints one JSON document on stdout, without a window. Run `bin/finplan help` for the commands and `bin/finplan help --json` for the same description as JSON, generated from the command definitions the parser uses.

- `--demo` (a throwaway seeded database, removed on exit) and `--db <path>` work on every command, before or after it; using both is a usage error. With neither, the real database is used (same location as the app).
- Read commands never write: they open the database read-only. A missing file is an error (`no_database`) and is not created. A database older than the program gives `old_schema` (open it with the app first so it is upgraded), and a newer one `future_schema`.
- Errors are `{"error":{"code","message"}}` on stdout, with exit code 2 for usage errors and 1 for all others. Codes: `usage`, `not_found`, `ambiguous`, `no_database`, `old_schema`, `future_schema`, `database`, `engine`.
- Dates are `YYYY-MM-DD`, and `null` when absent or unbounded. Statuses are lowercase: `default`, `actual`, `projected`, `partly_projected`. Reality appears as `"projection": null`.

Read commands:

| Command | Output |
| --- | --- |
| `account list` | accounts: id, name, type, tax, owner |
| `account entries <account>` | the account's snapshots and transactions with current values (voided entries left out) |
| `entry history <entry-id>` | every revision of one entry: original values, then each correction or void |
| `asset list` | assets with their members, members left out as invalid, and any resolve error |
| `budget list` | budget items with recurrence, start, end, indexing and accounts |
| `projection list` | projections: name, parent, start, override history, modification count |
| `projection show <name>` | a projection's settings, per-asset settings and modifications in the order they apply |
| `balance <target> --from D --to D [--step day\|week\|month\|year] [--projection P]` | samples of date, value and status, plus diagnostics; `--step` defaults to `month` |
| `breakdown <target> --from D --to D [--projection P]` | start and end values, total, and lines of cause with actual and projected parts, plus diagnostics |
| `starts [--projection P] [<target>]` | where each account switches from recorded data to projection; every account when no target is given |
| `help [--json]` | the command reference |

Commands about data that belongs to one kind of object are noun-verb (`account list`, `projection show`). The engine queries (`balance`, `breakdown`, `starts`) are top-level verbs because they accept either an asset or an account.

A `<target>` is an asset or account given by id or display name; ids win over names, an account and its default asset count as one object, and a name matching several objects is an `ambiguous` error. `--from` and `--to` take `YYYY-MM-DD` or `YYYY-MM`: for `--from` a month means its first day, for `--to` its last day. Month and year steps keep the day of month of `--from`, clamped to the end of shorter months.

Commands that change data come later.

An agent skill for using the CLI is in `.claude/skills/finplan/SKILL.md`.

## Fonts

The Inter font is bundled under the SIL Open Font License; the licence text is in `cmd/finplan/fonts/OFL.txt`.
