---
name: finplan
description: Inspect and edit a finplan financial plan from the command line. Use when asked about accounts, balances, projections, budgets or recorded entries in finplan.
---

# finplan

finplan is a personal financial planner. Its database holds accounts with recorded snapshots and transactions, assets that group accounts, a budget, and named projections (what-if changes over the base plan). The `finplan` binary with a subcommand reads or edits the database and prints JSON on stdout; without a subcommand it opens the window.

## Rules

- Use the CLI. Never open the database file or run SQL against it, and never edit it by hand.
- The recorded history is append-only: new balances and transactions are added with `entry add-snapshot` and `entry add-transaction`, and a change to a recorded entry is `entry correct` or `entry void`, never an edit in place or in the database.
- Run `finplan help --json` to discover the commands, their arguments, flags and output shapes; do not guess them.
- Add `--demo` to try a command against a throwaway invented family, or `--db <path>` to use another database file. The two cannot be combined.
- Read commands never write the database. Write commands edit projections and their items (`projection new`, `mod add`, `item add`, ...), assets (`asset new`, `asset add-member`, ...), accounts (`account new`, `account edit`), the record (`entry add-snapshot`, `entry correct`, `entry void`, ...) and the base budget (`budget add`, `budget edit`, `budget end`); see `help --json`. A budget item's `--every` is weekly, biweekly, semimonthly (with `--day` and `--day2`), monthly, quarterly, annually, one-time or custom (with `--count` and `--unit` of days, weeks, months or years), `--start` is its first date, and an amount or growth that changes over time is given as JSON, a list of `{from, end, value}` segments, instead of a number. Every one takes `--dry-run`, which validates and prints what would change but saves nothing, and prints the object it changed. Errors are JSON `{"error":{"code","message"}}` on stdout; exit 2 is a usage error, 1 anything else.
- Keep real personal financial data out of files, commits and examples. Use the demo family for anything you write down.
