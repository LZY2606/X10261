// Package vcd parses VCD waveform files. Raw tokens are always preserved;
// values that cannot be converted stay in place with a diagnostic instead
// of being silently cleaned. Structural errors abort the whole parse so an
// import can be rolled back atomically.
package vcd

import (
	"fmt"
	"strconv"
	"strings"

	"sigcourt/internal/logic4"
)

// Change is one raw value-change record from the file.
type Change struct {
	Time   int64         // simulation time as printed (timescale units)
	Signal string        // signal name
	Raw    string        // raw token, e.g. "1!" or "u&"
	Value  logic4.Value  // converted value (VX placeholder when !OK)
	OK     bool          // false when the token could not be converted
}

// Diagnostic marks data kept in raw form because conversion failed.
type Diagnostic struct {
	Where  string // e.g. "timescale", "value#12"
	Raw    string
	Reason string
}

// File is a parsed VCD document.
type File struct {
	TimescaleRaw  string
	TimescaleNum  int64
	TimescaleUnit string
	Signals       []string // declaration order
	Changes       []Change
	Diagnostics   []Diagnostic
}

// unitFactorFs maps a timescale unit to femtoseconds.
var unitFactorFs = map[string]int64{
	"fs": 1, "ps": 1000, "ns": 1000 * 1000, "us": 1000 * 1000 * 1000,
	"ms": 1000 * 1000 * 1000 * 1000, "s": 1000 * 1000 * 1000 * 1000 * 1000,
}

// ScaleFs returns femtoseconds per time unit, or 0 when the timescale
// could not be normalized (raw text is still kept).
func (f *File) ScaleFs() int64 {
	factor, ok := unitFactorFs[f.TimescaleUnit]
	if !ok || f.TimescaleNum <= 0 {
		return 0
	}
	return f.TimescaleNum * factor
}

// Parse reads a VCD document. It returns an error only for structural
// problems; unconvertible values are preserved with diagnostics.
func Parse(text string) (*File, error) {
	f := &File{}
	tokens := tokenize(text)
	ids := map[string]string{}
	i := 0
	// header
	seenEnd := false
	for i < len(tokens) {
		t := tokens[i]
		switch t {
		case "$timescale":
			i++
			parts := []string{}
			for i < len(tokens) && tokens[i] != "$end" {
				parts = append(parts, tokens[i])
				i++
			}
			raw := strings.Join(parts, " ")
			f.TimescaleRaw = raw
			num, unit, ok := parseTimescale(raw)
			if !ok {
				f.Diagnostics = append(f.Diagnostics, Diagnostic{
					Where: "timescale", Raw: raw,
					Reason: "timescale cannot be normalized; raw text kept",
				})
			} else {
				f.TimescaleNum, f.TimescaleUnit = num, unit
			}
		case "$var":
			// $var wire 1 ! name $end
			if i+4 >= len(tokens) {
				return nil, fmt.Errorf("vcd: truncated $var")
			}
			id := tokens[i+3]
			name := tokens[i+4]
			ids[id] = name
			f.Signals = append(f.Signals, name)
			i += 4
			for i < len(tokens) && tokens[i] != "$end" {
				i++
			}
		case "$enddefinitions":
			seenEnd = true
			for i < len(tokens) && tokens[i] != "$end" {
				i++
			}
		case "$scope", "$upscope", "$date", "$version", "$comment":
			for i < len(tokens) && tokens[i] != "$end" {
				i++
			}
		}
		i++
		if seenEnd {
			break
		}
	}
	if !seenEnd {
		return nil, fmt.Errorf("vcd: missing $enddefinitions")
	}
	// simulation section
	curTime := int64(0)
	for ; i < len(tokens); i++ {
		t := tokens[i]
		if t == "" {
			continue
		}
		switch t[0] {
		case '#':
			n, err := strconv.ParseInt(t[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd: bad time token %q", t)
			}
			curTime = n
		case '0', '1', 'x', 'X', 'z', 'Z':
			if len(t) < 2 {
				return nil, fmt.Errorf("vcd: dangling value token %q", t)
			}
			id := t[1:]
			name, ok := ids[id]
			if !ok {
				return nil, fmt.Errorf("vcd: change for undeclared id %q", id)
			}
			v, _ := logic4.Parse(t[0])
			f.Changes = append(f.Changes, Change{
				Time: curTime, Signal: name, Raw: t, Value: v, OK: true,
			})
		case 'b', 'B':
			// vector token: "b101" followed by the id token
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("vcd: bad vector token %q", t)
			}
			id := tokens[i+1]
			i++
			name, ok := ids[id]
			if !ok {
				return nil, fmt.Errorf("vcd: change for undeclared id %q", id)
			}
			bits := t[1:]
			if len(bits) == 1 {
				v, _ := logic4.Parse(bits[0])
				f.Changes = append(f.Changes, Change{
					Time: curTime, Signal: name, Raw: t + " " + id, Value: v, OK: true,
				})
			} else {
				f.Changes = append(f.Changes, Change{
					Time: curTime, Signal: name, Raw: t + " " + id, Value: logic4.VX, OK: false,
				})
				f.Diagnostics = append(f.Diagnostics, Diagnostic{
					Where: fmt.Sprintf("value#%d", curTime), Raw: t + " " + id,
					Reason: "multi-bit vector not supported by this tool; raw token kept",
				})
			}
		default:
			// unknown token (e.g. 'u&'): keep raw with a diagnostic
			if len(t) >= 2 {
				id := t[1:]
				if name, ok := ids[id]; ok {
					f.Changes = append(f.Changes, Change{
						Time: curTime, Signal: name, Raw: t, Value: logic4.VX, OK: false,
					})
					f.Diagnostics = append(f.Diagnostics, Diagnostic{
						Where: fmt.Sprintf("value#%d", curTime), Raw: t,
						Reason: fmt.Sprintf("token %q not convertible to 0/1/X/Z; raw kept", string(t[0])),
					})
					continue
				}
			}
			return nil, fmt.Errorf("vcd: unrecognized token %q", t)
		}
	}
	return f, nil
}

func tokenize(text string) []string {
	return strings.Fields(text)
}

func parseTimescale(raw string) (int64, string, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0, "", false
	}
	s := strings.Join(fields, "")
	units := []string{"fs", "ps", "ns", "us", "ms", "s"}
	for _, u := range units {
		if strings.HasSuffix(s, u) {
			numPart := strings.TrimSuffix(s, u)
			n, err := strconv.ParseInt(numPart, 10, 64)
			if err != nil || n <= 0 {
				return 0, "", false
			}
			return n, u, true
		}
	}
	return 0, "", false
}
