// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/ops"
	"github.com/joeriedel/finplan/internal/tax"
)

// The commands for the household and the tax tables: household show, household set, household member add|edit|remove, tax table show|set|revert|copy, tax compute and tax year.

const (
	householdOut = "{dry_run, changed, household: as household show prints it, after the edit}"
	memberOut    = "{dry_run, changed, member: {name, birth, role}, household: as household show prints it, after the edit}"
	taxTableNote = "a federal table is {year, status: {mfj|single|hoh|mfs: {brackets: [{over, rate}], standard_deduction, child_credit_phase_out, additional_medicare_threshold, niit_threshold, long_term_gains_15_over, long_term_gains_20_over (the taxable income above which long-term gains are taxed at 15% and at 20%)}}, child_credit, other_dependent_credit, early_withdrawal_penalty_rate, social_security_rate, social_security_wage_base, medicare_rate, additional_medicare_rate, niit_rate, limit_401k, limit_ira, limit_hsa}; a state table is {name, year, brackets: [{over, rate}], standard_deduction: {mfj|single|hoh|mfs: amount}, exemption, dependent_exemption, cap_529}; the first bracket starts at over 0 and the rest ascend, each rate applying to the income above its over; tax table show prints both shapes"
)

var filingStatuses = func() []string {
	var out []string
	for _, s := range tax.FilingStatuses {
		out = append(out, string(s))
	}
	return out
}()

var householdRoles = func() []string {
	var out []string
	for _, r := range model.Roles {
		out = append(out, string(r))
	}
	return out
}()

var taxReadCommands = []*command{
	{
		Name: "household show", Summary: "show the household: its members, filing status, state tax table and reconciliation account",
		Output: "{filing_status (mfj, single, hoh or mfs), state (a state tax table's name), checking (the account id set as the reconciliation account, null for the first checking account), reconciliation_account (the account id in effect, null without a checking account), members: [{name, birth, role (earner, spouse or dependent)}]}",
		run:    cmdHouseholdShow,
	},
	{
		Name: "tax table show", Summary: "show the tax tables: with --year the federal table of that year, with --state and --year a state table, else which tables there are",
		Flags: []flagDef{
			{Name: "year", Type: typeInt, Description: "the tax year"},
			{Name: "state", Type: typeString, Description: "the state table's name; needs --year"},
		},
		Output: "without flags {federal: [year], state: [{name, year}]}; with flags the table, as tax table set reads it: " + taxTableNote,
		run:    cmdTaxTableShow,
	},
	{
		Name: "tax year", Summary: "the household's tax for a calendar year under a projection, as the engine books it: the year's income, contributions and withdrawals, the tax by kind, what paychecks and vests withheld and the reconciliation on 15 April of the next year",
		Args:   []argDef{{Name: "year", Description: "the calendar year", Required: true}},
		Flags:  []flagDef{{Name: "projection", Type: typeString, Description: projDesc}},
		Output: "{year, projection, filing_status, state, inputs: {wages (paychecks), vest_wages, pre_tax: {k401, ira, hsa} (before the limits), ordinary_income (other-ordinary items, interest and taxable withdrawals that are not early), interest (included in ordinary_income), early_withdrawal, short_term_gains and long_term_gains (realized by sales in taxable accounts, a loss negative; not in ordinary_income), dependent_ages, contrib_529: [one per 529 account]}, tax: {agi, federal_taxable, federal_tax (before credits), credits, niit, early_withdrawal_penalty, state_taxable, state_tax, social_security, medicare, additional_medicare, total, gains_tax (the part of total the gains add)}, withheld: {paychecks, vests, total} (those before the projection's start included), due_minus_withheld, reconciliation: {date, account (null without a checking account), amount (positive paid, negative refunded)}, " + diagShape + "}; dollars rounded to cents",
		run:    cmdTaxYear,
	},
	{
		Name: "tax compute", Summary: "compute the household's tax for a year from the tax tables, the household's filing status, dependents and state, and the amounts given",
		Flags: []flagDef{
			{Name: "year", Type: typeInt, Description: "the tax year; the year of the record's today when omitted"},
			{Name: "wages", Type: typeString, Description: "the wages of each earner for the year, comma separated, e.g. 120000,96000; the Social Security wage base applies to each; none when omitted"},
			{Name: "other-income", Type: typeNumber, Description: "other ordinary income, without payroll tax; 0 when omitted"},
			{Name: "k401", Type: typeString, Description: "pre-tax 401(k) and TSP deferrals of each earner in the order of --wages, comma separated; each is limited to the year's limit, the excess being post-tax; they reduce income but not FICA wages"},
			{Name: "ira", Type: typeString, Description: "traditional IRA contributions of each earner in the order of --wages, comma separated; each is limited to the year's limit and deducted in full"},
			{Name: "hsa", Type: typeString, Description: "HSA contributions through payroll of each earner in the order of --wages, comma separated; the household's total is limited to the year's family limit; they reduce income and FICA wages"},
			{Name: "net-investment-income", Type: typeNumber, Description: "net investment income, which the net investment income tax applies to"},
			{Name: "short-term-gains", Type: typeNumber, Description: "net realized capital gains on assets held a year or less (a loss is negative): ordinary income, and net investment income; 0 when omitted"},
			{Name: "long-term-gains", Type: typeNumber, Description: "net realized capital gains on assets held more than a year (a loss is negative): taxed at the long-term rates on top of ordinary taxable income, and net investment income; a net loss deducts up to 3,000 from ordinary income; 0 when omitted"},
			{Name: "early-withdrawal", Type: typeNumber, Description: "a taxable withdrawal before age 59 and a half: ordinary income with the early withdrawal penalty"},
			{Name: "contrib-529", Type: typeString, Description: "the year's contributions to each 529 account, comma separated, e.g. 4000,2500; each deducted from state income up to the state's cap"},
			{Name: "inflation", Type: typeNumber, Description: "annual inflation as a fraction, indexing a year past the last federal table; the record's inflation today when omitted"},
		},
		Output: "{year, filing_status, state, dependent_ages, agi, federal_taxable, federal_tax (before credits), credits, niit, early_withdrawal_penalty, state_taxable, state_tax, social_security, medicare, additional_medicare, total}; dollars rounded to cents",
		run:    cmdTaxCompute,
	},
}

