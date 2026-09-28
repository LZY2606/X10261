// Package vcd parses Value Change Dump files without destructive
// normalization. Original tokens, raw values (including X and Z), native
// timescale and declaration order are all retained on the parsed records.
// Unconvertible values stay at their original position with a diagnostic.
package vcd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/timescale"
)

// Diagnostic is attached to a record that could not be fully converted.
type Diagnostic struct {
	Pos     string `json:"pos"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Value preserves the raw token and the parsed four-state bits.
type Value struct {
	Raw   string        `json:"raw"`
	Bits  []fourstate.T `json:"bits,omitempty"`
	Width int           `json:"width"`
	Diag  string        `json:"diag,omitempty"`
}

// Var is a signal declaration in file order.
type Var struct {
	Order int    `json:"order"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Width int    `json:"width"`
}

// Change is one value-change record in its exact file position.
type Change struct {
	Seq    int             `json:"seq"`
	Stamp  timescale.Stamp `json:"stamp"`
	Signal string          `json:"signal"`
	Value  Value           `json:"value"`
	Seed   bool            `json:"seed"` // originated in a $dumpvars block
}

// File is a parsed VCD source.
type File struct {
	Path        string           `json:"path"`
	SHA256      string           `json:"sha256"`
	Scale       timescale.Scale  `json:"scale"`
	ScaleSeen   bool             `json:"scale_seen"`
	Vars        []Var            `json:"vars"`
	Changes     []Change         `json:"changes"`
	Initial     map[string]Value `json:"initial"`
	Diagnostics []Diagnostic     `json:"diagnostics"`
	VarsByID    map[string]*Var  `json:"-"`
}

type token struct {
	text string
	line int
}

// Parse reads a VCD stream. Structural problems (missing/malformed
// timescale, unknown identifiers, malformed $var) return an error so the
// importer can roll the whole batch back; per-value conversion problems are
// retained in place with diagnostics.
func Parse(r io.Reader, path string) (*File, error) {
	f := &File{Path: path, VarsByID: map[string]*Var{}, Initial: map[string]Value{}}
	h := sha256.New()
	br := bufio.NewReader(io.TeeReader(r, h))

	var toks []token
	lineNo := 0
	for {
		line, err := br.ReadString('\n')
		lineNo++
		for _, t := range strings.Fields(strings.TrimRight(line, "\r\n")) {
			toks = append(toks, token{t, lineNo})
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}

	var tick int64
	var scope []string
	seq := 0
	inDump := false
	ignoreDumps := map[string]bool{"$dumpall": true, "$dumpoff": true, "$dumpon": true}

	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case strings.HasPrefix(t.text, "#"):
			n, err := parseInt(t.text[1:])
			if err != nil {
				return nil, fmt.Errorf("vcd: malformed timestamp %q (line %d)", t.text, t.line)
			}
			tick = n
		case t.text == "$end":
			inDump = false
		case strings.HasPrefix(t.text, "$"):
			kw := t.text
			var body []token
			i++
			for i < len(toks) && toks[i].text != "$end" {
				body = append(body, toks[i])
				i++
			}
			if i >= len(toks) {
				return nil, fmt.Errorf("vcd: unterminated %s (line %d)", kw, t.line)
			}
			switch kw {
			case "$timescale":
				sc, perr := timescale.ParseScale(joinTokens(body))
				if perr != nil {
					return nil, perr
				}
				f.Scale, f.ScaleSeen = sc, true
			case "$scope":
				if len(body) < 2 {
					return nil, fmt.Errorf("vcd: malformed $scope (line %d)", t.line)
				}
				scope = append(scope, body[1].text)
			case "$upscope":
				if len(scope) > 0 {
					scope = scope[:len(scope)-1]
				}
			case "$var":
				if err := f.addVar(body, scope, t.line); err != nil {
					return nil, err
				}
			case "$dumpvars":
				inDump = true
				for j := 0; j < len(body); j++ {
					j2, err := f.addChange(body, j, tick, &seq, true, t.line)
					if err != nil {
						return nil, err
					}
					j = j2
				}
			case "$enddefinitions", "$comment", "$date", "$version",
				"$dumpall", "$dumpoff", "$dumpon":
				if ignoreDumps[kw] {
					inDump = false
				}
			default:
				f.Diagnostics = append(f.Diagnostics, Diagnostic{
					Pos: fmt.Sprintf("line %d", t.line), Code: "unknown_directive",
					Message: kw,
				})
			}
		default:
			j, err := f.addChange(toks, i, tick, &seq, inDump, t.line)
			if err != nil {
				return nil, err
			}
			i = j
		}
	}
	f.SHA256 = hex.EncodeToString(h.Sum(nil))
	if !f.ScaleSeen {
		return nil, fmt.Errorf("vcd: missing $timescale in %s", path)
	}
	return f, nil
}

