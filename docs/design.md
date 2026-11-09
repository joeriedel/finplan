# finplan design

This document describes finplan's data model, projection engine, analysis tools and agent access. It records decisions, not code: where the code and this document disagree, one of them is wrong and should be fixed. It describes capabilities only; a user's own accounts, household and numbers are data entered into the app, never written here or anywhere in the repository.

Status: agreed design. The data model, asset rules and a placeholder projection engine with breakdowns exist as standalone libraries (`internal/model`, `internal/engine`) that import only the standard library; the GUI does not use them yet. Financial rules are placeholders (see Implementation notes); market classes, shocks and rebalances have types only.

## Principles

- **The record is append-only.** What actually happened is never rewritten. Any entry can be corrected at any time; the UI feels like an edit, but stores a correction that supersedes the original, which is kept and viewable.
- **Store inputs, never computed output.** Projections store only their inputs, so engine fixes and improvements refine every projection, old ones included.
- **Stored formats load forever.** Old inputs are migrated, never dropped.
- **One code path.** The GUI and the CLI change data only through the same library functions, so neither can bypass a rule the other enforces.
- **Every number is explainable.** The change in any account over any interval breaks down into its causes (income, expenses, tax, growth, transfers, shocks), for the UI and for the agent.
- **The engine is a standalone library** with its own tests, separate from the UI.

## Accounts

An account is naked data: a balance history, a transaction history (and, for investment accounts, holdings), plus descriptive attributes. Attributes are facts, not behavior. Examples:

- type: checking, savings, credit card, brokerage, 401k, TSP, IRA, Roth IRA, HSA, 529, mortgage, loan, equity (RSUs), property (such as a home)
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

Money lives only in accounts. Any asset can be asked for its balance on any date.

### Default balances

Every account and asset has a **default balance**: a fallback value for dates with no actual data. It is not added to anything and it does not grow; growth is a projection concept.

- A **base** default of 0 fills every date.
- The user can add **intervals** (from, optional to, value) that override the base on those dates (a missing `to` leaves the interval open-ended, and `to` is inclusive), for example "use 40,000 for this account until its real data begins".
- **Actual data always beats any default.** Defaults apply only on dates with no actual data.

A balance query returns the value with a flag saying whether it came from actual data or from defaults.

An account has actual data on every date within its real data span; gaps between snapshots inside the span are filled from transactions, never from defaults. An asset's **actual-data coverage** is the union of its members' coverage, which may be disjoint, and its **last actual data date** is the end of that union.

### Resolving an asset's balance

Actual data comes only from accounts; defaults never count as actual data, however deeply nested. For an asset on a given date:

- If any underlying account has actual data on that date, the asset's value is the sum of its members, each resolved the same way (its actual data, its own default, or 0).
- Otherwise, if the asset has a default interval covering that date, that value wins over its members' defaults.
- Otherwise, the asset's value is the sum of its members, each resolved the same way. The 0 base never beats a member's explicit interval.

Hand-offs between defaults and actual data can make a line jump; that is accepted and marked on the graph.

Any asset can be graphed and projected, individually or as an aggregate, by the user or by the agent.

### Overlap

An asset must never count an account twice, but nothing is refused. When a member would count something already included (for example, checking is already in the asset through "all cash"), that member is accepted but marked invalid: shown red with an error icon and a tooltip naming the duplicate, and left out of the sum. The user fixes it however they like. When members overlap, the later-added member is the one marked invalid, so the result is deterministic.

The check runs whenever an asset is used, not only when it is edited, because editing a wrapped asset can create overlap later. The same red marking gives immediate feedback while building an asset.

## Parameters and precedence

Every behavioral property is a parameter: growth rate, allocation, contribution rules, withdrawal order, and so on. Every parameter is a **schedule over time**: piecewise values by date range ("4% until 2029, then 6% until 2035, then 2%"). A constant is a schedule with one segment.

