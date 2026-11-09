# finplan design

This document describes finplan's data model, projection engine, analysis tools and agent access. It records decisions, not code: where the code and this document disagree, one of them is wrong and should be fixed. It describes capabilities only; a user's own accounts, household and numbers are data entered into the app, never written here or anywhere in the repository.

Status: agreed design. The data model, asset rules and a placeholder projection engine with breakdowns exist as standalone libraries (`internal/model`, `internal/engine`) that import only the standard library; the Graph and Projections tabs use them. Financial rules are placeholders (see Implementation notes); market classes, shocks and rebalances have types only.

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

The UI keeps a projection's modifications ordered and reorderable (drag a row). A modification is greyed, with a tooltip naming the later modification that overwrites it, when a later one in the chain (ancestors root first, then the projection's own, each in list order) has a schedule that covers every date of its schedule and, for an account or asset target, covers an account it covers (`Record.OverwrittenMods`, using `Resolve` for the accounts and `Schedule.Covers`); when that holds for only some of the accounts the tooltip says so (`Overwrite.Partial`). Budget item and global modifications are overwritten by a later one with the same target and property. Budget item additions are never marked. Marking an overwrite per account in the setting editor is not built.

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

A projection may build on one parent projection (a chain, never a merge of two). A modification whose target no longer exists, for example a change to a budget item later deleted from the base, is marked red and skipped, as with overlap (the red state is not built yet). The shared edit operations (`internal/ops`, used by the GUI and the CLI) keep this from arising through an edit: they refuse a move, removal, parent change or removal of an added item that would leave a modification that resolved before unresolved, and a new modification whose target does not resolve at its position; modifications already dangling do not block an edit.

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

**Layout.** Model types and asset rules are in `internal/model`; the engine is in `internal/engine` and imports the model. The invented family-of-six dataset (`demodata.Family()`, reference date 2026-11-08) is in `internal/demodata`; the older `demodata.Generate` is gone.

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

Highly dynamic graphs: pan, zoom and scroll horizontally with no fixed end date. Several projections and reality overlaid. Per-series visibility and color. Views saved as presets (stored in the SQLite database). Fast and lightweight.

**Setting editor (planned; no GUI for it yet):** the editor tabs will show each time-varying setting as a timeline strip of segment blocks. Touching segments share one draggable anchor and the earlier segment's end field is disabled, since its end is implied by the next start; dragging that end back sets an end and opens a gap, and a gap gives each edge its own handle. Dragging an edge into the next segment pushes that segment's start later; if that segment would shrink to nothing, it moves along with any later segments it meets, so nothing is deleted silently. The last segment's end handle sets or clears the setting's end. Clicking a segment selects it, and the Delete key removes it.

