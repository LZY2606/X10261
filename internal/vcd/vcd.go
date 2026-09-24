// Package vcd parses the subset of IEEE 1364 VCD needed by the review flow.
// Parsing is non-destructive: the original bytes, every value-change token,
// timescale spelling and declaration are retained by the caller. An unparseable
// file is returned as an error so the importer can roll the whole batch back;
// parseable-but-unconvertible values instead become diagnostics and stay put.
package vcd

import (
	"fmt"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/model"
	"signalcourt/internal/qty"
)

// Parsed is the structured interpretation of one VCD blob.
type Parsed struct {
	Source      model.Source
	Timescale   qty.Timescale
	TimescaleOK bool
	Signals     []model.Signal
	Changes     []model.Change
	Diagnostics []model.Diagnostic
	EndMark     int64 // largest raw mark
}

type scanner struct {
	buf   []byte
	pos   int
	diag  []model.Diagnostic
	src   model.Source
}

func (s *scanner) errorf(off int, code, format string, a ...any) error {
	return fmt.Errorf("vcd %s at byte %d: %s", code, off, fmt.Sprintf(format, a...))
}

func (s *scanner) diagf(off int, code, format string, a ...any) {
	s.diag = append(s.diag, model.Diagnostic{
		ID:       model.HashID("diag", s.src.ID, fmt.Sprint(off), code),
		SourceID: s.src.ID,
		ByteOff:  off,
		Raw:      string(s.buf[off:s.pos]),
		Code:     code,
		Message:  fmt.Sprintf(format, a...),
	})
}

// Parse interprets raw VCD bytes. src must already carry its content hash.
func Parse(src model.Source) (*Parsed, error) {
	s := &scanner{buf: src.Bytes, src: src}
	byIDCode := map[string]*model.Signal{}
	var signals []model.Signal
	var changes []model.Change

	var ts qty.Timescale
	tsOK := false

	currentMark := int64(0)
	markOrder := 0
	var endMark int64

	for s.pos < len(s.buf) {
		s.skipSpace()
		if s.pos >= len(s.buf) {
			break
		}
		start := s.pos
		if s.buf[s.pos] == '#' {
			s.pos++
			n, err := s.readInt()
			if err != nil {
				return nil, s.errorf(start, "BAD_TIME", "expected integer time mark after '#'")
			}
			currentMark = n
			markOrder = 0
			if n > endMark {
				endMark = n
			}
			continue
		}
		if s.buf[s.pos] == '$' {
			kwStart := s.pos
			kw, err := s.readWord()
			if err != nil {
				return nil, err
			}
			if kw == "$end" {
				return nil, s.errorf(kwStart, "STRAY_END", "stray $end outside a directive")
			}
			switch kw {
			case "$timescale":
				inner, to, err := s.readDirective()
				if err != nil {
					return nil, err
				}
				t, perr := qty.ParseTimescale(inner)
				if perr != nil {
					s.diagf(start, "BAD_TIMESCALE", "%v", perr)
				} else {
					ts = t
					tsOK = true
				}
				_ = to
			case "$scope", "$upscope", "$enddefinitions", "$version", "$comment", "$date":
				if err := s.skipDirectiveBody(kw); err != nil {
					return nil, err
				}
			case "$var":
				sig, err := s.readVar()
				if err != nil {
					return nil, err
				}
				if _, dup := byIDCode[sig.IDCode]; dup {
					s.diagf(start, "DUP_IDCODE", "identifier code %q already declared; later alias retained for raw fidelity but ignored in mapping", sig.IDCode)
					continue
				}
				sig.SourceID = src.ID
				sig.ID = model.HashID("sig", src.ID, sig.IDCode, sig.Name)
				byIDCode[sig.IDCode] = &sig
				signals = append(signals, sig)
			default:
				if err := s.skipDirectiveBody(kw); err != nil {
					return nil, err
				}
			}
			continue
		}
		// value change: scalar or vector
		tok, idcode, err := s.readChange()
		if err != nil {
			return nil, s.errorf(start, "BAD_CHANGE", "%v", err)
		}
		sig, ok := byIDCode[idcode]
		if !ok {
			s.diagf(start, "UNKNOWN_IDCODE", "value change for undeclared identifier %q retained as diagnostic", idcode)
			markOrder++
			continue
		}
		if err := validateValue(tok, sig.Width); err != nil {
			s.diagf(start, "BAD_VALUE", "%v", err)
			markOrder++
			continue
		}
		changes = append(changes, model.Change{
			ID:       model.HashID("chg", src.ID, idcode, fmt.Sprint(currentMark), fmt.Sprint(markOrder)),
			SourceID: src.ID,
			SignalID: sig.ID,
			IDCode:   idcode,
			RawMark:  currentMark,
			ValueTok: tok,
			Order:    markOrder,
			ByteOff:  start,
		})
		markOrder++
	}

	return &Parsed{
		Source:      src,
		Timescale:   ts,
		TimescaleOK: tsOK,
		Signals:     signals,
		Changes:     changes,
		Diagnostics: s.diag,
		EndMark:     endMark,
	}, nil
}

func validateValue(tok string, width int) error {
	bits := tok
	if tok[0] == 'b' || tok[0] == 'B' || tok[0] == 'r' || tok[0] == 'R' {
		bits = tok[1:]
		if len(bits) == 0 {
			return fmt.Errorf("empty vector value %q", tok)
		}
	}
	for i := range bits {
		if _, err := fourstate.Parse(bits[i]); err != nil {
			return fmt.Errorf("value token %q contains non four-state digit", tok)
		}
	}
	if width > 1 && len(bits) > width {
		return fmt.Errorf("value %q wider than declared width %d", tok, width)
	}
	return nil
}

