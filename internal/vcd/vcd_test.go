package vcd

import (
	"strings"
	"testing"
)

const sample = `$timescale 1 ns $end
$scope module dut $end
$var wire 1 ! clk $end
$var wire 1 " rst $end
$var wire 1 # sig $end
$upscope $end
$enddefinitions $end
#0
$dumpvars
0!
1"
x#
$end
#5
1!
#10
0!
z#
#15
1!
`

func TestParseBasics(t *testing.T) {
	w, err := Parse("sample.vcd", []byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Signals) != 3 {
		t.Fatalf("signals: %d", len(w.Signals))
	}
	if !w.Timescale.OK || w.Timescale.FSPerTick != 1_000_000 {
		t.Fatalf("timescale: %+v", w.Timescale)
	}
	var zfound bool
	for _, c := range w.Changes {
		if c.Value.String() == "z" {
			zfound = true
		}
		if c.Offset < 0 || c.RawToken == "" {
			t.Fatal("change must retain offset and raw token")
		}
	}
	if !zfound {
		t.Fatal("Z change must be retained as four-state z")
	}
	if w.Changes[0].Initial != true {
		t.Fatal("dumpvars changes must be marked initial")
	}
	if w.RawSHA256 == "" || len(w.RawBytes) != len(sample) {
		t.Fatal("raw bytes and hash must be retained")
	}
}

func TestMissingTimescaleRejected(t *testing.T) {
	if _, err := Parse("x.vcd", []byte("$var wire 1 ! a $end\n#0\n0!\n")); err == nil {
		t.Fatal("missing timescale must be a hard error enabling rollback")
	}
}

func TestUnknownValueDiagnostic(t *testing.T) {
	bad := strings.Replace(sample, "z#", "?#", 1)
	w, err := Parse("bad.vcd", []byte(bad))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Diagnostics) == 0 {
		t.Fatal("uninterpretable token must stay in place with a diagnostic, not be dropped")
	}
}
