// Package replay replays parsed properties over a sampled waveform under a
// fixed sampling semantics: every Boolean reads the value held immediately
// after the active clock edge. Four-state values propagate; Z is a distinct
// provenance state that collapses to X inside Boolean operators.
package replay

import (
	"fmt"
	"sort"
	"time"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/prop"
	"signalcourt/internal/vcd"
)

// Tri is the three-valued verdict required for every judgment:
// true / false / unknown (X or Z propagation, pending, disabled).
type Tri string

const (
	TrueTri    Tri = "true"
	FalseTri   Tri = "false"
	UnknownTri Tri = "unknown"
)

// Kind refines a tri-state into the explicit outcomes an auditor needs.
type Kind string

const (
	KPass       Kind = "pass"        // property held at this attempt
	KFail       Kind = "fail"        // property violated
	KVacuous    Kind = "vacuous"     // empty match: antecedent false
	KXPropagate Kind = "x_propagate" // X/Z reached the judgment
	KDisabled   Kind = "disabled"    // disable iff active
	KPending    Kind = "pending"     // unresolved at end of trace
)

// Config is the sampling configuration (parameters are versioned events).
type Config struct {
	Clock      string `json:"clock"`
	Edge       string `json:"edge"` // "posedge" | "negedge"
	Reset      string `json:"reset"`
	ResetLevel string `json:"reset_level"` // "1" active high, "0" active low
	SemVer     string `json:"semver"`
}

// Tick is one sampling point: normalized time basis and held values.
type Tick struct {
	Index     int                        `json:"index"`
	Time      int64                      `json:"time"` // in normalized time units (picoseconds)
	TimeRaw   int64                      `json:"time_raw"`
	UnitRaw   string                     `json:"unit_raw"`
	Edge      string                     `json:"edge"`
	Values    map[string]fourstate.Value `json:"-"`
	RawTokens map[string]string          `json:"raw_tokens"`
}

// Attempt is one property instance started from an antecedent sampling region.
type Attempt struct {
	ID        string `json:"id"`
	Property  string `json:"property"`
	StartTick int    `json:"start_tick"`
	Thread    int    `json:"thread"`
	Verdict   Tri    `json:"verdict"`
	Kind      Kind   `json:"kind"`
	EndTick   int    `json:"end_tick"` // -1 while pending
	Support   []int  `json:"support"`  // sampling points the verdict reads
	Reason    string `json:"reason"`
	AnteRaw   string `json:"ante_raw"`
	ConsRaw   string `json:"cons_raw,omitempty"`
	SemVer    string `json:"semver"`
}

// ResetInterval marks ticks inside reset with the source interval basis.
type ResetInterval struct {
	StartTick int    `json:"start_tick"`
	EndTick   int    `json:"end_tick"`
	Basis     string `json:"basis"`
}

// Result is the full replay record for one property set.
type Result struct {
	SemVer       string              `json:"semver"`
	Config       Config              `json:"config"`
	Ticks        []Tick              `json:"ticks"`
	Attempts     []*Attempt          `json:"attempts"`
	Reset        []ResetInterval     `json:"reset_intervals"`
	PropertyDeps map[string][]string `json:"property_deps"`
}

// normalizeTime maps raw VCD integers onto a shared derived basis expressed in
// picoseconds. Nanosecond units divide cleanly; picosecond/femtosecond units
// use integer ratios. Raw integers are still retained on every tick.
func normalizeTime(raw int64, ts vcd.Timescale) int64 {
	const picoPerNano = int64(1000)
	if !ts.Convertible {
		return raw
	}
	if ts.Unit >= time.Nanosecond {
		return raw * int64(ts.Unit/time.Nanosecond) * picoPerNano
	}
	// picosecond and finer: express via the nanosecond ratio at runtime.
	nano := int64(time.Nanosecond)
	unit := int64(ts.Unit)
	if unit > 0 && unit < nano {
		return raw * nano / unit / 1000
	}
	return raw
}

