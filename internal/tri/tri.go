// Package tri implements the four-state logic (0/1/X/Z) and the explicit
// three-state verdict (Pass/Fail/Unknown) used by the fixed-sampling replay.
package tri

// L is a four-state level: 0, 1, X (unknown/conflict) or Z (high impedance).
type L int8

const (
	X L = iota
	Z
	L0
	L1
)

// ParseLevel converts a single Verilog VCD level character.
func ParseLevel(c byte) (L, bool) {
	switch c {
	case '0':
		return L0, true
	case '1':
		return L1, true
	case 'x', 'X':
		return X, true
	case 'z', 'Z':
		return Z, true
	}
	return X, false
}

// String renders the canonical single-character representation.
func (l L) String() string {
	switch l {
	case L0:
		return "0"
	case L1:
		return "1"
	case X:
		return "X"
	case Z:
		return "Z"
	}
	return "?"
}

// IsKnown reports whether the level is a resolvable boolean level.
func (l L) IsKnown() bool { return l == L0 || l == L1 }

// Resolve applies Verilog weak-X semantics: an unconnected high-Z input pulls
// to X rather than to 0 or 1. The source level is retained separately.
func (l L) Resolve() L {
	if l == Z {
		return X
	}
	return l
}

// Not is the four-state negation.
func Not(a L) L {
	switch a {
	case L0:
		return L1
	case L1:
		return L0
	}
	return X
}

// And is the four-state conjunction.
func And(a, b L) L {
	a, b = a.Resolve(), b.Resolve()
	if a == L0 || b == L0 {
		return L0
	}
	if a == L1 && b == L1 {
		return L1
	}
	return X
}

// Or is the four-state disjunction.
func Or(a, b L) L {
	a, b = a.Resolve(), b.Resolve()
	if a == L1 || b == L1 {
		return L1
	}
	if a == L0 && b == L0 {
		return L0
	}
	return X
}

// Verdict is the explicit three-state result required by the derivation rules.
type Verdict int8

const (
	Unknown Verdict = iota
	Pass
	Fail
	Disabled // reset/disable-iff region: a fourth administrative outcome
)

func (v Verdict) String() string {
	switch v {
	case Pass:
		return "pass"
	case Fail:
		return "fail"
	case Disabled:
		return "disabled"
	}
	return "unknown"
}

// Tri renders the administrative Disabled state as Unknown for the mandated
// three-state result surface; callers carry Disabled separately when needed.
func (v Verdict) Tri() Verdict {
	if v == Disabled {
		return Unknown
	}
	return v
}
