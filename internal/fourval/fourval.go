// Package fourval implements Verilog four-state logic (0/1/X/Z).
//
// Z is a real signal value (high impedance) and is never silently erased.
// When Z enters a Boolean expression the Verilog LRM treats it as an
// unknown, so gate operations return X: callers must distinguish "value is
// Z" from "value is X" via Equal and coerce explicitly with ToBoolTri.
package fourval

// Value is one of V0, V1, VX (unknown) or VZ (high impedance).
type Value uint8

const (
	V0 Value = iota // logic zero
	V1              // logic one
	VX              // unknown
	VZ              // high impedance
)

// Parse parses a single Verilog value character: 0 1 x X z Z.
// VCD also uses ? for X/Z repeats in vector values; those are handled by
// the vector parser rather than Parse.
func Parse(c byte) (Value, bool) {
	switch c {
	case '0':
		return V0, true
	case '1':
		return V1, true
	case 'x', 'X':
		return VX, true
	case 'z', 'Z':
		return VZ, true
	default:
		return VX, false
	}
}

// String returns the canonical single character.
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
	}
	return "?"
}

// IsUnknown reports whether the value cannot drive a definite Boolean
// result (X) or is high impedance (Z).
func (v Value) IsUnknown() bool { return v == VX || v == VZ }

// Not implements the Verilog inverter. Z/X both invert to X because the
// driven output of an inverter with no definite input is unknown.
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

// And implements four-state conjunction. A definite 0 dominates (this is
// why an X on one input cannot corrupt an AND another input pulls low);
// otherwise any X/Z yields X.
func And(a, b Value) Value {
	if a == V0 || b == V0 {
		return V0
	}
	if a.IsUnknown() || b.IsUnknown() {
		return VX
	}
	return V1
}

// Or implements four-state disjunction. A definite 1 dominates.
func Or(a, b Value) Value {
	if a == V1 || b == V1 {
		return V1
	}
	if a.IsUnknown() || b.IsUnknown() {
		return VX
	}
	return V0
}

// Xor returns X whenever either operand is not definite.
func Xor(a, b Value) Value {
	if a.IsUnknown() || b.IsUnknown() {
		return VX
	}
	if a == b {
		return V0
	}
	return V1
}

// Equal is equality of the four states (Z == Z, X == X); it is not a gate
// evaluation and is used for provenance/identity comparisons.
func Equal(a, b Value) bool { return a == b }
