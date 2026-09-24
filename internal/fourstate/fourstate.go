// Package fourstate implements the four-state logic of HDL values (0/1/X/Z)
// and the three-state (true/false/unknown) projection used by assertions.
//
// The raw waveform value is always preserved as V4; only the derived
// boolean projection coerces Z to unknown, and that coercion is reported.
package fourstate

// V4 is a four-state HDL scalar value.
type V4 int

const (
	V0 V4 = iota // driven low
	V1           // driven high
	VX           // unknown
	VZ           // high-impedance
)

// String returns the canonical single-rune representation.
func (v V4) String() string {
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

// Parse parses a VCD scalar token ("0","1","x","X","z","Z") into a V4.
func Parse(s string) (V4, bool) {
	if len(s) == 0 {
		return VX, false
	}
	switch s[0] {
	case '0':
		return V0, true
	case '1':
		return V1, true
	case 'x', 'X':
		return VX, true
	case 'z', 'Z':
		return VZ, true
	}
	return VX, false
}

// T3 is a three-state boolean truth value.
type T3 int

const (
	TFalse T3 = iota
	TTrue
	TUnknown
)

// String returns false / true / unknown.
func (t T3) String() string {
	switch t {
	case TFalse:
		return "false"
	case TTrue:
		return "true"
	case TUnknown:
		return "unknown"
	}
	return "?"
}

// Bool returns the definite boolean and whether it was definite.
// Unknown and Z both project to (false,false).
func (t T3) Bool() (bool, bool) {
	switch t {
	case TTrue:
		return true, true
	case TFalse:
		return false, true
	}
	return false, false
}

// ToT3 projects a four-state value onto three-state logic.
// Z is high-impedance, not a definite logic value: it projects to unknown
// (never silently to false). X also projects to unknown.
func ToT3(v V4) T3 {
	switch v {
	case V0:
		return TFalse
	case V1:
		return TTrue
	}
	return TUnknown
}

// IsZ reports whether the raw four-state value is high-impedance.
func IsZ(v V4) bool { return v == VZ }

// Not is Kleene negation on three-state values.
func Not(a T3) T3 {
	switch a {
	case TTrue:
		return TFalse
	case TFalse:
		return TTrue
	}
	return TUnknown
}

// And is Kleene conjunction: unknown survives unless one side is definitely
// false (false AND unknown == false).
func And(a, b T3) T3 {
	if a == TFalse || b == TFalse {
		return TFalse
	}
	if a == TUnknown || b == TUnknown {
		return TUnknown
	}
	return TTrue
}

// Or is Kleene disjunction: unknown survives unless one side is definitely
// true (true OR unknown == true).
func Or(a, b T3) T3 {
	if a == TTrue || b == TTrue {
		return TTrue
	}
	if a == TUnknown || b == TUnknown {
		return TUnknown
	}
	return TFalse
}

// Xor is Kleene exclusive or; it is definite only when both sides are.
func Xor(a, b T3) T3 {
	if a == TUnknown || b == TUnknown {
		return TUnknown
	}
	if a == b {
		return TFalse
	}
	return TTrue
}

// Equal is definite only when both sides are known and equal.
func Equal(a, b T3) T3 {
	if a == TUnknown || b == TUnknown {
		return TUnknown
	}
	if a == b {
		return TTrue
	}
	return TFalse
}
