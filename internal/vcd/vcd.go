// Package vcd parses a minimal subset of IEEE 1364 VCD.
//
// Import fidelity rules enforced here:
//   - every value change keeps its raw textual token and raw time number;
//   - the $timescale directive is kept verbatim and only additionally mapped
//     to an integer picosecond factor in the derived layer;
//   - real-valued or otherwise unconvertible tokens are NOT dropped: they are
//     retained as changes with Kind == ChangeReal plus a diagnostic;
//   - no dedup/cleaning happens at parse time (even consecutive identical
//     values remain separate raw change records).
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"signalcourt/internal/fourval"
)

// ChangeKind classifies a raw value-change token.
type ChangeKind string

const (
	ChangeScalar ChangeKind = "scalar"
	ChangeVector ChangeKind = "vector"
	ChangeReal   ChangeKind = "real" // retained but not normalized
)

// Signal is one declared $var (aliases of one VCD id are kept separate).
type Signal struct {
	ID    string // VCD identifier characters
	Name  string // hierarchical full name
	Width int
	Kind  string // wire/reg/...
	MSB   int    // declared range; 0 when scalar/no range
	LSB   int
}

// Change is one raw value-change record.
type Change struct {
	Index   int        // stable 0-based order index within this file
	TimeRaw int64      // raw #time number (timescale units of the source file)
	ID      string     // target VCD identifier
	Raw     string     // verbatim value token ("1", "b1x0z", "r1.5", ...)
	Kind    ChangeKind
	Value   fourval.Value // empty when Kind == ChangeReal
}

// Diagnostic explains a retained-but-unconverted import item.
type Diagnostic struct {
	Where   string // e.g. "change:12" or "timescale"
	Code    string
	Message string
}

// File is the parsed but un-normalized source material.
type File struct {
	TimescaleRaw  string // e.g. "1ns"
	TimeUnit      string // ns
	TimeNumber    int64  // 1
	Picoseconds   int64  // derived factor per raw time unit; 0 if unknown
	DateRaw       string
	VersionRaw    string
	Signals       []Signal
	byID          map[string][]int // signal indices aliasing an id
	Changes       []Change
	Diagnostics   []Diagnostic
}

// SignalsForID returns all declared signals aliasing a VCD identifier.
func (f *File) SignalsForID(id string) []Signal {
	var out []Signal
	for _, i := range f.byID[id] {
		out = append(out, f.Signals[i])
	}
	return out
}

// unitFactor maps a VCD timescale unit to picoseconds.
func unitFactor(unit string) (int64, bool) {
	switch unit {
	case "s":
		return 1_000_000_000_000, true
	case "ms":
		return 1_000_000_000, true
	case "us", "µs":
		return 1_000_000, true
	case "ns":
		return 1_000, true
	case "ps":
		return 1, true
	case "fs":
		return 0, true // sub-picosecond; retain raw, no integer factor
	}
	return 0, false
}

// Parse reads VCD content. Parse never fails on "bad" values: unconvertible
// tokens are retained with diagnostics. Only structural tokenization errors
// produce an error (and the caller must roll back the whole import).
func Parse(r io.Reader) (*File, error) {
	f := &File{byID: map[string][]int{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)

	var (
		currentTime int64
		headerDone  bool
		inDump      bool
		changeIdx   int
	)

	addDiag := func(where, code, format string, args ...any) {
		f.Diagnostics = append(f.Diagnostics, Diagnostic{
			Where: where, Code: code, Message: fmt.Sprintf(format, args...),
		})
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			t, err := strconv.ParseInt(line[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd: malformed time token %q: %w", line, err)
			}
			currentTime = t
			continue
		}
		if strings.HasPrefix(line, "$") {
			kw, rest, _ := strings.Cut(line, " ")
			rest = strings.TrimSpace(rest)
			switch kw {
			case "$timescale":
				raw := rest
				if !strings.HasSuffix(raw, "$end") {
					var parts []string
					for sc.Scan() {
						l := strings.TrimSpace(sc.Text())
						if strings.HasSuffix(l, "$end") {
							l = strings.TrimSpace(strings.TrimSuffix(l, "$end"))
							if l != "" {
								parts = append(parts, l)
							}
							break
						}
						parts = append(parts, l)
					}
					if len(parts) > 0 {
						raw = strings.TrimSpace(raw + " " + strings.Join(parts, " "))
					}
				}
				raw = strings.TrimSpace(strings.TrimSuffix(raw, "$end"))
				raw = strings.TrimSpace(raw)
				f.TimescaleRaw = raw
				parseTimescale(f, raw, addDiag)
				continue
			case "$date":
				f.DateRaw = collectDirective(sc, rest)
				continue
			case "$version":
				f.VersionRaw = collectDirective(sc, rest)
				continue
			case "$scope":
				continue
			case "$upscope":
				continue
			case "$var":
				if err := parseVar(sc, f, rest); err != nil {
					return nil, err
				}
				continue
			case "$enddefinitions":
				headerDone = true
				continue
			case "$dumpvars", "$dumpon":
				inDump = true
				continue
			case "$dumpoff", "$dumpall":
				// values following still parse as value changes
				inDump = true
				continue
			default:
				// unknown directive: consume through $end and continue
				if !strings.HasSuffix(rest, "$end") {
					for sc.Scan() {
						if strings.HasSuffix(strings.TrimSpace(sc.Text()), "$end") {
							break
						}
					}
				}
				continue
			}
		}
		if !headerDone {
			addDiag("preheader", "ignored_token", "token before $enddefinitions: %q", line)
			continue
		}
		// value change line (possibly inside a dump section)
		if line == "$end" {
			inDump = false
			continue
		}
		ch, ok := parseChange(line, currentTime)
		if !ok {
			addDiag(fmt.Sprintf("change:%d", changeIdx), "unconvertible_value",
				"raw token %q at #%d retained but not normalized", line, currentTime)
			ch = Change{Index: changeIdx, TimeRaw: currentTime, Raw: line, Kind: ChangeReal}
		}
		ch.Index = changeIdx
		changeIdx++
		f.Changes = append(f.Changes, ch)
		if inDump {
			// value lines may be followed by $end on its own line
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("vcd: scan: %w", err)
	}
	// A final $end inside dump sections may have been consumed as a change;
	// real tokens are preserved regardless.
	return f, nil
}

func parseTimescale(f *File, raw string, addDiag func(string, string, string, ...any)) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		addDiag("timescale", "unknown_timescale", "empty $timescale; raw retained: %q", raw)
		return
	}
	num, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		addDiag("timescale", "unknown_timescale", "cannot parse timescale number in %q", raw)
		return
	}
	unit := fields[len(fields)-1]
	factor, ok := unitFactor(unit)
	if !ok {
		addDiag("timescale", "unknown_unit", "timescale unit %q not convertible; raw value retained", unit)
		return
	}
	f.TimeNumber = num
	f.TimeUnit = unit
	if factor == 0 {
		addDiag("timescale", "sub_picosecond", "timescale %s finer than 1ps; raw times retained", raw)
		return
	}
	f.Picoseconds = num * factor
}

