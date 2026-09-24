package replay

import (
	"fmt"

	"signalcourt/internal/fourval"
	"signalcourt/internal/property"
)

// Verdict is the explicit five-way result. The required three-state answer
// (true/false/unknown) is recoverable via Tri(): disabled is reported as
// unknown with reason "disabled", vacuous stays a true-with-vacuity flag so
// vacuity is never confused with failure.
type Verdict string

const (
	VerdictPass     Verdict = "pass"
	VerdictFail     Verdict = "fail"
	VerdictUnknown  Verdict = "unknown"
	VerdictVacuous  Verdict = "vacuous" // held but with zero matched attempts
	VerdictDisabled Verdict = "disabled"
)

// Tri maps to the mandated three-state result.
func (v Verdict) Tri() fourval.Tribool {
	switch v {
	case VerdictPass, VerdictVacuous:
		return fourval.True()
	case VerdictFail:
		return fourval.False()
	default:
		if v == VerdictDisabled {
			return fourval.Unknown("disabled")
		}
		return fourval.Unknown("undecided")
	}
}

// BasisEvent is a machine-readable judgement basis item.
type BasisEvent struct {
	Rule     string         `json:"rule"` // stable rule id
	Tri      string         `json:"tri"`  // true|false|unknown
	Verdict  string         `json:"verdict"`
	Tick     *int           `json:"tick,omitempty"`
	EndTick  *int           `json:"end_tick,omitempty"`
	Detail   string         `json:"detail,omitempty"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

// AttemptResult is one attempt started on a clock edge.
type AttemptResult struct {
	StartTick int          `json:"start_tick"`
	EndTick   int          `json:"end_tick"`
	Verdict   Verdict      `json:"verdict"`
	Vacuous   bool         `json:"vacuous"`
	Disabled  bool         `json:"disabled"`
	Unknown   bool         `json:"unknown"`
	Reason    string       `json:"reason,omitempty"`
	Basis     []BasisEvent `json:"basis"`
	Threads   []snapRec    `json:"threads,omitempty"`
	// FailureWindow is [lo,hi] tick range of the shortest failing alignment;
	// it is the seed for counterexample reduction.
	WindowLo int `json:"window_lo,omitempty"`
	WindowHi int `json:"window_hi,omitempty"`
}

// Replayer executes a property over a grid.
type Replayer struct {
	smp *Sampler
	sem property.SemParams
	cd  ClockDomain
	g   *Grid
}

// NewReplayer wires sampler, semantics and clock domain.
func NewReplayer(smp *Sampler, g *Grid, cd ClockDomain, sem property.SemParams) *Replayer {
	return &Replayer{smp: smp, sem: sem, cd: cd, g: g}
}

// Run evaluates a property, starting one attempt at every clock edge of the
// grid. Multi-threading is explicit: ranged forks coexist and are reported.
func (r *Replayer) Run(p property.Prop) []AttemptResult {
	n := len(r.g.Ticks)
	results := make([]AttemptResult, 0, n)
	for start := 0; start < n; start++ {
		ar := r.evalProp(p, start, n-1)
		ar.StartTick = start
		results = append(results, ar)
	}
	return results
}

func (r *Replayer) evalProp(p property.Prop, start, end int) AttemptResult {
	switch t := p.(type) {
	case *property.DisableProp:
		return r.evalDisable(t, start, end)
	case *property.SeqProp:
		return r.evalPlain(t.Seq, start, end)
	case *property.ImplyProp:
		return r.evalImply(t, start, end)
	}
	return AttemptResult{Verdict: VerdictUnknown, Reason: "unreachable"}
}

func (r *Replayer) evalDisable(d *property.DisableProp, start, end int) AttemptResult {
	cond := r.smp.EvalExpr(d.Cond, start)
	disabled := false
	switch cond.Value {
	case "true":
		disabled = true
	case "unknown":
		// v1: unknown disable condition is an explicit unknown, never a
		// silent pass. v2 parameter revision: unknown condition disables.
		if r.sem.DisableXDisables {
			disabled = true
		}
		ar := r.evalProp(d.Body, start, end)
		tick := start
		if !disabled {
			ar.Verdict = VerdictUnknown
			ar.Unknown = true
			ar.Reason = "disable-iff cond " + cond.Reason
			ar.Basis = append(ar.Basis, BasisEvent{
				Rule: "disable-iff-x", Tri: "unknown", Verdict: string(VerdictUnknown),
				Tick: &tick, Detail: "reset/disable condition reads " + cond.Reason +
					"; sem " + r.sem.Version + " keeps the attempt undecided",
				Evidence: map[string]any{
					"condition":      property.Print(d.Cond),
					"sampled_region": r.g.Ticks[start].SampledRegion,
					"sem_version":    r.sem.Version,
				},
			})
			return ar
		}
	}
	if disabled {
		ar := r.evalProp(d.Body, start, end)
		tick := start
		ar.Verdict = VerdictDisabled
		ar.Disabled = true
		ar.Basis = append(ar.Basis, BasisEvent{
			Rule: "disable-iff", Tri: "unknown", Verdict: string(VerdictDisabled),
			Tick: &tick,
			Detail: fmt.Sprintf("attempt disabled by (%s); sem %s disable_x_disables=%v",
				property.Print(d.Cond), r.sem.Version, r.sem.DisableXDisables),
			Evidence: map[string]any{"sem_version": r.sem.Version},
		})
		return ar
	}
	return r.evalProp(d.Body, start, end)
}

func (r *Replayer) evalPlain(s property.Seq, start, end int) AttemptResult {
	c := compileSeq(s)
	res := r.runSequence(c, start, end)
	ar := AttemptResult{Threads: finalThreads(res, end)}
	// Discrete end tick: earliest known match, else trace end.
	if len(res.matches) > 0 {
		m := res.matches[0]
		for _, mm := range res.matches {
			if mm.endTick < m.endTick {
				m = mm
			}
		}
		ar.EndTick = m.endTick
		ar.Basis = append(ar.Basis, matchBases(res, start)...)
		if m.vacuous {
			ar.Verdict = VerdictVacuous
			ar.Vacuous = true
			ar.Basis = append(ar.Basis, BasisEvent{
				Rule: "empty-match", Tri: "true", Verdict: string(VerdictVacuous),
				Tick: &start, EndTick: &m.endTick,
				Detail: "assertion matched the explicit empty sequence; vacuous success",
			})
			return ar
		}
		ar.Verdict = VerdictPass
		return ar
	}
	ar.EndTick = end
	t := start
	if res.unknown {
		ar.Verdict = VerdictUnknown
		ar.Unknown = true
		ar.Reason = "x/z element on every alignment"
		ar.Basis = append(ar.Basis, BasisEvent{
			Rule: "xz-propagation", Tri: "unknown", Verdict: string(VerdictUnknown),
			Tick: &t, Detail: "sequence alignment blocked by four-state unknown",
		})
		return ar
	}
	ar.Verdict = VerdictFail
	ar.WindowLo, ar.WindowHi = start, end
	ar.Basis = append(ar.Basis, BasisEvent{
		Rule: "seq-no-match", Tri: "false", Verdict: string(VerdictFail),
		Tick: &t, EndTick: &end,
		Detail: "all alignments died on known-false elements",
	})
	return ar
}

func (r *Replayer) evalImply(ip *property.ImplyProp, start, end int) AttemptResult {
	antC := compileSeq(ip.Antecedent)
	ant := r.runSequence(antC, start, end)
	ar := AttemptResult{}
	if len(ant.matches) == 0 {
		ar.EndTick = end
		t := start
		if ant.unknown {
			ar.Verdict = VerdictUnknown
			ar.Unknown = true
			ar.Reason = "antecedent x/z"
			ar.Basis = append(ar.Basis, BasisEvent{
				Rule: "xz-propagation", Tri: "unknown", Verdict: string(VerdictUnknown),
				Tick: &t, Detail: "antecedent alignment blocked by x/z",
			})
			ar.Threads = finalThreads(ant, end)
			return ar
		}
		// Antecedent never matched: vacuous success, explicit tri=true.
		ar.Verdict = VerdictVacuous
		ar.Vacuous = true
		ar.Basis = append(ar.Basis, BasisEvent{
			Rule: "empty-match", Tri: "true", Verdict: string(VerdictVacuous),
			Tick: &t, EndTick: &end,
			Detail: "no antecedent match in window; implication is vacuously true",
		})
		ar.Threads = finalThreads(ant, end)
		return ar
	}
	// One consequent check per antecedent match (multi-thread coexistence).
	consC := compileSeq(ip.Consequent)
	gap := 0
	if ip.Op == "|=>" {
		gap = r.sem.NonOverlapGap
	}
	var passes, fails, unknowns []matchRec
	var worst AttemptResult
	worst.WindowLo, worst.WindowHi = start, end
	for _, m := range ant.matches {
		cs := m.endTick + gap
		if m.vacuous {
			// Empty antecedent: overlapped starts at same tick; non-overlap
			// starts one tick later. Vacuity of the antecedent itself is
			// reported as an explicit basis rule.
			st := m.endTick
			ar.Basis = append(ar.Basis, BasisEvent{
				Rule: "empty-antecedent", Tri: "true", Verdict: string(VerdictVacuous),
				Tick: &st, Detail: "antecedent is the empty sequence at tick " +
					fmt.Sprint(st),
			})
		}
		cr := r.runSequence(consC, cs, end)
		switch {
		case len(cr.matches) > 0:
			passes = append(passes, cr.matches...)
		case cr.unknown:
			unknowns = append(unknowns, m)
		default:
			fails = append(fails, m)
		}
		ar.Threads = append(ar.Threads, finalThreads(cr, end)...)
	}
	ar.Threads = append(finalThreads(ant, end), ar.Threads...)
	t := start
	rule := "overlapped-implication"
	if ip.Op == "|=>" {
		rule = "non-overlapped-implication"
	}
	switch {
	case len(fails) > 0:
		f := fails[0]
		ar.Verdict = VerdictFail
		ar.EndTick = f.endTick
		ar.WindowLo, ar.WindowHi = start, f.endTick
		ar.Basis = append(ar.Basis, BasisEvent{
			Rule: rule, Tri: "false", Verdict: string(VerdictFail),
			Tick: &t, EndTick: &f.endTick,
			Detail: fmt.Sprintf("antecedent matched at %d but consequent did not (gap=%d)",
				f.endTick, gap),
			Evidence: map[string]any{"op": ip.Op, "gap_ticks": gap},
		})
	case len(unknowns) > 0:
		ar.Verdict = VerdictUnknown
		ar.Unknown = true
		ar.Reason = "consequent x/z"
		ar.EndTick = end
		ar.Basis = append(ar.Basis, BasisEvent{
			Rule: rule, Tri: "unknown", Verdict: string(VerdictUnknown),
			Tick: &t, Detail: "consequent alignment blocked by x/z after a matched antecedent",
			Evidence: map[string]any{"op": ip.Op, "gap_ticks": gap},
		})
	default:
		m := passes[0]
		ar.Verdict = VerdictPass
		ar.EndTick = m.endTick
		ar.Basis = append(ar.Basis, BasisEvent{
			Rule: rule, Tri: "true", Verdict: string(VerdictPass),
			Tick: &t, EndTick: &m.endTick,
			Detail:   "antecedent and consequent both matched",
			Evidence: map[string]any{"op": ip.Op, "gap_ticks": gap},
		})
	}
	return ar
}

func matchBases(res seqResult, start int) []BasisEvent {
	var out []BasisEvent
	for _, m := range res.matches {
		st, en := start, m.endTick
		rule := "seq-match"
		if m.vacuous {
			rule = "empty-match"
		}
		out = append(out, BasisEvent{
			Rule: rule, Tri: "true", Verdict: string(VerdictPass),
			Tick: &st, EndTick: &en,
			Evidence: map[string]any{"path_offsets": m.path},
		})
	}
	return out
}

func finalThreads(res seqResult, end int) []snapRec {
	if len(res.threads) > 0 {
		return res.threads
	}
	return nil
}
