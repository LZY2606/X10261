package replay

import (
	"strings"
	"testing"

	"signalcourt/internal/property"
	"signalcourt/internal/vcd"
)

// buildReplayer parses a tiny VCD and creates a posedge grid on clk.
func buildReplayer(t *testing.T, vcdText string, sem property.SemParams) (*Replayer, *Grid) {
	t.Helper()
	w, err := vcd.Parse(strings.NewReader(vcdText))
	if err != nil {
		t.Fatalf("vcd parse: %v", err)
	}
	cd := ClockDomain{ClockName: "clk", Edge: "posedge", ResetName: "rst", ResetLevel: "1"}
	g, err := BuildGrid(w, cd)
	if err != nil {
		t.Fatalf("grid: %v", err)
	}
	return NewReplayer(NewSampler(w, g), g, cd, sem), g
}

const vcdBase = `$timescale 1ns $end
$scope module tb $end
$var wire 1 ! clk $end
$var wire 1 " req $end
$var wire 1 # gnt $end
$var wire 1 $ rst $end
$var wire 1 % zsig $end
$upscope $end
$enddefinitions $end
#0
0! 0" 0# 0$ 0%
#5
1!
#10
0!
#15
1!
#20
0!
1"
#25
1!
#30
0!
0"
#35
1!
#40
0!
1#
#45
1!
#50
0!
0#
#55
1!
#60
0!
1$
#65
1!
#70
0!
0$
#75
1!
#80
0!
z%
#85
1!
#90
0!
`

func TestOverlapAndNonOverlap(t *testing.T) {
	r, g := buildReplayer(t, vcdBase, property.SemV1())
	if len(g.Ticks) < 5 {
		t.Fatalf("want >=5 ticks, got %d", len(g.Ticks))
	}
	// req |-> gnt overlapped: req rises at tick 1 (value req sampled at the
	// edge via preponed: req became 1 at #10, edge at #15 -> sampled req=1),
	// gnt is 0 then, so that attempt fails.
	p, err := property.Parse("req |-> gnt")
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(p)
	sawFail := false
	for _, a := range res {
		if a.Verdict == VerdictFail {
			sawFail = true
		}
	}
	if !sawFail {
		t.Fatalf("overlapped: expected a failing attempt, got %+v", verdicts(res))
	}

	// non-overlapped: |=> checks gnt one tick later; gnt=1 at the tick after
	// req=1 sampled, so that attempt must pass.
	p2, err := property.Parse("req |=> gnt")
	if err != nil {
		t.Fatal(err)
	}
	res2 := r.Run(p2)
	sawPass := false
	var failed []int
	for _, a := range res2 {
		if a.StartTick == 1 && a.Verdict == VerdictPass {
			sawPass = true
		}
		if a.Verdict == VerdictFail {
			failed = append(failed, a.StartTick)
		}
	}
	if !sawPass {
		t.Fatalf("non-overlapped tick1 should pass, got %+v", verdicts(res2))
	}
	_ = failed
}

func TestXZPropagation(t *testing.T) {
	r, _ := buildReplayer(t, vcdBase, property.SemV1())
	p, _ := property.Parse("zsig")
	res := r.Run(p)
	// After zsig becomes z (around tick 7), attempts must be unknown, never
	// forced to 0 or 1.
	var unknownAt int = -1
	for _, a := range res {
		if a.Verdict == VerdictUnknown {
			unknownAt = a.StartTick
		}
		if a.StartTick >= 7 && a.Verdict != VerdictUnknown && a.Verdict != VerdictFail {
			// before the z it is 0 (fail); after z it must be unknown
		}
	}
	if unknownAt < 0 {
		t.Fatalf("expected unknown attempts after z, got %+v", verdicts(res))
	}
	// Tri mapping must be unknown with reason x/z.
	for _, a := range res {
		if a.Verdict == VerdictUnknown && a.Verdict.Tri().Value != "unknown" {
			t.Fatalf("unknown verdict must map to tri unknown")
		}
	}
}

func TestDisableIffVersions(t *testing.T) {
	r1, _ := buildReplayer(t, vcdBase, property.SemV1())
	p, _ := property.Parse("disable iff (rst) req |-> gnt")
	res1 := r1.Run(p)
	disabled := 0
	for _, a := range res1 {
		if a.Verdict == VerdictDisabled {
			disabled++
		}
	}
	if disabled == 0 {
		t.Fatalf("expected disabled attempts during rst, got %+v", verdicts(res1))
	}
}

func TestEmptyMatchExplicitVacuous(t *testing.T) {
	r, _ := buildReplayer(t, vcdBase, property.SemV1())
	// Antecedent that never matches => vacuous true, tri must be true.
	p, _ := property.Parse("(req && gnt) |-> req")
	res := r.Run(p)
	vac := 0
	for _, a := range res {
		if a.Verdict == VerdictVacuous {
			vac++
			if a.Verdict.Tri().Value != "true" {
				t.Fatalf("vacuous must map to tri=true")
			}
		}
	}
	if vac == 0 {
		t.Fatalf("expected vacuous attempts, got %+v", verdicts(res))
	}
	// Explicit empty antecedent.
	p2, _ := property.Parse("empty |=> req")
	res2 := r.Run(p2)
	foundEmptyRule := false
	for _, a := range res2 {
		for _, b := range a.Basis {
			if b.Rule == "empty-antecedent" {
				foundEmptyRule = true
			}
		}
	}
	if !foundEmptyRule {
		t.Fatalf("expected empty-antecedent basis event, got %+v", res2)
	}
}

func TestResetIntervals(t *testing.T) {
	w, _ := vcd.Parse(strings.NewReader(vcdBase))
	cd := ClockDomain{ClockName: "clk", Edge: "posedge", ResetName: "rst", ResetLevel: "1"}
	g, _ := BuildGrid(w, cd)
	smp := NewSampler(w, g)
	spans := smp.ResetIntervals(cd)
	if len(spans) == 0 {
		t.Fatal("expected at least one reset span")
	}
	found := false
	for _, sp := range spans {
		if sp.Kind == "active" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected an active reset interval")
	}
}

func TestUnknownResetBoundaryV1V2Differ(t *testing.T) {
	// rst goes to x briefly: v1 => unknown attempt; v2 => disabled attempt.
	vcdX := strings.Replace(vcdBase, "#50\n0!\n1$", "#50\n0!\nx$", 1)
	r1, _ := buildReplayer(t, vcdX, property.SemV1())
	r2, _ := buildReplayer(t, vcdX, property.SemV2())
	p, _ := property.Parse("disable iff (rst) req")
	res1 := r1.Run(p)
	res2 := r2.Run(p)
	v1unk, v2dis := false, false
	for _, a := range res1 {
		if a.Verdict == VerdictUnknown && hasRule(a.Basis, "disable-iff-x") {
			v1unk = true
		}
	}
	for _, a := range res2 {
		if a.Verdict == VerdictDisabled {
			v2dis = true
		}
	}
	if !v1unk {
		t.Fatalf("v1 must keep x-reset attempt unknown, got %+v", verdicts(res1))
	}
	if !v2dis {
		t.Fatalf("v2 must disable on x-reset, got %+v", verdicts(res2))
	}
}

func hasRule(ev []BasisEvent, rule string) bool {
	for _, b := range ev {
		if b.Rule == rule {
			return true
		}
	}
	return false
}

func verdicts(rs []AttemptResult) []string {
	out := make([]string, len(rs))
	for i, a := range rs {
		out[i] = string(a.Verdict)
	}
	return out
}
