// Package reduce shrinks a failing trace by removing only intervals
// irrelevant to the failure. The time origin is never changed: ticks of
// surviving changes keep their original values, removed changes are
// simply absent. Multiple legal minimal candidates are all preserved.
package reduce

import (
	"fmt"

	"pairwise-gsb/internal/eval"
	"pairwise-gsb/internal/prop"
	"pairwise-gsb/internal/vcd"
)

// Candidate is one minimal counterexample.
type Candidate struct {
	ID           string  `json:"id"`
	RemovedTicks []int64 `json:"removed_ticks"` // ticks whose changes were dropped
	KeptTicks    []int64 `json:"kept_ticks"`
	FailThread   string  `json:"fail_thread"`
	OriginTick   int64   `json:"origin_tick"` // always equals the original first tick
}

// Minimize finds minimal sets of waveform changes that still reproduce
// the failure of the first failing thread of property p. It runs greedy
// minimization in both scan orders and keeps every distinct candidate.
func Minimize(w *vcd.Waveform, p *prop.Property, semantics string) ([]*Candidate, error) {
	base, err := eval.Run(w, p, semantics)
	if err != nil {
		return nil, err
	}
	failThread := ""
	for _, t := range base.Threads {
		if t.Outcome == eval.Fail {
			failThread = t.ID
			break
		}
	}
	if failThread == "" {
		return nil, fmt.Errorf("property %s has no failing thread to minimize", p.Name)
	}
	// Group change indices by tick; a tick is the removal unit.
	var ticks []int64
	seen := map[int64]bool{}
	for _, c := range w.Changes {
		if !seen[c.Tick] {
			seen[c.Tick] = true
			ticks = append(ticks, c.Tick)
		}
	}
	stillFails := func(removed map[int64]bool) bool {
		nw := &vcd.Waveform{Raw: w.Raw, Timescale: w.Timescale, Signals: w.Signals, EndTick: w.EndTick}
		for _, c := range w.Changes {
			if !removed[c.Tick] {
				nw.Changes = append(nw.Changes, c)
			}
		}
		r, err := eval.Run(nw, p, semantics)
		if err != nil {
			return false
		}
		for _, t := range r.Threads {
			if t.ID == failThread && t.Outcome == eval.Fail {
				return true
			}
		}
		return false
	}
	greedy := func(order []int64) *Candidate {
		removed := map[int64]bool{}
		for _, tk := range order {
			removed[tk] = true
			if !stillFails(removed) {
				delete(removed, tk)
			}
		}
		c := &Candidate{FailThread: failThread}
		if len(w.Changes) > 0 {
			c.OriginTick = w.Changes[0].Tick
		}
		for _, tk := range ticks {
			if removed[tk] {
				c.RemovedTicks = append(c.RemovedTicks, tk)
			} else {
				c.KeptTicks = append(c.KeptTicks, tk)
			}
		}
		return c
	}
	fwd := append([]int64{}, ticks...)
	rev := append([]int64{}, ticks...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	var out []*Candidate
	seenKey := map[string]bool{}
	for i, order := range [][]int64{fwd, rev} {
		c := greedy(order)
		key := fmt.Sprint(c.RemovedTicks)
		if seenKey[key] {
			continue
		}
		seenKey[key] = true
		c.ID = fmt.Sprintf("min-%s-%d", p.Name, i)
		out = append(out, c)
	}
	return out, nil
}

// Apply produces the reduced waveform for a candidate. Timestamps are
// untouched: the time origin and every surviving tick stay identical.
func Apply(w *vcd.Waveform, c *Candidate) *vcd.Waveform {
	removed := map[int64]bool{}
	for _, t := range c.RemovedTicks {
		removed[t] = true
	}
	nw := &vcd.Waveform{Raw: w.Raw, Timescale: w.Timescale, Signals: w.Signals, EndTick: w.EndTick}
	for _, ch := range w.Changes {
		if !removed[ch.Tick] {
			nw.Changes = append(nw.Changes, ch)
		}
	}
	return nw
}