var taxCommands = []*command{
	{
		Name: "household set", Summary: "change the household's filing status, state tax table or reconciliation account; flags given override the current values",
		Flags: []flagDef{
			{Name: "filing", Type: typeEnum, Choices: filingStatuses, Description: "filing status: mfj (married filing jointly, the default), single, hoh (head of household) or mfs (married filing separately)"},
			{Name: "state", Type: typeString, Description: "the name of the state tax table that taxes the household; it must exist (see tax table show)"},
			{Name: "checking", Type: typeString, Description: "the checking account (id or name) tax is reconciled against; none goes back to the first checking account"},
		},
		Output: householdOut,
		run:    cmdHouseholdSet,
	},
	{
		Name: "household member add", Summary: "add a person to the household",
		Args: []argDef{{Name: "name", Description: "the member's name, unique among the members", Required: true}},
		Flags: []flagDef{
			{Name: "birth", Type: typeDate, Required: true, Description: "date of birth: " + dateNote},
			{Name: "role", Type: typeEnum, Required: true, Choices: householdRoles, Description: "earner, spouse or dependent; dependents earn the child tax credit when under 17 and the credit for other dependents otherwise"},
		},
		Output: memberOut,
		run:    cmdMemberAdd,
	},
	{
		Name: "household member edit", Summary: "change a household member; flags given override the current values",
		Args: []argDef{{Name: "name", Description: "the member's name", Required: true}},
		Flags: []flagDef{
			{Name: "name", Type: typeString, Description: "the new name"},
			{Name: "birth", Type: typeDate, Description: "date of birth: " + dateNote},
			{Name: "role", Type: typeEnum, Choices: householdRoles, Description: "earner, spouse or dependent"},
		},
		Output: memberOut,
		run:    cmdMemberEdit,
	},
	{
		Name: "household member remove", Summary: "remove a person from the household",
		Args:   []argDef{{Name: "name", Description: "the member's name", Required: true}},
		Output: "{dry_run, changed, removed: name, member: the member as it was, household: as household show prints it, after the edit}",
		run:    cmdMemberRemove,
	},
	{
		Name: "tax table set", Summary: "replace the federal table of a year or a state table of a name and year with the JSON table, or add one; a table with a name is a state table, which with a new name adds a state a household can name",
		Input:  "a table: " + taxTableNote,
		Output: "{dry_run, changed, federal or state: the table, as tax table show prints it}",
		run:    cmdTaxTableSet,
	},
	{
		Name: "tax table revert", Summary: "restore the shipped federal table of --year, or with --state the shipped table of that state and year; a table the program ships none of is refused",
		Flags: []flagDef{
			{Name: "year", Type: typeInt, Required: true, Description: "the tax year"},
			{Name: "state", Type: typeString, Description: "the state table's name; the federal table when omitted"},
		},
		Output: "{dry_run, changed, federal or state: the table, as tax table show prints it}",
		run:    cmdTaxTableRevert,
	},
	{
		Name: "tax table copy", Summary: "add the state tax table --name as a copy of every year of the state table --state, which a household can then name and the tables can be edited to differ from",
		Flags: []flagDef{
			{Name: "state", Type: typeString, Required: true, Description: "the name of the state tax table to copy"},
			{Name: "name", Type: typeString, Required: true, Description: "the name of the new state table; no state may have it"},
		},
		Output: "{dry_run, changed, state: the new name, years: [the years copied]}",
		run:    cmdTaxTableCopy,
	},
}