**Application shell and Graph tab:** a menu bar (File, Edit, View, Help) and tabs Graph, Projections, Assets, Accounts, Budget and Markets; Graph and Projections are built. The Graph tab has a line list beside the plot, where each line is an asset under a projection. Unpinned lines follow the toolbar's projection switcher; a pinned line keeps its projection, and pinning is toggled from the row. Range buttons 1Y, 5Y, 10Y, 30Y and All set the visible span, and the sampling resolution follows the zoom. Lines are solid where the data is actual, faded where partly projected and dashed where projected, with a hollow marker at the handoff; a line whose last actual is more than 45 days before today is labelled stale. Each row has an S (solo) button between the visibility checkbox and the colour swatch, faint while the row is hovered and lit while soloed. While any line is soloed, only soloed lines are drawn (soloing more adds them), even if a soloed line's checkbox is off; with none soloed the checkboxes decide. Solo never changes the checkboxes, so clearing the last solo restores the previously visible lines, and removing a line drops its solo. While any solo is active a "Solo: N · Clear" chip at the top of the line list clears all solos, as does Ctrl-click on any S button. The plot, crosshair tags, tooltip, data lanes and y-range fit all follow the drawn lines. Data lanes sit under the plot, one per drawn line on the plot's time axis: actual spans are a strong solid fill in the line colour, partly projected spans a medium solid fill, projected spans diagonal stripes in the line colour, and stretches with no data are empty. A lane highlights subtly on hover (no tooltip); clicking it selects its line, the same as clicking the line on the plot or its row. The selected line's lane gets the same background as its selected row and a full-strength label; unselected labels are dimmer. The crosshair shows value and date tags and a tooltip of values and statuses. The value tags sit outside the plot, in the y-axis gutter with their right edge against the plot's left edge, one per drawn line with a sample on the hovered day, in the line's colour. Each is centred on its value's height, kept within the plot's vertical extent, and hidden when the value is outside the visible range. The tag shows the exact whole-dollar amount up to $9,999,999, and from $10,000,000 up a three-significant-digit short form with an M, B or T suffix ("$10.0M", "$124M", "$1.24B"; ">$999T" beyond), while the tooltip keeps the full amount. The date tag sits just below the plot in the x-axis area, centred on the cursor and kept within the plot's horizontal extent. The app draws the tags itself on the window draw list after the plot ends, not as ImPlot tags, so they never change axis sizing. The y-axis gutter has a fixed width, set by a hidden tick far below the visible range whose label is space-padded to be at least as wide as the widest value tag and the widest axis label at the current font and scale, so the plot area does not move with the cursor, panning or zooming. Series default to a colour by account type. There is no legend; the line list takes its place.

**Projections tab:** a tree of projections on the left, Reality first (not editable) and each projection under the one it builds on, with a search box (a projection shows if it or one that builds on it matches, ignoring case) and the buttons New, Clone, Rename and Delete. Delete asks for confirmation and is refused while another projection builds on the projection; undo brings it back. The right side shows the selected projection's header: Name, Builds on (a dropdown that does not offer the projection itself or anything that builds on it; a cycle is refused), Start date (empty inherits), Override history (inherit, true or false), Inflation (a percentage, shown when the projection has none or one constant inflation modification, otherwise read-only) and Show on graph, which pins a line under the projection for each asset the Graph currently draws and switches to the Graph tab. Below it the modifications table lists, in the order they apply, the row number (drag it to reorder), From, Object, Property, Value and a remove button; the Add modification form appends one. A modification with a single constant value can have its value and its start edited in place; one with several segments, or a budget item addition, is read-only until the timeline strip editor exists. A refused typed value (an unparsable date or number, an empty or used name) stays in its field with its own "Not changed: <reason>" message, beside a header field and below the value inside a modification cell; each field's message is independent and goes away when the field's text is restored or abandoned (for example with Escape), or when the selection or record changes. A refused cycle or similar has its message at the top of the right side.

**Editing and undo:** every edit on the Projections tab goes through `internal/ops` (see "Shared edit operations" below) by way of one GUI path, `app.edit`. `ops` validates the edit, applies it to a copy of the record's editable documents and saves the copy; only if that succeeds does `app.edit` make the new record the live one, rebuild the Graph's data and push one undo step; the Graph keeps its lines, preset and range. A change that changes nothing (`ops.ErrNoChange`) makes no step, and a failure shows a notification and leaves everything as it was. Edit > Undo and Edit > Redo (labelled with the step, for example "Undo rename projection") and Ctrl+Z, Ctrl+Shift+Z and Ctrl+Y save through `ops.RestoreProjections`, move the history only after the save succeeds, and restore the Graph's projection switcher as it was in the step; the shortcuts are ignored while a text field has focus. Undo history is cleared by any reload of the record, including one caused by another program changing the database. The GUI's edit path keeps only window concerns: the undo history, rebuilding the Graph's data, the switcher memory, and renaming the in-memory Graph lines and loaded presets. Renaming a projection also updates its children's parent (in `ops`), the Graph's lines that name it (pinned lines and the switcher) and the saved view presets; `ops` rewrites the presets after the record is saved, and a failed write shows a notification. Deleting a projection leaves the Graph lines pinned to it in place; they draw nothing until the delete is undone. Not built yet: the timeline strip editor, per-asset start settings, shocks and rebalances, and the Assets, Accounts, Budget and Markets tabs.

