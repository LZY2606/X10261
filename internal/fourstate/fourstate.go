// Package fourstate implements IEEE-1364 four-state (0/1/X/Z) bit logic and the
// tri-state (true/false/unknown) verdicts used by the property engine.
package fourstate

import "fmt"

// Bit is a single VCD four-state bit.
type Bit uint8

const (
	Zero Bit = iota
	One
	X // unknown / contention
	Z // high impedance
)

// Parse converts a single VCD bit token (one of 0 1 x X z Z) into a Bit.
func Parse(r byte) (Bit, error) {
	switch r {
	case '0':
		return Zero, nil
	case '1':
		return One, nil
	case 'x', 'X':
		return X, nil
	case 'z', 'Z':
		return Z, nil
	}
	return X, fmt.Errorf("fourstate: not a four-state bit: %q", r)
}

func (b Bit) String() string {
	switch b {
	case Zero:
		return "0"
	case One:
		return "1"
	case X:
		return "x"
	case Z:
		return "z"
	}
	return "?"
}

// IsUnknown reports whether the bit cannot be resolved to 0 or 1.
func (b Bit) IsUnknown() bool { return b == X || b == Z }

// And is the IEEE four-state AND table.
func And(a, b Bit) Bit {
	switch {
	case a == Zero || b == Zero:
		return Zero
	case a == One && b == One:
		return One
	case a == One && b.IsUnknown():
		return b
	case b == One && a.IsUnknown():
		return a
	}
	return X
}

// Or is the IEEE four-state OR table.
func Or(a, b Bit) Bit {
	switch {
	case a == One || b == One:
		return One
	case a == Zero && b == Zero:
		return Zero
	case a == Zero && b.IsUnknown():
		return b
	case b == Zero && a.IsUnknown():
		return a
	}
	return X
}

// Not flips 0/1 and maps X->X, Z->X (Z is unknowable in a boolean context).
func Not(a Bit) Bit {
	switch a {
	case Zero:
		return One
	case One:
		return Zero
	}
	return X
}

// Verdict is the explicit three-state result used for every derived decision.
type Verdict uint8

const (
	VFalse Verdict = iota
	VTrue
	VUnknown
)

func (v Verdict) String() string {
	switch v {
	case VFalse:
		return "false"
	case VTrue:
		return "true"
	case VUnknown:
		return "unknown"
	}
	return "?"
}

// Tri converts a resolved four-state bit into a tri-state verdict.
// X and Z both yield Unknown; X-vs-Z provenance is carried separately.
func Tri(b Bit) Verdict {
	switch b {
	case Zero:
		return VFalse
	case One:
		return VTrue
	default:
		return VUnknown
	}
}

// NotV is tri-state negation: unknown stays unknown.
func NotV(v Verdict) Verdict {
	switch v {
	case VFalse:
		return VTrue
	case VTrue:
		return VFalse
	default:
		return VUnknown
	}
}

// AndV is tri-state conjunction (unknown is absorbing only with no false term).
func AndV(a, b Verdict) Verdict {
	if a == VFalse || b == VFalse {
		return VFalse
	}
	if a == VUnknown || b == VUnknown {
		return VUnknown
	}
	return VTrue
}

// OrV is tri-state disjunction.
func OrV(a, b Verdict) Verdict {
	if a == VTrue || b == VTrue {
		return VTrue
	}
	if a == VUnknown || b == VUnknown {
		return VUnknown
	}
	return VFalse
}
