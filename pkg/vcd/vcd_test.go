package vcd

import (
	"strings"
	"testing"
)

const sample = `$date today $end
$timescale 1 ns $end
$scope module tb $end
$var wire 1 ! clk $end
$var wire 1 " rst_n $end
$var wire 4 # data $end
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
0"
#15
b10xz #
#20
z!
`

func TestParsePreservesRawAndFourState(t *testing.T) {
	f, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Timescale.Number != 1 || f.Timescale.Unit != "ns" ||
		f.Timescale.TicksPerSecond != 1_000_000_000 {
		t.Fatalf("timescale normalized wrong: %+v", f.Timescale)
	}
	if string(f.Raw) != sample {
		t.Fatal("raw bytes must be retained verbatim")
	}
	if len(f.Signals) != 3 {
		t.Fatalf("signals: %+v", f.Signals)
	}
	// Locate X/Z vector change with raw token retained.
	var found bool
	for _, ch := range f.Changes {
		if ch.Code == "#" && ch.TimeTicks == 15 {
			found = true
			if ch.Raw != "b10xz #" {
				t.Fatalf("raw vector token not retained: %q", ch.Raw)
			}
			// stored LSB-first: z,x,0,1
			want := []string{"z", "x", "0", "1"}
			if len(ch.Bits) != 4 {
				t.Fatalf("bits %v", ch.Bits)
			}
			for i, w := range want {
				if ch.Bits[i].String() != w {
					t.Fatalf("bit %d = %s want %s", i, ch.Bits[i], w)
				}
			}
		}
	}
	if !found {
		t.Fatal("vector change missing")
	}
	if f.MaxTick != 20 {
		t.Fatalf("max tick %d", f.MaxTick)
	}
	if got := f.ValueAt("!", 19); got.String() != "0" {
		t.Fatalf("held value at 19: %s", got)
	}
	if got := f.ValueAt("!", 20); got.String() != "z" {
		t.Fatalf("z must be held at 20: %s", got)
	}
	// No $dumpvars for this signal at #0 in a fresh parse: the parser
	// initializes to x until the first change is observed. Test that on
	// a signal whose code never appears.
	if got := f.ValueAt("ZZZ", 1); got.String() != "x" {
		t.Fatalf("never-driven signal must be x, got %s", got)
	}
}

func TestTimescaleDiagnostics(t *testing.T) {
	f, err := Parse([]byte(`$timescale 2 pluto $end
$enddefinitions $end
#0
`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Timescale.Number != 2 || f.Timescale.Unit != "pluto" {
		t.Fatalf("unconvertible unit fields not retained: %+v", f.Timescale)
	}
	if len(f.Timescale.Diagnostics) == 0 ||
		!strings.Contains(f.Timescale.Diagnostics[0], "unknown timescale unit") {
		t.Fatalf("diagnostic missing: %+v", f.Timescale.Diagnostics)
	}
}
