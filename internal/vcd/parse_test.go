package vcd

import (
	"os"
	"testing"
)

const sample = `$timescale 10 ns $end
$scope module tb $end
$var wire 1 ! clk $end
$var wire 1 " req $end
$var wire 4 # data [3:0] $end
$var real 1 $ temp $end
$upscope $end
$enddefinitions $end
#0
0!
0"
bxxxx #
r3.14 $
#5
1!
#10
0!
1"
b10xz #
#15
1!
r? $
`

func TestParsePreservesSource(t *testing.T) {
	res, err := Parse([]byte(sample), "ing1")
	if err != nil {
		t.Fatal(err)
	}
	if res.TsRaw != "10 ns" || res.Timescale.Fs != 10_000_000 {
		t.Fatalf("timescale raw=%q fs=%d", res.TsRaw, res.Timescale.Fs)
	}
	if len(res.Signals) != 4 {
		t.Fatalf("signals %d", len(res.Signals))
	}
	if res.Signals[2].Width != 4 || res.Signals[2].Short != "data" {
		t.Fatalf("vector signal %+v", res.Signals[2])
	}
	// change 1! at tick 5 -> 50 ns
	var posedge *Change
	for i := range res.Changes {
		if res.Changes[i].Code == "!" && res.Changes[i].Tick == 5 {
			posedge = &res.Changes[i]
		}
	}
	if posedge == nil || posedge.TimeFs != 50_000_000 || posedge.Token != "1" {
		t.Fatalf("posedge change %+v", posedge)
	}
	if posedge.Begin == posedge.End || sample[posedge.Begin] != '1' {
		t.Fatalf("byte range %d:%d not pointing at source", posedge.Begin, posedge.End)
	}
	// 10xz preserved raw
	var vec *Change
	for i := range res.Changes {
		if res.Changes[i].WordRaw == "10xz" {
			vec = &res.Changes[i]
		}
	}
	if vec == nil || vec.Tick != 10 || vec.TimeFs != 100_000_000 {
		t.Fatalf("vector change %+v", vec)
	}
	// unparseable real stays in place with a diagnostic, not dropped
	var nan *Change
	for i := range res.Changes {
		if res.Changes[i].IsReal && res.Changes[i].RealVal == "?" {
			nan = &res.Changes[i]
		}
	}
	if nan == nil || nan.Diag == "" {
		t.Fatal("unconvertible real value must be retained with diagnosis")
	}
	if len(res.Diagnostic) == 0 {
		t.Fatal("diagnostic record expected")
	}
	if res.EndTick != 15 || res.EndTimeFs != 150_000_000 {
		t.Fatalf("end %d %d", res.EndTick, res.EndTimeFs)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := Parse([]byte("$enddefinitions $end\n"), "x"); err == nil {
		t.Fatal("expected error on VCD without signals")
	}
	if _, err := Parse(nil, "x"); err == nil {
		t.Fatal("nil must error, not panic")
	}
}

func TestMain(m *testing.M) { os.Exit(m.Run()) }
