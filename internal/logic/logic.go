// Package logic implements the four-state signal algebra (0/1/X/Z) and the
// three-state verdict algebra (true/false/unknown) used by the replay engine.
//
// The four states follow IEEE 1164 style resolution semantics. Z ("high
// impedance") is preserved as a distinct source representation but, once read
// by Boolean sampling, collapses into the unknown verdict so that X and Z can
// never be silently treated as a definite 0 or 1.
package logic

import (
	"fmt"
	"strings"
)

// Bit is one of the four HDL signal states.
type Bit byte

const (
	B0 Bit = iota // driven 0
	B1            // driven 1
	BX            // unknown / conflict
	BZ            // high impedance (undriven)
)

// ParseBit parses a single raw VCD scalar token: "0","1","x"/"X","z"/"Z".
// ok is false for anything else (the caller keeps the raw representation and
// attaches a diagnostic instead of guessing).
func ParseBit(s string) (Bit, bool) {
	switch strings.TrimSpace(s) {
	case "0":
		return B0, true
	case "1":
		return B1, true
	case "x", "X":
		return BX, true
	case "z", "Z":
		return BZ, true
	}
	return BX, false
}

// String returns the canonical single-character representation.
func (b Bit) String() string {
	switch b {
	case B0:
		return "0"
	case B1:
		return "1"
	case BX:
		return "x"
	case BZ:
		return "z"
	}
	return "?"
}

// Resolve models a wired resolution: two drivers driving the same node.
//
//	0/1 conflict  -> X
//	Z + anything  -> anything (Z is the absence of a driver)
//	X + anything  -> X
func Resolve(a, b Bit) Bit {
	if a == BZ {
		return b
	}
	if b == BZ {
		return a
	}
	if a == BX || b == BX {
		return BX
	}
	if a != b {
		return BX
	}
	return a
}

// Trit is the three-state verdict algebra.
type Trit byte

const (
	TFalse   Trit = iota // definite false
	TTrue                // definite true
	TUnknown             // unknown (X or propagated Z)
)

func (t Trit) String() string {
	switch t {
	case TFalse:
		return "false"
	case TTrue:
		return "true"
	case TUnknown:
		return "unknown"
	}
	return "?"
}

// Sample reads a four-state signal through a Boolean sampling point.
// Z read by Boolean logic is unknown, never 0.
func Sample(b Bit) Trit {
	switch b {
	case B0:
		return TFalse
	case B1:
		return TTrue
	default:
		return TUnknown
	}
}

// Not is three-state negation: unknown stays unknown.
func Not(a Trit) Trit {
	switch a {
	case TTrue:
		return TFalse
	case TFalse:
		return TTrue
	}
	return TUnknown
}

// And / Or implement Kleene's strong three-valued logic. The result is only
// definite when a definite result is forced even if every unknown were
// instantiated to either 0 or 1.
func And(a, b Trit) Trit {
	if a == TFalse || b == TFalse {
		return TFalse
	}
	if a == TTrue && b == TTrue {
		return TTrue
	}
	return TUnknown
}

func Or(a, b Trit) Trit {
	if a == TTrue || b == TTrue {
		return TTrue
	}
	if a == TFalse && b == TFalse {
		return TFalse
	}
	return TUnknown
}

// Xor is definite only when both operands are definite.
func Xor(a, b Trit) Trit {
	if a == TUnknown || b == TUnknown {
		return TUnknown
	}
	if a == b {
		return TFalse
	}
	return TTrue
}

// Impl is Boolean implication a -> b = !a | b under strong three-state logic.
func Impl(a, b Trit) Trit { return Or(Not(a), b) }

// Vector is a fixed-width four-state bus value. The raw representation is
// kept separately by the importing layer; this is the decoded form.
type Vector struct {
	Bits []Bit // MSB first, width fixed at parse time
}

func NewVector(width int) Vector { return Vector{Bits: make([]Bit, width)} }

func (v Vector) Width() int { return len(v.Bits) }

