// Package minimize shrinks a failing trace to the smallest set of sampling
// regions that still produces the same failure. It never moves the time
// origin: tick 0 and all retained timestamps keep their original coordinates,
// only intervals irrelevant to the failure are dropped (shown as gaps).
package minimize

import (
	"sort"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/prop"
	"signalcourt/internal/replay"
)

// Reduced is one legal minimum counterexample.
type Reduced struct {
	ID         string `json:"id"`
	OriginTick int    `json:"origin_tick"`
	Keep       []int  `json:"keep"` // ticks retained, ascending
	Gaps       []Gap  `json:"gaps"` // intervals removed
	FailedAt   int    `json:"failed_at"`
	Property   string `json:"property"`
	StartTick  int    `json:"start_tick"`
	SemVer     string `json:"semver"`
}

// Gap marks a contiguous run of removed sampling regions.
type Gap struct {
	FromTick int `json:"from_tick"`
	ToTick   int `json:"to_tick"`
}

// Replay is the subset of replay.Run used here, kept as an interface so tests
// can substitute deterministic engines.
type Replay func(props []*prop.Property, ticks []replay.Tick, cfg replay.Config) *replay.Result

// Minimize returns all distinct equally-minimal subsets of the attempt's
// support that still reproduce the same failure. Origin tick 0 is always
// retained. Candidates that shrink equally are all kept; adjudication picks
// one without discarding the others.
func Minimize(r *replay.Result, attemptID string, props []*prop.Property, run Replay) []Reduced {
	var att *replay.Attempt
	var p *prop.Property
	for _, a := range r.Attempts {
		if a.ID == attemptID {
			att = a
		}
	}
	for _, pp := range props {
		if pp.Name == att.Property {
			p = pp
		}
	}
	if att == nil || att.Kind != replay.KFail {
		return nil
	}
	// mandatory: origin and the antecedent/failing sampling regions
	must := map[int]bool{0: true, att.StartTick: true, att.EndTick: true}
	var optional []int
	for _, k := range att.Support {
		if !must[k] {
			optional = append(optional, k)
		}
	}
	base := []replay.Tick{}
	keepSet := func(extra []int) map[int]bool {
		m := map[int]bool{}
		for k := range must {
			m[k] = true
		}
		for _, k := range extra {
			m[k] = true
		}
		return m
	}
	subTicks := func(set map[int]bool) []replay.Tick {
		var out []replay.Tick
		for _, tk := range r.Ticks {
			if set[tk.Index] {
				cp := tk
				cp.Index = len(out)
				out = append(out, cp)
			}
		}
		return out
	}
	_ = base
	// The failure only reads the support ticks. Any other sampling regions
	// are irrelevant and are removed first (greedy monotone reduction), then
	// we enumerate ties among the support points (small trace => exact).
	stillFails := func(set map[int]bool) bool {
		sub := subTicks(set)
		rr := run([]*prop.Property{p}, sub, r.Config)
		for _, a := range rr.Attempts {
			if a.Kind == replay.KFail {
				return true
			}
		}
		return false
	}
	if !stillFails(keepSet(optional)) {
		return nil // should not happen for a genuine fail, stay conservative
	}
	// exhaustive over optional support points (<= small N in the case pack)
	best := -1
	var winners [][]int
	for mask := 0; mask < 1<<len(optional); mask++ {
		var ex []int
		for i := range optional {
			if mask&(1<<i) != 0 {
				ex = append(ex, optional[i])
			}
		}
		set := keepSet(ex)
		if stillFails(set) {
			n := len(set)
			if best == -1 || n < best {
				best, winners = n, [][]int{ex}
			} else if n == best {
				winners = append(winners, ex)
			}
		}
	}
	var out []Reduced
	seen := map[string]bool{}
	for ci, ex := range winners {
		set := keepSet(ex)
		var keep []int
		for k := range set {
			keep = append(keep, k)
		}
		sort.Ints(keep)
		key := fmtKey(keep)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Reduced{
			ID:         "ce_" + att.Property + "_" + key + "_c" + itoa(ci),
			OriginTick: 0, Keep: keep,
			FailedAt: att.EndTick, Property: att.Property,
			StartTick: att.StartTick, SemVer: att.SemVer,
			Gaps: gaps(keep, len(r.Ticks)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func gaps(keep []int, n int) []Gap {
	set := map[int]bool{}
	for _, k := range keep {
		set[k] = true
	}
	var gs []Gap
	for i := 0; i < n; {
		if set[i] {
			i++
			continue
		}
		j := i
		for j < n && !set[j] {
			j++
		}
		gs = append(gs, Gap{FromTick: i, ToTick: j - 1})
		i = j
	}
	return gs
}

func fmtKey(keep []int) string {
	b := make([]byte, 0, len(keep)*2)
	for _, k := range keep {
		b = append(b, 'k')
		b = append(b, []byte(itoa(k))...)
		b = append(b, '_')
	}
	return string(b)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ValuesAt is a small helper for the UI to show which four-state value a
// retained tick carries for a signal.
func ValuesAt(tk replay.Tick, signal string) (fourstate.Value, string) {
	return tk.Values[signal], tk.RawTokens[signal]
}
