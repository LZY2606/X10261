// Package vcd parses the subset of IEEE 1364 VCD used by the offline case pack.
// It deliberately preserves source-level representations: raw value tokens,
// the timescale string as written, and unconvertible values are kept in place
// with a diagnostic instead of being dropped.
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"signalcourt/internal/fourstate"
)

// Change is one raw value change exactly as seen in the VCD.
type Change struct {
	Time     int64  // integer time in the VCD's own timescale units
	Raw      string // token exactly as written, e.g. "x", "1", "z"
	Valid    bool   // false when the token could not be converted to 0/1/X/Z
	Value    fourstate.Value
	Diagnostic string // populated when Valid is false
}

// Signal is one wire with its complete change history (source preserved).
type Signal struct {
	ID       string
	Name     string
	Width    int
	Changes  []Change
}

// Diagnostic records a source fragment that could not be normalized.
type Diagnostic struct {
	Line int
	Kind string // "value" | "timescale"
	Raw  string
	Msg  string
}

// Timescale preserves both the original representation and a derived basis.
type Timescale struct {
	Raw       string        // e.g. "10ns", exactly as written
	Unit      time.Duration // derived normalized unit; 0 when unconvertible
	Precision time.Duration // sampling precision basis for derived layers
	Convertible bool
}

// File is the parsed VCD: provenance-first, normalization optional.
type File struct {
	Timescale   Timescale
	Signals     []*Signal
	byID        map[string]*Signal
	Order       []string // signal IDs in declaration order
	Diagnostics []Diagnostic
}

var (
	scopeRe   = regexp.MustCompile(`^\s*\$?(?:var\s+wire\s+(\d+)\s+(\S+)\s+(\S+))`)
	tsRe      = regexp.MustCompile(`(?i)^(?:(\d+)\s*(s|ms|us|µs|ns|ps|fs))$`)
	changeRe  = regexp.MustCompile(`^([01xXzZwW])([!-~]+)$`)
	bchangeRe = regexp.MustCompile(`^([01xXzZwW]{2,})\s+([!-~]+)$`)
)

// Parse reads a VCD stream. Parse errors are returned; soft problems
// (unconvertible values/timescales) are attached as diagnostics.
func Parse(r io.Reader) (*File, error) {
	f := &File{byID: map[string]*Signal{}, Timescale: Timescale{Raw: ""}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	var cur int64
	line := 0
	inHeader := true
	for sc.Scan() {
		line++
		text := sc.Text()
		trim := strings.TrimSpace(text)
		if trim == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trim, "$timescale"):
			rest := strings.TrimSpace(strings.TrimPrefix(trim, "$timescale"))
			if i := strings.Index(rest, "$end"); i >= 0 {
				rest = rest[:i]
			}
			if rest == "" {
				if sc.Scan() {
					line++
					rest = strings.TrimSpace(sc.Text())
					if i := strings.Index(rest, "$end"); i >= 0 {
						rest = rest[:i]
					}
				}
			}
			f.Timescale = parseTimescale(strings.TrimSpace(rest), line)
		case strings.HasPrefix(trim, "$var"):
			m := scopeRe.FindStringSubmatch(trim)
			if m == nil {
				return nil, fmt.Errorf("line %d: malformed $var: %q", line, trim)
			}
			width, _ := strconv.Atoi(m[1])
			id, name := m[2], m[3]
			if _, ok := f.byID[id]; !ok {
				s := &Signal{ID: id, Name: name, Width: width}
				f.byID[id] = s
				f.Signals = append(f.Signals, s)
				f.Order = append(f.Order, id)
			}
		case strings.HasPrefix(trim, "$enddefinitions"):
			inHeader = false
		case strings.HasPrefix(trim, "$"):
			// ignored directive body ($dumpvars, $scope, ...)
		case strings.HasPrefix(trim, "#"):
			n, err := strconv.ParseInt(strings.TrimPrefix(trim, "#"), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: bad timestamp %q", line, trim)
			}
			cur = n
			_ = inHeader
		default:
			bm := bchangeRe.FindStringSubmatch(trim)
			m := changeRe.FindStringSubmatch(trim)
			var tok, id string
			switch {
			case bm != nil:
				tok, id = bm[1], bm[2]
				if strings.ContainsAny(tok, "xXzZ") {
					tok = strings.ToLower(strings.TrimLeft(tok, "01"))
					if tok == "" {
						tok = "x"
					}
					tok = string(tok[0])
				} else {
					tok = string(tok[len(tok)-1])
				}
			case m != nil:
				tok, id = m[1], m[2]
			default:
				return nil, fmt.Errorf("line %d: cannot parse change %q", line, trim)
			}
			s, ok := f.byID[id]
			if !ok {
				return nil, fmt.Errorf("line %d: change for undeclared id %q", line, id)
			}
			ch := Change{Time: cur, Raw: tok}
			if v, err := fourstate.Parse(tok); err != nil {
				ch.Valid = false
				ch.Value = fourstate.VX
				ch.Diagnostic = err.Error()
				f.Diagnostics = append(f.Diagnostics, Diagnostic{
					Line: line, Kind: "value", Raw: tok, Msg: ch.Diagnostic,
				})
			} else {
				ch.Valid = true
				ch.Value = v
			}
			s.Changes = append(s.Changes, ch)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(f.Signals) == 0 {
		return nil, fmt.Errorf("no signals declared")
	}
	return f, nil
}

func parseTimescale(raw string, line int) Timescale {
	ts := Timescale{Raw: raw}
	m := tsRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		ts.Convertible = false
		return ts
	}
	n, _ := strconv.Atoi(m[1])
	var unit time.Duration
	switch strings.ToLower(m[2]) {
	case "s":
		unit = time.Second
	case "ms":
		unit = time.Millisecond
	case "us", "µs":
		unit = time.Microsecond
	case "ns", "":
		unit = time.Nanosecond
	case "ps":
		unit = time.Nanosecond / 1000
	case "fs":
		unit = time.Nanosecond / 1_000_000
	default:
		ts.Convertible = false
		return ts
	}
	ts.Unit = time.Duration(n) * unit
	ts.Precision = ts.Unit
	ts.Convertible = true
	return ts
}

// SignalByName returns the declared signal or nil.
func (f *File) SignalByName(name string) *Signal {
	for _, s := range f.Signals {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// ValueAt returns the value holding at time t (step-function readout) plus the
// raw token. Unconvertible tokens stay at their original position.
func (s *Signal) ValueAt(t int64) (fourstate.Value, Change, bool) {
	var hold Change
	found := false
	for _, c := range s.Changes {
		if c.Time <= t {
			hold = c
			found = true
		} else {
			break
		}
	}
	return hold.Value, hold, found
}
