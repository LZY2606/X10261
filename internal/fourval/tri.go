package fourval

// Tri is the explicit three-valued verdict required for X/Z propagation,
// implication, disable iff and empty matching: false / true / unknown.
// It is never a default zero value accident: the zero value is TriUnknown,
// and constructors must be used.
type Tri uint8

const (
	TriUnknown Tri = iota
	TriFalse
	TriTrue
)

// FromValue maps a four-state signal value to a definite Boolean.
// Both X and Z map to unknown: Z cannot quietly become 0 or 1.
func FromValue(v Value) Tri {
	switch v {
	case V0:
		return TriFalse
	case V1:
		return TriTrue
	default:
		return TriUnknown
	}
}

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

// IsDefinite reports false for the unknown state.
func (t Tri) IsDefinite() bool { return t != TriUnknown }

// NotTri is three-valued negation.
func NotTri(t Tri) Tri {
	switch t {
	case TriFalse:
		return TriTrue
	case TriTrue:
		return TriFalse
	default:
		return TriUnknown
	}
}
