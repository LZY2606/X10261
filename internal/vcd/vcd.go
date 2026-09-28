// Package vcd parses IEEE 1364 Value Change Dump files while preserving raw
// tokens, original timescale precision and four-state (0/1/x/z) values.
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"gsb-svc/internal/fourstate"
)

// Variable describes one VCD identifier declaration.
type Variable struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Width     int    `json:"width"`
	Name      string `json:"name"`
	Reference string `json:"reference"`
}

// Change is a single value-change event at a timestamp (picoseconds).
type Change struct {
	TimePS int64             `json:"time_ps"`
	ID     string            `json:"id"`
	Value  fourstate.Value   `json:"value"`
	// Bits holds the raw per-bit vector tokens (MSB first) so multi-bit
	// vectors are not lossily collapsed to a scalar.
	Bits       []fourstate.Value `json:"bits,omitempty"`
	Raw        string            `json:"raw"`
	ConvertErr string            `json:"convert_err,omitempty"`
}

// File is a parsed VCD preserving raw representation and timescale.
type File struct {
	// RawTimescaleNumber/RawUnit preserve the exact source spelling, e.g. "1"
	// and "ns"; TimePS carries the normalized picosecond coordinate.
	RawTimescaleNumber string  `json:"raw_timescale_number"`
	RawTimescaleUnit   string  `json:"raw_timescale_unit"`
	TimeUnitPS         int64   `json:"time_unit_ps"`
	Unit               string  `json:"unit"` // normalized unit name with basis
	Variables          []Variable      `json:"variables"`
	Changes            []Change        `json:"changes"`
	EndTimePS          int64           `json:"end_time_ps"`
	Diagnostics        []Diagnostic    `json:"diagnostics,omitempty"`
	raw                string
}

