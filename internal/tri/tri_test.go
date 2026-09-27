package tri

import "testing"

func TestFourState(t *testing.T) {
	if And(L1, Z) != X {
		t.Fatal("Z must resolve to X in AND")
	}
	if Or(L0, Z) != X {
		t.Fatal("Z must resolve to X in OR")
	}
	if Or(Z, L1) != L1 {
		t.Fatal("driven 1 must dominate Z")
	}
	if And(L0, X) != L0 {
		t.Fatal("0 must dominate X in AND")
	}
	if Not(Z) != X {
		t.Fatal("negation of Z is X")
	}
}
