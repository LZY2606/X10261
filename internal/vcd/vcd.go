// Package vcd parses Value Change Dump files. Import keeps the original
// bytes and per-change tokens verbatim; normalization into a femtosecond
// coordinate base happens only on the derived layer.
package vcd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"signalcourt/internal/fourstate"
)

// Change is one original value change record.
type Change struct {
	Time      int64             // femtoseconds in the declared coordinate base
	TimeRaw   string            // original "#123" numeric text
	Code      string            // VCD identifier code
	Value     fourstate.Value   // parsed value (X when unconvertible)
	ValueRaw  string            // original value token, e.g. "b101x"
	BitValues []fourstate.Value // bit-level values for vectors (MSB first)
	Unconv    bool              // true if the token could not be converted
	Line      int               // 1-based source line of the record
}

// Signal is one declared wire/reg.
type Signal struct {
	Code  string
	Name  string
	Width int
	Line  int
}

// Timescale carries the original timescale representation plus the derived
// coordinate base (femtoseconds per VCD time unit).
type Timescale struct {
	Raw          string `json:"raw"`
	FemtosPerUnit int64 `json:"femtos_per_unit"`
	Unit         string `json:"unit"`
	Number       int    `json:"number"`
	Convertible  bool   `json:"convertible"`
}

// File is the parsed-but-not-normalized VCD material.
type File struct {
	Name       string      `json:"name"`
	Raw        []byte      `json:"-"`
	Timescale  Timescale   `json:"timescale"`
	Signals    []Signal    `json:"signals"`
	Changes    []Change    `json:"changes"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Diagnostic describes a value that cannot be converted/normalized; the
// raw value stays in place rather than being dropped for rendering.
type Diagnostic struct {
	Line    int    `json:"line"`
	Code    string `json:"code,omitempty"`
	Raw     string `json:"raw"`
	Reason  string `json:"reason"`
}

var unitFemtos = map[string]int64{
	"fs": 1,
	"ps": 1_000,
	"ns": 1_000_000,
	"us": 1_000_000_000,
	"ms": 1_000_000_000_000,
	"s":  1_000_000_000_000_000,
}

// Parse reads a VCD. All failures return an error so the importer can roll
// the whole import back; diagnostics describe recoverable oddities.
func Parse(name string, r io.Reader) (*File, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	f := &File{Name: name, Raw: raw}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	lineNo := 0
	var curTime int64
	curTimeRaw := "0"
	signalByCode := map[string]*Signal{}
	inHeader := true
	depth := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			num := line[1:]
			t, perr := strconv.ParseInt(num, 10, 64)
			if perr != nil {
				return nil, fmt.Errorf("line %d: bad time mark %q", lineNo, line)
			}
			if inHeader {
				inHeader = false
			}
			curTime = t
			curTimeRaw = num
			continue
		}
		if inHeader {
			switch {
			case strings.HasPrefix(line, "$timescale"):
				ts, rest, ok := consumeSection(line, raw, scanner, &lineNo)
				_ = rest
				if !ok {
					ts = strings.TrimSpace(strings.TrimPrefix(line, "$timescale"))
				}
				f.Timescale = parseTimescale(strings.TrimSpace(ts), lineNo, f)
			case strings.HasPrefix(line, "$scope"), strings.HasPrefix(line, "$upscope"),
				strings.HasPrefix(line, "$var"), strings.HasPrefix(line, "$enddefinitions"),
				strings.HasPrefix(line, "$date"), strings.HasPrefix(line, "$version"),
				strings.HasPrefix(line, "$comment"):
				if strings.HasPrefix(line, "$var") {
					sig, perr := parseVar(line, lineNo)
					if perr != nil {
						return nil, perr
					}
					if existing, ok := signalByCode[sig.Code]; ok {
						// same code, additional hierarchical alias
						_ = existing
						f.Signals = append(f.Signals, sig)
					} else {
						signalByCode[sig.Code] = &sig
						f.Signals = append(f.Signals, sig)
					}
				}
				if strings.Contains(line, "$end") {
					depth += strings.Count(line, "$end")
				}
				continue
			default:
				continue
			}
			if line == "$end" {
				depth--
			}
			continue
		}
		// dump section: value change
		ch, perr := parseChange(line, lineNo, curTime, curTimeRaw)
		if perr != nil {
			f.Diagnostics = append(f.Diagnostics, Diagnostic{Line: lineNo, Raw: line, Reason: perr.Error()})
			ch = Change{Time: curTime, TimeRaw: curTimeRaw, Value: fourstate.X, ValueRaw: line, Unconv: true, Line: lineNo}
		}
		f.Changes = append(f.Changes, ch)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if f.Timescale.Raw == "" {
		f.Timescale = Timescale{Raw: "(none)", Convertible: false}
		f.Diagnostics = append(f.Diagnostics, Diagnostic{Raw: "$timescale", Reason: "no timescale declared; coordinate base unknown"})
	}
	sort.SliceStable(f.Changes, func(i, j int) bool {
		if f.Changes[i].Time != f.Changes[j].Time {
			return f.Changes[i].Time < f.Changes[j].Time
		}
		return f.Changes[i].Line < f.Changes[j].Line
	})
	return f, nil
}

func consumeSection(line string, raw []byte, scanner *bufio.Scanner, lineNo *int) (body, rest string, ok bool) {
	if i := strings.Index(line, "$end"); i >= 0 {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "$timescale"), "$end")), "", true
	}
	var sb strings.Builder
	sb.WriteString(strings.TrimPrefix(line, "$timescale"))
	for scanner.Scan() {
		*lineNo++
		l := strings.TrimSpace(scanner.Text())
		if i := strings.Index(l, "$end"); i >= 0 {
			sb.WriteString(" ")
			sb.WriteString(l[:i])
			return strings.TrimSpace(sb.String()), strings.TrimSpace(l[i+4:]), true
		}
		sb.WriteString(" ")
		sb.WriteString(l)
	}
	return strings.TrimSpace(sb.String()), "", false
}

func parseTimescale(body string, line int, f *File) Timescale {
	ts := Timescale{Raw: body, Convertible: false}
	fields := strings.Fields(body)
	if len(fields) == 0 {
		f.Diagnostics = append(f.Diagnostics, Diagnostic{Line: line, Raw: body, Reason: "empty timescale"})
		return ts
	}
	num := fields[0]
	unit := ""
	if len(fields) >= 2 {
		unit = strings.ToLower(fields[1])
	} else {
		// "1ns" style
		for i, c := range num {
			if c < '0' || c > '9' {
				unit = strings.ToLower(num[i:])
				num = num[:i]
				break
			}
		}
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		f.Diagnostics = append(f.Diagnostics, Diagnostic{Line: line, Raw: body, Reason: "unconvertible timescale number: kept at origin"})
		return ts
	}
	mult, ok := unitFemtos[unit]
	if !ok {
		f.Diagnostics = append(f.Diagnostics, Diagnostic{Line: line, Raw: body, Reason: "unconvertible timescale unit " + unit + ": kept at origin"})
		ts.Number = n
		ts.Unit = unit
		return ts
	}
	ts.Number = n
	ts.Unit = unit
	ts.FemtosPerUnit = int64(n) * mult
	ts.Convertible = true
	return ts
}

func parseVar(line, lineNo int) (Signal, error) {
	fields := strings.Fields(line)
	// $var wire width code name [$end]
	if len(fields) < 5 {
		return Signal{}, fmt.Errorf("line %d: malformed $var record: %q", lineNo, line)
	}
	w, err := strconv.Atoi(fields[2])
	if err != nil {
		return Signal{}, fmt.Errorf("line %d: bad width in $var: %q", lineNo, line)
	}
	return Signal{Code: fields[3], Name: fields[4], Width: w, Line: lineNo}, nil
}

func parseChange(line string, lineNo int, t int64, traw string) (Change, error) {
	ch := Change{Time: t, TimeRaw: traw, Line: lineNo, ValueRaw: line}
	switch line[0] {
	case '0', '1', 'x', 'X', 'z', 'Z':
		// scalar: value immediately followed by code, e.g. "1! clk"
		fields := strings.Fields(line)
		v, err := fourstate.Parse(fields[0][:1])
		if err != nil {
			ch.Unconv = true
			return ch, err
		}
		ch.Value = v
		ch.BitValues = []fourstate.Value{v}
		if len(fields) >= 2 {
			ch.Code = fields[1]
		} else {
			ch.Code = fields[0][1:]
		}
	case 'b', 'B':
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return ch, fmt.Errorf("malformed vector change %q", line)
		}
		bits := fields[0][1:]
		vals := make([]fourstate.Value, 0, len(bits))
		unconv := false
		var reason error
		for i := 0; i < len(bits); i++ {
			v, err := fourstate.Parse(string(bits[i]))
			if err != nil {
				unconv = true
				reason = err
			}
			vals = append(vals, v)
		}
		ch.BitValues = vals
		ch.Code = fields[1]
		// aggregate value: any X -> X, any Z (and no X) -> Z, else all 0/1
		ch.Value = aggregate(vals)
		if unconv {
			ch.Unconv = true
			return ch, reason
		}
	case 'r', 'R':
		return ch, fmt.Errorf("real-valued change not supported: kept raw at origin")
	default:
		return ch, fmt.Errorf("unrecognized change token %q", line)
	}
	return ch, nil
}

func aggregate(vals []fourstate.Value) fourstate.Value {
	hasZ, hasX := false, false
	allOne, allZero := true, true
	for _, v := range vals {
		switch v {
		case fourstate.X:
			hasX = true
		case fourstate.Z:
			hasZ = true
		case fourstate.One:
			allZero = false
		case fourstate.Zero:
			allOne = false
		}
	}
	if hasX {
		return fourstate.X
	}
	if hasZ && !allOne && !allZero {
		return fourstate.X
	}
	if hasZ {
		return fourstate.Z
	}
	if allOne {
		return fourstate.One
	}
	if allZero {
		return fourstate.Zero
	}
	// mixed 0/1 vector: keep LSB semantics flag as unknown aggregate,
	// but bits remain individually readable; caller may use BitValues.
	return fourstate.X
}

// SignalValueAt reports the value of a signal code at femtosecond time t
// (last change at or before t), plus whether the signal was ever driven.
func (f *File) SignalValueAt(code string, t int64) (fourstate.Value, []fourstate.Value, bool) {
	var best *Change
	for i := range f.Changes {
		c := &f.Changes[i]
		if c.Code != code {
			continue
		}
		if c.Time <= t {
			best = c
		} else {
			break
		}
	}
	if best == nil {
		return fourstate.X, nil, false
	}
	return best.Value, best.BitValues, true
}
