package replay

import (
	"strings"
	"testing"

	"signalcourt/internal/prop"
	"signalcourt/internal/vcd"
)

// clk rises at #10,#20,...; data assigned just before each rise so the
// sampling region (post-edge) reads the intended value.
const wave = `$timescale 1ns $end
$var wire 1 ! clk $end
$var wire 1 " rst $end
$var wire 1 # req $end
$var wire 1 $ gnt $end
$var wire 1 % xs $end
$var wire 1 & za $end
$enddefinitions $end
#0
0!
1"
0#
0$
1%
0&
#10
1!
#15
0!
#20
0"
1#
1!
#25
0!
#30
1$
z&
1!
#35
0!
#40
0#
x%
1&
1!
#45
0!
#50
0&
1!
#55
0!
#60
1!
#65
0!
#70
1!
#75
0!
#80
1#
1!
#85
0!
#90
0$
1!
#95
0!
`

func build(t *testing.T) (*vcd.File, []Tick) {
	t.Helper()
	f, err := vcd.Parse(strings.NewReader(wave))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Clock: "clk", Edge: "posedge", Reset: "rst", ResetLevel: "1"}
	ticks, err := BuildTicks(f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 9 {
		t.Fatalf("ticks=%d want 9", len(ticks))
	}
	return f, ticks
}

func props(t *testing.T, ver string) []*prop.Property {
	t.Helper()
	src := `semver = "` + ver + `"
property p_over: (req |-> ##[1:1] gnt)
property p_non: (req |=> ##[1:1] gnt)
property p_rst: disable iff (rst) (xs |-> ##[1:1] xs)
property p_x: (xs |-> ##[1:1] xs)
property p_z: (za |-> ##[1:1] gnt)
`
	pf, err := prop.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return pf.Properties
}

func kindsByStart(r *Result, name string) map[int]Kind {
	m := map[int]Kind{}
	for _, a := range r.Attempts {
		if a.Property == name {
			m[a.StartTick] = a.Kind
		}
	}
	return m
}

func TestOverlapAndThreads(t *testing.T) {
	_, ticks := build(t)
	r := Run(props(t, "1.0.0"), ticks, Config{Clock: "clk", Edge: "posedge", Reset: "rst", ResetLevel: "1", SemVer: "1.0.0"})
	over := kindsByStart(r, "p_over")
	// k1 req=1 -> gnt k2=1 pass; k7 req=1 -> gnt k8=0 fail;
	// k8 starts inside the last sampling region, window beyond trace -> pending.
	if over[1] != KPass || over[7] != KFail || over[0] != KVacuous || over[8] != KPending {
		t.Fatalf("overlap=%+v", over)
	}
	non := kindsByStart(r, "p_non")
	// non-overlap shifts one tick later: k1 req=1 -> gnt at k3 = 1 pass
	if non[1] != KPass {
		t.Fatalf("non-overlap k1 = %v want pass", non[1])
	}
	// overlapping and non-overlapping attempts from the same antecedent region
	// coexist as distinct threads until each resolves.
	threads := map[string]bool{}
	for _, a := range r.Attempts {
		if a.StartTick == 1 && (a.Property == "p_over" || a.Property == "p_non") {
			threads[a.Property] = true
		}
	}
	if len(threads) != 2 {
		t.Fatalf("expected one |-> and one |=> thread from k1, got %v", threads)
	}
}

