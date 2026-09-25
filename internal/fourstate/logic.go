// Package fourstate implements the four-valued logic (0/1/X/Z) used when
// replaying sampled assertions. Every combining operation is total: an
// unknown control input always yields an explicit UNKNOWN result rather than
// silently collapsing to true or false.
package fourstate

// Value is a four-state signal value.
type Value uint8

const (
	V0 Value = iota // strong zero
	V1              // strong one
	VX              // unknown / uninitialized / contention
	VZ              // high-impedance
)

// Parse converts a textual token ("0","1","x","X","z","Z") into a Value.
// Tokens "?" and "b" (VCD "unknown"/"both" shorthand used by some dumpers)
// map to X. Any other token yields ok=false so the caller can retain the
// raw representation with a diagnostic instead of inventing a value.
func Parse(tok string) (Value, bool) {
	switch tok {
	case "0":
		return V0, true
	case "1":
		return V1, true
	case "x", "X", "?", "b":
		return VX, true
	case "z", "Z":
		return VZ, true
	default:
		return VX, false
	}
}

// String renders the canonical single-character token.
func (v Value) String() string {
	switch v {
	case V0:
		return "0"
	case V1:
		return "1"
	case VX:
		return "x"
	case VZ:
		return "z"
	default:
		return "?"
	}
}

// IsKnown reports whether the value carries a definite binary strength.
// Both X and Z are unknown in boolean control contexts.
func (v Value) IsKnown() bool { return v == V0 || v == V1 }

// Bool projects the value into the boolean domain. X and Z return
// ok=false; callers MUST propagate that unknown-ness instead of guessing.
func (v Value) Bool() (bool, bool) {
	switch v {
	case V0:
		return false, true
	case V1:
		return true, true
	default:
		return false, false
	}
}

// Not is total four-state negation. X and Z stay unknown (Z loses drive
// strength information under an explicit boolean operator, becoming X).
func Not(a Value) Value {
	switch a {
	case V0:
		return V1
	case V1:
		return V0
	default:
		return VX
	}
}

// And implements four-state AND with explicit X/Z propagation.
// A controlling 0 forces 0 regardless of unknowns; otherwise any unknown
// yields X.
func And(a, b Value) Value {
	if a == V0 || b == V0 {
		return V0
	}
	if a == V1 && b == V1 {
		return V1
	}
	return VX
}

// Or implements four-state OR. A controlling 1 forces 1; otherwise any
// unknown yields X.
func Or(a, b Value) Value {
	if a == V1 || b == V1 {
		return V1
	}
	if a == V0 && b == V0 {
		return V0
	}
	return VX
}

// Xor implements four-state XOR; any unknown operand yields X.
func Xor(a, b Value) Value {
	if !a.IsKnown() || !b.IsKnown() {
		return VX
	}
	if a == b {
		return V0
	}
	return V1
}

// ReduceAnd folds a list of values with four-state AND.
func ReduceAnd(vs []Value) Value {
	r := V1
	for _, v := range vs {
		r = And(r, v)
	}
	return r
}

// ReduceOr folds a list of values with four-state OR.
func ReduceOr(vs []Value) Value {
	r := V0
	for _, v := range vs {
		r = Or(r, v)
	}
	return r
}
