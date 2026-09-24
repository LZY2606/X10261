// Package logic4 implements four-state logic values (0/1/X/Z) and the
// three-valued verdict algebra used by the assertion replay engine.
package logic4

import "fmt"

// Value is a single-bit four-state logic value.
type Value uint8

const (
	V0 Value = iota
	V1
	VX
	VZ
)

// Parse converts one character of a VCD value token into a Value.
// Unknown characters yield VX and ok=false so callers can keep the raw
// token and attach a diagnostic instead of silently cleaning the data.
func Parse(c byte) (v Value, ok bool) {
	switch c {
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

func (v Value) Char() byte {
	switch v {
	case V0:
		return '0'
	case V1:
		return '1'
	case VZ:
		return 'z'
	default:
		return 'x'
	}
}

func (v Value) String() string { return string(v.Char()) }

// IsUnknown reports whether the value is X or Z.
func (v Value) IsUnknown() bool { return v == VX || v == VZ }

// Truth maps a four-state value to a three-valued truth: 0->False,
// 1->True, X/Z->Unknown.
func (v Value) Truth() Tribool {
	switch v {
	case V0:
		return False
	case V1:
		return True
	default:
		return Unknown
	}
}

// Tribool is the three-valued result used for X/Z propagation, empty
// matches, disable-iff and implication outcomes.
type Tribool int8

const (
	False   Tribool = -1
	Unknown Tribool = 0
	True    Tribool = 1
)

func (t Tribool) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	default:
		return "unknown"
	}
}

func ParseTribool(s string) (Tribool, error) {
	switch s {
	case "true":
		return True, nil
	case "false":
		return False, nil
	case "unknown":
		return Unknown, nil
	}
	return Unknown, fmt.Errorf("logic4: bad tribool %q", s)
}

func Not(t Tribool) Tribool { return -t }

func And(a, b Tribool) Tribool {
	if a == False || b == False {
		return False
	}
	if a == True && b == True {
		return True
	}
	return Unknown
}

func Or(a, b Tribool) Tribool {
	if a == True || b == True {
		return True
	}
	if a == False && b == False {
		return False
	}
	return Unknown
}

// Implies is the material implication used by overlapped implication:
// (a -> b) == (!a || b) in three-valued logic.
func Implies(a, b Tribool) Tribool { return Or(Not(a), b) }
