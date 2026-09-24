// Package replay executes assertions over a VCD under fixed sampled
// semantics. Values are read from a preponed sampling region (the value
// effective immediately before the clock edge), four-state unknowns are
// explicit outcomes, and every judgement emits machine-readable basis events.
package replay

import (
	"fmt"

	"signalcourt/internal/fourval"
	"signalcourt/internal/property"
	"signalcourt/internal/vcd"
)

// ClockDomain fixes the sampling grid for a replay.
type ClockDomain struct {
	ClockName  string `json:"clock_name"`
	Edge       string `json:"edge"` // "posedge" or "negedge"
	ResetName  string `json:"reset_name,omitempty"`
	ResetLevel string `json:"reset_level,omitempty"` // "1" active high, "0" active low
	// RawCoordBasis records the coordinate frame as imported; derived
	// nanosecond coordinates live alongside, never replacing it.
	CoordBasis string `json:"coord_basis"`
}

func (c ClockDomain) Validate() error {
	if c.ClockName == "" {
		return fmt.Errorf("clock name required")
	}
	if c.Edge != "posedge" && c.Edge != "negedge" {
		return fmt.Errorf("edge must be posedge/negedge, got %q", c.Edge)
	}
	if c.ResetName != "" && c.ResetLevel != "0" && c.ResetLevel != "1" {
		return fmt.Errorf("reset level must be 0 or 1, got %q", c.ResetLevel)
	}
	return nil
}

// Tick is one sampling point on the clock grid.
type Tick struct {
	Index          int     `json:"index"`            // clock-edge ordinal, the time origin anchor
	WaveTick       int     `json:"wave_tick"`        // index into vcd.TimeMarkers of the edge
	TimeRaw        int64   `json:"time_raw"`         // declared timescale number at the edge
	Nanos          float64 `json:"nanos"`            // derived coordinate; -1 if unconvertible
	SampledRegion  string  `json:"sampled_region"`   // "preponed"
	SampleWaveTick int     `json:"sample_wave_tick"` // wave tick the sampled values are read from
	SampleTimeRaw  int64   `json:"sample_time_raw"`
}

// Grid builds the clock-edge tick list. Edges through X/Z are reported
// separately rather than guessed.
type Grid struct {
	Ticks         []Tick   `json:"ticks"`
	UnknownEdges  []int    `json:"unknown_edge_wave_ticks"`
	Diagnostics   []string `json:"diagnostics"`
	TimescaleUnit string   `json:"timescale_unit"`
	NanosPerUnit  float64  `json:"nanos_per_unit"`
}

// BuildGrid constructs sampling ticks from the waveform and clock domain.
func BuildGrid(w *vcd.Waveform, cd ClockDomain) (*Grid, error) {
	if err := cd.Validate(); err != nil {
		return nil, err
	}
	clk, ok := w.SignalByName(cd.ClockName)
	if !ok {
		return nil, fmt.Errorf("clock signal %q not found in waveform", cd.ClockName)
	}
	if cd.ResetName != "" {
		if _, ok := w.SignalByName(cd.ResetName); !ok {
			return nil, fmt.Errorf("reset signal %q not found in waveform", cd.ResetName)
		}
	}
	series := w.BuildSeries()
	clkSeries := series[clk.ID]
	rising, falling, unk := clkSeries.EdgesAt()
	var edges []int
	switch cd.Edge {
	case "posedge":
		edges = rising
	case "negedge":
		edges = falling
	}
	g := &Grid{
		UnknownEdges:  unk,
		NanosPerUnit:  w.Timescale.NanosPerUnit,
		TimescaleUnit: w.Timescale.Unit,
	}
	for i, wt := range edges {
		nanos := -1.0
		if w.Timescale.NanosPerUnit >= 0 {
			nanos = float64(w.TimeMarkers[wt]) * w.Timescale.NanosPerUnit
		}
		// Preponed sample: value effective just before the edge = value at
		// the previous time marker; x before the first marker (no guesses).
		swt := wt - 1
		var stime int64 = -1
		if swt >= 0 {
			stime = w.TimeMarkers[swt]
		}
		g.Ticks = append(g.Ticks, Tick{
			Index: i, WaveTick: wt, TimeRaw: w.TimeMarkers[wt], Nanos: nanos,
			SampledRegion: "preponed", SampleWaveTick: swt, SampleTimeRaw: stime,
		})
	}
	if len(g.Ticks) == 0 {
		g.Diagnostics = append(g.Diagnostics,
			fmt.Sprintf("no %s edges on clock %q", cd.Edge, cd.ClockName))
	}
	return g, nil
}