**Shared edit operations:** `internal/ops` is the single way editable data changes: projections, accounts, assets and the base budget, and record entries by way of corrections. The GUI calls it, and the CLI's write commands call it too; it does not depend on the GUI, so an edit is validated, applied and saved the same way whoever makes it. Every operation takes the loaded record and the store, validates its input, applies the change to a copy, saves the copy in one transaction and returns a `Result` with the new record and the changed object: the projection, account, asset or budget item (and, for a rename, the rename, the presets rewritten and any preset warnings). The record passed in is never changed. On any failure nothing is saved and the error says why; a failed save is an `ops.SaveError`. Every operation takes `Options`; with `DryRun` it validates and applies to a copy and returns the same result without saving the record or writing presets. An edit that changes nothing returns `ErrNoChange` and saves nothing.

The operations: new, clone, rename and delete projection; set parent, start, override_history and inflation (nil removes it); add a modification; set a modification's values (its value and start); move and remove a modification; and restore a projection list (used by undo and redo). The rules `ops` enforces: names are trimmed and must be non-empty and unused; an unknown projection or parent is refused; delete is refused while another projection builds on it; a parent change that makes a cycle is refused; a modification on anything but a global needs an object; values must be finite and at most 1e15 in magnitude; modification indexes must be in range. Rename rewrites the saved view presets that name the projection after the record is saved, each in its own write; presets that cannot be decoded are skipped, and a failed preset write is returned as a warning while the record save stands. Projection operations edit `Record.EditableCopy`; account, asset and budget operations edit a deep copy (`Record.Clone`, `editRecord`) and return `ErrNoChange` if accounts, assets and budget are unchanged. Asset, account, budget and entry operations (`internal/ops/assets.go`, `accounts.go`, `budget.go`, `entries.go`, `items.go`) add: new, rename and delete asset; add and remove member; add and remove default interval; set or clear an asset's growth setting; new and edit account; add snapshot and transaction; correct and void entry; add, edit and end a base budget item. Rules: overlapping members are never refused (`Resolve` lists the later one as invalid); delete asset is `ErrInUse` while another asset wraps it or a projection names it in an asset setting or a modification target; an account's default asset (`acct:<id>`) cannot be renamed, deleted or have members changed; removing a member or asset renumbers all member seqs 1..N in order (`Record.renumber`); on a default asset the default interval operations write `Account.Default` and set-param writes the asset's params; a stated rate must be within -1..1; a transfer needs an existing counter account other than the account; a snapshot on a date that already has one is refused in favour of `CorrectEntry`; `EndBudgetItem` refuses an end before the start; semimonthly items cannot be created but existing ones can be edited and ended (`checkItemAs`). Entries are never edited in place: correct and void call the store's `Correct` and `Void`, and their dry run uses `TryCorrect` and `TryVoid`, which run the same checks in a rolled-back transaction.

**View presets:** a "View preset" section at the top of the sidebar has a dropdown, Save and Save as…. Save overwrites the current preset and is disabled when none is current; Save as… names a new preset and is disabled for an empty or duplicate name. Picking a preset restores it, discarding unsaved changes without a prompt. At startup no preset is selected and the default view is shown. A preset is plain data (`graph.Preset`, with `PresetLine` and `PresetBand`) capturing each line's asset, whether it is pinned or follows the switcher and its projection, colour, visibility and solo; the switcher value; the bands (lines referenced by index, colour, hatch, today's dollars, visible); and the x range. It does not capture selection or hover. Presets are stored durably: Save and Save as… write to the store (`PutPreset`, in `graph.PresetFormat`), and the saved presets are read back at startup. A reload after another process changes the database keeps the lines, x range, selection and bands, and the current preset if its name still exists. With no accounts in the record the Graph tab shows an empty-state hint instead. Two invented presets built from `demodata.Family()` are seeded into the `--demo` database: "Retirement overview", and "Early withdrawal cost" (the 401k pinned under Base and under Withdraw $100k, with a hatched band between them).

