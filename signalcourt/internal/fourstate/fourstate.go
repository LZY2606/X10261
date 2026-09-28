// Package fourstate implements IEEE 1364 four-state logic values (0/1/X/Z)
// together with a strict three-state (TRUE/FALSE/X) verdict layer.
//
// Z is kept as a distinct value for storage and display. In boolean contexts
// Z is coerced to X (high impedance is not a known logical level), so X/Z
// propagation yields the explicit three-state result X rather than silently
// becoming 0 or 1.
package fourstate

import "fmt"

// T is a four-state logic value.
type T uint8

const (
	Zero T = iota // 0
	One           // 1
	X             // unknown
	Z             // high impedance (distinct from X in storage; X in logic)
)

// Parse converts a single VCD/Verilog scalar byte into a T.
func Parse(c byte) (T, error) {
	switch c {
	case '0':
		return Zero, nil
	case '1':
		return One, nil
	case 'x', 'X':
		return X, nil
	case 'z', 'Z':
		return Z, nil
	}
	return X, fmt.Errorf("fourstate: invalid scalar byte %q", c)
}

// Rune returns the canonical representation.
func (v T) Rune() rune {
	switch v {
	case Zero:
		return '0'
	case One:
		return '1'
	case X:
		return 'x'
	case Z:
		return 'z'
	}
	return 'x'
}

func (v T) String() string { return string(v.Rune()) }

// Known reports whether v is a definite 0 or 1.
func (v T) Known() bool { return v == Zero || v == One }

// Bool is valid only for Known values.
func (v T) Bool() bool {
	if !v.Known() {
		panic("fourstate: Bool on unknown value")
	}
	return v == One
}

// CoerceTo3 maps a four-state value to the strict three-state logic layer:
// both X and Z become the unknown truth value X3.
func CoerceTo3(v T) T {
	if v == Z {
		return X
	}
	return v
}

// And is the IEEE 1364 four-state AND: an unknown input only forces X when
// no definite input already determines the result (0 dominates, then X, 1).
func And(a, b T) T {
	if a == Zero || b == Zero {
		return Zero
	}
	if a == One && b == One {
		return One
	}
	return X
}

// Or is the IEEE 1364 four-state OR (1 dominates, then X, 0).
func Or(a, b T) T {
	if a == One || b == One {
		return One
	}
	if a == Zero && b == Zero {
		return Zero
	}
	return X
}

// Not is logical negation; X and Z stay unknown.
func Not(a T) T {
	switch a {
	case Zero:
		return One
	case One:
		return Zero
	default:
		return X
	}
}

// Merge joins concurrent evaluations: equal definite values agree, anything
// else is unknown (X/Z never silently resolve to 0/1).
func Merge(a, b T) T {
	if a == b {
		return a
	}
	if a == Z {
		return Merge(CoerceTo3(a), b)
	}
	if b == Z {
		return Merge(a, CoerceTo3(b))
	}
	return X
}

// Compare is a three-state equality on four-state operands.
func Compare(a, b T) T {
	if a == Z || b == Z || a == X || b == X {
		return X
	}
	if a == b {
		return One
	}
	return Zero
}

// Mux is a three-state multiplexer: unknown select yields X.
func Mux(sel, a, b T) T {
	switch CoerceTo3(sel) {
	case One:
		return a
	case Zero:
		return b
	default:
		return X
	}
}

// Equal is exact four-state equality.
func Equal(a, b T) bool { return a == b }

// IsUnknown reports whether logic evaluation cannot decide (X or Z coercion).
func IsUnknown(v T) bool { return !v.Known() }