func init() {
	commands = append(commands, taxReadCommands...)
}

type jhouseholdMember struct {
	Name  string `json:"name"`
	Birth jdate  `json:"birth"`
	Role  string `json:"role"`
}

func householdJSON(rec *model.Record) any {
	h := rec.Household
	members := []jhouseholdMember{}
	for _, m := range h.Members {
		members = append(members, jhouseholdMember{m.Name, jd(m.Birth), string(m.Role)})
	}
	return struct {
		Filing         string             `json:"filing_status"`
		State          string             `json:"state"`
		Checking       *string            `json:"checking"`
		Reconciliation *string            `json:"reconciliation_account"`
		Members        []jhouseholdMember `json:"members"`
	}{string(h.Filing), h.State, nullable(string(h.Checking)), nullable(string(engine.ReconciliationAccount(rec, rec.Today))), members}
}

func cmdHouseholdShow(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	return householdJSON(rec), nil
}

// householdResult is the output of an edit of the household: the household after the edit, or as it is when the edit changed nothing.
func (c *call) householdResult(res *ops.Result, err error) (map[string]any, error) {
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	rec := c.rec
	if changed {
		rec = res.Record
	}
	out["household"] = householdJSON(rec)
	return out, nil
}

func cmdHouseholdSet(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var ch ops.HouseholdChange
	if c.set["filing"] {
		ch.Filing = model.Ptr(tax.FilingStatus(c.str("filing")))
	}
	if c.set["state"] {
		ch.State = model.Ptr(c.str("state"))
	}
	if c.set["checking"] {
		id := model.AccountID("")
		if c.str("checking") != "none" {
			if id, err = c.accountNamed(c.str("checking")); err != nil {
				return nil, err
			}
		}
		ch.Checking = &id
	}
	res, err := ops.SetHousehold(st, rec, ch, c.options())
	return c.householdResult(res, err)
}

func cmdMemberAdd(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	birth, _, err := c.date("birth")
	if err != nil {
		return nil, err
	}
	m := model.HouseholdMember{Name: c.args[0], Birth: birth, Role: model.Role(c.str("role"))}
	res, err := ops.AddHouseholdMember(st, rec, m, c.options())
	return c.memberResult(res, err, m.Name)
}

func cmdMemberEdit(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var ch ops.MemberChange
	name := c.args[0]
	if c.set["name"] {
		ch.Name, name = model.Ptr(c.str("name")), strings.TrimSpace(c.str("name"))
	}
	if d, ok, err := c.date("birth"); err != nil {
		return nil, err
	} else if ok {
		ch.Birth = &d
	}
	if c.set["role"] {
		ch.Role = model.Ptr(model.Role(c.str("role")))
	}
	res, err := ops.EditHouseholdMember(st, rec, c.args[0], ch, c.options())
	return c.memberResult(res, err, name)
}

func cmdMemberRemove(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	res, err := ops.RemoveHouseholdMember(st, rec, c.args[0], c.options())
	out, err := c.memberResult(res, err, c.args[0])
	if err != nil {
		return nil, err
	}
	if res != nil {
		out["removed"] = res.Removed
	}
	return out, nil
}

