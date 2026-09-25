package prop

import "testing"

func TestParseRoundtrip(t *testing.T) {
	texts := []string{
		"req ##1 gnt",
		"req |-> gnt",
		"req |=> gnt ##2 done",
		"disable iff (rst) req |-> gnt[*3]",
		"!rst && (a || b) |-> (c ##1 d) or e",
		"data[0] ##1 !data[3]",
	}
	for _, tx := range texts {
		sp, err := Parse(tx)
		if err != nil {
			t.Fatalf("parse %q: %v", tx, err)
		}
		h1 := ASTHash(sp.Property)
		sp2, err := Parse(tx)
		if err != nil {
			t.Fatal(err)
		}
		if h2 := ASTHash(sp2.Property); h1 != h2 {
			t.Fatalf("unstable hash for %s", tx)
		}
		if len(sp.Signals()) == 0 {
			t.Fatalf("no signals for %q", tx)
		}
	}
}

func TestParseImplicationKinds(t *testing.T) {
	o, _ := Parse("a |-> b")
	n, _ := Parse("a |=> b")
	pl, _ := Parse("a ##1 b")
	if o.Property.Kind != POverlap || n.Property.Kind != PNonOverlap || pl.Property.Kind != PPlain {
		t.Fatalf("kinds %s %s %s", o.Property.Kind, n.Property.Kind, pl.Property.Kind)
	}
}

func TestParseDisableAndBits(t *testing.T) {
	sp, err := Parse("disable iff (rst_n == 0) a")
	_ = sp
	// '==' is intentionally unsupported and must be a parse error (rollback).
	if err == nil {
		t.Fatal("expected parse error for unsupported operator")
	}
	sp2, err := Parse("disable iff (rst) data[2]")
	if err != nil {
		t.Fatal(err)
	}
	if sp2.Disable == nil || sp2.Disable.Name != "rst" {
		t.Fatal("disable iff not parsed")
	}
	var found bool
	for _, r := range sp2.Signals() {
		if r.Name == "data" && r.Bit == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("data[2] signal ref missing")
	}
}
