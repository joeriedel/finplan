// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package graph

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/joeriedel/finplan/internal/engine"
	"github.com/joeriedel/finplan/internal/model"
)

// StaleDays is how many days before today a line's last manual entry must lie for the line to be labelled.
const StaleDays = 45

// Span is a run of dates sharing a status.
type Span struct {
	From, To model.Date
	Status   engine.Status
}

// StatusSpans merges consecutive samples of the same status. Each span runs from its first sample to the
// first sample of the next span, and the last one to the last sample. A line segment takes the status of
// the sample it starts at.
func StatusSpans(samples []engine.Sample) []Span {
	var out []Span
	for _, s := range samples {
		if n := len(out); n > 0 && out[n-1].Status == s.Status {
			out[n-1].To = s.Date
			continue
		}
		if n := len(out); n > 0 {
			out[n-1].To = s.Date
		}
		out = append(out, Span{From: s.Date, To: s.Date, Status: s.Status})
	}
	return out
}

// Handoff returns the first sample that is projected (fully or partly) right after an actual one: where the
// line passes from recorded data to projection. It reports false if no such change lies in the samples.
func Handoff(samples []engine.Sample) (engine.Sample, bool) {
	for i := 1; i < len(samples); i++ {
		p := samples[i].Status
		if samples[i-1].Status == engine.StatusActual && (p == engine.StatusProjected || p == engine.StatusPartlyProjected) {
			return samples[i], true
		}
	}
	return engine.Sample{}, false
}

// Stale reports whether the last manual entry lies more than StaleDays before today.
func Stale(lastActual, today model.Date) bool { return int(today-lastActual) > StaleDays }

// StaleLabel returns "name · last manual entry Jan 2 2006" if the last actual date is more than StaleDays before today.
func StaleLabel(name string, lastActual, today model.Date) (string, bool) {
	if !Stale(lastActual, today) {
		return "", false
	}
	return name + " · last manual entry " + FormatDate(lastActual), true
}

// LastActual returns the latest last-actual date over the starts that have actual data.
func LastActual(starts []engine.AccountStart) (model.Date, bool) {
	var last model.Date
	found := false
	for _, s := range starts {
		if s.HasActual && (!found || s.LastActual > last) {
			last, found = s.LastActual, true
		}
	}
	return last, found
}

// At returns the line's value at the date by linear interpolation between the two samples around it,
// with the status of the earlier one (the one the line segment starts at). It reports false outside the samples.
func At(samples []engine.Sample, d model.Date) (float64, engine.Status, bool) {
	n := len(samples)
	if n == 0 || d < samples[0].Date || d > samples[n-1].Date {
		return 0, 0, false
	}
	lo, hi := 0, n-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if samples[mid].Date <= d {
			lo = mid
		} else {
			hi = mid
		}
	}
	a, b := samples[lo], samples[hi]
	if d == b.Date {
		return b.Amount, b.Status, true
	}
	if b.Date == a.Date {
		return a.Amount, a.Status, true
	}
	t := float64(d-a.Date) / float64(b.Date-a.Date)
	return a.Amount + (b.Amount-a.Amount)*t, a.Status, true
}

// DataStart returns the earliest snapshot or transaction date in the record, or today if there is none.
func DataStart(rec *model.Record) model.Date {
	start, found := rec.Today, false
	for _, a := range rec.Accounts {
		for _, s := range a.Snapshots {
			if !found || s.Date < start {
				start, found = s.Date, true
			}
		}
		for _, t := range a.Transactions {
			if !found || t.Date < start {
				start, found = t.Date, true
			}
		}
	}
	return start
}

// FormatDate formats a date as "Jan 2 2006".
func FormatDate(d model.Date) string { return d.Time().Format("Jan 2 2006") }

// FormatMoney formats whole dollars with thousands separators: "$1,234" and "-$1,234".
func FormatMoney(v float64) string {
	r := math.Round(math.Abs(v))
	s := fmt.Sprintf("%.0f", r)
	var b strings.Builder
	if v < 0 && r != 0 {
		b.WriteByte('-')
	}
	b.WriteByte('$')
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// FormatMoneyCompact formats a money value for an axis tick: "$1.5M", "$250K", "-$40K", "$800".
func FormatMoneyCompact(v float64) string {
	sign := ""
	if v < 0 {
		sign = "-"
	}
	a := math.Abs(v)
	trim := func(x float64, unit string) string {
		return sign + "$" + strconv.FormatFloat(math.Round(x*100)/100, 'f', -1, 64) + unit
	}
	switch {
	case a >= 1e9:
		return trim(a/1e9, "B")
	case a >= 1e6:
		return trim(a/1e6, "M")
	case a >= 1e3:
		return trim(a/1e3, "K")
	}
	return sign + fmt.Sprintf("$%.0f", a)
}

// NiceTicks returns round tick values within [lo, hi], about target of them, spaced at 1, 2 or 5 times a power of ten.
func NiceTicks(lo, hi float64, target int) []float64 {
	if !(hi > lo) || target < 1 {
		return nil
	}
	raw := (hi - lo) / float64(target)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step := mag
	for _, m := range []float64{1, 2, 5, 10} {
		step = m * mag
		if step >= raw {
			break
		}
	}
	var out []float64
	for v := math.Ceil(lo/step) * step; v <= hi+step*1e-9; v += step {
		out = append(out, math.Round(v/step)*step+0)
	}
	return out
}
