// Package vcd parses Value Change Dump files.
//
// The parser is deliberately non-destructive: the original raw time token and
// the original raw value token are kept on every change. Decoded four-state
// values are stored alongside them. Anything that cannot be decoded stays at
// its original position with a diagnostic instead of being dropped.
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"signalcourt/internal/logic"
)

// Diagnostic is a non-fatal note attached to an exact source location.
type Diagnostic struct {
	SourceID string `json:"source_id"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	Raw      string `json:"raw,omitempty"`
}

// Timescale keeps both the raw declaration and the femtosecond multiplier.
type Timescale struct {
	Raw      string `json:"raw"`
	UnitFS   int64  `json:"unit_fs"`
	UnitName string `json:"unit_name"`
	Number   int64  `json:"number"`
}

// Change is one value change record. RawTime is the exact "#n" token body;
// TimeFS is derived in the declared timescale and is int64 femtoseconds.
type Change struct {
	RawTime    string      `json:"raw_time"`
	TimeFS     int64       `json:"time_fs"`
	RawValue   string      `json:"raw_value"`
	Value      logic.Value `json:"value"`
	Decodable  bool        `json:"decodable"`
	Diagnostic string      `json:"diagnostic,omitempty"`
}

// Signal is one declared variable and its ordered change stream.
type Signal struct {
	VCDID   string    `json:"vcd_id"`
	Name    string    `json:"name"`
	Width   int       `json:"width"`
	Aliases []string  `json:"aliases,omitempty"`
	Changes []Change  `json:"changes"`
}

// Source is the parsed result for one VCD byte stream.
type Source struct {
	ID         string       `json:"id"`
	Filename   string       `json:"filename"`
	Timescale  Timescale    `json:"timescale"`
	Signals    []*Signal    `json:"signals"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	EndTimeFS  int64        `json:"end_time_fs"`
}

var unitFS = map[string]int64{
	"s":  1_000_000_000_000_000,
	"ms": 1_000_000_000_000,
	"us": 1_000_000_000,
	"µs": 1_000_000_000,
	"ns": 1_000_000,
	"ps": 1_000,
	"fs": 1,
}

type parser struct {
	src       *Source
	sc        *bufio.Scanner
	line      int
	byID      map[string]*Signal
	curTime   int64
	curRaw    string
	inDump    bool
}

// Parse reads one VCD stream. id and filename are provenance labels.
// A returned error is fatal: the caller must roll the whole import back.
func Parse(r io.Reader, id, filename string) (*Source, error) {
	p := &parser{
		src:    &Source{ID: id, Filename: filename},
		byID:   map[string]*Signal{},
		curRaw: "0",
	}
	p.src.Signals = nil
	p.sc = bufio.NewScanner(r)
	p.sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for p.sc.Scan() {
		p.line++
		line := strings.TrimSpace(p.sc.Text())
		if line == "" {
			continue
		}
		if err := p.line_(line); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", filename, p.line, err)
		}
	}
	if err := p.sc.Err(); err != nil {
		return nil, err
	}
	if p.src.Timescale.UnitFS == 0 {
		return nil, fmt.Errorf("%s: missing or unparseable $timescale", filename)
	}
	for _, sig := range p.src.Signals {
		if len(sig.Changes) > 0 {
			last := sig.Changes[len(sig.Changes)-1].TimeFS
			if last > p.src.EndTimeFS {
				p.src.EndTimeFS = last
			}
		}
	}
	return p.src, nil
}

func (p *parser) diag(kind, msg, raw string) {
	p.src.Diagnostics = append(p.src.Diagnostics, Diagnostic{
		SourceID: p.src.ID, Line: p.line, Kind: kind, Message: msg, Raw: raw,
	})
}

