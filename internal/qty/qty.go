// Package qty holds physical quantities: VCD timescales, integer time grids
// and unit/coordinate metadata. Raw representations are always preserved at
// the source layer; this package only serves the derived normalization layer.
package qty

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Scale orders VCD timescale units by seconds (10^exp).
var scaleOrder = map[string]int{
	"s":  0,
	"ms": -3,
	"us": -6,
	"ns": -9,
	"ps": -12,
	"fs": -15,
}

// Timescale is a parsed `$timescale` directive with its original spelling.
type Timescale struct {
	Number string `json:"number"` // raw token, e.g. "1" or "10"
	Unit   string `json:"unit"`   // normalized unit, e.g. "ns"
	Raw    string `json:"raw"`    // exact directive interior, e.g. "10ns"
	exp    int    // exponent of one unit in seconds
}

var timescaleRe = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s*(s|ms|us|ns|ps|fs)\s*$`)

// ParseTimescale parses a VCD timescale interior. The returned error carries a
// diagnosis and leaves the caller free to keep the value at its raw position.
func ParseTimescale(raw string) (Timescale, error) {
	s := strings.TrimSpace(raw)
	m := timescaleRe.FindStringSubmatch(s)
	if m == nil {
		return Timescale{Raw: s}, fmt.Errorf("qty: unconvertible timescale %q: expected e.g. 1ns/10ps", s)
	}
	return Timescale{Number: m[1], Unit: m[2], Raw: s, exp: scaleOrder[m[2]]}, nil
}

// CanNormalize reports whether the scale is resolvable to an integer grid.
func (t Timescale) CanNormalize() bool { return t.exp != 0 || t.Unit == "s" }

// FemtosecondsPerUnit returns 10^(-15-exp): how many femtoseconds one unit holds.
func (t Timescale) FemtosecondsPerUnit() int64 {
	d := t.exp + 15 // unit exponent relative to fs (fs exp is -15)
	v := int64(1)
	for i := 0; i < d; i++ {
		v *= 10
	}
	return v
}

// UnitStep returns the raw number parsed when it is a positive integer; a
// fractional number yields ok=false so the importer can retain it with a
// diagnostic instead of silently rounding.
func (t Timescale) UnitStep() (int64, bool) {
	if strings.Contains(t.Number, ".") {
		return 0, false
	}
	n, err := strconv.ParseInt(t.Number, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// Grid is an integer time coordinate system, measured in FemtosecondsPerTick
// femtoseconds after the waveform time origin (t=0). The origin never moves:
// counterexample reduction and re-grid both index into the same coordinate
// base and store an offset-free mapping back to raw marks.
type Grid struct {
	FemtosecondsPerTick int64 `json:"femtoseconds_per_tick"`
}

// CommonGrid chooses a common integer grid for two timescales so that both
// raw time marks map to integer ticks. ok=false is returned if either scale
// is fractional (currently unconvertible); the values must stay raw then.
func CommonGrid(a, b Timescale) (Grid, bool) {
	sa, oka := a.UnitStep()
	sb, okb := b.UnitStep()
	if !oka || !okb {
		return Grid{}, false
	}
	fa := sa * a.FemtosecondsPerUnit()
	fb := sb * b.FemtosecondsPerUnit()
	g := gcd(fa, fb)
	if g == 0 {
		return Grid{}, false
	}
	return Grid{FemtosecondsPerTick: g}, true
}

func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// Map converts a raw time mark (in units of ts) to ticks of this grid.
func (g Grid) Map(ts Timescale, rawMark int64) (int64, bool) {
	step, ok := ts.UnitStep()
	if !ok {
		return 0, false
	}
	fsPerRaw := step * ts.FemtosecondsPerUnit()
	if fsPerRaw%g.FemtosecondsPerTick != 0 {
		return 0, false
	}
	return rawMark * (fsPerRaw / g.FemtosecondsPerTick), true
}

// Femtoseconds renders a tick coordinate with its physical unit for display.
func (g Grid) Femtoseconds(tick int64) int64 { return tick * g.FemtosecondsPerTick }
