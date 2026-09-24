// Package units keeps every domain quantity together with its unit or
// coordinate basis. Raw representations are preserved exactly as
// imported; normalization happens only in the derived layer. Values
// that cannot be converted stay in place and carry a diagnostic.
package units

import (
	"fmt"
	"strconv"
	"strings"
)

// Time is a time quantity with an explicit unit basis.
type Time struct {
	Raw  string `json:"raw"`  // original textual representation, never rewritten
	Tick int64  `json:"tick"` // integer ticks in Unit
	Unit string `json:"unit"` // "fs","ps","ns","us","ms","s" or "" if unknown
}

// Diagnostic explains why a value could not be normalized.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Raw     string `json:"raw"`
}

var unitToFs = map[string]int64{
	"fs": 1,
	"ps": 1000,
	"ns": 1000 * 1000,
	"us": 1000 * 1000 * 1000,
	"ms": 1000 * 1000 * 1000 * 1000,
	"s":  1000 * 1000 * 1000 * 1000 * 1000,
}

// ParseTimescale parses a VCD $timescale body like "1ns" or "10 ps".
func ParseTimescale(s string) (Time, *Diagnostic) {
	raw := strings.TrimSpace(s)
	fields := strings.Fields(raw)
	var numStr, unit string
	if len(fields) == 2 {
		numStr, unit = fields[0], fields[1]
	} else if len(fields) == 1 {
		// split digits from unit suffix
		i := 0
		for i < len(fields[0]) && (fields[0][i] >= '0' && fields[0][i] <= '9' || fields[0][i] == '.') {
			i++
		}
		numStr, unit = fields[0][:i], fields[0][i:]
	} else {
		return Time{Raw: raw}, &Diagnostic{Code: "bad_timescale", Message: "unparseable $timescale", Raw: raw}
	}
	n, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil || n <= 0 {
		return Time{Raw: raw}, &Diagnostic{Code: "bad_timescale_num", Message: "non-positive or non-integer timescale magnitude", Raw: raw}
	}
	if _, ok := unitToFs[unit]; !ok {
		return Time{Raw: raw, Tick: n, Unit: unit}, &Diagnostic{Code: "unknown_unit", Message: "unknown time unit; value left unnormalized", Raw: raw}
	}
	return Time{Raw: raw, Tick: n, Unit: unit}, nil
}

// ToFs normalizes to femtoseconds. If the unit is unknown the value is
// returned unconverted with ok=false so callers keep it in place.
func (t Time) ToFs() (fs int64, ok bool) {
	m, known := unitToFs[t.Unit]
	if !known {
		return 0, false
	}
	return t.Tick * m, true
}

// Scale converts a tick count expressed in this time basis to fs.
func (t Time) Scale(ticks int64) (int64, bool) {
	m, known := unitToFs[t.Unit]
	if !known {
		return 0, false
	}
	return ticks * m, true
}

func (t Time) String() string {
	if t.Raw != "" {
		return fmt.Sprintf("%s(%d%s)", t.Raw, t.Tick, t.Unit)
	}
	return fmt.Sprintf("%d%s", t.Tick, t.Unit)
}
