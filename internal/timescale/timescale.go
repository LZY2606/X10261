// Package timescale models VCD timescale precision and time coordinates.
//
// Every domain time quantity carries both the original textual
// representation (number + unit) and an optional normalization to
// femtoseconds. Values that cannot be normalized are kept at their raw
// position with a diagnostic instead of being dropped or invented.
package timescale

import (
	"fmt"
	"strconv"
	"strings"
)

// UnitExp maps an SI time unit suffix to its power-of-ten factor toward fs.
var UnitExp = map[string]int{
	"s":  15,
	"ms": 12,
	"us": 9,
	"µs": 9,
	"ns": 6,
	"ps": 3,
	"fs": 0,
}

// Quantity is a time value with both raw representation and normalized fs.
type Quantity struct {
	Raw     string `json:"raw"`     // original textual representation
	Number  string `json:"number"`  // number token as imported
	Unit    string `json:"unit"`    // unit token as imported
	FS      int64  `json:"fs"`      // normalized value in femtoseconds
	Basis   string `json:"basis"`   // coordinate basis label
	OK      bool   `json:"ok"`      // whether normalization succeeded
	Diagnostic string `json:"diagnostic,omitempty"`
}

// Scale is a parsed $timescale directive.
type Scale struct {
	Raw        string     `json:"raw"`
	Number     string     `json:"number"`
	Unit       string     `json:"unit"`
	FSPerTick  int64      `json:"fs_per_tick"`
	Quantity   Quantity   `json:"quantity"`
	OK         bool       `json:"ok"`
	Diagnostic string     `json:"diagnostic,omitempty"`
}

// At converts a raw tick count into a Quantity on this scale.
func (s Scale) At(ticks int64) Quantity {
	q := Quantity{Raw: strconv.FormatInt(ticks, 10), Number: strconv.FormatInt(ticks, 10), Unit: s.Unit, Basis: "fs"}
	if s.OK {
		q.FS = ticks * s.FSPerTick
		q.OK = true
	} else {
		q.Diagnostic = "timescale not normalizable; raw tick retained"
	}
	return q
}

// Parse parses a $timescale body, e.g. "100 ps" or "1ns".
func Parse(body string) Scale {
	raw := strings.TrimSpace(body)
	s := Scale{Raw: raw}
	fields := strings.Fields(raw)
	if len(fields) == 2 {
		s.Number, s.Unit = fields[0], fields[1]
	} else if len(fields) == 1 {
		n, u := splitNumberUnit(fields[0])
		s.Number, s.Unit = n, u
	} else {
		s.Diagnostic = fmt.Sprintf("timescale %q must be '<number> <unit>'", raw)
		s.Quantity = Quantity{Raw: raw, Basis: "fs", Diagnostic: s.Diagnostic}
		return s
	}
	num, err := strconv.ParseInt(s.Number, 10, 64)
	if err != nil {
		s.Diagnostic = fmt.Sprintf("timescale number %q is not an integer: %v", s.Number, err)
		s.Quantity = Quantity{Raw: raw, Number: s.Number, Unit: s.Unit, Basis: "fs", Diagnostic: s.Diagnostic}
		return s
	}
	exp, known := UnitExp[strings.ToLower(s.Unit)]
	if !known {
		s.Diagnostic = fmt.Sprintf("timescale unit %q is unknown; raw value retained", s.Unit)
		s.Quantity = Quantity{Raw: raw, Number: s.Number, Unit: s.Unit, Basis: "fs", Diagnostic: s.Diagnostic}
		return s
	}
	s.FSPerTick = num * pow10(exp)
	s.OK = true
	s.Quantity = Quantity{Raw: raw, Number: s.Number, Unit: s.Unit, FS: s.FSPerTick, Basis: "fs", OK: true}
	return s
}

func splitNumberUnit(tok string) (string, string) {
	for i, r := range tok {
		if (r < '0' || r > '9') && r != '.' {
			return tok[:i], tok[i:]
		}
	}
	return tok, ""
}

func pow10(n int) int64 {
	var v int64 = 1
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}