**Difference bands:** a shaded area between two lines, for example a 401k under a base projection and under one with an early withdrawal, showing the growing gap of tax, penalty and lost growth. The sidebar has a "Bands" section below the line list, where "+ Add" opens a popup choosing an upper and a lower line from the existing lines (any assets and projections). The colour defaults to the upper line's and follows it until the user edits the colour; Add is disabled when the two lines are the same or either is unset. Each band row has a visibility checkbox, a colour swatch, a name such as "401k: Base − Withdraw $100k" (or "Asset (Upper) − Asset (Lower)" when the assets differ), a Hatch toggle, a "Today's $" toggle and Remove. A band fills the area between its two lines, translucent (alpha about 0.25) and beneath the lines. Hatching is optional: 45° stripes in the band colour (alpha about 0.6), drawn with the plot draw list and clipped to the band and to the visible plot area, since ImPlot has no hatch patterns. A band draws only while it is visible and both its lines are drawn, so line visibility and solo apply, and it follows its lines' effective projections, so the switcher applies to unpinned lines. With the crosshair over the plot, the tooltip lists each drawn band's gap (upper − lower) at the cursor date, omitting the band if either line is out of range. When the band's Today's $ toggle is on, the tooltip adds the gap converted to today's dollars using the record's inflation schedule (`Globals[model.PropInflation]`) compounded from `Record.Today`; dates before today are inflated forward. Removing a line removes the bands that use it.

The GUI picks up changes made outside it (by the CLI) without a restart. The store runs SQLite in WAL mode; the GUI polls `PRAGMA data_version` through `Store.DataVersion()` once a second and reloads the record and presets only when it changes.

## Storage and privacy

All app data lives in SQLite outside the repository (the app uses `$XDG_DATA_HOME/finplan/finplan.db` when `XDG_DATA_HOME` is absolute, else `~/.local/share/finplan/finplan.db`; `--demo` uses a temporary seeded database instead). File → Backup... writes a consistent copy with `Store.Backup`. Credentials live in the OS keyring, never in files. The repository is public: real financial data never enters it, and tests and fixtures use invented numbers, such as an invented budget for a family of six.

### The storage layer

`internal/store` is a headless package, built on the pure-Go driver `modernc.org/sqlite`, that saves and loads a whole `model.Record`. `store.Open(path)` takes a path, creates or migrates the database, sets WAL mode, foreign keys and a busy timeout, and fails if WAL did not take. The store has one connection and is safe for concurrent use. `Save(record)` and `Load()` are the main calls; each runs in one transaction.

**Record versus documents.** The record proper, balance snapshots and transactions (with their kind, counter and memo), is append-only. Everything else is a freely editable document: account attributes, assets, the budget, projections, market classes, globals. Real tables hold the record so that it can be queried by account and date and so that the database itself enforces append-only. Versioned JSON holds the documents because they are nested, freely edited and versioned per document.

**Schema.** The schema version is `PRAGMA user_version`. Migrations are a numbered, forward-only list applied in one transaction on `Open`; a released migration is never edited. A database newer than the code fails with `ErrFutureSchema` and is left untouched. Migration 1 creates:

- `entries`: one row per snapshot or transaction, indexed by (account, date).
- `corrections`: the full replacement values, a note and the time made, referencing an entry.
- triggers that reject UPDATE and DELETE on both tables.
- `documents`: kind, key, ordinal, format version and JSON body.

Migration 2 adds `presets`, kept apart because `Save` replaces the documents wholesale. Migration 3 adds `corrections.void`.

**Saving the record.** `Save` replaces every document and appends the model's entries that are not yet stored. It matches the stored entries of each account (with corrections applied) against the model's as a multiset of values. If a stored entry was changed or removed in the model, or an account that has stored entries is gone, `Save` fails with `ErrRecordChanged` and writes nothing. Changes to stored entries go through corrections.

