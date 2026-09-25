package vcd

import (
	"strings"
	"testing"
)

const sample = `$date today $end
$timescale 1 ns $end
$scope module tb $end
$var wire 1 ! clk $end
$var wire 1 " rst $end
$var wire 1 # data $end
$upscope $end
$enddefinitions $end
#0
0!
1"
x#
#5
1!
#10
0!
1#
#15
H#
`

func TestParseBaseline(t *testing.T) {
	res, err := Parse("id_proj_x", "tb.vcd", []byte(sample), "casepack")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Material.Timescale != "1ns" {
		t.Fatalf("timescale raw = %q", res.Material.Timescale)
	}
	if len(res.Signals) != 3 {
		t.Fatalf("signals = %d", len(res.Signals))
	}
	if res.Signals[0].RawName != "clk" || res.Signals[1].VCDCode != `"` {
		t.Fatal("raw signal identity/order lost")
	}
	if len(res.Changes) != 6 {
		t.Fatalf("changes = %d", len(res.Changes))
	}
	// X propagation token retained.
	if res.Changes[2].Value != "x" {
		t.Fatalf("want x, got %q", res.Changes[2].Value)
	}
	// Unconvertible token H stays in place with raw token and diagnostic.
	last := res.Changes[5]
	if last.Kind != ChangeUnconvertKind() || last.RawValue != "H" || last.Value != "" {
		t.Fatalf("unconvertible handling wrong: %+v", last)
	}
	if last.Picoseconds != -1 {
		t.Fatal("unconvertible change must not carry normalized coordinate")
	}
	if !strings.Contains(last.Diagnostics, "retained") {
		t.Fatalf("diagnostic lost: %q", last.Diagnostics)
	}
	// Normalized ps derived on convertible changes.
	if res.Changes[3].Picoseconds != 5_000 {
		t.Fatalf("ps at #5 = %d", res.Changes[3].Picoseconds)
	}
	if res.Material.RawSHA256 == "" || !strings.Contains(string(res.Material.RawBytes), "$timescale") {
		t.Fatal("raw bytes/hash not retained")
	}
}

func TestParseFatalRollback(t *testing.T) {
	bad := strings.Replace(sample, `$var wire 1 ! clk $end`, "", 1)
	if _, err := Parse("p", "x.vcd", []byte(bad), "upload"); err == nil {
		t.Fatal("undeclared id change must be fatal for rollback")
	}
	if _, err := Parse("p", "empty.vcd", []byte(""), "upload"); err == nil {
		t.Fatal("empty input must fail")
	}
}

func TestTimescaleSubpsUnconvertible(t *testing.T) {
	if _, ok := TimescaleToPs("1fs"); ok {
		t.Fatal("fs must be unconvertible to integer ps")
	}
	if ps, ok := TimescaleToPs("10ps"); !ok || ps != 10 {
		t.Fatalf("10ps = %d,%v", ps, ok)
	}
}
