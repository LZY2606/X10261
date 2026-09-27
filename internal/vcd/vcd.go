// Package vcd parses VCD waveform files. It preserves the raw
// representation of every value change and the raw timescale; the
// derived layer normalizes times to femtoseconds. Values that cannot
// be converted stay in place and produce diagnostics.
package vcd

import (
	"fmt"
	"sort"
	"strings"

	"signalcourt/internal/logic4"
)

// unitToFS maps VCD time units to femtoseconds.
var unitToFS = map[string]int64{
	"s": 1_000_000_000_000_000,
	"ms": 1_000_000_000_000,
	"us": 1_000_000_000,
	"ns": 1_000_000,
	"ps": 1_000,
	"fs": 1,
}

// Change is a single raw value change, normalized to fs when possible.
type Change struct {
	RawTime  string    // raw integer text as it appeared after '#'
	TimeFS   int64     // normalized time in femtoseconds (0 if unconvertible)
	RawToken string    // raw value token, e.g. "1", "x", "z"
	Value    logic4.V  // parsed four-state value
	ConvOK   bool      // false when time or value could not be converted
}

// Waveform is one signal's change list plus coordinate basis.
type Waveform struct {
	Name         string
	RawTimescale string // e.g. "1ns" exactly as declared
	UnitFS       int64  // femtoseconds per timescale unit (0 = unknown unit)
	Changes      []Change
}

// ValueAt returns the value held strictly before time tFS (preponed
// sampling region semantics): the last change with TimeFS < tFS, or the
// change at tFS-0 when none is strictly earlier. Changes exactly at tFS
// belong to the next region.
func (w *Waveform) ValueAt(tFS int64) (logic4.V, bool) {
	var cur *Change
	for i := range w.Changes {
		c := &w.Changes[i]
		if !c.ConvOK {
			continue
		}
		if c.TimeFS < tFS || (c.TimeFS == tFS && i == 0) {
			cur = c
		}
		if c.TimeFS >= tFS {
			break
		}
	}
	if cur == nil {
		return 0, false
	}
	return cur.Value, true
}

// Diagnostic records a value that stayed raw because it could not be
// converted into the normalized derived layer.
type Diagnostic struct {
	Where   string // file/section context
	Raw     string // raw representation kept in place
	Message string
}

// File is a parsed VCD document.
type File struct {
	RawTimescale string
	UnitFS       int64
	Signals      map[string]*Waveform // keyed by signal name
	Order        []string             // declaration order (stable identity of order)
	Diagnostics  []Diagnostic
}

// Parse reads a VCD text. Scalar signals only; vector value changes are
// kept raw and reported through diagnostics rather than dropped.
func Parse(text string) *File {
	f := &File{Signals: map[string]*Waveform{}}
	ids := map[string]string{} // vcd id code -> signal name
	var curTime string
	var curFS int64
	timeOK := true

	lines := strings.Fields(text)
	i := 0
	for i < len(lines) {
		tok := lines[i]
		switch {
		case tok == "$timescale":
			// collect until $end
			j := i + 1
			var parts []string
			for j < len(lines) && lines[j] != "$end" {
				parts = append(parts, lines[j])
				j++
			}
			raw := strings.Join(parts, "")
			raw = strings.ReplaceAll(raw, " ", "")
			f.RawTimescale = raw
			f.UnitFS = parseTimescale(raw)
			if f.UnitFS == 0 {
				f.Diagnostics = append(f.Diagnostics, Diagnostic{
					Where:   "$timescale",
					Raw:     raw,
					Message: "unknown time unit; times kept raw, not normalized",
				})
			}
			i = j + 1
		case tok == "$var":
			// $var wire 1 ! name $end
			if i+4 < len(lines) {
				id := lines[i+3]
				name := lines[i+4]
				ids[id] = name
				if _, ok := f.Signals[name]; !ok {
					f.Signals[name] = &Waveform{
						Name:         name,
						RawTimescale: f.RawTimescale,
						UnitFS:       f.UnitFS,
					}
					f.Order = append(f.Order, name)
				}
			}
			j := i
			for j < len(lines) && lines[j] != "$end" {
				j++
			}
			i = j + 1
		case strings.HasPrefix(tok, "$"):
			// skip section to $end if it is a section keyword
			switch tok {
			case "$scope", "$upscope", "$enddefinitions", "$comment", "$date", "$version":
				if tok == "$enddefinitions" || tok == "$upscope" {
					for i < len(lines) && lines[i] != "$end" {
						i++
					}
					i++
				} else {
					for i < len(lines) && lines[i] != "$end" {
						i++
					}
					i++
				}
			default:
				i++
			}
		case strings.HasPrefix(tok, "#"):
			curTime = tok[1:]
			var n int64
			if _, err := fmt.Sscanf(curTime, "%d", &n); err != nil || f.UnitFS == 0 {
				timeOK = false
			} else {
				curFS = n * f.UnitFS
				timeOK = true
			}
			i++
		default:
			// value change: scalar like "0!" or vector like "b101 %"
			if len(tok) >= 1 && (tok[0] == '0' || tok[0] == '1' || tok[0] == 'x' || tok[0] == 'X' || tok[0] == 'z' || tok[0] == 'Z') && len(tok) > 1 {
				id := tok[1:]
				name, ok := ids[id]
				if !ok {
					i++
					continue
				}
				v, vok := logic4.Parse(tok[0])
				w := f.Signals[name]
				ch := Change{RawTime: curTime, RawToken: tok[:1], Value: v, ConvOK: timeOK && vok}
				if timeOK {
					ch.TimeFS = curFS
				}
				if !vok {
					f.Diagnostics = append(f.Diagnostics, Diagnostic{
						Where:   name,
						Raw:     tok[:1],
						Message: "unrecognized value token kept raw",
					})
				}
				w.Changes = append(w.Changes, ch)
				i++
			} else if tok[0] == 'b' || tok[0] == 'B' {
				// vector change: keep raw, emit diagnostic, do not drop
				id := ""
				if i+1 < len(lines) {
					id = lines[i+1]
				}
				f.Diagnostics = append(f.Diagnostics, Diagnostic{
					Where:   ids[id],
					Raw:     tok,
					Message: "vector value change kept raw; scalar layer does not normalize it",
				})
				i += 2
			} else {
				i++
			}
		}
	}
	// propagate timescale to waveforms declared before $timescale was seen
	for _, name := range f.Order {
		w := f.Signals[name]
		w.RawTimescale = f.RawTimescale
		w.UnitFS = f.UnitFS
	}
	for _, w := range f.Signals {
		sort.SliceStable(w.Changes, func(a, b int) bool { return w.Changes[a].TimeFS < w.Changes[b].TimeFS })
	}
	return f
}

func parseTimescale(raw string) int64 {
	// forms like "1ns", "10ps"
	num := int64(0)
	i := 0
	for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
		num = num*10 + int64(raw[i]-'0')
		i++
	}
	unit := raw[i:]
	mult, ok := unitToFS[unit]
	if !ok || num == 0 {
		return 0
	}
	return num * mult
}
