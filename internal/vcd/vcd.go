// Package vcd parses Value Change Dump files. The raw representation
// (original timescale text, original time stamps, original four-state
// value strings) is always preserved; normalization to a common unit
// happens only in the derived layer, and values that cannot be converted
// stay in place with a diagnostic attached.
package vcd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"signalcourt/internal/fourstate"
)

// Timescale keeps the raw declaration plus its parsed parts.
type Timescale struct {
	Raw    string // e.g. "1ps" exactly as declared
	Number int64
	Unit   string // "s","ms","us","ns","ps","fs" or anything else
}

// Signal is one declared VCD variable.
type Signal struct {
	ID    string
	Name  string
	Width int
}

// Change is one raw value change: time is in declared timescale units,
// RawValue is the exact token from the file.
type Change struct {
	Time     uint64
	Signal   string // signal name (resolved from id code)
	RawValue string
	Value    fourstate.Vector
}

// Diagnostic marks a value that could not be normalized; it stays in
// place in raw form.
type Diagnostic struct {
	Where  string
	Reason string
	Raw    string
}

type File struct {
	Timescale   Timescale
	Signals     []Signal
	Changes     []Change
	Diagnostics []Diagnostic
}

// unitToFs maps a time unit to its length in femtoseconds.
var unitToFs = map[string]uint64{
	"s":  1_000_000_000_000_000,
	"ms": 1_000_000_000_000,
	"us": 1_000_000_000,
	"ns": 1_000_000,
	"ps": 1_000,
	"fs": 1,
}

// UnitToFs exposes the multiplier; ok=false means the unit is unknown and
// the value must be left raw with a diagnostic.
func UnitToFs(unit string) (uint64, bool) {
	m, ok := unitToFs[unit]
	return m, ok
}

// ToFemtoseconds converts a raw timestamp using the file timescale.
func (f *File) ToFemtoseconds(t uint64) (uint64, bool) {
	m, ok := unitToFs[f.Timescale.Unit]
	if !ok {
		return 0, false
	}
	return t * m * uint64(f.Timescale.Number), true
}

// Parse reads a VCD file. It never fails on unknown value tokens: the raw
// token is preserved and a diagnostic is recorded.
func Parse(text string) (*File, error) {
	f := &File{}
	ids := map[string]*Signal{}
	lines := strings.Split(text, "\n")
	i := 0
	// header section
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(line, "$timescale"):
			raw := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "$timescale"), "$end"))
			for raw == "" && i+1 < len(lines) {
				i++
				raw = strings.TrimSpace(strings.TrimSuffix(lines[i], "$end"))
			}
			ts, err := parseTimescale(raw)
			if err != nil {
				f.Diagnostics = append(f.Diagnostics, Diagnostic{Where: "timescale", Reason: err.Error(), Raw: raw})
			} else {
				f.Timescale = ts
			}
		case strings.HasPrefix(line, "$var"):
			// $var wire 1 ! sig_name $end
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				w, _ := strconv.Atoi(fields[2])
				sig := Signal{ID: fields[3], Name: fields[4], Width: w}
				f.Signals = append(f.Signals, sig)
				ids[sig.ID] = &f.Signals[len(f.Signals)-1]
			}
		case strings.HasPrefix(line, "$enddefinitions"):
			i++
			goto body
		}
	}
body:
	var cur uint64
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		switch line[0] {
		case '#':
			n, err := strconv.ParseUint(line[1:], 10, 64)
			if err != nil {
				f.Diagnostics = append(f.Diagnostics, Diagnostic{Where: "timestamp", Reason: "unparseable timestamp", Raw: line})
				continue
			}
			cur = n
		case '0', '1', 'x', 'X', 'z', 'Z':
			if len(line) < 2 {
				continue
			}
			f.addChange(ids, cur, line[1:], line[:1])
		case 'b', 'B':
			parts := strings.Fields(line)
			if len(parts) != 2 {
				f.Diagnostics = append(f.Diagnostics, Diagnostic{Where: "value", Reason: "malformed vector change", Raw: line})
				continue
			}
			f.addChange(ids, cur, parts[1], strings.ToLower(parts[0][1:]))
		case '$':
			// $dumpvars etc.: ignore section keywords, their lines are
			// ordinary value changes handled above.
		default:
			f.Diagnostics = append(f.Diagnostics, Diagnostic{Where: "line", Reason: "unrecognized record", Raw: line})
		}
	}
	sort.SliceStable(f.Signals, func(a, b int) bool { return f.Signals[a].Name < f.Signals[b].Name })
	return f, nil
}

func (f *File) addChange(ids map[string]*Signal, t uint64, id, rawval string) {
	sig, ok := ids[id]
	if !ok {
		f.Diagnostics = append(f.Diagnostics, Diagnostic{Where: "value", Reason: "change for undeclared id", Raw: fmt.Sprintf("%d %s %s", t, rawval, id)})
		return
	}
	vec, err := fourstate.ParseVector(rawval)
	if err != nil {
		f.Diagnostics = append(f.Diagnostics, Diagnostic{Where: "value@" + sig.Name, Reason: err.Error(), Raw: rawval})
		return
	}
	f.Changes = append(f.Changes, Change{Time: t, Signal: sig.Name, RawValue: rawval, Value: vec})
}

func parseTimescale(raw string) (Timescale, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Timescale{}, fmt.Errorf("empty timescale")
	}
	i := 0
	for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
		i++
	}
	n, err := strconv.ParseInt(raw[:i], 10, 64)
	if err != nil {
		return Timescale{}, fmt.Errorf("bad timescale number in %q", raw)
	}
	unit := strings.TrimSpace(raw[i:])
	ts := Timescale{Raw: raw, Number: n, Unit: unit}
	if _, ok := unitToFs[unit]; !ok {
		return ts, fmt.Errorf("unknown time unit %q", unit)
	}
	return ts, nil
}

// ValueAt returns the value of a signal at raw time t (last change at or
// before t). ok=false when the signal has no change at or before t.
func (f *File) ValueAt(signal string, t uint64) (fourstate.Vector, bool) {
	var best *Change
	for i := range f.Changes {
		c := &f.Changes[i]
		if c.Signal == signal && c.Time <= t {
			if best == nil || c.Time >= best.Time {
				best = c
			}
		}
	}
	if best == nil {
		return fourstate.Vector{}, false
	}
	return best.Value, true
}
