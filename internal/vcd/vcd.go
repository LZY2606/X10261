// Package vcd parses Value Change Dump files, preserving raw timestamp
// precision and four-state values exactly as written.
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Change is a raw value change: Time is in raw timescale ticks (not normalized).
type Change struct {
	Time  int64
	Code  string
	Value string // four-state string, raw from file
}

// Signal describes one declared VCD identifier.
type Signal struct {
	Code  string
	Name  string
	Width int
}

// File is a parsed VCD document.
type File struct {
	TimescaleNum  int64  // raw number, e.g. 1
	TimescaleUnit string // raw unit, e.g. "ns"
	Signals       []Signal
	Changes       []Change // in file order
}

// Parse reads a VCD stream. Malformed input returns an error and nothing
// is partially committed by callers (they import transactionally).
func Parse(r io.Reader) (*File, error) {
	f := &File{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	byCode := map[string]Signal{}
	inHeader := true
	var curTime int64
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if s == "" {
			continue
		}
		if inHeader {
			switch {
			case strings.HasPrefix(s, "$timescale"):
				rest := strings.TrimSpace(strings.TrimPrefix(s, "$timescale"))
				if strings.HasSuffix(rest, "$end") {
					rest = strings.TrimSpace(strings.TrimSuffix(rest, "$end"))
				}
				if rest == "" { // value on next line
					for sc.Scan() {
						line++
						rest = strings.TrimSpace(sc.Text())
						if rest != "" {
							break
						}
					}
				}
				var num int64
				var unit string
				if _, err := fmt.Sscanf(rest, "%d%s", &num, &unit); err != nil {
					return nil, fmt.Errorf("vcd line %d: bad timescale %q", line, rest)
				}
				f.TimescaleNum = num
				f.TimescaleUnit = unit
			case strings.HasPrefix(s, "$var"):
				// $var wire 4 a data [3:0] $end
				fields := strings.Fields(s)
				if len(fields) < 5 {
					return nil, fmt.Errorf("vcd line %d: bad $var %q", line, s)
				}
				var width int
				if _, err := fmt.Sscanf(fields[2], "%d", &width); err != nil {
					return nil, fmt.Errorf("vcd line %d: bad width in %q", line, s)
				}
				sig := Signal{Code: fields[3], Name: fields[4], Width: width}
				f.Signals = append(f.Signals, sig)
				byCode[sig.Code] = sig
			case strings.HasPrefix(s, "$enddefinitions"):
				inHeader = false
			}
			continue
		}
		switch s[0] {
		case '#':
			var t int64
			if _, err := fmt.Sscanf(s[1:], "%d", &t); err != nil {
				return nil, fmt.Errorf("vcd line %d: bad timestamp %q", line, s)
			}
			curTime = t
		case '0', '1', 'x', 'X', 'z', 'Z':
			if len(s) < 2 {
				return nil, fmt.Errorf("vcd line %d: bad scalar change %q", line, s)
			}
			code := s[1:]
			if _, ok := byCode[code]; !ok {
				return nil, fmt.Errorf("vcd line %d: unknown code %q", line, code)
			}
			f.Changes = append(f.Changes, Change{Time: curTime, Code: code, Value: strings.ToLower(s[:1])})
		case 'b', 'B':
			fields := strings.Fields(s)
			if len(fields) != 2 {
				return nil, fmt.Errorf("vcd line %d: bad vector change %q", line, s)
			}
			if _, ok := byCode[fields[1]]; !ok {
				return nil, fmt.Errorf("vcd line %d: unknown code %q", line, fields[1])
			}
			f.Changes = append(f.Changes, Change{Time: curTime, Code: fields[1], Value: strings.ToLower(fields[0][1:])})
		case '$':
			// $dumpvars etc.: values inside handled by the scalar/vector arms
		default:
			return nil, fmt.Errorf("vcd line %d: unrecognized record %q", line, s)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(f.Signals) == 0 {
		return nil, fmt.Errorf("vcd: no signals declared")
	}
	return f, nil
}