// Diagnostic records an unconvertible value left in place rather than dropped.
type Diagnostic struct {
	TimePS int64  `json:"time_ps"`
	ID     string `json:"id"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

func (f *File) Raw() string { return f.raw }

// VariableByName returns the variable and its index for a hierarchical name.
func (f *File) VariableByName(name string) (Variable, bool) {
	for _, v := range f.Variables {
		if v.Name == name || v.Reference == name {
			return v, true
		}
	}
	return Variable{}, false
}

// unitPS converts a VCD timescale unit string ("s","ms","us","ns","ps","fs")
// to picoseconds.
func unitPS(unit string) (int64, error) {
	switch strings.TrimSpace(unit) {
	case "s":
		return 1_000_000_000_000, nil
	case "ms":
		return 1_000_000_000, nil
	case "us", "µs":
		return 1_000_000, nil
	case "ns":
		return 1_000, nil
	case "ps":
		return 1, nil
	case "fs":
		// cannot represent a femtosecond in the picosecond basis: caller keeps
		// the raw spelling and attaches a diagnostic instead of silently
		// truncating.
		return 0, fmt.Errorf("unit %q is below the picosecond basis", unit)
	default:
		return 0, fmt.Errorf("unknown timescale unit %q", unit)
	}
}

// Parser consumes a VCD stream.
type Parser struct {
	sc  *bufio.Scanner
	raw strings.Builder
}

// NewParser constructs a parser over r.
func NewParser(r io.Reader) *Parser {
	return &Parser{sc: bufio.NewScanner(r)}
}

// Parse fully reads the VCD.
func (p *Parser) Parse() (*File, error) {
	p.sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	f := &File{}
	vars := map[string]Variable{}
	var currentTime int64
	section := ""
	for p.sc.Scan() {
		line := strings.TrimSpace(p.sc.Text())
		p.raw.WriteString(p.sc.Text())
		p.raw.WriteByte('\n')
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch {
		case line[0] == '$':
			key := fields[0]
			switch key {
			case "$timescale":
				section = "timescale"
				if len(fields) >= 3 {
					f.RawTimescaleNumber = fields[1]
					f.RawTimescaleUnit = fields[2]
				} else if len(fields) == 2 {
					f.RawTimescaleNumber = fields[1]
				}
			case "$enddefinitions":
				section = ""
			case "$var":
				// $var wire 8 ! data [7:0] $end
				if len(fields) < 5 {
					return nil, fmt.Errorf("vcd: malformed var directive: %q", line)
				}
				width, err := strconv.Atoi(fields[2])
				if err != nil {
					return nil, fmt.Errorf("vcd: bad var width: %q", fields[2])
				}
				id := fields[3]
				name := fields[4]
				ref := name
				if end := strings.Index(line, "$end"); end >= 0 && len(fields) >= 6 {
					mid := strings.Fields(line[5:])
					if len(mid) > 0 && mid[len(mid)-1] != "$end" {
						ref = strings.Join(mid[:len(mid)-1], " ")
					}
				}
				v := Variable{ID: id, Kind: fields[1], Width: width, Name: name, Reference: ref}
				f.Variables = append(f.Variables, v)
				vars[id] = v
			case "$scope", "$upscope", "$comment", "$date", "$version", "$dumpvars", "$dumpon", "$dumpoff":
				if key == "$dumpvars" {
					section = "dumpvars"
					continue
				}
			}
			if hasEnd(line) && section != "dumpvars" {
				section = ""
			}
		case line[0] == '#':
			t, err := strconv.ParseInt(line[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd: bad timestamp %q", line)
			}
			if f.TimeUnitPS == 0 {
				return nil, fmt.Errorf("vcd: timestamp #%d before $timescale", t)
			}
			currentTime = t * f.TimeUnitPS
			if currentTime > f.EndTimePS {
				f.EndTimePS = currentTime
			}
		case line[0] == 'b' || line[0] == 'B' || line[0] == 'r' || line[0] == 'R':
			parts := strings.Fields(line)
			if len(parts) != 2 {
				return nil, fmt.Errorf("vcd: malformed vector change %q", line)
			}
			token := parts[0]
			id := parts[1]
			ch := Change{TimePS: currentTime, ID: id, Raw: line}
			if token[0] == 'r' || token[0] == 'R' {
				ch.ConvertErr = "real values are kept raw; the four-state layer cannot normalize them"
				f.Diagnostics = append(f.Diagnostics, Diagnostic{TimePS: currentTime, ID: id, Raw: line, Reason: ch.ConvertErr})
			} else {
				bits := parseBits(token[1:])
				ch.Bits = bits
				ch.Value = reduceBits(bits)
			}
			f.Changes = append(f.Changes, ch)
		default:
			// scalar change: "0!", "x#", "z$" ...
			valueToken := ""
			id := ""
			if len(fields) >= 2 {
				valueToken, id = fields[0], fields[1]
			} else {
				valueToken, id = line[:1], line[1:]
			}
			v, err := fourstate.Parse(valueToken)
			if err != nil {
				d := Diagnostic{TimePS: currentTime, ID: id, Raw: line, Reason: err.Error()}
				f.Diagnostics = append(f.Diagnostics, d)
				f.Changes = append(f.Changes, Change{TimePS: currentTime, ID: id, Value: fourstate.X, Raw: line, ConvertErr: err.Error()})
				continue
			}
			f.Changes = append(f.Changes, Change{TimePS: currentTime, ID: id, Value: v, Raw: line})
		}
	}
	if err := p.sc.Err(); err != nil {
		return nil, err
	}
	if f.RawTimescaleNumber == "" {
		return nil, fmt.Errorf("vcd: missing $timescale")
	}
	num, err := strconv.ParseInt(f.RawTimescaleNumber, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("vcd: bad timescale number %q", f.RawTimescaleNumber)
	}
	unitScale, err := unitPS(f.RawTimescaleUnit)
	if err != nil {
		return nil, fmt.Errorf("vcd: %w", err)
	}
	f.TimeUnitPS = num * unitScale
	f.Unit = "ps"
	f.raw = p.raw.String()
	sort.SliceStable(f.Changes, func(i, j int) bool {
		if f.Changes[i].TimePS != f.Changes[j].TimePS {
			return f.Changes[i].TimePS < f.Changes[j].TimePS
		}
		return f.Changes[i].ID < f.Changes[j].ID
	})
	return f, nil
}

func hasEnd(line string) bool {
	for _, f := range strings.Fields(line) {
		if f == "$end" {
			return true
		}
	}
	return false
}

func parseBits(s string) []fourstate.Value {
	bits := make([]fourstate.Value, 0, len(s))
	for _, r := range s {
		v, err := fourstate.Parse(string(r))
		if err != nil {
			bits = append(bits, fourstate.X)
			continue
		}
		bits = append(bits, v)
	}
	return bits
}

// reduceBits collapses a vector to a scalar: all zeros -> 0, all ones -> 1,
// any x -> x, only z plus one binary -> that binary, mix -> x.
func reduceBits(bits []fourstate.Value) fourstate.Value {
	return fourstate.Resolve(bits...)
}

// ValueAt returns the four-state value of id at time ps (last change <= ps),
// and whether the signal has any driver event at or before ps.
func (f *File) ValueAt(id string, ps int64) (fourstate.Value, bool) {
	var cur fourstate.Value = fourstate.X
	driven := false
	for _, c := range f.Changes {
		if c.ID != id || c.TimePS > ps {
			continue
		}
		cur = c.Value
		driven = true
	}
	return cur, driven
}

// ChangesFor returns the change stream of one variable.
func (f *File) ChangesFor(id string) []Change {
	var out []Change
	for _, c := range f.Changes {
		if c.ID == id {
			out = append(out, c)
		}
	}
	return out
}
