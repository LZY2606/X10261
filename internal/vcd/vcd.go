// Package vcd parses a deliberately small, well-defined subset of IEEE 1364
// VCD. Raw representations (timescale text, value-change tokens, identifiers)
// are preserved on the parsed structure; normalization to canonical picosecond
// coordinates happens only in the derived interpretation layer.
package vcd

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"signalcourt/internal/fourstate"
)

// Change is one raw value-change at a raw time offset.
type Change struct {
	ID       string // stable content-derived identity
	Seq      int    // 0-based order of appearance at the same time
	RawTime  int64  // time offset in the file's raw timescale units
	RawValue string // exact token text, e.g. "1", "x", "b101x"
	Canon    fourstate.Value
	ValueOK  bool
	SigID    string
}

// Signal is one declared scalar or collapsed vector signal.
type Signal struct {
	ID       string
	Code     string // VCD identifier code
	Name     string // human-readable name from $var
	Kind     string // wire/reg/...
	Width    int
	IsVector bool
}

// TimeScale is the raw timescale plus, when convertible, its canonical
// picosecond factor.
type TimeScale struct {
	Raw      string // exact source text, e.g. "10ns", "1tick"
	Number   int
	Unit     string
	FactorPS int64 // canonical picoseconds per raw unit; 0 when unconvertible
	Diag     string
}

// File is the parse result.
type File struct {
	Changes     []Change
	Signals     []Signal
	Timescale   TimeScale
	Diagnostics []string
}

var unitPS = map[string]int64{
	"s":  1_000_000_000_000,
	"ms": 1_000_000_000,
	"us": 1_000_000,
	"ns": 1_000,
	"ps": 1,
	"fs": 0, // expressible but below the coordinate floor
}

// Parse extracts signals, timescale and value changes from src.
func Parse(src []byte) (*File, error) {
	t := tokenize(src)
	f := &File{}

	sigByCode := map[string]int{}
	rawAt := map[string]int64{}

	var curTime int64
	var timeSeen bool
	seqAtTime := 0

	for i := 0; i < len(t); i++ {
		tok := t[i]
		switch {
		case tok == "$timescale":
			var parts []string
			for i++; i < len(t) && t[i] != "$end"; i++ {
				parts = append(parts, t[i])
			}
			f.Timescale = parseTimescale(strings.Join(parts, " "))
			if f.Timescale.Diag != "" {
				f.Diagnostics = append(f.Diagnostics, f.Timescale.Diag)
			}
		case tok == "$var":
			kind := t[i+1]
			width, _ := strconv.Atoi(t[i+2])
			code := t[i+3]
			name := t[i+4]
			i += 4
			for i+1 < len(t) && t[i+1] != "$end" {
				i++
			}
			i++
			if _, exists := sigByCode[code]; exists {
				continue
			}
			isVec := width > 1
			s := Signal{
				ID:       stableID("sig", name+"|"+code+"|"+strconv.Itoa(width)),
				Code:     code,
				Name:     name,
				Kind:     kind,
				Width:    width,
				IsVector: isVec,
			}
			sigByCode[code] = len(f.Signals)
			f.Signals = append(f.Signals, s)
		case strings.HasPrefix(tok, "#"):
			v, err := strconv.ParseInt(tok[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("bad time token %q: %w", tok, err)
			}
			if timeSeen && v < curTime {
				return nil, fmt.Errorf("non-monotonic time %d after %d", v, curTime)
			}
			curTime = v
			timeSeen = true
			seqAtTime = 0
		case strings.HasPrefix(tok, "$"):
			// Other header directives: skip through $end.
			depth := 0
			if !strings.HasSuffix(tok, "$end") {
				for i++; i < len(t); i++ {
					if t[i] == "$end" {
						depth = 0
						break
					}
					_ = depth
				}
			}
		default:
			rawVal, code, ok := splitChange(tok)
			if !ok {
				if i+1 < len(t) {
					rawVal = tok
					code = t[i+1]
					i++
				} else {
					continue
				}
			}
			si, known := sigByCode[code]
			if !known {
				f.Diagnostics = append(f.Diagnostics,
					fmt.Sprintf("value change for undeclared code %q retained as diagnostic", code))
				continue
			}
			sig := f.Signals[si]
			canon, vok := decodeValue(rawVal, sig.Width)
			key := sig.ID + "@" + strconv.FormatInt(curTime, 10)
			prev := rawAt[key]
			ch := Change{
				ID:       stableID("ch", key+"|"+rawVal+"|"+strconv.Itoa(seqAtTime)),
				Seq:      seqAtTime,
				RawTime:  curTime,
				RawValue: rawVal,
				Canon:    canon,
				ValueOK:  vok,
				SigID:    sig.ID,
			}
			seqAtTime++
			_ = prev
			f.Changes = append(f.Changes, ch)
			rawAt[key] = curTime
		}
	}
	if !timeSeen {
		return nil, fmt.Errorf("no value-change time markers found")
	}
	return f, nil
}

func tokenize(src []byte) []string {
	var out []string
	var sb strings.Builder
	flush := func() {
		if sb.Len() > 0 {
			out = append(out, sb.String())
			sb.Reset()
		}
	}
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case ' ', '\t', '\r', '\n':
			flush()
		case '$':
			flush()
			start := i
			for i+1 < len(src) && src[i+1] != ' ' && src[i+1] != '\t' &&
				src[i+1] != '\n' && src[i+1] != '\r' {
				i++
			}
			out = append(out, string(src[start:i+1]))
		default:
			sb.WriteByte(c)
		}
	}
	flush()
	return out
}

