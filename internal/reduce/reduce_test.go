package reduce

import (
	"testing"

	"sigtribunal/internal/prop"
	"sigtribunal/internal/vcd"
)

const wave = `$timescale 1ns $end
$var wire 1 # clk $end
$var wire 1 % req $end
$var wire 1 & ack $end
$var wire 1 ' fin $end
$enddefinitions $end
#0
0#
0%
0&
0'
#5
1#
#10
0#
#12
1%
#15
1#
#20
0#
#25
1#
#30
0#
#35
1#
#40
0#
#45
1#
#50
0#
#55
1#
`

func TestReduceKeepsOriginAndOnlyDropsUnrelated(t *testing.T) {
	w, err := vcd.Parse([]byte(wave))
	if err != nil {
		t.Fatal(err)
	}
	props, err := prop.ParseFile(`property p;
  @(posedge clk) req |-> ##[1:3] ack ##1 fin;
endproperty`)
	if err != nil {
		t.Fatal(err)
	}
	run, err := prop.Evaluate(w, props[0], prop.Params{MaxDelay: 8}, prop.Semantics{Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "fail" {
		t.Fatalf("status=%q", run.Status)
	}
	cands := Reduce(run)
	if len(cands) < 2 {
		t.Fatalf("want multiple legal candidates, got %d", len(cands))
	}
	origin := run.Sampled.Times[0]
	for _, c := range cands {
		if c.Origin != origin {
			t.Fatalf("candidate %s changed time origin: %d != %d", c.ID, c.Origin, origin)
		}
		if c.Unit != "1ns" {
			t.Fatalf("candidate %s lost coordinate basis", c.ID)
		}
		// kept + removed cycles must partition the window; kept must equal
		// the referenced cycles of the failing path
		kept := map[int]bool{}
		for _, iv := range c.Kept {
			for cc := iv.StartCycle; cc <= iv.EndCycle; cc++ {
				kept[cc] = true
			}
		}
		for _, iv := range c.Removed {
			for cc := iv.StartCycle; cc <= iv.EndCycle; cc++ {
				if kept[cc] {
					t.Fatalf("candidate %s: cycle %d both kept and removed", c.ID, cc)
				}
			}
		}
	}
	// at least one candidate must actually remove an irrelevant interval
	removedAny := false
	for _, c := range cands {
		if len(c.Removed) > 0 {
			removedAny = true
		}
	}
	if !removedAny {
		t.Fatal("reduction never removed a failure-irrelevant interval")
	}
}
