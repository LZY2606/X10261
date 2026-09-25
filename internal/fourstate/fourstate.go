// Package fourstate implements Verilog four-state (0,1,X,Z) scalar and vector
// logic with explicit propagation rules. Z propagates through bit operations
// as X (high impedance is not a data value), while X propagates wherever the
// result is unknown. IsKnownX reports whether an X arose from an actual X or
// from Z collapsing through logic, so the replay record can distinguish the two.
package fourstate

import (
	"fmt"
	"strings"
)

// Bit is a single four-state bit.
type Bit uint8

const (
	Bit0 Bit = iota // false
	Bit1            // true
	BitX            // unknown
	BitZ            // high impedance
)

// ParseBit parses a single VCD bit character: 0 1 x X z Z.
func ParseBit(r byte) (Bit, error) {
	switch r {
	case '0':
		return Bit0, nil
	case '1':
		return Bit1, nil
	case 'x', 'X':
		return BitX, nil
	case 'z', 'Z':
		return BitZ, nil
	default:
		return BitX, fmt.Errorf("fourstate: invalid bit %q", r)
	}
}

// Char renders the canonical single-character representation.
func (b Bit) Char() byte {
	switch b {
	case Bit0:
		return '0'
	case Bit1:
		return '1'
	case BitX:
		return 'x'
	default:
		return 'z'
	}
}

func (b Bit) String() string { return string([]byte{b.Char()}) }

// Not is logical inversion. Z reads as X: !(z) = x.
func Not(a Bit) Bit {
	switch a {
	case Bit0:
		return Bit1
	case Bit1:
		return Bit0
	default:
		return BitX
	}
}

// And, Or, Xor collapse Z to X like Verilog gate outputs.
func And(a, b Bit) Bit {
	if a == Bit0 || b == Bit0 {
		return Bit0
	}
	if a == Bit1 && b == Bit1 {
		return Bit1
	}
	return BitX
}

func Or(a, b Bit) Bit {
	if a == Bit1 || b == Bit1 {
		return Bit1
	}
	if a == Bit0 && b == Bit0 {
		return Bit0
	}
	return BitX
}

func Xor(a, b Bit) Bit {
	if a == BitX || b == BitX || a == BitZ || b == BitZ {
		return BitX
	}
	if a == b {
		return Bit0
	}
	return Bit1
}

// TriBool is the explicit three-state verdict required by the derivation
// rules: True, False or Unknown. X and Z inputs never silently collapse to
// false; they yield Unknown together with a machine-readable reason.
type TriBool uint8

const (
	TriUnknown TriBool = iota
	TriTrue
	TriFalse
)

func (t TriBool) String() string {
	switch t {
	case TriTrue:
		return "true"
	case TriFalse:
		return "false"
	default:
		return "unknown"
	}
}

// BoolTest converts a sampled bit under the four-state rule: 0 -> false,
// 1 -> true, x/z -> unknown. Z is reported through zSeen rather than hidden.
func BoolTest(b Bit) (TriBool, bool) {
	switch b {
	case Bit0:
		return TriFalse, false
	case Bit1:
		return TriTrue, false
	case BitZ:
		return TriUnknown, true
	default:
		return TriUnknown, false
	}
}

// LogicEq is four-state equality of two bits (casez is *not* used here):
// any X/Z operand makes the result Unknown.
func LogicEq(a, b Bit) TriBool {
	if a == BitX || b == BitX || a == BitZ || b == BitZ {
		return TriUnknown
	}
	if a == b {
		return TriTrue
	}
	return TriFalse
}

// Vector is an MSB-first vector of four-state bits, carrying its raw VCD
// token alongside so the importer's original representation is preserved on
// the waveform record even when normalization or comparison is impossible.
type Vector struct {
	Bits []Bit
	// Width is the declared signal width; len(Bits) < Width marks a malformed
	// raw token that was kept in place with a diagnostic instead of dropped.
	Width int
	Raw   string
	// Malformed marks a token that was kept in place with a diagnostic
	// (bad digit or width mismatch) instead of being silently dropped.
	Malformed bool
}

// ParseVector parses a VCD value token such as "b10xz" or a single scalar.
// width<=0 means inferred. Unparseable tokens return an error vector whose
// Bits are all X with the raw token retained; callers keep the raw value.
func ParseVector(tok string, width int) (Vector, error) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return Vector{Raw: tok, Width: width}, fmt.Errorf("fourstate: empty value token")
	}
	if !(tok[0] == 'b' || tok[0] == 'B') {
		b, err := ParseBit(tok[0])
		if err != nil {
			return Vector{Raw: tok, Width: width}, err
		}
		w := width
		if w <= 0 {
			w = 1
		}
		return Vector{Bits: []Bit{b}, Width: w, Raw: tok}, nil
	}
	digits := tok[1:]
	if width <= 0 {
		width = len(digits)
	}
	bits := make([]Bit, 0, len(digits))
	var bad byte
	for i := 0; i < len(digits); i++ {
		b, err := ParseBit(digits[i])
		if err != nil {
			b = BitX
			bad = digits[i]
		}
		bits = append(bits, b)
	}
	v := Vector{Bits: bits, Width: width, Raw: tok}
	if len(digits) != width {
		v.Malformed = true
		return v, fmt.Errorf("fourstate: token %q width %d != declared width %d", tok, len(digits), width)
	}
	if bad != 0 {
		v.Malformed = true
		return v, fmt.Errorf("fourstate: token %q has non-quaternary digit %q", tok, string(bad))
	}
	return v, nil
}

// IsWellFormed reports whether the token matched the declared width exactly.
func (v Vector) IsWellFormed() bool {
	return !v.Malformed && len(v.Bits) == v.Width && v.Width > 0
}

// Scalar returns the effective scalar bit. Malformed vectors degrade to X
// (never to 0), and Z stays Z so sampling rules can report it explicitly.
func (v Vector) Scalar() Bit {
	if len(v.Bits) == 0 {
		return BitX
	}
	return v.Bits[len(v.Bits)-1]
}

// Equals is four-state vector equality (Unknown if any bit is X/Z).
func (v Vector) Equals(o Vector) TriBool {
	if len(v.Bits) != len(o.Bits) {
		return TriFalse
	}
	for i := range v.Bits {
		if eq := LogicEq(v.Bits[i], o.Bits[i]); eq != TriTrue {
			if eq == TriUnknown {
				return TriUnknown
			}
			return TriFalse
		}
	}
	return TriTrue
}

// Render returns the canonical bxxxx form for well-formed vectors or the
// stored raw token for malformed ones (original representation preserved).
func (v Vector) Render() string {
	if !v.IsWellFormed() {
		if v.Raw != "" {
			return v.Raw
		}
		return "<malformed>"
	}
	if len(v.Bits) == 1 {
		return string(v.Bits[0].Char())
	}
	var sb strings.Builder
	sb.WriteByte('b')
	for _, b := range v.Bits {
		sb.WriteByte(b.Char())
	}
	return sb.String()
}