func TestXZPropagationAndDisable(t *testing.T) {
	_, ticks := build(t)
	r := Run(props(t, "1.0.0"), ticks, Config{Clock: "clk", Edge: "posedge", Reset: "rst", ResetLevel: "1", SemVer: "1.0.0"})
	xk := kindsByStart(r, "p_x")
	// xs is X from k3 onward (raw token retained): k3 antecedent X -> unknown
	if xk[3] != KXPropagate {
		t.Fatalf("p_x k3 = %v want x_propagate", xk[3])
	}
	// k2 antecedent holds (1), consequent samples X at k3 -> unknown
	if xk[2] != KXPropagate {
		t.Fatalf("p_x k2 = %v want x_propagate on consequent", xk[2])
	}
	for _, a := range r.Attempts {
		if a.Property == "p_x" && a.Kind == KXPropagate && a.Verdict != UnknownTri {
			t.Fatal("X propagation must yield explicit unknown tri-state")
		}
	}
	dz := kindsByStart(r, "p_z")
	// za is Z at k2 -> antecedent Z -> x_propagate, raw Z token retained;
	// at k3 za=1 -> gnt k4=1 pass
	if dz[2] != KXPropagate {
		t.Fatalf("p_z k2 = %v want x_propagate (Z)", dz[2])
	}
	if dz[3] != KPass {
		t.Fatalf("p_z k3 = %v want pass", dz[3])
	}
	for _, a := range r.Attempts {
		if a.Property == "p_z" && a.StartTick == 2 && a.AnteRaw != "z" {
			t.Fatalf("Z raw token not retained on attempt: %q", a.AnteRaw)
		}
	}
	// p_rst at k0 inside reset -> disabled, explicit true tri-state
	d := kindsByStart(r, "p_rst")
	if d[0] != KDisabled {
		t.Fatalf("p_rst k0 = %v want disabled", d[0])
	}
	for _, a := range r.Attempts {
		if a.Property == "p_rst" && a.Kind == KDisabled && a.Verdict != TrueTri {
			t.Fatal("disable iff must be an explicit true tri-state")
		}
	}
	if len(r.Reset) != 1 || r.Reset[0].StartTick != 0 || r.Reset[0].EndTick != 0 {
		t.Fatalf("reset intervals = %+v", r.Reset)
	}
	// k0 is sampled while rst=1: disable iff must fire at the boundary tick
	d0 := kindsByStart(r, "p_rst")
	if d0[0] != KDisabled {
		t.Fatalf("reset boundary tick must be disabled, got %v", d0[0])
	}
}

func TestSemVerPendingDifference(t *testing.T) {
	_, ticks := build(t)
	// req=1 at k5: consequent window lies beyond the trace.
	cfg := Config{Clock: "clk", Edge: "posedge", Reset: "rst", ResetLevel: "1"}
	c1 := cfg; c1.SemVer = "1.0.0"
	c2 := cfg; c2.SemVer = "1.1.0"
	pf1, _ := prop.Parse(`semver = "1.0.0"
property late: (req |-> ##[1:3] gnt)
`)
	// last req=1 start is k2; instead craft start near end: req at k5? req is 0;
	// use gnt antecedent at k6: gnt=0 -> vacuous. Build a thread pending via a
	// property whose true antecedent is gnt=1 at k4 with window past the end.
	pf2, _ := prop.Parse(`semver = "1.0.0"
property late: (gnt |-> ##[2:3] gnt)
`)
	_ = pf1
	r1 := Run(pf2.Properties, ticks, c1)
	r2 := Run(pf2.Properties, ticks, c2)
	var a1, a2 *Attempt
	for i := range r1.Attempts {
		if r1.Attempts[i].Property == "late" && r1.Attempts[i].StartTick == 6 {
			a1 = r1.Attempts[i]
		}
	}
	for i := range r2.Attempts {
		if r2.Attempts[i].Property == "late" && r2.Attempts[i].StartTick == 6 {
			a2 = r2.Attempts[i]
		}
	}
	if a1 == nil || a2 == nil {
		t.Fatal("pending attempt missing")
	}
	if a1.Verdict != UnknownTri || a1.Kind != KPending {
		t.Fatalf("v1.0.0 pending = %s/%s", a1.Verdict, a1.Kind)
	}
	if a2.Verdict != TrueTri || a2.Kind != KPass {
		t.Fatalf("v1.1.0 pending = %s/%s", a2.Verdict, a2.Kind)
	}
}

func TestSupportTickProvenance(t *testing.T) {
	_, ticks := build(t)
	r := Run(props(t, "1.0.0"), ticks, Config{Clock: "clk", Edge: "posedge", Reset: "rst", ResetLevel: "1", SemVer: "1.0.0"})
	for _, a := range r.Attempts {
		if len(a.Support) == 0 || a.Support[0] != a.StartTick {
			t.Fatalf("attempt %s support must start at its own sampling tick", a.ID)
		}
	}
}
