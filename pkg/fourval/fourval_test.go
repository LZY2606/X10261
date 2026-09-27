package fourval

import "testing"

func TestParseAndString(t *testing.T) {
	cases := []struct {
		in   byte
		want Value
	}{
		{'0', Zero}, {'1', One}, {'x', X}, {'X', X}, {'z', Z}, {'Z', Z},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil || got != c.want {
			t.Fatalf("Parse(%q)=%v,%v want %v", c.in, got, err, c.want)
		}
		if got.String() != string(c.want.String()[0]) {
			t.Fatalf("roundtrip %s", got)
		}
	}
	if _, err := Parse('7'); err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestTriFrom(t *testing.T) {
	if TriFrom(Zero) != TriFalse || TriFrom(One) != TriTrue {
		t.Fatal("driven reduction wrong")
	}
	if TriFrom(X) != TriUnknown || TriFrom(Z) != TriUnknown {
		t.Fatal("X/Z must reduce to explicit unknown, never coerced")
	}
}

func TestPropagation(t *testing.T) {
	// 0 controls AND even beside X/Z.
	if And(Zero, X) != Zero || And(Zero, Z) != Zero {
		t.Fatal("AND control rule violated")
	}
	// 1 controls OR even beside X/Z.
	if Or(One, X) != One || Or(One, Z) != One {
		t.Fatal("OR control rule violated")
	}
	// undriven propagation yields explicit unknown.
	for _, pair := range [][2]Value{{X, One}, {Z, One}, {X, Z}} {
		if And(pair[0], pair[1]) != X {
			t.Fatalf("And(%v,%v) must be x", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]Value{{X, Zero}, {Z, Zero}, {Z, X}} {
		if Or(pair[0], pair[1]) != X {
			t.Fatalf("Or(%v,%v) must be x", pair[0], pair[1])
		}
	}
	if Not(Z) != X {
		t.Fatal("not(z) must propagate unknown")
	}
	if Xor(X, One) != X || Xor(One, Zero) != One || Xor(One, One) != Zero {
		t.Fatal("xor table wrong")
	}
}

func TestReduce(t *testing.T) {
	if ReduceAnd([]Value{One, X}) != TriUnknown {
		t.Fatal("reduceand x propagation")
	}
	if ReduceAnd([]Value{Zero, X}) != TriFalse {
		t.Fatal("reduceand control")
	}
	if ReduceOr([]Value{Zero, Z}) != TriUnknown {
		t.Fatal("reduceor z propagation")
	}
	if ReduceOr([]Value{One, Z}) != TriTrue {
		t.Fatal("reduceor control")
	}
}
