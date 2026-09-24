package logic4

import "testing"

func TestTriboolAlgebra(t *testing.T) {
	// X/Z propagation must have explicit three-valued results
	if And(True, Unknown) != Unknown || And(False, Unknown) != False {
		t.Fatal("and")
	}
	if Or(False, Unknown) != Unknown || Or(True, Unknown) != True {
		t.Fatal("or")
	}
	if Not(Unknown) != Unknown {
		t.Fatal("not")
	}
	if Implies(True, Unknown) != Unknown || Implies(False, Unknown) != True {
		t.Fatal("implies")
	}
	if VX.Truth() != Unknown || VZ.Truth() != Unknown || V0.Truth() != False || V1.Truth() != True {
		t.Fatal("truth mapping")
	}
	if !VX.IsUnknown() || !VZ.IsUnknown() || V0.IsUnknown() {
		t.Fatal("isunknown")
	}
}

func TestParse(t *testing.T) {
	for _, c := range []byte{'0', '1', 'x', 'X', 'z', 'Z'} {
		if _, ok := Parse(c); !ok {
			t.Fatalf("Parse(%q) failed", c)
		}
	}
	if _, ok := Parse('u'); ok {
		t.Fatal("Parse('u') must fail so raw token is kept")
	}
}
