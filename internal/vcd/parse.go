// Package vcd parses a deliberately small subset of IEEE 1364 VCD while
// preserving everything the archive needs:
//   - raw value changes (including x/z and multi-bit words, exactly spelled);
//   - time markers as imported (number + timescale unit), with a derived
//     nanosecond coordinate produced only in a separate field;
//   - clock/edge information is recoverable from changes, edges are never
//     synthesized silently;
//   - values that cannot be normalized stay at their original position with
//     a diagnostic instead of being dropped or guessed.
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Signal is a wire/reg declaration. RawBusWidth is kept verbatim.
type Signal struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Width   int    `json:"width"`
	RawKind string `json:"raw_kind"` // wire|reg as declared
	RawMSB  string `json:"raw_msb,omitempty"`
	RawLSB  string `json:"raw_lsb,omitempty"`
}

// Change is one value-change entry. TimeRaw is the declared timescale-number
// of the current #time marker; ValueRaw is the exact VCD payload.
type Change struct {
	AtTick  int    `json:"at_tick"`  // ordinal of the #time marker (0-based)
	TimeRaw int64  `json:"time_raw"` // declared time number (timescale units)
	SigID   string `json:"sig_id"`
	Value   string `json:"value_raw"` // "1","x","zz", "b10x ",... normalized glyphs
	RawText string `json:"raw_text"`  // exact source token
}

// Timescale preserves the declared scale. When the unit is unconvertible,
// Unit stays raw, NanosPerUnit is -1 and Diagnostics explains why.
type Timescale struct {
	Raw          string  `json:"raw"`
	Number       int64   `json:"number"`
	Unit         string  `json:"unit"`           // fs,ps,ns,us,ms,s normalized lowercase
	NanosPerUnit float64 `json:"nanos_per_unit"` // derived only; -1 if unknown
}

// Diagnostic is attached to the exact import position when a value cannot be
// normalized; nothing is discarded to make a picture draw.
type Diagnostic struct {
	Line    int    `json:"line"`
	AtTick  int    `json:"at_tick,omitempty"`
	TimeRaw int64  `json:"time_raw,omitempty"`
	Token   string `json:"token"`
	Kind    string `json:"kind"` // bad-value, bad-time, duplicate-id, ...
	Detail  string `json:"detail"`
}

// Waveform is the full parse result. Order is the VCD source order.
type Waveform struct {
	Timescale   Timescale    `json:"timescale"`
	Signals     []Signal     `json:"signals"`
	Changes     []Change     `json:"changes"`
	TimeMarkers []int64      `json:"time_markers"` // distinct # numbers in order
	Diagnostics []Diagnostic `json:"diagnostics"`
	// ClockIDs, when declared in an accompanying clock-domain file, is
	// populated by callers; the parser never guesses a clock.
}

var (
	declVar = regexp.MustCompile(`^\s*\$var\s+(\w+)\s+(\d+)\s+(\S+)\s+(\S+?)(?:\s+\[[^\]]*\])?\s*(?:\$end)?\s*$`)
	declTS  = regexp.MustCompile(`^\s*\$timescale\s+(\d+)\s*([munpf]?s)\s*\$end\s*$`)
	tokenRe = regexp.MustCompile(`^(?:([01xXzZ])(\S+)|[bB]([01xXzZ]+)\s*(\S+)|[rR]\S*?\s*(\S+))$`)
)

