// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/store"
)

// The write commands for assets, accounts, the record and the base budget. They print {dry_run, changed, <the changed object>}, the object in the shape the read command for it prints. An edit that would change nothing prints changed false and the object as it is.

const (
	assetOut   = "{dry_run, changed, asset: as asset list prints it, after the edit}"
	accountOut = "{dry_run, changed, account: as account list prints it, after the edit}"
	entryOut   = "{dry_run, changed, entry: as account entries prints it}; id is null for an entry added in a dry run"
	itemOut    = "{dry_run, changed, item: as budget list prints it, after the edit}"
	assetNote  = "asset id or name, or an account id or name for the account's default asset"
	entryNote  = "entry id, as listed by account entries"
)

var accountTypes = func() []string {
	var out []string
	for _, t := range model.AccountTypes {
		out = append(out, string(t))
	}
	return out
}()

var taxTreatments = func() []string {
	var out []string
	for _, t := range model.TaxTreatments {
		out = append(out, string(t))
	}
	return out
}()

var dataCommands = []*command{
	{
		Name: "asset new", Summary: "add an aggregate asset with no members; add them with asset add-member",
		Args:   []argDef{{Name: "name", Description: "name of the asset", Required: true}},
		Flags:  []flagDef{{Name: "id", Type: typeString, Description: "the asset's id; made from the name when omitted. It must differ from every account and asset id, and cannot start with acct:"}},
		Output: assetOut,
		run:    cmdAssetNew,
	},
	{
		Name: "asset rename", Summary: "rename an aggregate asset; an account's default asset is renamed with account edit",
		Args:   []argDef{{Name: "asset", Description: "asset id or name", Required: true}, {Name: "new-name", Description: "its new name", Required: true}},
		Output: assetOut,
		run:    cmdAssetRename,
	},
	{
		Name: "asset delete", Summary: "delete an aggregate asset; refused while another asset has it as a member or a projection has a setting or modification for it or for an account under it",
		Args:   []argDef{{Name: "asset", Description: "asset id or name", Required: true}},
		Output: "{dry_run, changed, removed: asset id, asset: the asset as it was, as asset list prints it}; its members are removed with it, the accounts they name are not",
		run:    cmdAssetDelete,
	},
	{
		Name: "asset add-member", Summary: "add an account, an asset or a rule to an aggregate asset, after its other members; a member that overlaps an earlier one is added and shown as invalid, not refused",
		Args: []argDef{{Name: "asset", Description: "asset id or name", Required: true}},
		Flags: []flagDef{
			{Name: "account", Type: typeString, Description: "account id or name to add"},
			{Name: "asset", Type: typeString, Description: "asset id or name to add; a cycle is refused"},
			{Name: "rule", Type: typeBool, Default: "false", Description: "add a rule that takes the accounts matching --types and --tax; with neither it takes every account"},
			{Name: "types", Type: typeString, Description: "rule: comma-separated account types, one of " + strings.Join(accountTypes, ", ")},
			{Name: "tax", Type: typeString, Description: "rule: comma-separated tax treatments, one of " + strings.Join(taxTreatments, ", ")},
		},
		Output: assetOut,
		run:    cmdAssetAddMember,
	},
	{
		Name: "asset remove-member", Summary: "remove a member from an aggregate asset; the members of the record are renumbered afterwards, so seq numbers can shift",
		Args: []argDef{
			{Name: "asset", Description: "asset id or name", Required: true},
			{Name: "seq", Description: "the member's seq, as asset list prints it", Required: true},
		},
		Output: assetOut,
		run:    cmdAssetRemoveMember,
	},
	{
		Name: "asset add-default", Summary: "add a default balance interval to an asset or account: the value used on dates with no actual data. Where intervals overlap, the last added wins. For an account's default asset it is the account's interval",
		Args: []argDef{{Name: "asset", Description: assetNote, Required: true}},
		Flags: []flagDef{
			{Name: "value", Type: typeNumber, Required: true, Description: "the default balance"},
			{Name: "from", Type: typeDate, Description: "first date of the interval: " + dateNote + "; unbounded when omitted"},
			{Name: "to", Type: typeDate, EndOfMonth: true, Description: "last date of the interval, inclusive: " + endNote + "; unbounded when omitted"},
		},
		Output: assetOut,
		run:    cmdAssetAddDefault,
	},
	{
		Name: "asset remove-default", Summary: "remove a default balance interval",
		Args: []argDef{
			{Name: "asset", Description: assetNote, Required: true},
			{Name: "index", Description: "the interval's index from 0, in the order the default list of asset list shows", Required: true},
		},
		Output: assetOut,
		run:    cmdAssetRemoveDefault,
	},
	{
		Name: "asset set-param", Summary: "set a parameter of everything an asset wraps, replacing the earlier setting, or clear it",
		Args: []argDef{{Name: "asset", Description: assetNote, Required: true}},
		Flags: []flagDef{
			{Name: "param", Type: typeEnum, Default: model.PropGrowth, Choices: []string{model.PropGrowth}, Description: "the parameter; only growth, a fraction per year"},
			{Name: "value", Type: typeNumber, Description: "a constant value (0.04 for 4%); without it, and without --clear, the schedule is read as JSON"},
			{Name: "from", Type: typeDate, Description: "with --value, the date the value starts: " + dateNote + "; always when omitted"},
			{Name: "clear", Type: typeBool, Default: "false", Description: "remove the setting"},
		},
		Input:  "without --value and --clear, a schedule: " + scheduleNote,
		Output: assetOut,
		run:    cmdAssetSetParam,
	},
	{
		Name: "account new", Summary: "add an account with no record entries, and its default asset",
		Args: []argDef{{Name: "name", Description: "name of the account", Required: true}},
		Flags: []flagDef{
			{Name: "type", Type: typeEnum, Required: true, Choices: accountTypes, Description: "account type"},
			{Name: "tax", Type: typeEnum, Required: true, Choices: taxTreatments, Description: "tax treatment"},
			{Name: "owner", Type: typeString, Description: "who owns the account"},
			{Name: "rate", Type: typeNumber, Description: "stated rate, the default growth when the account is projected: a fraction per year between -1 and 1; 0 when omitted"},
			{Name: "id", Type: typeString, Description: "the account's id; made from the name when omitted. It must differ from every account and asset id"},
		},
		Output: accountOut + "; the default asset follows as asset list prints it in asset",
		run:    cmdAccountNew,
	},
	{
		Name: "account edit", Summary: "change attributes of an account; flags given override its current values; the default asset is renamed with it",
		Args: []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Flags: []flagDef{
			{Name: "name", Type: typeString, Description: "the account's name"},
			{Name: "type", Type: typeEnum, Choices: accountTypes, Description: "account type"},
			{Name: "tax", Type: typeEnum, Choices: taxTreatments, Description: "tax treatment"},
			{Name: "owner", Type: typeString, Description: "who owns the account; empty clears it"},
			{Name: "rate", Type: typeNumber, Description: "stated rate: a fraction per year between -1 and 1"},
		},
		Output: accountOut,
		run:    cmdAccountEdit,
	},
	{
		Name: "account close", Summary: "close an account: it keeps its history, is left out of pickers for new lines and objects, and reads 0 after its close date; refused for an account already closed or with record entries after the date",
		Args:   []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Flags:  []flagDef{{Name: "date", Type: typeDate, Description: "the last day the account has a balance: " + dateNote + "; the record's today when omitted"}},
		Output: accountOut,
		run:    cmdAccountClose,
	},
	{
		Name: "account reopen", Summary: "reopen a closed account; changes nothing for an account that is open",
		Args:   []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Output: accountOut,
		run:    cmdAccountReopen,
	},
	{
		Name: "account delete", Summary: "delete an account and its default asset; refused while it has record entries (close it instead) or a budget item, asset member, modification or asset setting refers to it",
		Args:   []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Output: "{dry_run, changed, removed: account id, account: the account as it was, as account list prints it}",
		run:    cmdAccountDelete,
	},
	{
		Name: "entry add-snapshot", Summary: "record the balance of an account at the end of a day; refused when the account has a snapshot on that date, which entry correct changes",
		Args: []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Flags: []flagDef{
			{Name: "date", Type: typeDate, Required: true, Description: "the day: " + dateNote},
			{Name: "balance", Type: typeNumber, Required: true, Description: "the balance, negative for a debt"},
		},
		Output: entryOut,
		run:    cmdEntryAddSnapshot,
	},
	{
		Name: "entry add-transaction", Summary: "record a transaction of an account; the amount is signed from the account's view",
		Args: []argDef{{Name: "account", Description: "account id or name", Required: true}},
		Flags: []flagDef{
			{Name: "date", Type: typeDate, Required: true, Description: "the day: " + dateNote},
			{Name: "amount", Type: typeNumber, Required: true, Description: "the amount, negative when money leaves the account"},
			{Name: "kind", Type: typeEnum, Required: true, Choices: []string{string(model.TxIncome), string(model.TxExpense), string(model.TxTransfer)}, Description: "income, expense or transfer"},
			{Name: "counter", Type: typeString, Description: "a transfer's account on the other side: account id or name"},
			{Name: "memo", Type: typeString, Description: "a note on the transaction"},
		},
		Output: entryOut,
		run:    cmdEntryAddTransaction,
	},
	{
		Name: "entry correct", Summary: "correct an entry: only the values given change, and the earlier values stay in entry history. Correcting a voided entry restores it",
		Args: []argDef{{Name: "entry-id", Description: entryNote, Required: true}},
		Flags: []flagDef{
			{Name: "date", Type: typeDate, Description: "the day: " + dateNote},
			{Name: "amount", Type: typeNumber, Description: "the amount, or the balance of a snapshot"},
			{Name: "kind", Type: typeEnum, Choices: []string{string(model.TxIncome), string(model.TxExpense), string(model.TxTransfer)}, Description: "a transaction's kind"},
			{Name: "counter", Type: typeString, Description: "a transfer's account on the other side: account id or name; empty clears it"},
			{Name: "memo", Type: typeString, Description: "a note on the transaction; empty clears it"},
			{Name: "note", Type: typeString, Description: "why the entry is corrected, kept in entry history"},
		},
		Output: entryOut + ", with the values after the correction",
		run:    cmdEntryCorrect,
	},
	{
		Name: "entry void", Summary: "take an entry out of the record: account entries and balances leave it out, entry history keeps it",
		Args:   []argDef{{Name: "entry-id", Description: entryNote, Required: true}},
		Flags:  []flagDef{{Name: "note", Type: typeString, Description: "why the entry is voided, kept in entry history"}},
		Output: "{dry_run, changed, voided, entry: as account entries prints it, with the values it had}",
		run:    cmdEntryVoid,
	},
	{
		Name: "budget add", Summary: "add an item to the base budget, which every projection builds on",
		Flags:  itemFlags(true),
		Input:  "instead of the flags, " + itemJSONNote,
		Output: itemOut,
		run:    cmdBudgetAdd,
	},
	{
		Name: "budget edit", Summary: "change a base budget item; flags given override the item's current values",
		Args:   []argDef{{Name: "item", Description: "item id or name", Required: true}},
		Flags:  itemFlags(false),
		Input:  "instead of the flags, the whole new item: " + itemJSONNote + "; its id, if given, must be the item's",
		Output: itemOut,
		run:    cmdBudgetEdit,
	},
	{
		Name: "budget end", Summary: "make a date the last one a base budget item happens on",
		Args: []argDef{
			{Name: "item", Description: "item id or name", Required: true},
			{Name: "date", Description: "last date it happens: " + dateNote, Required: true},
		},
		Output: itemOut,
		run:    cmdBudgetEnd,
	},
	{
		Name: "budget delete", Summary: "delete a base budget item; a projection's modification of it is kept but points at nothing, so the engine skips it and projection show marks it missing",
		Args:   []argDef{{Name: "item", Description: "item id or name", Required: true}},
		Output: "{dry_run, changed, removed: item id, item: the item as it was, as budget list prints it}",
		run:    cmdBudgetDelete,
	},
}

