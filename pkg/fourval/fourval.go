// Package fourval implements the four-state logic values used by HDL
// waveforms (IEEE 1364 style): 0, 1, X (unknown/uninitialized) and Z
// (high impedance). Boolean reduction always yields an explicit tri-state
// result: True, False or Unknown.
package fourval

import "fmt"

// Value is a four-state scalar.
type Value uint8

const (
	Zero Value = iota // logic 0
	One               // logic 1
	X                 // unknown
	Z                 // high impedance
)

// Parse decodes a VCD scalar token (0,1,x,X,z,Z).
func Parse(s byte) (Value, error) {
	switch s {
	case '0':
		return Zero, nil
	case '1':
		return One, nil
	case 'x', 'X':
		return X, nil
	case 'z', 'Z':
		return Z, nil
	default:
		return X, fmt.Errorf("fourval: invalid scalar token %q", s)
	}
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

// Tri is the explicit three-state boolean result of reducing a four-value.
type Tri uint8

const (
	TriFalse Tri = iota
	TriTrue
	TriUnknown
)

func (t Tri) String() string {
	switch t {
	case TriFalse:
		return "false"
	case TriTrue:
		return "true"
	case TriUnknown:
		return "unknown"
	default:
		return "?"
	}
}

// TriFrom reduces a four-state value to an explicit tri-state boolean.
// Both X and Z reduce to Unknown; neither is silently coerced to 0.
func TriFrom(v Value) Tri {
	switch v {
	case Zero:
		return TriFalse
	case One:
		return TriTrue
	default:
		return TriUnknown
	}
}

// BitVector is a bus value: bits[i] is the i-th bit (index 0 = LSB as
// recorded during parsing, unless the parser reverses order explicitly).
type BitVector struct {
	Bits []Value
	Raw  string // original VCD token, preserved verbatim
}

// Equal compares two bit vectors including X/Z positions.
func (b BitVector) Equal(o BitVector) bool {
	if len(b.Bits) != len(o.Bits) {
		return false
	}
	for i := range b.Bits {
		if b.Bits[i] != o.Bits[i] {
			return false
		}
	}
	return true
}

// Not is logical negation. X and Z propagate as Unknown (X); Z is not
// silently coerced.
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

// And is four-state conjunction.
//
// Propagation rule (explicit):
//   - any 0 input forces 0 (control value), even if another input is X/Z;
//   - otherwise any X/Z input yields X (Z contributes unknown, never 0);
//   - all-ones yields 1.
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
//
//   - any 1 input forces 1, even if another input is X/Z;
//   - otherwise any X/Z input yields X;
//   - all-zeroes yields 0.
func Or(a, b Value) Value {
	if a == One || b == One {
		return One
	}
	if a == Zero && b == Zero {
		return Zero
	}
	return X
}

// Xor is four-state exclusive-or: X/Z propagates unless both operands are
// driven.
func Xor(a, b Value) Value {
	if (a == X || a == Z) || (b == X || b == Z) {
		return X
	}
	if a == b {
		return Zero
	}
	return One
}

// ReduceAnd reduces a bus with the same explicit propagation table as And.
func ReduceAnd(bits []Value) Tri {
	seenUnknown := false
	for _, v := range bits {
		switch v {
		case Zero:
			return TriFalse
		case One:
			continue
		default:
			seenUnknown = true
		}
	}
	if seenUnknown {
		return TriUnknown
	}
	return TriTrue
}

// ReduceOr reduces a bus with the same explicit propagation table as Or.
func ReduceOr(bits []Value) Tri {
	seenUnknown := false
	for _, v := range bits {
		switch v {
		case One:
			return TriTrue
		case Zero:
			continue
		default:
			seenUnknown = true
		}
	}
	if seenUnknown {
		return TriUnknown
	}
	return TriFalse
}
