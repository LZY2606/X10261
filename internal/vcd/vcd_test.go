package vcd

import (
	"strings"
	"testing"
)

const sample = `$date today $end
$timescale 10 ns $end
$scope module tb $end
$var wire 1 ! clk $end
$var wire 1 " rst $end
$var wire 4 # data [3:0] $end
$upscope $end
$enddefinitions $end
#0
$dumpvars
0!
1"
bxxxx #
$end
#5
1!
#10
0!
b101z #
#15
b?? #
`

func TestParseBasics(t *testing.T) {
	s, err := Parse(strings.NewReader(sample), "src1", "a.vcd")
	if err != nil {
		t.Fatal(err)
	}
	if s.Timescale.UnitName != "ns" || s.Timescale.UnitFS != 10_000_000 {
		t.Fatalf("timescale: %+v", s.Timescale)
	}
	if len(s.Signals) != 3 {
		t.Fatalf("signals: %d", len(s.Signals))
	}
	clk := s.Signals[0]
	if clk.Name != "clk" || clk.Width != 1 || len(clk.Changes) != 3 {
		t.Fatalf("clk: %+v", clk)
	}
	// Raw time and femtosecond time preserved.
	if clk.Changes[1].RawTime != "5" || clk.Changes[1].TimeFS != 50_000_000 {
		t.Fatalf("time: %+v", clk.Changes[1])
	}
	data := s.Signals[2]
	if got := data.Changes[1].Value.Vec.String(); got != "101z" {
		t.Fatalf("vector = %s", got)
	}
	// Undecodable token retained in place.
	last := data.Changes[2]
	if last.Diagnostic == "" || last.RawValue != "b??" || last.TimeFS != 150_000_000 {
		t.Fatalf("undecodable change: %+v", last)
	}
	if len(s.Diagnostics) == 0 {
		t.Fatal("expected diagnostic for undecodable value")
	}
}

func TestMissingTimescaleFatal(t *testing.T) {
	if _, err := Parse(strings.NewReader("#0\n1!\n"), "x", "x.vcd"); err == nil {
		t.Fatal("expected fatal error for missing timescale")
	}
}