// dataResult starts the output of a data write: {dry_run, changed}. The bool is false when the edit changed nothing. An error of the operation is the command's error.
func (c *call) dataResult(err error) (map[string]any, bool, error) {
	out := map[string]any{"dry_run": c.options().DryRun, "changed": true}
	switch {
	case errors.Is(err, ops.ErrNoChange):
		out["changed"] = false
		return out, false, nil
	case err != nil:
		return nil, false, opsError(err)
	}
	return out, true, nil
}

// assetResult is the output of an edit of the asset.
func (c *call) assetResult(res *ops.Result, err error, id model.AssetID) (any, error) {
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	rec, a := c.rec, c.rec.Asset(id)
	if changed {
		rec, a = res.Record, res.Asset
	}
	out["asset"] = assetJSON(rec, a)
	return out, nil
}

// accountResult is the output of an edit of the account.
func (c *call) accountResult(res *ops.Result, err error, id model.AccountID) (any, error) {
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	a := c.rec.Account(id)
	if changed {
		a = res.Account
	}
	out["account"] = accountJSON(a)
	return out, nil
}

// itemResult is the output of an edit of the base budget item.
func (c *call) itemResult(res *ops.Result, err error, id string) (any, error) {
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	var it model.BudgetItem
	if changed {
		it = *res.Item
	} else {
		it, _ = c.rec.Budget.Item(id)
	}
	out["item"] = budgetItemJSON(it)
	return out, nil
}

