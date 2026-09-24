// Package vcd parses the Value Change Dump subset accepted on import.
//
// The parser is deliberately non-destructive:
//
//   - every physical input line keeps its 1-based number and raw text;
//   - every change keeps its timestamp in the file's own unit (origin at
//     tick 0) AND a femtosecond coordinate derived from the timescale;
//   - scalar signals decode to the four-state alphabet 0/1/x/z;
//   - vector signals keep the raw token (e.g. "b10xz") plus its decoded
//     four-state bits — no value is dropped to make a picture;
//   - anything that cannot be converted stays in place with a Diagnostic
//     attached to the change/waveform instead of aborting or coercing.
package vcd

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"signalverdict/internal/fourstate"
)

// Diagnostic is an in-place conversion problem. It never removes the value.
type Diagnostic struct {
	Line    int    `json:"line"`
	Code    string `json:"code"` // stable machine-readable code
	Message string `json:"message"`
	Raw     string `json:"raw"`
}

// Change is one value-change record.
type Change struct {
	Line       int    `json:"line"`
	ID         string `json:"id"`
	Tick       int64  `json:"tick"`        // file-native time, origin #0
	Femtosec   int64  `json:"femtosec"`    // coordinate-basis time
	Scalar     fourstate.Value `json:"scalar"`
	IsScalar   bool   `json:"is_scalar"`
	RawValue   string `json:"raw_value"`   // exact token from the file
	Unresolved bool   `json:"unresolved"`  // vector/bus contains X or Z
	HasZ       bool   `json:"has_z"`
}

// Signal describes one declared identifier.
type Signal struct {
	ID      string   `json:"id"`
	Names   []string `json:"names"`
	Width   int      `json:"width"`
	Vector  bool     `json:"vector"`
	Scalars []fourstate.Value `json:"scalars,omitempty"` // current bus bits MSB..LSB
}

// RawLine preserves an input line verbatim.
type RawLine struct {
	No   int    `json:"no"`
	Text string `json:"text"`
}

// Timescale carries the original representation and the conversion factor to
// the common femtosecond coordinate basis (1 unit = FactorFS femtoseconds).
type Timescale struct {
	Raw      string `json:"raw"`
	Number   int64  `json:"number"`
	Unit     string `json:"unit"`
	FactorFS int64  `json:"factor_fs"`
}

// Waveform is the parsed, still-faithful view of one VCD file.
type Waveform struct {
	Timescale   Timescale          `json:"timescale"`
	EndTimeTick int64              `json:"end_time_tick"`
	Signals     map[string]*Signal `json:"signals"`
	Order       []string           `json:"order"`
	Changes     []Change           `json:"changes"`
	RawLines    []RawLine          `json:"raw_lines"`
	Diagnostics []Diagnostic       `json:"diagnostics"`
}

var timescaleRe = regexp.MustCompile(`^\s*(\d+)\s*(fs|ps|ns|us|µs|ms|s)\s*$`)

func unitFactorFS(unit string) (int64, bool) {
	switch unit {
	case "fs":
		return 1, true
	case "ps":
		return 1_000, true
	case "ns":
		return 1_000_000, true
	case "us", "µs":
		return 1_000_000_000, true
	case "ms":
		return 1_000_000_000_000, true
	case "s":
		return 1_000_000_000_000_000, true
	}
	return 0, false
}

