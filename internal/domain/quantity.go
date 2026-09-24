package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// TimeUnit is the physical time unit carried verbatim from the source.
type TimeUnit string

const (
	Second TimeUnit = "s"
	Milli  TimeUnit = "ms"
	Micro  TimeUnit = "us"
	Nano   TimeUnit = "ns"
	Pico   TimeUnit = "ps"
	Femto  TimeUnit = "fs"
)

var unitOrder = map[TimeUnit]int{Second: 0, Milli: 1, Micro: 2, Nano: 3, Pico: 4, Femto: 5}

// Multiplier returns the integer femtosecond count covered by one unit.
func (u TimeUnit) Multiplier() (int64, error) {
	o, ok := unitOrder[u]
	if !ok {
		return 0, fmt.Errorf("domain: unknown time unit %q", u)
	}
	m := int64(1)
	for i := 0; i < 15-3*o; i++ {
		m *= 10
	}
	return m, nil
}

// Quantity is a domain quantity in its ORIGINAL representation plus a
// normalized integer femtosecond value. The raw token is preserved at
// import; normalization happens only in the derived layer.
type Quantity struct {
	Raw   string   `json:"raw"`
	Value int64    `json:"value"`
	Unit  TimeUnit `json:"unit"`
	Basis string   `json:"basis"` // coordinate basis, e.g. "vcd:abc123" or "global:fs"
	Fs    int64    `json:"fs"`
	Valid bool     `json:"valid"`
	Diag  string   `json:"diag,omitempty"`
}

// Femtoseconds normalizes the quantity (value * unit).
func (q Quantity) Femtoseconds() (int64, error) {
	m, err := q.Unit.Multiplier()
	if err != nil {
		return 0, err
	}
	return q.Value * m, nil
}

// ParseTime parses tokens like "10ns", "1 us", "100ps" and keeps the
// raw text. Unconvertible values stay at their origin with a diagnosis.
func ParseTime(raw, basis string) Quantity {
	q := Quantity{Raw: raw, Basis: basis}
	f := strings.Fields(strings.TrimSpace(strings.ReplaceAll(raw, "µ", "u")))
	if len(f) != 2 {
		q.Diag = "expected '<number> <unit>'"
		return q
	}
	n, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		q.Diag = "number not parseable: " + err.Error()
		return q
	}
	q.Value = n
	q.Unit = TimeUnit(f[1])
	fs, err := q.Femtoseconds()
	if err != nil {
		q.Diag = err.Error()
		return q
	}
	q.Fs, q.Valid = fs, true
	return q
}
