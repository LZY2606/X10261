// Package vcd parses the subset of IEEE 1364 VCD used by the case pack:
// $timescale, $scope/$var (wire/reg/logic), $enddefinitions and scalar
// value changes. Original representations (value characters, timescale token
// and raw timestamps) are preserved on every record.
package vcd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Timescale keeps the timescale declaration exactly as authored, alongside a
// normalized picosecond resolution used only by the derived sampling layer.
type Timescale struct {
	Raw      string `json:"raw"`      // e.g. "1ns"
	Unit     string `json:"unit"`     // "s","ms","us","ns","ps","fs"
	Number   int    `json:"number"`   // multiplier, e.g. 1
	ResPicos int64  `json:"resPicos"` // one tick in picoseconds (0 when unconvertible)
}

// Signal is one VCD variable declaration.
type Signal struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Width int    `json:"width"`
	Kind  string `json:"kind"` // wire/reg/logic
	Scope string `json:"scope"` // dotted scope path
}

// Change is one scalar value-change record. Raw holds the exact VCD token
// (e.g. "z", "1") and At is the timestamp in VCD time units (timescale units).
type Change struct {
	SignalID string `json:"signalId"`
	At       int64  `json:"at"`
	Raw      string `json:"raw"`
	Value    string `json:"value"` // canonical 0/1/X/Z
	Initial  bool   `json:"initial"`
}

// ParseDiagnostic records a fragment that could not be converted; the raw
// fragment is retained in place and never dropped.
type ParseDiagnostic struct {
	Line     int    `json:"line"`
	Kind     string `json:"kind"`
	Fragment string `json:"fragment"`
	Message  string `json:"message"`
}

// File is the structured, lossless view of one VCD source.
type File struct {
	Timescale   Timescale         `json:"timescale"`
	Signals     []Signal          `json:"signals"`
	Changes     []Change          `json:"changes"`
	EndDefLine  int               `json:"endDefLine"`
	Diagnostics []ParseDiagnostic `json:"diagnostics"`

	byID map[string]*Signal
}

var unitPicos = map[string]int64{
	"s":  1e12,
	"ms": 1e9,
	"us": 1e6,
	"ns": 1e3,
	"ps": 1,
	"fs": 0, // sub-picosecond cannot be represented: leave resPicos 0
}

