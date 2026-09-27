// Package vcd parses a strict subset of IEEE 1364 Value Change Dump
// files. The parser is deliberately preservation-oriented:
//
//   - the original raw scalar/vector token of every change is retained
//     alongside its decoded four-state bits;
//   - the $timescale directive is retained verbatim with a normalized
//     integer picosecond resolution plus the conversion diagnostic (if
//     any); values that cannot be converted stay in place;
//   - only value changes are stored; between two timestamps the signal
//     holds the last driven value (VCD semantics).
package vcd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"signalsword/pkg/fourval"
)

// Change is one value-change record at a VCD timestamp.
type Change struct {
	TimeTicks int64  // timestamp exactly as written in the VCD (time unit)
	Code      string // VCD identifier code, verbatim
	Raw       string // raw token, e.g. "1", "bxxz ", "bx"
	Bits      []fourval.Value
	IsVector  bool
}

// Signal is a declared $var.
type Signal struct {
	Code string
	Name string // full hierarchical name, e.g. tb.clk
	Kind string // wire/reg/...
	Size int
}

// Timescale records the $timescale directive. TicksPerSecond is the
// normalized integer scale used by the derived layer; Diagnostics lists
// anything that could not be converted (the raw text is always kept).
type Timescale struct {
	Raw            string
	Number         int64
	Unit           string // s, ms, us, ns, ps, fs
	TicksPerSecond int64
	Diagnostics    []string
}

// File is the parsed, preservation-oriented view of a VCD.
type File struct {
	Raw       []byte // original bytes, retained for provenance
	Timescale Timescale
	Signals   []Signal
	Changes   []Change // in file order; timestamps nondecreasing
	MaxTick   int64
	codeIdx   map[string]int
}

// unitScale converts a timescale unit string to ticks-per-second or 0.
func unitScale(unit string) int64 {
	switch strings.TrimSpace(unit) {
	case "s":
		return 1
	case "ms":
		return 1_000
	case "us":
		return 1_000_000
	case "ns":
		return 1_000_000_000
	case "ps":
		return 1_000_000_000_000
	case "fs":
		return 1_000_000_000_000_000
	}
	return 0
}

// Parse reads a VCD. The original byte slice is referenced unchanged so
// callers can always recover exactly what was imported.
func Parse(raw []byte) (*File, error) {
	f := &File{Raw: raw, codeIdx: map[string]int{}}
	r := bufio.NewReader(bytes.NewReader(raw))

	var (
		currentTime int64
		codeToSig   = map[string]*Signal{}
	)
	for {
		tok, err := nextToken(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch tok {
		case "$timescale":
			ts, err := readUntilEnd(r)
			if err != nil {
				return nil, err
			}
			f.Timescale = parseTimescale(strings.TrimSpace(ts))
		case "$var":
			parts, err := readTokensUntilEnd(r)
			if err != nil {
				return nil, err
			}
			// $var wire 1 ! clk [msb:lsb] $end
			if len(parts) < 3 {
				return nil, fmt.Errorf("vcd: malformed $var: %v", parts)
			}
			size, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("vcd: bad $var size %q", parts[1])
			}
			code := parts[2]
			name := parts[3]
			sig := Signal{Code: code, Name: name, Kind: parts[0], Size: size}
			if _, exists := codeToSig[code]; !exists {
				codeToSig[code] = &sig
				idx := len(f.Signals)
				f.Signals = append(f.Signals, sig)
				f.codeIdx[code] = idx
			}
		case "#":
			// handled below as part of timestamp
			return nil, fmt.Errorf("vcd: unexpected bare #")
		case "$enddefinitions":
			if _, err := readUntilEnd(r); err != nil {
				return nil, err
			}
		case "$dumpvars", "$dumpon", "$dumpoff", "$dumpall", "$comment",
			"$date", "$version", "$scope", "$upscope":
			// Skip directive body up to its $end; value changes inside
			// dump sections are instead tokenized individually below
			// because these sections contain bare values followed by
			// $end. Value-bearing dump sections start with values on the
			// next token, so only non-dump directives skip to $end here.
			if tok != "$dumpvars" && tok != "$dumpon" &&
				tok != "$dumpoff" && tok != "$dumpall" {
				if _, err := readUntilEnd(r); err != nil {
					return nil, err
				}
			}
		case "$end":
			// closes a dump section
		default:
			if strings.HasPrefix(tok, "#") {
				t, err := strconv.ParseInt(tok[1:], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("vcd: bad timestamp %q", tok)
				}
				currentTime = t
				if t > f.MaxTick {
					f.MaxTick = t
				}
				continue
			}
			ch, consumed, err := parseChangeToken(r, tok)
			if err != nil {
				// Skip unknown dump section contents conservatively.
				return nil, err
			}
			ch.TimeTicks = currentTime
			f.Changes = append(f.Changes, ch)
			_ = consumed
		}
	}
	return f, nil
}

