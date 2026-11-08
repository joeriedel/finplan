# finplan design

This document describes finplan's data model, projection engine, analysis tools and agent access. It records decisions, not code: where the code and this document disagree, one of them is wrong and should be fixed. It describes capabilities only; a user's own accounts, household and numbers are data entered into the app, never written here or anywhere in the repository.

Status: agreed design, not yet implemented beyond the UI scaffold.

## Principles

- **The record is append-only.** What actually happened is never rewritten. Any entry can be corrected at any time; the UI feels like an edit, but stores a correction that supersedes the original, which is kept and viewable.
- **Store inputs, never computed output.** Projections store only their inputs, so engine fixes and improvements refine every projection, old ones included.
- **Stored formats load forever.** Old inputs are migrated, never dropped.
- **One code path.** The GUI and the CLI change data only through the same library functions, so neither can bypass a rule the other enforces.
- **Every number is explainable.** The change in any account over any interval breaks down into its causes (income, expenses, tax, growth, transfers, shocks), for the UI and for the agent.
- **The engine is a standalone library** with its own tests, separate from the UI.

## Accounts

An account is naked data: a balance history, a transaction history (and, for investment accounts, holdings), plus descriptive attributes. Attributes are facts, not behavior. Examples:

- type: checking, savings, credit card, brokerage, 401k, TSP, IRA, Roth IRA, HSA, 529, mortgage, loan, equity (RSUs)
- tax treatment: taxable, tax-deferred, tax-free, 529
- owner
- a stated interest rate, which serves only as a default when the account is projected

Liabilities carry negative balances.

Accounts come from aggregation (SimpleFIN Bridge, including holdings where the institution provides them), from file import, or from manual entry. A projection can also introduce hypothetical accounts.

### Transactions

Each transaction is classified as **income**, **expense** or **transfer**. A transfer moves money between two of the user's own accounts and is never spending: a credit card payment from checking is a transfer, otherwise every card purchase would count twice. Classification and categories are corrections layered over the synced rows, so the synced data stays untouched.

## Assets

An asset carries behavior. It wraps one or more members, each of which is an account or another asset.

- Every account has a **default asset** wrapping it alone, which surfaces the account's attributes (such as its interest rate) as defaults.
- An **aggregate asset** wraps several members, for example "all cash" over every cash account, or "net worth" over everything (liabilities are negative, so net worth is a plain sum).
- Membership is an explicit list, a rule (for example: type is cash, so newly linked accounts join on their own), or both.
- Assets nest freely; they form a graph, and cycles are rejected.
- Every asset has a **default balance** (0 unless set) with an as-of date. It grows at the asset's rate from that date and may have an end date. The end date covers the hand-off from a stand-in number to a real account: "use 40,000 for this account until its real data begins". The jump at a hand-off is accepted and marked on the graph.

At any time, an asset's value is its default balance plus the sum of its distinct underlying accounts at that time. Money lives only in accounts; an asset never holds a balance of its own beyond its default balance.

Any asset can be graphed and projected, individually or as an aggregate, by the user or by the agent.

### Overlap

An asset must never count an account twice, but nothing is refused. When a member would count something already included (for example, checking is already in the asset through "all cash"), that member is accepted but marked invalid: shown red with an error icon and a tooltip naming the duplicate, and left out of the sum. The user fixes it however they like. When members overlap, the later-added member is the one marked invalid, so the result is deterministic.

The check runs whenever an asset is used, not only when it is edited, because editing a wrapped asset can create overlap later. The same red marking gives immediate feedback while building an asset.

## Parameters and precedence

Every behavioral property is a parameter: growth rate, allocation, contribution rules, withdrawal order, and so on. Every parameter is a **schedule over time**: piecewise values by date range ("4% until 2029, then 6% until 2035, then 2%"). A constant is a schedule with one segment.

An asset can override the parameters of anything it wraps. One precedence rule applies: **the setting closest to the outside wins**, down to the account's own default. A projection is the outermost layer, so its settings beat every asset's. A variant such as "all cash at 5%" is a new asset wrapping "all cash" and setting 5%, which leaves "all cash" untouched everywhere else.

When two sibling members of the same asset set different values for the same shared account, nothing decides between them; that is reported as a conflict and settled by setting the value at an outer level.

The engine still computes each account individually, because flows land on accounts and tax treatment differs per account; the assets supply the resolved parameters.

## Budgets

