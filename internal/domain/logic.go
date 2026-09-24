package domain

// Logic is the four-state VHDL/Verilog value: 0, 1, X (unknown), Z (high-Z).
type Logic int8

const (
	L0 Logic = iota
	L1
	LX
	LZ
)

func (l Logic) Char() byte {
	switch l {
	case L0:
		return '0'
	case L1:
		return '1'
	case LX:
		return 'x'
	default:
		return 'z'
	}
}

func (l Logic) String() string { return string([]byte{l.Char()}) }

// ParseLogic parses a single four-state character. Anything outside
// 0/1/x/X/z/Z is treated as X and the second return value reports whether
// the raw token was a canonical four-state value.
func ParseLogic(c byte) (Logic, bool) {
	switch c {
	case '0':
		return L0, true
	case '1':
		return L1, true
	case 'x', 'X':
		return LX, true
	case 'z', 'Z':
		return LZ, true
	default:
		return LX, false
	}
}

// Not implements four-state negation: not X = X, not Z = X
// (high-impedance read in a boolean context is unknown, not 0).
func Not(a Logic) Logic {
	switch a {
	case L0:
		return L1
	case L1:
		return L0
	default:
		return LX
	}
}

// And implements four-state conjunction:
//   - a definite 0 forces false regardless of X/Z
//   - otherwise X/Z contaminates the result to X
func And(a, b Logic) Logic {
	if a == L0 || b == L0 {
		return L0
	}
	if a == L1 && b == L1 {
		return L1
	}
	return LX
}

// Or implements four-state disjunction:
//   - a definite 1 forces true regardless of X/Z
//   - otherwise X/Z contaminates the result to X
func Or(a, b Logic) Logic {
	if a == L1 || b == L1 {
		return L1
	}
	if a == L0 && b == L0 {
		return L0
	}
	return LX
}

// Tri is the explicit three-valued assertion result.
type Tri int8

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
	default:
		return "unknown"
	}
}

// BoolToTri converts a four-state boolean expression to the tri-state result.
func BoolToTri(l Logic) Tri {
	switch l {
	case L0:
		return TriFalse
	case L1:
		return TriTrue
	default:
		return TriUnknown
	}
}
