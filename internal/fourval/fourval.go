// Package fourval implements the four-state logic (0/1/X/Z) used for VCD
// signal values and the explicit three-state verdict (pass/fail/unknown)
// used by property evaluation.
package fourval

import "fmt"

// Value is a four-state scalar value.
type Value uint8

const (
	V0 Value = iota // strong zero
	V1              // strong one
	VX              // unknown
	VZ              // high impedance
)

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

// Parse parses a single VCD scalar token character.
func Parse(c byte) (Value, error) {
	switch c {
	case '0':
		return V0, nil
	case '1':
		return V1, nil
	case 'x', 'X':
		return VX, nil
	case 'z', 'Z':
		return VZ, nil
	}
	return VX, fmt.Errorf("fourval: %q is not a 0/1/x/z scalar token", string(rune(c)))
}

// MustParse panics on error; it is intended for tests and embedded fixtures.
func MustParse(c byte) Value {
	v, err := Parse(c)
	if err != nil {
		panic(err)
	}
	return v
}

// IsKnown reports whether v is a driven binary value.
func (v Value) IsKnown() bool { return v == V0 || v == V1 }

// PullZ returns the value with high-impedance pulled to the supplied level.
// X is preserved; this models a pull-up/pull-down network.
func (v Value) PullZ(level Value) Value {
	if v == VZ {
		return level
	}
	return v
}

// Not is logical negation; X and Z both yield X (undriven inputs are unknown).
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

// And is four-state logical AND.
func And(a, b Value) Value {
	if a == V0 || b == V0 {
		return V0
	}
	if a == V1 && b == V1 {
		return V1
	}
	return VX
}

// Or is four-state logical OR.
func Or(a, b Value) Value {
	if a == V1 || b == V1 {
		return V1
	}
	if a == V0 && b == V0 {
		return V0
	}
	return VX
}

// Eq compares two four-state values for boolean equivalence.
// Equality between equal unknowns is NOT known in hardware, so x==x is
// unknown here as well; z==z is unknown too unless both are driven.
func Eq(a, b Value) Value {
	if a.IsKnown() && b.IsKnown() {
		if a == b {
			return V1
		}
		return V0
	}
	return VX
}

// Verdict is the explicit three-state property result.
type Verdict uint8

const (
	VerdictPass Verdict = iota
	VerdictFail
	VerdictUnknown
)

func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "pass"
	case VerdictFail:
		return "fail"
	case VerdictUnknown:
		return "unknown"
	}
	return "?"
}
