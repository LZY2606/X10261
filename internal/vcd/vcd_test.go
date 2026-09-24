package vcd

import (
	"strings"
	"testing"

	"sigcourt/internal/logic4"
)

const sample = `$timescale 1ns $end
$var wire 1 ! clk $end
$var wire 1 " req $end
$var wire 1 & aux $end
$enddefinitions $end
#0
0!
0"
0&
#5
1"
#10
1!
#15
0!
u&
#20
1!
`

func TestParseKeepsRawAndDiagnoses(t *testing.T) {
	f, err := Parse(sample)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.TimescaleRaw != "1ns" || f.TimescaleNum != 1 || f.TimescaleUnit != "ns" {
		t.Fatalf("timescale: %+v", f)
	}
	if f.ScaleFs() != 1000000 {
		t.Fatalf("scale = %d, want 1e6 fs per ns", f.ScaleFs())
	}
	// the unconvertible token u& must stay in place with a diagnostic
	var found bool
	for _, c := range f.Changes {
		if c.Raw == "u&" {
			found = true
			if c.OK {
				t.Fatal("u& must be marked not-OK")
			}
			if c.Time != 15 || c.Signal != "aux" {
				t.Fatalf("u& at wrong place: %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("raw token u& was dropped")
	}
	if len(f.Diagnostics) != 1 || !strings.Contains(f.Diagnostics[0].Reason, "not convertible") {
		t.Fatalf("diagnostics: %+v", f.Diagnostics)
	}
	// converted values are right
	for _, c := range f.Changes {
		if c.Raw == "1!" && (c.Value != logic4.V1 || !c.OK) {
			t.Fatalf("bad conversion: %+v", c)
		}
	}
}

func TestStructuralErrorRollsBack(t *testing.T) {
	if _, err := Parse("$timescale 1ns $end\n#0\n1!\n"); err == nil {
		t.Fatal("missing $enddefinitions must be a structural error")
	}
	if _, err := Parse(strings.Replace(sample, "u&", "u%", 1)); err == nil {
		t.Fatal("change for undeclared id must be a structural error")
	}
}

func TestVectorTokenKeptRaw(t *testing.T) {
	f, err := Parse("$timescale 1ps $end\n$var wire 4 $ bus $end\n$enddefinitions $end\n#0\nb1010 $\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Changes) != 1 || f.Changes[0].OK {
		t.Fatalf("vector must be kept raw: %+v", f.Changes)
	}
	if len(f.Diagnostics) != 1 {
		t.Fatalf("want diagnostic for vector token")
	}
}
