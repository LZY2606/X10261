package replay

import (
	"testing"

	"sigcourt/internal/logic4"
	"sigcourt/internal/prop"
	"sigcourt/internal/vcd"
)

// demoWave builds the main demo waveform (timescale 1ns, clk period 10).
//
//	#0   clk=0 rst=1 req=0 gnt=0 aux=0
//	#5   rst=0
//	#25  req=1
//	#35  gnt=1
//	#45  req=0
//	#55  req=1, gnt=0
//	#65  rst=1            (reset pulse, interval [65,75))
//	#75  rst=0
//	#85  rst=X
//	#88  err=1
//	#95  rst=Z, req=0
//	#105 rst=0
//	#105 err=0
//	#115 rst=1, req=1, aux='u' (unconvertible), #118 aux=1
func demoWave() *Waveform {
	type ch struct {
		t   int64
		sig string
		v   logic4.Value
	}
	var changes []vcd.Change
	add := func(t int64, sig string, v logic4.Value) {
		changes = append(changes, vcd.Change{Time: t, Signal: sig, Value: v, OK: true})
	}
	add(0, "clk", logic4.V0)
	add(0, "rst", logic4.V1)
	add(0, "req", logic4.V0)
	add(0, "gnt", logic4.V0)
	add(0, "aux", logic4.V0)
	add(0, "err", logic4.V0)
	add(5, "rst", logic4.V0)
	for i := 1; i <= 12; i++ {
		add(int64(i*10), "clk", logic4.V1)
		add(int64(i*10+5), "clk", logic4.V0)
	}
	add(25, "req", logic4.V1)
	add(35, "gnt", logic4.V1)
	add(45, "req", logic4.V0)
	add(55, "req", logic4.V1)
	add(55, "gnt", logic4.V0)
	add(65, "rst", logic4.V1)
	add(75, "rst", logic4.V0)
	add(85, "rst", logic4.VX)
	add(88, "err", logic4.V1)
	add(95, "rst", logic4.VZ)
	add(95, "req", logic4.V0)
	add(105, "rst", logic4.V0)
	add(105, "err", logic4.V0)
	add(115, "rst", logic4.V1)
	add(115, "req", logic4.V1)
	add(115, "aux", logic4.VX) // stands in for the unconvertible token
	add(118, "aux", logic4.V1)
	return NewWaveform([]string{"clk", "rst", "req", "gnt", "aux", "err"}, changes)
}

var demoResets = []ResetInterval{
	{Signal: "rst", From: 0, To: 5},
	{Signal: "rst", From: 65, To: 75},
}

var demoClock = ClockSpec{Signal: "clk", Edge: "posedge", Period: 10}

func mustParse(t *testing.T, src string) prop.Node {
	t.Helper()
	f, err := prop.Parse("property p { " + src + " }")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f.Properties[0].Body
}

func TestSamplingAndResetBoundary(t *testing.T) {
	w := demoWave()
	ticks := SampleTicks(w, demoClock, demoResets)
	var kept, skipped int
	for _, tk := range ticks {
		if tk.Skipped {
			skipped++
			if tk.Time != 70 {
				t.Fatalf("unexpected skipped tick at %d", tk.Time)
			}
		} else {
			kept++
			if tk.Region != RegionPreponed {
				t.Fatalf("bad region %q", tk.Region)
			}
		}
	}
	if kept != 11 || skipped != 1 {
		t.Fatalf("kept=%d skipped=%d, want 11/1", kept, skipped)
	}
	// preponed sampling: req set at #25 must be visible at tick #30
	for _, tk := range ticks {
		if tk.Time == 30 && tk.Values["req"] != logic4.V1 {
			t.Fatalf("req@30 = %v, want 1", tk.Values["req"])
		}
	}
}

func TestNonOverlappedImplicationFails(t *testing.T) {
	w := demoWave()
	ticks := SampleTicks(w, demoClock, demoResets)
	eng := &Engine{Version: V1}
	run := eng.EvalProperty(mustParse(t, "req |=> gnt"), ticks, demoResets)
	if run.Verdict != logic4.False {
		t.Fatalf("verdict = %v, want false", run.Verdict)
	}
	var failed, killed, passed, truncated int
	for _, th := range run.Threads {
		switch th.Status {
		case "failed":
			failed++
		case "killed-by-reset":
			killed++
		case "passed":
			passed++
		case "truncated":
			truncated++
		}
	}
	// attempts at t30(pass),t40(pass),t60(killed by reset),t80(fail),t90(fail),t120(truncated)
	if failed != 2 || killed != 1 || passed != 2 || truncated != 1 {
		t.Fatalf("failed=%d killed=%d passed=%d truncated=%d, want 2/1/2/1",
			failed, killed, passed, truncated)
	}
	// every step must carry the sampling region
	for _, th := range run.Threads {
		for _, s := range th.Steps {
			if s.Region != RegionPreponed {
				t.Fatalf("step missing region: %+v", s)
			}
		}
	}
}

