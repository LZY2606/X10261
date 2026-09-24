package fourstate

import "testing"

func TestParseAndTables(t *testing.T) {
	if got, _ := Parse('Z'); got != Z {
		t.Fatalf("parse Z = %v", got)
	}
	cases := []struct {
		op          string
		a, b, want  Bit
	}{
		{"and", Zero, X, Zero},
		{"and", One, X, X},
		{"and", One, Z, Z},
		{"and", X, Z, X},
		{"and", One, One, One},
		{"or", One, Z, One},
		{"or", Zero, Z, Z},
		{"or", Zero, X, X},
	}
	for _, c := range cases {
		var got Bit
		if c.op == "and" {
			got = And(c.a, c.b)
		} else {
			got = Or(c.a, c.b)
		}
		if got != c.want {
			t.Errorf("%s(%v,%v)=%v want %v", c.op, c.a, c.b, got, c.want)
		}
	}
	if Not(Z) != X {
		t.Fatalf("not Z must collapse to X in a boolean context, got %v", Not(Z))
	}
}

func TestTriVerdict(t *testing.T) {
	if Tri(X) != VUnknown || Tri(Z) != VUnknown {
		t.Fatal("X and Z must both yield explicit unknown")
	}
	if Tri(Zero) != VFalse || Tri(One) != VTrue {
		t.Fatal("resolved bits map to false/true")
	}
	if AndV(VFalse, VUnknown) != VFalse || OrV(VTrue, VUnknown) != VTrue {
		t.Fatal("tri-state operators must short-circuit on resolved values")
	}
	if NotV(VUnknown) != VUnknown {
		t.Fatal("unknown must survive negation")
	}
}
