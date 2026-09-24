package domain

import "testing"

func TestTimescale(t *testing.T) {
	q := ParseTime("10 ns", "vcd:abc")
	if !q.Valid || q.Fs != 10_000_000 {
		t.Fatalf("10ns = %d (%s)", q.Fs, q.Diag)
	}
	q100 := ParseTime("100 ps", "vcd:abc")
	if !q100.Valid || q100.Fs != 100_000 {
		t.Fatalf("100ps = %d valid %v", q100.Fs, q100.Valid)
	}
	if q100.Raw != "100 ps" {
		t.Fatal("raw lost")
	}
	bad := ParseTime("soon", "vcd:abc")
	if bad.Valid || bad.Diag == "" {
		t.Fatal("unconvertible value must stay in place with diagnosis")
	}
	// tick conversion: #3 under 100ps timescale
	if q100.Fs*3 != 300_000 {
		t.Fatal("tick multiply")
	}
}

func TestWord(t *testing.T) {
	w := NewWord("10xz")
	if w.Width() != 4 || w.String() != "10xz" || w.Raw != "10xz" {
		t.Fatalf("word %s", w.String())
	}
	if NewWord("1").Scalar() != B1 {
		t.Fatal("scalar")
	}
}
