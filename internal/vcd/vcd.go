// Package vcd parses VCD waveforms while preserving the raw source:
// every value change keeps its original text, the declared timescale
// precision is retained, and four-state values are kept as-is.
package vcd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"signaltribunal/internal/logic"
	"signaltribunal/internal/units"
)

// Change is one raw value change, exactly as imported.
type Change struct {
	Time  int64  `json:"time"` // ticks in the file's timescale unit
	Raw   string `json:"raw"`  // original line text, preserved
	Value string `json:"value"`
}

// Signal is one VCD variable with its change list.
type Signal struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Width   int      `json:"width"`
	Changes []Change `json:"changes"`
}

// File is a parsed VCD document. Raw is the untouched source bytes.
type File struct {
	Raw         string             `json:"-"`
	Timescale   units.Time         `json:"timescale"`
	Diagnostics []units.Diagnostic `json:"diagnostics,omitempty"`
	Signals     []*Signal          `json:"signals"`
	byName      map[string]*Signal
	byID        map[string]*Signal
}

// Parse parses VCD text. It never discards input: unknown or malformed
// constructs are recorded as diagnostics and parsing continues.
func Parse(raw string) (*File, error) {
	f := &File{Raw: raw, byName: map[string]*Signal{}, byID: map[string]*Signal{}}
	lines := strings.Split(raw, "\n")
	var curTime int64
	inTimescale := false
	var tsBuf []string
	for _, ln := range lines {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		if inTimescale {
			if strings.HasPrefix(line, "$end") {
				inTimescale = false
				ts, diag := units.ParseTimescale(strings.Join(tsBuf, " "))
				f.Timescale = ts
				if diag != nil {
					f.Diagnostics = append(f.Diagnostics, *diag)
				}
			} else {
				tsBuf = append(tsBuf, line)
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "$timescale"):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "$timescale"))
			if strings.HasSuffix(rest, "$end") {
				ts, diag := units.ParseTimescale(strings.TrimSuffix(rest, "$end"))
				f.Timescale = ts
				if diag != nil {
					f.Diagnostics = append(f.Diagnostics, *diag)
				}
			} else if rest != "" {
				tsBuf = append(tsBuf, rest)
				inTimescale = true
			} else {
				inTimescale = true
			}
		case strings.HasPrefix(line, "$var"):
			// $var wire 1 ! sig_name $end
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				width, _ := strconv.Atoi(fields[2])
				if width <= 0 {
					width = 1
				}
				sig := &Signal{ID: fields[3], Name: fields[4], Width: width}
				f.Signals = append(f.Signals, sig)
				f.byName[sig.Name] = sig
				f.byID[sig.ID] = sig
			}
		case strings.HasPrefix(line, "$"):
			// other declaration keywords: ignored but source kept in Raw
		case line[0] == '#':
			t, err := strconv.ParseInt(line[1:], 10, 64)
			if err != nil {
				f.Diagnostics = append(f.Diagnostics, units.Diagnostic{Code: "bad_time", Message: "unparseable timestamp", Raw: line})
				continue
			}
			curTime = t
		default:
			// value change: scalar "0!" / "x!" or vector "b101 %" / "Bxz %"
			c := line[0]
			if c == 'b' || c == 'B' {
				parts := strings.Fields(line)
				if len(parts) != 2 {
					f.Diagnostics = append(f.Diagnostics, units.Diagnostic{Code: "bad_vector_change", Message: "malformed vector value change", Raw: line})
					continue
				}
				sig := f.byID[parts[1]]
				if sig == nil {
					f.Diagnostics = append(f.Diagnostics, units.Diagnostic{Code: "unknown_id", Message: "value change for unknown id", Raw: line})
					continue
				}
				sig.Changes = append(sig.Changes, Change{Time: curTime, Raw: line, Value: strings.ToLower(parts[0][1:])})
			} else if _, ok := logic.ParseV4(c); ok && len(line) >= 2 {
				sig := f.byID[line[1:]]
				if sig == nil {
					f.Diagnostics = append(f.Diagnostics, units.Diagnostic{Code: "unknown_id", Message: "value change for unknown id", Raw: line})
					continue
				}
				sig.Changes = append(sig.Changes, Change{Time: curTime, Raw: line, Value: strings.ToLower(string(c))})
			} else {
				f.Diagnostics = append(f.Diagnostics, units.Diagnostic{Code: "unparsed_line", Message: "line not understood; preserved in raw source", Raw: line})
			}
		}
	}
	if f.Timescale.Unit == "" && len(f.Diagnostics) == 0 {
		f.Diagnostics = append(f.Diagnostics, units.Diagnostic{Code: "no_timescale", Message: "no $timescale declared; ticks have no unit basis", Raw: ""})
	}
	return f, nil
}

// Signal returns the named signal or nil.
func (f *File) Signal(name string) *Signal { return f.byName[name] }

// ValueAt returns the four-state value of a signal at the given tick,
// using the last change at or before t (sampled semantics).
func (s *Signal) ValueAt(t int64) (logic.V4, bool) {
	idx := sort.Search(len(s.Changes), func(i int) bool { return s.Changes[i].Time > t }) - 1
	if idx < 0 {
		return 0, false
	}
	v, ok := logic.ParseV4(s.Changes[idx].Value[0])
	return v, ok
}

// Edges returns the tick of every change where the signal transitions
// from a non-1 value to 1 (posedge) or 1 to non-1 (negedge).
func (s *Signal) Edges(pos bool) []int64 {
	var out []int64
	var prev logic.V4
	havePrev := false
	for _, ch := range s.Changes {
		v, ok := logic.ParseV4(ch.Value[0])
		if !ok {
			continue
		}
		if havePrev {
			if pos && prev != logic.One && v == logic.One {
				out = append(out, ch.Time)
			}
			if !pos && prev == logic.One && v != logic.One {
				out = append(out, ch.Time)
			}
		}
		prev, havePrev = v, true
	}
	return out
}

// Summary is a stable digest of what the archive promises to preserve.
func (f *File) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "timescale=%s signals=%d changes=", f.Timescale.String(), len(f.Signals))
	n := 0
	for _, s := range f.Signals {
		n += len(s.Changes)
	}
	fmt.Fprintf(&b, "%d", n)
	return b.String()
}