// BuildTicks extracts sampling ticks at the configured clock edge.
// Normalized time is in picoseconds so different timescale precisions share
// one derived basis; the raw integers stay on every tick.
func BuildTicks(f *vcd.File, cfg Config) ([]Tick, error) {
	clk := f.SignalByName(cfg.Clock)
	if clk == nil {
		return nil, fmt.Errorf("clock signal %q not found", cfg.Clock)
	}
	want := fourstate.V1
	edgeName := "posedge"
	if cfg.Edge == "negedge" {
		want = fourstate.V0
		edgeName = "negedge"
	} else if cfg.Edge != "" && cfg.Edge != "posedge" {
		return nil, fmt.Errorf("unknown edge %q", cfg.Edge)
	}
	type edge struct{ t int64 }
	var edges []int64
	var prev fourstate.Value = fourstate.VX
	for _, ch := range clk.Changes {
		v := ch.Value
		if v == want && prev != want {
			edges = append(edges, ch.Time)
		}
		prev = v
	}
	ticks := make([]Tick, 0, len(edges))
	for i, t := range edges {
		tk := Tick{
			Index: i, Time: normalizeTime(t, f.Timescale), TimeRaw: t,
			UnitRaw: f.Timescale.Raw, Edge: edgeName,
			Values: map[string]fourstate.Value{}, RawTokens: map[string]string{},
		}
		for _, s := range f.Signals {
			v, hold, _ := s.ValueAt(t)
			tk.Values[s.Name] = v
			tk.RawTokens[s.Name] = hold.Raw
			if hold.Raw == "" {
				tk.RawTokens[s.Name] = v.String()
			}
		}
		ticks = append(ticks, tk)
	}
	return ticks, nil
}

// Run replays every property over the ticks.
func Run(props []*prop.Property, ticks []Tick, cfg Config) *Result {
	r := &Result{SemVer: cfg.SemVer, Config: cfg, Ticks: ticks, PropertyDeps: map[string][]string{}}
	active := map[string][]*Attempt{}
	threadSeq := map[string]int{}
	for _, p := range props {
		r.PropertyDeps[p.Name] = p.SignalNames()
	}
	for ti, tk := range ticks {
		for _, p := range props {
			name := p.Name
			// resolve threads that are inside their ##[lo:hi] window now
			var next []*Attempt
			for _, a := range active[name] {
				loTick := a.StartTick + loOffset(p)
				hiTick := a.StartTick + hiOffset(p)
				// disable iff is re-checked at every sampling region the
				// thread stays alive, including inside the match window.
				if dv := evalExpr(p.Disable, tk); dv == fourstate.V1 {
					finish(a, KDisabled, TrueTri, ti, a.Support, "disable iff active at tick", "")
					r.Attempts = append(r.Attempts, a)
					continue
				}
				if ti < loTick {
					next = append(next, a)
					continue
				}
				cv := evalExpr(p.Cons.End, tk)
				a.Support = append(a.Support, ti)
				a.ConsRaw = tk.RawTokens[firstName(p.Cons.End)]
				switch cv {
				case fourstate.V1:
					finish(a, KPass, TrueTri, ti, a.Support, "consequent matched inside range", "")
					r.Attempts = append(r.Attempts, a)
					continue
				case fourstate.V0:
					if ti >= hiTick {
						finish(a, KFail, FalseTri, ti, a.Support,
							fmt.Sprintf("consequent false through ##[%d:%d]", p.Cons.Delay.Lo, p.Cons.Delay.Hi), "")
						r.Attempts = append(r.Attempts, a)
						continue
					}
					next = append(next, a)
				default:
					// X/Z inside the window: if another tick could still make
					// it pass, stay alive; at window end verdict is unknown.
					if ti >= hiTick {
						finish(a, KXPropagate, UnknownTri, ti, a.Support, "consequent X/Z across whole window", "")
						r.Attempts = append(r.Attempts, a)
						continue
					}
					next = append(next, a)
				}
			}
			active[name] = next
			// start a new attempt at this sampling region
			threadSeq[name]++
			a := &Attempt{ID: fmt.Sprintf("att_%s_k%d_t%d", name, ti, threadSeq[name]),
				Property: name, StartTick: ti, Thread: threadSeq[name], EndTick: -1, SemVer: cfg.SemVer}
			a.Support = append(a.Support, ti)
			a.AnteRaw = tk.RawTokens[firstName(p.Ante)]
			if dv := evalExpr(p.Disable, tk); dv == fourstate.V1 {
				finish(a, KDisabled, TrueTri, ti, a.Support, "disabled before antecedent", "")
				r.Attempts = append(r.Attempts, a)
				continue
			}
			av := evalExpr(p.Ante, tk)
			switch av {
			case fourstate.V0:
				finish(a, KVacuous, TrueTri, ti, a.Support, "antecedent false: empty match", "")
				r.Attempts = append(r.Attempts, a)
				continue
			case fourstate.VX, fourstate.VZ:
				finish(a, KXPropagate, UnknownTri, ti, a.Support,
					fmt.Sprintf("antecedent %s: X/Z propagated", av), "")
				r.Attempts = append(r.Attempts, a)
				continue
			}
			// antecedent true: overlapping ##[0:0] resolves at once,
			// everything else becomes a live thread.
			if p.Overlap && p.Cons.Delay.Lo == 0 && p.Cons.Delay.Hi == 0 {
				judgeConsequent(p, a, tk, ti, cfg, r)
				r.Attempts = append(r.Attempts, a)
			} else {
				active[name] = append(active[name], a)
			}
		}
	}
	// at end of trace, still-active threads are pending; semantics diverge
	for _, p := range props {
		for _, a := range active[p.Name] {
			tri := UnknownTri
			kind := Kind("pending")
			reason := "unresolved at end of trace"
			if cfg.SemVer == "1.1.0" {
				tri = TrueTri
				kind = Kind("pass")
				reason = "v1.1.0: pending attempt at end of trace is non-vacuous-true"
			}
			finish(a, kind, tri, len(ticks)-1, a.Support, reason, "")
			r.Attempts = append(r.Attempts, a)
		}
	}
	sort.SliceStable(r.Attempts, func(i, j int) bool {
		if r.Attempts[i].StartTick != r.Attempts[j].StartTick {
			return r.Attempts[i].StartTick < r.Attempts[j].StartTick
		}
		return r.Attempts[i].ID < r.Attempts[j].ID
	})
	// reset intervals (provenance)
	if cfg.Reset != "" {
		r.Reset = resetIntervals(ticks, cfg)
	}
	return r
}

