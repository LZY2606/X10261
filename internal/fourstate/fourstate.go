// Package fourstate implements SystemVerilog four-state (0/1/X/Z) values and
// operators. Raw representations are preserved at import time; only the
// derived interpretation layer produces these values.
package fourstate

import "fmt"

// Value is the four-state value.
type Value uint8

const (
	V0 Value = iota // false
	V1              // true
	VX              // unknown
	VZ              // high impedance
)

// FromVCDChar parses a single VCD scalar character (case-insensitive).
// Vector literals like 'b101x' are handled by the VCD package.
func FromVCDChar(c byte) (Value, bool) {
	switch c {
	case '0', 'l', 'L':
		return V0, true
	case '1', 'h', 'H':
		return V1, true
	case 'x', 'X':
		return VX, true
	case 'z', 'Z', '?':
		return VZ, true
	}
	return VX, false
}

// ParseVector converts a VCD binary vector literal such as "101x" or "01zz".
// Missing digits (leading zeros omitted, 'x'/'z' meaning all-x/all-z) use VCD
// semantics: a single x/z digit fills the whole width.
func ParseVector(lit string, width int) (Value, error) {
	if len(lit) == 0 {
		return VX, fmt.Errorf("empty vector literal")
	}
	if lit[0] == 'x' || lit[0] == 'X' {
		return VX, nil
	}
	if lit[0] == 'z' || lit[0] == 'Z' {
		return VZ, nil
	}
	v := V0
	for i := 0; i < len(lit); i++ {
		c, ok := FromVCDChar(lit[i])
		if !ok {
			return VX, fmt.Errorf("illegal vector digit %q", lit[i])
		}
		switch c {
		case V1:
			v = V1
		case VX:
			return VX, nil
		case VZ:
			return VZ, nil
		}
	}
	return v, nil
}

// String returns the canonical single-character representation.
func (v Value) String() string {
	switch v {
	case V0:
		return "0"
	case V1:
		return "1"
	case VX:
		return "x"
	case VZ:
		return "z"
	}
	return "?"
}

// IsUnknown reports whether the value is X or Z.
func IsUnknown(v Value) bool { return v == VX || v == VZ }

// ToBool collapses to the three-state Boolean domain: X and Z both read as
// unknown, preserving the distinction via IsUnknown callers. ok is false when
// the result is not a definite 0/1.
func ToBool(v Value) (b bool, ok bool) {
	switch v {
	case V0:
		return false, true
	case V1:
		return true, true
	}
	return false, false
}

// Not is four-state negation. Z is an unknowable input and propagates as X.
func Not(a Value) Value {
	switch a {
	case V0:
		return V1
	case V1:
		return V0
	}
	return VX
}

// And is four-state conjunction following SystemVerilog truth tables.
func And(a, b Value) Value {
	if a == V0 || b == V0 {
		return V0
	}
	if a == V1 && b == V1 {
		return V1
	}
	return VX
}

// Or is four-state disjunction following SystemVerilog truth tables.
func Or(a, b Value) Value {
	if a == V1 || b == V1 {
		return V1
	}
	if a == V0 && b == V0 {
		return V0
	}
	return VX
}

// Xor is four-state exclusive-or; any unknowable input propagates X.
func Xor(a, b Value) Value {
	if IsUnknown(a) || IsUnknown(b) {
		return VX
	}
	if a == b {
		return V0
	}
	return V1
}

// Mux is four-state selection. An unknown select propagates X even if both
// data inputs agree (matching simulation 4-state semantics).
func Mux(sel, a, b Value) Value {
	if IsUnknown(sel) {
		return VX
	}
	if sel == V1 {
		return a
	}
	return b
}