// Sampler reads preponed four-state words at grid ticks.
type Sampler struct {
	w      *vcd.Waveform
	series map[string]*vcd.Series
	byName map[string]string // signal name -> id
	g      *Grid
}

// NewSampler builds a sampler over the grid.
func NewSampler(w *vcd.Waveform, g *Grid) *Sampler {
	return &Sampler{
		w:      w,
		series: w.BuildSeries(),
		byName: nameMap(w),
		g:      g,
	}
}

func nameMap(w *vcd.Waveform) map[string]string {
	m := map[string]string{}
	for _, s := range w.Signals {
		m[s.Name] = s.ID
	}
	return m
}

// ValueAt returns the raw sampled word of a signal at clock tick ti.
// Unknown signals / first preponed region read as explicit "x".
func (s *Sampler) ValueAt(name string, ti int) fourval.Word {
	id, ok := s.byName[name]
	if !ok || ti < 0 || ti >= len(s.g.Ticks) {
		return fourval.Word{Raw: "x"}
	}
	swt := s.g.Ticks[ti].SampleWaveTick
	if swt < 0 {
		return fourval.Word{Raw: "x"}
	}
	v := s.series[id].Values[swt]
	if v == "" {
		v = "x"
	}
	return fourval.Word{Raw: v}
}

// EvalExpr evaluates a boolean expression at a tick to an explicit tri-state.
func (s *Sampler) EvalExpr(e property.Expr, ti int) fourval.Tribool {
	switch t := e.(type) {
	case *property.IdentExpr:
		return s.ValueAt(t.Name, ti).Bool()
	case *property.NotExpr:
		return fourval.NotT(s.EvalExpr(t.Inner, ti))
	case *property.BinExpr:
		l := s.EvalExpr(t.Lhs, ti)
		r := s.EvalExpr(t.Rhs, ti)
		if t.Op == "&&" {
			return fourval.AndT(l, r)
		}
		return fourval.OrT(l, r)
	}
	return fourval.Unknown("unreachable")
}

// ResetIntervals returns contiguous clock-tick intervals [start,end] where
// the preponed reset value is at its active level. X/Z boundaries are
// emitted as separate unknown intervals (both bounds inclusive tick indices).
type ResetSpan struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Kind   string `json:"kind"` // "active" or "unknown"
	Reason string `json:"reason,omitempty"`
}

func (s *Sampler) ResetIntervals(cd ClockDomain) []ResetSpan {
	if cd.ResetName == "" {
		return nil
	}
	var spans []ResetSpan
	n := len(s.g.Ticks)
	classAt := func(ti int) string {
		v := s.ValueAt(cd.ResetName, ti).Raw
		switch {
		case v == cd.ResetLevel:
			return "active"
		case v == "0" || v == "1":
			return "inactive"
		default:
			return "unknown"
		}
	}
	for ti := 0; ti < n; {
		c := classAt(ti)
		if c == "inactive" {
			ti++
			continue
		}
		start := ti
		for ti < n && classAt(ti) == c {
			ti++
		}
		sp := ResetSpan{Start: start, End: ti - 1, Kind: c}
		if c == "unknown" {
			sp.Reason = "reset reads " + s.ValueAt(cd.ResetName, start).Raw
		}
		spans = append(spans, sp)
	}
	return spans
}
