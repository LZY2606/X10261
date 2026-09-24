package qty

import "testing"

func TestCommonGrid(t *testing.T) {
	ns, _ := ParseTimescale("1ns")
	ps, _ := ParseTimescale("10ps")
	g, ok := CommonGrid(ns, ps)
	if !ok {
		t.Fatal("integer scales must normalize")
	}
	if g.FemtosecondsPerTick != 10000 {
		t.Fatalf("grid = %d fs, want 10000 (10ps)", g.FemtosecondsPerTick)
	}
	if tk, _ := g.Map(ns, 5); tk != 500 {
		t.Fatalf("5ns = %d ticks of 10ps, want 500", tk)
	}
	if tk, _ := g.Map(ps, 3); tk != 3 {
		t.Fatalf("3*10ps = %d ticks, want 3", tk)
	}
}

func TestFractionalStaysRaw(t *testing.T) {
	ts, err := ParseTimescale("0.1ns")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ts.UnitStep(); ok {
		t.Fatal("fractional unit step must not be silently normalized away")
	}
	if _, err := ParseTimescale("3 fortnights"); err == nil {
		t.Fatal("unconvertible timescale must carry a diagnosis")
	}
}
