// Package vcd parses a deliberately small subset of Value Change Dump files.
// Nothing is normalized here: the raw timescale representation, raw tokens and
// raw timestamps survive verbatim. Normalization happens only in a derived
// layer (see internal/engine), keyed by a coordinate basis.
package vcd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Timescale is the raw timescale declaration. Unit and Number keep the source
// spelling; UnitBase records the declared coordinate basis.
type Timescale struct {
	Number   string // e.g. "1", "10"
	Unit     string // e.g. "ns", "ps"
	Raw      string // full header spelling
	UnitBase string // normalized SI base unit ("s","ms","us","ns","ps","fs")
	Factor   int    // unit scale in picoseconds
}

// Signal is one declared variable/wire.
type Signal struct {
	ID     string // VCD identifier code
	Name   string // full hierarchical name
	Width  int
	Kind   string // wire/reg/...
	Raw    string // original declaration line
}

// Change is one value-change record. Raw is the exact token spelling
// (including unconvertible values). Valid=false means the token could not be
// converted and is retained in place with a diagnostic.
type Change struct {
	Time        int64  // raw #timestamp, in timescale units
	SignalID    string
	Raw         string
	Valid       bool
	Value       byte // '0','1','x','z' when Valid
	Vector      bool
	Diagnostic  string
}

// File is the parsed-but-not-normalized material.
type File struct {
	Timescale  Timescale
	Signals    []Signal
	SignalByID map[string]int
	Changes    []Change
	RawBytes   []byte
	Diagnostics []Diagnostic
}

// Diagnostic attaches an explanation to an unconvertible fragment without
// removing the fragment.
type Diagnostic struct {
	Line    int
	Token   string
	Message string
}

var unitToPS = map[string]int{
	"s":  1_000_000_000_000,
	"ms": 1_000_000_000,
	"us": 1_000_000,
	"ns": 1_000,
	"ps": 1,
	"fs": 0, // femtoseconds cannot be losslessly expressed in the ps basis
}

// Parse reads a VCD stream.
func Parse(r io.Reader) (*File, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	f := &File{RawBytes: raw, SignalByID: map[string]int{}}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var (
		line      int
		curTime   int64
		hdr       []string
		inDefs    bool
	)
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		switch {
		case strings.HasPrefix(text, "$timescale"):
			ts, body, err := readSection(text, sc, &line)
			if err != nil {
				return nil, err
			}
			f.Timescale = parseTimescale(strings.TrimSpace(body))
			hdr = append(hdr, ts)
			if f.Timescale.Factor == 0 && f.Timescale.UnitBase != "fs" {
				return nil, fmt.Errorf("line %d: unsupported timescale %q", line, f.Timescale.Raw)
			}
		case text == "$scope":
			inDefs = true
		case strings.HasPrefix(text, "$scope") || strings.HasPrefix(text, "$upscope") || strings.HasPrefix(text, "$enddefinitions"):
			if strings.HasPrefix(text, "$enddefinitions") {
				inDefs = false
			}
		case strings.HasPrefix(text, "$var"):
			sig, err := parseVar(text)
			if err != nil {
				f.Diagnostics = append(f.Diagnostics, Diagnostic{line, text, err.Error()})
				continue
			}
			if _, ok := f.SignalByID[sig.ID]; !ok {
				f.SignalByID[sig.ID] = len(f.Signals)
				f.Signals = append(f.Signals, sig)
			}
		case strings.HasPrefix(text, "#"):
			t, perr := strconv.ParseInt(text[1:], 10, 64)
			if perr != nil {
				f.Diagnostics = append(f.Diagnostics, Diagnostic{line, text, "unparseable timestamp: " + perr.Error()})
				continue
			}
			curTime = t
		case text[0] == 'b' || text[0] == 'B' || text[0] == 'r' || text[0] == 'R':
			// Vector / real change: "b01xz id". Keep raw; take leftmost bit.
			parts := strings.Fields(text)
			if len(parts) != 2 {
				f.Diagnostics = append(f.Diagnostics, Diagnostic{line, text, "malformed vector change"})
				continue
			}
			ch := Change{Time: curTime, SignalID: parts[1], Raw: text, Vector: true}
			bits := strings.TrimPrefix(strings.TrimPrefix(parts[0], "b"), "B")
			f.classifyBit(&ch, firstBit(bits), line)
			f.Changes = append(f.Changes, ch)
		case isScalarStart(text[0]) && len(text) >= 2:
			ch := Change{Time: curTime, SignalID: text[1:], Raw: text}
			f.classifyBit(&ch, text[0], line)
			f.Changes = append(f.Changes, ch)
		default:
			_ = inDefs
		}
	}
	return f, sc.Err()
}

func readSection(first string, sc *bufio.Scanner, line *int) (string, string, error) {
	if strings.Contains(first, "$end") {
		return first, strings.TrimSpace(stripSection(first)), nil
	}
	var body strings.Builder
	body.WriteString(first)
	for sc.Scan() {
		*line++
		t := sc.Text()
		body.WriteString(" ")
		body.WriteString(strings.TrimSpace(t))
		if strings.Contains(t, "$end") {
			break
		}
	}
	raw := body.String()
	return raw, strings.TrimSpace(stripSection(raw)), nil
}

func stripSection(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$timescale")
	s = strings.TrimSuffix(s, "$end")
	return s
}

func parseTimescale(body string) Timescale {
	fields := strings.Fields(body)
	ts := Timescale{Raw: body}
	if len(fields) >= 2 {
		ts.Number = fields[0]
		ts.Unit = fields[1]
		ts.UnitBase = normalizeUnit(fields[1])
		ts.Factor = unitToPS[ts.UnitBase]
		if n, err := strconv.Atoi(fields[0]); err == nil {
			ts.Factor *= n
		}
	}
	return ts
}

func normalizeUnit(u string) string {
	switch strings.ToLower(u) {
	case "s":
		return "s"
	case "ms":
		return "ms"
	case "us", "µs":
		return "us"
	case "ns":
		return "ns"
	case "ps":
		return "ps"
	case "fs":
		return "fs"
	default:
		return strings.ToLower(u)
	}
}

func parseVar(line string) (Signal, error) {
	fields := strings.Fields(line)
	// $var wire 8 ! data [7:0] $end
	if len(fields) < 5 || fields[0] != "$var" {
		return Signal{}, fmt.Errorf("malformed $var line")
	}
	width, err := strconv.Atoi(fields[2])
	if err != nil {
		return Signal{}, fmt.Errorf("bad width: %v", err)
	}
	nameEnd := len(fields)
	if fields[nameEnd-1] == "$end" {
		nameEnd--
	}
	name := strings.Join(fields[4:nameEnd], " ")
	return Signal{Kind: fields[1], Width: width, ID: fields[3], Name: name, Raw: line}, nil
}

func isScalarStart(c byte) bool {
	return c == '0' || c == '1' || c == 'x' || c == 'X' || c == 'z' || c == 'Z'
}

func firstBit(bits string) byte {
	for i := 0; i < len(bits); i++ {
		c := bits[i]
		if isScalarStart(c) {
			return c
		}
	}
	return 0
}

func (f *File) classifyBit(ch *Change, bit byte, line int) {
	switch bit {
	case '0', '1', 'x', 'X', 'z', 'Z':
		ch.Valid = true
		ch.Value = bit
	default:
		ch.Valid = false
		ch.Diagnostic = fmt.Sprintf("line %d: unconvertible bit %q kept at original position", line, string(bit))
		f.Diagnostics = append(f.Diagnostics, Diagnostic{line, ch.Raw, ch.Diagnostic})
	}
}
