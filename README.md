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

Drag to pan and scroll to zoom the plot. The toolbar has a Projection dropdown and range buttons (1Y, 5Y, 10Y, 30Y, All). In the line list, checkboxes toggle a line, the S button solos it (only soloed lines are drawn; a "Solo: N · Clear" chip or Ctrl-click on any S button clears all solos, and the checkboxes are left as they were), "+ Add" adds one, and rows can be pinned to a projection, recoloured and removed (the solo, pin and remove buttons show while the row is hovered). The Bands section shades the gap between two lines (optionally hatched, with the gap at the cursor in the tooltip, also in today's dollars), and the View preset section saves and restores the lines, bands and date range (presets are saved in the database and persist across runs; the demo database holds two example presets). Changes made to the database by another process are picked up: the app checks the database's data version once a second and reloads when it changes. Click a line, its row or its data lane under the plot to select it, and hover over the plot for the crosshair and tooltip, with value tags in the y-axis gutter and the date tag below the plot.

The Projections tab lists the projections as a tree with Reality first, with search, New, Clone, Rename and Delete, and edits a projection's parent, start date, override history, inflation and modifications (add, remove, edit a modification's kind, object, property, value and start in place, add and edit budget items, end a budget item on a date, drag to reorder; a How column makes a modification set or scale, with a scale entered as a percentage change such as -30, and budget items can have an annual raise; modifications overwritten by later ones are greyed with a tooltip). Edits are saved to the database at once. Edit > Undo and Redo (Ctrl+Z, Ctrl+Shift+Z or Ctrl+Y) step through them until the database is reloaded. Only modifications with several segments are shown but not yet editable.

## Command line

    bin/finplan [--demo | --db <path>] <command> [arguments] [flags]

With no command (`finplan` or `finplan --demo`) the app opens its window as above. With a command, the same binary reads or writes the database and prints one JSON document on stdout, without a window. Run `bin/finplan help` for the commands and `bin/finplan help --json` for the same description as JSON, generated from the command definitions the parser uses.

- `--demo` (a throwaway seeded database, removed on exit) and `--db <path>` work on every command, before or after it; using both is a usage error. With neither, the real database is used (same location as the app).
- Read commands never write: they open the database read-only. A missing file is an error (`no_database`) and is not created. A database older than the program gives `old_schema` (open it with the app first so it is upgraded), and a newer one `future_schema`.
- Errors are `{"error":{"code","message"}}` on stdout, with exit code 2 for usage errors and 1 for all others. Codes: `usage`, `not_found`, `ambiguous`, `no_database`, `old_schema`, `future_schema`, `database`, `engine`, and for the write commands `unknown_target`, `name_taken`, `has_children`, `cycle`, `invalid`, `in_use` (an asset still used by another asset or a projection) and `record_changed` (the record changed since it was loaded). `not_found` also covers unknown assets, accounts and entries.
- Dates are `YYYY-MM-DD`, and `null` when absent or unbounded. Statuses are lowercase: `default`, `actual`, `projected`, `partly_projected`. Reality appears as `"projection": null`.

Read commands:

| Command | Output |
| --- | --- |
| `account list` | accounts: id, name, type, tax, owner, `stated_rate` and `default` (the default balance intervals) |
| `account entries <account>` | the account's snapshots and transactions with current values (voided entries left out) |
| `entry history <entry-id>` | every revision of one entry: original values, then each correction or void |
| `asset list` | assets with their members, members left out as invalid, any resolve error, `default` (default balance intervals) and `params` (parameter settings) |
| `budget list` | budget items with recurrence, start, end, indexing and accounts |
| `projection list` | projections: name, parent, start, override history, modification count |
| `projection show <name>` | a projection's settings, per-asset settings and modifications in the order they apply |
| `balance <target> --from D --to D [--step day\|week\|month\|year] [--projection P]` | samples of date, value and status, plus diagnostics; `--step` defaults to `month` |
| `breakdown <target> --from D --to D [--projection P]` | start and end values, total, and lines of cause with actual and projected parts, plus diagnostics |
| `starts [--projection P] [<target>]` | where each account switches from recorded data to projection; every account when no target is given |
| `help [--json]` | the command reference |