The budget is the forecast of every flow, and the shared context for extrapolation: every asset is extrapolated against the same budget, so "all cash" and "net worth" always agree about the same accounts.

A budget item has:

- an amount and a schedule: a calendar rule (biweekly, semimonthly, monthly on a given day, annual on a date) or a one-time date
- optional start and end dates
- whether it is indexed to inflation
- the account it lands in or draws from

Items cover income (gross salary, RSU vests, bonuses, pensions, Social Security) as well as expenses. Matching rules (payee, category, account) map transactions to budget items, so baselines can be derived from history and actual spending compared with the budget.

Taxes are not budget items. The engine computes them (see Taxes), and the net pay after tax and payroll deductions is what lands in the account.

### Past and future

For each account, the engine uses actual transactions up to the account's last real data and budget items after it. Accounts sync on different dates, so the boundary is per account, not one global date.

## Projections

A projection is a named set of hypothetical modifications layered over the base (the real accounts and assets plus the base budget). It stores only its changes, never a copy, so corrections to the base flow into every projection.

A projection can modify **any property of any object on a timetable**: a modification is an object, a property and a schedule of values. This covers, for example:

- budget changes: add, change or end an item on a date ("new job in March 2027", "cut dining 30% from 2026"); dated budget changes are what other tools call events
- parameter schedules ("brokerage at 4% until 2029, then 6% until 2035, then 2%")
- one-off actions: a market shock ("large cap −30% in March 2035"), a rebalance ("brokerage to 75% treasuries, 25% small cap in 2027"), a withdrawal or transfer
- hypothetical accounts and default balances
- the extrapolation start date, so a projection can begin in the past and ignore real data after that date
- global assumptions such as inflation

A projection may build on one parent projection (a chain, never a merge of two). A modification whose target no longer exists, for example a change to a budget item later deleted from the base, is marked red and skipped, as with overlap.

A plotted line is an asset under a projection, or under reality: `asset.balance(t, projection)`. A report is an asset viewed over a time range. Extrapolation continues indefinitely; the view decides how far to look.

### Scope

Extrapolating an asset computes only the accounts under it. A transfer to an account outside it counts as money leaving. Income comes from the budget regardless of scope, and taxes are computed for the household as a whole, so a narrow asset ("all cash") still sees correct net pay.

## Market classes and holdings

Returns come from **market classes** (treasuries, large cap, small cap, international, cash, and so on), not from accounts. An account's growth is its allocation across classes times the classes' returns. A plain fixed rate, such as a savings account's interest, is a single fixed-rate class, so simple accounts stay simple.

- Each investment account tracks a **sub-balance per class**, filled from holdings: each held symbol maps to one or more classes (a target-date fund is a blend whose mix changes over time; TSP funds map one-to-one).
- Allocations drift between rebalances; a **rebalance** is an explicit dated action.
- A **shock** hits a class, and each account takes it in proportion to its exposure.

### Real market data

Classes are pinned to real data for the past and for defaults; the future stays an assumption the user can override.

- Class history comes from real total-return series (adjusted close, dividends reinvested) of a proxy fund per class. It drives projections starting in the past, splits real balances between syncs, and supplies return sequences for Monte Carlo.
- Each class's default forward return is its long-run historical return.
- TSP fund share prices come from tsp.gov.
- The price source is not yet chosen; providers' adjusted closes differ slightly, so the first market-data work should compare them.

## Cost basis

Taxable accounts track cost basis as dated purchase lots per market class, sold oldest first or by the broker's method. RSUs track exact lots per grant and vest, because each vest's price sets its basis.

Trades inside tax-advantaged accounts (401k, TSP, IRA, Roth IRA, HSA, 529) are never taxed. In a taxable account, a rebalance or withdrawal realizes gains (short-term or long-term by holding period) that feed that year's taxes. Rebalancing with new money involves no sale.

## Taxes

The engine computes taxes from income, realized gains, contributions and deductions, using tax rules shipped as engine data, versioned in code: brackets, standard deductions, credits and contribution limits by year, with rules for extending them past the last known year (indexed to inflation). Tax rules are engine data, not projection inputs.

Scope: US federal and Virginia, modeled properly; no generic multi-state support. Filing status, dependents (including the child tax credit), the Virginia 529 deduction, early-withdrawal penalties and the net investment income tax are in scope.

Withholding, estimated payments, refunds and amounts owed are what land in accounts. At each year end the engine reconciles tax due against tax paid.

