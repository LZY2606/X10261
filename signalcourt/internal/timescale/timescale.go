// Package timescale preserves VCD time precision: every timestamp keeps both
// its raw textual representation (with the file's native unit) and a derived
// femtosecond coordinate used by the replay engine. Values that cannot be
// converted stay at their original position with a diagnostic instead of
// being dropped to make plotting easier.
package timescale

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Unit is a declared VCD time unit (IEEE 1364 timescale).
type Unit string

const (
	Fs Unit = "fs"
	Ps Unit = "ps"
	Ns Unit = "ns"
	Us Unit = "us"
	Ms Unit = "ms"
	S  Unit = "s"
)

var unitFactor = map[Unit]int64{
	Fs: 1,
	Ps: 1_000,
	Ns: 1_000_000,
	Us: 1_000_000_000,
	Ms: 1_000_000_000_000,
	S:  1_000_000_000_000_000,
}

// Scale is a `$timescale 1ps $end` declaration: number, unit and raw text.
type Scale struct {
	Number int64  `json:"number"`
	Unit   Unit   `json:"unit"`
	Raw    string `json:"raw"` // exact source token, e.g. "10ps"
}

var scaleRE = regexp.MustCompile(`^\s*([0-9]+)\s*(fs|ps|ns|us|ms|s)\s*$`)

// ParseScale parses the body of a $timescale token. A malformed timescale is
// fatal to the import so the caller can roll back; it is never silently
// normalized to a default.
func ParseScale(body string) (Scale, error) {
	raw := strings.TrimSpace(body)
	m := scaleRE.FindStringSubmatch(raw)
	if m == nil {
		return Scale{}, fmt.Errorf("timescale: cannot parse %q", raw)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return Scale{}, fmt.Errorf("timescale: bad number in %q: %w", raw, err)
	}
	return Scale{Number: n, Unit: Unit(m[2]), Raw: raw}, nil
}

// FsPerTick is one raw time tick expressed in femtoseconds.
func (s Scale) FsPerTick() (int64, error) {
	f, ok := unitFactor[s.Unit]
	if !ok {
		return 0, fmt.Errorf("timescale: unknown unit %q", s.Unit)
	}
	return s.Number * f, nil
}

// Resolution returns the human label "number unit".
func (s Scale) Resolution() string { return s.Raw }

// Stamp is a raw timestamp plus its derived femtosecond coordinate.
type Stamp struct {
	Raw           string `json:"raw"`                     // original token from the file
	Tick          int64  `json:"tick"`                    // numeric raw #time value
	Scale         Scale  `json:"scale"`                   // native scale of the source
	Fs            int64  `json:"fs"`                      // derived coordinate, 0 when Unconvertible
	Unconvertible string `json:"unconvertible,omitempty"` // diagnostic
}

// Resolve derives the fs coordinate. A stamp whose scale cannot convert is
// retained in place with the diagnostic populated; it is never discarded.
func Resolve(tick int64, raw string, sc Scale) Stamp {
	st := Stamp{Raw: raw, Tick: tick, Scale: sc}
	f, err := sc.FsPerTick()
	if err != nil {
		st.Unconvertible = err.Error()
		return st
	}
	st.Fs = tick * f
	return st
}

// Coordinate is the derived normalized layer (femtoseconds from the time
// origin). Origin is always the zero tick of the first imported source.
type Coordinate struct {
	Fs     int64
	Origin int64 // femtosecond coordinate declared as time zero (never moves)
}

// Offset is elapsed femtoseconds from the origin. Reduction must never
// change the origin; it may only remove intervals unrelated to a failure.
func (c Coordinate) Offset(st Stamp) (int64, bool) {
	if st.Unconvertible != "" {
		return 0, false
	}
	return st.Fs - c.Origin, true
}

// PrecisionCompare reports the ordering of two scales' resolutions.
// <0 means a is coarser than b, 0 equal, >0 finer.
func PrecisionCompare(a, b Scale) int {
	af, _ := a.FsPerTick()
	bf, _ := b.FsPerTick()
	switch {
	case af > bf:
		return -1
	case af < bf:
		return 1
	default:
		return 0
	}
}
