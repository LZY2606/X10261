// Package fourstate implements the four-valued logic (0/1/X/Z) used when
// replaying waveform assertions. Every signal value in the system carries one
// of these states; unknown (X) and high-impedance (Z) are never silently
// coerced into 0 or 1.
package fourstate

import (
	"errors"
	"strings"
)

// Value is a four-state scalar.
type Value uint8

const (
	Zero Value = iota // 0, driven low
	One               // 1, driven high
	X                 // unknown / contention
	Z                 // high impedance
)

// Parse converts a raw VCD token ("0","1","x","X","z","Z") into a Value.
// Empty input is an error: missing data must be diagnosed, not assumed.
func Parse(s string) (Value, error) {
	switch strings.TrimSpace(s) {
	case "0":
		return Zero, nil
	case "1":
		return One, nil
	case "x", "X":
		return X, nil
	case "z", "Z":
		return Z, nil
	default:
		return X, errors.New("fourstate: unconvertible scalar token: " + quote(s))
	}
}

func quote(s string) string {
	if s == "" {
		return "<empty>"
	}
	return s
}

// String returns the canonical single-character representation.
func (v Value) String() string {
	switch v {
	case Zero:
		return "0"
	case One:
		return "1"
	case X:
		return "x"
	case Z:
		return "z"
	default:
		return "?"
	}
}

// IsKnown reports whether the value is a driven, defined level.
func (v Value) IsKnown() bool { return v == Zero || v == One }

// Not is four-state negation: X and Z both propagate as X (the level of a
// floating net cannot be determined).
func Not(v Value) Value {
	switch v {
	case Zero:
		return One
	case One:
		return Zero
	default:
		return X
	}
}

// And is four-state conjunction. Z inputs are treated as X for level
// resolution (float level is unknown) and recorded separately by callers via
// IsZ. Resolution order: definite result wins over unknown.
func And(a, b Value) Value {
	if a == Zero || b == Zero {
		return Zero
	}
	if a == One && b == One {
		return One
	}
	return X
}

// Or is four-state disjunction.
func Or(a, b Value) Value {
	if a == One || b == One {
		return One
	}
	if a == Zero && b == Zero {
		return Zero
	}
	return X
}

// IsZ reports a high-impedance value, kept distinct from X for provenance.
func (v Value) IsZ() bool { return v == Z }

// Kind classifies a value for machine-readable propagation events.
func (v Value) Kind() string {
	switch v {
	case Zero:
		return "zero"
	case One:
		return "one"
	case X:
		return "unknown"
	case Z:
		return "high_impedance"
	default:
		return "invalid"
	}
}
