package fourval

import "testing"

func TestGatePropagation(t *testing.T) {
	cases := []struct {
		op       string
		a, b, w Value
	}{
		{"and", V0, VX, V0}, // definite 0 dominates
		{"and", V1, VZ, VX}, // Z propagates as unknown
		{"and", VX, VZ, VX},
		{"or", V1, VX, V1}, // definite 1 dominates
		{"or", V0, VZ, VX},
		{"xor", V0, VZ, VX},
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
			t.Fatalf("%s(%s,%s)=%s want %s", c.op, c.a, c.b, got, c.w)
		}
	}
	if Not(VZ) != VX {
		t.Fatal("inverter on Z must be X")
	}
	if FromValue(VZ).IsDefinite() {
		t.Fatal("Z must map to unknown tri-state")
	}
}
