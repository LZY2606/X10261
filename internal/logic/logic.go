// Package logic implements the four-state value algebra used by VCD
// import and property evaluation, plus the explicit three-valued
// verdict type (true / false / unknown) required by the derivation
// rules. Z is kept distinct from X at the signal level; inside boolean
// expressions Z is treated as X (IEEE high-impedance convention).
package logic

import "fmt"

// L is a four-state value: 0, 1, X, Z.
type L uint8

const (
	Zero L = iota
	One
	X
	Z
)

func Parse(b byte) (L, error) {
	switch b {
	case '0':
		return Zero, nil
	case '1':
		return One, nil
	case 'x', 'X':
		return X, nil
	case 'z', 'Z':
		return Z, nil
	}
	return X, fmt.Errorf("logic: %q is not a four-state scalar", string(b))
}

// String renders the canonical single character representation.
func (v L) String() string {
	switch v {
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

// Bool maps 1 -> true, 0 -> false, x/z -> unknown.
func (v L) Bool() (val, known bool) {
	switch v {
	case One:
		return true, true
	case Zero:
		return false, true
	default:
		return false, false
	}
}

// IsContended reports whether the value is X or Z.
func (v L) IsContended() bool { return v == X || v == Z }

// andTable / orTable are the IEEE 1364 four-state resolution tables in
// the 0/1/X boolean lattice (Z coerced to X).
var andTable = [4][4]L{
	{Zero, Zero, Zero, Zero},
	{Zero, One, X, X},
	{Zero, X, X, X},
	{Zero, X, X, X},
}
var orTable = [4][4]L{
	{Zero, One, X, X},
	{One, One, One, One},
	{X, One, X, X},
	{X, One, X, X},
}

// And is four-state AND (Z coerced to X in the boolean lattice).
func And(a, b L) L { return andTable[a][b] }

// Or is four-state OR.
func Or(a, b L) L { return orTable[a][b] }

// Not is four-state NOT.
func Not(a L) L {
	switch a {
	case One:
		return Zero
	case Zero:
		return One
	default:
		return X
	}
}

// Eq is four-state equality: unknown if either operand is contended.
func Eq(a, b L) L {
	if a.IsContended() || b.IsContended() {
		return X
	}
	if a == b {
		return One
	}
	return Zero
}

// Tri is the explicit three-state verdict used by every derivation
// rule: X/Z propagation, implication, disable iff and empty matches
// never collapse unknown into a binary pass/fail.
type Tri uint8

const (
	TFalse Tri = iota
	TTrue
	TUnknown
)

func (t Tri) String() string {
	switch t {
	case TTrue:
		return "true"
	case TFalse:
		return "false"
	default:
		return "unknown"
	}
}

// Known is true for true/false verdicts.
func (t Tri) Known() bool { return t != TUnknown }

// TAnd / TOr implement the three-state lattice.
func TAnd(a, b Tri) Tri {
	if a == TFalse || b == TFalse {
		return TFalse
	}
	if a == TUnknown || b == TUnknown {
		return TUnknown
	}
	return TTrue
}

func TOr(a, b Tri) Tri {
	if a == TTrue || b == TTrue {
		return TTrue
	}
	if a == TUnknown || b == TUnknown {
		return TUnknown
	}
	return TFalse
}

func TNot(a Tri) Tri {
	switch a {
	case TTrue:
		return TFalse
	case TFalse:
		return TTrue
	default:
		return TUnknown
	}
}

// FromL lifts a four-state value to a verdict.
func FromL(v L) Tri {
	b, known := v.Bool()
	if !known {
		return TUnknown
	}
	if b {
		return TTrue
	}
	return TFalse
}
