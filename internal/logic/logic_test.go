package logic

import "testing"

func TestResolve(t *testing.T) {
	cases := []struct{ a, b, want Bit }{
		{BZ, B0, B0}, {B0, BZ, B0}, {B0, B1, BX},
		{BX, B1, BX}, {B1, B1, B1}, {BZ, BZ, BZ},
	}
	for _, c := range cases {
		if got := Resolve(c.a, c.b); got != c.want {
			t.Errorf("Resolve(%v,%v)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestKleene(t *testing.T) {
	if Sample(BZ) != TUnknown || Sample(BX) != TUnknown {
		t.Fatal("X/Z must sample unknown")
	}
	if And(TUnknown, TFalse) != TFalse {
		t.Fatal("unknown & false must be false")
	}
	if Or(TUnknown, TTrue) != TTrue {
		t.Fatal("unknown | true must be true")
	}
	if And(TUnknown, TTrue) != TUnknown || Or(TUnknown, TFalse) != TUnknown {
		t.Fatal("indefinite must stay unknown")
	}
	if Impl(TUnknown, TTrue) != TTrue {
		t.Fatal("unknown -> true must be true")
	}
}

func TestVector(t *testing.T) {
	v, ok := ParseVector("b101z", 4)
	if !ok || v.String() != "101z" {
		t.Fatalf("v=%s ok=%v", v.String(), ok)
	}
	if got, _ := v.SampleBit(3); got != TUnknown {
		t.Fatal("z bit unknown")
	}
	if !v.AllUnknown() {
		t.Fatal("expected unknown presence")
	}
	// LSB anchoring / MSB padding with Z.
	v2, ok := ParseVector("b10", 4)
	if !ok || v2.String() != "zz10" {
		t.Fatalf("pad=%s %v", v2.String(), ok)
	}
}

func TestDecodeBadRetained(t *testing.T) {
	if val, ok := Decode("b??", 4); ok || val.Raw != "b??" {
		t.Fatalf("bad decode must retain raw, got %+v %v", val, ok)
	}
	if _, ok := Decode("1", 1); !ok {
		t.Fatal("scalar decode")
	}
}
