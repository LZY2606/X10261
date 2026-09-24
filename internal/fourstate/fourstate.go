// Package fourstate implements Verilog-style four-state logic (0, 1, X, Z)
// with explicit three-valued truth results used by the assertion engine.
package fourstate

import "fmt"

// Value is a single four-state logic bit.
type Value uint8

const (
	Zero Value = iota
	One
	X
	Z
)

// Truth is the explicit three-valued result required by the derivation
// rules: every X/Z propagation, implication, disable-iff and empty-match
// judgement must resolve to one of these.
type Truth int8

const (
	False   Truth = -1
	Unknown Truth = 0
	True    Truth = 1
)

func (t Truth) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	default:
		return "unknown"
	}
}

// ParseBit converts a VCD value character to a Value.
func ParseBit(c byte) (Value, error) {
	switch c {
	case '0':
		return Zero, nil
	case '1':
		return One, nil
	case 'x', 'X':
		return X, nil
	case 'z', 'Z':
		return Z, nil
	}
	return Zero, fmt.Errorf("invalid four-state bit %q", c)
}

func (v Value) Char() byte {
	switch v {
	case Zero:
		return '0'
	case One:
		return '1'
	case X:
		return 'x'
	case Z:
		return 'z'
	}
	return '?'
}

func (v Value) String() string { return string(v.Char()) }

// ToTruth maps a four-state value onto the explicit three-valued result.
// Z is treated as unknown for truth purposes but is preserved as Z in the
// stored waveform so provenance never loses the original representation.
func (v Value) ToTruth() Truth {
	switch v {
	case One:
		return True
	case Zero:
		return False
	default:
		return Unknown
	}
}

// Not, And, Or implement four-state truth tables (X and Z both behave as
// unknown operands, but the result keeps X/Z distinguishable where the
// standard distinguishes them; here Z is dominated like X).
func Not(a Value) Value {
	switch a {
	case Zero:
		return One
	case One:
		return Zero
	case X:
		return X
	case Z:
		return Z
	}
	return X
}

func And(a, b Value) Value {
	if a == Zero || b == Zero {
		return Zero
	}
	if a == One {
		return b
	}
	if b == One {
		return a
	}
	if a == Z && b == Z {
		return Z
	}
	return X
}

func Or(a, b Value) Value {
	if a == One || b == One {
		return One
	}
	if a == Zero {
		return b
	}
	if b == Zero {
		return a
	}
	if a == Z && b == Z {
		return Z
	}
	return X
}

// Vector is a multi-bit four-state value, LSB-last in string form
// (String renders MSB first, like VCD "b101x").
type Vector struct {
	Bits []Value // Bits[0] is MSB
}

func ParseVector(s string) (Vector, error) {
	v := Vector{Bits: make([]Value, len(s))}
	for i := 0; i < len(s); i++ {
		b, err := ParseBit(s[i])
		if err != nil {
			return Vector{}, err
		}
		v.Bits[i] = b
	}
	return v, nil
}

func (v Vector) String() string {
	out := make([]byte, len(v.Bits))
	for i, b := range v.Bits {
		out[i] = b.Char()
	}
	return string(out)
}

// Scalar collapses a vector to a single truth value: 1 iff all bits are 1,
// 0 iff any bit is 0... following reduction-to-boolean semantics used at
// sampling time: a vector is "true" iff it is non-zero and contains no
// X/Z; "false" iff all bits are 0; otherwise unknown.
func (v Vector) ToTruth() Truth {
	allZero := true
	for _, b := range v.Bits {
		switch b {
		case One:
			allZero = false
		case Zero:
		default:
			return Unknown
		}
	}
	if allZero {
		return False
	}
	return True
}
