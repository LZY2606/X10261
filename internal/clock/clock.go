// Package clock builds clock domains over imported waveforms: the edge
// tick grid, per-tick sampling regions and sampled four-state values.
//
// The sampling region is shown explicitly at every evaluation step.
// Semantic versions differ only in the sampling region convention:
//
//	v1.0.0: region (prevEdge, edge], value = last change with tick <= edge
//	v1.1.0: region [prevEdge, edge) plus a one-tick prepone, value = last
//	        change with tick < edge (strict); at-edge changes are seen next
//
// Every region carries both raw tick coordinates and a normalized fs basis
// (0 fs with a diagnostic on the timescale when normalization is impossible).
package clock

import (
	"fmt"
	"sort"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/timescale"
	"signalcourt/internal/vcd"
)

// Edge selects the driving edge.
type Edge string

const (
	Posedge Edge = "posedge"
	negedge Edge = "negedge"
)

// Region is a sampling region with raw tick coordinates and fs basis.
type Region struct {
	Index       int    `json:"index"`
	EdgeTick    int64  `json:"edge_tick"`
	StartTick   int64  `json:"start_tick"`
	EndTick     int64  `json:"end_tick"`
	SampleTick  int64  `json:"sample_tick"`
	EdgeFS      int64  `json:"edge_fs"`
	StartFS     int64  `json:"start_fs"`
	EndFS       int64  `json:"end_fs"`
	Basis       string `json:"basis"`
	Strict      bool   `json:"strict"`
	Unit        string `json:"unit"`
	ScaleOK     bool   `json:"scale_ok"`
}

// Domain is a resolved clock domain over a waveform.
type Domain struct {
	Name      string             `json:"name"`
	ClockName string             `json:"clock_name"`
	Edge      Edge               `json:"edge"`
	Scale     timescale.Scale    `json:"scale"`
	edgeTicks []int64
	traces    map[string][]tracePoint
	nameToID  map[string]string
}

type tracePoint struct {
	tick   int64
	val    fourstate.V4
	offset int64
}

// Negedge is the falling driving edge.
const Negedge = negedge

// Build constructs a domain by extracting the clock edge grid.
func Build(name, clockName string, edge Edge, w *vcd.Waveform) (*Domain, error) {
	sig, ok := w.Signal(clockName)
	if !ok {
		return nil, fmt.Errorf("clock signal %q not found in VCD", clockName)
	}
	d := &Domain{
		Name:      name,
		ClockName: clockName,
		Edge:      edge,
		Scale:     w.Timescale,
		traces:    map[string][]tracePoint{},
		nameToID:  map[string]string{},
	}
	for _, s := range w.Signals {
		d.nameToID[s.Name] = s.ID
	}
	bySignal := map[string][]tracePoint{}
	changes := append([]vcd.Change(nil), w.Changes...)
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Tick != changes[j].Tick {
			return changes[i].Tick < changes[j].Tick
		}
		return changes[i].Offset < changes[j].Offset
	})
	for _, c := range changes {
		if !c.OK {
			continue
		}
		bySignal[c.SignalID] = append(bySignal[c.SignalID], tracePoint{c.Tick, c.Value, c.Offset})
	}
	for id, rows := range bySignal {
		dedup := rows[:0]
		for _, p := range rows {
			if len(dedup) > 0 && dedup[len(dedup)-1].tick == p.tick {
				dedup[len(dedup)-1] = p
				continue
			}
			dedup = append(dedup, p)
		}
		d.traces[id] = dedup
	}
	prev := fourstate.VX
	for _, p := range d.traces[sig.ID] {
		hit := false
		switch edge {
		case Posedge:
			hit = prev == fourstate.V0 && p.val == fourstate.V1
		case Negedge:
			hit = prev == fourstate.V1 && p.val == fourstate.V0
		}
		if hit {
			d.edgeTicks = append(d.edgeTicks, p.tick)
		}
		prev = p.val
	}
	if len(d.edgeTicks) == 0 {
		return nil, fmt.Errorf("clock %q produced no %s edges", clockName, edge)
	}
	return d, nil
}

// Ticks returns the driving edge tick coordinates.
func (d *Domain) Ticks() []int64 { return append([]int64(nil), d.edgeTicks...) }

// NEdges returns the number of driving edges.
func (d *Domain) NEdges() int { return len(d.edgeTicks) }

// RegionAt returns the sampling region for edge index k under a semantic
// version. k=0 starts at the waveform origin 0.
func (d *Domain) RegionAt(k int, semver string) Region {
	edge := d.edgeTicks[k]
	prev := int64(0)
	if k > 0 {
		prev = d.edgeTicks[k-1]
	}
	strict := semver == "v1.1.0"
	r := Region{
		Index: k, EdgeTick: edge, StartTick: prev, EndTick: edge,
		EdgeFS: d.fs(edge), StartFS: d.fs(prev), EndFS: d.fs(edge),
		Basis: "fs", Strict: strict, Unit: d.Scale.Unit, ScaleOK: d.Scale.OK,
	}
	if strict {
		r.SampleTick = edge - 1
	} else {
		r.SampleTick = edge
	}
	return r
}

func (d *Domain) fs(tick int64) int64 {
	if !d.Scale.OK {
		return 0
	}
	return tick * d.Scale.FSPerTick
}

// Sample returns the sampled four-state value at edge k. The second result
// is false when no change has occurred yet (value unknown, never forced 0).
func (d *Domain) Sample(signalName, semver string, k int) (fourstate.V4, bool) {
	id, ok := d.nameToID[signalName]
	if !ok {
		id = signalName
	}
	r := d.RegionAt(k, semver)
	rows := d.traces[id]
	idx := sort.Search(len(rows), func(i int) bool { return rows[i].tick > r.SampleTick }) - 1
	if idx < 0 {
		return fourstate.VX, false
	}
	return rows[idx].val, true
}

// ChangeTick returns the raw tick of the change that the sample reads.
func (d *Domain) ChangeTick(signalName, semver string, k int) (int64, bool) {
	id, ok := d.nameToID[signalName]
	if !ok {
		id = signalName
	}
	r := d.RegionAt(k, semver)
	rows := d.traces[id]
	idx := sort.Search(len(rows), func(i int) bool { return rows[i].tick > r.SampleTick }) - 1
	if idx < 0 {
		return 0, false
	}
	return rows[idx].tick, true
}