// Parse reads a VCD stream. On hard failure (unclosed header directive,
// malformed timestamp) an error is returned so the importer can roll back;
// soft diagnostics on unconvertible fragments are retained on the File.
func Parse(r io.Reader) (*File, error) {
	f := &File{byID: map[string]*Signal{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)

	var (
		line      int
		scopes    []string
		curTime   int64
		inHeader  = true
		dirTokens []string
		initial   = true
	)
	flushDir := func() error {
		if len(dirTokens) == 0 {
			return nil
		}
		kw := dirTokens[0]
		args := dirTokens[1:]
		dirTokens = nil
		switch kw {
		case "$timescale":
			ts, d := parseTimescale(strings.Join(args, " "))
			f.Timescale = ts
			if d != nil {
				f.Diagnostics = append(f.Diagnostics, *d)
			}
		case "$scope":
			if len(args) >= 2 {
				scopes = append(scopes, args[1])
			}
		case "$upscope":
			if len(scopes) > 0 {
				scopes = scopes[:len(scopes)-1]
			}
		case "$var":
			if err := parseVar(args, scopes, f); err != nil {
				return err
			}
		case "$enddefinitions":
			inHeader = false
			f.EndDefLine = line
		}
		return nil
	}

	for sc.Scan() {
		line++
		txt := strings.TrimSpace(sc.Text())
		if txt == "" {
			continue
		}
		if strings.HasPrefix(txt, "$comment") || strings.HasPrefix(txt, "$dumpvars") ||
			strings.HasPrefix(txt, "$dumpall") || strings.HasPrefix(txt, "$dumpon") ||
			strings.HasPrefix(txt, "$dumpoff") {
			if strings.HasPrefix(txt, "$dumpvars") {
				initial = true
				if strings.Contains(txt, "$end") && txt != "$dumpvars" {
					initial = false
				}
			}
			continue
		}
		if txt == "$end" {
			if inHeader && len(dirTokens) > 0 {
				if err := flushDir(); err != nil {
					return nil, err
				}
			}
			initial = false
			continue
		}
		if inHeader {
			if strings.HasPrefix(txt, "$") {
				if len(dirTokens) > 0 {
					return nil, fmt.Errorf("vcd: nested directive at line %d", line)
				}
				dirTokens = strings.Fields(txt)
				if strings.Contains(txt, "$end") && len(dirTokens) > 1 {
					dirTokens = dirTokens[:len(dirTokens)-1]
					if err := flushDir(); err != nil {
						return nil, err
					}
				}
				continue
			}
			if len(dirTokens) > 0 {
				dirTokens = append(dirTokens, strings.Fields(txt)...)
				continue
			}
			return nil, fmt.Errorf("vcd: unexpected header content at line %d: %q", line, txt)
		}
		if strings.HasPrefix(txt, "#") {
			t, err := strconv.ParseInt(txt[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd: bad timestamp at line %d: %w", line, err)
			}
			curTime = t
			initial = false
			continue
		}
		if err := parseChange(txt, curTime, initial, f, line); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if f.Timescale.Raw == "" {
		return nil, errors.New("vcd: missing $timescale")
	}
	return f, nil
}

func parseTimescale(s string) (Timescale, *ParseDiagnostic) {
	raw := strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	t := Timescale{Raw: raw}
	var numStr, unit string
	for i, c := range raw {
		if (c < '0' || c > '9') && c != '.' {
			numStr, unit = raw[:i], raw[i:]
			break
		}
	}
	if numStr == "" || unit == "" {
		return t, &ParseDiagnostic{Kind: "timescale", Fragment: raw, Message: "unparseable timescale"}
	}
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return t, &ParseDiagnostic{Kind: "timescale", Fragment: raw, Message: "bad timescale number"}
	}
	t.Number = int(n)
	t.Unit = unit
	if p, ok := unitPicos[unit]; ok {
		if unit == "fs" {
			return t, &ParseDiagnostic{Kind: "timescale", Fragment: raw, Message: "femtosecond resolution cannot be normalized to picosecond base; retained raw"}
		}
		t.ResPicos = int64(n * float64(p))
		if t.ResPicos == 0 {
			return t, &ParseDiagnostic{Kind: "timescale", Fragment: raw, Message: "timescale normalizes to zero picoseconds; retained raw"}
		}
	} else {
		return t, &ParseDiagnostic{Kind: "timescale", Fragment: raw, Message: "unknown timescale unit; retained raw"}
	}
	return t, nil
}

func parseVar(args []string, scopes []string, f *File) error {
	if len(args) < 4 || args[0] != "$var" {
		return fmt.Errorf("vcd: malformed $var: %v", args)
	}
	w, err := strconv.Atoi(args[2])
	if err != nil {
		return fmt.Errorf("vcd: bad var width: %v", args)
	}
	id, name := args[3], args[4]
	scope := strings.Join(scopes, ".")
	sig := &Signal{ID: id, Name: name, Width: w, Kind: args[1], Scope: scope}
	f.Signals = append(f.Signals, *sig)
	f.byID[id] = sig
	return nil
}

func parseChange(txt string, at int64, initial bool, f *File, line int) error {
	fields := strings.Fields(txt)
	var tok, id string
	switch len(fields) {
	case 1:
		tok = fields[0]
		if len(tok) < 2 {
			return fmt.Errorf("vcd: malformed change at line %d: %q", line, txt)
		}
		val, id = tok[:1], tok[1:]
	case 2:
		val, id = fields[0], fields[1]
	default:
		return fmt.Errorf("vcd: malformed change at line %d: %q", line, txt)
	}
	if _, ok := f.byID[id]; !ok {
		f.Diagnostics = append(f.Diagnostics, ParseDiagnostic{
			Line: line, Kind: "change", Fragment: txt,
			Message: fmt.Sprintf("value change for undeclared identifier %q retained in place", id),
		})
	}
	canon := "X"
	if len(val) > 0 {
		if v, ok := parseVal(val[len(val)-1]); ok {
			canon = v
		} else {
			f.Diagnostics = append(f.Diagnostics, ParseDiagnostic{
				Line: line, Kind: "value", Fragment: txt,
				Message: fmt.Sprintf("value token %q not convertible to 0/1/X/Z; kept at original position", val),
			})
		}
	}
	f.Changes = append(f.Changes, Change{
		SignalID: id, At: at, Raw: val, Value: canon, Initial: initial,
	})
	return nil
}

func parseVal(c byte) (string, bool) {
	switch c {
	case '0':
		return "0", true
	case '1':
		return "1", true
	case 'x', 'X':
		return "X", true
	case 'z', 'Z':
		return "Z", true
	default:
		return "X", false
	}
}

// SignalByID returns the declared signal for a VCD identifier.
func (f *File) SignalByID(id string) (*Signal, bool) {
	s, ok := f.byID[id]
	return s, ok
}
