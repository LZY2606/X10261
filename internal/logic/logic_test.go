package logic

import "testing"

func TestFourState(t *testing.T) {
	cases := []struct {
		a, b, and, or L
	}{
		{Zero, One, Zero, One},
		{One, X, X, One},
		{Zero, X, Zero, X},
		{One, Z, X, One},
		{Zero, Z, Zero, X},
		{X, Z, X, X},
	}
	for _, c := range cases {
		if And(c.a, c.b) != c.and || Or(c.a, c.b) != c.or {
			t.Fatalf("%v,%v got and %v or %v want %v/%v", c.a, c.b, And(c.a, c.b), Or(c.a, c.b), c.and, c.or)
		}
	}
	if Not(Z) != X {
		t.Fatal("not z")
	}
	if Eq(Z, Z) != X || Eq(One, One) != One || Eq(One, Zero) != Zero {
		t.Fatal("eq")
	}
}

func TestTri(t *testing.T) {
	if TAnd(TTrue, TUnknown) != TUnknown || TAnd(TFalse, TUnknown) != TFalse {
		t.Fatal("tand")
	}
	if TOr(TFalse, TUnknown) != TUnknown || TOr(TTrue, TUnknown) != TTrue {
		t.Fatal("tor")
	}
	if FromL(Z) != TUnknown || FromL(One) != TTrue || FromL(Zero) != TFalse {
		t.Fatal("fromL")
	}
}
