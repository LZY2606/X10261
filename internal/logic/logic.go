// Package logic implements four-state logic values (0/1/X/Z) and the
// three-valued result algebra used by assertion replay. Every evaluation
// step must produce an explicit three-valued result: True, False, or
// Unknown. Nothing is silently coerced.
package logic

// V is a four-state logic value as found in VCD waveforms.
type V byte

const (
	V0 V = '0'
	V1 V = '1'
	VX V = 'x'
	VZ V = 'z'
)

func ParseV(b byte) (V, bool) {
	switch b {
	case '0':
		return V0, true
	case '1':
		return V1, true
	case 'x', 'X':
		return VX, true
	case 'z', 'Z':
		return VZ, true
	}
	return 0, false
}

func (v V) String() string { return string(byte(v)) }

// T is a three-valued result: True / False / Unknown.
type T int8

const (
	False   T = 0
	True    T = 1
	Unknown T = 2
)

func (t T) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	default:
		return "unknown"
	}
}

func ParseT(s string) (T, bool) {
	switch s {
	case "true":
		return True, true
	case "false":
		return False, true
	case "unknown":
		return Unknown, true
	}
	return 0, false
}

// BoolOf lifts a four-state value into the three-valued domain.
// X and Z both map to Unknown; the original four-state value is
// preserved separately in provenance, never overwritten.
func BoolOf(v V) T {
	switch v {
	case V0:
		return False
	case V1:
		return True
	default:
		return Unknown
	}
}

func Not(a T) T {
	switch a {
	case True:
		return False
	case False:
		return True
	default:
		return Unknown
	}
}

// And implements X/Z propagation: false dominates, unknown is sticky.
func And(a, b T) T {
	if a == False || b == False {
		return False
	}
	if a == Unknown || b == Unknown {
		return Unknown
	}
	return True
}

func Or(a, b T) T {
	if a == True || b == True {
		return True
	}
	if a == Unknown || b == Unknown {
		return Unknown
	}
	return False
}
