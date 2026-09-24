// Package vcd parses the small, fixed subset of VCD used by the case pack:
// $timescale, $var wire/reg, $enddefinitions and scalar/vector value changes.
// Raw tokens are preserved on every parsed record; normalization happens only
// in derived layers.
package vcd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gsb/internal/domain"
)

type ParseResult struct {
	Timescale   domain.Timescale
	Signals     []domain.Signal
	Changes     []domain.Change
	Diagnostics []domain.Diagnostic
	EndTick     int64
}

type tokenizer struct {
	src  []byte
	pos  int
	line int
}

func (t *tokenizer) skipSpace() {
	for t.pos < len(t.src) {
		c := t.src[t.pos]
		if c == ' ' || c == '\t' || c == '\r' {
			t.pos++
		} else if c == '\n' {
			t.line++
			t.pos++
		} else {
			return
		}
	}
}

func (t *tokenizer) token() string {
	t.skipSpace()
	start := t.pos
	for t.pos < len(t.src) {
		c := t.src[t.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			break
		}
		t.pos++
	}
	return string(t.src[start:t.pos])
}

// Parse consumes VCD bytes. A syntax error makes the whole import fail so the
// caller can roll back; unconvertible *values* are kept with diagnostics
// rather than aborting.
func Parse(src []byte) (*ParseResult, error) {
	t := &tokenizer{src: src, line: 1}
	res := &ParseResult{}
	codeToSignal := map[string]int{}
	var tick int64

	// Header until $enddefinitions.
	headerDone := false
	for !headerDone {
		t.skipSpace()
		if t.pos >= len(t.src) {
			return nil, errors.New("vcd: unexpected end of file before $enddefinitions")
		}
		if t.src[t.pos] == '$' {
			kw := t.token()
			switch kw {
			case "$timescale":
				ts, err := readTimescale(t)
				if err != nil {
					return nil, err
				}
				res.Timescale = ts
			case "$var":
				sig, err := readVar(t)
				if err != nil {
					return nil, err
				}
				if _, dup := codeToSignal[sig.Code]; !dup {
					codeToSignal[sig.Code] = len(res.Signals)
					res.Signals = append(res.Signals, sig)
				}
			case "$comment":
				if err := skipDirective(t); err != nil {
					return nil, err
				}
			case "$scope", "$upscope", "$version", "$date":
				if err := skipDirective(t); err != nil {
					return nil, err
				}
			case "$enddefinitions":
				// consume $end
				if end := t.token(); end != "$end" {
					return nil, fmt.Errorf("vcd: expected $end after $enddefinitions, got %q", end)
				}
				headerDone = true
			default:
				return nil, fmt.Errorf("vcd: unsupported header directive %q (line %d)", kw, t.line)
			}
			continue
		}
		return nil, fmt.Errorf("vcd: unexpected token %q in header (line %d)", t.token(), t.line)
	}

	changeIdx := 0
	for {
		t.skipSpace()
		if t.pos >= len(t.src) {
			break
		}
		tok := t.token()
		if tok == "" {
			break
		}
		if strings.HasPrefix(tok, "$") {
			// Body directives (e.g. $dumpvars) are skipped to their $end.
			if tok == "$end" {
				continue
			}
			if err := skipDirectiveTokens(t, tok); err != nil {
				return nil, err
			}
			continue
		}
		if tok[0] == '#' {
			n, err := strconv.ParseInt(tok[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd: bad time mark %q (line %d)", tok, t.line)
			}
			tick = n
			if tick > res.EndTick {
				res.EndTick = tick
			}
			continue
		}
		// Value change forms:
		//   scalar glued:  "0!"  "x!"
		//   scalar spaced: "0" "!"
		//   vector:        "b10" "!"
		var value, code string
		if tok[0] == 'b' || tok[0] == 'B' || tok[0] == 'r' || tok[0] == 'R' {
			value = tok[1:]
			code = t.token()
		} else if strings.ContainsAny(tok[:1], "01xXzZ") {
			if len(tok) == 1 {
				value = tok
				code = t.token()
			} else {
				value = tok[:1]
				code = tok[1:]
			}
		} else {
			return nil, fmt.Errorf("vcd: unrecognized value-change token %q (line %d)", tok, t.line)
		}
		if code == "" {
			return nil, fmt.Errorf("vcd: value change without identifier near line %d", t.line)
		}
		sigIdx, ok := codeToSignal[code]
		if !ok {
			// Unknown identifier: keep raw value, attach diagnostic, do not abort.
			id := domain.StableID("chg", code, strconv.FormatInt(tick, 10), strconv.Itoa(changeIdx))
			res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
				ID:       domain.StableID("diag", code, strconv.FormatInt(tick, 10), strconv.Itoa(changeIdx)),
				RefID:    id,
				Code:     "unknown_identifier",
				Message:  fmt.Sprintf("value change references undeclared identifier %q", code),
				RawValue: value,
			})
			changeIdx++
			continue
		}
		sig := res.Signals[sigIdx]
		ch := domain.Change{
			ID:       domain.StableID("chg", code, strconv.FormatInt(tick, 10), strconv.Itoa(changeIdx)),
			Code:     code,
			SignalID: sig.ID,
			Index:    changeIdx,
			AtTick:   tick,
			RawValue: value,
			Width:    sig.Width,
		}
		bits, nonCanon, diag := decodeBits(value, sig.Width)
		ch.Bits = bits
		ch.NonCanon = nonCanon
		res.Changes = append(res.Changes, ch)
		if diag.Message != "" {
			diag.ID = domain.StableID("diag", ch.ID, diag.Code)
			diag.RefID = ch.ID
			res.Diagnostics = append(res.Diagnostics, diag)
		}
		changeIdx++
	}
	return res, nil
}

