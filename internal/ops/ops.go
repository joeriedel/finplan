// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

// Package ops holds the edits of a record: its projections, accounts, assets, budget and recorded entries. Every editor, whether the GUI or the command line, makes its edit through these functions, so an edit is validated, applied and saved the same way whoever makes it.
//
// An operation takes the loaded record and the store it came from. It validates its input, applies the change to a copy of the record's editable documents and saves the copy in one transaction. The record passed in is never changed; the result carries the new one. If the operation fails, nothing was saved. With Options.DryRun the operation does all of that except the saving.
package ops

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/joeriedel/finplan/internal/graph"
	"github.com/joeriedel/finplan/internal/model"
	"github.com/joeriedel/finplan/internal/store"
)

// The reasons an operation refuses. Errors returned by operations match these with errors.Is, as well as model.ErrCycle.
var (
	// ErrNoChange is returned by an edit that would leave the record as it is. Nothing is saved.
	ErrNoChange = errors.New("ops: no change")
	// ErrEmptyName is returned for a projection name that is empty once trimmed.
	ErrEmptyName = errors.New("ops: a projection needs a name")
	// ErrNameTaken is returned for a projection name that another projection has.
	ErrNameTaken = errors.New("ops: projection name already used")
	// ErrUnknownProjection is returned for a projection name the record does not have.
	ErrUnknownProjection = errors.New("ops: unknown projection")
	// ErrHasChildren is returned by a delete of a projection that others build on.
	ErrHasChildren = errors.New("ops: projection has projections building on it")
	// ErrNoObject is returned for a modification that needs an object and names none.
	ErrNoObject = errors.New("ops: a modification needs an object")
	// ErrInvalidValue is returned for a modification or budget item with a value that is not valid: a number out of range, a property that does not suit the object, an item that is incomplete.
	ErrInvalidValue = errors.New("ops: value out of range")
	// ErrUnknownTarget is returned for a modification whose object the record does not have or, for a budget item, that no earlier add_item mod in the chain adds; and for an edit that would leave a modification that resolved before pointing at nothing.
	ErrUnknownTarget = errors.New("ops: unknown modification target")
	// ErrUnknownObject is returned for an account, asset, asset member, base budget item or entry that the record does not have.
	ErrUnknownObject = errors.New("ops: unknown object")
	// ErrInUse is returned by a delete of an account that has record entries or is the counter account of one: it is closed instead.
	ErrInUse = errors.New("ops: object is still in use")
)

// maxValue is the largest magnitude a modification value may have.
const maxValue = 1e15

// refusal is an error with a message of its own that matches kind.
type refusal struct {
	kind error
	msg  string
}

func (e *refusal) Error() string { return e.msg }
func (e *refusal) Unwrap() error { return e.kind }

func refuse(kind error, format string, args ...any) error {
	return &refusal{kind, fmt.Sprintf(format, args...)}
}

// SaveError is returned when the record could not be saved. Nothing was changed.
type SaveError struct{ Err error }

func (e *SaveError) Error() string { return "could not save: " + e.Err.Error() }
func (e *SaveError) Unwrap() error { return e.Err }

// Options changes how an operation runs.
type Options struct {
	// DryRun validates and applies the edit to a copy and returns the result, but saves nothing: neither the record nor any preset.
	DryRun bool
}

// Rename is the rename of a projection. Editors that keep the name elsewhere (graph lines, say) follow it.
type Rename struct{ From, To string }

// Reverse returns the rename that undoes r; nil for nil.
func (r *Rename) Reverse() *Rename {
	if r == nil {
		return nil
	}
	return &Rename{r.To, r.From}
}

// PresetWarning is a failure to rewrite a saved view preset after a rename. The rename itself was saved.
type PresetWarning struct {
	// Name is the preset; empty if the stored presets could not be read at all.
	Name string
	Err  error
}

func (w PresetWarning) Error() string {
	if w.Name == "" {
		return fmt.Sprintf("view presets: %v", w.Err)
	}
	return fmt.Sprintf("view preset %q: %v", w.Name, w.Err)
}

