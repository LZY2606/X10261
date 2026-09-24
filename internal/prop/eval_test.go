package prop

import (
	"strings"
	"testing"

	"sigtribunal/internal/vcd"
)

const waveA = `$timescale 1ns $end
$var wire 1 # clk $end
$var wire 1 $ rst $end
$var wire 1 % req $end
$var wire 1 & ack $end
$var wire 1 ' fin $end
$var wire 1 ^ reqx $end
$var wire 1 _ ackx $end
$enddefinitions $end
#0
0#
0$
0%
0&
0'
0^
0_
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
#28
1&
#30
0#
#35
1#
#38
0&
#40
0#
#42
0%
#44
1'
#45
1#
#46
1^
#48
1&
#50
0#
0'
0_
#52
1%
#55
1#
#58
0&
#60
0#
x_
#62
0%
#65
1#
#70
0#
0^
#75
1#
#80
0#
x^
#85
1#
#90
0#
0^
#92
1%
#95
1#
1$
#100
0#
0%
#102
0$
#105
1#
#110
0#
#115
1#
#120
0#
#125
1#
`

func parseWave(t *testing.T) *vcd.File {
	t.Helper()
	w, err := vcd.Parse([]byte(waveA))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func parseOne(t *testing.T, src string) *Property {
	t.Helper()
	ps, err := ParseFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 {
		t.Fatalf("want 1 property, got %d", len(ps))
	}
	return ps[0]
}

var semV10 = Semantics{Version: "1.0"}
var semV11 = Semantics{Version: "1.1", AntecedentUnknownAsNoMatch: true, UnknownConsequenceAsFail: true}
var par = Params{MaxDelay: 8}

func threadStates(run *Run) map[string]string {
	m := map[string]string{}
	for _, th := range run.Threads {
		m[th.ID] = th.State
	}
	return m
}

func TestOverlappedImplicationThreads(t *testing.T) {
	w := parseWave(t)
	p := parseOne(t, `property p_overlap;
  @(posedge clk) disable iff (rst)
  req |-> ##[1:3] ack ##1 fin;
endproperty`)
	run, err := Evaluate(w, p, par, semV10)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "fail" {
		t.Fatalf("status = %q, want fail", run.Status)
	}
	st := threadStates(run)
	if st["t1"] != "pass" || st["t2"] != "pass" {
		t.Fatalf("attempts 1,2 should pass: %v", st)
	}
	if st["t3"] != "fail" || st["t5"] != "fail" {
		t.Fatalf("attempts 3,5 should fail: %v", st)
	}
	if st["t9"] != "disabled" {
		t.Fatalf("attempt 9 should be disabled (rst at edge): %v", st)
	}
	// machine-readable rule events present
	var sawDisable, sawOverlap bool
	for _, e := range run.Events {
		if e.Rule == "disable-iff" && e.Cycle == 9 && e.Result == "T" {
			sawDisable = true
		}
		if e.Rule == "overlapped-implication" {
			sawOverlap = true
		}
	}
	if !sawDisable || !sawOverlap {
		t.Fatalf("missing rule events: %+v", run.Events)
	}
	// failing paths: multiple legal candidates for attempt 5
	var n5 int
	for _, fp := range run.FailPaths {
		if fp.ThreadID == "t5" {
			n5++
		}
	}
	if n5 != 3 {
		t.Fatalf("attempt 5 should yield 3 failing paths, got %d", n5)
	}
}

func TestNonOverlappedImplication(t *testing.T) {
	w := parseWave(t)
	p := parseOne(t, `property p_nonoverlap;
  @(posedge clk) req && ack |=> fin;
endproperty`)
	run, err := Evaluate(w, p, par, semV10)
	if err != nil {
		t.Fatal(err)
	}
	st := threadStates(run)
	if st["t3"] != "pass" {
		t.Fatalf("t3 should pass (fin at next cycle): %v", st)
	}
	if st["t5"] != "fail" {
		t.Fatalf("t5 should fail (fin=0 at next cycle): %v", st)
	}
	var saw bool
	for _, e := range run.Events {
		if e.Rule == "non-overlapped-implication" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("missing non-overlapped-implication rule event")
	}
}

func TestEmptyMatchHasExplicitResult(t *testing.T) {
	w, err := vcd.Parse([]byte(`$timescale 1ps $end
$var wire 1 a clk2 $end
$var wire 1 b start $end
$var wire 1 c done $end
$var wire 1 d flag $end
$enddefinitions $end
#0
0a
0b
0c
0d
#5000
1a
#6000
1b
#10000
0a
#15000
1a
#20000
0a
#22000
1d
#25000
1a
#30000
0a
#35000
1a
`))
	if err != nil {
		t.Fatal(err)
	}
	p := parseOne(t, `property p_empty;
  @(posedge clk2) start |=> done[*0] ##1 flag;
endproperty`)
	run, err := Evaluate(w, p, par, semV10)
	if err != nil {
		t.Fatal(err)
	}
	st := threadStates(run)
	if st["t1"] != "pass" {
		t.Fatalf("empty-match consequent should pass: %v", st)
	}
	var saw bool
	for _, e := range run.Events {
		if e.Rule == "empty-match" && e.Result == "T" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("empty match must produce an explicit three-state result")
	}
}

func TestXZPropagationAndSemanticsVersions(t *testing.T) {
	w := parseWave(t)
	p := parseOne(t, `property p_sem;
  @(posedge clk) reqx |-> ##[0:2] ackx;
endproperty`)
	run10, err := Evaluate(w, p, par, semV10)
	if err != nil {
		t.Fatal(err)
	}
	if run10.Status != "unknown" {
		t.Fatalf("v1.0: X in consequent must propagate, got %q", run10.Status)
	}
	run11, err := Evaluate(w, p, par, semV11)
	if err != nil {
		t.Fatal(err)
	}
	if run11.Status != "fail" {
		t.Fatalf("v1.1: unknown consequence treated as fail, got %q", run11.Status)
	}
	// antecedent X at cycle 8 (reqx=x): v1.0 -> unknown thread, v1.1 -> no-match
	st10 := threadStates(run10)
	st11 := threadStates(run11)
	if st10["t8"] != "unknown" {
		t.Fatalf("v1.0 t8 should be unknown: %v", st10)
	}
	if st11["t8"] != "vacuous" {
		t.Fatalf("v1.1 t8 should be vacuous (X as no-match): %v", st11)
	}
	var sawXZ bool
	for _, e := range run10.Events {
		if e.Rule == "xz-propagation" && e.Result == "X" {
			sawXZ = true
		}
	}
	if !sawXZ {
		t.Fatal("xz-propagation rule events must be recorded")
	}
}

func TestRawASTPreserved(t *testing.T) {
	src := `property p_raw;
  @(posedge clk) disable iff (rst)
  req |-> ##[1:3] ack;
endproperty`
	p := parseOne(t, src)
	if !strings.Contains(p.Raw, "|->") || !strings.Contains(p.Raw, "## [ 1 : 3 ]") {
		t.Fatalf("raw AST text not preserved: %q", p.Raw)
	}
}