func joinTokens(ts []token) string {
	var b strings.Builder
	for i, t := range ts {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t.text)
	}
	return b.String()
}

func (f *File) addVar(body []token, scope []string, line int) error {
	// $var wire size id name [$end]
	if len(body) < 4 {
		return fmt.Errorf("vcd: malformed $var (line %d)", line)
	}
	w, err := parseInt(body[2].text)
	if err != nil {
		return fmt.Errorf("vcd: bad var width %q (line %d)", body[2].text, line)
	}
	id := body[3].text
	name := body[4].text
	if len(scope) > 0 {
		name = strings.Join(scope, ".") + "." + name
	}
	if _, dup := f.VarsByID[id]; dup {
		return fmt.Errorf("vcd: duplicate var id %q (line %d)", id, line)
	}
	v := Var{Order: len(f.Vars), ID: id, Name: name, Type: body[1].text, Width: int(w)}
	f.Vars = append(f.Vars, v)
	f.VarsByID[id] = &f.Vars[len(f.Vars)-1]
	return nil
}

// addChange consumes one value change starting at body[idx]; vectors occupy
// two tokens ("b101", "!"). It returns the index of the last consumed token.
func (f *File) addChange(body []token, idx int, tick int64, seq *int, dump bool, line int) (int, error) {
	tok := body[idx].text
	var raw, id string
	switch tok[0] {
	case '0', '1', 'x', 'X', 'z', 'Z':
		if len(tok) < 2 {
			return idx, fmt.Errorf("vcd: scalar value missing identifier %q (line %d)", tok, line)
		}
		raw, id = tok[:1], tok[1:]
	case 'b', 'B':
		raw = tok[1:]
		if idx+1 >= len(body) {
			return idx, fmt.Errorf("vcd: vector value missing identifier (line %d)", line)
		}
		id = body[idx+1].text
		idx++
	case 'r', 'R':
		// real values are preserved as raw text and reported unconvertible;
		// they stay at their position instead of being dropped for plotting.
		if idx+1 >= len(body) {
			return idx, fmt.Errorf("vcd: real value missing identifier (line %d)", line)
		}
		id = body[idx+1].text
		v, ok := f.VarsByID[id]
		if !ok {
			return idx, fmt.Errorf("vcd: change for undeclared id %q (line %d)", id, line)
		}
		ch := f.mkChange(tok, tok, v, tick, *seq, dump)
		ch.Value.Diag = "real value unconvertible to four-state logic; raw token kept"
		ch.Value.Bits = nil
		f.Changes = append(f.Changes, ch)
		*seq++
		return idx + 1, nil
	default:
		return idx, fmt.Errorf("vcd: unexpected token %q (line %d)", tok, line)
	}
	v, ok := f.VarsByID[id]
	if !ok {
		return idx, fmt.Errorf("vcd: change for undeclared id %q (line %d)", id, line)
	}
	f.Changes = append(f.Changes, f.mkChange(raw, tok, v, tick, *seq, dump))
	*seq++
	return idx, nil
}

func (f *File) mkChange(raw, fullTok string, v *Var, tick int64, seqNo int, dump bool) Change {
	val := decodeValue(raw, v.Width)
	st := timescale.Resolve(tick, fmt.Sprintf("#%d", tick), f.Scale)
	ch := Change{Seq: seqNo, Stamp: st, Signal: v.Name, Value: val, Seed: dump}
	if dump {
		f.Initial[v.Name] = val
	}
	return ch
}

func decodeValue(raw string, declaredWidth int) Value {
	val := Value{Raw: raw, Width: declaredWidth}
	bits := make([]fourstate.T, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		b, err := fourstate.Parse(raw[i])
		if err != nil {
			if val.Diag != "" {
				val.Diag += "; "
			}
			val.Diag = fmt.Sprintf("unconvertible scalar %q kept at position %d", raw[i], i)
			bits = append(bits, fourstate.X)
			continue
		}
		bits = append(bits, b)
	}
	val.Bits = bits
	if declaredWidth > 0 && len(bits) > 0 && len(bits) != declaredWidth {
		d := fmt.Sprintf("width mismatch: raw %d bits vs declared %d; raw representation kept",
			len(bits), declaredWidth)
		if val.Diag == "" {
			val.Diag = d
		} else {
			val.Diag += "; " + d
		}
	}
	return val
}

func parseInt(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad int %q", s)
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