func loOffset(p *prop.Property) int {
	if p.Overlap {
		return p.Cons.Delay.Lo
	}
	return 1 + p.Cons.Delay.Lo
}

func hiOffset(p *prop.Property) int {
	if p.Overlap {
		return p.Cons.Delay.Hi
	}
	return 1 + p.Cons.Delay.Hi
}

func judgeConsequent(p *prop.Property, a *Attempt, tk Tick, ti int, cfg Config, r *Result) {
	a.Support = append(a.Support, ti)
	cv := evalExpr(p.Cons.End, tk)
	a.ConsRaw = tk.RawTokens[firstName(p.Cons.End)]
	switch cv {
	case fourstate.V1:
		finish(a, KPass, TrueTri, ti, a.Support, "consequent matched", "")
	case fourstate.V0:
		finish(a, KFail, FalseTri, ti, a.Support,
			fmt.Sprintf("consequent false at tick %d (range ##[%d:%d])", ti, p.Cons.Delay.Lo, p.Cons.Delay.Hi), "")
	default:
		finish(a, KXPropagate, UnknownTri, ti, a.Support, "consequent X/Z propagated", "")
	}
}

func finish(a *Attempt, kind Kind, tri Tri, end int, support []int, reason, cons string) {
	a.Kind = kind
	a.Verdict = tri
	a.EndTick = end
	a.Reason = reason
	if cons != "" {
		a.ConsRaw = cons
	}
	a.Support = append([]int(nil), support...)
}

// evalExpr evaluates an AST node against one sampling region.
func evalExpr(e *prop.Expr, tk Tick) fourstate.Value {
	if e == nil {
		// No disable iff present: the disable condition is inactive.
		return fourstate.V0
	}
	switch e.Kind {
	case prop.KConst:
		v, _ := fourstate.Parse(e.Name)
		return v
	case prop.KIdent:
		v, ok := tk.Values[e.Name]
		if !ok {
			return fourstate.VX
		}
		return v
	case prop.KNot:
		return fourstate.Not(evalExpr(e.Args[0], tk))
	case prop.KAnd:
		return fourstate.And(evalExpr(e.Args[0], tk), evalExpr(e.Args[1], tk))
	case prop.KOr:
		return fourstate.Or(evalExpr(e.Args[0], tk), evalExpr(e.Args[1], tk))
	}
	return fourstate.VX
}

func firstName(e *prop.Expr) string {
	var n string
	e.Walk(func(x *prop.Expr) {
		if n == "" && x.Kind == prop.KIdent {
			n = x.Name
		}
	})
	return n
}

func resetIntervals(ticks []Tick, cfg Config) []ResetInterval {
	activeHi := cfg.ResetLevel != "0"
	var out []ResetInterval
	start := -1
	in := false
	for _, tk := range ticks {
		v := tk.Values[cfg.Reset]
		on := (activeHi && v == fourstate.V1) || (!activeHi && v == fourstate.V0)
		switch {
		case on && !in:
			start, in = tk.Index, true
		case !on && in:
			out = append(out, ResetInterval{StartTick: start, EndTick: tk.Index - 1,
				Basis: fmt.Sprintf("%s %s over %s", cfg.Reset, cfg.ResetLevel, cfg.Edge)})
			in = false
		}
	}
	if in {
		out = append(out, ResetInterval{StartTick: start, EndTick: len(ticks) - 1,
			Basis: fmt.Sprintf("%s %s over %s", cfg.Reset, cfg.ResetLevel, cfg.Edge)})
	}
	return out
}