A segment has a start (`From`), an optional end (`End`) and a value. The end is inclusive, so a segment covers [From, End], like `BudgetItem.End` and `Interval.To`. A segment that touches the next has no end: it runs until the day before the next segment's start, or indefinitely if it is the last. Segments are sorted by start and never overlap: an end must be before the next segment's start, and an end may equal its own start (a one-day segment) but not precede it. `NewSchedule` rejects an unsorted, duplicate or overlapping schedule, and an end before its start, with an error, and since segments are unexported every schedule goes through it. It normalizes touching to one representation: an end on the day before the next start is dropped, and an end of `MaxDate` is dropped as no end. An earlier end leaves a gap.

Dates that no segment covers (before the first segment, in a gap, or from the last segment's end on) are not covered by the setting, and the value beneath shows through. A setting's start is its first segment's start and its end is its last segment's end; there is no separate setting-level start or end. Compound growth over dates no segment covers contributes no growth.

Settings apply **in order, the last applied winning**. For each account on a line (a line is an asset queried under a projection), the value starts as the account's own (for growth, its stated rate). Settings are then applied in sequence, each overwriting the value for the accounts it covers, on the dates its schedule covers; what remains is the value. There are no modes and no specificity rules: the order alone decides.

The order is:

1. **Asset definitions.** An asset can set the parameters of anything it wraps. Along the path from the account to the line's asset, the account's own (default) asset comes first, then inner assets before outer ones, and unrelated assets in scope order; within one asset, list order. A variant such as "all cash at 5%" is a new asset wrapping "all cash" and setting 5%, which leaves "all cash" untouched everywhere else.
2. **The projection's parent chain**, oldest ancestor first, each projection's settings in list order.
3. **The projection's own settings**, in list order.

"All cash 7%" followed by "Checking 1%" gives Checking 1% and every other cash account 7%; in the reverse order Checking gets 7%. Overlapping siblings need no conflict rule: the later one wins.

Settings layer per covered date: a later setting overwrites the value beneath only on the dates its segments cover, and the earlier value stands everywhere else. A constant covers every date, so it overwrites entirely. This applies to growth and other parameter settings on asset definitions and in projections, to global inflation modifications and to budget item amount changes; inflation and amount modifications no longer replace the whole value.

Per-asset start and `override_history` follow the same order but are single values with no segments: each setting replaces the one before it (the last wins). They are projection settings, never properties of an asset definition: "Retirement 2024" followed by "TSP 2020" gives the TSP its 2020 start.

A projection's setting reaches every account under its target asset on every line queried under that projection, even when the line's asset does not contain the target; a setting aimed at an account reaches just that account. An asset-definition setting applies only on lines whose asset is, or contains, that asset. An account may therefore show different values on different lines; that is intended. A setting that names an unknown asset is an error.

The UI will keep settings lists ordered and reorderable, and will mark a setting as overwritten for an account.

The engine still computes each account individually, because flows land on accounts and tax treatment differs per account; the assets supply the resolved parameters.

## Budgets

The budget is the forecast of every flow, and the shared context for extrapolation: every asset is extrapolated against the same budget, so "all cash" and "net worth" always agree about the same accounts.

A budget item has:

- an amount and a schedule: a calendar rule (biweekly, semimonthly, monthly on a given day, annual on a date) or a one-time date
- optional start and end dates (the end is inclusive; a missing end means open-ended, and so does a missing start)
- whether it is indexed to inflation
- the account it lands in or draws from

Items cover income (gross salary, RSU vests, bonuses, pensions, Social Security) as well as expenses. Matching rules (payee, category, account) map transactions to budget items, so baselines can be derived from history and actual spending compared with the budget.

Taxes are not budget items. The engine computes them (see Taxes), and the net pay after tax and payroll deductions is what lands in the account.

### Past and future

The engine reads back actual data where it applies and uses budget items after that. Where that switch happens is set by the projection start rule (see Projections).

## Projections

A projection is a named set of hypothetical modifications layered over the base (the real accounts and assets plus the base budget). It stores only its changes, never a copy, so corrections to the base flow into every projection.

A projection can modify **any property of any object on a timetable**: a modification is an object, a property and a schedule of values. This covers, for example:

- budget changes: add, change or end an item on a date ("new job in March 2027", "cut dining 30% from 2026"); dated budget changes are what other tools call events
- parameter schedules ("brokerage at 4% until 2029, then 6% until 2035, then 2%")
- one-off actions: a market shock ("large cap −30% in March 2035"), a rebalance ("brokerage to 75% treasuries, 25% small cap in 2027"), a withdrawal or transfer
- hypothetical accounts and default balances
- the projection start date, globally or per asset, and `override_history` (see below)
- global assumptions such as inflation

A projection may build on one parent projection (a chain, never a merge of two). A modification whose target no longer exists, for example a change to a budget item later deleted from the base, is marked red and skipped, as with overlap.

### Querying the engine

A projection does no computing of its own. It asks the engine what its assets will hold, passing in every modification in effect:

    engine.balances(target, sample_times, modifications)

Reality is the same query with no modifications. The engine is a pure function of the record, the base budget, the modifications and the engine version, so the same inputs always give the same answer and results can be cached; the GUI and the CLI see identical numbers. A query takes a set of sample times (or a range and a resolution), so one simulation serves a whole graph. Extrapolation continues indefinitely; the caller decides how far to look.

A plotted line is an asset under a projection, or under reality. A report is an asset viewed over a time range.

### Projection start

A projection has a global **projection start date**. Each asset in it may be given its own start date, and its own `override_history` setting. The engine applies the rule to each account, using the start date and `override_history` resolved from the projection-wide values, over which the per-asset settings covering the account are applied in order (see Parameters and precedence):

    if override_history:
        start = min(last_actual_data_date, projection_start_date)
    else:
        start = last_actual_data_date

- Without `override_history`, all real data is read back, and each account starts projecting where its own data runs out. Checking synced through yesterday projects from tomorrow, while a hand-entered account last updated in March projects from March, under the same projection.
- With `override_history`, real data after the projection start is ignored: the "what if I had done this back then" case.
- An account with no actual data starts at the projection start date, reading back its defaults before it.

Before its start, an account's values are read back; from its start on, the engine extrapolates from the value at that point, applying the projection's modifications. Real data therefore beats modifications on the dates it covers, unless the start moves earlier.

The engine **aggregates as it projects**: every account is read back or projected on its own, and an asset's value is the sum of its accounts. A member whose data ends earlier than its siblings' is projected from its own end, never filled with defaults.

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

## Implementation notes

Details the build settled, and the placeholders it uses until the real rules arrive.

**Layout.** Model types and asset rules are in `internal/model`; the engine is in `internal/engine` and imports the model. The invented family-of-six dataset (`demodata.Family()`, reference date 2026-11-08) is in `internal/demodata`; the GUI still uses the older `demodata.Generate`.

**Dates and growth.** Dates have day resolution. Growth compounds annually over exactly days/365.25.

**Query.** `engine.Balances(record, projection name, target asset, sample times)` takes its modifications from the named projection and its parent chain; an empty name means reality. `Record.Today` stands in for the clock and is the default projection start.

**Sample status.** Each sample is actual, projected, partly projected (an aggregate with some members projected) or default (no underlying account has data).

**Projection start.** The projection-wide start (default `Record.Today`) and `override_history` (default false) are taken from the last projection in the chain that sets them; they are the base value, and the per-asset settings that cover the account are applied over it in order.

**Precedence.** Settings are applied in order, the last winning (`model.Apply`): asset-definition parameters first (`Resolution.AssetsAround`: the account's default asset, then inner before outer assets), then the projection chain root first, each projection's settings in list order. A schedule is written over the prior one on the dates its segments cover (`Schedule.Overlay`), so gaps and ends let the value beneath show through; start and `override_history` simply replace. A projection setting covers an account when it lies under the setting's target (resolved separately, so the target need not lie within the line's asset); an asset's own parameters cover only accounts it wraps within the line's scope. An `AssetSetting` naming an unknown asset is an error.

**Overlap.** The later-added member (by a record-wide sequence) is invalid as a whole, rule members included, is named with its duplicate and is left out of sums. It is checked on every resolve. A rule matching no accounts contributes 0; an empty rule matches every account. Default asset ids are `acct:<id>`.

**Actual data span.** From the earliest to the latest snapshot or transaction; an account needs at least one snapshot. Between snapshots the balance is the earlier snapshot plus transactions; before the first snapshot it is counted back from it.

**Breakdown.** `engine.Breakdown(record, projection, target, from, to)` covers (from, to]. Lines are split by cause and by actual (read back) versus projected. Causes: income, expense, transfer, contribution, withdrawal, tax, growth, unexplained (snapshot jumps not covered by transactions) and default. A `default` cause absorbs changes from default balances so the lines always total the balance change; residues under 1e-6 are dropped. Transfers with both sides inside the target cancel.

**Transfer classification.** Unsheltered to sheltered (tax-deferred, tax-free, 529) is a contribution, sheltered to unsheltered a withdrawal, anything else a transfer. A transfer to an account outside the target's scope counts as money leaving.

**Placeholder rules.** Tax is 25% of gross on income flows; a withdrawal from a tax-deferred account also charges 32% tax on top, to the source, with no penalty. On one date the engine applies income, expenses, then transfers (the full order under The engine is not yet modelled). Indexed budget items scale by the global inflation schedule compounded from `Record.Today` to the flow date; history is never indexed.

**Modifications supported.** A budget item's `amount` schedule, an account or asset `growth` schedule, global `inflation`, and `add_item` (adds a whole budget item). Unsupported properties and dangling targets are skipped and reported in the result's diagnostics.

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

**Setting editor (planned; no GUI for it yet):** the editor tabs will show each time-varying setting as a timeline strip of segment blocks. Touching segments share one draggable anchor and the earlier segment's end field is disabled, since its end is implied by the next start; dragging that end back sets an end and opens a gap, and a gap gives each edge its own handle. Dragging an edge into the next segment pushes that segment's start later; if that segment would shrink to nothing, it moves along with any later segments it meets, so nothing is deleted silently. The last segment's end handle sets or clears the setting's end. Clicking a segment selects it, and the Delete key removes it.

**Application shell and Graph tab:** a menu bar (File, Edit, View, Help) and tabs Graph, Projections, Assets, Accounts, Budget and Markets; only Graph is built. The Graph tab has a line list beside the plot, where each line is an asset under a projection. Unpinned lines follow the toolbar's projection switcher; a pinned line keeps its projection, and pinning is toggled from the row. Range buttons 1Y, 5Y, 10Y, 30Y and All set the visible span, and the sampling resolution follows the zoom. Lines are solid where the data is actual, faded where partly projected and dashed where projected, with a hollow marker at the handoff; a line whose last actual is more than 45 days before today is labelled stale. Data lanes sit under the plot. The crosshair shows value and date tags and a tooltip of values and statuses. The value tags sit outside the plot, in the y-axis gutter with their right edge against the plot's left edge, one per visible line with a sample on the hovered day, in the line's colour. Each is centred on its value's height, kept within the plot's vertical extent, and hidden when the value is outside the visible range. The tag shows the exact whole-dollar amount up to $9,999,999, and from $10,000,000 up a three-significant-digit short form with an M, B or T suffix ("$10.0M", "$124M", "$1.24B"; ">$999T" beyond), while the tooltip keeps the full amount. The date tag sits just below the plot in the x-axis area, centred on the cursor and kept within the plot's horizontal extent. The app draws the tags itself on the window draw list after the plot ends, not as ImPlot tags, so they never change axis sizing. The y-axis gutter has a fixed width, set by a hidden tick far below the visible range whose label is space-padded to be at least as wide as the widest value tag and the widest axis label at the current font and scale, so the plot area does not move with the cursor, panning or zooming. Series default to a colour by account type. There is no legend; the line list takes its place.

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
