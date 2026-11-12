// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import (
	"strings"

	"github.com/joeriedel/finplan/internal/model"
)

// allBlank reports whether none of the texts holds anything but spaces: the form they belong to is untouched.
func allBlank(texts ...string) bool {
	for _, s := range texts {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

// untouched reports whether nothing has been typed or picked in the Add FERS form.
func (f fersForm) untouched() bool {
	return allBlank(f.high3, f.years, f.sep, f.start) && f.survivor == ""
}

// untouched reports whether nothing has been typed or picked in the Add member form.
func (f householdForm) untouched() bool { return allBlank(f.name, f.birth) && f.role == "" }

// untouched reports whether nothing has been typed into the form. The rate is not shown for an equity account, so it does not count there.
func (f newAcctForm) untouched() bool {
	return allBlank(f.name) && (f.typ == model.TypeEquity || allBlank(f.rate))
}

// untouched reports whether the form still holds what the grant had: the draft is the grant's own and no note is typed.
func (d grantDraft) untouched(g *model.Grant) bool { return d == draftOfGrant(g) }

// untouched reports whether nothing has been typed into the draft.
func (d entryDraft) untouched() bool { return allBlank(d.date, d.amount, d.memo) }

// untouched reports whether the only thing missing is the pick: no kind yet, or an account or asset kind with nothing chosen. A rule member needs no pick, so it is never untouched.
func (f memberForm) untouched() bool {
	switch f.kind {
	case memberAccount:
		return f.account == ""
	case memberAsset:
		return f.asset == ""
	case memberRule:
		return false
	}
	return true
}

// untouched reports whether nothing has been typed or picked in the form.
func (f acctForm) untouched() bool {
	return allBlank(f.name, f.rate, f.opening, f.from) && f.typ == "" && f.tax == ""
}

// untouched reports whether nothing has been picked or typed in the form: no asset, no start and the default override.
func (f settingForm) untouched() bool { return f.asset == "" && allBlank(f.start) && f.override == 0 }

// untouched reports whether nothing has been typed in the form and no account picked; the kind and the recurrence always hold a value.
func (f itemForm) untouched() bool {
	return allBlank(f.name, f.amount, f.growth, f.date, f.day, f.month, f.end, f.day2, f.count, f.matchPercent, f.matchUpTo) &&
		f.account == "" && f.from == "" && f.to == "" && f.matchAccount == ""
}

// modUntouched reports whether nothing has been picked or typed in the Add modification form: no object and no value or dates. The kind, property and how always hold a value.
func modUntouched(obj, value, start, end string) bool {
	return obj == "" && allBlank(value, start, end)
}

// untouched reports whether nothing has been typed or picked in the Add grant form that the user starts: the amount, price, date, percents and cliff are blank and no first vest is picked. The stock, the calendar and deposit account (those of the account's latest grant), the choice of value or shares and the rates start filled.
func (f grantForm) untouched() bool {
	return allBlank(f.amount, f.price, f.date, f.years, f.cliff) && f.first == 0
}

// allocUntouched reports whether the allocation editor's percent fields still hold what the account's allocation says.
func allocUntouched(rec *model.Record, acct *model.Account, texts map[string]string) bool {
	want := allocTexts(acct)
	for _, c := range rec.Classes {
		if strings.TrimSpace(texts[c.ID]) != want[c.ID] {
			return false
		}
	}
	return true
}
