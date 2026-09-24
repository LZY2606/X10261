package replay

import (
	"sort"
	"strconv"

	"gsb/internal/domain"
)

// ResetInterval marks a maximal interval in which the reset signal is at its
// active level. Coordinates are kept both in raw ticks and femtoseconds.
type ResetInterval struct {
	StartTick int64
	EndTick   int64 // inclusive tick of the deasserting change
	StartFS   int64
	EndFS     int64
}

// Sampled holds the preponed-region sampling table for one clock domain.
type Sampled struct {
	Domain    domain.ClockDomain
	Samples   []domain.Sample
	Values    map[string][]domain.Logic // signalID -> value per sample
	ResetInts []ResetInterval
	// changeBySignal keeps ordered changes for provenance/support tracking.
	changeBySignal map[string][]domain.Change
	timescale      domain.Timescale
}

// BuildSampled derives the sampling table from raw changes under fixed
// preponed-region semantics: at an edge tick t, the sampled value is the last
// value with change tick strictly less than t (NBA updates at t are not yet
// visible in the preponed region).
func BuildSampled(cd domain.ClockDomain, sigs []domain.Signal, changes []domain.Change, ts domain.Timescale) *Sampled {
	s := &Sampled{
		Domain:         cd,
		Values:         map[string][]domain.Logic{},
		changeBySignal: map[string][]domain.Change{},
		timescale:      ts,
	}
	bySig := map[string][]domain.Change{}
	for _, ch := range changes {
		bySig[ch.SignalID] = append(bySig[ch.SignalID], ch)
	}
	for id := range bySig {
		sort.SliceStable(bySig[id], func(i, j int) bool {
			if bySig[id][i].AtTick != bySig[id][j].AtTick {
				return bySig[id][i].AtTick < bySig[id][j].AtTick
			}
			return bySig[id][i].Index < bySig[id][j].Index
		})
	}
	s.changeBySignal = bySig

	// Collect every tick at which anything changes, in order.
	tickSet := map[int64]bool{}
	for _, ch := range changes {
		tickSet[ch.AtTick] = true
	}
	ticks := make([]int64, 0, len(tickSet))
	for t := range tickSet {
		ticks = append(ticks, t)
	}
	sort.Slice(ticks, func(i, j int) bool { return ticks[i] < ticks[j] })

	clockID := signalIDByName(sigs, cd.ClockSignal)
	prevClock := domain.LX
	if len(ticks) > 0 {
		if v, ok := valueAtOrBefore(bySig[clockID], ticks[0]); ok {
			prevClock = v
		}
	}
	for _, t := range ticks {
		curClock, _ := valueAtOrBeforeEq(bySig[clockID], t)
		if curClock == domain.LX || curClock == domain.LZ {
			// X/Z clock edge cannot define a sampling point; the edge is
			// consumed into diagnostics by the caller.
			prevClock = curClock
			continue
		}
		isEdge := false
		switch cd.Edge {
		case "posedge":
			isEdge = prevClock == domain.L0 && curClock == domain.L1
		case "negedge":
			isEdge = prevClock == domain.L1 && curClock == domain.L0
		}
		if isEdge {
			idx := len(s.Samples)
			smp := domain.Sample{
				Index:      idx,
				AtTick:     t,
				AtFS:       toFS(t, ts),
				Region:     "preponed",
				Edge:       cd.Edge,
				ClockValue: curClock,
			}
			s.Samples = append(s.Samples, smp)
			for _, sig := range sigs {
				// Preponed: strictly before t.
				v, ok := valueAtStrictBefore(bySig[sig.ID], t)
				if !ok {
					v = domain.LX
				}
				s.Values[sig.ID] = append(s.Values[sig.ID], v)
			}
		}
		prevClock = curClock
	}
	if cd.ResetSignal != "" {
		s.ResetInts = buildResetIntervals(sigs, cd, bySig, ticks, ts)
	}
	return s
}

func buildResetIntervals(sigs []domain.Signal, cd domain.ClockDomain, bySig map[string][]domain.Change, ticks []int64, ts domain.Timescale) []ResetInterval {
	rid := signalIDByName(sigs, cd.ResetSignal)
	var out []ResetInterval
	var start int64
	active := false
	allTicks := append([]int64{0}, ticks...)
	for _, t := range allTicks {
		v, ok := valueAtOrBeforeEq(bySig[rid], t)
		if !ok {
			v = domain.LX
		}
		if v == cd.ResetLevel && !active {
			active = true
			start = t
		} else if v != cd.ResetLevel && active {
			out = append(out, ResetInterval{StartTick: start, EndTick: t, StartFS: toFS(start, ts), EndFS: toFS(t, ts)})
			active = false
		}
	}
	if active {
		last := ticks[len(ticks)-1]
		out = append(out, ResetInterval{StartTick: start, EndTick: last, StartFS: toFS(start, ts), EndFS: toFS(last, ts)})
	}
	return out
}

// InReset reports whether preponed sample i lies inside a reset interval.
func (s *Sampled) InReset(i int) bool {
	if i < 0 || i >= len(s.Samples) {
		return false
	}
	t := s.Samples[i].AtTick
	for _, ri := range s.ResetInts {
		if t >= ri.StartTick && t < ri.EndTick {
			return true
		}
	}
	return false
}

func (s *Sampled) sigID(name string) string {
	// resolved via map built by Engine; helper retained for readability.
	return name
}

func valueAtStrictBefore(changes []domain.Change, tick int64) (domain.Logic, bool) {
	var found domain.Logic
	hit := false
	for _, ch := range changes {
		if ch.AtTick < tick && len(ch.Bits) > 0 {
			found = ch.Bits[0]
			hit = true
		} else if ch.AtTick >= tick {
			break
		}
	}
	return found, hit
}

func valueAtOrBefore(changes []domain.Change, tick int64) (domain.Logic, bool) {
	return valueAtStrictBefore(changes, tick)
}

func valueAtOrBeforeEq(changes []domain.Change, tick int64) (domain.Logic, bool) {
	var found domain.Logic
	hit := false
	for _, ch := range changes {
		if ch.AtTick <= tick && len(ch.Bits) > 0 {
			found = ch.Bits[0]
			hit = true
		} else if ch.AtTick > tick {
			break
		}
	}
	return found, hit
}

func toFS(tick int64, ts domain.Timescale) int64 {
	if !ts.Convertible {
		return 0
	}
	return tick * ts.BaseFS
}

func signalIDByName(sigs []domain.Signal, name string) string {
	for _, sg := range sigs {
		if sg.Name == name {
			return sg.ID
		}
	}
	return name
}

var _ = strconv.Itoa