// accountArg returns the account the positional argument at pos names.
func (c *call) accountArg(pos int) (model.AccountID, error) {
	return c.accountNamed(c.args[pos])
}

func (c *call) accountNamed(s string) (model.AccountID, error) {
	if _, _, err := c.open(); err != nil {
		return "", err
	}
	cand, err := resolveTarget(c.rec, s, true)
	return cand.account, err
}

// assetNamed returns the asset s names; an account names its default asset.
func (c *call) assetNamed(s string) (model.AssetID, error) {
	if _, _, err := c.open(); err != nil {
		return "", err
	}
	cand, err := resolveTarget(c.rec, s, false)
	return cand.asset, err
}

// assetArg returns the asset the positional argument at pos names, opening the database.
func (c *call) assetArg(pos int) (model.AssetID, error) {
	return c.assetNamed(c.args[pos])
}

func cmdAssetNew(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	res, err := ops.NewAsset(st, rec, c.args[0], c.str("id"), c.options())
	id := model.AssetID("")
	if res != nil {
		id = res.Asset.ID
	}
	return c.assetResult(res, err, id)
}

func cmdAssetRename(c *call) (any, error) {
	id, err := c.assetArg(0)
	if err != nil {
		return nil, err
	}
	res, err := ops.RenameAsset(c.st, c.rec, id, c.args[1], c.options())
	return c.assetResult(res, err, id)
}