func decodeBits(raw string, width int) ([]domain.Logic, bool, domain.Diagnostic) {
	diag := domain.Diagnostic{}
	if len(raw) == 0 {
		diag.Code = "empty_value"
		diag.Message = "value change carries no bits"
		diag.RawValue = raw
		return nil, true, diag
	}
	nonCanon := false
	bits := make([]domain.Logic, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		b, ok := domain.ParseLogic(raw[i])
		if !ok {
			nonCanon = true
		}
		bits = append(bits, b)
	}
	if width > 0 && len(bits) != width {
		diag.Code = "width_mismatch"
		diag.Message = fmt.Sprintf("raw value has %d bit(s), declaration width is %d; kept at original position", len(bits), width)
		diag.RawValue = raw
	}
	return bits, nonCanon, diag
}

func readTimescale(t *tokenizer) (domain.Timescale, error) {
	ts := domain.Timescale{}
	numTok := t.token()
	unit := t.token()
	if end := t.token(); end != "$end" {
		return ts, fmt.Errorf("vcd: expected $end after $timescale, got %q", end)
	}
	ts.Raw = numTok + " " + unit
	n, err := strconv.Atoi(numTok)
	if err != nil {
		// Keep raw representation; mark non-convertible.
		ts.Number, ts.BaseFS = 0, 0
		return ts, nil
	}
	ts.Number = n
	ts.Unit = unit
	mult, ok := timescaleUnitFS(unit)
	if !ok {
		return ts, nil
	}
	ts.Convertible = true
	ts.BaseFS = int64(n) * mult
	return ts, nil
}

func timescaleUnitFS(unit string) (int64, bool) {
	switch unit {
	case "fs":
		return 1, true
	case "ps":
		return 1_000, true
	case "ns":
		return 1_000_000, true
	case "us":
		return 1_000_000_000, true
	case "ms":
		return 1_000_000_000_000, true
	case "s":
		return 1_000_000_000_000_000, true
	default:
		return 0, false
	}
}

func readVar(t *tokenizer) (domain.Signal, error) {
	sig := domain.Signal{}
	kind := t.token() // wire | reg
	if kind != "wire" && kind != "reg" {
		return sig, fmt.Errorf("vcd: unsupported $var kind %q (line %d)", kind, t.line)
	}
	widthTok := t.token()
	w, err := strconv.Atoi(widthTok)
	if err != nil {
		return sig, fmt.Errorf("vcd: bad signal width %q (line %d)", widthTok, t.line)
	}
	code := t.token()
	name := t.token()
	if end := t.token(); end != "$end" {
		return sig, fmt.Errorf("vcd: expected $end after $var, got %q", end)
	}
	sig.Code = code
	sig.Name = name
	sig.RawName = name
	sig.Width = w
	sig.ID = domain.StableID("sig", code, name, widthTok)
	return sig, nil
}

func skipDirective(t *tokenizer) error {
	for {
		tok := t.token()
		if tok == "" {
			return errors.New("vcd: unexpected end of file inside directive")
		}
		if tok == "$end" {
			return nil
		}
	}
}

func skipDirectiveTokens(t *tokenizer, first string) error {
	if first == "$end" {
		return nil
	}
	return skipDirective(t)
}
