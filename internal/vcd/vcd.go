// Package vcd parses a small, well-defined subset of Value Change Dump:
// one module, scalar (1-bit) wires/regs, $timescale, $dumpvars and
// timestamped value changes. The parser is lossless for what it imports:
// original tokens, byte offsets and raw VCD values are retained; tokens
// that cannot be interpreted stay in place with a diagnostic rather than
// being silently dropped or coerced.
package vcd

import (
	"fmt"
	"strings"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/timescale"
)

// Signal is an imported scalar signal definition.
type Signal struct {
	ID        string `json:"id"`         // VCD identifier code
	Name      string `json:"name"`       // hierarchical short name
	Kind      string `json:"kind"`       // wire/reg as declared
	Width     int    `json:"width"`      // declared width
	WidthText string `json:"width_text"` // raw width token
	RawDecl   string `json:"raw_decl"`   // raw $var line
}

// Change is one raw value change at a raw tick coordinate.
type Change struct {
	Tick      int64           `json:"tick"`
	Offset    int64           `json:"offset"`     // byte offset of the token in the source
	SignalID  string          `json:"signal_id"`
	Value     fourstate.V4    `json:"value"`      // interpreted 4-state value
	RawToken  string          `json:"raw_token"`  // exact token from the VCD
	Initial   bool            `json:"initial"`    // from $dumpvars
	OK        bool            `json:"ok"`         // token was interpretable
	Diagnostic string         `json:"diagnostic,omitempty"`
}