func cmdAssetDelete(c *call) (any, error) {
	id, err := c.assetArg(0)
	if err != nil {
		return nil, err
	}
	a := c.rec.Asset(id)
	res, err := ops.DeleteAsset(c.st, c.rec, id, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "removed": res.Removed, "asset": assetJSON(c.rec, a)}, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func cmdAssetAddMember(c *call) (any, error) {
	id, err := c.assetArg(0)
	if err != nil {
		return nil, err
	}
	isRule := c.str("rule") == "true" || c.set["types"] || c.set["tax"]
	n := 0
	for _, set := range []bool{c.set["account"], c.set["asset"], isRule} {
		if set {
			n++
		}
	}
	if n != 1 {
		return nil, usageErr("asset add-member needs exactly one of --account, --asset and --rule (or --types or --tax)")
	}
	var m model.Member
	switch {
	case c.set["account"]:
		acct, err := c.accountNamed(c.str("account"))
		if err != nil {
			return nil, err
		}
		m = model.AccountMember(acct)
	case c.set["asset"]:
		member, err := c.assetNamed(c.str("asset"))
		if err != nil {
			return nil, err
		}
		if _, isAcct := strings.CutPrefix(string(member), string(model.DefaultAssetID(""))); isAcct {
			return nil, usageErr("--asset %s: that is an account; use --account", c.str("asset"))
		}
		m = model.AssetMember(member)
	default:
		r := model.Rule{}
		for _, t := range splitList(c.str("types")) {
			r.Types = append(r.Types, model.AccountType(t))
		}
		for _, t := range splitList(c.str("tax")) {
			r.Tax = append(r.Tax, model.TaxTreatment(t))
		}
		m = model.RuleMember(r)
	}
	res, err := ops.AddMember(c.st, c.rec, id, m, c.options())
	return c.assetResult(res, err, id)
}

func cmdAssetRemoveMember(c *call) (any, error) {
	id, err := c.assetArg(0)
	if err != nil {
		return nil, err
	}
	seq, err := strconv.Atoi(c.args[1])
	if err != nil {
		return nil, usageErr("<seq>: %q is not a whole number", c.args[1])
	}
	res, err := ops.RemoveMember(c.st, c.rec, id, seq, c.options())
	return c.assetResult(res, err, id)
}

func cmdAssetAddDefault(c *call) (any, error) {
	id, err := c.assetArg(0)
	if err != nil {
		return nil, err
	}
	v, _, err := c.number("value")
	if err != nil {
		return nil, err
	}
	iv := model.Interval{From: model.MinDate, Value: v}
	if d, ok, err := c.date("from"); err != nil {
		return nil, err
	} else if ok {
		iv.From = d
	}
	if d, ok, err := c.date("to"); err != nil {
		return nil, err
	} else if ok {
		iv.To = &d
	}
	res, err := ops.AddDefault(c.st, c.rec, id, iv, c.options())
	return c.assetResult(res, err, id)
}

func cmdAssetRemoveDefault(c *call) (any, error) {
	id, err := c.assetArg(0)
	if err != nil {
		return nil, err
	}
	i, err := strconv.Atoi(c.args[1])
	if err != nil {
		return nil, usageErr("<index>: %q is not a whole number", c.args[1])
	}
	res, err := ops.RemoveDefault(c.st, c.rec, id, i, c.options())
	return c.assetResult(res, err, id)
}

func cmdAssetSetParam(c *call) (any, error) {
	id, err := c.assetArg(0)
	if err != nil {
		return nil, err
	}
	var values *model.Schedule
	if c.str("clear") == "true" {
		if c.set["value"] || c.set["from"] || c.set["json-file"] {
			return nil, usageErr("--clear cannot be combined with --value, --from or --json-file")
		}
	} else {
		s, err := c.schedule()
		if err != nil {
			return nil, err
		}
		values = &s
	}
	res, err := ops.SetParam(c.st, c.rec, id, c.str("param"), values, c.options())
	return c.assetResult(res, err, id)
}

func cmdAccountNew(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	rate, _, err := c.number("rate")
	if err != nil {
		return nil, err
	}
	acct := model.Account{
		ID: model.AccountID(c.str("id")), Name: c.args[0], Type: model.AccountType(c.str("type")),
		Tax: model.TaxTreatment(c.str("tax")), Owner: c.str("owner"), StatedRate: rate,
	}
	res, err := ops.NewAccount(st, rec, acct, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	return map[string]any{
		"dry_run": c.options().DryRun, "changed": true,
		"account": accountJSON(res.Account), "asset": assetJSON(res.Record, res.Asset),
	}, nil
}

func cmdAccountEdit(c *call) (any, error) {
	id, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	var ch ops.AccountChange
	if c.set["name"] {
		ch.Name = model.Ptr(c.str("name"))
	}
	if c.set["type"] {
		ch.Type = model.Ptr(model.AccountType(c.str("type")))
	}
	if c.set["tax"] {
		ch.Tax = model.Ptr(model.TaxTreatment(c.str("tax")))
	}
	if c.set["owner"] {
		ch.Owner = model.Ptr(c.str("owner"))
	}
	if v, ok, err := c.number("rate"); err != nil {
		return nil, err
	} else if ok {
		ch.StatedRate = &v
	}
	if ch == (ops.AccountChange{}) {
		return nil, usageErr("account edit needs at least one of --name, --type, --tax, --owner and --rate")
	}
	res, err := ops.EditAccount(c.st, c.rec, id, ch, c.options())
	return c.accountResult(res, err, id)
}

func cmdAccountClose(c *call) (any, error) {
	id, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	date, ok, err := c.date("date")
	if err != nil {
		return nil, err
	}
	if !ok {
		date = c.rec.Today
	}
	res, err := ops.CloseAccount(c.st, c.rec, id, date, c.options())
	return c.accountResult(res, err, id)
}

func cmdAccountReopen(c *call) (any, error) {
	id, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	res, err := ops.ReopenAccount(c.st, c.rec, id, c.options())
	return c.accountResult(res, err, id)
}

func cmdAccountDelete(c *call) (any, error) {
	id, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	res, err := ops.DeleteAccount(c.st, c.rec, id, c.options())
	if err != nil {
		return nil, opsError(err)
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "removed": res.Removed, "account": accountJSON(res.Account)}, nil
}

// entryResult is the output of an operation on an entry.
func (c *call) entryResult(res *ops.Result, err error, current func() (store.Entry, error)) (map[string]any, error) {
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	var e store.Entry
	if changed {
		e = *res.Entry
	} else if e, err = current(); err != nil {
		return nil, failErr("database", "%v", err)
	}
	out["entry"] = entryJSON(e)
	return out, nil
}

func cmdEntryAddSnapshot(c *call) (any, error) {
	acct, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	date, _, err := c.date("date")
	if err != nil {
		return nil, err
	}
	balance, _, err := c.number("balance")
	if err != nil {
		return nil, err
	}
	res, err := ops.AddSnapshot(c.st, c.rec, acct, date, balance, c.options())
	return c.entryResult(res, err, nil)
}

func cmdEntryAddTransaction(c *call) (any, error) {
	acct, err := c.accountArg(0)
	if err != nil {
		return nil, err
	}
	date, _, err := c.date("date")
	if err != nil {
		return nil, err
	}
	amount, _, err := c.number("amount")
	if err != nil {
		return nil, err
	}
	t := model.Transaction{Date: date, Amount: amount, Kind: model.TxKind(c.str("kind")), Memo: c.str("memo")}
	if c.set["counter"] {
		if t.Counter, err = c.accountNamed(c.str("counter")); err != nil {
			return nil, err
		}
	}
	res, err := ops.AddTransaction(c.st, c.rec, acct, t, c.options())
	return c.entryResult(res, err, nil)
}

// entryArg reads the entry id at pos and opens the database.
func (c *call) entryArg(pos int) (int64, error) {
	id, err := strconv.ParseInt(c.args[pos], 10, 64)
	if err != nil {
		return 0, usageErr("entry id %q is not a number", c.args[pos])
	}
	if _, _, err := c.open(); err != nil {
		return 0, err
	}
	return id, nil
}

func cmdEntryCorrect(c *call) (any, error) {
	id, err := c.entryArg(0)
	if err != nil {
		return nil, err
	}
	var ch ops.EntryChange
	if d, ok, err := c.date("date"); err != nil {
		return nil, err
	} else if ok {
		ch.Date = &d
	}
	if v, ok, err := c.number("amount"); err != nil {
		return nil, err
	} else if ok {
		ch.Amount = &v
	}
	if c.set["kind"] {
		ch.TxKind = model.Ptr(model.TxKind(c.str("kind")))
	}
	if c.set["counter"] {
		counter := model.AccountID("")
		if c.str("counter") != "" {
			if counter, err = c.accountNamed(c.str("counter")); err != nil {
				return nil, err
			}
		}
		ch.Counter = &counter
	}
	if c.set["memo"] {
		ch.Memo = model.Ptr(c.str("memo"))
	}
	if ch == (ops.EntryChange{}) {
		return nil, usageErr("entry correct needs at least one of --date, --amount, --kind, --counter and --memo")
	}
	res, err := ops.CorrectEntry(c.st, c.rec, id, ch, c.str("note"), c.options())
	return c.entryResult(res, err, func() (store.Entry, error) { return c.st.Entry(id) })
}

func cmdEntryVoid(c *call) (any, error) {
	id, err := c.entryArg(0)
	if err != nil {
		return nil, err
	}
	res, err := ops.VoidEntry(c.st, c.rec, id, c.str("note"), c.options())
	out, err := c.entryResult(res, err, func() (store.Entry, error) { return c.st.Entry(id) })
	if err != nil {
		return nil, err
	}
	out["voided"] = true
	return out, nil
}

func cmdBudgetAdd(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	it, err := c.itemInputFor(nil)
	if err != nil {
		return nil, err
	}
	res, err := ops.AddBudgetItem(st, rec, it, c.options())
	id := ""
	if res != nil {
		id = res.Item.ID
	}
	return c.itemResult(res, err, id)
}

// baseItemArg returns the base budget item the positional argument at pos names, by id or name.
func (c *call) baseItemArg(pos int) (model.BudgetItem, error) {
	if _, _, err := c.open(); err != nil {
		return model.BudgetItem{}, err
	}
	id, err := pickItem(c.rec.Budget.Items, c.args[pos])
	if err != nil {
		return model.BudgetItem{}, err
	}
	it, ok := c.rec.Budget.Item(id)
	if !ok {
		return it, failErr("not_found", "there is no budget item %q", c.args[pos])
	}
	return it, nil
}

func cmdBudgetEdit(c *call) (any, error) {
	cur, err := c.baseItemArg(0)
	if err != nil {
		return nil, err
	}
	it, err := c.itemInputFor(&cur)
	if err != nil {
		return nil, err
	}
	res, err := ops.EditBudgetItem(c.st, c.rec, cur.ID, it, c.options())
	return c.itemResult(res, err, cur.ID)
}

func cmdBudgetEnd(c *call) (any, error) {
	cur, err := c.baseItemArg(0)
	if err != nil {
		return nil, err
	}
	end, err := parseCLIDate(c.args[1], false)
	if err != nil {
		return nil, usageErr("<date>: %v", err)
	}
	res, err := ops.EndBudgetItem(c.st, c.rec, cur.ID, end, c.options())
	return c.itemResult(res, err, cur.ID)
}

func cmdBudgetDelete(c *call) (any, error) {
	cur, err := c.baseItemArg(0)
	if err != nil {
		return nil, err
	}
	res, err := ops.DeleteBudgetItem(c.st, c.rec, cur.ID, c.options())
	out, _, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	out["removed"] = res.Removed
	out["item"] = budgetItemJSON(*res.Item)
	return out, nil
}
