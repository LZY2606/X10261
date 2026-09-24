// Package fourstate implements four-valued (0/1/X/Z) VCD logic and the
// three-valued verdict lattice used by sampled assertions.
package fourstate

import "fmt"

// Value is a four-state VCD scalar value.
type Value uint8

const (
	X Value = 0 // unknown
	Z Value = 1 // high impedance
	Low Value = 2 // strong 0
	High Value = 3 // strong 1
)

// Parse converts a VCD value character (0/1/x/X/z/Z) into a Value.
// Any other character yields X (with ok=false).
func Parse(c byte) (Value, bool) {
	switch c {
	case '0':
		return Low, true
	case '1':
		return High, true
	case 'x', 'X':
		return X, true
	case 'z', 'Z':
		return Z, true
	default:
		return X, false
	}
}

// String returns the canonical VCD representation.
func (v Value) String() string {
	switch v {
	case Low:
		return "0"
	case High:
		return "1"
	case Z:
		return "Z"
	default:
		return "X"
	}
}

// Bool maps a driven value to a definite boolean; X and Z are unknown.
func (v Value) Bool() (bool, bool) {
	switch v {
	case Low:
		return false, true
	case High:
		return true, true
	default:
		return false, false
	}
}

// Not is four-state negation. X and Z both propagate as X (negation has no
// meaningful high-impedance result).
func Not(a Value) Value {
	switch a {
	case Low:
		return High
	case High:
		return Low
	default:
		return X
	}
}

// And is four-state AND. Resolution rules (IEEE 1364 strength table, scalar):
//   - High AND High = High
//   - any Low = Low
//   - Z collapses to X unless the other operand is Low (Low dominates)
//   - X otherwise propagates
func And(a, b Value) Value {
	if a == Low || b == Low {
		return Low
	}
	if a == High && b == High {
		return High
	}
	return X
}

// Or is four-state OR: any High dominates; Low/Low is Low; otherwise X.
func Or(a, b Value) Value {
	if a == High || b == High {
		return High
	}
	if a == Low && b == Low {
		return Low
	}
	return X
}

// Mux resolves the value sampled on a signal when a VCD change record carries
// X or Z: both are retained (Z is not silently coerced), and only the derived
// boolean view treats Z as unknown.
func Mux(sel bool, whenTrue, whenFalse Value) Value {
	if sel {
		return whenTrue
	}
	return whenFalse
}

// Verdict is the three-state result every property/sequence evaluation must
// produce explicitly.
type Verdict uint8

const (
	VUnknown Verdict = 0 // still pending or undecidable (X propagation / open window)
	VFail    Verdict = 1 // no possible match
	VPass    Verdict = 2 // definite satisfied
)

// String renders a verdict.
func (v Verdict) String() string {
	switch v {
	case VFail:
		return "fail"
	case VPass:
		return "pass"
	default:
		return "unknown"
	}
}

// IsKnown reports whether the verdict is decided.
func (v Verdict) IsKnown() bool { return v != VUnknown }

// Check is a derived boolean expression over a named signal at one sample
// point; it returns a three-state verdict so X/Z never silently become 0.
func Check(signal Value, op string) (Verdict, error) {
	b, ok := signal.Bool()
	if !ok {
		return VUnknown, nil
	}
	switch op {
	case "==", "":
		if b {
			return VPass, nil
		}
		return VFail, nil
	case "!=":
		if !b {
			return VPass, nil
		}
		return VFail, nil
	default:
		return VUnknown, fmt.Errorf("fourstate: unsupported operator %q", op)
	}
}