// ParseVector decodes a raw VCD vector token such as "b101x" / "B10".
// The token is normalised MSB-first and zero/z-padded to width.
// When width <= 0 the token length is used. Unparseable characters yield ok
// false so the caller can retain the raw token with a diagnostic.
func ParseVector(raw string, width int) (Vector, bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "b")
	s = strings.TrimPrefix(s, "B")
	s = strings.TrimPrefix(s, "r")
	s = strings.TrimPrefix(s, "R")
	if s == "" {
		return Vector{}, false
	}
	for _, c := range s {
		if _, ok := ParseBit(string(c)); !ok {
			return Vector{}, false
		}
	}
	if width <= 0 {
		width = len(s)
	}
	v := NewVector(width)
	// Right-align: VCD emits b-value LSB-anchored; pad the MSB side.
	offset := width - len(s)
	for i := 0; i < width; i++ {
		j := i - offset
		if j < 0 {
			v.Bits[i] = BZ // unprovided MSBs are undriven
			continue
		}
		b, _ := ParseBit(string(s[j]))
		v.Bits[i] = b
	}
	return v, true
}

func (v Vector) String() string {
	var sb strings.Builder
	for _, b := range v.Bits {
		sb.WriteString(b.String())
	}
	return sb.String()
}

// IsConcrete reports whether every bit is 0 or 1.
func (v Vector) IsConcrete() bool {
	for _, b := range v.Bits {
		if b == BX || b == BZ {
			return false
		}
	}
	return true
}

// SampleBit samples one bit (MSB index 0) to a verdict.
func (v Vector) SampleBit(index int) (Trit, error) {
	if index < 0 || index >= len(v.Bits) {
		return TUnknown, fmt.Errorf("bit index %d out of width %d", index, len(v.Bits))
	}
	return Sample(v.Bits[index]), nil
}

// AllUnknown reports X or Z presence, per state, so the UI can show X/Z
// propagation distinctly even though verdicts only see three states.
func (v Vector) AllUnknown() bool {
	for _, b := range v.Bits {
		if b == BX || b == BZ {
			return true
		}
	}
	return false
}

// Value is a decoded signal value at one change point: either a scalar or a
// vector. Raw is always the exact source token.
type Value struct {
	Raw    string  `json:"raw"`
	Scalar bool    `json:"scalar"`
	Bit    Bit     `json:"bit,omitempty"`
	Vec    *Vector `json:"vec,omitempty"`
}

// Decode turns a raw value token into a Value, retaining the raw text even
// when decoding fails (ok=false attaches a diagnostic upstream).
func Decode(raw string, width int) (Value, bool) {
	s := strings.TrimSpace(raw)
	if len(s) == 1 || (width <= 1 && (s == "0" || s == "1" || s == "x" || s == "X" || s == "z" || s == "Z")) {
		if b, ok := ParseBit(s); ok {
			return Value{Raw: raw, Scalar: true, Bit: b}, true
		}
	}
	if len(s) >= 2 && (s[0] == 'b' || s[0] == 'B') {
		if v, ok := ParseVector(s, width); ok {
			vv := v
			return Value{Raw: raw, Scalar: false, Vec: &vv}, true
		}
	}
	return Value{Raw: raw}, false
}

// Verdict samples a decoded value's referenced bit.
func (val Value) Verdict(bit int) Trit {
	if val.Scalar {
		return Sample(val.Bit)
	}
	if val.Vec != nil {
		t, err := val.Vec.SampleBit(bit)
		if err != nil {
			return TUnknown
		}
		return t
	}
	return TUnknown
}

// HasZ / HasX let the presentation layer distinguish the two unknown sources.
func (val Value) HasZ() bool {
	if val.Scalar {
		return val.Bit == BZ
	}
	if val.Vec != nil {
		for _, b := range val.Vec.Bits {
			if b == BZ {
				return true
			}
		}
	}
	return false
}

func (val Value) HasX() bool {
	if val.Scalar {
		return val.Bit == BX
	}
	if val.Vec != nil {
		for _, b := range val.Vec.Bits {
			if b == BX {
				return true
			}
		}
	}
	return false
}