func (p *parser) line_(line string) error {
	// A logical line may contain several tokens and even a directive.
	for line != "" {
		line = strings.TrimSpace(line)
		if line == "" {
			return nil
		}
		switch {
		case strings.HasPrefix(line, "#"):
			num := line
			if sp := strings.IndexAny(line, " \t"); sp >= 0 {
				num = line[:sp]
				line = line[sp:]
			} else {
				line = ""
			}
			body := strings.TrimPrefix(num, "#")
			n, err := strconv.ParseInt(body, 10, 64)
			if err != nil {
				return fmt.Errorf("bad time token %q", num)
			}
			p.curRaw = body
			p.curTime = n
		case strings.HasPrefix(line, "$"):
			consumed, err := p.directive(line)
			if err != nil {
				return err
			}
			if consumed < 0 {
				return nil // scanner will continue; directive consumed whole physical line
			}
			line = line[consumed:]
		case line == "$dumpvars" || line == "$dumpall" || line == "$dumpon" || line == "$dumpoff" || line == "$end":
			p.inDump = line != "$end"
			line = ""
		default:
			consumed := p.valueChange(line)
			if consumed <= 0 {
				// Skip an unrecognised token but record it for the audit trail.
				p.diag("unparsed_token", "token retained but not interpreted", firstToken(line))
				if sp := strings.IndexAny(line, " \t"); sp >= 0 {
					line = line[sp:]
				} else {
					line = ""
				}
			} else {
				line = line[consumed:]
			}
		}
	}
	return nil
}

func firstToken(line string) string {
	if sp := strings.IndexAny(line, " \t"); sp >= 0 {
		return line[:sp]
	}
	return line
}

// directive processes a $... directive. It returns the number of bytes
// consumed from line; a multiline directive consumes the rest of all input
// via the scanner and returns -1.
func (p *parser) directive(line string) (int, error) {
	// Inline: the physical line contains a standalone "$end" token.
	if endIdx, endTok := endToken(line); endIdx >= 0 {
		body := line[:endIdx+len(endTok)]
		if err := p.directiveBody(body); err != nil {
			return 0, err
		}
		rest := strings.TrimSpace(line[endIdx+len(endTok):])
		if rest != "" {
			p.line = p.line // keep line accounting simple
			// Any tokens after $end on the same line are processed by caller,
			// but we already advanced its scanner state; feed them directly.
			if err := p.line_(rest); err != nil {
				return 0, err
			}
		}
		return -1, nil
	}
	// Multiline: gather until a line containing a standalone "$end" token.
	var sb strings.Builder
	sb.WriteString(line)
	for p.sc.Scan() {
		p.line++
		t := p.sc.Text()
		sb.WriteByte('\n')
		sb.WriteString(t)
		if idx, _ := endToken(t); idx >= 0 {
			if err := p.directiveBody(sb.String()); err != nil {
				return 0, err
			}
			return -1, nil
		}
	}
	return 0, fmt.Errorf("unterminated directive: %s", firstToken(line))
}

// endToken returns the byte offset and text of a standalone "$end" token.
func endToken(line string) (int, string) {
	for i := 0; i+4 <= len(line); i++ {
		if line[i] != '$' {
			continue
		}
		tok := line[i : i+4]
		if tok != "$end" {
			continue
		}
		before := i == 0 || line[i-1] == ' ' || line[i-1] == '\t'
		after := i+4 == len(line) || line[i+4] == ' ' || line[i+4] == '\t'
		if before && after {
			return i, tok
		}
	}
	return -1, ""
}

