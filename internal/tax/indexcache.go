// Copyright (c) 2026 Joseph Riedel
// SPDX-License-Identifier: MIT

package tax

import (
	"math"
	"sync"
)

// indexKey names an indexed table: the content of the table it was indexed from, the year and the inflation. Keying on content keeps an edited table from reading an entry of its earlier self.
type indexKey struct {
	source    uint64
	year      int
	inflation float64
}

// maxIndexed bounds each cache; one that fills up is dropped whole, as the entries are cheap to make again.
const maxIndexed = 4096

// indexCache holds the tables Federal.index and State.index made, shared by every caller and goroutine. A cached table is never changed.
type indexCache struct {
	mu   sync.RWMutex
	fed  map[indexKey]Federal
	stat map[indexKey]State
}

var indexed indexCache

func (c *indexCache) federal(k indexKey) (Federal, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.fed[k]
	return v, ok
}

func (c *indexCache) putFederal(k indexKey, v Federal) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fed == nil || len(c.fed) >= maxIndexed {
		c.fed = map[indexKey]Federal{}
	}
	c.fed[k] = v
}

func (c *indexCache) state(k indexKey) (State, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.stat[k]
	return v, ok
}

func (c *indexCache) putState(k indexKey, v State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stat == nil || len(c.stat) >= maxIndexed {
		c.stat = map[indexKey]State{}
	}
	c.stat[k] = v
}

// fnv1a hashing, inlined so that fingerprinting a table allocates nothing.
const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

func hashFloat(h uint64, v float64) uint64 {
	b := math.Float64bits(v)
	for range 8 {
		h = (h ^ (b & 0xff)) * fnvPrime
		b >>= 8
	}
	return h
}

func hashString(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h = (h ^ uint64(s[i])) * fnvPrime
	}
	return (h ^ 0) * fnvPrime
}

func hashBrackets(h uint64, bs []Bracket) uint64 {
	h = hashFloat(h, float64(len(bs)))
	for _, b := range bs {
		h = hashFloat(hashFloat(h, b.Over), b.Rate)
	}
	return h
}

// fingerprint hashes everything of the table. The statuses are combined by addition, so the map's order does not matter.
func (f Federal) fingerprint() uint64 {
	h := uint64(fnvOffset)
	for _, v := range [...]float64{float64(f.Year), f.ChildCredit, f.OtherDependentCredit, f.PenaltyRate, f.SocialSecurityRate, f.SocialSecurityWageBase,
		f.MedicareRate, f.AdditionalMedicareRate, f.NIITRate, f.Limit401k, f.LimitIRA, f.LimitHSA} {
		h = hashFloat(h, v)
	}
	for k, s := range f.Status {
		sh := hashBrackets(hashString(fnvOffset, string(k)), s.Brackets)
		for _, v := range [...]float64{s.StandardDeduction, s.ChildCreditPhaseOut, s.AdditionalMedicareThreshold, s.NIITThreshold,
			s.LongTermGains15Over, s.LongTermGains20Over, s.BenefitBase, s.BenefitAdjusted} {
			sh = hashFloat(sh, v)
		}
		h += sh
	}
	return h
}

// fingerprint hashes everything of the table, like Federal.fingerprint.
func (s State) fingerprint() uint64 {
	h := hashBrackets(hashString(fnvOffset, s.Name), s.Brackets)
	for _, v := range [...]float64{float64(s.Year), s.Exemption, s.DependentExemption, s.Cap529} {
		h = hashFloat(h, v)
	}
	for k, v := range s.StandardDeduction {
		h += hashFloat(hashString(fnvOffset, string(k)), v)
	}
	return h
}
