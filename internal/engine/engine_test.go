package engine

import (
	"testing"

	"signalcourt/internal/clock"
	"signalcourt/internal/property"
	"signalcourt/internal/vcd"
)

const engineVCD = `$timescale 1 ns $end
$scope module d $end
$var wire 1 ! clk $end
$var wire 1 " rst $end
$var wire 1 # a $end
$var wire 1 $ b $end
$var wire 1 % c $end
$var wire 1 & zs $end
$upscope $end
$enddefinitions $end
#0
$dumpvars
0!
1"
0#
0$
0%
1&
$end
#5
1!
#7
0"
#10
0!
#15
1!
#20
0!
#25
1!
#27
z&
#30
0!
#35
1!
#35
1#
#40
0!
#45
1!
#45
1$
#50
0!
#55
1!
#55
0$
#60
0!
#65
1!
#70
0!
#75
1!
#80
0!
#85
1!
#90
0!
#95
1!
#100
0!
#105
1!
#110
0!
#115
1!
`

const engineProps = `module demo;
clock @ posedge clk;
reset rst;
p_dis: disable iff (rst) a |-> b;
p_non: a |=> b;
p_empty: empty |-> b;
p_strong: strong c;
p_z: zs |-> b;
p_delay: a ##[1:2] b;
`

func setup(t *testing.T, semver string) (*Engine, *vcd.Waveform) {
	w, err := vcd.Parse("engine.vcd", []byte(engineVCD))
	if err != nil {
		t.Fatal(err)
	}
	d, err := clock.Build("aclk", "clk", clock.Posedge, w)
	if err != nil {
		t.Fatal(err)
	}
	m, err := property.Parse(engineProps)
	if err != nil {
		t.Fatal(err)
	}
	return New(m, d, semver), w
}

func verdictOf(t *testing.T, rs []*Replay, name string, tick int) TickResult {
	t.Helper()
	for _, r := range rs {
		if r.PropName == name {
			return r.Results[tick]
		}
	}
	t.Fatalf("no replay for %s", name)
	return TickResult{}
}

func hasRule(tr TickResult, rule string) bool {
	for _, ev := range tr.RuleEvents {
		if ev.Rule == rule {
			return true
		}
	}
	return false
}

func TestResetBoundaryAndOverlap(t *testing.T) {
	e, _ := setup(t, "v1.0.0")
	rs, err := e.Run()
	if err != nil {
		t.Fatal(err)
	}
	// tick 0: rst sampled 1 -> disabled, explicit pass via disable_iff
	tr := verdictOf(t, rs, "p_dis", 0)
	if tr.Disabled != "true" || tr.Verdict != VPass {
		t.Fatalf("tick0 reset: %+v", tr)
	}
	if !hasRule(tr, "disable_iff") {
		t.Fatal("disable iff must emit a machine-readable rule event")
	}
	// rst falls at 7; tick1 (edge15) samples rst=0 and a=0: ante false -> vacuous
	tr1 := verdictOf(t, rs, "p_dis", 1)
	if tr1.Disabled != "false" || !tr1.Vacuous || tr1.Verdict != VPass {
		t.Fatalf("tick1 ante false must be vacuous pass: %+v", tr1)
	}
	if !hasRule(tr1, "empty_match") {
		t.Fatal("non-match of antecedent must be an explicit empty_match event")
	}
}

func TestNonOverlappedVsOverlapped(t *testing.T) {
	e, _ := setup(t, "v1.0.0")
	rs, _ := e.Run()
	// a rises exactly on edge #35 = tick index 6; v1.0 reads 1 at that edge,
	// b is still 0, so the overlapped form fails in the same tick.
	ov := verdictOf(t, rs, "p_dis", 6)
	non := verdictOf(t, rs, "p_non", 6)
	if ov.Verdict != VFail {
		t.Fatalf("overlapped checks same tick, expected fail got %s events=%+v", ov.Verdict, ov.RuleEvents)
	}
	if !hasRule(ov, "implication_overlapped") {
		t.Fatal("missing overlapped rule event")
	}
	// non-overlapped starts cons at tick 7 (b still 0) then tick 8 where b
	// becomes 1 exactly on the edge; the v1.0 read is 1 and it passes.
	if non.Verdict == VFail {
		t.Fatalf("non-overlapped cons at tick7/8 should survive, got %s", non.Verdict)
	}
	if !hasRule(non, "implication_non_overlapped") {
		t.Fatal("missing non-overlapped rule event")
	}
}

func TestEmptyMatchExplicitThreeState(t *testing.T) {
	e, _ := setup(t, "v1.0.0")
	rs, _ := e.Run()
	tr := verdictOf(t, rs, "p_empty", 0)
	if !tr.Vacuous || tr.Verdict != VPass || !hasRule(tr, "empty_match") {
		t.Fatalf("empty antecedent must be an explicit three-state event: %+v", tr)
	}
}

func TestZPropagation(t *testing.T) {
	e, _ := setup(t, "v1.0.0")
	rs, _ := e.Run()
	// zs initial 1, becomes z at #27; edge tick5 (#25) still reads 1,
	// edge tick6 (#35) reads z and the antecedent becomes unknown.
	tr := verdictOf(t, rs, "p_z", 6)
	if tr.Verdict != VUnknown {
		t.Fatalf("Z antecedent must yield unknown, not fail/pass: %s %+v", tr.Verdict, tr.RuleEvents)
	}
	var coerced bool
	for _, ev := range tr.RuleEvents {
		if ev.Rule == "xz_propagation" {
			if cz, _ := ev.Details["coerced_z"].(bool); cz {
				coerced = true
			}
		}
	}
	if !coerced {
		t.Fatal("Z coercion must be recorded on the rule event")
	}
}

func TestMultiThreads(t *testing.T) {
	e, _ := setup(t, "v1.0.0")
	rs, _ := e.Run()
	// p_delay with a window [1:2] creates up to two consequent candidates.
	// At tick6 a=1 (edge#35), b=0 so the window endpoints at tick7/8
	// produce distinct live/failing candidate threads.
	tr := verdictOf(t, rs, "p_delay", 6)
	ids := map[string]bool{}
	for _, th := range tr.Threads {
		ids[th.ID] = true
	}
	if len(ids) < 2 {
		t.Fatalf("delay window must retain multiple thread candidates: %d", len(ids))
	}
}

func TestSamplingVersionDifference(t *testing.T) {
	e10, _ := setup(t, "v1.0.0")
	e11, _ := setup(t, "v1.1.0")
	r10, _ := e10.Run()
	r11, _ := e11.Run()
	// a rises exactly on edge #35 = tick6: v1.0 reads 1, v1.1 prepone 0.
	v10 := verdictOf(t, r10, "p_delay", 6)
	v11 := verdictOf(t, r11, "p_delay", 6)
	if v10.Verdict == v11.Verdict {
		t.Fatalf("sampling version must change the boundary verdict: %s vs %s", v10.Verdict, v11.Verdict)
	}
	if !v10.Region.Strict && v11.Region.Strict {
		t.Fatal("regions must identify their sampling convention")
	}
}
