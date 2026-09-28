package fourstate

import "testing"

func TestParseAndString(t *testing.T) {
	cases := map[string]Value{"0": V0, "1": V1, "x": VX, "X": VX, "z": VZ, "Z": VZ}
	for tok, want := range cases {
		got, err := Parse(tok)
		if err != nil || got != want || got.String() != want.String() {
			t.Fatalf("Parse(%q)=%v,%v want %v", tok, got, err, want)
		}
	}
	if _, err := Parse("w"); err == nil {
		t.Fatal("expected error for unconvertible token")
	}
}

func TestZCollapsesToX(t *testing.T) {
	if VZ.ToStrong() != VX {
		t.Fatal("Z must collapse to X, never to a definite level")
	}
	if Not(VZ) != VX || And(VZ, V1) != VX || Or(VZ, V0) != VX {
		t.Fatal("Z must propagate as X")
	}
	if Equal(VZ, VX) {
		t.Fatal("Z and X stay distinct for exact provenance comparisons")
	}
}

func TestKleeneDominance(t *testing.T) {
	if And(VX, V0) != V0 || Or(VX, V1) != V1 {
		t.Fatal("definite side must dominate when insensitive")
	}
	if And(VX, V1) != VX || Or(VX, V0) != VX {
		t.Fatal("genuinely unknown inputs must stay X")
	}
}
