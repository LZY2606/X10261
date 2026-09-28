package fourstate

import "testing"

func TestTruthTables(t *testing.T) {
	if Not(VZ) != VX {
		t.Fatalf("Not(Z) = %s, want x", Not(VZ))
	}
	cases := []struct {
		op       string
		a, b, w  Value
	}{
		{"and", V0, VZ, V0},
		{"and", V1, VZ, VX},
		{"and", V1, VX, VX},
		{"and", V1, V1, V1},
		{"or", V1, VZ, V1},
		{"or", V0, VZ, VX},
		{"xor", V0, VZ, VX},
		{"xor", V1, V0, V1},
		{"xor", V1, V1, V0},
	}
	for _, c := range cases {
		var got Value
		switch c.op {
		case "and":
			got = And(c.a, c.b)
		case "or":
			got = Or(c.a, c.b)
		case "xor":
			got = Xor(c.a, c.b)
		}
		if got != c.w {
			t.Errorf("%s(%s,%s)=%s want %s", c.op, c.a, c.b, got, c.w)
		}
	}
	if b, ok := ToBool(VZ); ok {
		t.Fatalf("ToBool(Z) reported definite %v", b)
	}
	if v, _ := ParseVector("zz", 4); v != VZ {
		t.Fatalf("ParseVector(zz)=%s want z", v)
	}
	if v, _ := ParseVector("101x", 4); v != VX {
		t.Fatalf("ParseVector(101x)=%s want x", v)
	}
	if v, _ := ParseVector("101", 4); v != V1 {
		t.Fatalf("ParseVector(101)=%s want 1", v)
	}
}
