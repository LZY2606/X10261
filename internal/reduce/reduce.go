// Package reduce shrinks failing traces to minimal counterexamples.
//
// Rule: reduction only removes intervals that no failing cycle references;
// it never re-bases timestamps, so the time origin of the source waveform is
// preserved in every candidate.
package reduce

import (
	"fmt"

	"sigtribunal/internal/prop"
)

// Interval is a kept or removed cycle range with absolute timestamps and
// the waveform's time unit (coordinate basis).
type Interval struct {
	StartCycle int    `json:"start_cycle"`
	EndCycle   int    `json:"end_cycle"`
	Start      int64  `json:"start"`
	End        int64  `json:"end"`
	Unit       string `json:"unit"`
}

// Candidate is one legal minimal counterexample. When several are legal the
// system keeps all of them for adjudication.
type Candidate struct {
	ID      string     `json:"id"`
	Thread  string     `json:"thread"`
	Kept    []Interval `json:"kept"`
	Removed []Interval `json:"removed"`
	Origin  int64      `json:"origin"` // time origin of the source waveform, never changed
	Unit    string     `json:"unit"`
}

// Reduce builds one minimal-counterexample candidate per failing path of run.
func Reduce(run *prop.Run) []Candidate {
	var out []Candidate
	s := run.Sampled
	if s == nil || len(s.Times) == 0 {
		return out
	}
	origin := s.Times[0]
	for i, fp := range run.FailPaths {
		keptSet := map[int]bool{}
		for _, c := range fp.Cycles {
			keptSet[c] = true
		}
		var kept, removed []Interval
		for c := fp.Window[0]; c <= fp.Window[1] && c < len(s.Times); c++ {
			iv := Interval{
				StartCycle: c, EndCycle: c,
				Start: s.Times[c], End: endTime(s, c), Unit: run.Unit,
			}
			if keptSet[c] {
				kept = append(kept, iv)
			} else {
				removed = append(removed, iv)
			}
		}
		out = append(out, Candidate{
			ID:      fmt.Sprintf("%s-%s-c%d", run.Property, fp.ThreadID, i),
			Thread:  fp.ThreadID,
			Kept:    merge(kept),
			Removed: merge(removed),
			Origin:  origin,
			Unit:    run.Unit,
		})
	}
	return out
}

func endTime(s *prop.Sampled, c int) int64 {
	if c+1 < len(s.Times) {
		return s.Times[c+1]
	}
	return s.Times[c] + 1
}

// merge coalesces adjacent cycle intervals.
func merge(in []Interval) []Interval {
	var out []Interval
	for _, iv := range in {
		if n := len(out); n > 0 && out[n-1].EndCycle+1 == iv.StartCycle {
			out[n-1].EndCycle = iv.EndCycle
			out[n-1].End = iv.End
			continue
		}
		out = append(out, iv)
	}
	return out
}
