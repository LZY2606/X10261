// Package vcd parses Value Change Dump files. The parser preserves the raw
// representation of every value change and the timescale exactly as written;
// a normalized femtosecond coordinate is derived on top. Values that cannot be
// converted stay on the change with a diagnostic instead of being dropped.
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/example/signal-court/internal/fourval"
	"github.com/example/signal-court/internal/ident"
)

// Timescale is the original $timescale declaration plus the derived factor to
// femtoseconds: timeFs = rawTimestamp * unitToFs.
type Timescale struct {
	Raw      string `json:"raw"`
	Number   int64  `json:"number"`
	Unit     string `json:"unit"` // s/ms/us/ns/ps/fs
	UnitToFs int64  `json:"unitToFs"`
}

// Signal is a declared VCD identifier with its reference name and width.
type Signal struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Width int    `json:"width"`
}

// Change is one value-change record. Raw is the exact VCD token; Bits holds
// per-bit four-state values (MSB first) when parseable. Diag records problems
// without removing the change.
type Change struct {
	TimeRaw string          `json:"timeRaw"`
	TimeFs  int64           `json:"timeFs"`
	Signal  string          `json:"signal"`
	Raw     string          `json:"raw"`
	Bits    []fourval.Value `json:"bits,omitempty"`
	Diag    string          `json:"diag,omitempty"`
	Key     string          `json:"key"`
}

// File is the parse result: provenance first, normalized view second.
type File struct {
	Timescale Timescale `json:"timescale"`
	Signals   []Signal  `json:"signals"`
	Changes   []Change  `json:"changes"`
	RawHash   string    `json:"rawHash"`
}

var unitFactors = map[string]int64{
	"s": 1_000_000_000_000_000,
	"ms": 1_000_000_000_000,
	"us": 1_000_000_000,
	"ns": 1_000_000,
	"ps": 1_000,
	"fs": 1,
}