// Diagnostic records a non-fatal parse problem at a byte offset.
type Diagnostic struct {
	Offset int64  `json:"offset"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
}

// Waveform is the imported, still-raw waveform material.
type Waveform struct {
	SourceName  string               `json:"source_name"`
	RawBytes    []byte               `json:"-"`
	RawSHA256   string               `json:"raw_sha256"`
	Timescale   timescale.Scale      `json:"timescale"`
	Date        string               `json:"date"`
	Version     string               `json:"version"`
	Signals     []Signal             `json:"signals"`
	Changes     []Change             `json:"changes"`
	Diagnostics []Diagnostic         `json:"diagnostics"`
}

// Signal finds a signal by name.
func (w *Waveform) Signal(name string) (Signal, bool) {
	for _, s := range w.Signals {
		if s.Name == name {
			return s, true
		}
	}
	return Signal{}, false
}

// Parse parses raw VCD content. A hard syntax error is returned so the
// caller can roll back the whole import; recoverable problems are recorded
// in Waveform.Diagnostics / Change.Diagnostic.
func Parse(name string, raw []byte) (*Waveform, error) {
	w := &Waveform{SourceName: name, RawBytes: append([]byte(nil), raw...)}
	w.RawSHA256 = sha256Hex(raw)

	tokens := lex(raw)
	byID := map[string]Signal{}
	var tick int64
	inDumpvars := false

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch {
		case tok.text == "$timescale":
			body, next, err := collectDirective(tokens, i)
			if err != nil {
				return nil, err
			}
			w.Timescale = timescale.Parse(body)
			if !w.Timescale.OK {
				w.Diagnostics = append(w.Diagnostics, Diagnostic{Offset: tok.offset, Kind: "timescale", Text: w.Timescale.Diagnostic})
			}
			i = next
		case tok.text == "$date":
			body, next, err := collectDirective(tokens, i)
			if err != nil {
				return nil, err
			}
			w.Date = body
			i = next
		case tok.text == "$version":
			body, next, err := collectDirective(tokens, i)
			if err != nil {
				return nil, err
			}
			w.Version = body
			i = next
		case tok.text == "$enddefinitions":
			_, next, err := collectDirective(tokens, i)
			if err != nil {
				return nil, err
			}
			i = next
		case tok.text == "$scope" || tok.text == "$upscope":
			_, next, err := collectDirective(tokens, i)
			if err != nil {
				return nil, err
			}
			i = next
		case tok.text == "$var":
			line, next, err := collectDirective(tokens, i)
			if err != nil {
				return nil, err
			}
			s, ok := parseVar(line, "$var "+line)
			if ok {
				if _, dup := byID[s.ID]; !dup {
					byID[s.ID] = s
					w.Signals = append(w.Signals, s)
				}
			} else {
				w.Diagnostics = append(w.Diagnostics, Diagnostic{Offset: tok.offset, Kind: "var", Text: "unparseable $var retained as diagnostic: " + line})
			}
			i = next
		case tok.text == "$dumpvars":
			inDumpvars = true
		case tok.text == "$end" && inDumpvars:
			inDumpvars = false
		case strings.HasPrefix(tok.text, "#"):
			n, err := parseTick(tok.text)
			if err != nil {
				w.Diagnostics = append(w.Diagnostics, Diagnostic{Offset: tok.offset, Kind: "tick", Text: err.Error()})
				continue
			}
			tick = n
		default:
			c, ok := parseChange(tok.text, byID)
			c.Tick = tick
			c.Offset = tok.offset
			c.Initial = inDumpvars
			if !ok {
				c.Diagnostic = fmt.Sprintf("value token %q not bound to a declared scalar signal", tok.text)
				w.Diagnostics = append(w.Diagnostics, Diagnostic{Offset: tok.offset, Kind: "change", Text: c.Diagnostic})
			} else if !c.OK {
				w.Diagnostics = append(w.Diagnostics, Diagnostic{Offset: tok.offset, Kind: "change", Text: c.Diagnostic})
			}
			w.Changes = append(w.Changes, c)
		}
	}
	if w.Timescale.Raw == "" {
		return nil, fmt.Errorf("VCD missing $timescale directive")
	}
	if len(w.Signals) == 0 {
		return nil, fmt.Errorf("VCD contains no scalar $var signals")
	}
	return w, nil
}

type token struct {
	text   string
	offset int64
}

func lex(raw []byte) []token {
	var toks []token
	start := -1
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			if start >= 0 {
				toks = append(toks, token{string(raw[start:i]), int64(start)})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		toks = append(toks, token{string(raw[start:]), int64(start)})
	}
	return toks
}

// collectDirective returns the joined body tokens and the index of $end.
func collectDirective(tokens []token, start int) (string, int, error) {
	var body []string
	for j := start + 1; j < len(tokens); j++ {
		if tokens[j].text == "$end" {
			return strings.Join(body, " "), j, nil
		}
		body = append(body, tokens[j].text)
	}
	return "", 0, fmt.Errorf("directive at token %d has no $end", start)
}

// parseVar parses "wire 1 ! clk".
func parseVar(line, raw string) (Signal, bool) {
	f := strings.Fields(line)
	if len(f) < 4 || f[0] != "wire" && f[0] != "reg" {
		return Signal{}, false
	}
	return Signal{Kind: f[0], WidthText: f[1], Width: atoiOr1(f[1]), ID: f[2], Name: f[3], RawDecl: raw}, true
}

func parseChange(text string, byID map[string]Signal) (Change, bool) {
	if len(text) < 2 {
		return Change{Value: fourstate.VX, RawToken: text, OK: false}, false
	}
	vTok := text[:1]
	id := text[1:]
	v, parsed := fourstate.Parse(vTok)
	c := Change{SignalID: id, Value: v, RawToken: text, OK: parsed}
	if !parsed {
		c.Diagnostic = fmt.Sprintf("value token %q is not one of 0/1/x/z", text)
	}
	_, exists := byID[id]
	return c, exists
}

func parseTick(text string) (int64, error) {
	var n int64
	for _, r := range text[1:] {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("tick %q has a non-digit coordinate", text)
		}
		n = n*10 + int64(r-'0')
	}
	return n, nil
}

func atoiOr1(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 1
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return 1
	}
	return n
}
