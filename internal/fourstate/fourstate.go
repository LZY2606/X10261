// Package fourstate implements four-valued logic (0/1/X/Z) with std_ulogic
// resolution and explicit X/Z propagation.
package fourstate

import "fmt"

// Value is a four-state (0/1/X/Z) logic value.
type Value uint8

const (
	Zero Value = iota
	One
	X // unknown / uninitialized
	Z // high-impedance
)

func (v Value) String() string {
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

// MarshalText renders the canonical one-character representation that is also
// the preserved on-disk form.
func (v Value) MarshalText() ([]byte, error) { return []byte(v.String()), nil }

// UnmarshalText accepts both VCD-style (x/X/z/Z) tokens.
func (v *Value) UnmarshalText(b []byte) error {
	parsed, err := Parse(string(b))
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// Parse parses a four-state token ("0","1","x","X","z","Z").
func Parse(s string) (Value, error) {
	switch s {
	case "0":
		return Zero, nil
	case "1":
		return One, nil
	case "x", "X":
		return X, nil
	case "z", "Z":
		return Z, nil
	default:
		return X, fmt.Errorf("fourstate: %q is not a 0/1/x/z value", s)
	}
}

// IsKnown reports whether v is a driven binary value.
func (v Value) IsKnown() bool { return v == Zero || v == One }

func (v Value) Bool() bool { return v == One }

// Resolve models std_ulogic multiple-source resolution:
// conflicting driven values (0 vs 1) and unknown drivers propagate X;
// Z is a weak driver that never overrides a driven value.
func Resolve(values ...Value) Value {
	seenZero, seenOne := false, false
	allZ := true
	for _, v := range values {
		if v == X {
			return X
		}
		if v == Zero {
			seenZero, allZ = true, false
		}
		if v == One {
			seenOne, allZ = true, false
		}
	}
	switch {
	case seenZero && seenOne:
		return X
	case seenOne:
		return One
	case seenZero:
		return Zero
	case allZ:
		return Z
	}
	return Z
}

// Not is four-state inversion: X and Z stay non-binary (X/Z are preserved).
func Not(a Value) Value {
	switch a {
	case Zero:
		return One
	case One:
		return Zero
	case X:
		return X
	default:
		return Z
	}
}

// And is four-state conjunction (truth-table semantics, Z distinct from X).
func And(a, b Value) Value {
	if a == Zero || b == Zero {
		return Zero
	}
	if a == X || b == X {
		return X
	}
	if a == Z || b == Z {
		return Z
	}
	return One
}

// Or is four-state disjunction.
func Or(a, b Value) Value {
	if a == One || b == One {
		return One
	}
	if a == X || b == X {
		return X
	}
	if a == Z || b == Z {
		return Z
	}
	return Zero
}

// ReduceBool reduces a four-state value to a boolean for predicate
// evaluation. Under binary semantics it is the value itself; X/Z yield ok=false
// so callers must model the non-binary result explicitly instead of guessing.
func ReduceBool(v Value) (result, ok bool) {
	switch v {
	case One:
		return true, true
	case Zero:
		return false, true
	default:
		return false, false
	}
}