func parseTimescale(s string) TimeScale {
	ts := TimeScale{Raw: s}
	s = strings.TrimSpace(s)
	n := 0
	for n < len(s) && (s[n] >= '0' && s[n] <= '9') {
		n++
	}
	if n == 0 {
		ts.Diag = fmt.Sprintf("timescale %q has no numeric factor; left at origin", s)
		return ts
	}
	num, err := strconv.Atoi(s[:n])
	if err != nil {
		ts.Diag = fmt.Sprintf("timescale %q unparseable: %v", s, err)
		return ts
	}
	unit := strings.TrimSpace(s[n:])
	ts.Number = num
	ts.Unit = unit
	fac, known := unitPS[unit]
	if !known {
		ts.Diag = fmt.Sprintf("timescale unit %q cannot convert to ps; raw positions retained", unit)
		return ts
	}
	if unit == "fs" {
		ts.Diag = fmt.Sprintf("timescale %s is below the 1ps coordinate floor; raw positions retained", s)
		return ts
	}
	ts.FactorPS = int64(num) * fac
	return ts
}

// splitChange handles compact scalar tokens like "1!", "x#".
func splitChange(tok string) (value, code string, ok bool) {
	if len(tok) < 2 {
		return "", "", false
	}
	switch tok[0] {
	case '0', '1', 'x', 'X', 'z', 'Z':
		return string(tok[0]), tok[1:], true
	case 'b', 'B', 'r', 'R':
		return "", "", false
	}
	return "", "", false
}

func decodeValue(raw string, width int) (fourstate.Value, bool) {
	if len(raw) == 0 {
		return fourstate.VX, false
	}
	switch raw[0] {
	case 'b', 'B':
		return fourstate.ParseVector(raw[1:], width)
	case 'r', 'R':
		return fourstate.VX, false
	}
	v, ok := fourstate.FromVCDChar(raw[0])
	return v, ok
}

// CanonicalPS converts a raw time offset to picoseconds. ok is false when the
// timescale is unconvertible; the caller must keep the value at its origin
// with a diagnostic instead of fabricating a coordinate.
func (f *File) CanonicalPS(raw int64) (int64, bool) {
	if f.Timescale.FactorPS == 0 {
		return 0, false
}
	return raw * f.Timescale.FactorPS, true
}

func stableID(kind, content string) string {
	sum := sha1.Sum([]byte(content))
	return kind + "_" + hex.EncodeToString(sum[:10])
}
