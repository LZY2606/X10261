// Package timescale parses VCD timescale directives and keeps both the time
// unit of the simulation ticks and the time precision. Raw representations
// are retained; only the derived layer normalizes to picoseconds.
package timescale

import (
	"fmt"
	"strconv"
	"strings"
)

// Unit is a VCD time unit. Picoseconds is the canonical coordinate base.
type Unit string

const (
	Second      Unit = "s"
	Millisecond Unit = "ms"
	Microsecond Unit = "us"
	Nanosecond  Unit = "ns"
	Picosecond  Unit = "ps"
	Femtosecond Unit = "fs"
)

// Picoseconds converts one unit to picoseconds. The second return value is
// false for an unrecognized unit so the caller can retain the raw value with
// a diagnostic instead of guessing.
func Picoseconds(u Unit) (int64, bool) {
	switch u {
	case Second:
		return 1_000_000_000_000, true
	case Millisecond:
		return 1_000_000_000, true
	case Microsecond:
		return 1_000_000, true
	case Nanosecond:
		return 1_000, true
	case Picosecond:
		return 1, true
	case Femtosecond:
		return 0, true // sub-picosecond; represented as 0 ps with raw retained
	}
	return 0, false
}

// Scale is the parsed timescale. Number/Unit describe the delay numbers used
// in the VCD (#<n>); PrecisionNumber/PrecisionUnit describe sampling
// precision when present. Raw is the verbatim directive body.
type Scale struct {
	Number          int64
	Unit            Unit
	PrecisionNumber int64
	PrecisionUnit   Unit
	HasPrecision    bool
	Raw             string
}

// TickPs is the canonical coordinate (ps) of one #1 delay tick. Femtosecond
// files round to 0 ps; callers should use Femtoseconds for those.
func (s Scale) TickPs() int64 {
	m, _ := Picoseconds(s.Unit)
	return s.Number * m
}

// PrecisionPs is the precision coordinate in picoseconds (defaults to the
// tick when no explicit precision was given).
func (s Scale) PrecisionPs() int64 {
	if !s.HasPrecision {
		return s.TickPs()
	}
	m, _ := Picoseconds(s.PrecisionUnit)
	return s.PrecisionNumber * m
}

func parseToken(tok string) (int64, Unit, error) {
	tok = strings.TrimSpace(tok)
	i := 0
	for i < len(tok) && tok[i] >= '0' && tok[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, "", fmt.Errorf("timescale: %q has no numeric part", tok)
	}
	n, err := strconv.ParseInt(tok[:i], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("timescale: %q: %w", tok, err)
	}
	u := Unit(strings.TrimSpace(tok[i:]))
	if _, ok := Picoseconds(u); !ok {
		return 0, "", fmt.Errorf("timescale: unrecognized unit %q in %q", string(u), tok)
	}
	return n, u, nil
}

// Parse accepts the standard form ("10ns") and an extended form carrying an
// explicit time precision ("10ns precision 100ps" or "10ns/100ps").
func Parse(body string) (Scale, error) {
	raw := strings.TrimSpace(body)
	s := Scale{Raw: raw}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return s, fmt.Errorf("timescale: empty directive")
	}
	// Extended slash form: 10ns/100ps
	if strings.Contains(raw, "/") && len(parts) == 1 {
		two := strings.SplitN(raw, "/", 2)
		n, u, err := parseToken(two[0])
		if err != nil {
			return Scale{Raw: raw}, err
		}
		s.Number, s.Unit = n, u
		pn, pu, err := parseToken(two[1])
		if err != nil {
			return Scale{Raw: raw}, err
		}
		s.PrecisionNumber, s.PrecisionUnit, s.HasPrecision = pn, pu, true
		return s, nil
	}
	switch {
	case len(parts) >= 4 && strings.EqualFold(parts[1], "precision"):
		// 10ns precision 100ps
	case len(parts) == 4 && strings.EqualFold(parts[0], "precision"):
		// precision 100ps after unit; fall through to generic scan below
	}
	// Generic scan: first token = unit spec; optional "precision <spec>".
	n, u, err := parseToken(parts[0])
	if err != nil {
		return Scale{Raw: raw}, err
	}
	s.Number, s.Unit = n, u
	for i := 1; i < len(parts); i++ {
		if strings.EqualFold(parts[i], "precision") && i+1 < len(parts) {
			pn, pu, err := parseToken(parts[i+1])
			if err != nil {
				return Scale{Raw: raw}, err
			}
			s.PrecisionNumber, s.PrecisionUnit, s.HasPrecision = pn, pu, true
		}
	}
	return s, nil
}

// String renders back the retained representation.
func (s Scale) String() string {
	if s.Raw != "" {
		return s.Raw
	}
	return fmt.Sprintf("%d%s", s.Number, s.Unit)
}
