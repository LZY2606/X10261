package vcd

import (
	"strings"
	"testing"

	"sigtribunal/internal/fourval"
)

const sample = `$timescale 1ns $end
$scope module top $end
$var wire 1 # clk $end
$var wire 1 $ sig $end
$upscope $end
$enddefinitions $end
#0
0#
0$
#5
1#
#10
0#
x$
#15
1#
r2.5 $
#20
z$
`

func TestParsePreservesRawAndDiagnostics(t *testing.T) {
	f, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Timescale != "1ns" || !f.ScaleOK || f.ScaleFactor != 1000 {
		t.Fatalf("timescale: %q ok=%v factor=%d", f.Timescale, f.ScaleOK, f.ScaleFactor)
	}
	sig := f.ByName["sig"]
	if sig == nil {
		t.Fatal("missing sig")
	}
	// four-state values preserved
	if ch, _ := sig.At(10); ch.Val != fourval.X || ch.Raw != "x$" {
		t.Fatalf("x change not preserved: %+v", ch)
	}
	if ch, _ := sig.At(20); ch.Val != fourval.Z {
		t.Fatalf("z change not preserved: %+v", ch)
	}
	// unconvertible real value kept in place with diagnostic
	found := false
	for _, c := range sig.Changes {
		if !c.OK && c.Raw == "r2.5 $" && c.Time == 15 {
			found = true
		}
	}
	if !found {
		t.Fatal("unconvertible value not kept in place")
	}
	if len(f.Diagnostics) == 0 || !strings.Contains(f.Diagnostics[0], "r2.5") {
		t.Fatalf("missing diagnostic: %v", f.Diagnostics)
	}
	// sampled (preponed) vs current (inclusive) at same timestamp
	clk := f.ByName["clk"]
	if ch, ok := clk.SampledAt(5); !ok || ch.Val != fourval.Zero {
		t.Fatal("preponed sample at edge time must see the old value, not the edge change")
	}
	if ch, ok := clk.At(5); !ok || ch.Val != fourval.One {
		t.Fatal("current value at edge time must include the edge change")
	}
}