// Result is what an operation did.
type Result struct {
	// Record is the record with the edit applied. It is saved unless the operation was a dry run.
	Record *model.Record
	// Projection is the projection the edit changed or added, in Record; nil for a delete, a restore and an edit of anything but projections.
	Projection *model.Projection
	// Account is the account an account, entry or default balance edit changed or added, in Record; for an account delete, the account as it was.
	Account *model.Account
	// Asset is the asset an asset edit changed or added, in Record; for an asset delete, the asset as it was.
	Asset *model.Asset
	// Item is the base budget item a budget edit changed or added.
	Item *model.BudgetItem
	// Entry is the entry an entry operation added, corrected or voided, with its values after the operation. Its ID is 0 for an entry added in a dry run. Record is the reloaded record for a correction and a void, and nil for their dry run.
	Entry *store.Entry
	// Removed is the name of the projection or id of the asset or account a delete removed.
	Removed string
	// Rename is set when the edit renamed a projection.
	Rename *Rename
	// Rewritten names the saved view presets a rename rewrote (in a dry run, would rewrite).
	Rewritten []string
	// Warnings are failures to rewrite presets. They do not undo the saved record.
	Warnings []PresetWarning
}

// edit runs change on a copy of the editable documents of rec and commits the copy. It refuses a change that leaves the projections as they were.
func edit(st *store.Store, rec *model.Record, o Options, change func(next *model.Record) (Result, error)) (*Result, error) {
	next := rec.EditableCopy()
	res, err := change(next)
	if err != nil {
		return nil, err
	}
	if reflect.DeepEqual(rec.Projections, next.Projections) {
		return nil, ErrNoChange
	}
	if err := checkNoNewDangling(rec, next, res.Rename); err != nil {
		return nil, err
	}
	return commit(st, next, res, o)
}

// editRecord is edit for a change to the accounts, assets or budget. It runs the change on a deep copy of the record and refuses a change that leaves them as they were.
func editRecord(st *store.Store, rec *model.Record, o Options, change func(next *model.Record) (Result, error)) (*Result, error) {
	next := rec.Clone()
	res, err := change(next)
	if err != nil {
		return nil, err
	}
	if reflect.DeepEqual(rec.Accounts, next.Accounts) && reflect.DeepEqual(rec.Assets, next.Assets) && reflect.DeepEqual(rec.Budget, next.Budget) {
		return nil, ErrNoChange
	}
	return commit(st, next, res, o)
}

// editProjection is edit for a change to one existing projection.
func editProjection(st *store.Store, rec *model.Record, o Options, name string, change func(p *model.Projection) error) (*Result, error) {
	return edit(st, rec, o, func(next *model.Record) (Result, error) {
		p := next.Projection(name)
		if p == nil {
			return Result{}, errUnknown(name)
		}
		if err := change(p); err != nil {
			return Result{}, err
		}
		return Result{Projection: p}, nil
	})
}

func errUnknown(name string) error {
	return refuse(ErrUnknownProjection, "there is no projection named %q", name)
}

// commit saves next (a dry run only checks that the record could be saved and restored) and, after a rename, rewrites the presets that name the projection.
func commit(st *store.Store, next *model.Record, res Result, o Options) (*Result, error) {
	res.Record = next
	save := st.Save
	if o.DryRun {
		save = store.CheckSave
	}
	if err := save(next); err != nil {
		return nil, &SaveError{err}
	}
	if res.Rename != nil {
		renamePresets(st, &res, o)
	}
	return &res, nil
}

// renamePresets rewrites each stored preset that names the renamed projection. A preset that cannot be decoded is skipped, as the editors skip it when they load presets.
func renamePresets(st *store.Store, res *Result, o Options) {
	rows, err := st.Presets()
	if err != nil {
		res.Warnings = append(res.Warnings, PresetWarning{Err: err})
		return
	}
	for _, r := range rows {
		p, err := graph.DecodePreset(r.Name, r.Format, r.Body)
		if err != nil {
			continue
		}
		np, changed := p.RenameProjection(res.Rename.From, res.Rename.To)
		if !changed {
			continue
		}
		if !o.DryRun {
			body, err := graph.EncodePreset(np)
			if err == nil {
				err = st.PutPreset(np.Name, graph.PresetFormat, body)
			}
			if err != nil {
				res.Warnings = append(res.Warnings, PresetWarning{r.Name, err})
				continue
			}
		}
		res.Rewritten = append(res.Rewritten, r.Name)
	}
}
