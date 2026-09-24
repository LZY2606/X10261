package prop

import "testing"

func TestParseRoundTrip(t *testing.T) {
	src := `clock main posedge clk every 10 ns;
reset rst from 0 to 5;
property p1 { req |-> gnt }
property p2 on main { disable iff (rst) req ##1 req |=> gnt & !err }
property p3 { (req | gnt) ##2 aux }
`
	f, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Clocks) != 1 || f.Clocks[0].Period != 10 || f.Clocks[0].Unit != "ns" {
		t.Fatalf("clocks: %+v", f.Clocks)
	}
	if len(f.Resets) != 1 || f.Resets[0].From != 0 || f.Resets[0].To != 5 {
		t.Fatalf("resets: %+v", f.Resets)
	}
	if len(f.Properties) != 3 {
		t.Fatalf("properties: %d", len(f.Properties))
	}
	// canonical rendering must re-parse to an identical rendering
	for _, p := range f.Properties {
		s := String(p.Body)
		f2, err := Parse("property x { " + s + " }")
		if err != nil {
			t.Fatalf("reparse %q: %v", s, err)
		}
		if String(f2.Properties[0].Body) != s {
			t.Fatalf("round trip mismatch: %q", s)
		}
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse("property p { req |-> }"); err == nil {
		t.Fatal("want error")
	}
	if _, err := Parse("property p { req ## gnt }"); err == nil {
		t.Fatal("want error")
	}
}