**Corrections.** `Entries(account)` lists an account's entries with their ids and current values. `Correct(id, values, note)` appends a correction, stamped with the time made, that holds the full replacement values; the original and earlier corrections stay. `Correct` rejects an unknown id (`ErrNotFound`), a snapshot correction that carries a transaction kind, counter or memo or would give the account two snapshots on one date, and a transfer with no counter account. `History(id)` returns the original and then each correction in order. `Load` applies the latest correction to each entry. `Void(id, note)` records a void correction: voided entries are left out of `Entries` and `Load` but stay in `History`, and a later correction restores the entry.

**Stale records.** `Load` records a marker, the highest `corrections.id`. `Save` returns `ErrRecordChanged` if the model still holds a value of an entry voided or corrected (any correction with an id above the marker) since that `Load`. A genuinely new entry equal to an entry voided before the last `Load` saves normally.

**Change polling and backup.** `DataVersion()` returns SQLite's `data_version`, which changes when another connection commits, for polling. `Backup(path)` runs `VACUUM INTO` a temporary file in the target folder and renames it into place, so a failure leaves an existing file untouched; it refuses the store's own database file.

**Versioning and loading.** Each document row carries its own format version, and decoding dispatches on it; a later format adds a case that reads the old shape and converts it. A row with an unknown format fails with `ErrUnknownFormat` rather than being dropped. The stored shape is a set of DTOs separate from the model types, so renaming a model field does not change what is on disk. `Load` rebuilds the record through the model constructors, replaying members in their original `Seq` order, so the loaded record equals the saved one; an empty database loads as an empty record.

**View presets.** `PutPreset`, `Preset`, `Presets` and `DeletePreset` store opaque bytes under a name with a format version owned by the writer. The store does not interpret them and does not import `internal/graph`. The app stores them as JSON in `PresetFormat` 1 (`graph.EncodePreset`, `graph.DecodePreset`; an unknown format is an error). `Restore` skips lines the record no longer has and the bands that use them, and falls back to another switcher if the saved projection is gone.

**Not done yet.** There is no delete correction (entries can be voided); and holdings, life events and a manual-versus-synced distinction have no storage because the model has no types for them yet.

## CLI and agent access

The `finplan` binary doubles as a CLI: with no subcommand (`finplan`, `finplan --demo`) it opens the GUI, and a non-flag argument routes to the CLI (`main`, `isCLI`), which does one job through the shared library and exits. Every command prints one JSON document on stdout; errors are `{"error":{"code","message"}}` on stdout, with exit 2 for usage errors and 1 for all others. The codes are `usage`, `not_found`, `ambiguous`, `no_database`, `old_schema`, `future_schema`, `database` and `engine`; the write commands add `unknown_target`, `name_taken`, `has_children`, `cycle`, `invalid`, `in_use` and `record_changed`.

Commands are data: `cmd/finplan/cliread.go` holds the read command table and `cmd/finplan/cliwrite.go` the projection write commands and `cmd/finplan/cliwritedata.go` (`dataCommands`) those for assets, accounts, entries and the base budget (name, summary, arguments, flags, output shape, whether it writes, its JSON input), and both the parser and `finplan help` / `finplan help --json` are generated from it, so the reference cannot drift from the code.

**Global flags.** `--demo` (a throwaway seeded database, removed on exit) and `--db <path>` work on every command, before or after it; using both is a usage error. With neither, the real database is used (`$XDG_DATA_HOME/finplan/finplan.db`, else `~/.local/share/finplan/finplan.db`).

**Read commands never write.** They open the database with `store.OpenReadOnly`, which never creates, migrates or writes it. A missing file is `no_database` and is not created; a schema older than the code is `old_schema` (the message asks you to open the database with the app first, which migrates it); a newer schema is `future_schema` (`store.ErrOldSchema`, `store.ErrFutureSchema`).

**Output conventions.** Dates are `YYYY-MM-DD`, `null` when absent or unbounded. Statuses are lowercase: `default`, `actual`, `projected`, `partly_projected`. Reality appears as `"projection": null`.