func (p *parser) directiveBody(body string) error {
	body = strings.TrimSpace(body)
	rest := strings.TrimPrefix(body, "$")
	if sp := strings.IndexAny(rest, " \t\n"); sp >= 0 {
		rest = rest[:sp]
	}
	kw := rest
	switch kw {
	case "timescale":
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(body, "$timescale"), "$end"))
		ts, err := parseTimescale(strings.TrimSpace(inner))
		if err != nil {
			return err
		}
		p.src.Timescale = ts
	case "var":
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(body, "$var"), "$end"))
		f := strings.Fields(inner)
		// $var wire 4 # data [3:0]
		if len(f) < 3 {
			p.diag("bad_var", "malformed $var declaration", body)
			return nil
		}
		width, err := strconv.Atoi(f[1])
		if err != nil || width < 1 {
			p.diag("bad_var_width", "invalid width in $var", body)
			width = 1
		}
		id := f[2]
		name := f[3]
		if s, ok := p.byID[id]; ok {
			s.Aliases = append(s.Aliases, name)
			return nil
		}
		sig := &Signal{VCDID: id, Name: name, Width: width}
		p.byID[id] = sig
		p.src.Signals = append(p.src.Signals, sig)
	case "dumpvars", "dumpall", "dumpon", "dumpoff":
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(body, "$"+kw), "$end"))
		tofs := strings.Fields(inner)
		for k := 0; k < len(tofs); k++ {
			f := tofs[k]
			if (strings.HasPrefix(f, "b") || strings.HasPrefix(f, "B") ||
				strings.HasPrefix(f, "r") || strings.HasPrefix(f, "R")) && k+1 < len(tofs) {
				p.applyChange(f+" "+tofs[k+1], f, tofs[k+1])
				k++
				continue
			}
			if len(f) >= 2 && (f[0] == '0' || f[0] == '1' || f[0] == 'x' || f[0] == 'X' || f[0] == 'z' || f[0] == 'Z') {
				p.applyChange(f, string(f[0]), f[1:])
			}
		}
	case "scope", "upscope", "enddefinitions", "comment", "version", "date":
		// Structural markers carry no values here.
	default:
		p.diag("unknown_directive", "unknown directive skipped", "$"+kw)
	}
	return nil
}

func parseTimescale(s string) (Timescale, error) {
	ts := Timescale{Raw: s}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ts, fmt.Errorf("empty timescale")
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return ts, fmt.Errorf("bad timescale number %q", s)
	}
	unit := fields[len(fields)-1]
	mult, ok := unitFS[unit]
	if !ok {
		return ts, fmt.Errorf("unsupported timescale unit %q", unit)
	}
	ts.Number = n
	ts.UnitName = unit
	ts.UnitFS = n * mult
	return ts, nil
}

// valueChange parses one "value id" pair and returns bytes consumed.
func (p *parser) valueChange(line string) int {
	f := strings.Fields(line)
	if len(f) == 0 {
		return 0
	}
	tok := f[0]
	var valTok, id string
	consumed := len(tok)
	if len(f) >= 2 && (strings.HasPrefix(tok, "b") || strings.HasPrefix(tok, "B") ||
		strings.HasPrefix(tok, "r") || strings.HasPrefix(tok, "R")) {
		valTok, id = tok, f[1]
		consumed += 1 + len(id)
	} else {
		// scalar forms: 0!, 1!, x!, z!  (id glued to the value)
		t := tok
		if len(t) >= 2 && (t[0] == '0' || t[0] == '1' || t[0] == 'x' || t[0] == 'X' || t[0] == 'z' || t[0] == 'Z') {
			valTok, id = t[:1], t[1:]
		} else {
			return 0
		}
	}
	p.applyChange(tok, valTok, id)
	return consumed
}

// applyChange records one value change for id; rawTok is the exact source text.
func (p *parser) applyChange(rawTok, valTok, id string) {
	sig, ok := p.byID[id]
	if !ok {
		p.diag("unknown_signal", "value change for undeclared id", rawTok)
		return
	}
	val, decodable := logic.Decode(valTok, sig.Width)
	ch := Change{
		RawTime:   p.curRaw,
		TimeFS:    p.curTime * p.src.Timescale.UnitFS,
		RawValue:  rawTok,
		Value:     val,
		Decodable: decodable,
	}
	if !decodable {
		ch.Diagnostic = "value could not be decoded as four-state scalar/vector; raw token retained at original position"
		p.diag("undecodable_value", ch.Diagnostic, rawTok)
	}
	sig.Changes = append(sig.Changes, ch)
}
