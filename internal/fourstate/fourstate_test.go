package fourstate

import "testing"

func TestParseAndString(t *testing.T) {
	cases := map[string]Value{"0": Zero, "1": One, "x": X, "X": X, "z": Z, "Z": Z}
	for raw, want := range cases {
		got, err := Parse(raw)
		if err != nil || got != want {
			t.Fatalf("Parse(%q)=%v,%v want %v", raw, got, err, want)
		}
	}
	if _, err := Parse(""); err == nil {
		t.Fatal("empty token must fail")
	}
	if _, err := Parse("w"); err == nil {
		t.Fatal("unknown token must fail")
	}
}

func TestPropagation(t *testing.T) {
	if Not(Z) != X {
		t.Fatal("Z must propagate as X through inverters")
	}
	if And(One, X) != X || And(Zero, X) != Zero {
		t.Fatal("definite AND result must win, otherwise X propagates")
	}
	if Or(Zero, X) != X || Or(One, Z) != One {
		t.Fatal("OR propagation wrong")
	}
	if Not(One) != Zero || Not(Zero) != One {
		t.Fatal("NOT wrong")
	}
}
