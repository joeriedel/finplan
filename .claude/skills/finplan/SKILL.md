---
name: finplan
description: Inspect a finplan financial plan from the command line. Use when asked about accounts, balances, projections, budgets or recorded entries in finplan.
---

# finplan

finplan is a personal financial planner. Its database holds accounts with recorded snapshots and transactions, assets that group accounts, a budget, and named projections (what-if changes over the base plan). The `finplan` binary with a subcommand reads the database and prints JSON on stdout; without a subcommand it opens the window.

## Rules

- Use the CLI. Never open the database file or run SQL against it, and never edit it by hand.
- The recorded history is append-only: a change to a recorded entry is a correction or a void, never an edit in place.
- Run `finplan help --json` to discover the commands, their arguments, flags and output shapes; do not guess them.
- Add `--demo` to try a command against a throwaway invented family, or `--db <path>` to use another database file. The two cannot be combined.
- Read commands never write the database. Write commands change projections and their budget items only (`projection new`, `mod add`, `item add`, ...; see `help --json`); every one takes `--dry-run`, which validates and prints what would change but saves nothing. Changes to recorded entries are not among them: those go through corrections. Errors are JSON `{"error":{"code","message"}}` on stdout; exit 2 is a usage error, 1 anything else.
- Keep real personal financial data out of files, commits and examples. Use the demo family for anything you write down.
