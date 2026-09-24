package property

import "testing"

const src = `module demo;
clock @ posedge clk;
reset rst;
p_imp: disable iff (rst) a |-> b ##[1:2] c;
p_non: a |=> b;
p_str: strong a[*1:3];
p_empty: empty |-> b;
p_and: a ##[0:0] b or c;
`

func TestParseAndIDs(t *testing.T) {
	m, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "demo" || m.ClockName != "clk" || m.ClockEdge != "posedge" || m.ResetName != "rst" {
		t.Fatalf("header parsed wrong: %+v", m)
	}
	if len(m.Decls) != 5 {
		t.Fatalf("decls: %d", len(m.Decls))
	}
	for _, d := range m.Decls {
		if d.NodeID == "" || d.Property.NodeID == "" {
			t.Fatalf("decl %s missing stable ids", d.Name)
		}
	}
	if m.Decls[0].Property.Kind != PropDisable {
		t.Fatal("p_imp expected disable wrapper")
	}
	imp := m.Decls[0].Property.Inner
	if imp.Kind != PropImp || !imp.Overlapped {
		t.Fatal("expected overlapped implication")
	}
	if m.Decls[1].Property.Kind != PropImp || m.Decls[1].Property.Overlapped {
		t.Fatal("expected non-overlapped implication")
	}
	if m.Decls[2].Property.Kind != PropSeq || m.Decls[2].Property.Strength != "strong" {
		t.Fatal("expected strong seq")
	}
}

func TestParseStableAcrossRepeats(t *testing.T) {
	a, _ := Parse(src)
	b, _ := Parse(src)
	if a.NodeID != b.NodeID {
		t.Fatal("module identity must be content-stable")
	}
	if a.Decls[0].NodeID != b.Decls[0].NodeID {
		t.Fatal("decl identity must be content-stable")
	}
}

func TestParseBad(t *testing.T) {
	if _, err := Parse("module x; clock @ posedge clk; p: a |->;"); err == nil {
		t.Fatal("malformed consequent must error")
	}
}