// Parse reads a VCD. rawHash identifies the exact source bytes and stabilizes
// change identities across import/export/import.
func Parse(r io.Reader, rawHash string) (*File, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	f := &File{RawHash: rawHash}
	signalsByID := map[string]Signal{}
	var currentRaw string
	var currentFs int64
	var inDirective bool
	var dirTokens []string

	finishDirective := func(tokens []string) error {
		if len(tokens) == 0 {
			return nil
		}
		switch tokens[0] {
		case "timescale":
			ts, err := parseTimescale(tokens[1:])
			if err != nil {
				return err
			}
			f.Timescale = ts
		case "var":
			// var <type> <width> <id> <name>
			if len(tokens) >= 5 {
				w, err := strconv.Atoi(tokens[2])
				if err != nil || w <= 0 {
					return fmt.Errorf("vcd: bad width in $var: %q", strings.Join(tokens, " "))
				}
				id, name := tokens[3], tokens[4]
				if _, ok := signalsByID[id]; !ok {
					s := Signal{ID: id, Name: name, Width: w}
					signalsByID[id] = s
					f.Signals = append(f.Signals, s)
				}
			}
		}
		return nil
	}
	addChange := func(tok string) {
		id, raw, isVec, ok := splitValue(tok)
		if !ok || id == "" {
			return
		}
		sig, declared := signalsByID[id]
		if !declared {
			sig = Signal{ID: id, Name: id, Width: 1}
			signalsByID[id] = sig
			f.Signals = append(f.Signals, sig)
		}
		bits, diag := decodeValue(raw, isVec, sig.Width)
		if !declared {
			diag = joinDiag(diag, fmt.Sprintf("change for undeclared identifier %q kept raw", id))
		}
		f.Changes = append(f.Changes, Change{
			TimeRaw: currentRaw, TimeFs: currentFs, Signal: id, Raw: tok,
			Bits: bits, Diag: diag, Key: ident.Parts("change", rawHash, currentRaw, id, tok),
		})
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if inDirective {
			if i := strings.Index(line, "$end"); i >= 0 {
				head := strings.TrimSpace(line[:i])
				if head != "" {
					dirTokens = append(dirTokens, strings.Fields(head)...)
				}
				if err := finishDirective(dirTokens); err != nil {
					return nil, err
				}
				dirTokens = nil
				inDirective = false
			} else {
				dirTokens = append(dirTokens, strings.Fields(line)...)
			}
			continue
		}
		if strings.HasPrefix(line, "$") {
			name := strings.Fields(strings.TrimPrefix(line, "$"))
			if strings.Contains(line, "$end") {
				// Single-line directive: $timescale 1ns $end.
				body := strings.TrimSpace(strings.TrimPrefix(line, "$"))
				if i := strings.Index(body, "$end"); i >= 0 {
					body = strings.TrimSpace(body[:i])
				}
				tokens := strings.Fields(body)
				if err := finishDirective(tokens); err != nil {
					return nil, err
				}
			} else {
				inDirective = true
				dirTokens = append(dirTokens[:0], name...)
			}
			continue
		}
		if strings.HasPrefix(line, "#") && !strings.ContainsAny(line, " \t") {
			if f.Timescale.UnitToFs == 0 {
				return nil, fmt.Errorf("vcd: timestamp %s before $timescale", line)
			}
			currentRaw = line[1:]
			t, err := strconv.ParseInt(currentRaw, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd: bad time %q: %w", line, err)
			}
			currentFs = t * f.Timescale.UnitToFs
			continue
		}
		if isValueStart(line) {
			addChange(line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if inDirective {
		return nil, fmt.Errorf("vcd: unterminated directive")
	}
	if f.Timescale.Raw == "" {
		return nil, fmt.Errorf("vcd: missing $timescale")
	}
	return f, nil
}

func joinDiag(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

func isValueStart(tok string) bool {
	if tok == "" {
		return false
	}
	switch tok[0] {
	case '0', '1', 'x', 'X', 'z', 'Z', 'b', 'B', 'r', 'R':
		return true
	}
	return false
}

func parseTimescale(fields []string) (Timescale, error) {
	joined := strings.Join(fields, "")
	numPart, unitPart := splitNumberUnit(joined)
	if numPart == "" || unitPart == "" {
		return Timescale{}, fmt.Errorf("vcd: bad timescale %q", joined)
	}
	num, err := strconv.ParseInt(numPart, 10, 64)
	if err != nil {
		return Timescale{}, fmt.Errorf("vcd: bad timescale number %q", joined)
	}
	factor, ok := unitFactors[unitPart]
	if !ok {
		return Timescale{}, fmt.Errorf("vcd: unsupported timescale unit %q", unitPart)
	}
	return Timescale{Raw: numPart + unitPart, Number: num, Unit: unitPart, UnitToFs: factor}, nil
}

func splitNumberUnit(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i], s[i:]
}

// splitValue separates the value token from the identifier. Scalar: "0!";
// vector: "b1010 clk".
func splitValue(tok string) (id, raw string, isVec, ok bool) {
	switch tok[0] {
	case 'b', 'B', 'r', 'R':
		parts := strings.Fields(tok)
		if len(parts) != 2 {
			return "", "", true, false
		}
		return parts[1], parts[0][1:], true, true
	default:
		if len(tok) < 2 {
			return "", "", false, false
		}
		return tok[1:], string(tok[0]), false, true
	}
}

func decodeValue(raw string, isVec bool, declaredWidth int) ([]fourval.Value, string) {
	if !isVec {
		v, err := fourval.Parse(raw)
		if err != nil {
			return nil, fmt.Sprintf("unconvertible value %q kept in place", raw)
		}
		return []fourval.Value{v}, ""
	}
	width := declaredWidth
	if width <= 0 {
		width = len(raw)
	}
	bits := make([]fourval.Value, width)
	for i := range bits {
		bits[i] = fourval.X
	}
	var diag string
	if len(raw) != width {
		diag = fmt.Sprintf("vector width mismatch: declared %d, raw %q (kept verbatim)", width, raw)
	}
	start := width - len(raw)
	if start < 0 {
		bits = make([]fourval.Value, len(raw))
		for i := range bits {
			bits[i] = fourval.X
		}
		start = 0
	}
	for i, ch := range raw {
		pos := start + i
		v, err := fourval.Parse(string(ch))
		if err != nil {
			diag = joinDiag(diag, fmt.Sprintf("unconvertible digit %q kept in raw token %q", string(ch), raw))
			continue
		}
		bits[pos] = v
	}
	return bits, diag
}