// Parse converts raw VCD bytes. A parse error that makes the file unusable
// is returned so the caller can roll the whole import back; conversion
// problems that preserve the raw bytes become Diagnostics instead.
func Parse(data []byte) (*Waveform, error) {
	w := &Waveform{Signals: map[string]*Signal{}}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	inDump := false
	var currentTick int64 = -1
	lineNo := 0
	seenTimescale := false
	for sc.Scan() {
		lineNo++
		raw := strings.TrimRight(sc.Text(), "\r")
		w.RawLines = append(w.RawLines, RawLine{No: lineNo, Text: raw})
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case line == "$end":
			continue
		case strings.HasPrefix(line, "$timescale"):
			body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "$timescale"), "$end"))
			if !seenTimescale {
				if err := w.parseTimescale(body, lineNo); err != nil {
					return nil, err
				}
				seenTimescale = true
			}
		case strings.HasPrefix(line, "$var"):
			if err := w.parseVar(line, lineNo); err != nil {
				w.addDiag(lineNo, "vcd-var-malformed", err.Error(), line)
			}
		case line == "$dumpvars", line == "$dumpon", line == "$dumpoff", line == "$dumpall":
			inDump = true
		case strings.HasPrefix(line, "#"):
			t, err := strconv.ParseInt(line[1:], 10, 64)
			if err != nil {
				w.addDiag(lineNo, "vcd-bad-time", "timestamp not an integer", line)
				continue
			}
			currentTick = t
			if t > w.EndTimeTick {
				w.EndTimeTick = t
			}
		case line[0] == '$' && strings.HasSuffix(line, "$end"):
			// single-line other directive; ignore but keep raw line
		case strings.HasPrefix(line, "$"):
			// unrecognized directive; ignored, line retained
		default:
			tick := currentTick
			if tick < 0 {
				tick = 0
				if inDump {
					w.addDiag(lineNo, "vcd-change-before-time", "change before first #time, anchored to 0", line)
				}
			}
			w.parseChange(line, lineNo, tick)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !seenTimescale {
		w.addDiag(1, "vcd-no-timescale", "no $timescale; assuming 1ns coordinate basis", "")
		w.Timescale = Timescale{Raw: "1ns (assumed)", Number: 1, Unit: "ns", FactorFS: 1_000_000}
	}
	return w, nil
}

func (w *Waveform) parseTimescale(body string, lineNo int) error {
	m := timescaleRe.FindStringSubmatch(body)
	if m == nil {
		return fmt.Errorf("line %d: unparseable timescale %q", lineNo, body)
	}
	num, _ := strconv.ParseInt(m[1], 10, 64)
	factor, _ := unitFactorFS(m[2])
	w.Timescale = Timescale{Raw: body, Number: num, Unit: m[2], FactorFS: num * factor}
	return nil
}

var varRe = regexp.MustCompile(`^\$var\s+(wire|reg|logic|tri)\s+(\d+)\s+(\S+)\s+(\S+?)(?:\s+\[\d+:\d+\])?\s+\$end$`)

func (w *Waveform) parseVar(line string, lineNo int) error {
	m := varRe.FindStringSubmatch(line)
	if m == nil {
		// tolerant fallback for missing $end
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return fmt.Errorf("malformed $var")
		}
		width, _ := strconv.Atoi(fields[2])
		id := fields[3]
		name := fields[4]
		w.addSignal(id, name, width)
		return nil
	}
	width, _ := strconv.Atoi(m[2])
	w.addSignal(m[3], m[4], width)
	return nil
}

func (w *Waveform) addSignal(id, name string, width int) {
	s, ok := w.Signals[id]
	if !ok {
		vec := width > 1
		s = &Signal{ID: id, Width: width, Vector: vec}
		if vec {
			s.Scalars = make([]fourstate.Value, width)
			for i := range s.Scalars {
				s.Scalars[i] = fourstate.X
			}
		}
		w.Signals[id] = s
		w.Order = append(w.Order, id)
	}
	for _, n := range s.Names {
		if n == name {
			return
		}
	}
	s.Names = append(s.Names, name)
}

func (w *Waveform) addDiag(line int, code, msg, raw string) {
	w.Diagnostics = append(w.Diagnostics, Diagnostic{Line: line, Code: code, Message: msg, Raw: raw})
}