## The engine

The engine is event-driven, with no fixed time step. It advances from one dated flow to the next (each paycheck, bill, vest, contribution, shock and rebalance on its actual date) and grows balances over exactly the time elapsed between them. Granularity is therefore not a property of the engine: `balance(t)` is defined for any instant, and the caller chooses how to sample it (the graph by its zoom level, a report by its interval, the CLI by a flag). Zoomed out over decades the graph samples sparsely; zoomed in on a month it shows each paycheck landing.

- **Growth** compounds by each account's own convention: daily for credit-card interest, per period for a mortgage's amortization schedule, and continuously over elapsed time for market classes.
- **Ordering on one date:** income, then payroll deductions (401k/TSP and employer match, HSA, withholding), RSU vests taxed as income, expenses, debt payments, then contributions and where leftover cash goes (a waterfall), then shocks and rebalances.
- **Taxes** are computed per tax year, because that is how they work: withholding and estimated payments flow on their dates, and each year end reconciles tax due against tax paid.
- **Breakdowns** cover any interval the caller picks.
- **Monte Carlo** draws class returns at a chosen base resolution, since randomized returns need a step; the rest of the engine is unchanged.

Capabilities the engine must model: 401k and TSP limits, employer match, RSU vesting and sale, mortgage amortization, 529 rules, inflation, FERS pensions (annuity, the supplement before 62, survivor options), and retirement drawdown (withdrawal order, Social Security, required minimum distributions, Roth conversions).

The engine must be fast: goal solving and Monte Carlo rerun it thousands of times. Sixty years of biweekly pay and monthly bills is a few thousand events per run.

## Analysis tools

In build order:

1. **Single projection:** an asset's curve plus the breakdown for any interval.
2. **Compare:** several projections and reality on one graph, and the difference between any two.
3. **Plan versus actual:** an earlier projection against what really happened.
4. **Sensitivity:** vary one parameter across a range to get a band of outcomes.
5. **Goal solving:** for example, the earliest retirement date where money lasts to a given age.
6. **Monte Carlo:** randomized class return sequences, with realistic correlations, giving percentile bands.
7. **Spending analysis:** category trends and baselines from the record.

## UI

Highly dynamic graphs: pan, zoom and scroll horizontally with no fixed end date. Several projections and reality overlaid. Per-series visibility and color. Views saved as presets. Fast and lightweight.

**Difference bands:** a shaded area between any two series, for example a 401k under a base projection and under one with an early withdrawal, showing the growing gap of tax, penalty and lost growth. The gap is readable at the cursor and optionally in today's dollars. Fill is translucent by default; hatching is optional and needs custom drawing clipped to the band, since ImPlot has no hatch patterns.

The GUI picks up changes made outside it (by the CLI) without a restart: SQLite runs in WAL mode, and the GUI polls `PRAGMA data_version` and reloads only when it changes.

## Storage and privacy

All app data lives in SQLite outside the repository (under `~/.local/share/finplan/`). File → Backup... writes a consistent copy (`VACUUM INTO` or the SQLite backup API). Credentials live in the OS keyring, never in files. The repository is public: real financial data never enters it, and tests and fixtures use invented numbers, such as an invented budget for a family of six.

## CLI and agent access

The `finplan` binary doubles as a CLI: with no arguments it opens the GUI, and with a subcommand it does one job through the shared library and exits. Every command prints stable JSON, and errors are JSON with a nonzero exit code. Illustrative shape, not a final command set:

    finplan asset list
    finplan asset balance "All cash" --projection "New job" --from 2026-01 --to 2060-12
    finplan projection set "New job" brokerage.growth "2027-01:0.04,2030-01:0.06"
    finplan budget add --projection "New job" --item groceries --amount 1800 --monthly
    finplan record correct <entry-id> --amount 1234.56

The agent (Claude) has full access: it reads the record, assets, budgets, projections and engine output, and writes projections, budgets and record entries. Record writes go through corrections, like the GUI's. Whatever the agent reads is sent to its model provider.

A thin agent skill lives in this repository at `.claude/skills/finplan/SKILL.md`: what finplan is, the rules (use the CLI, never touch the database; record changes are corrections), and an instruction to run `finplan help --json`, which the binary generates so the command reference cannot drift from the code. Symlinking the skill into `~/.claude/skills/finplan` makes it available in any terminal.
