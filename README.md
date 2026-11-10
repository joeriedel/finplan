# finplan

finplan is a personal financial planner for one user. It is a Linux desktop app written in Go, drawing its UI with Dear ImGui and ImPlot (via cimgui-go) on SDL3, as a native Wayland client. It shows a tabbed application shell with working Graph, Projections, Assets, Accounts and Budget tabs over the record in its database (an invented family record with `--demo`); the other tabs are placeholders. The UI uses a dark theme and the bundled Inter font.

The data model, projection engine and planned features are described in [docs/design.md](docs/design.md). The model and a placeholder engine exist as standalone Go packages, `internal/model` and `internal/engine` and the Graph tab draws from them. A storage layer, `internal/store`, saves and loads the record to SQLite, and the app uses it.

## Prerequisites

- podman, used to build and test inside a Fedora 43 container (no Go toolchain is needed on the host)
- SDL3 on the host (`libSDL3.so.0`, Fedora package `SDL3`) to run the app

## Build

    scripts/build.sh

This builds the container image on first use (and whenever the `Containerfile` changes) and produces `bin/finplan`.

## Test

    scripts/test.sh

Runs a gofmt check, `go vet` and `go test` in the container. The tests include a headless GUI smoke test (`cmd/finplan/smoke_test.go`): it builds the binary and, on the demo data and SDL's offscreen video driver, takes a screenshot of every tab (of an account selected with `--select`, of the Assets tab with Net worth selected and of each `--open` picker). It fails on a non-zero exit, panic output, a PNG of the wrong size or a single-colour image, and also checks the errors for bad screenshot arguments and that a SIGINT to a running `--demo` window leaves no demo folder behind. `scripts/test.sh` sets `FINPLAN_SMOKE_OUT` so the screenshots are kept in `test-output/screenshots/` (ignored by git); without it they go to a temporary folder. The container installs Mesa's software OpenGL drivers, so the smoke test needs no display and runs in CI.

## Run

    bin/finplan [--demo]

With `--demo` the app runs against a temporary database seeded from the invented family data and the family view presets; it is removed on exit, including on SIGINT and SIGTERM, and the window title is "finplan (demo)". The demo folder is `finplan-demo-*` in the system temp folder, holding `demo.db` and an `owner.pid` file with the PID of the running process; when a demo starts, `finplan-demo-*` folders whose `owner.pid` names a process that is no longer running (for example after a kill) are removed, while folders with a live owner or without a readable `owner.pid` are left alone. Without it the app uses `$XDG_DATA_HOME/finplan/finplan.db` (only when `XDG_DATA_HOME` is an absolute path) or else `~/.local/share/finplan/finplan.db`; the folder is created with mode 0700. An empty database shows a hint on the Graph tab.

File > Backup... opens a save dialog (`.db` filter, default name `finplan-backup-YYYY-MM-DD.db`) and writes a consistent copy of the database. It refuses to overwrite the open database itself.

Drag to pan and scroll to zoom the plot. The toolbar has a Projection dropdown and range buttons (1Y, 5Y, 10Y, 30Y, All). In the line list, checkboxes toggle a line, the S button solos it (only soloed lines are drawn; a "Solo: N · Clear" chip or Ctrl-click on any S button clears all solos, and the checkboxes are left as they were), "+ Add" adds one, and rows can be pinned to a projection, recoloured and removed (the solo, pin and remove buttons show while the row is hovered). The Bands section shades the gap between two lines (optionally hatched, with the gap at the cursor in the tooltip, also in today's dollars), and the View preset section saves and restores the lines, bands and date range (presets are saved in the database and persist across runs; the demo database holds two example presets). Changes made to the database by another process are picked up: the app checks the database's data version once a second and reloads when it changes. Click a line, its row or its data lane under the plot to select it, and hover over the plot for the crosshair and tooltip, with value tags in the y-axis gutter and the date tag below the plot.

The Projections tab lists the projections as a tree with Reality first, with search, New, Clone, Rename and Delete, and edits a projection's parent, start date, override history, inflation and modifications (add, remove, edit a modification's kind, object, property, value and start in place, add and edit budget items, end a budget item on a date, drag to reorder; a How column makes a modification set or scale, with a scale entered as a percentage change such as -30, and budget items can have an annual raise; modifications overwritten by later ones are greyed with a tooltip, and one whose target no longer exists is red with a tooltip and skipped). Edits are saved to the database at once. Edit > Undo and Redo (Ctrl+Z, Ctrl+Shift+Z or Ctrl+Y) step through them until the database is reloaded. An "Accounts in this projection" section adds, edits and removes the projection's own hypothetical accounts (inherited ones are read-only), and the modification target and budget-item account pickers list them. A "Start settings" section sets a start date and/or override history per asset (in order, drag to reorder, add and remove) and is followed by a table of the effective start of each account. Account and asset targets offer a default balance property (money in set mode, a percent change in scale mode), and the Add form has an optional To end date (an end without a start is refused); a modification with several segments (or a range) has a Timeline button that opens a timeline strip editor under the table, also used for a varying Inflation: drag segment edges (snapped to months, Ctrl for days), split, delete, add and clear the end, each as one undo step.