// memberResult is the output of an edit of a household member; name is the member's name after the edit.
func (c *call) memberResult(res *ops.Result, err error, name string) (map[string]any, error) {
	out, err := c.householdResult(res, err)
	if err != nil {
		return nil, err
	}
	rec := c.rec
	if res != nil {
		rec = res.Record
	}
	if res != nil && res.Member != nil {
		out["member"] = jhouseholdMember{res.Member.Name, jd(res.Member.Birth), string(res.Member.Role)}
	} else if i := rec.Household.Member(name); i >= 0 {
		hm := rec.Household.Members[i]
		out["member"] = jhouseholdMember{hm.Name, jd(hm.Birth), string(hm.Role)}
	}
	return out, nil
}

func cmdTaxTableShow(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	year, haveYear, err := c.integer("year")
	if err != nil {
		return nil, err
	}
	switch {
	case !haveYear && c.set["state"]:
		return nil, usageErr("--state needs --year")
	case !haveYear:
		type key struct {
			Name string `json:"name"`
			Year int    `json:"year"`
		}
		out := struct {
			Federal []int `json:"federal"`
			State   []key `json:"state"`
		}{[]int{}, []key{}}
		for _, f := range rec.Tax.Federal {
			out.Federal = append(out.Federal, f.Year)
		}
		for _, s := range rec.Tax.State {
			out.State = append(out.State, key{s.Name, s.Year})
		}
		return out, nil
	case c.set["state"]:
		s, ok := rec.Tax.StateYear(c.str("state"), year)
		if !ok {
			return nil, failErr("not_found", "there is no state tax table %q for %d", c.str("state"), year)
		}
		return s, nil
	}
	f, ok := rec.Tax.FederalYear(year)
	if !ok {
		return nil, failErr("not_found", "there is no federal tax table for %d", year)
	}
	return f, nil
}

// taxTableResult is the output of an edit of the federal table of the year or, with a state name, the state table of the name and year: the table after the edit, or as it is when the edit changed nothing.
func (c *call) taxTableResult(res *ops.Result, err error, state string, year int) (any, error) {
	out, changed, err := c.dataResult(err)
	if err != nil {
		return nil, err
	}
	switch {
	case changed && res.Federal != nil:
		out["federal"] = res.Federal
	case changed:
		out["state"] = res.State
	case state == "":
		out["federal"], _ = c.rec.Tax.FederalYear(year)
	default:
		out["state"], _ = c.rec.Tax.StateYear(state, year)
	}
	return out, nil
}

