// Package fourval implements four-state HDL logic (0/1/X/Z).
//
// Values are kept in their raw width: a scalar is one rune ('0','1','x','z'),
// a vector is a rune slice in MSB->LSB order. Raw representations coming from
// the waveform are never collapsed at import time; normalization (e.g. reducing
// a vector to a boolean for a property expression) happens only in the
// derived/replay layer.
package fourval

import (
	"errors"
	"strings"
)

// V is a single four-state bit.
type V = rune

const (
	V0 V = '0'
	V1 V = '1'
	VX V = 'x'
	VZ V = 'z'
)

// ErrInvalidBit is returned when a raw token cannot be represented in 4-state.
var ErrInvalidBit = errors.New("invalid four-state value")

// NormalizeBit canonicalizes a raw bit character (uppercase X/Z accepted).
func NormalizeBit(r rune) (V, bool) {
	switch r {
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

// Value is a four-state vector of fixed width (raw waveform representation).
type Value struct {
	Bits []V // MSB first
}

// Scalar returns a width-1 value.
func Scalar(v V) Value { return Value{Bits: []V{v}} }

// ParseScalar parses a single VCD scalar token such as "0", "1", "x", "z".
// Tokens may include the strength prefix letters (e.g. "X0", "D1") used in
// some VCD writers; the value character is the final rune.
func ParseScalar(token string) (Value, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Value{}, false
	}
	r, ok := NormalizeBit(rune(token[len(token)-1]))
	if !ok {
		return Value{}, false
	}
	return Scalar(r), true
}

// ParseVector parses a raw VCD vector token ("101", "1x0z"). Every digit must
// be a four-state character; otherwise ok is false and the raw token should be
// retained at its origin with a diagnostic instead of being dropped.
func ParseVector(token string) (Value, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Value{}, false
	}
	bits := make([]V, 0, len(token))
	for _, r := range token {
		b, ok := NormalizeBit(r)
		if !ok {
			return Value{}, false
		}
		bits = append(bits, b)
	}
	return Value{Bits: bits}, true
}

// String returns the raw textual representation (e.g. "1x0z").
func (v Value) String() string {
	if len(v.Bits) == 0 {
		return ""
	}
	var b strings.Builder
	for _, bit := range v.Bits {
		b.WriteRune(bit)
	}
	return b.String()
}

// Width is the number of bits.
func (v Value) Width() int { return len(v.Bits) }

// IsScalar reports whether the value is exactly one bit wide.
func (v Value) IsScalar() bool { return len(v.Bits) == 1 }

// Equal compares raw representations (same width and bits).
func (v Value) Equal(o Value) bool {
	if len(v.Bits) != len(o.Bits) {
		return false
	}
	for i := range v.Bits {
		if v.Bits[i] != o.Bits[i] {
			return false
		}
	}
	return true
}

// Reduce reduces a vector to a scalar using standard 4-state reduction:
//
//	all bits identical -> that bit
//	mix of 0/1 (no X, no Z) -> X
//	any X -> X
//	any Z (and no X) -> Z
//
// This is the only place a wide raw value is collapsed, and it belongs to the
// derived evaluation layer, never to import.
func (v Value) Reduce() Value {
	if len(v.Bits) == 0 {
		return Scalar(VX)
	}
	first := v.Bits[0]
	allSame := true
	hasX, hasZ := false, false
	for _, b := range v.Bits {
		if b == VX {
			hasX = true
		}
		if b == VZ {
			hasZ = true
		}
		if b != first {
			allSame = false
		}
	}
	if allSame {
		return Scalar(first)
	}
	if hasX {
		return Scalar(VX)
	}
	if hasZ {
		return Scalar(VX)
	}
	return Scalar(VX)
}

// Truth is the scalar truthiness used when an expression needs a boolean.
// X and Z are both "unknown" (not 1).
func (v Value) Truth() bool {
	r := v.Reduce()
	return len(r.Bits) == 1 && r.Bits[0] == V1
}

// Not is four-state logical inversion.
//   - 0 <-> 1
//   - X -> X
//   - Z -> Z (high impedance is preserved by the raw v1 semantics;
//     semantics v1.1.0 collapses Z to X here, see semantics.Not)
func Not(v Value) Value {
	r := v.Reduce()
	b := r.Bits[0]
	switch b {
	case V0:
		return Scalar(V1)
	case V1:
		return Scalar(V0)
	case VZ:
		return Scalar(VZ)
	default:
		return Scalar(VX)
	}
}

// And is four-state logical conjunction with a controlling value:
// a definite 0 forces 0; otherwise X/Z contamination propagates, Z only when
// there is no X (X dominates Z).
func And(a, b Value) Value {
	x, y := a.Reduce().Bits[0], b.Reduce().Bits[0]
	switch {
	case x == V0 || y == V0:
		return Scalar(V0)
	case x == V1 && y == V1:
		return Scalar(V1)
	case x == VX || y == VX:
		return Scalar(VX)
	default:
		return Scalar(VZ)
	}
}

// Or is four-state logical disjunction: a definite 1 forces 1.
func Or(a, b Value) Value {
	x, y := a.Reduce().Bits[0], b.Reduce().Bits[0]
	switch {
	case x == V1 || y == V1:
		return Scalar(V1)
	case x == V0 && y == V0:
		return Scalar(V0)
	case x == VX || y == VX:
		return Scalar(VX)
	default:
		return Scalar(VZ)
	}
}

// Eq is four-state equality: unknown if either operand contains X/Z.
func Eq(a, b Value) Value {
	x, y := a.Reduce().Bits[0], b.Reduce().Bits[0]
	if x == VX || x == VZ || y == VX || y == VZ {
		return Scalar(VX)
	}
	if x == y {
		return Scalar(V1)
	}
	return Scalar(V0)
}