Commands about data that belongs to one kind of object are noun-verb (`account list`, `projection show`). The engine queries (`balance`, `breakdown`, `starts`) are top-level verbs because they accept either an asset or an account.

A `<target>` is an asset or account given by id or display name; ids win over names, an account and its default asset count as one object, and a name matching several objects is an `ambiguous` error. `--from` and `--to` take `YYYY-MM-DD` or `YYYY-MM`: for `--from` a month means its first day, for `--to` its last day. Month and year steps keep the day of month of `--from`, clamped to the end of shorter months.

Write commands (see below):

| Command | Effect |
| --- | --- |
| `projection new <name> [--parent P]` | add an empty projection |
| `projection clone <name> --as <new-name>` | add a copy |
| `projection rename <name> <new-name>` | rename, pointing child projections and saved view presets at the new name |
| `projection set-parent <name> [<parent>]` | build on another projection, or on nothing when omitted |
| `projection delete <name>` | delete a projection no other builds on |
| `projection set-start <name> <date\|none>` | set the start date; `none` inherits |
| `projection set-override-history <name> <true\|false\|none>` | set whether the start overrides recorded data; `none` inherits |
| `projection set-inflation <name> <rate\|none>` | set a constant inflation rate, replacing the first global set-mode inflation modification (scale ones are kept); `none` removes it |
| `mod add <projection> --target T --property amount\|growth\|inflation [--mode set\|scale] [--value N [--from D]] [--at I]` | add a modification (`growth` also suits an item: an annual raise; with `--mode scale` the value is a multiplier, 0.7 cuts by 30%); `T` is `item:<id or name>`, `account:<id or name>`, `asset:<id or name>` or `global` |
| `mod edit <projection> <index> [--mode set\|scale] [--value N [--from D]]` | replace a modification's values; `--mode` alone changes the mode and keeps the values |
| `mod move <projection> <from> <to>` | move a modification so that it ends up at index `to` |
| `mod remove <projection> <index>` | remove a modification |
| `item add <projection> [flags]` | add a new budget item that exists only under that projection and the ones building on it |
| `item edit <projection> <item> [flags]` | change an item the projection adds, keeping its id |
| `item end <projection> <item> <date>` | make a budget item (the record's or one added in the chain) pay nothing from a date on |
| `asset new <name> [--id]` | add an aggregate asset with no members |
| `asset rename <asset> <new-name>` | rename an aggregate asset |
| `asset delete <asset>` | delete an aggregate asset and its members; refused (`in_use`) while another asset wraps it or a projection names it in an asset setting or modification |
| `asset add-member <asset> (--account A \| --asset A \| --rule [--types T] [--tax T])` | add a member after the others |
| `asset remove-member <asset> <seq>` | remove a member |
| `asset add-default <asset> --value N [--from D] [--to D]` | add a default balance interval |
| `asset remove-default <asset> <index>` | remove a default balance interval; the index counts from 0 in the `default` list of `asset list` |
| `asset set-param <asset> [--param growth] (--value N [--from D] \| --clear)` | set or clear the asset's growth setting; without `--value` and `--clear` a schedule is read as JSON |
| `account new <name> --type T --tax T [--owner] [--rate] [--id]` | add an account and its default asset |
| `account edit <account> [--name --type --tax --owner --rate]` | change an account's attributes; a rename also renames its default asset |
| `entry add-snapshot <account> --date D --balance N` | record a balance |
| `entry add-transaction <account> --date D --amount N --kind income\|expense\|transfer [--counter A] [--memo]` | record a transaction |
| `entry correct <entry-id> [--date --amount --kind --counter --memo] [--note]` | correct an entry; the earlier values stay in `entry history` |
| `entry void <entry-id> [--note]` | take an entry out of the record |
| `budget add [item flags]` | add an item to the base budget |
| `budget edit <item> [item flags]` | change a base budget item |
| `budget end <item> <date>` | set the base item's last date |

Write commands go through the same edit operations (`internal/ops`) as the app, so both agree on what an edit may do. Details:

- Database: `--demo`, `--db` and the default work as for reads, but a write command opens the database with `store.Open`, so it migrates an older database, and still never creates a missing one (`no_database`). Read commands still never write.
- `--dry-run` (every write command) applies the edit to a copy, prints what would change and saves nothing.
- Output of the projection commands is `{"dry_run","changed","projection"}`, the projection in the same shape as `projection show`, after the edit. The asset, account, entry and budget commands print `{"dry_run","changed",<object>}` with `asset`, `account`, `entry` or `item` in the shape its read command prints (`asset delete` adds `removed`, `entry void` adds `voided`; `account new` also prints its default `asset`); an entry added in a dry run has `id` null. An edit that changes nothing prints `"changed": false` and exits 0. `projection rename` adds `renamed` (`{from,to}`, or null), `rewritten_presets` and `warnings` (presets that could not be rewritten; the rename is saved regardless). `projection delete` prints `removed` and the projection as it was.
- Rates (growth, inflation) are fractions: `0.03` is 3%. A negative rate given as an argument needs `--` before it.
- Modification values are a constant (`--value`, from `--from`, always when omitted) or a schedule read as JSON on stdin or from `--json-file`: `[{"from":"YYYY-MM-DD","end":"YYYY-MM-DD" or null,"value":number}]`, sorted and not overlapping. Input is decoded strictly: unknown fields and trailing data are usage errors.
- Budget items: `--kind income|expense|transfer`, `--name`, `--amount` (above zero), `--every one-time|monthly|biweekly|annual`, `--on <date>`, `--day` (monthly), `--end <date|none>`, `--account` (income, expense), `--from` and `--to` (transfer), `--indexed`, `--growth` (annual raise as a fraction, 0.03 for 3%) and for `item add` `--id`. Or give the item as JSON (the shape `budget list` prints) on stdin or `--json-file` instead of the flags. One-time and biweekly items need a date; an annual item's day is checked against its month; semimonthly is not accepted for added items. The id is made from the name (a unique slug) when not given. An item is stored as an add_item modification. `item end` appends an amount modification with value 0 from the date.
- Targets are checked by the shared operations: the property must suit the target (amount or growth for an item, growth for an account or asset, inflation for global), and the target must exist at the modification's position, else `unknown_target`: a record budget item, an item added by an ancestor projection, or an item added by an earlier row of the same projection. `mod add` cannot add an item (use `item add`). A move, removal, parent change or removal of an added item is refused if a modification that resolved before would no longer resolve; modifications already dangling do not block an edit.
- Assets: a member that overlaps an earlier one is never refused; the later member is listed under `invalid`. An account's default asset (`acct:<id>`) cannot be renamed, deleted or have members changed. `asset remove-member` and `asset delete` renumber the members' seqs 1..N in order, so later seqs can shift. On a default asset, `add-default` and `remove-default` change the account's own default balance and `set-param` its settings.
- Record: entries are never edited in place. A snapshot on a date that already has one is refused (use `entry correct`); `entry correct` and `entry void` go through the store's corrections, and their dry run runs the same checks in a rolled-back transaction. A transfer needs a counter account that exists and is not the account itself. The stated rate is limited to -1..1.
- Base budget: `budget end` sets the item's end date and refuses one before the start. Semimonthly items cannot be created, but existing ones can be edited and ended.
- Exit codes and error codes are as above: `usage` exits 2; `not_found`, `unknown_target`, `name_taken`, `has_children`, `cycle`, `invalid`, `in_use`, `record_changed`, `database`, `no_database`, `old_schema`, `future_schema` and `ambiguous` exit 1.

Run `bin/finplan help --json` for every command's arguments, flags (with types), whether it writes and what JSON input it reads.

An agent skill for using the CLI is in `.claude/skills/finplan/SKILL.md`.

## Fonts

The Inter font is bundled under the SIL Open Font License; the licence text is in `cmd/finplan/fonts/OFL.txt`.
