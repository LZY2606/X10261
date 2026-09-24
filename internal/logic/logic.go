// Package logic implements four-state HDL values (0/1/X/Z) and the
// explicit three-valued result domain (True/False/Unknown) that every
// judgment in the system must produce.
package logic

// V4 is a four-state logic value.
type V4 byte

const (
	Zero V4 = '0'
	One  V4 = '1'
	X    V4 = 'x'
	Z    V4 = 'z'
)

func ParseV4(c byte) (V4, bool) {
	switch c {
	case '0':
		return Zero, true
	case '1':
		return One, true
	case 'x', 'X':
		return X, true
	case 'z', 'Z', '?':
		return Z, true
	}
	return 0, false
}

func (v V4) String() string { return string(byte(v)) }

// IsUnknown reports whether the value is X or Z.
func (v V4) IsUnknown() bool { return v == X || v == Z }

// TV is an explicit three-valued result. Every rule outcome in the
// system (X/Z propagation, implication, disable iff, empty match)
// must resolve to exactly one of these.
type TV string

const (
	True    TV = "true"
	False   TV = "false"
	Unknown TV = "unknown"
)

func (t TV) Valid() bool { return t == True || t == False || t == Unknown }

// TVOf lifts a four-state value into the three-valued domain.
func TVOf(v V4) TV {
	switch v {
	case One:
		return True
	case Zero:
		return False
	default:
		return Unknown
	}
}

func Not(t TV) TV {
	switch t {
	case True:
		return False
	case False:
		return True
	default:
		return Unknown
	}
}

func And(a, b TV) TV {
	if a == False || b == False {
		return False
	}
	if a == True && b == True {
		return True
	}
	return Unknown
}

func Or(a, b TV) TV {
	if a == True || b == True {
		return True
	}
	if a == False && b == False {
		return False
	}
	return Unknown
}

// Eq4 compares two four-state values under a semantic version.
//   - "v1": any X/Z operand makes the result Unknown (strict 4-state).
//   - "v2": X/Z act as wildcards that match anything (structural match),
//     so the comparison collapses to a definite True.
//
// The difference between v1 and v2 is the semantic-version axis used by
// the archive to demonstrate version-dependent conclusions.
func Eq4(semVersion string, a, b V4) TV {
	switch semVersion {
	case "v1":
		if a.IsUnknown() || b.IsUnknown() {
			return Unknown
		}
		if a == b {
			return True
		}
		return False
	case "v2":
		if a.IsUnknown() || b.IsUnknown() {
			return True // wildcard match
		}
		if a == b {
			return True
		}
		return False
	default:
		return Unknown
	}
}
