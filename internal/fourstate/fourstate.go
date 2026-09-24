// Package fourstate implements the four-valued logic (0/1/X/Z) used when
// replaying imported waveforms. X and Z are first-class values, never
// silently dropped or converted for rendering convenience.
package fourstate

import "fmt"

// Value is a four-state logic value.
type Value uint8

const (
	Zero Value = iota // strong/known false 0
	One               // strong/known true  1
	X                 // unknown / conflict
	Z                 // high-impedance
)

// Parse converts a textual token ("0","1","x","X","z","Z") into a Value.
// An empty token means unknown: X with an explicit error so callers can
// attach a diagnostic rather than silently dropping the value.
func Parse(s string) (Value, error) {
	switch s {
	case "0":
		return Zero, nil
	case "1":
		return One, nil
	case "x", "X":
		return X, nil
	case "z", "Z":
		return Z, nil
	default:
		return X, fmt.Errorf("unconvertible four-state token %q: kept at origin with diagnostic", s)
	}
}

func (v Value) String() string {
	switch v {
	case Zero:
		return "0"
	case One:
		return "1"
	case X:
		return "X"
	case Z:
		return "Z"
	default:
		return "?"
	}
}

// IsKnown reports whether the value carries Boolean strength.
func (v Value) IsKnown() bool { return v == Zero || v == One }

// Not is four-state negation. X and Z stay non-Boolean (they propagate).
func Not(a Value) Value {
	switch a {
	case Zero:
		return One
	case One:
		return Zero
	default:
		return a
	}
}

// And is four-state conjunction following Verilog resolution semantics:
// a single strong 0 forces 0; otherwise X/Z propagate instead of becoming 1.
func And(a, b Value) Value {
	if a == Zero || b == Zero {
		return Zero
	}
	if a == One && b == One {
		return One
	}
	if a == X || b == X {
		return X
	}
	return Z
}

// Or is four-state disjunction: a single strong 1 forces 1.
func Or(a, b Value) Value {
	if a == One || b == One {
		return One
	}
	if a == Zero && b == Zero {
		return Zero
	}
	if a == X || b == X {
		return X
	}
	return Z
}

// Xor keeps X/Z as unknown rather than guessing parity.
func Xor(a, b Value) Value {
	if !a.IsKnown() || !b.IsKnown() {
		if a == X || b == X || (a == Z && b == Z) {
			return X
		}
		return Z
	}
	if a == b {
		return Zero
	}
	return One
}