func (w *Waveform) parseChange(line string, lineNo int, tick int64) {
	// vector:  b<value> <id>   or   r<real> <id>
	if line[0] == 'b' || line[0] == 'B' {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			w.addDiag(lineNo, "vcd-change-malformed", "vector change needs value + id", line)
			return
		}
		w.recordVector(fields[0][1:], fields[1], lineNo, tick, fields[0])
		return
	}
	if line[0] == 'r' || line[0] == 'R' {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			w.addDiag(lineNo, "vcd-change-malformed", "real change needs value + id", line)
			return
		}
		// Real values cannot be converted into the four-state alphabet.
		// They stay at their origin with a diagnostic instead of being
		// coerced to 0 just for rendering.
		if _, err := strconv.ParseFloat(fields[0][1:], 64); err != nil {
			w.addDiag(lineNo, "vcd-real-unparseable", "real value not a number, kept verbatim", line)
		} else {
			w.addDiag(lineNo, "vcd-real-unconvertible",
				"real value has no four-state representation; retained at origin for traceability", line)
		}
		w.Changes = append(w.Changes, Change{Line: lineNo, ID: fields[1], Tick: tick,
			Femtosec: tick * w.Timescale.FactorFS, RawValue: fields[0], IsScalar: false})
		return
	}
	// scalar:  x0 / 0! / 1clk  (value char + id)
	fields := strings.Fields(line)
	token := line
	id := ""
	if len(fields) == 2 {
		token = fields[0]
		id = fields[1]
	} else {
		id = line[1:]
	}
	v, ok := fourstate.Parse(token[0])
	if !ok {
		w.addDiag(lineNo, "vcd-bad-scalar", "scalar token not in 0/1/x/z", line)
		return
	}
	if s := w.Signals[id]; s != nil && s.Vector {
		// scalar token applied to a vector: broadcast to the bus.
		w.recordVector(strings.Repeat(token[:1], s.Width), id, lineNo, tick, token)
		return
	}
	w.Changes = append(w.Changes, Change{Line: lineNo, ID: id, Tick: tick,
		Femtosec: tick * w.Timescale.FactorFS, Scalar: v, IsScalar: true, RawValue: token[:1],
		HasZ: v == fourstate.Z})
}

func (w *Waveform) recordVector(bits, id string, lineNo, tick int64, raw string) {
	s, ok := w.Signals[id]
	ch := Change{Line: lineNo, ID: id, Tick: tick, Femtosec: tick * w.Timescale.FactorFS,
		IsScalar: false, RawValue: raw}
	if !ok {
		w.addDiag(lineNo, "vcd-unknown-id", "change for undeclared identifier; kept verbatim", raw+" "+id)
		w.Changes = append(w.Changes, ch)
		return
	}
	if len(bits) != s.Width {
		w.addDiag(lineNo, "vcd-width-mismatch",
			fmt.Sprintf("vector width %d does not match declared %d; raw value retained", len(bits), s.Width), raw)
	}
	decoded := make([]fourstate.Value, 0, len(bits))
	for i := 0; i < len(bits); i++ {
		v, ok := fourstate.Parse(bits[i])
		if !ok {
			v = fourstate.X
			w.addDiag(lineNo, "vcd-bad-vector-digit", "vector digit not in 0/1/x/z", string(bits[i]))
		}
		if v == fourstate.X {
			ch.Unresolved = true
		}
		if v == fourstate.Z {
			ch.Unresolved = true
			ch.HasZ = true
		}
		decoded = append(decoded, v)
	}
	// Update the bus state, left-padding with X if the token was short.
	if s.Vector {
		s.Scalars = make([]fourstate.Value, s.Width)
		for i := range s.Scalars {
			s.Scalars[i] = fourstate.X
		}
		off := s.Width - len(decoded)
		for i, v := range decoded {
			if off >= 0 && off+i < s.Width {
				s.Scalars[off+i] = v
			}
		}
	}
	w.Changes = append(w.Changes, ch)
}

// ResolveAt returns the four-state value of a scalar signal at file-native
// tick t (last change <= t). Signals start as X.
func (w *Waveform) ResolveAt(id string, t int64) fourstate.Value {
	cur := fourstate.X
	for _, c := range w.Changes {
		if c.ID != id || !c.IsScalar || c.Tick > t {
			continue
		}
		cur = c.Scalar
	}
	return cur
}
