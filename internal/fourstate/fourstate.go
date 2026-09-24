// Package fourstate implements 0/1/X/Z logic values and three-valued results.
package fourstate

import "strings"

// Bit is a single four-state logic bit.
type Bit byte

const (
	Zero      Bit = '0'
	One       Bit = '1'
	Unknown   Bit = 'x'
	HighImped Bit = 'z'
)

// Tri is an explicit three-valued result.
type Tri int

const (
	False    Tri = 0
	True     Tri = 1
	UnknownT Tri = 2
)

func (t Tri) String() string {
	switch t {
	case True:
		return "TRUE"
	case False:
		return "FALSE"
	default:
		return "UNKNOWN"
	}
}

// Normalize lowercases and validates a four-state value string (e.g. "10xz").
func Normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Valid reports whether s consists only of 0/1/x/z (optionally b-prefixed).
func Valid(s string) bool {
	s = Normalize(s)
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '0', '1', 'x', 'z':
		default:
			return false
		}
	}
	return true
}

func andBit(a, b Bit) Bit {
	if a == Zero || b == Zero {
		return Zero
	}
	if a == One && b == One {
		return One
	}
	return Unknown
}

func orBit(a, b Bit) Bit {
	if a == One || b == One {
		return One
	}
	if a == Zero && b == Zero {
		return Zero
	}
	return Unknown
}

func notBit(a Bit) Bit {
	switch a {
	case Zero:
		return One
	case One:
		return Zero
	default:
		return Unknown
	}
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	fill := byte('0')
	if len(s) > 0 && (s[0] == 'x' || s[0] == 'z') {
		fill = s[0]
	}
	return strings.Repeat(string(fill), n-len(s)) + s
}

func align(a, b string) (string, string) {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	return pad(Normalize(a), n), pad(Normalize(b), n)
}

// And computes bitwise four-state AND.
func And(a, b string) string {
	a, b = align(a, b)
	out := make([]byte, len(a))
	for i := range a {
		out[i] = byte(andBit(Bit(a[i]), Bit(b[i])))
	}
	return string(out)
}

// Or computes bitwise four-state OR.
func Or(a, b string) string {
	a, b = align(a, b)
	out := make([]byte, len(a))
	for i := range a {
		out[i] = byte(orBit(Bit(a[i]), Bit(b[i])))
	}
	return string(out)
}

// Not computes bitwise four-state NOT.
func Not(a string) string {
	a = Normalize(a)
	out := make([]byte, len(a))
	for i := range a {
		out[i] = byte(notBit(Bit(a[i])))
	}
	return string(out)
}

// ToTri reduces a four-state vector to a three-valued truth value:
// any 1 bit -> TRUE (unless other bits unknown? no: OR-reduction semantics
// for booleans: value is true iff != 0; unknown iff any x/z and no 1).
func ToTri(s string) Tri {
	s = Normalize(s)
	hasOne, hasX := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '1':
			hasOne = true
		case 'x', 'z':
			hasX = true
		}
	}
	if hasOne {
		return True
	}
	if hasX {
		return UnknownT
	}
	return False
}

// Eq compares two four-state vectors; result is UNKNOWN if any x/z bit present.
func Eq(a, b string) Tri {
	a, b = align(a, b)
	for i := 0; i < len(a); i++ {
		if a[i] == 'x' || a[i] == 'z' || b[i] == 'x' || b[i] == 'z' {
			return UnknownT
		}
		if a[i] != b[i] {
			return False
		}
	}
	return True
}

// Uint interprets a four-state string as unsigned integer; ok=false if x/z present.
func Uint(s string) (uint64, bool) {
	s = Normalize(s)
	var v uint64
	for i := 0; i < len(s); i++ {
		v <<= 1
		switch s[i] {
		case '1':
			v |= 1
		case '0':
		default:
			return 0, false
		}
	}
	return v, true
}

// Cmp compares two vectors as unsigned integers; UNKNOWN if either has x/z.
func Cmp(a, b string) (int, Tri) {
	a, b = align(a, b)
	av, aok := Uint(a)
	bv, bok := Uint(b)
	if !aok || !bok {
		return 0, UnknownT
	}
	switch {
	case av < bv:
		return -1, True
	case av > bv:
		return 1, True
	default:
		return 0, True
	}
}