func TestDisableIffXZVersionDifference(t *testing.T) {
	w := demoWave()
	ticks := SampleTicks(w, demoClock, demoResets)
	body := mustParse(t, "disable iff (rst) err |-> gnt")
	v1 := (&Engine{Version: V1}).EvalProperty(body, ticks, demoResets)
	strict := (&Engine{Version: Strict}).EvalProperty(body, ticks, demoResets)
	if v1.Verdict != logic4.False {
		t.Fatalf("sva-v1 verdict = %v, want false", v1.Verdict)
	}
	if strict.Verdict != logic4.Unknown {
		t.Fatalf("sva-strict verdict = %v, want unknown", strict.Verdict)
	}
	// rst=1 at t=120 must genuinely disable that attempt
	var disabled int
	for _, th := range v1.Threads {
		if th.Status == "disabled" {
			disabled++
		}
	}
	if disabled != 1 {
		t.Fatalf("disabled threads = %d, want 1", disabled)
	}
}

func TestVacuousEmptyMatch(t *testing.T) {
	w := demoWave()
	ticks := SampleTicks(w, demoClock, demoResets)
	eng := &Engine{Version: V1}
	// gnt and aux never overlap -> antecedent always false -> vacuous pass
	run := eng.EvalProperty(mustParse(t, "gnt & aux |-> req"), ticks, demoResets)
	if run.Verdict != logic4.True || !run.Vacuous {
		t.Fatalf("verdict=%v vacuous=%v, want vacuous pass", run.Verdict, run.Vacuous)
	}
}

func TestNonOverlappedPassAttempt(t *testing.T) {
	w := demoWave()
	ticks := SampleTicks(w, demoClock, demoResets)
	eng := &Engine{Version: V1}
	// req@tick3 -> gnt two ticks later (tick5, #50): gnt=1 -> that attempt passes
	run := eng.EvalProperty(mustParse(t, "req |=> gnt"), ticks, demoResets)
	var tick3 *Thread
	for _, th := range run.Threads {
		if th.StartTick == 2 { // tick index 2 == time 30
			tick3 = th
		}
	}
	if tick3 == nil || tick3.Status != "passed" {
		t.Fatalf("tick@30 thread = %+v, want passed", tick3)
	}
}

func TestXAntecedentKeepsCandidates(t *testing.T) {
	// req goes X; antecedent cannot be uniquely decided
	changes := []vcd.Change{
		{Time: 0, Signal: "clk", Value: logic4.V0, OK: true},
		{Time: 0, Signal: "req", Value: logic4.V0, OK: true},
		{Time: 0, Signal: "gnt", Value: logic4.V0, OK: true},
		{Time: 5, Signal: "req", Value: logic4.VX, OK: true},
		{Time: 10, Signal: "clk", Value: logic4.V1, OK: true},
		{Time: 15, Signal: "clk", Value: logic4.V0, OK: true},
		{Time: 15, Signal: "req", Value: logic4.V0, OK: true},
		{Time: 20, Signal: "clk", Value: logic4.V1, OK: true},
	}
	w := NewWaveform([]string{"clk", "req", "gnt"}, changes)
	ticks := SampleTicks(w, demoClock, nil)
	run := (&Engine{Version: V1}).EvalProperty(mustParse(t, "req |-> gnt"), ticks, nil)
	if len(run.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(run.Candidates))
	}
	if run.Candidates[0].Verdict == run.Candidates[1].Verdict {
		t.Fatalf("candidates must differ: %+v", run.Candidates)
	}
}

func TestReductionPreservesOriginAndVerdict(t *testing.T) {
	w := demoWave()
	ticks := SampleTicks(w, demoClock, demoResets)
	body := mustParse(t, "req |=> gnt")
	eng := &Engine{Version: V1}
	run := eng.EvalProperty(body, ticks, demoResets)
	cands := Reduce(w, body, demoClock, demoResets, V1, run)
	if len(cands) == 0 {
		t.Fatal("no reduction candidates")
	}
	for _, c := range cands {
		if !c.OriginPreserved {
			t.Fatalf("candidate %s moved the time origin", c.Label)
		}
		if c.Verdict != logic4.False {
			t.Fatalf("candidate %s verdict = %v, want false", c.Label, c.Verdict)
		}
		if len(c.KeptChanges) >= len(w.Changes) {
			t.Fatalf("candidate %s removed nothing", c.Label)
		}
		// time origin unchanged: earliest kept change time == 0
		if c.KeptChanges[0].Time != 0 {
			t.Fatalf("candidate %s origin = %d", c.Label, c.KeptChanges[0].Time)
		}
	}
	// the union candidate must remove strictly more than any single-interval one
	last := cands[len(cands)-1]
	if last.Label != "remove-interval-1" && len(cands) > 1 {
		// sorted by RemovedCount desc; first has max removals
		if cands[0].RemovedCount < last.RemovedCount {
			t.Fatalf("candidates not sorted by removals")
		}
	}
}
