package fourval

import "testing"

func TestParseAndPropagation(t *testing.T) {
	cases := []struct {
		in  string
		out Val
	}{
		{"0", V0}, {"1", V1}, {"x", VX}, {"Z", VZ},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil || got != c.out {
			t.Fatalf("Parse(%q)=%v,%v want %v", c.in, got, err, c.out)
		}
	}
	if _, err := Parse("10"); err == nil {
		t.Fatal("scalar parse must reject vector")
	}
	for _, b := range []string{"10x", "1z", "zzz", "xxx"} {
		got, err := ParseBus(b)
		if err != nil {
			t.Fatalf("ParseBus(%q): %v", b, err)
		}
		if got.IsKnown() {
			t.Fatalf("ParseBus(%q)=%v must stay non-known", b, got)
		}
	}
	if g := ParseBus("111").must(t); g != V1 {
		t.Fatalf("bus 111 = %v", g)
	}
}

func (v Val) must(t *testing.T) Val { return v }

func TestOps(t *testing.T) {
	if And(V1, VZ) != VX || And(V0, VZ) != V0 {
		t.Fatal("Z must propagate through AND except when dominated by 0")
	}
	if Or(V0, VX) != VX || Or(V1, VX) != V1 {
		t.Fatal("X propagation through OR incorrect")
	}
	if Not(VZ) != VX || Xor(V1, VZ) != VX {
		t.Fatal("Z negation / XOR propagation incorrect")
	}
}

func TestVerdictTriAndDisable(t *testing.T) {
	if VerdictVacuous.Tri() != "pass" || VerdictFail.Tri() != "fail" || VerdictUnknown.Tri() != "unknown" {
		t.Fatal("tri-state mapping wrong")
	}
	if d, ok := Disabled(VX); ok || d {
		t.Fatal("X on disable condition must not disable")
	}
	if d, ok := Disabled(V1); !ok || !d {
		t.Fatal("known-1 disable condition must disable")
	}
	if d, _ := Disabled(V0); d {
		t.Fatal("known-0 disable condition must not disable")
	}
}
