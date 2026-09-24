package replay

import (
	"fmt"
	"sort"

	"sigcourt/internal/logic4"
	"sigcourt/internal/prop"
	"sigcourt/internal/vcd"
)

// ReductionCandidate is one minimal-counterexample candidate. Reduction
// only removes intervals unrelated to the failure and never renumbers
// time: the origin stays where it was.
type ReductionCandidate struct {
	Label            string
	RemovedIntervals [][2]int64
	RemovedCount     int
	KeptChanges      []vcd.Change
	Verdict          logic4.Tribool // verdict of re-running on the reduced waveform
	OriginPreserved  bool
}

// Reduce computes minimal-failure-trace candidates for a failed run.
// The footprint is the set of sampled reads of failing threads; a raw
// change is relevant when it feeds a footprint read, drives the sampling
// clock, or sits at the time origin. Everything else forms removable
// intervals; each interval (and their union) yields one candidate that is
// verified by replaying the property on the reduced waveform.
func Reduce(w *Waveform, body prop.Node, clk ClockSpec, resets []ResetInterval, ver Version, run *RunResult) []ReductionCandidate {
	ticks := SampleTicks(w, clk, resets)
	var kept []Tick
	for _, t := range ticks {
		if !t.Skipped {
			kept = append(kept, t)
		}
	}
	// 1. footprint reads of failing threads
	relevant := map[int]bool{} // indexes into w.Changes
	markRead := func(sig string, tickIdx int) {
		if tickIdx < 0 || tickIdx >= len(kept) {
			return
		}
		tt := kept[tickIdx].Time
		best := -1
		for i, c := range w.Changes {
			if c.Time >= tt {
				break
			}
			if c.Signal == sig {
				best = i
			}
		}
		if best >= 0 {
			relevant[best] = true
		}
	}
	for _, th := range run.Threads {
		if th.Status != "failed" {
			continue
		}
		for _, s := range th.Steps {
			for _, r := range s.Reads {
				markRead(r.Signal, r.Tick)
			}
		}
	}
	// 2. clock changes and origin changes are never removable
	minTime := int64(0)
	if len(w.Changes) > 0 {
		minTime = w.Changes[0].Time
	}
	for i, c := range w.Changes {
		if c.Signal == clk.Signal || c.Time == minTime {
			relevant[i] = true
		}
	}
	// 3. group removable changes into maximal intervals
	type interval struct {
		lo, hi int64 // [lo, hi) in time; hi is the next relevant change time or +inf
		idxs   []int
	}
	var intervals []interval
	i := 0
	n := len(w.Changes)
	for i < n {
		if relevant[i] {
			i++
			continue
		}
		iv := interval{lo: w.Changes[i].Time}
		hi := int64(-1)
		for i < n && !relevant[i] {
			iv.idxs = append(iv.idxs, i)
			i++
		}
		if i < n {
			hi = w.Changes[i].Time
		}
		iv.hi = hi
		intervals = append(intervals, iv)
	}
	if len(intervals) == 0 {
		return nil
	}
	// 4. build candidates: one per interval, plus the union
	type plan struct {
		label string
		drop  map[int]bool
		ivs   [][2]int64
	}
	var plans []plan
	for k, iv := range intervals {
		p := plan{label: fmt.Sprintf("remove-interval-%d", k+1), drop: map[int]bool{}}
		for _, idx := range iv.idxs {
			p.drop[idx] = true
		}
		p.ivs = [][2]int64{{iv.lo, iv.hi}}
		plans = append(plans, p)
	}
	if len(intervals) > 1 {
		p := plan{label: "remove-all-unrelated", drop: map[int]bool{}}
		for _, iv := range intervals {
			for _, idx := range iv.idxs {
				p.drop[idx] = true
			}
			p.ivs = append(p.ivs, [2]int64{iv.lo, iv.hi})
		}
		plans = append(plans, p)
	}
	// 5. verify each candidate by replaying on the reduced waveform
	var out []ReductionCandidate
	for _, p := range plans {
		var keptChanges []vcd.Change
		for idx, c := range w.Changes {
			if !p.drop[idx] {
				keptChanges = append(keptChanges, c)
			}
		}
		rw := NewWaveform(w.Signals, keptChanges)
		rticks := SampleTicks(rw, clk, resets)
		eng := &Engine{Version: ver}
		rerun := eng.EvalProperty(body, rticks, resets)
		if rerun.Verdict != run.Verdict {
			continue // candidate does not preserve the failure; discard
		}
		origin := len(keptChanges) > 0 && keptChanges[0].Time == minTime
		out = append(out, ReductionCandidate{
			Label:            p.label,
			RemovedIntervals: p.ivs,
			RemovedCount:     len(p.drop),
			KeptChanges:      keptChanges,
			Verdict:          rerun.Verdict,
			OriginPreserved:  origin,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RemovedCount > out[j].RemovedCount })
	return out
}
