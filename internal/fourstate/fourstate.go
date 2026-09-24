// Package fourstate implements the four-valued logic values used by HDL
// waveforms (0, 1, X, Z) and the two semantic revisions used by the replay
// engine.
//
// Semantic versions:
//
//	semver-1.0 "pessimistic": any X/Z operand forces a ternary result to X
//	                          (the operand, even if it is Z, is treated as
//	                          unresolved); Z is never a controlling value.
//	semver-1.1 "controlling-value" (IEEE-like): the resolved 0/1 values use
//	                          gate controlling values (AND:0, OR:1, XOR:none);
//	                          Z is coalesced to X before the gate table, but
//	                          an explicit Z operand is observable through
//	                          ExplainZ and still yields a ternary X.
//
// Both versions ALWAYS produce one of the three ternary outcomes
// {true, false, unknown} for a boolean read: X and Z never silently become
// 0 or 1. They only disagree on whether a controlling 0/1 operand can
// rescue a gate whose other operand is unresolved.
package fourstate

import "fmt"

// Value is a four-state scalar bit.
type Value uint8

const (
	X Value = iota // unknown / uninitialized
	Z              // high impedance
	V0             // driven low
	V1             // driven high
)

// Parse decodes a single VCD scalar token character (case-insensitive).
func Parse(c byte) (Value, bool) {
	switch c {
	case '0', 'l', 'L':
		return V0, true
	case '1', 'h', 'H':
		return V1, true
	case 'x', 'X':
		return X, true
	case 'z', 'Z', '?':
		return Z, true
	}
	return X, false
}

// String renders the canonical single-character token.
func (v Value) String() string {
	switch v {
	case V0:
		return "0"
	case V1:
		return "1"
	case Z:
		return "z"
	default:
		return "x"
	}
}

// IsResolved reports whether the value is a definite 0 or 1.
func (v Value) IsResolved() bool { return v == V0 || v == V1 }

// Ternary converts the four-state value to the mandatory three-state boolean
// result: V1 -> true, V0 -> false, X/Z -> unknown. The caller receives the
// distinction between X and Z through HasZ.
func (v Value) Ternary() (b bool, known bool) {
	switch v {
	case V1:
		return true, true
	case V0:
		return false, true
	default:
		return false, false
	}
}

// HasZ reports whether the value originates from high impedance.
func (v Value) HasZ() bool { return v == Z }

// ResolutionVersion identifies a semantic revision.
type ResolutionVersion string

const (
	SemVer10 ResolutionVersion = "semver-1.0"
	SemVer11 ResolutionVersion = "semver-1.1"
)

// ResolveWire applies Verilog wire resolution to concurrently driven values:
// two drivers are combined per the 4-state resolution table.
//
//	0/1 conflict -> X ; Z yields to anything ; X dominates
func ResolveWire(a, b Value) Value {
	if a == Z {
		return b
	}
	if b == Z {
		return a
	}
	if a == X || b == X {
		return X
	}
	if a != b {
		return X
	}
	return a
}

// And is the 4-state AND gate under the given semantic revision.
func And(a, b Value, ver ResolutionVersion) Value {
	return gate(a, b, ver, V0)
}

// Or is the 4-state OR gate.
func Or(a, b Value, ver ResolutionVersion) Value {
	return gate(a, b, ver, V1)
}

// Xor is the 4-state XOR gate. XOR has no controlling value so any
// unresolved operand yields X in both revisions.
func Xor(a, b Value, _ ResolutionVersion) Value {
	if !a.IsResolved() || !b.IsResolved() {
		return X
	}
	if a == b {
		return V0
	}
	return V1
}

// Not is the 4-state inverter.
func Not(a Value) Value {
	switch a {
	case V0:
		return V1
	case V1:
		return V0
	default:
		return X
	}
}

// gate implements the AND/OR controlling-value tables. controlling is V0
// for AND and V1 for OR.
func gate(a, b Value, ver ResolutionVersion, controlling Value) Value {
	switch ver {
	case SemVer11:
		// Z behaves like X at a gate input, but a definite controlling
		// value decides the gate regardless of the other operand.
		if a == controlling || b == controlling {
			return controlling
		}
		if a.IsResolved() && b.IsResolved() {
			if controlling == V0 { // AND, no 0 present
				return V1
			}
			return V0 // OR, no 1 present
		}
		return X
	default: // SemVer10: pessimistic, every unresolved operand contaminates.
		if !a.IsResolved() || !b.IsResolved() {
			return X
		}
		if controlling == V0 {
			if a == V0 || b == V0 {
				return V0
			}
			return V1
		}
		if a == V1 || b == V1 {
			return V1
		}
		return V0
	}
}

// Edge classifies a clock transition for edge detection. X/Z never produce
// a definite edge under any revision.
type Edge uint8

const (
	EdgeNone Edge = iota
	EdgeRise
	EdgeFall
)

// DetectEdge classifies the transition prev -> cur. A transition involving
// X or Z is EdgeNone (an unknown transition cannot be a sampling edge).
func DetectEdge(prev, cur Value) Edge {
	if prev == V0 && cur == V1 {
		return EdgeRise
	}
	if prev == V1 && cur == V0 {
		return EdgeFall
	}
	return EdgeNone
}

// ExplainZ returns a stable diagnostic tag when v originates from Z.
func ExplainZ(v Value) string {
	if v != Z {
		return ""
	}
	return "z-propagation:high-impedance-coalesced-to-x"
}

// CheckVersion validates a semantic revision tag.
func CheckVersion(ver ResolutionVersion) error {
	switch ver {
	case SemVer10, SemVer11:
		return nil
	}
	return fmt.Errorf("fourstate: unknown semantic version %q", ver)
}
