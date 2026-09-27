// Package logic4 implements four-state logic values (0/1/X/Z) used
// throughout the signalcourt archive. Raw tokens are always preserved
// alongside normalized values.
package logic4

import "fmt"

// V is a four-state logic value.
type V byte

const (
	V0 V = '0'
	V1 V = '1'
	VX V = 'x'
	VZ V = 'z'
)

// Parse converts a VCD/token byte into a four-state value.
// The second return value reports whether the token was recognized;
// unrecognized tokens must be kept raw by callers plus a diagnostic.
func Parse(b byte) (V, bool) {
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

// Known reports whether v is 0 or 1 (not X/Z).
func (v V) Known() bool { return v == V0 || v == V1 }

// T is a three-state truth value: true, false, or unknown (X/Z induced).
type T int8

const (
	False T = -1
	Unknown T = 0
	True  T = 1
)

func (t T) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	}
	return "unknown"
}

// FromBool lifts a boolean into the three-state domain.
func FromBool(b bool) T {
	if b {
		return True
	}
	return False
}

// Eq compares two four-state values under logical equality:
// any X/Z operand makes the result unknown (X/Z propagation).
func Eq(a, b V) T {
	if !a.Known() || !b.Known() {
		return Unknown
	}
	return FromBool(a == b)
}

// CaseEq compares under case equality: X/Z match exactly, never unknown.
func CaseEq(a, b V) T { return FromBool(a == b) }

// Not is three-state negation.
func Not(t T) T { return -t }

// And is three-state conjunction (strong: false dominates).
func And(a, b T) T {
	if a == False || b == False {
		return False
	}
	if a == True && b == True {
		return True
	}
	return Unknown
}

func (v V) GoString() string { return fmt.Sprintf("logic4.%s", v.String()) }
