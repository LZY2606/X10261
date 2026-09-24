package fourstate

import "testing"

func TestProjectionAndZ(t *testing.T) {
	if ToT3(VZ) != TUnknown {
		t.Fatal("Z must project to unknown, never to a definite boolean")
	}
	if ToT3(VX) != TUnknown || ToT3(V0) != TFalse || ToT3(V1) != TTrue {
		t.Fatal("bad projection")
	}
	v, ok := Parse("Z")
	if !ok || v != VZ {
		t.Fatal("parse Z")
	}
	if _, ok := (TUnknown).Bool(); ok {
		t.Fatal("unknown must be indefinite")
	}
}

func TestKleenePropagation(t *testing.T) {
	cases := []struct {
		op      string
		a, b    T3
		want    T3
	}{
		{"and", TFalse, TUnknown, TFalse},
		{"and", TTrue, TUnknown, TUnknown},
		{"and", TUnknown, TUnknown, TUnknown},
		{"or", TTrue, TUnknown, TTrue},
		{"or", TFalse, TUnknown, TUnknown},
		{"xor", TTrue, TUnknown, TUnknown},
		{"eq", TUnknown, TFalse, TUnknown},
	}
	for _, c := range cases {
		var got T3
		switch c.op {
		case "and":
			got = And(c.a, c.b)
		case "or":
			got = Or(c.a, c.b)
		case "xor":
			got = Xor(c.a, c.b)
		case "eq":
			got = Equal(c.a, c.b)
		}
		if got != c.want {
			t.Fatalf("%s(%s,%s)=%s want %s", c.op, c.a, c.b, got, c.want)
		}
	}
	if Not(TUnknown) != TUnknown {
		t.Fatal("not unknown must stay unknown")
	}
}
