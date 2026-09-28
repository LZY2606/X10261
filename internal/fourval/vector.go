package fourval

import (
	"strings"
)

// Vec is an MSB-first four-state bit vector together with the original VCD
// token so the raw representation can always be displayed again.
type Vec struct {
	Bits []Value // index 0 is the leftmost token character (MSB side)
	Raw  string  // original token, e.g. "10xz" or "b10xz" without the b prefix
}

// ParseVec parses a VCD vector token (without the leading 'b'/'B').
// The raw token is retained verbatim. Characters other than 0/1/x/X/z/Z are
// rejected so callers can keep the value at its original location with a
// diagnostic instead of silently normalizing it.
func ParseVec(tok string) (Vec, error) {
	if tok == "" {
		return Vec{}, errEmpty
	}
	bits := make([]Value, len(tok))
	for i := 0; i < len(tok); i++ {
		v, err := Parse(tok[i])
		if err != nil {
			return Vec{Raw: tok}, err
		}
		bits[i] = v
	}
	return Vec{Bits: bits, Raw: strings.ToLower(tok)}, nil
}

// Scalar returns the vector as a scalar value. Width-1 vectors unwrap
// directly; wider vectors collapse only when all bits agree on a binary
// value, otherwise X. Z-only vectors return Z.
func (v Vec) Scalar() Value {
	if len(v.Bits) == 1 {
		return v.Bits[0]
	}
	saw0, saw1, sawZ := false, false, false
	for _, b := range v.Bits {
		switch b {
		case VX:
			return VX
		case V0:
			saw0 = true
		case V1:
			saw1 = true
		case VZ:
			sawZ = true
		}
	}
	switch {
	case saw0 && saw1:
		return VX
	case saw1 && !saw0:
		return V1
	case saw0 && !saw1:
		return V0
	case sawZ:
		return VZ
	}
	return VX
}

func (v Vec) String() string {
	if v.Raw != "" {
		return v.Raw
	}
	var b strings.Builder
	for _, bit := range v.Bits {
		b.WriteString(bit.String())
	}
	return b.String()
}

// HasXZ reports whether any bit is X or Z.
func (v Vec) HasXZ() bool {
	for _, b := range v.Bits {
		if b == VX || b == VZ {
			return true
		}
	}
	return false
}
