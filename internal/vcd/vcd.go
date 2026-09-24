// Package vcd parses VCD waveforms while preserving the raw source
// representation: original bytes, the declared timescale precision and
// every value change are kept verbatim. Normalization (e.g. converting
// all timestamps to a common unit) happens only in the derived layer.
package vcd

import (
	"fmt"
	"sort"
	"strings"

	"pairwise-gsb/internal/logic"
)

// Timescale records the raw timescale declaration exactly as written.
type Timescale struct {
	Raw       string // e.g. "1ns" or "10ps"
	Number    int64  // 1, 10, 100
	Unit      string // "s","ms","us","ns","ps","fs"
	ScaleToFs int64  // femtoseconds per tick; 0 if unit unknown
}

var unitFs = map[string]int64{
	"s":  1_000_000_000_000_000,
	"ms": 1_000_000_000_000,
	"us": 1_000_000_000,
	"ns": 1_000_000,
	"ps": 1_000,
	"fs": 1,
}

// Change is one value change on one signal at an integer tick.
type Change struct {
	Tick   int64
	Signal string
	Value  logic.V
}

// Signal describes one dumped variable.
type Signal struct {
	Name string
	ID   string // VCD identifier code
}

// Waveform is the parsed, source-faithful waveform.
type Waveform struct {
	Raw       []byte
	Timescale Timescale
	Signals   []Signal
	Changes   []Change // sorted by tick, then declaration order
	EndTick   int64
}

// Parse parses VCD text. Scalar signals only (the case-pack language);
// vector values are rejected with a diagnostic rather than truncated.
func Parse(raw []byte) (*Waveform, error) {
	w := &Waveform{Raw: append([]byte(nil), raw...)}
	ids := map[string]string{} // id code -> signal name
	var order []string
	lines := strings.Split(string(raw), "\n")
	inHeader := true
	var tick int64
	for ln, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if inHeader {
			switch {
			case strings.HasPrefix(line, "$timescale"):
				rest := strings.TrimSpace(strings.TrimPrefix(line, "$timescale"))
				rest = strings.TrimSuffix(rest, "$end")
				rest = strings.TrimSpace(rest)
				ts, err := parseTimescale(rest)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", ln+1, err)
				}
				w.Timescale = ts
			case strings.HasPrefix(line, "$var"):
				f := strings.Fields(line)
				// $var wire 1 <id> <name> $end
				if len(f) < 6 {
					return nil, fmt.Errorf("line %d: malformed $var", ln+1)
				}
				if f[2] != "1" {
					return nil, fmt.Errorf("line %d: only scalar signals supported, got width %s", ln+1, f[2])
				}
				id, name := f[3], f[4]
				ids[id] = name
				order = append(order, id)
				w.Signals = append(w.Signals, Signal{Name: name, ID: id})
			case strings.HasPrefix(line, "$enddefinitions"):
				inHeader = false
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			var t int64
			if _, err := fmt.Sscanf(line[1:], "%d", &t); err != nil {
				return nil, fmt.Errorf("line %d: bad timestamp %q", ln+1, line)
			}
			tick = t
			if t > w.EndTick {
				w.EndTick = t
			}
			continue
		}
		if strings.HasPrefix(line, "$") {
			continue
		}
		// scalar value change: <v><id>
		v, ok := logic.ParseV(line[0])
		if !ok {
			return nil, fmt.Errorf("line %d: unsupported value change %q", ln+1, line)
		}
		id := line[1:]
		name, ok := ids[id]
		if !ok {
			return nil, fmt.Errorf("line %d: change for undeclared id %q", ln+1, id)
		}
		w.Changes = append(w.Changes, Change{Tick: tick, Signal: name, Value: v})
	}
	if w.Timescale.Raw == "" {
		return nil, fmt.Errorf("missing $timescale declaration")
	}
	_ = order
	sort.SliceStable(w.Changes, func(i, j int) bool { return w.Changes[i].Tick < w.Changes[j].Tick })
	return w, nil
}

func parseTimescale(s string) (Timescale, error) {
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return Timescale{}, fmt.Errorf("empty timescale")
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return Timescale{}, fmt.Errorf("bad timescale %q", s)
	}
	var n int64
	fmt.Sscanf(s[:i], "%d", &n)
	unit := s[i:]
	scale, ok := unitFs[unit]
	if !ok {
		// Keep the raw declaration, mark scale as unconvertible.
		return Timescale{Raw: s, Number: n, Unit: unit, ScaleToFs: 0}, nil
	}
	return Timescale{Raw: s, Number: n, Unit: unit, ScaleToFs: n * scale}, nil
}

// ValueAt returns the four-state value of sig at tick (last change <= tick).
func (w *Waveform) ValueAt(sig string, tick int64) logic.V {
	val := logic.VX
	for _, c := range w.Changes {
		if c.Tick > tick {
			break
		}
		if c.Signal == sig {
			val = c.Value
		}
	}
	return val
}

// Edge is a sampled clock edge in tick coordinates.
type Edge struct {
	Tick int64
	From logic.V
	To   logic.V
}

// Edges returns posedge (0->1) transitions of the named clock signal.
func (w *Waveform) Edges(clock string) []Edge {
	var edges []Edge
	prev := logic.VX
	for _, c := range w.Changes {
		if c.Signal != clock {
			continue
		}
		if prev == logic.V0 && c.Value == logic.V1 {
			edges = append(edges, Edge{Tick: c.Tick, From: prev, To: c.Value})
		}
		prev = c.Value
	}
	return edges
}

// Interval is a closed tick interval [Start, End].
type Interval struct {
	Start int64
	End   int64
}

// ResetIntervals derives intervals where the reset signal is actively 1.
func (w *Waveform) ResetIntervals(reset string) []Interval {
	var out []Interval
	active := false
	var start int64
	prev := logic.VX
	for _, c := range w.Changes {
		if c.Signal != reset {
			continue
		}
		if !active && c.Value == logic.V1 {
			active, start = true, c.Tick
		}
		if active && c.Value != logic.V1 {
			active = false
			out = append(out, Interval{Start: start, End: c.Tick})
		}
		prev = c.Value
	}
	_ = prev
	if active {
		out = append(out, Interval{Start: start, End: w.EndTick})
	}
	return out
}
