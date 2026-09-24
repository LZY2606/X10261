// Package fourval implements four-state (0/1/X/Z) bit and word logic with
// explicit unknown propagation. Every result is one of "0","1","x","z"
// (words are strings of those glyphs, MSB first, matching the raw VCD
// representation). Nothing here ever silently guesses a value.
package fourval

import "strings"

// Bit is a single four-state bit.
type Bit byte

const (
	Bit0 Bit = '0'
	Bit1 Bit = '1'
	BitX Bit = 'x'
	BitZ Bit = 'z'
)

// NormalizeBit canonicalizes a single glyph (VCD may emit X/Z in upper case).
// Anything outside the four-state alphabet returns x with ok=false so callers
// can attach a diagnostic instead of forcing a value.
func NormalizeBit(r rune) (Bit, bool) {
	switch r {
	case '0', '1':
		return Bit(r), true
	case 'x', 'X':
		return BitX, true
	case 'z', 'Z':
		return BitZ, true
	default:
		return BitX, false
	}
}

// IsKnown reports whether the bit is a definite 0 or 1.
func (b Bit) IsKnown() bool { return b == Bit0 || b == Bit1 }

// Not is four-state negation; x and z propagate (they do not collapse).
func (b Bit) Not() Bit {
	switch b {
	case Bit0:
		return Bit1
	case Bit1:
		return Bit0
	default:
		return b
	}
}

// And, Or and Xor implement four-state truth tables. Z is treated strictly
// like X for logical strength (no pull-up assumptions): an unknown input makes
// the result unknown whenever the known inputs do not already force it.
func And(a, b Bit) Bit { return bin(a, b, [2][2]Bit{{Bit0, Bit0}, {Bit0, Bit1}}) }
func Or(a, b Bit) Bit  { return bin(a, b, [2][2]Bit{{Bit0, Bit1}, {Bit1, Bit1}}) }
func Xor(a, b Bit) Bit { return bin(a, b, [2][2]Bit{{Bit0, Bit1}, {Bit1, Bit0}}) }

func bin(a, b Bit, known [2][2]Bit) Bit {
	ai, aok := asKnown(a)
	bi, bok := asKnown(b)
	switch {
	case aok && bok:
		return known[ai][bi]
	case !aok && !bok:
		return BitX
	case aok:
		// One unknown operand: if the known operand forces the gate
		// regardless of the other operand, propagate that; else unknown.
		if known[ai][0] == known[ai][1] {
			return known[ai][0]
		}
		return BitX
	default:
		if known[0][bi] == known[1][bi] {
			return known[0][bi]
		}
		return BitX
	}
}

func asKnown(b Bit) (int, bool) {
	switch b {
	case Bit0:
		return 0, true
	case Bit1:
		return 1, true
	default:
		return 0, false
	}
}

// Word is a fixed-width four-state word; the raw VCD spelling is preserved
// (no endianness reordering, no padding guesses).
type Word struct {
	Raw string // e.g. "01x" or "1" or "zz"; exactly as imported
}

func NewWord(raw string) (Word, bool) {
	for _, r := range raw {
		if _, ok := NormalizeBit(r); !ok {
			return Word{Raw: raw}, false
		}
	}
	return Word{Raw: strings.ToLower(raw)}, true
}

// Bit returns the glyph at position i (0 = first/leftmost raw character).
func (w Word) Bit(i int) Bit {
	if i < 0 || i >= len(w.Raw) {
		return BitX
	}
	b, _ := NormalizeBit(rune(w.Raw[i]))
	return b
}

// AllKnown reports that every glyph is 0 or 1.
func (w Word) AllKnown() bool {
	for _, r := range w.Raw {
		if b, _ := NormalizeBit(r); !b.IsKnown() {
			return false
		}
	}
	return true
}

// HasZ reports presence of high-impedance glyphs (distinct diagnostic from X).
func (w Word) HasZ() bool { return strings.ContainsAny(w.Raw, "zZ") }
func (w Word) HasX() bool { return strings.ContainsAny(w.Raw, "xX") }

// Tribool is the explicit three-valued result required by the judgement rules:
// true/false/unknown. Unknown carries an X/Z provenance reason.
type Tribool struct {
	Value  string // "true","false","unknown"
	Reason string // "", "x","z","z-polarity",...
}

func True() Tribool  { return Tribool{Value: "true"} }
func False() Tribool { return Tribool{Value: "false"} }
func Unknown(reason string) Tribool {
	return Tribool{Value: "unknown", Reason: reason}
}

// Bool converts a single bit to an explicit tri-state result.
func (b Bit) Bool() Tribool {
	switch b {
	case Bit0:
		return False()
	case Bit1:
		return True()
	case BitZ:
		return Unknown("z")
	default:
		return Unknown("x")
	}
}

// WordBool treats a scalar-width word as a boolean; non-scalar or unknown
// words yield unknown with the concrete reason.
func (w Word) Bool() Tribool {
	switch {
	case w.Raw == "":
		return Unknown("empty")
	case w.HasZ():
		return Unknown("z")
	case w.HasX():
		return Unknown("x")
	case len(w.Raw) == 1 && w.Raw[0] == '1':
		return True()
	case len(w.Raw) == 1 && w.Raw[0] == '0':
		return False()
	default:
		// Known multi-bit word: truth is non-zero, but the language only
		// declares scalar signals as boolean operands; report explicitly.
		return Unknown("non-scalar")
	}
}

// NotT, AndT etc. lift tri-state logic, keeping unknown sticky.
func NotT(t Tribool) Tribool {
	switch t.Value {
	case "true":
		return False()
	case "false":
		return True()
	default:
		return t
	}
}

func lift(a, b Tribool, f func(bool, bool) bool) Tribool {
	if a.Value == "unknown" {
		return a
	}
	if b.Value == "unknown" {
		return b
	}
	if f(a.Value == "true", b.Value == "true") {
		return True()
	}
	return False()
}

func AndT(a, b Tribool) Tribool { return lift(a, b, func(x, y bool) bool { return x && y }) }
func OrT(a, b Tribool) Tribool  { return lift(a, b, func(x, y bool) bool { return x || y }) }