// Parse reads VCD text. It never returns a partial result on hard structural
// failure: callers wrap this in a transaction for all-or-nothing import.
func Parse(r io.Reader) (*Waveform, error) {
	w := &Waveform{Timescale: Timescale{NanosPerUnit: -1}}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	lineNo := 0
	tick := -1
	var currentTime int64
	inTimescaleBlock := false
	var tsLines []string
	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "$comment") ||
			strings.HasPrefix(trimmed, "$enddefinitions") || trimmed == "$dumpvars" ||
			trimmed == "$end" || strings.HasPrefix(trimmed, "$scope") ||
			strings.HasPrefix(trimmed, "$upscope") || strings.HasPrefix(trimmed, "$dumpall") ||
			strings.HasPrefix(trimmed, "$dumpoff") || strings.HasPrefix(trimmed, "$dumpon") ||
			strings.HasPrefix(trimmed, "$version") || strings.HasPrefix(trimmed, "$date") {
			// Multi-line header blocks we support: collect their raw lines.
			if strings.HasPrefix(trimmed, "$timescale") {
				inTimescaleBlock = true
				tsLines = []string{trimmed}
				if strings.Contains(trimmed, "$end") {
					if err := applyTimescale(w, strings.Join(tsLines, " ")); err != nil {
						return nil, fmt.Errorf("line %d: %w", lineNo, err)
					}
					inTimescaleBlock = false
				}
			}
			continue
		}
		if inTimescaleBlock {
			tsLines = append(tsLines, trimmed)
			if strings.HasPrefix(trimmed, "$end") {
				if err := applyTimescale(w, strings.Join(tsLines, " ")); err != nil {
					return nil, fmt.Errorf("line %d: %w", lineNo, err)
				}
				inTimescaleBlock = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			n, err := strconv.ParseInt(strings.TrimPrefix(trimmed, "#"), 10, 64)
			if err != nil {
				w.Diagnostics = append(w.Diagnostics, Diagnostic{
					Line: lineNo, Token: trimmed, Kind: "bad-time", Detail: err.Error(),
				})
				continue
			}
			currentTime = n
			tick++
			w.TimeMarkers = append(w.TimeMarkers, n)
			continue
		}
		if m := declVar.FindStringSubmatch(line); m != nil {
			width, _ := strconv.Atoi(m[2])
			if seen[m[3]] {
				w.Diagnostics = append(w.Diagnostics, Diagnostic{
					Line: lineNo, Token: trimmed, Kind: "duplicate-id",
					Detail: "identifier already declared: " + m[3],
				})
			}
			seen[m[3]] = true
			w.Signals = append(w.Signals, Signal{
				RawKind: m[1], Width: width, ID: m[3], Name: unescapeName(m[4]),
			})
			continue
		}
		// Value change.
		val, id, ok := splitChange(trimmed)
		if !ok {
			w.Diagnostics = append(w.Diagnostics, Diagnostic{
				Line: lineNo, AtTick: tick, TimeRaw: currentTime,
				Token: trimmed, Kind: "bad-value",
				Detail: "not a recognized scalar/binary value change",
			})
			continue
		}
		if !seen[id] {
			// Structural integrity: a change to an undeclared id is a hard
			// error so imports cannot silently reference phantom signals.
			return nil, fmt.Errorf("line %d: value change for undeclared identifier %q", lineNo, id)
		}
		norm, clean := normalizeValue(val)
		c := Change{
			AtTick: tick, TimeRaw: currentTime, SigID: id,
			Value: norm, RawText: trimmed,
		}
		w.Changes = append(w.Changes, c)
		if !clean {
			w.Diagnostics = append(w.Diagnostics, Diagnostic{
				Line: lineNo, AtTick: tick, TimeRaw: currentTime, Token: trimmed,
				Kind:   "bad-value",
				Detail: "value kept verbatim; glyphs outside 0/1/x/z: " + val,
			})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if tick < 0 {
		w.Diagnostics = append(w.Diagnostics, Diagnostic{
			Line: lineNo, Token: "", Kind: "no-time",
			Detail: "VCD contains no #time markers",
		})
	}
	return w, nil
}

func splitChange(tok string) (value, id string, ok bool) {
	m := tokenRe.FindStringSubmatch(tok)
	if m == nil {
		return "", "", false
	}
	switch {
	case m[1] != "":
		return strings.ToLower(m[1]), m[2], true
	case m[3] != "":
		return strings.ToLower(m[3]), m[4], true
	default:
		// real-value changes (r...) are retained raw but flagged by caller.
		parts := strings.Fields(tok)
		if len(parts) == 2 {
			return parts[0], parts[1], true
		}
		return "", "", false
	}
}

func normalizeValue(v string) (string, bool) {
	clean := true
	var b strings.Builder
	for _, r := range v {
		if _, ok := map[rune]bool{'0': true, '1': true, 'x': true, 'X': true, 'z': true, 'Z': true}[r]; ok {
			b.WriteRune(rune(strings.ToLower(string(r))[0]))
		} else {
			b.WriteRune(r)
			clean = false
		}
	}
	return b.String(), clean
}

func unescapeName(n string) string { return strings.TrimSpace(n) }

func applyTimescale(w *Waveform, joined string) error {
	m := declTS.FindStringSubmatch(strings.Join(strings.Fields(joined), " "))
	if m == nil {
		w.Timescale = Timescale{Raw: joined, NanosPerUnit: -1}
		w.Diagnostics = append(w.Diagnostics, Diagnostic{
			Token: joined, Kind: "bad-timescale",
			Detail: "timescale not convertible; raw representation retained",
		})
		return nil
	}
	num, _ := strconv.ParseInt(m[1], 10, 64)
	unit := strings.ToLower(m[2])
	mult := map[string]float64{
		"fs": 1e-6, "ps": 1e-3, "ns": 1, "us": 1e3, "ms": 1e6, "s": 1e9,
	}[unit]
	w.Timescale = Timescale{
		Raw:    strings.Join(strings.Fields(joined), " "),
		Number: num, Unit: unit, NanosPerUnit: float64(num) * mult,
	}
	return nil
}

// SignalByName is a convenience lookup.
func (w *Waveform) SignalByName(name string) (Signal, bool) {
	for _, s := range w.Signals {
		if s.Name == name {
			return s, true
		}
	}
	return Signal{}, false
}
