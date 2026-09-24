// Package fourval implements the four-state logic (0/1/X/Z) used when
// replaying sampled HDL waveforms and the three-state assertion verdict
// (pass/fail/unknown) together with disabled and vacuous distinctions.
package fourval

import "fmt"

// Val is a four-state logic value.
type Val uint8

const (
	V0 Val = iota // strong zero
	V1            // strong one
	VX            // unknown / uninitialized / contention
	VZ            // high impedance
)

// Parse parses a VCD-style scalar token. Both 'x'/'X' and 'z'/'Z' are
// accepted; 'b'/'B' (bus value) is handled by ParseBus.
func Parse(s string) (Val, error) {
	if len(s) == 0 {
		return VX, fmt.Errorf("empty four-state token")
	}
	switch s[0] {
	case '0':
		return V0, nil
	case '1':
		return V1, nil
	case 'x', 'X':
		return VX, nil
	case 'z', 'Z':
		return VZ, nil
	}
	return VX, fmt.Errorf("not a scalar four-state token: %q", s)
}

// ParseBus parses a binary VCD vector such as "10x", "z1" or "x".
// Whole-value X ("x" / "X" possibly padded with x) and Z are preserved;
// any mixed X/Z bit makes the whole bus X for logical purposes while the
// raw representation stays untouched at the storage layer.
func ParseBus(s string) (Val, error) {
	if len(s) == 0 {
		return VX, fmt.Errorf("empty bus token")
	}
	has0, has1, hasZ := false, false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '0':
			has0 = true
		case '1':
			has1 = true
		case 'x', 'X':
		case 'z', 'Z':
			hasZ = true
		default:
			return VX, fmt.Errorf("illegal four-state bit %q in %q", s[i], s)
		}
	}
	switch {
	case has0 && has1:
		return VX, fmt.Errorf("mixed 0/1 bus needs numeric decoding: %q", s)
	case has1:
		if hasZ {
			return VX, nil // partially driven: unknown
		}
		return V1, nil
	case has0:
		if hasZ {
			return VX, nil
		}
		return V0, nil
	case hasZ:
		return VZ, nil
	default:
		return VX, nil
	}
}

// String renders the canonical single-character form.
func (v Val) String() string {
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

// IsKnown reports whether the value is a definite 0 or 1.
func (v Val) IsKnown() bool { return v == V0 || v == V1 }

// Bool converts a known value; X and Z are not boolean and yield ok=false.
func (v Val) Bool() (bool, bool) {
	switch v {
	case V0:
		return false, true
	case V1:
		return true, true
	default:
		return false, false
	}
}

// Not is four-state negation: Z negates to X (no driven level), X stays X.
func Not(v Val) Val {
	switch v {
	case V0:
		return V1
	case V1:
		return V0
	default:
		return VX
	}
}

// And is four-state conjunction (IEEE 1800 Table 16-7 style strength-free
// abstraction): Z participates as X so that un-driven inputs produce X
// rather than being silently treated as 0.
func And(a, b Val) Val {
	switch {
	case a == V0 || b == V0:
		return V0
	case a == V1 && b == V1:
		return V1
	default:
		return VX
	}
}

// Or is four-state disjunction.
func Or(a, b Val) Val {
	switch {
	case a == V1 || b == V1:
		return V1
	case a == V0 && b == V0:
		return V0
	default:
		return VX
	}
}

// Xor is four-state exclusive or; any X/Z operand yields X.
func Xor(a, b Val) Val {
	if a.IsKnown() && b.IsKnown() {
		if a == b {
			return V0
		}
		return V1
	}
	return VX
}

// Verdict is the explicit three-state assertion result.
type Verdict uint8

const (
	VerdictUnknown  Verdict = iota // not (yet) decided: X/Z propagated, open thread
	VerdictPass                    // matched non-vacuously
	VerdictFail                    // no possible match
	VerdictVacuous                 // antecedent never matched (empty match)
	VerdictDisabled                // disable iff / reset window cancelled the attempt
)

// Tri maps the five fine-grained verdicts to the mandated three-state
// result {pass, fail, unknown} plus the orthogonal disabled marker.
func (v Verdict) Tri() string {
	switch v {
	case VerdictPass, VerdictVacuous:
		return "pass"
	case VerdictFail:
		return "fail"
	default:
		return "unknown"
	}
}

func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "pass"
	case VerdictFail:
		return "fail"
	case VerdictVacuous:
		return "vacuous"
	case VerdictDisabled:
		return "disabled"
	default:
		return "unknown"
	}
}

// ParseVerdict parses the canonical verdict spelling.
func ParseVerdict(s string) (Verdict, error) {
	switch s {
	case "pass":
		return VerdictPass, nil
	case "fail":
		return VerdictFail, nil
	case "vacuous":
		return VerdictVacuous, nil
	case "disabled":
		return VerdictDisabled, nil
	case "unknown":
		return VerdictUnknown, nil
	}
	return VerdictUnknown, fmt.Errorf("unknown verdict %q", s)
}

// Disabled reports whether a disable condition (known true) cancelled a
// result. X on the disable condition does NOT disable: it propagates to
// unknown instead.
func Disabled(disable Val) (bool, bool) {
	b, ok := disable.Bool()
	if !ok {
		return false, false // X/Z on disable iff -> unknown, not disabled
	}
	return b, true
}
