// Package fourval implements the VHDL/Verilog four-state value set
// {0, 1, X, Z} and the two published semantics versions used by the bundled
// case: v1.0.0 (strict: no four-state short circuit) and v1.1.0
// (Verilog-style short circuit). Z is its own value and never silently
// collapsed to X; values that cannot be converted stay put with a diagnostic.
package fourval

import "fmt"

// Value is a four-state logic value.
type Value byte

const (
	X Value = iota // unknown / uninitialized
	Z              // high impedance
	V0
	V1
)

// Parse parses a VCD-style token: "0","1","x"/"X","z"/"Z" plus VCD
// strength-encoded vectors such as "x10z". Single-bit tokens are required for
// scalar logic; ParseVec handles the vector form.
func Parse(s string) (Value, error) {
	switch s {
	case "0":
		return V0, nil
	case "1":
		return V1, nil
	case "x", "X":
		return X, nil
	case "z", "Z":
		return Z, nil
	default:
		if len(s) == 1 {
			return X, fmt.Errorf("fourval: %q is not 0/1/x/z", s)
		}
		return X, fmt.Errorf("fourval: scalar %q has %d digits", s, len(s))
	}
}

func (v Value) String() string {
	switch v {
	case X:
		return "x"
	case Z:
		return "z"
	case V0:
		return "0"
	case V1:
		return "1"
	}
	return "?"
}

// Known reports whether v is a resolved binary value.
func (v Value) Known() bool { return v == V0 || v == V1 }

// Not implements logical inversion in four-state logic.
//
//	not 0 = 1, not 1 = 0, not X = X, not Z = X
func Not(a Value) Value {
	switch a {
	case V0:
		return V1
	case V1:
		return V0
	case Z:
		return X
	default:
		return X
	}
}

// And is the strict four-state AND used by semantics v1.0.0: every operand is
// evaluated; any X poisons the result, Z is permitted only when the remaining
// operands force the result to 0 (z is not 1).
func And(vals ...Value) Value { return combine(false, false, vals...) }

// Or is the strict four-state OR used by semantics v1.0.0.
func Or(vals ...Value) Value { return combine(true, false, vals...) }

// AndShortCircuit is the Verilog-style AND used by v1.1.0: a resolved 0
// operand forces 0 even when another operand is X.
func AndShortCircuit(vals ...Value) Value { return combine(false, true, vals...) }

// OrShortCircuit is the Verilog-style OR used by v1.1.0: a resolved 1
// operand forces 1 even when another operand is X.
func OrShortCircuit(vals ...Value) Value { return combine(true, true, vals...) }

func combine(isOr, shortCircuit bool, vals ...Value) Value {
	if len(vals) == 0 {
		return X
	}
	var sawX, sawZ bool
	for _, v := range vals {
		if shortCircuit {
			if isOr && v == V1 {
				return V1
			}
			if !isOr && v == V0 {
				return V0
			}
		}
		switch v {
		case X:
			sawX = true
		case Z:
			sawZ = true
		}
	}
	// Short-circuit forcing values have already returned above.
	if sawX {
		return X
	}
	if sawZ {
		return Z
	}
	if isOr {
		return V0
	}
	return V1
}
