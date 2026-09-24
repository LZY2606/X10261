// Package vcd parses VCD waveforms while preserving the raw representation:
// original value-change tokens, four-state values and the timescale are kept
// verbatim. Values that cannot be converted stay in place and produce
// diagnostics instead of being dropped.
package vcd

import (
	"fmt"
	"strconv"
	"strings"

	"sigtribunal/internal/fourval"
)

// Change is a single raw value change. Raw always holds the original token
// text; OK is false when the token could not be converted to a four-state
// scalar (the value is kept in place with a diagnostic, never dropped).
type Change struct {
	Time int64     `json:"time"`
	Val  fourval.V `json:"val"`
	OK   bool      `json:"ok"`
	Raw  string    `json:"raw"`
}

// Signal is one scalar wire with its ordered raw value changes.
type Signal struct {
	Code    string   `json:"code"`
	Name    string   `json:"name"`
	Changes []Change `json:"changes"`
}

// At returns the last change at or before t (inclusive). Used by disable iff,
// which observes current rather than sampled values.
func (s *Signal) At(t int64) (Change, bool) {
	var out Change
	found := false
	for _, c := range s.Changes {
		if c.Time > t {
			break
		}
		out, found = c, true
	}
	return out, found
}

// SampledAt returns the last change strictly before t: the preponed-region
// sampled value observed at a clock edge.
func (s *Signal) SampledAt(t int64) (Change, bool) {
	var out Change
	found := false
	for _, c := range s.Changes {
		if c.Time >= t {
			break
		}
		out, found = c, true
	}
	return out, found
}

// File is a parsed VCD document. Timescale is kept as raw text; Scale* is the
// derived normalization to picoseconds (OK=false when unparseable).
type File struct {
	Timescale   string             `json:"timescale"`
	ScaleFactor int64              `json:"scale_factor"` // derived: picoseconds per time unit
	ScaleOK     bool               `json:"scale_ok"`
	Signals     []*Signal          `json:"signals"`
	ByName      map[string]*Signal `json:"-"`
	byCode      map[string]*Signal
	Diagnostics []string `json:"diagnostics"`
	Raw         []byte   `json:"-"`
}

// Parse parses VCD data, preserving raw bytes and raw value-change tokens.
func Parse(data []byte) (*File, error) {
	f := &File{ByName: map[string]*Signal{}, byCode: map[string]*Signal{}}
	f.Raw = append([]byte(nil), data...)
	toks := strings.Fields(string(data))
	var cur int64
	header := true
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case strings.HasPrefix(t, "$"):
			switch t {
			case "$timescale":
				var parts []string
				for i+1 < len(toks) && toks[i+1] != "$end" {
					i++
					parts = append(parts, toks[i])
				}
				i++ // consume $end
				f.Timescale = strings.Join(parts, " ")
				f.ScaleFactor, f.ScaleOK = parseScale(f.Timescale)
				if !f.ScaleOK {
					f.Diagnostics = append(f.Diagnostics,
						fmt.Sprintf("unconvertible timescale %q kept as raw text", f.Timescale))
				}
			case "$var":
				// $var <type> <size> <code> <name> $end
				if i+4 < len(toks) {
					size, code, name := toks[i+2], toks[i+3], toks[i+4]
					if size != "1" {
						f.Diagnostics = append(f.Diagnostics,
							fmt.Sprintf("non-scalar var %s (size %s) kept, vector changes stay raw", name, size))
					}
					sig := &Signal{Code: code, Name: name}
					f.Signals = append(f.Signals, sig)
					f.ByName[name] = sig
					f.byCode[code] = sig
					i += 5 // through $end
				}
			case "$end":
				// block terminator, nothing to do
			default:
				// skip other keyword blocks up to their $end
				for i+1 < len(toks) && toks[i+1] != "$end" {
					i++
				}
			}
		case strings.HasPrefix(t, "#"):
			header = false
			v, err := strconv.ParseInt(t[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("bad timestamp %q: %w", t, err)
			}
			cur = v
		default:
			if header {
				continue
			}
			c := t[0]
			if v, ok := fourval.Parse(c); ok && len(t) > 1 {
				f.set(cur, t[1:], Change{Time: cur, Val: v, OK: true, Raw: t})
			} else if c == 'b' || c == 'B' || c == 'r' || c == 'R' {
				raw := t
				code := ""
				if i+1 < len(toks) {
					code = toks[i+1]
					raw = t + " " + code
					i++
				}
				f.set(cur, code, Change{Time: cur, OK: false, Raw: raw})
				f.Diagnostics = append(f.Diagnostics, fmt.Sprintf(
					"unconvertible value %q for code %q at t=%d kept in place", raw, code, cur))
			} else {
				f.Diagnostics = append(f.Diagnostics,
					fmt.Sprintf("unrecognized token %q at t=%d kept as diagnostic", t, cur))
			}
		}
	}
	return f, nil
}

func (f *File) set(t int64, code string, ch Change) {
	sig, ok := f.byCode[code]
	if !ok {
		f.Diagnostics = append(f.Diagnostics,
			fmt.Sprintf("change for unknown code %q at t=%d kept as diagnostic", code, t))
		return
	}
	sig.Changes = append(sig.Changes, ch)
}

// parseScale derives picoseconds per time unit from raw timescale text.
func parseScale(ts string) (int64, bool) {
	ts = strings.ReplaceAll(ts, " ", "")
	units := []struct {
		suffix string
		mult   int64
	}{{"ms", 1_000_000_000}, {"us", 1_000_000}, {"ns", 1_000}, {"ps", 1}, {"fs", 0}, {"s", 1_000_000_000_000}}
	for _, u := range units {
		suffix, mult := u.suffix, u.mult
		if strings.HasSuffix(ts, suffix) {
			n, err := strconv.ParseInt(strings.TrimSuffix(ts, suffix), 10, 64)
			if err != nil || n <= 0 || mult == 0 {
				return 0, false
			}
			return n * mult, true
		}
	}
	return 0, false
}
