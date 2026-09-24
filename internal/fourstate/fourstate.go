// Package fourstate implements 0/1/X/Z four-state logic with explicit
// three-valued verdicts (True/False/Unknown) used by the replay engine.
package fourstate

import "fmt"

// Value is a four-state logic value.
type Value uint8

const (
	V0 Value = iota
	V1
	VX
	VZ
)

func Parse(ch byte) (Value, error) {
	switch ch {
	case '0':
		return V0, nil
	case '1':
		return V1, nil
	case 'x', 'X':
		return VX, nil
	case 'z', 'Z':
		return VZ, nil
	}
	return V0, fmt.Errorf("fourstate: bad value %q", ch)
}

func (v Value) String() string {
	switch v {
	case V0:
		return "0"
	case V1:
		return "1"
	case VX:
		return "X"
	case VZ:
		return "Z"
	}
	return "?"
}

// Verdict is the explicit three-valued result of any rule evaluation.
type Verdict uint8

const (
	False   Verdict = iota // definite 0
	True                   // definite 1
	Unknown                // X/Z involvement makes the result undecidable
)

func (t Verdict) String() string {
	switch t {
	case True:
		return "TRUE"
	case False:
		return "FALSE"
	default:
		return "UNKNOWN"
	}
}

// Lift converts a four-state value to a three-valued verdict.
// X and Z both map to Unknown (X/Z propagation rule).
func Lift(v Value) Verdict {
	switch v {
	case V1:
		return True
	case V0:
		return False
	default:
		return Unknown
	}
}

func Not(t Verdict) Verdict {
	switch t {
	case True:
		return False
	case False:
		return True
	default:
		return Unknown
	}
}

func And(a, b Verdict) Verdict {
	if a == False || b == False {
		return False
	}
	if a == True && b == True {
		return True
	}
	return Unknown
}

func Or(a, b Verdict) Verdict {
	if a == True || b == True {
		return True
	}
	if a == False && b == False {
		return False
	}
	return Unknown
}

// XZInvolved reports whether any of the values is X or Z.
func XZInvolved(vals ...Value) bool {
	for _, v := range vals {
		if v == VX || v == VZ {
			return true
		}
	}
	return false
}
