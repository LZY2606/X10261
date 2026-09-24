package vcd

import "sort"

// Series holds the stepwise raw value of one signal over time-marker ticks:
// Values[tick] is the value effective at that tick (last change at or before
// the marker). Initial entries before the first change are "x".
type Series struct {
	Sig    Signal   `json:"sig"`
	Values []string `json:"values"` // one raw word per time marker, length = ticks
}

// BuildSeries materializes per-tick values in declared order. Ticks with no
// change inherit the previous value; before the first change the signal is
// unknown "x" (no power-up guessing).
func (w *Waveform) BuildSeries() map[string]*Series {
	ticks := len(w.TimeMarkers)
	out := map[string]*Series{}
	for _, s := range w.Signals {
		out[s.ID] = &Series{Sig: s, Values: make([]string, ticks)}
	}
	// Changes are in source order; apply sequentially.
	for _, ch := range w.Changes {
		if ser := out[ch.SigID]; ser != nil && ch.AtTick >= 0 && ch.AtTick < ticks {
			// Every cell from this tick until the next change is filled by
			// replay below; just record here, then sweep.
			_ = ser
		}
	}
	// Sweep per signal.
	for _, s := range w.Signals {
		ser := out[s.ID]
		cur := "x"
		// group changes by tick
		next := make(map[int]string)
		for _, ch := range w.Changes {
			if ch.SigID == s.ID && ch.AtTick >= 0 && ch.AtTick < ticks {
				next[ch.AtTick] = ch.Value
			}
		}
		for t := 0; t < ticks; t++ {
			if v, ok := next[t]; ok {
				cur = v
			}
			ser.Values[t] = cur
		}
	}
	return out
}

// EdgesAt returns tick ordinals where the signal transitions 0->1 (rising)
// or 1->0 (falling). X/Z boundaries are returned separately so clock edges
// are never inferred through unknown values.
func (ser *Series) EdgesAt() (rising, falling, unknownBoundary []int) {
	prev := ""
	for t, v := range ser.Values {
		if prev != "" {
			switch {
			case prev == "0" && v == "1":
				rising = append(rising, t)
			case prev == "1" && v == "0":
				falling = append(falling, t)
			case (prev == "1" || prev == "0") != (v == "1" || v == "0") &&
				((prev == "1" || prev == "0") || (v == "1" || v == "0")):
				unknownBoundary = append(unknownBoundary, t)
			}
		}
		prev = v
	}
	return
}

// SortedIDs returns signal IDs in declaration order convenience.
func (w *Waveform) SortedIDs() []string {
	ids := make([]string, 0, len(w.Signals))
	for _, s := range w.Signals {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return ids
}
