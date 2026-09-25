package fourstate

import "testing"

func TestGatePropagation(t *testing.T) {
	if And(BitZ, Bit1) != BitX {
		t.Fatal("z & 1 must be x")
	}
	if Or(BitZ, Bit0) != BitX {
		t.Fatal("z | 0 must be x")
	}
	if Not(BitZ) != BitX {
		t.Fatal("!z must be x")
	}
	if And(Bit0, BitX) != Bit0 {
		t.Fatal("0 & x must be 0 (deterministic side)")
	}
	if Xor(Bit1, BitZ) != BitX {
		t.Fatal("xor with z must be x")
	}
}

func TestTriState(t *testing.T) {
	for _, c := range []struct {
		b     Bit
		want  TriBool
		fromZ bool
	}{
		{Bit0, TriFalse, false},
		{Bit1, TriTrue, false},
		{BitX, TriUnknown, false},
		{BitZ, TriUnknown, true},
	} {
		got, z := BoolTest(c.b)
		if got != c.want || z != c.fromZ {
			t.Fatalf("BoolTest(%v)=%v,z=%v want %v,z=%v", c.b, got, z, c.want, c.fromZ)
		}
	}
}

func TestVectorPreserve(t *testing.T) {
	v, err := ParseVector("b10z", 3)
	if err != nil {
		t.Fatal(err)
	}
	if v.Render() != "b10z" || v.Scalar() != BitZ {
		t.Fatalf("render=%q scalar=%v", v.Render(), v.Scalar())
	}
	bad, err := ParseVector("b12", 2)
	if err == nil {
		t.Fatal("expected diagnostic for non-quaternary digit")
	}
	if bad.Raw != "b12" || bad.IsWellFormed() {
		t.Fatal("malformed raw token must be retained in place")
	}
	if bad.Scalar() != BitX {
		t.Fatal("malformed vector must degrade to X, never 0")
	}
	narrow, err := ParseVector("b10", 4)
	if err == nil || narrow.IsWellFormed() {
		t.Fatal("width mismatch must be diagnostic, token retained")
	}
	if narrow.Equals(Vector{Bits: []Bit{Bit1, Bit0, BitX, BitZ}}) != TriFalse {
		t.Fatal("expected false equality for differing width")
	}
}
