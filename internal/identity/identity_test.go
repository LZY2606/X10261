package identity

import "testing"

func TestStable(t *testing.T) {
	a := Of("sig", []any{"x", 1})
	b := Of("sig", []any{"x", 1})
	c := Of("sig", []any{"x", 2})
	if a != b || a == c {
		t.Fatal("identity not stable/discriminating")
	}
	if len(a) != 20 || a[:4] != "sig_" {
		t.Fatalf("bad id %q", a)
	}
	if len(Hash([]byte("abc"))) != 64 {
		t.Fatal("hash length")
	}
}
