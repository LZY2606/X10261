// Package fourval implements four-state logic values (0/1/X/Z) and the
// three-state truth algebra used when evaluating assertions.
//
// Rule: X/Z propagation, overlapped/non-overlapped implication, disable iff
// and empty match must all produce an explicit three-state result (T/F/X).
package fourval

// V is a four-state logic value.
type V byte

const (
	Zero V = '0'
	One  V = '1'
	X    V = 'x'
	Z    V = 'z'
)

// Parse converts a VCD value character to a four-state value.
func Parse(c byte) (V, bool) {
	switch c {
	case '0':
		return Zero, true
	case '1':
		return One, true
	case 'x', 'X':
		return X, true
	case 'z', 'Z':
		return Z, true
	}
	return 0, false
}

func (v V) String() string { return string(rune(v)) }

// T is a three-state truth value: the explicit result of evaluating an
// expression over four-state signals.
type T int

const (
	False T = iota
	True
	Unknown
)

func (t T) String() string {
	switch t {
	case True:
		return "T"
	case False:
		return "F"
	default:
		return "X"
	}
}

// Bit lifts a four-state value to a three-state truth value (X/Z -> Unknown).
func Bit(v V) T {
	switch v {
	case One:
		return True
	case Zero:
		return False
	default:
		return Unknown
	}
}

func Not(a T) T {
	switch a {
	case True:
		return False
	case False:
		return True
	default:
		return Unknown
	}
}

func And(a, b T) T {
	if a == False || b == False {
		return False
	}
	if a == True && b == True {
		return True
	}
	return Unknown
}

func Or(a, b T) T {
	if a == True || b == True {
		return True
	}
	if a == False && b == False {
		return False
	}
	return Unknown
}

// Eq is logical equality (==): any X/Z operand makes the result Unknown.
func Eq(a, b V) T {
	if (a == Zero || a == One) && (b == Zero || b == One) {
		if a == b {
			return True
		}
		return False
	}
	return Unknown
}
