package vcd

import (
	"strings"
	"testing"
	"time"

	"signalcourt/internal/fourstate"
)

const sample = `$timescale 10ns $end
$scope module tb $end
$var wire 1 ! clk $end
$var wire 1 " d $end
$enddefinitions $end
#0
0!
0"
#5
1!
x"
#10
0!
w"
#15
z"
`

func TestParsePreservesRawAndDiagnostics(t *testing.T) {
	f, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Timescale.Raw != "10ns" || f.Timescale.Unit != 10*time.Nanosecond {
		t.Fatalf("timescale not preserved: %+v", f.Timescale)
	}
	d := f.SignalByName("d")
	if d == nil || len(d.Changes) != 4 {
		t.Fatalf("d changes = %v", d)
	}
	if d.Changes[1].Raw != "x" || d.Changes[1].Value != fourstate.VX || !d.Changes[1].Valid {
		t.Fatal("X change not preserved")
	}
	bad := d.Changes[2]
	if bad.Valid || bad.Diagnostic == "" || bad.Raw != "w" {
		t.Fatalf("unconvertible value must stay in place with diagnostic: %+v", bad)
	}
	if len(f.Diagnostics) != 1 || f.Diagnostics[0].Kind != "value" {
		t.Fatalf("diagnostics = %+v", f.Diagnostics)
	}
	z := d.Changes[3]
	if z.Value != fourstate.VZ || z.Time != 15 {
		t.Fatalf("Z change wrong: %+v", z)
	}
	v, hold, _ := d.ValueAt(12)
	if v != fourstate.VX || hold.Raw != "w" {
		t.Fatalf("hold at 12 must retain raw w token, got %q", hold.Raw)
	}
	clk := f.SignalByName("clk")
	if got, _, _ := clk.ValueAt(5); got != fourstate.V1 {
		t.Fatalf("clk@5 = %v", got)
	}
}

func TestUnconvertibleTimescaleStaysRaw(t *testing.T) {
	src := strings.NewReader("$timescale 12 glarps $end\n$var wire 1 ! q $end\n$enddefinitions $end\n#0\n0!\n")
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if f.Timescale.Convertible || f.Timescale.Raw != "12 glarps" || f.Timescale.Unit != 0 {
		t.Fatalf("unconvertible timescale must remain raw with no basis: %+v", f.Timescale)
	}
}

func TestTimescalePrecisionBasis(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		unit time.Duration
	}{
		{"1ns", time.Nanosecond},
		{"10ns", 10 * time.Nanosecond},
		{"1ps", time.Nanosecond / 1000},
	} {
		f, err := Parse(strings.NewReader("$timescale " + tc.raw + " $end\n$var wire 1 ! q $end\n$enddefinitions $end\n#0\n0!\n"))
		if err != nil {
			t.Fatal(err)
		}
		if f.Timescale.Unit != tc.unit || f.Timescale.Precision != tc.unit {
			t.Fatalf("%s: got %v", tc.raw, f.Timescale.Unit)
		}
	}
}
