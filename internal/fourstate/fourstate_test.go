package fourstate

import "testing"

func TestTernaryNeverSilentlyResolves(t *testing.T) {
	for _, v := range []Value{X, Z} {
		if _, known := v.Ternary(); known {
			t.Fatalf("%s must read as unknown, got a definite boolean", v)
		}
	}
	if b, known := V1.Ternary(); !known || !b {
		t.Fatal("V1 must read as true")
	}
	if b, known := V0.Ternary(); !known || b {
		t.Fatal("V0 must read as false")
	}
}

func TestSemanticRevisionDifference(t *testing.T) {
	// 0 AND X: pessimistic -> X, controlling value -> 0.
	if got := And(V0, X, SemVer10); got != X {
		t.Fatalf("v1.0 pessimistic AND(0,X) = %s, want x", got)
	}
	if got := And(V0, Z, SemVer10); got != X {
		t.Fatalf("v1.0 pessimistic AND(0,Z) = %s, want x", got)
	}
	if got := And(V0, X, SemVer11); got != V0 {
		t.Fatalf("v1.1 controlling AND(0,X) = %s, want 0", got)
	}
	// OR(1, X) mirrors the rule with controlling value 1.
	if got := Or(V1, X, SemVer10); got != X {
		t.Fatalf("v1.0 OR(1,X) = %s want x", got)
	}
	if got := Or(V1, Z, SemVer11); got != V1 {
		t.Fatalf("v1.1 OR(1,Z) = %s want 1", got)
	}
	// XOR has no controlling value in either revision.
	if got := Xor(V1, Z, SemVer11); got != X {
		t.Fatalf("XOR(1,Z) = %s want x under v1.1", got)
	}
	// Z remains observable even when it coalesces at a gate.
	if ExplainZ(Z) == "" {
		t.Fatal("Z must carry an explicit propagation diagnostic")
	}
}

func TestWireResolution(t *testing.T) {
	cases := []struct{ a, b, want Value }{
		{Z, V1, V1}, {V0, Z, V0}, {V0, V1, X}, {X, V1, X}, {V1, V1, V1},
	}
	for _, c := range cases {
		if got := ResolveWire(c.a, c.b); got != c.want {
			t.Fatalf("ResolveWire(%s,%s)=%s want %s", c.a, c.b, got, c.want)
		}
	}
}

func TestEdges(t *testing.T) {
	if DetectEdge(V0, V1) != EdgeRise || DetectEdge(V1, V0) != EdgeFall {
		t.Fatal("definite edges misclassified")
	}
	for _, pair := range [][2]Value{{X, V1}, {Z, V1}, {V0, Z}, {V1, X}} {
		if DetectEdge(pair[0], pair[1]) != EdgeNone {
			t.Fatalf("edge over %s->%s must be unknown", pair[0], pair[1])
		}
	}
}