func collectDirective(sc *bufio.Scanner, first string) string {
	first = strings.TrimSpace(strings.TrimSuffix(first, "$end"))
	if strings.TrimSpace(first) != "" {
		return strings.TrimSpace(first)
	}
	var parts []string
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasSuffix(l, "$end") {
			l = strings.TrimSpace(strings.TrimSuffix(l, "$end"))
			if l != "" {
				parts = append(parts, l)
			}
			break
		}
		parts = append(parts, l)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func parseVar(sc *bufio.Scanner, f *File, first string) error {
	body := strings.TrimSuffix(first, "$end")
	if !strings.HasSuffix(first, "$end") {
		var parts []string
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if strings.HasSuffix(l, "$end") {
				l = strings.TrimSpace(strings.TrimSuffix(l, "$end"))
				if l != "" {
					parts = append(parts, l)
				}
				break
			}
			parts = append(parts, l)
		}
		body = strings.TrimSpace(body + " " + strings.Join(parts, " "))
	}
	fields := strings.Fields(body)
	// $var type size id name [range]
	if len(fields) < 4 {
		return fmt.Errorf("vcd: malformed $var: %q", body)
	}
	width, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("vcd: bad var width %q", fields[1])
	}
	sig := Signal{Kind: fields[0], Width: width, ID: fields[2], Name: fields[3], MSB: width - 1}
	if len(fields) >= 6 {
		rng := fields[4]
		if strings.HasPrefix(rng, "[") && strings.HasSuffix(rng, "]") {
			lo, hi, found := strings.Cut(rng[1:len(rng)-1], ":")
			if found {
				if msb, e1 := strconv.Atoi(hi); e1 == nil {
					if lsb, e2 := strconv.Atoi(lo); e2 == nil {
						sig.MSB, sig.LSB = msb, lsb
					}
				}
			}
		}
	}
	idx := len(f.Signals)
	f.Signals = append(f.Signals, sig)
	f.byID[sig.ID] = append(f.byID[sig.ID], idx)
	return nil
}

func parseChange(line string, t int64) (Change, bool) {
	if line[0] == 'b' || line[0] == 'B' {
		// b<bits> <id>
		token, id, found := splitTokenID(line)
		if !found {
			return Change{}, false
		}
		v, ok := fourval.ParseVector(token[1:])
		if !ok {
			return Change{TimeRaw: t, ID: id, Raw: line, Kind: ChangeReal}, false
		}
		return Change{TimeRaw: t, ID: id, Raw: line, Kind: ChangeVector, Value: v}, true
	}
	if line[0] == 'r' || line[0] == 'R' {
		_, id, found := splitTokenID(line)
		if !found {
			return Change{}, false
		}
		return Change{TimeRaw: t, ID: id, Raw: line, Kind: ChangeReal}, false
	}
	// scalar: final char before optional id; forms like "1!", "x #", "(0#"
	token := line
	id := ""
	// strip strength prefix "(0" / "D1" style: take last non-space run as id,
	// last char before it as value
	if sp := strings.IndexAny(line, " \t"); sp >= 0 {
		token = strings.TrimSpace(line[:sp])
		id = strings.TrimSpace(line[sp:])
	} else {
		// VCD ids may be multi-char; value is exactly the first rune
		rs := []rune(line)
		token = string(rs[0])
		id = string(rs[1:])
	}
	v, ok := fourval.ParseScalar(token)
	if !ok {
		return Change{TimeRaw: t, ID: id, Raw: line, Kind: ChangeReal}, false
	}
	return Change{TimeRaw: t, ID: id, Raw: line, Kind: ChangeScalar, Value: v}, true
}

func splitTokenID(line string) (string, string, bool) {
	sp := strings.IndexAny(line, " \t")
	if sp < 0 {
		return "", "", false
	}
	return line[:sp], strings.TrimSpace(line[sp:]), true
}
