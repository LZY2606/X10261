// Package fourstate implements the 0/1/X/Z four-state logic used when replaying
// waveforms. Z is preserved as a distinct state for provenance but collapses to
// X whenever it participates in a Boolean operation (weak value propagation).
package fourstate

import (
	"errors"
	"strings"
)

// Value is a four-state bit: 0, 1, X (unknown), Z (high impedance).
type Value byte

const (
	V0 Value = iota
	V1
	VX
	VZ
)

// Parse converts a raw token into a four-state value.
// Tokens are kept exactly as they appeared in the source on ParseError.
func Parse(tok string) (Value, error) {
	switch strings.TrimSpace(tok) {
	case "0":
		return V0, nil
	case "1":
		return V1, nil
	case "x", "X":
		return VX, nil
	case "z", "Z":
		return VZ, nil
	default:
		return VX, errors.New("fourstate: unconvertible token: " + tok)
	}
}

// String is the canonical single-character rendering.
func (v Value) String() string {
	switch v {
	case V0:
		return "0"
	case V1:
		return "1"
	case VZ:
		return "Z"
	default:
		return "X"
	}
}

// ToStrong collapses Z to X: Z is never silently treated as 0 or 1.
func (v Value) ToStrong() Value {
	if v == VZ {
		return VX
	}
	return v
}

// IsKnown reports whether the value is a definite logic level.
func (v Value) IsKnown() bool { return v == V0 || v == V1 }

// Not is logical negation; X and Z both yield X.
func Not(a Value) Value {
	switch a.ToStrong() {
	case V0:
		return V1
	case V1:
		return V0
	default:
		return VX
	}
}

// And / Or implement Kleene three-valued logic after Z->X collapse.
func And(a, b Value) Value { return kleene(a.ToStrong(), b.ToStrong(), false) }
func Or(a, b Value) Value  { return kleene(a.ToStrong(), b.ToStrong(), true) }

func kleene(a, b Value, or bool) Value {
	if a == VX || b == VX {
		// A definite side can still dominate when the other side is unknown.
		if or {
			if a == V1 || b == V1 {
				return V1
			}
		} else {
			if a == V0 || b == V0 {
				return V0
			}
		}
		return VX
	}
	var ai, bi int
	if a == V1 {
		ai = 1
	}
	if b == V1 {
		bi = 1
	}
	if or {
		if ai|bi == 1 {
			return V1
		}
		return V0
	}
	if ai&bi == 1 {
		return V1
	}
	return V0
}

// Equal is an exact four-state comparison (used for provenance display).
func Equal(a, b Value) bool { return a == b }
