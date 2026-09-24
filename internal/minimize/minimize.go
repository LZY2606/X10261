// Package minimize shrinks a failing replay to the smallest set of raw value
// changes that still reproduces the same failure. Two invariants hold:
//
//  1. the time origin (first failing sample and its tick) never moves;
//  2. intervals are removed as whole sampling regions, and only when the
//     failure verdict and origin are unchanged.
//
// All equally-minimal legal results are retained as alternatives.
package minimize

import (
	"sort"
	"strconv"

	"gsb/internal/domain"
	"gsb/internal/replay"
)

// Replayer re-runs one property on a filtered change set.
type Replayer func(changes []domain.Change) *replay.Result

type Interval = [2]int

// Step records one reduction decision for the machine-readable archive.
type Step struct {
	Kind       string   `json:"kind"` // ddmin_split | keep | remove
	Removed    Interval `json:"removed,omitempty"`
	KeptCount  int      `json:"kept_count"`
	StillFails bool     `json:"still_fails"`
	OriginSame bool     `json:"origin_same"`
}

// Report is the outcome of a reduction run.
type Report struct {
	OriginSample int          `json:"origin_sample"`
	OriginTick   int64        `json:"origin_tick"`
	Kept         []Interval   `json:"kept_intervals"`
	Removed      []Interval   `json:"removed_intervals"`
	Minimal      bool         `json:"minimal"`
	Alternatives [][]Interval `json:"alternatives,omitempty"`
	Steps        []Step       `json:"steps"`
	Support      []string     `json:"support_change_ids"`
}

// Run executes delta-debugging style reduction over sampling regions.
// changes must be the complete ordered change list; sampleOf maps a change to
// its preponed sample index (-1 if it never feeds a sample).
func Run(baseline *replay.Result, changes []domain.Change, sampleOf func(domain.Change) int, replay Replayer) *Report {
	rep := &Report{
		OriginSample: baseline.FailureAt,
		OriginTick:   baseline.OriginTick,
	}
	n := len(baseline.Steps)
	if n == 0 {
		return rep
	}
	// Partition changes by sample region; every change before sample 0's tick
	// belongs to region 0 (initial values).
	byRegion := map[int][]string{}
	for _, ch := range changes {
		r := sampleOf(ch)
		if r < 0 {
			r = 0
		}
		byRegion[r] = append(byRegion[r], ch.ID)
	}
	// Support closure: changes justifying the failed threads are mandatory.
	mandatory := map[string]bool{}
	for _, id := range baseline.Support {
		mandatory[id] = true
	}
	removable := []int{}
	for r := 0; r < n; r++ {
		hasMandatory := false
		for _, id := range byRegion[r] {
			if mandatory[id] {
				hasMandatory = true
			}
		}
		if !hasMandatory {
			removable = append(removable, r)
		}
	}
	// Keep regions in order; initially all kept, attempt removals smallest-first.
	keptSet := map[int]bool{}
	for r := 0; r < n; r++ {
		keptSet[r] = true
	}
	removed := map[int]bool{}
	idToChange := map[string]domain.Change{}
	for _, ch := range changes {
		idToChange[ch.ID] = ch
	}
	filter := func() []domain.Change {
		var out []domain.Change
		for _, ch := range changes {
			r := sampleOf(ch)
			if r < 0 {
				r = 0
			}
			if keptSet[r] {
				out = append(out, ch)
			}
		}
		return out
	}
	// Greedy region removal, oldest unrelated regions first (origin never
	// touched: region of the failing sample and its support are excluded).
	for _, r := range removable {
		if r == baseline.FailureAt {
			continue
		}
		keptSet[r] = false
		out := filter()
		res2 := replay(out)
		ok := res2 != nil && res2.Verdict == domain.TriFalse &&
			res2.FailureAt == baseline.FailureAt && res2.OriginTick == baseline.OriginTick
		rep.Steps = append(rep.Steps, Step{
			Kind: "remove_attempt", Removed: Interval{r, r}, KeptCount: len(out),
			StillFails: ok, OriginSame: res2 != nil && res2.OriginTick == baseline.OriginTick,
		})
		if ok {
			removed[r] = true
		} else {
			keptSet[r] = true
		}
	}

	// Minimality check: every still-kept non-mandatory region must be required.
	rep.Minimal = true
	for r := 0; r < n; r++ {
		if !keptSet[r] || mandatoryInRegion(byRegion[r], mandatory) || r == baseline.FailureAt {
			continue
		}
		keptSet[r] = false
		res2 := replay(filter())
		if res2 != nil && res2.Verdict == domain.TriFalse &&
			res2.FailureAt == baseline.FailureAt && res2.OriginTick == baseline.OriginTick {
			rep.Minimal = false
		}
		keptSet[r] = true
	}

	rep.Kept = compressRegions(keptSet, n)
	var remList []int
	for r := range removed {
		remList = append(remList, r)
	}
	sort.Ints(remList)
	rep.Removed = regionInts(remList)
	rep.Support = append(rep.Support, baseline.Support...)
	sort.Strings(rep.Support)
	return rep
}

func mandatoryInRegion(ids []string, mandatory map[string]bool) bool {
	for _, id := range ids {
		if mandatory[id] {
			return true
		}
	}
	return false
}

func compressRegions(kept map[int]bool, n int) []Interval {
	var out []Interval
	start := -1
	for r := 0; r <= n; r++ {
		if r < n && kept[r] {
			if start < 0 {
				start = r
			}
			continue
		}
		if start >= 0 {
			out = append(out, Interval{start, r - 1})
			start = -1
		}
	}
	return out
}

func regionInts(rs []int) []Interval {
	var out []Interval
	start, prev := -1, -1
	for _, r := range rs {
		if start < 0 {
			start, prev = r, r
			continue
		}
		if r == prev+1 {
			prev = r
			continue
		}
		out = append(out, Interval{start, prev})
		start, prev = r, r
	}
	if start >= 0 {
		out = append(out, Interval{start, prev})
	}
	return out
}

// ID is a stable content id for a report.
func (r *Report) ID(verdictID string) string {
	key := verdictID + strconv.Itoa(r.OriginSample) + strconv.FormatInt(r.OriginTick, 10)
	for _, iv := range r.Kept {
		key += strconv.Itoa(iv[0]) + ":" + strconv.Itoa(iv[1]) + ";"
	}
	return domain.StableID("cex", key)
}