**Commands.** Commands about data that belongs to one kind of object are noun-verb; the engine queries are top-level verbs because they accept either an asset or an account.

    finplan account list                  # [{id, name, type, tax, owner, stated_rate, default}]
    finplan account entries <account>     # [{id, kind, date, amount, tx_kind, counter, memo}], voided entries left out
    finplan entry history <entry-id>      # {id, revisions: [{date, amount, tx_kind, counter, memo, made_at, note, void}]}
    finplan asset list                    # [{id, name, members, invalid, error, default, params}]
    finplan budget list                   # [{id, name, kind, amount, recurrence, start, end, indexed, account, from, to}]
    finplan projection list               # [{name, parent, start, override_history, modification_count}]
    finplan projection show <name>        # settings, asset_settings and modifications in the order they apply
    finplan balance <target> --from 2026-01 --to 2060-12 [--step day|week|month|year] [--projection P]
                                          # {target, projection, from, to, step, samples: [{date, value, status}], diagnostics}
    finplan breakdown <target> --from 2026-01 --to 2026-12 [--projection P]
                                          # {target, projection, from, to, start, end, total, lines: [{cause, actual, projected}], diagnostics}
    finplan starts [--projection P] [<target>]
                                          # [{account, start, last_actual, has_actual, override_history, projection_start}]
    finplan help [--json]

`--step` defaults to `month`. `diagnostics` is `{skipped: [...], overlaps: [...]}`: modifications the engine skipped and invalid asset members.

**Targets.** A `<target>` is accepted by id or display name; ids win over names. An account and its default asset count as one object. A name that matches several objects is an `ambiguous` error (`findCandidates`, `resolveTarget`). `account entries` takes an account only.

**Dates.** Dates are `YYYY-MM-DD` or `YYYY-MM`. For `--from`, `YYYY-MM` is the first of the month; for `--to`, the last day. Month and year steps keep the start's day of month, clamped to the month's end, and are computed from `--from` each time so a clamped day does not carry forward (`parseCLIDate`, `sampleDates`).

**Write commands.** `cmd/finplan/cliwrite.go` (projections) and `cliwritedata.go` (the rest) define them; each makes its edit through `internal/ops`, the same operations the GUI uses, so both agree on what an edit may do.

    finplan projection new <name> [--parent P]
    finplan projection clone <name> --as <new-name>
    finplan projection rename <name> <new-name>
    finplan projection set-parent <name> [<parent>]
    finplan projection delete <name>
    finplan projection set-start <name> <date|none>
    finplan projection set-override-history <name> <true|false|none>
    finplan projection set-inflation <name> <rate|none>
    finplan mod add <projection> --target item:<x>|account:<x>|asset:<x>|global --property amount|growth|inflation [--value N [--from D]] [--at I]
    finplan mod edit <projection> <index> [--value N [--from D]]
    finplan mod move <projection> <from> <to>
    finplan mod remove <projection> <index>
    finplan item add <projection> [--kind --name --amount --every --on --day --end --account --from --to --indexed --id]
    finplan item edit <projection> <item> [item flags]
    finplan item end <projection> <item> <date>
    finplan asset new <name> [--id]
    finplan asset rename <asset> <new-name>
    finplan asset delete <asset>
    finplan asset add-member <asset> (--account A | --asset A | --rule [--types T] [--tax T])
    finplan asset remove-member <asset> <seq>
    finplan asset add-default <asset> --value N [--from D] [--to D]
    finplan asset remove-default <asset> <index>        # index from 0 in asset list's default
    finplan asset set-param <asset> [--param growth] (--value N [--from D] | JSON schedule | --clear)
    finplan account new <name> --type T --tax T [--owner] [--rate] [--id]
    finplan account edit <account> [--name --type --tax --owner --rate]   # a rename also renames the default asset
    finplan entry add-snapshot <account> --date D --balance N
    finplan entry add-transaction <account> --date D --amount N --kind K [--counter A] [--memo]
    finplan entry correct <entry-id> [--date --amount --kind --counter --memo] [--note]
    finplan entry void <entry-id> [--note]
    finplan budget add [item flags]
    finplan budget edit <item> [item flags]
    finplan budget end <item> <date>

