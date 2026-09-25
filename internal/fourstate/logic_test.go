package fourstate

import "testing"

func TestParse(t *testing.T) {
	cases := map[string]Value{
		"0": V0, "1": V1, "x": VX, "X": VX, "z": VZ, "Z": VZ, "?": VX, "b": VX,
	}
	for tok, want := range cases {
		got, ok := Parse(tok)
		if !ok || got != want {
			t.Fatalf("Parse(%q)=%v,%v want %v,true", tok, got, ok, want)
		}
	}
	if _, ok := Parse("H"); ok {
		t.Fatal("H must not parse into a four-state value")
	}
}

func TestPropagation(t *testing.T) {
	// Controlling values win even with unknown operands.
	if And(V0, VX) != V0 || And(V0, VZ) != V0 {
		t.Fatal("AND controlling-0 must force 0")
	}
	if Or(V1, VX) != V1 || Or(V1, VZ) != V1 {
		t.Fatal("OR controlling-1 must force 1")
	}
	// Non-controlling unknowns propagate as X.
	if And(V1, VX) != VX || And(V1, VZ) != VX {
		t.Fatal("AND with unknown must be X")
	}
	if Or(V0, VX) != VX || Or(V0, VZ) != VX {
		t.Fatal("OR with unknown must be X")
	}
	if Xor(V1, VZ) != VX {
		t.Fatal("XOR with Z must be X")
	}
	if Not(VZ) != VX || Not(VX) != VX {
		t.Fatal("NOT of unknown must be X")
	}
	// Definite cases.
	if And(V1, V1) != V1 || Or(V0, V0) != V0 || Xor(V1, V0) != V1 {
		t.Fatal("definite boolean algebra wrong")
	}
	if v, ok := VZ.Bool(); ok {
		t.Fatalf("Z.Bool() must be unknown, got %v", v)
	}
}

func TestReduce(t *testing.T) {
	if ReduceAnd([]Value{V1, V1, VX}) != VX {
		t.Fatal("ReduceAnd must propagate X")
	}
	if ReduceAnd([]Value{V1, V0, VX}) != V0 {
		t.Fatal("ReduceAnd controlling 0 must win")
	}
	if ReduceOr([]Value{V0, VZ}) != VX {
		t.Fatal("ReduceOr must propagate Z as X")
	}
}