// parseChangeToken handles both scalar (!) tokens ("1 !") and vector
// tokens ("b101 "). The scanner already split on whitespace; vectors are
// reassembled when the first token starts with b/B/r/R/x/X/z/Z followed
// by digits/x/z without a separate code.
func parseChangeToken(r *bufio.Reader, tok string) (Change, bool, error) {
	if len(tok) >= 2 {
		switch tok[0] {
		case 'b', 'B', 'r', 'R':
			bits, err := decodeBits(tok[1:])
			if err != nil {
				return Change{}, false, err
			}
			code, err := nextToken(r)
			if err != nil {
				return Change{}, false, err
			}
			return Change{Code: code, Raw: tok + " " + code, Bits: bits, IsVector: true}, true, nil
		}
	}
	if len(tok) > 0 {
		switch tok[0] {
		case '0', '1', 'x', 'X', 'z', 'Z':
			v, err := fourval.Parse(tok[0])
			if err != nil {
				return Change{}, false, err
			}
			if len(tok) < 2 {
				return Change{}, false, fmt.Errorf("vcd: scalar value without code: %q", tok)
			}
			return Change{Code: tok[1:], Raw: tok, Bits: []fourval.Value{v}, IsVector: false}, true, nil
		}
	}
	return Change{}, false, fmt.Errorf("vcd: unrecognized token %q", tok)
}

func decodeBits(s string) ([]fourval.Value, error) {
	s = strings.TrimSpace(s)
	bits := make([]fourval.Value, 0, len(s))
	for i := len(s) - 1; i >= 0; i-- { // VCD MSB-first; store LSB-first
		v, err := fourval.Parse(s[i])
		if err != nil {
			return nil, fmt.Errorf("vcd: bad vector bit %q in %q", s[i], s)
		}
		bits = append(bits, v)
	}
	return bits, nil
}

// nextToken returns the next whitespace-separated token. Timestamps like
// "#123" are returned as a single token.
func nextToken(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		c, err := r.ReadByte()
		if err != nil {
			if b.Len() == 0 {
				return "", err
			}
			return b.String(), nil
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if b.Len() > 0 {
				return b.String(), nil
			}
			continue
		}
		b.WriteByte(c)
	}
}

// readUntilEnd reads raw text up to a standalone $end keyword.
func readUntilEnd(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		tok, err := nextToken(r)
		if err != nil {
			return "", err
		}
		if tok == "$end" {
			return b.String(), nil
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(tok)
	}
}

// readTokensUntilEnd reads whitespace-separated tokens until $end.
func readTokensUntilEnd(r *bufio.Reader) ([]string, error) {
	var out []string
	for {
		tok, err := nextToken(r)
		if err != nil {
			return nil, err
		}
		if tok == "$end" {
			return out, nil
		}
		out = append(out, tok)
	}
}

func parseTimescale(raw string) Timescale {
	ts := Timescale{Raw: raw}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		ts.Diagnostics = append(ts.Diagnostics, "empty $timescale")
		return ts
	}
	numUnit := fields[0]
	// Split trailing unit letters from leading number.
	numPart, unitPart := splitNumberUnit(numUnit)
	if numPart == "" && len(fields) > 1 {
		numPart = fields[0]
		unitPart = fields[1]
	}
	if unitPart == "" && len(fields) > 1 {
		unitPart = fields[1]
	}
	n, err := strconv.ParseInt(numPart, 10, 64)
	if err != nil || n <= 0 {
		ts.Diagnostics = append(ts.Diagnostics, fmt.Sprintf("cannot convert timescale number %q; raw value retained", numPart))
		return ts
	}
	scale := unitScale(unitPart)
	if scale == 0 {
		ts.Diagnostics = append(ts.Diagnostics, fmt.Sprintf("unknown timescale unit %q; raw value retained", unitPart))
		ts.Number = n
		ts.Unit = unitPart
		return ts
	}
	ts.Number = n
	ts.Unit = unitPart
	ts.TicksPerSecond = scale / n
	return ts
}

func splitNumberUnit(s string) (string, string) {
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
		i++
	}
	return s[:i], s[i:]
}

// SignalByName returns the declared signal with the hierarchical name.
func (f *File) SignalByName(name string) (Signal, bool) {
	for _, sig := range f.Signals {
		if sig.Name == name {
			return sig, true
		}
	}
	return Signal{}, false
}

// ValueAt resolves the held four-state value of a signal at tick t using
// last-change-wins semantics. Before the first change the value is X.
func (f *File) ValueAt(code string, t int64) fourval.Value {
	held := fourval.X
	for _, ch := range f.Changes {
		if ch.TimeTicks > t {
			break
		}
		if ch.Code != code {
			continue
		}
		held = ch.Bits[0]
	}
	return held
}