**Database.** A write command opens the database with `store.Open` (`openWrite`): it migrates an older database but never creates a missing one (`no_database`); `--demo` and `--db` work as for reads. Read commands still never write.

**Dry run and output.** Every write command takes `--dry-run`: the edit is validated and applied to a copy, and nothing is saved (`ops.Options`, `commit`). The output of the projection commands is `{dry_run, changed, projection}`, the projection in the shape of `projection show` after the edit; the other write commands print `{dry_run, changed, <object>}` with `asset`, `account`, `entry` or `item` built by the same functions as the read commands (`assetJSON`, `accountJSON`, `entryJSON`, `budgetItemJSON`); `asset delete` adds `removed`, `entry void` adds `voided`, and an entry added in a dry run has id null. An edit that changes nothing (`ops.ErrNoChange`) prints `changed: false` and exits 0. `projection rename` adds `renamed {from, to}`, `rewritten_presets` and `warnings` (presets that could not be rewritten; the rename is saved regardless); `projection delete` prints `removed` and the projection as it was.

**Errors.** `opsError` maps the operations' errors to codes: `not_found` (unknown projection, asset, account or entry), `in_use` (an asset still used), `record_changed` (the stored record changed since it was loaded), `unknown_target`, `name_taken`, `has_children`, `cycle`, `invalid` (out-of-range values, a missing object, an empty name) and `database` (the save failed). Usage errors exit 2, all others 1.

**Input.** Rates are fractions (`0.03` for 3%); a negative rate argument needs `--` before it. A modification's values are a constant (`--value`, optionally `--from`) or a schedule as JSON on stdin or `--json-file`, `[{"from","end","value"}]`; a budget item is given by flags or as JSON in the shape `budget list` prints. JSON is decoded strictly (unknown fields and trailing data are usage errors).

**Budget items.** `item add` stores a new item as an `add_item` modification (`ops.AddItem`, `checkItem`). Kinds are income and expense (which need an account) and transfer (from and to; at least one an account of the record). Recurrence is one-time, monthly (day), biweekly or annual (month and day, checked against the month), with an optional end; one-time and biweekly need a date; semimonthly is not accepted for added items. An id not given is a unique slug of the name. `item edit` (`ops.EditItem`) replaces an item the projection itself adds and keeps its id. `item end` (`ops.EndItem`) appends an amount modification of 0 from the date, for a record item or one added in the chain.

**Modification targets.** The operations check that the property suits the target (amount for a budget item, growth for an account or asset, inflation for global) and that the target resolves at the modification's position (`checkTarget`): a record budget item, an item added by an ancestor's `add_item` modification, or one added by an earlier row of the same projection; otherwise `unknown_target`. `mod add` cannot add an item. Every edit then checks (`checkNoNewDangling`) that no modification which resolved before stops resolving, which refuses moves, removals, parent changes and removals of added items that would orphan a modification; modifications already dangling do not block an edit, including rename and clone.

Record writes are the `entry` commands: snapshots and transactions are appended, and changes to them go through corrections (`entry correct`, `entry void`).

The agent (Claude) has full access: it reads the record, assets, budgets, projections and engine output, and writes through the write commands: projections (settings, modifications and added budget items), assets, accounts, the base budget and record entries. Changes to recorded entries go through corrections, like the GUI's. Whatever the agent reads is sent to its model provider.

A thin agent skill lives in this repository at `.claude/skills/finplan/SKILL.md`: what finplan is, the rules (use the CLI, never touch the database; record changes are corrections), and an instruction to run `finplan help --json`, which the binary generates so the command reference cannot drift from the code. Symlinking the skill into `~/.claude/skills/finplan` makes it available in any terminal.
