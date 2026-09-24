// Package wave models waveforms with explicit coordinate basis (timescale),
// clock edges, reset intervals and preponed-region sampled reads.
package wave

import (
	"fmt"
	"sort"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/vcd"
)

// Timescale is the coordinate basis of a waveform. RawNum/RawUnit keep the
// original representation; FemtoPerTick is the derived normalization
// (femtoseconds per tick). Unconvertible units leave FemtoPerTick=0 and a
// diagnostic is attached instead of dropping the value.
type Timescale struct {
	RawNum       int64
	RawUnit      string
	FemtoPerTick int64
}

var unitFemto = map[string]int64{
	"s":  1_000_000_000_000_000,
	"ms": 1_000_000_000_000,
	"us": 1_000_000_000,
	"ns": 1_000_000,
	"ps": 1_000,
	"fs": 1,
}

// Normalize converts the raw timescale to femtoseconds per tick.
func NormalizeTimescale(num int64, unit string) (Timescale, error) {
	mult, ok := unitFemto[unit]
	if !ok {
		return Timescale{RawNum: num, RawUnit: unit},
			fmt.Errorf("cannot convert timescale unit %q to femtoseconds", unit)
	}
	return Timescale{RawNum: num, RawUnit: unit, FemtoPerTick: num * mult}, nil
}

// Signal is one named waveform channel.
type Signal struct {
	Name    string
	Width   int
	Changes []vcd.Change // sorted by Time, raw ticks
}

// Waveform is a set of signals sharing one coordinate basis.
type Waveform struct {
	Timescale Timescale
	Signals   map[string]*Signal
	Order     []string // deterministic signal order
}

// FromVCD builds a Waveform from a parsed VCD file. Raw values and ticks are
// preserved; normalization happens only in derived accessors.
func FromVCD(f *vcd.File) (*Waveform, []string) {
	var diags []string
	ts, err := NormalizeTimescale(f.TimescaleNum, f.TimescaleUnit)
	if err != nil {
		diags = append(diags, err.Error())
	}
	w := &Waveform{Timescale: ts, Signals: map[string]*Signal{}}
	for _, s := range f.Signals {
		w.Signals[s.Name] = &Signal{Name: s.Name, Width: s.Width}
		w.Order = append(w.Order, s.Name)
	}
	sort.Strings(w.Order)
	for _, c := range f.Changes {
		var target *Signal
		for _, s := range f.Signals {
			if s.Code == c.Code {
				target = w.Signals[s.Name]
				break
			}
		}
		if target == nil {
			continue
		}
		if !fourstate.Valid(c.Value) {
			diags = append(diags, fmt.Sprintf("signal %s @%d: unconvertible value %q kept in place", target.Name, c.Time, c.Value))
		}
		target.Changes = append(target.Changes, c)
	}
	return w, diags
}

// ValueAt returns the four-state value of sig at raw time t (last change at
// or before t). Returns "" if the signal is unknown to the waveform.
func (w *Waveform) ValueAt(sig string, t int64) string {
	s := w.Signals[sig]
	if s == nil {
		return ""
	}
	idx := sort.Search(len(s.Changes), func(i int) bool { return s.Changes[i].Time > t }) - 1
	if idx < 0 {
		return "x"
	}
	return s.Changes[idx].Value
}

// SampledValue implements the fixed preponed-region sampling semantics:
// the value sampled at a clock edge at time t is the value strictly before t.
func (w *Waveform) SampledValue(sig string, edgeTime int64) string {
	s := w.Signals[sig]
	if s == nil {
		return ""
	}
	idx := sort.Search(len(s.Changes), func(i int) bool { return s.Changes[i].Time >= edgeTime }) - 1
	if idx < 0 {
		return "x"
	}
	return s.Changes[idx].Value
}

// Edge identifies a clock edge.
type Edge struct {
	Index int
	Time  int64 // raw ticks
}

// Edges returns the times of the requested edges ("posedge"/"negedge") of clk.
func (w *Waveform) Edges(clk, kind string) []Edge {
	s := w.Signals[clk]
	if s == nil {
		return nil
	}
	var out []Edge
	prev := "x"
	for _, c := range s.Changes {
		if kind == "posedge" && prev != "1" && c.Value == "1" {
			out = append(out, Edge{Index: len(out), Time: c.Time})
		}
		if kind == "negedge" && prev != "0" && c.Value == "0" {
			out = append(out, Edge{Index: len(out), Time: c.Time})
		}
		prev = c.Value
	}
	return out
}

// Interval is a half-open raw-tick interval [Start, End) with its basis.
type Interval struct {
	Start int64
	End   int64
}

// ResetIntervals returns intervals where the reset signal is active.
// polarity "low" means active when sampled value is 0.
func (w *Waveform) ResetIntervals(reset, polarity string) []Interval {
	s := w.Signals[reset]
	if s == nil {
		return nil
	}
	active := func(v string) bool {
		if polarity == "low" {
			return v == "0"
		}
		return v == "1"
	}
	var out []Interval
	var cur *Interval
	for _, c := range s.Changes {
		if active(c.Value) {
			if cur == nil {
				cur = &Interval{Start: c.Time}
			}
		} else if cur != nil {
			cur.End = c.Time
			out = append(out, *cur)
			cur = nil
		}
	}
	if cur != nil {
		cur.End = w.End() + 1
		out = append(out, *cur)
	}
	return out
}

// End returns the last timestamp present in the waveform.
func (w *Waveform) End() int64 {
	var max int64
	for _, s := range w.Signals {
		if n := len(s.Changes); n > 0 && s.Changes[n-1].Time > max {
			max = s.Changes[n-1].Time
		}
	}
	return max
}