func cmdTaxTableSet(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.readJSON(&raw); err != nil {
		return nil, err
	}
	var probe struct {
		Name *string `json:"name"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, usageErr("the JSON input: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if probe.Name != nil {
		var s tax.State
		if err := dec.Decode(&s); err != nil {
			return nil, usageErr("the JSON input: %v", err)
		}
		res, err := ops.SetStateTable(st, rec, s, c.options())
		return c.taxTableResult(res, err, s.Name, s.Year)
	}
	var f tax.Federal
	if err := dec.Decode(&f); err != nil {
		return nil, usageErr("the JSON input: %v", err)
	}
	res, err := ops.SetFederalTable(st, rec, f, c.options())
	return c.taxTableResult(res, err, "", f.Year)
}

func cmdTaxTableRevert(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	year, _, err := c.integer("year")
	if err != nil {
		return nil, err
	}
	if c.set["state"] {
		res, err := ops.RevertStateTable(st, rec, c.str("state"), year, c.options())
		return c.taxTableResult(res, err, c.str("state"), year)
	}
	res, err := ops.RevertFederalTable(st, rec, year, c.options())
	return c.taxTableResult(res, err, "", year)
}

func cmdTaxTableCopy(c *call) (any, error) {
	st, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	res, err := ops.CopyStateTables(st, rec, c.str("state"), c.str("name"), c.options())
	if err != nil {
		return nil, opsError(err)
	}
	years := []int{}
	for _, s := range res.Record.Tax.State {
		if s.Name == c.str("name") {
			years = append(years, s.Year)
		}
	}
	return map[string]any{"dry_run": c.options().DryRun, "changed": true, "state": c.str("name"), "years": years}, nil
}

func cmdTaxCompute(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	y, _, _ := rec.Today.YMD()
	year, ok, err := c.integer("year")
	if err != nil {
		return nil, err
	} else if !ok {
		year = y
	}
	in := tax.Input{
		Year: year, Status: rec.Household.Filing, State: rec.Household.State, DependentAges: rec.Household.DependentAges(year),
	}
	if v, ok := rec.Globals[model.PropInflation].At(rec.Today); ok {
		in.Inflation = v
	}
	for _, f := range []struct {
		name string
		out  *float64
	}{
		{"other-income", &in.OtherIncome},
		{"net-investment-income", &in.NetInvestmentIncome}, {"short-term-gains", &in.ShortTermGains}, {"long-term-gains", &in.LongTermGains}, {"early-withdrawal", &in.EarlyWithdrawal}, {"inflation", &in.Inflation},
	} {
		if v, ok, err := c.number(f.name); err != nil {
			return nil, err
		} else if ok {
			*f.out = v
		}
	}
	list := func(name string) ([]float64, error) {
		var vs []float64
		if !c.set[name] {
			return vs, nil
		}
		for _, s := range strings.Split(c.str(name), ",") {
			v, err := parseNumber(strings.TrimSpace(s))
			if err != nil {
				return nil, usageErr("--%s: %v", name, err)
			}
			vs = append(vs, v)
		}
		return vs, nil
	}
	if in.Contrib529, err = list("contrib-529"); err != nil {
		return nil, err
	}
	var perEarner [4][]float64
	for i, name := range []string{"wages", "k401", "ira", "hsa"} {
		if perEarner[i], err = list(name); err != nil {
			return nil, err
		}
		if len(perEarner[i]) > len(in.Earners) {
			in.Earners = append(in.Earners, make([]tax.Earner, len(perEarner[i])-len(in.Earners))...)
		}
	}
	for i := range in.Earners {
		at := func(vs []float64) float64 {
			if i < len(vs) {
				return vs[i]
			}
			return 0
		}
		in.Earners[i] = tax.Earner{Wages: at(perEarner[0]), K401: at(perEarner[1]), IRA: at(perEarner[2]), HSA: at(perEarner[3])}
	}
	res, err := tax.Compute(rec.Tax, in)
	if err != nil {
		return nil, failErr("invalid", "%v", err)
	}
	cents := func(v float64) float64 { return math.Round(v*100) / 100 }
	ages := in.DependentAges
	if ages == nil {
		ages = []int{}
	}
	return struct {
		Year           int     `json:"year"`
		Filing         string  `json:"filing_status"`
		State          string  `json:"state"`
		DependentAges  []int   `json:"dependent_ages"`
		AGI            float64 `json:"agi"`
		FederalTaxable float64 `json:"federal_taxable"`
		FederalTax     float64 `json:"federal_tax"`
		Credits        float64 `json:"credits"`
		NIIT           float64 `json:"niit"`
		Penalty        float64 `json:"early_withdrawal_penalty"`
		StateTaxable   float64 `json:"state_taxable"`
		StateTax       float64 `json:"state_tax"`
		SocialSecurity float64 `json:"social_security"`
		Medicare       float64 `json:"medicare"`
		AddlMedicare   float64 `json:"additional_medicare"`
		Total          float64 `json:"total"`
	}{year, string(in.Status), in.State, ages, cents(res.AGI), cents(res.FederalTaxable), cents(res.FederalTax), cents(res.Credits), cents(res.NIIT),
		cents(res.Penalty), cents(res.StateTaxable), cents(res.StateTax), cents(res.SocialSecurity), cents(res.Medicare), cents(res.AdditionalMedicare), cents(res.Total)}, nil
}

type jtax struct {
	AGI            float64 `json:"agi"`
	FederalTaxable float64 `json:"federal_taxable"`
	FederalTax     float64 `json:"federal_tax"`
	Credits        float64 `json:"credits"`
	NIIT           float64 `json:"niit"`
	Penalty        float64 `json:"early_withdrawal_penalty"`
	StateTaxable   float64 `json:"state_taxable"`
	StateTax       float64 `json:"state_tax"`
	SocialSecurity float64 `json:"social_security"`
	Medicare       float64 `json:"medicare"`
	AddlMedicare   float64 `json:"additional_medicare"`
	Total          float64 `json:"total"`
	GainsTax       float64 `json:"gains_tax"`
}

type jtaxYear struct {
	Year       int     `json:"year"`
	Projection *string `json:"projection"`
	Filing     string  `json:"filing_status"`
	State      string  `json:"state"`
	Inputs     struct {
		Wages     float64 `json:"wages"`
		VestWages float64 `json:"vest_wages"`
		PreTax    struct {
			K401 float64 `json:"k401"`
			IRA  float64 `json:"ira"`
			HSA  float64 `json:"hsa"`
		} `json:"pre_tax"`
		Ordinary        float64   `json:"ordinary_income"`
		Interest        float64   `json:"interest"`
		EarlyWithdrawal float64   `json:"early_withdrawal"`
		ShortTermGains  float64   `json:"short_term_gains"`
		LongTermGains   float64   `json:"long_term_gains"`
		DependentAges   []int     `json:"dependent_ages"`
		Contrib529      []float64 `json:"contrib_529"`
	} `json:"inputs"`
	Tax      jtax `json:"tax"`
	Withheld struct {
		Paychecks float64 `json:"paychecks"`
		Vests     float64 `json:"vests"`
		Total     float64 `json:"total"`
	} `json:"withheld"`
	Due            float64 `json:"due_minus_withheld"`
	Reconciliation struct {
		Date    jdate   `json:"date"`
		Account *string `json:"account"`
		Amount  float64 `json:"amount"`
	} `json:"reconciliation"`
	Diagnostics jdiagnostics `json:"diagnostics"`
}

func cmdTaxYear(c *call) (any, error) {
	_, rec, err := c.open()
	if err != nil {
		return nil, err
	}
	year, err := strconv.Atoi(c.args[0])
	if err != nil || year < 1 || year > 9998 {
		return nil, usageErr("year %q is not a calendar year", c.args[0])
	}
	projection, err := c.projection(rec)
	if err != nil {
		return nil, err
	}
	ty, err := engine.TaxYearOf(rec, projection, year)
	if err != nil {
		return nil, failErr("invalid", "%v", err)
	}
	cents := func(v float64) float64 { return math.Round(v*100) / 100 }
	ages := ty.Input.DependentAges
	if ages == nil {
		ages = []int{}
	}
	contrib := []float64{}
	for _, v := range ty.Input.Contrib529 {
		contrib = append(contrib, cents(v))
	}
	res := ty.Result
	var out jtaxYear
	out.Year, out.Projection = year, nullable(projection)
	out.Filing, out.State = string(ty.Input.Status), ty.Input.State
	in := &out.Inputs
	in.Wages, in.VestWages = cents(ty.Wages), cents(ty.VestWages)
	in.PreTax.K401, in.PreTax.IRA, in.PreTax.HSA = cents(ty.K401), cents(ty.IRA), cents(ty.HSA)
	in.Ordinary, in.Interest, in.EarlyWithdrawal = cents(ty.Ordinary), cents(ty.Interest), cents(ty.EarlyWithdrawal)
	in.ShortTermGains, in.LongTermGains = cents(ty.ShortTermGains), cents(ty.LongTermGains)
	in.DependentAges, in.Contrib529 = ages, contrib
	out.Tax = jtax{cents(res.AGI), cents(res.FederalTaxable), cents(res.FederalTax), cents(res.Credits), cents(res.NIIT), cents(res.Penalty),
		cents(res.StateTaxable), cents(res.StateTax), cents(res.SocialSecurity), cents(res.Medicare), cents(res.AdditionalMedicare), cents(res.Total), cents(ty.GainsTax)}
	out.Withheld.Paychecks, out.Withheld.Vests, out.Withheld.Total = cents(ty.WithheldPaychecks), cents(ty.WithheldVests), cents(ty.Withheld())
	out.Due = cents(ty.Due)
	out.Reconciliation.Date, out.Reconciliation.Account, out.Reconciliation.Amount = jd(ty.ReconciliationDate), nullable(string(ty.ReconciliationAccount)), cents(ty.Due)
	out.Diagnostics = diagnosticsJSON(ty.Diagnostics)
	return out, nil
}
