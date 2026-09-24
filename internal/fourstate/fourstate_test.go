package fourstate

import "testing"

func TestTruthTables(t *testing.T) {
	cases := []struct {
		a, b, and, or Value
	}{
		{Zero, X, Zero, X},
		{One, X, X, One},
		{X, Z, X, X},
		{Z, Z, Z, Z},
		{One, One, One, One},
		{Zero, Zero, Zero, Zero},
	}
	for _, c := range cases {
		if got := And(c.a, c.b); got != c.and {
			t.Errorf("And(%v,%v)=%v want %v", c.a, c.b, got, c.and)
		}
		if got := Or(c.a, c.b); got != c.or {
			t.Errorf("Or(%v,%v)=%v want %v", c.a, c.b, got, c.or)
		}
	}
	if Not(X) != X || Not(Z) != Z || Not(Zero) != One || Not(One) != Zero {
		t.Error("Not table wrong")
	}
}

func TestToTruth(t *testing.T) {
	if X.ToTruth() != Unknown || Z.ToTruth() != Unknown {
		t.Error("X/Z must map to explicit unknown")
	}
	if One.ToTruth() != True || Zero.ToTruth() != False {
		t.Error("0/1 mapping wrong")
	}
	v, _ := ParseVector("10x0")
	if v.ToTruth() != Unknown {
		t.Error("vector with X must be unknown")
	}
	v2, _ := ParseVector("0000")
	if v2.ToTruth() != False {
		t.Error("all-zero vector must be false")
	}
	v3, _ := ParseVector("010")
	if v3.ToTruth() != True {
		t.Error("non-zero known vector must be true")
	}
}