func (s *scanner) skipSpace() {
	for s.pos < len(s.buf) {
		c := s.buf[s.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			s.pos++
			continue
		}
		return
	}
}

func (s *scanner) readWord() (string, error) {
	start := s.pos
	for s.pos < len(s.buf) && !isSpace(s.buf[s.pos]) {
		s.pos++
	}
	if s.pos == start {
		return "", s.errorf(start, "LEX", "expected word")
	}
	return string(s.buf[start:s.pos]), nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (s *scanner) readInt() (int64, error) {
	start := s.pos
	for s.pos < len(s.buf) && s.buf[s.pos] >= '0' && s.buf[s.pos] <= '9' {
		s.pos++
	}
	if s.pos == start {
		return 0, fmt.Errorf("no digits")
	}
	var n int64
	for i := start; i < s.pos; i++ {
		n = n*10 + int64(s.buf[i]-'0')
	}
	return n, nil
}

// readDirective reads from the keyword (already consumed) up to $end and
// returns the trimmed interior and the offset of $end.
func (s *scanner) readDirective() (string, int, error) {
	start := s.pos
	for s.pos < len(s.buf) {
		if s.buf[s.pos] == '$' {
			wStart := s.pos
			w, _ := s.readWord()
			if w == "$end" {
				inner := trimSpace(string(s.buf[start:wStart]))
				return inner, wStart + len("$end"), nil
			}
			continue
		}
		s.pos++
	}
	return "", 0, s.errorf(start, "UNEXPECTED_EOF", "directive missing $end")
}

func (s *scanner) skipDirectiveBody(kw string) error {
	_, end, err := s.readDirective()
	if err != nil {
		return err
	}
	s.pos = end
	return nil
}

// readVar parses: $var wire 8 ! data [7:0] $end
func (s *scanner) readVar() (model.Signal, error) {
	s.skipSpace()
	start := s.pos
	tok, err := s.readWord()
	if err != nil {
		return model.Signal{}, err
	}
	if tok != "wire" && tok != "reg" && tok != "logic" {
		return model.Signal{}, s.errorf(start, "BAD_VAR", "unsupported var type %q", tok)
	}
	vtype := tok
	s.skipSpace()
	width := 1
	if s.pos < len(s.buf) && s.buf[s.pos] >= '0' && s.buf[s.pos] <= '9' {
		n, err := s.readInt()
		if err != nil {
			return model.Signal{}, err
		}
		width = int(n)
	} else {
		s.diagf(s.pos, "BAD_VAR_WIDTH", "missing width; keeping raw position, assuming 1")
	}
	s.skipSpace()
	idStart := s.pos
	idcode, err := s.readWord()
	if err != nil || idcode == "" {
		return model.Signal{}, s.errorf(idStart, "BAD_VAR", "missing identifier code")
	}
	s.skipSpace()
	nameStart := s.pos
	name, err := s.readWord()
	if err != nil {
		return model.Signal{}, err
	}
	// optional bus reference like [7:0] before $end
	for {
		s.skipSpace()
		if s.pos < len(s.buf) && s.buf[s.pos] == '[' {
			for s.pos < len(s.buf) && s.buf[s.pos] != '$' && !isSpace(s.buf[s.pos]) {
				s.pos++
			}
			continue
		}
		break
	}
	if s.pos > len(s.buf)-4 || string(s.buf[s.pos:s.pos+4]) != "$end" {
		return model.Signal{}, s.errorf(nameStart, "BAD_VAR", "expected $end after %s", name)
	}
	s.pos += 4
	return model.Signal{IDCode: idcode, Name: name, Width: width, VType: vtype}, nil
}

// readChange parses either a scalar token immediately followed by an id code
// (e.g. "1!") or a vector token separated by whitespace (e.g. "b10x !").
func (s *scanner) readChange() (string, string, error) {
	start := s.pos
	first := s.buf[s.pos]
	vec := first == 'b' || first == 'B' || first == 'r' || first == 'R'
	if vec {
		tokStart := s.pos
		s.pos++
		for s.pos < len(s.buf) && !isSpace(s.buf[s.pos]) {
			s.pos++
		}
		tok := string(s.buf[tokStart:s.pos])
		s.skipSpace()
		idStart := s.pos
		for s.pos < len(s.buf) && !isSpace(s.buf[s.pos]) {
			s.pos++
		}
		idcode := string(s.buf[idStart:s.pos])
		if idcode == "" {
			return "", "", s.errorf(start, "LEX", "missing id after vector value")
		}
		return tok, idcode, nil
	}
	// scalar: exactly one four-state digit then id code
	if _, err := fourstate.Parse(first); err != nil {
		return "", "", fmt.Errorf("unexpected character %q at change start", string(first))
	}
	tok := string(first)
	s.pos++
	idStart := s.pos
	for s.pos < len(s.buf) && !isSpace(s.buf[s.pos]) {
		s.pos++
	}
	idcode := string(s.buf[idStart:s.pos])
	if idcode == "" {
		return "", "", fmt.Errorf("missing id after scalar value")
	}
	return tok, idcode, nil
}

func trimSpace(v string) string {
	for len(v) > 0 && isSpace(v[0]) {
		v = v[1:]
	}
	for len(v) > 0 && isSpace(v[len(v)-1]) {
		v = v[:len(v)-1]
	}
	return v
}
