package fourstate

import "testing"

func TestResolve(t *testing.T) {
	cases := []struct {
		in   []Value
		want Value
	}{
		{[]Value{Z, Z}, Z},
		{[]Value{Z, One}, One},
		{[]Value{Zero, Z}, Zero},
		{[]Value{Zero, One}, X},
		{[]Value{One, X}, X},
		{[]Value{Zero, X}, X},
	}
	for _, c := range cases {
		if got := Resolve(c.in...); got != c.want {
			t.Errorf("Resolve%v = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestPropagation(t *testing.T) {
	if And(X, One) != X || And(Z, One) != Z {
		t.Fatal("and must preserve X and Z distinctly")
	}
	if Or(X, Zero) != X || Or(Z, Zero) != Z {
		t.Fatal("or must preserve X and Z distinctly")
	}
	if Not(X) != X || Not(Z) != Z {
		t.Fatal("not must preserve X and Z")
	}
}