The Assets tab shows a tree of the user's assets (aggregates) with their members nested (accounts, assets and rule members showing what they match) and today's balance for each; an account's default asset is not listed. A member that would count an account already counted is drawn red with a tooltip naming the duplicates and the earlier member. The selected asset has a small chart (not zoomable) of actual (solid), projected (dashed) and default (faded) values from about a year back to 10 years ahead, with a coverage bar below it showing where its members have actual data and the last actual date. The name is edited in place; an Add member form (account, asset or rule by types and tax) and a New asset form are disabled with a red reason until valid; Delete opens a confirmation dialog that lists what refers to the asset (other assets' members, projection asset settings and modifications for it) and that is removed with it; it is disabled, with the reason, for an account's default asset. Accounts under the asset stay, with their modifications. Edits can be undone with Edit > Undo. A rule member is live: an account added later that matches joins the asset. Asset and account names must be unique (ignoring case and surrounding space), else the edit is refused (`name_taken` on the command line). The detail pane also has a Default balance section (the intervals in the order they apply, a later one winning where they overlap; edit a value or dates in place, remove, drag to reorder, and an Add form disabled with a red reason until valid) and a Parameters section (growth: none, a constant edited in place with Set, or a schedule; a Timeline button, shown once a setting exists, opens a timeline strip editor; Clear removes the setting). An Effective growth table lists each member account's layers in apply order (the account's stated rate, then each asset along the path that sets growth, the later winning on the dates it covers; a layer overwritten today is greyed) and the effective value today. An empty growth schedule counts as no setting.

The Accounts tab lists the accounts grouped by type with each one's balance, the date of its last data and a stale flag (an open account whose last data is more than 45 days old), and closed accounts apart. An Add account form (disabled with a red reason until valid) adds a manual account; the selected account's name, type, tax treatment, owner and stated rate are edited in place; a Record a balance form adds a snapshot; the default balance intervals are listed with remove and add. Close (with a date), Reopen and Delete are offered. Delete opens a confirmation dialog (Delete or Cancel) that lists what the delete changes: the account's membership in other assets, projection asset settings and modifications for it or its default asset, and the base and projection-added budget items that move money in or out of it, with the modifications of them. An account with record entries, or one that is the counter account of any entry, is never deleted: the dialog says so and offers Close account (disabled, with a red reason, while the Closes-on date is invalid) or Cancel. A closed account keeps its history, reads 0 after its close date, stops its budget items after that date, is left out of the pickers for new Graph lines, modification targets and budget-item accounts, and refuses entries dated after the close date. Account and asset edits, including a delete with everything it changed, can be undone with Edit > Undo in one step, except recording entries, which is append-only. The selected account's record entries are listed newest first (date descending, same-day ties newest-recorded first) with date, kind (snapshot, income, expense or transfer), amount or balance, counter account and memo; a Show voided checkbox includes voided entries, marked voided, and with it off the tab says how many voided entries are hidden. A Record a transaction form (date, amount, kind, a counter account picker that leaves out the account itself and closed accounts, memo) sits beside the Record a balance form and is disabled with a red reason until valid. Selecting an entry opens a panel with a Correct form prefilled with its current values and a note, a Void button with a note (for a voided entry, Restore instead: a correction that changes no value) and a history table of every revision with the time made (local), note and voided marker. Adding, correcting, voiding and restoring entries push no undo step.

The Budget tab shows the base budget's items grouped as income, expense and transfer, each row with name, schedule, amount, accounts, start, end, indexed, annual raise and monthly equivalent. Above them are the monthly income, expenses and amount left over as of the record's today, in today's dollars (transfers are in neither; a one-time item counts for nothing and an annual one for a twelfth). Edit opens an item's fields in place (the Projections tab's item fields plus a "rises with inflation" checkbox), with an End date and a Delete button (a projection's modification of a deleted item is left pointing at nothing: red, and skipped, on the Projections tab); an Add form is disabled with a red reason until valid. The forms offer one-time, monthly, biweekly and annual repeats with a single amount and raise; the other repeats (weekly, semimonthly, quarterly, custom) and amount or raise schedules are set through the command line for now. Edits can be undone with Edit > Undo. Comparing actual spending with the budget is not built yet.

## Screenshot

    bin/finplan [--demo] --screenshot <out.png> [--tab <name>] [--frames N] [--size WxH] [--select <name>] [--open <picker>]

Screenshot mode lets the GUI be inspected without a person at the window. It draws `--frames` frames (default 10) without waiting for input, reads the last frame back from the framebuffer, writes it to `<out.png>` at the framebuffer's pixel size and exits 0; any failure exits non-zero with a message.

- `--tab <name>` is the tab to show: Graph (the default), Projections, Assets, Accounts, Budget or Markets; the name is case-insensitive.
- `--size WxH` is the window size in pixels (default `1280x800`).
- `--select <name>` selects the projection, asset or account of that name, and works only with the Projections, Assets and Accounts tabs. On the Assets tab an account's default asset is not listed and cannot be selected. An unknown name is an error.
- `--open <picker>` shows a picker open: `add-line` (the Graph tab's Add line picker), `target` (a Projections tab target picker, which needs `--select <projection>`) or `member` (the Assets tab's member picker, which needs `--select <asset>`). It is an error for an unknown picker or with a tab other than the one named. The add-line and target lists are scrolled so the boundary between accounts and assets is in view.
- Each of `--tab`, `--frames`, `--size`, `--select` and `--open` is an error without `--screenshot`.

If `SDL_VIDEO_DRIVER` is unset and neither `DISPLAY` nor `WAYLAND_DISPLAY` is set, screenshot mode sets `SDL_VIDEO_DRIVER=offscreen` itself; it can also be set explicitly, which needs no display server (OpenGL still needs a driver, such as Mesa's software one):

    SDL_VIDEO_DRIVER=offscreen bin/finplan --demo --screenshot /tmp/accounts.png --tab Accounts

## Command line

    bin/finplan [--demo | --db <path>] <command> [arguments] [flags]

With no command (`finplan` or `finplan --demo`) the app opens its window as above. With a command, the same binary reads or writes the database and prints one JSON document on stdout, without a window. Run `bin/finplan help` for the commands and `bin/finplan help --json` for the same description as JSON, generated from the command definitions the parser uses.

- `--demo` (a throwaway seeded database, removed on exit) and `--db <path>` work on every command, before or after it; using both is a usage error. With neither, the real database is used (same location as the app).
- Read commands never write: they open the database read-only. A missing file is an error (`no_database`) and is not created. A database older than the program gives `old_schema` (open it with the app first so it is upgraded), and a newer one `future_schema`.
- Errors are `{"error":{"code","message"}}` on stdout, with exit code 2 for usage errors and 1 for all others. Codes: `usage`, `not_found`, `ambiguous`, `no_database`, `old_schema`, `future_schema`, `database`, `engine`, and for the write commands `unknown_target`, `name_taken`, `has_children`, `cycle`, `invalid`, `in_use` (an account with record entries, or that is the counter account of one: close it instead), `confirm_required` (a delete that changes other objects and was not given `--force`; the error also has `affected`) and `record_changed` (the record changed since it was loaded). `not_found` also covers unknown assets, accounts and entries.
- Dates are `YYYY-MM-DD`, and `null` when absent or unbounded. Statuses are lowercase: `default`, `actual`, `projected`, `partly_projected`. Reality appears as `"projection": null`.

Read commands:

| Command | Output |
| --- | --- |
| `account list` | accounts: id, name, type, tax, owner, `stated_rate`, `default` (the default balance intervals), `closed` (close date or null), `balance` (on the record's today; 0 once today is after the close date), `last_data` (date of the latest record entry, null without data) and `stale` (an open account whose last data is more than 45 days before today) |
| `account entries <account> [--voided]` | the account's snapshots and transactions with current values (voided entries left out unless `--voided`, which includes them with a `void` field) |
| `entry history <entry-id>` | every revision of one entry: original values, then each correction or void |
| `asset list` | assets with their `kind` (`account` for an account's default asset, else `asset`), their members, members left out as invalid, any resolve error, `default` (default balance intervals) and `params` (parameter settings) |
| `budget list` | budget items with recurrence, start, end, indexing, accounts, and `amount` and `growth` (the value in effect today) with `amount_schedule` and `growth_schedule` |
| `budget totals` | `as_of` (the record's today), `monthly_income`, `monthly_expenses` and `monthly_net`, in today's dollars rounded to cents, using the amount in effect on `as_of`: a monthly item counts once a month, semimonthly twice, biweekly 26/12, weekly 4.348, quarterly a third, annual a twelfth, one-time not at all, and a custom repeat by its average (every N days 30.4375/N, weeks 4.348/N, months 1/N, years 1/(12N)); transfers are left out; only items active on `as_of` count |
| `projection list` | projections: name, parent, start, override history, modification count |
| `projection show <name>` | a projection's settings, per-asset settings, the hypothetical accounts it defines (`accounts`: id, name, type, tax, stated_rate, opening, from; not the inherited ones) and modifications in the order they apply (each with `missing`, and `missing_target` naming a target that no longer exists) |
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
| `mod add <projection> --target T --property amount\|growth\|default\|inflation [--mode set\|scale] [--value N [--from D [--end D]]] [--at I]` | add a modification (`growth` also suits an item: an annual raise; `default` suits an account or asset: its default balance, used only on dates with no actual data; with `--mode scale` the value is a multiplier, 0.7 cuts by 30%; `--end` is the inclusive last date and needs `--value` and `--from`); `T` is `item:<id or name>`, `account:<id or name>` (including the projection's own and inherited accounts), `asset:<id or name>` or `global` |
| `mod edit <projection> <index> [--mode set\|scale] [--value N [--from D [--end D]]]` | replace a modification's values; `--mode` alone changes the mode and keeps the values |
| `mod move <projection> <from> <to>` | move a modification so that it ends up at index `to` |
| `mod remove <projection> <index>` | remove a modification |
| `projection add-account <projection> --name N --type T --tax X --from D [--id I] [--rate R] [--opening N]` | add a hypothetical account that exists only under that projection and the ones building on it, is never written to the record and is picked up by rule-based aggregates such as All cash and Net worth; it has no value before `--from`, then `--opening` (negative for a debt) growing at `--rate` |
| `projection edit-account <projection> <account> [--name N] [--type T] [--tax X] [--rate R] [--opening N] [--from D]` | change an account the projection defines; at least one flag |
| `projection remove-account <projection> <account>` | remove an account the projection defines; refused (`in_use`) while a modification, item or asset setting of the projection or one building on it uses it |
| `item add <projection> [flags]` | add a new budget item that exists only under that projection and the ones building on it |
| `item edit <projection> <item> [flags]` | change an item the projection adds, keeping its id |
| `item end <projection> <item> <date>` | make a budget item (the record's or one added in the chain) pay nothing from a date on |
| `asset new <name> [--id]` | add an aggregate asset with no members |
| `asset rename <asset> <new-name>` | rename an aggregate asset |
| `asset delete <asset>` | delete an aggregate asset and its members, and remove it from other assets' members and projections' asset settings and modifications for it (modifications of the accounts under it stay); without `--force`, refused with `confirm_required` and the list in `error.affected` when anything refers to it; `--dry-run` prints the plan |
| `asset add-member <asset> (--account A \| --asset A \| --rule [--types T] [--tax T])` | add a member after the others |
| `asset remove-member <asset> <seq>` | remove a member |
| `asset add-default <asset> --value N [--from D] [--to D]` | add a default balance interval |
| `asset edit-default <asset> <index> --value N [--from D] [--to D]` | replace a default balance interval whole in place (an omitted `--from`/`--to` means unbounded); refused as no change when equal |
| `asset move-default <asset> <from> <to>` | move a default balance interval so it ends up at index `to`; where intervals overlap the later one wins, so a move can change the balance |
| `asset remove-default <asset> <index>` | remove a default balance interval; the index counts from 0 in the `default` list of `asset list` |
| `asset set-param <asset> [--param growth] (--value N [--from D] \| --clear)` | set or clear the asset's growth setting; without `--value` and `--clear` a schedule is read as JSON |
| `asset params <asset>` | (read) the asset's `default` intervals and `growth` schedule, `today`, and for each account under it (`accounts`) its growth `layers` (`source` `stated_rate` or `asset`, `asset`, `schedule`, `today`, `applies`: false when overwritten today), the `effective` schedule and `today` |
| `account new <name> --type T --tax T [--owner] [--rate] [--id]` | add an account and its default asset |
| `account edit <account> [--name --type --tax --owner --rate]` | change an account's attributes; a rename also renames its default asset |
| `account close <account> [--date D]` | close an account: it keeps its history, is left out of pickers for new lines and objects, and reads 0 after the date (the record's today when omitted); refused for an account already closed, one a projection defines, or a date before its last record entry or an entry that names it as counter account |
| `account reopen <account>` | clear the close date; no change for an open account |
| `account delete <account>` | delete an account and its default asset, and remove it from asset members, asset settings and modifications for it or its default asset, and delete the base and projection-added budget items that move money in or out of it with the modifications of them; without `--force`, refused with `confirm_required` and the list in `error.affected` when anything refers to it; `--dry-run` prints the plan; refused (`in_use`) even with `--force` or `--dry-run` while it has record entries or is the counter account of any entry (close it instead) |
| `entry add-snapshot <account> --date D --balance N` | record a balance |
| `entry add-transaction <account> --date D --amount N --kind income\|expense\|transfer [--counter A] [--memo]` | record a transaction |
| `entry correct <entry-id> [--date --amount --kind --counter --memo] [--note]` | correct an entry; the earlier values stay in `entry history` |
| `entry void <entry-id> [--note]` | take an entry out of the record |
| `budget add [item flags]` | add an item to the base budget |
| `budget edit <item> [item flags]` | change a base budget item |
| `budget end <item> <date>` | set the base item's last date |
| `budget delete <item>` | delete a base budget item; a projection's modification of it is kept but points at nothing, so the engine skips it and `projection show` marks it `missing` |
| `asset-setting add <projection> --asset A [--start D] [--override-history true\|false] [--at N]` | give an asset its own start date and/or override history in a projection; at least one is needed, the asset must be in the projection's view (`unknown_target`); settings apply in order and the last wins |
| `asset-setting edit <projection> <index> [--asset A] [--start D\|none] [--override-history true\|false\|none]` | change a setting; `none` makes the asset inherit; indexes count from 0 as `projection show` numbers `asset_settings` |
| `asset-setting move <projection> <from> <to>` | move a setting so that it ends up at index `to` |
| `asset-setting remove <projection> <index>` | remove a setting |

Write commands go through the same edit operations (`internal/ops`) as the app, so both agree on what an edit may do. Details:

- Database: `--demo`, `--db` and the default work as for reads, but a write command opens the database with `store.Open`, so it migrates an older database, and still never creates a missing one (`no_database`). Read commands still never write.
- `--dry-run` (every write command) applies the edit to a copy, prints what would change and saves nothing.
- Output of the projection commands is `{"dry_run","changed","projection"}`, the projection in the same shape as `projection show`, after the edit. The asset, account, entry and budget commands print `{"dry_run","changed",<object>}` with `asset`, `account`, `entry` or `item` in the shape its read command prints (`asset delete` and `account delete` add `removed` and `affected`, a list of `{kind, in, detail, effect}`, `entry void` adds `voided`; `account new` also prints its default `asset`); an entry added in a dry run has `id` null. An edit that changes nothing prints `"changed": false` and exits 0. `projection rename` adds `renamed` (`{from,to}`, or null), `rewritten_presets` and `warnings` (presets that could not be rewritten; the rename is saved regardless). `projection delete` prints `removed` and the projection as it was.
- Projection accounts: `projection add-account` needs `--name`, `--type`, `--tax` and `--from`; `--id` is made from the name when omitted and must differ from every account and asset id and from the accounts of the projection's chain and descendants (siblings may reuse an id). The account exists under that projection and the ones building on it, reads 0 (no data) before `--from`, is `--opening` on that day and grows at `--rate` from then. Balance, breakdown and starts accept it under `--projection`, as do `mod --target account:`, and `item --account/--from/--to`. A breakdown over the opening date has an `opening` line. An item that touches such an account counts, on every side, only after the latest of its accounts' cutoffs, so a transfer's two sides agree. To model buying a house: add the home as a `property` account (price as `--opening`, appreciation as `--rate`) and the mortgage as a `mortgage` account (negative `--opening`, interest as `--rate`), both `--from` the purchase date; add the down payment as a one-time transfer from checking to `outside`, and the monthly payment as a transfer from checking to the mortgage. Amortization is not modelled.
- Rates (growth, inflation) are fractions: `0.03` is 3%. A negative rate given as an argument needs `--` before it.
- Modification values are a constant (`--value`, from `--from` to the inclusive `--end`, always and open-ended when omitted; `--end` without `--value` and `--from` is a usage error) or a schedule read as JSON on stdin or from `--json-file`: `[{"from":"YYYY-MM-DD","end":"YYYY-MM-DD" or null,"value":number}]`, sorted and not overlapping. Input is decoded strictly: unknown fields and trailing data are usage errors.
- Budget items: `--kind income|expense|transfer`, `--name`, `--amount` (above zero), `--every one-time|weekly|biweekly|semimonthly|monthly|quarterly|annually|custom` (`annual` also works), `--start <date>` (`--on` is the same; give one), `--day` (monthly, semimonthly), `--day2` (semimonthly), `--count` and `--unit days|weeks|months|years` (custom), `--end <date|none>`, `--account` (income, expense), `--from` and `--to` (transfer), `--indexed`, `--growth` (annual raise as a fraction, 0.03 for 3%) and for `item add` `--id`. Or give the item as JSON (the shape `budget list` prints) on stdin or `--json-file` instead of the flags. A new item needs a start; days 29 to 31 fall on the last day of shorter months; an annual item's day is checked against its month; weekly, biweekly, quarterly and custom repeats count from the start (or the recurrence anchor in JSON). `--amount` and `--growth` take one number; for amounts or raises that change over time give the item as JSON, where `amount` and `growth` are a number or `[{"from","end","value"}]` (the `budget list` output adds `amount_schedule` and `growth_schedule`, and a number given with a schedule must equal its value today). An edit without a date flag keeps the start. The id is made from the name (a unique slug) when not given. An item is stored as an add_item modification. `item end` appends an amount modification with value 0 from the date.
- Targets are checked by the shared operations: the property must suit the target (amount or growth for an item, growth for an account or asset, inflation for global), and the target must exist at the modification's position, else `unknown_target`: a record budget item, an item added by an ancestor projection, or an item added by an earlier row of the same projection. `mod add` cannot add an item (use `item add`). A move, removal, parent change or removal of an added item is refused if a modification that resolved before would no longer resolve; modifications already dangling do not block an edit.
- Assets: a member that overlaps an earlier one is never refused; the later member is listed under `invalid`. An account's default asset (`acct:<id>`) cannot be renamed, deleted or have members changed. `asset remove-member` and `asset delete` renumber the members' seqs 1..N in order, so later seqs can shift. On a default asset, `add-default` and `remove-default` change the account's own default balance and `set-param` its settings.
- Record: entries are never edited in place. A snapshot on a date that already has one is refused (use `entry correct`); `entry correct` and `entry void` go through the store's corrections, and their dry run runs the same checks in a rolled-back transaction. A transfer needs a counter account that exists and is not the account itself. The stated rate is limited to -1..1.
- Base budget: `budget end` sets the item's end date and refuses one before the start. Semimonthly items can be created (with `--day` and `--day2`), edited and ended.
- Exit codes and error codes are as above: `usage` exits 2; `not_found`, `unknown_target`, `name_taken`, `has_children`, `cycle`, `invalid`, `in_use`, `record_changed`, `database`, `no_database`, `old_schema`, `future_schema` and `ambiguous` exit 1.

Run `bin/finplan help --json` for every command's arguments, flags (with types), whether it writes and what JSON input it reads.

An agent skill for using the CLI is in `.claude/skills/finplan/SKILL.md`.

## Fonts

The Inter font is bundled under the SIL Open Font License; the licence text is in `cmd/finplan/fonts/OFL.txt`.
