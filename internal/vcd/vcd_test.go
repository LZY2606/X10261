package vcd

import (
	"strings"
	"testing"
)

const sample = `$timescale 100ps $end
$scope module tb $end
$var wire 1 ! clk [0:0] $end
$var wire 8 # data [7:0] $end
$var real 32 $ rtemp temp $end
$upscope $end
$enddefinitions $end
#0
$dumpvars
0!
b101x011z #
r1.5 $
$end
#5
1!
b?10 #
rnotanumber $
#9
q!
`

func TestParsePreservesEverything(t *testing.T) {
	f := Parse("case.vcd", []byte(sample))
	if f.FSPerTick != 100 {
		t.Fatalf("fs per tick = %d, want 100", f.FSPerTick)
	}
	if got := f.Changes[0].TimeFS; got != 0 {
		t.Fatalf("first change fs=%d", got)
	}
	if got := f.Changes[3].TimeFS; got != 500 {
		t.Fatalf("change at #5 fs=%d, want 500", got)
	}
	// raw tokens preserved including x/z/?
	var vec *Change
	var badReal *Change
	var badScalar *Change
	for i := range f.Changes {
		c := &f.Changes[i]
		if c.RawValue == "b101x011z" {
			vec = c
		}
		if c.RawValue == "rnotanumber" {
			badReal = c
		}
		if c.RawValue == "q" {
			badScalar = c
		}
	}
	if vec == nil || string(vec.Bits) != "101x011z" || vec.Problem != "" {
		t.Fatalf("vector not preserved: %+v", vec)
	}
	if badReal == nil || badReal.Problem == "" || badReal.Kind != KindReal {
		t.Fatalf("unconvertible real must stay in place with problem: %+v", badReal)
	}
	if badScalar == nil || badScalar.Kind != KindInvalid {
		t.Fatalf("unconvertible scalar retained as invalid: %+v", badScalar)
	}
	// '?' repeat marker retained verbatim
	if !strings.Contains(joinRaw(f), "b?10") {
		t.Fatal("repeat marker lost")
	}
	if f.Signals[0].Name != "clk" || f.Signals[1].Width != 8 {
		t.Fatalf("signals parsed wrong: %+v", f.Signals)
	}
	if len(f.Diagnostics) < 3 {
		t.Fatalf("expected diagnostics for ? marker context/real/scalar, got %d: %+v", len(f.Diagnostics), f.Diagnostics)
	}
}

func joinRaw(f *File) string {
	var b strings.Builder
	for _, c := range f.Changes {
		b.WriteString(c.RawValue)
	}
	return b.String()
}
