// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package main

import "strings"

// allBlank reports whether none of the texts holds anything but spaces: the form they belong to is untouched.
func allBlank(texts ...string) bool {
	for _, s := range texts {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

// untouched reports whether nothing has been typed into the form.
func (f newAcctForm) untouched() bool { return allBlank(f.name, f.owner, f.rate) }

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
	return allBlank(f.name, f.amount, f.growth, f.date, f.day, f.month, f.end, f.day2, f.count) &&
		f.account == "" && f.from == "" && f.to == ""
}

// modUntouched reports whether nothing has been picked or typed in the Add modification form: no object and no value or dates. The kind, property and how always hold a value.
func modUntouched(obj, value, start, end string) bool {
	return obj == "" && allBlank(value, start, end)
}
