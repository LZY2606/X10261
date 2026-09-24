// Package replay replays sequence properties over sampled waveforms under
// a fixed sampling semantics. Two semantics versions are kept side by side
// (sva-v1 and sva-strict); they differ only in how an X/Z disable-iff
// condition is treated, which makes semantics-version differences
// observable and auditable.
package replay

import (
	"fmt"
	"sort"

	"sigcourt/internal/logic4"
	"sigcourt/internal/prop"
	"sigcourt/internal/vcd"
)

// Version selects the semantics used for a replay run.
type Version string

const (
	V1     Version = "sva-v1"     // X/Z disable-iff condition counts as "not disabled"
	Strict Version = "sva-strict" // X/Z disable-iff condition yields an explicit UNKNOWN
)

// RegionPreponed is the fixed sampling region used by every step.
const RegionPreponed = "Preponed"

// Waveform is the sampled view over raw value changes.
type Waveform struct {
	Signals []string
	Changes []vcd.Change // sorted by (Time, original order)
}

func NewWaveform(signals []string, changes []vcd.Change) *Waveform {
	sorted := append([]vcd.Change(nil), changes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time < sorted[j].Time })
	return &Waveform{Signals: signals, Changes: sorted}
}

// ValueBefore returns the preponed sample: the last change strictly
// before time t, or X when no change exists yet.
func (w *Waveform) ValueBefore(sig string, t int64) logic4.Value {
	v := logic4.VX
	for _, c := range w.Changes {
		if c.Time >= t {
			break
		}
		if c.Signal == sig {
			v = c.Value
		}
	}
	return v
}

// ClockSpec describes the sampling clock.
type ClockSpec struct {
	Signal string
	Edge   string // "posedge"
	Period int64
}

// ResetInterval is a declared reset region; ticks inside are skipped and
// in-flight threads crossing the boundary are killed.
type ResetInterval struct {
	Signal string
	From   int64
	To     int64
}

// Tick is one sampling point.
type Tick struct {
	Index   int // evaluation index among kept ticks, -1 when skipped
	Time    int64
	Region  string
	Skipped bool // inside a declared reset interval
	Values  map[string]logic4.Value
}

// SampleTicks finds posedges of the clock and samples every signal in the
// preponed region. Ticks inside a declared reset interval are kept in the
// list (marked Skipped) so the reset boundary stays visible.
func SampleTicks(w *Waveform, clk ClockSpec, resets []ResetInterval) []Tick {
	var times []int64
	last := logic4.VX
	for _, c := range w.Changes {
		if c.Signal != clk.Signal {
			continue
		}
		if last != logic4.V1 && c.Value == logic4.V1 {
			times = append(times, c.Time)
		}
		last = c.Value
	}
	var ticks []Tick
	idx := 0
	for _, t := range times {
		skipped := false
		for _, r := range resets {
			if t >= r.From && t < r.To {
				skipped = true
				break
			}
		}
		tk := Tick{Index: -1, Time: t, Region: RegionPreponed, Skipped: skipped, Values: map[string]logic4.Value{}}
		if !skipped {
			tk.Index = idx
			idx++
			for _, s := range w.Signals {
				tk.Values[s] = w.ValueBefore(s, t)
			}
		}
		ticks = append(ticks, tk)
	}
	return ticks
}

// Read records one sampled read for provenance and reduction footprints.
type Read struct {
	Signal string
	Tick   int
	Value  logic4.Value
}

// StepRec is one evaluation step inside a thread.
type StepRec struct {
	Tick   int
	Time   int64
	Region string
	Detail string
	Reads  []Read
}

// Thread is one attempt evaluation.
type Thread struct {
	StartTick int
	Status    string // passed|failed|vacuous|disabled|unknown|truncated|killed-by-reset
	Verdict   logic4.Tribool
	Vacuous   bool
	Disabled  bool
	Steps     []StepRec
}

// VerdictCandidate is an alternative outcome kept when the result cannot
// be uniquely decided (e.g. X/Z in the antecedent).
type VerdictCandidate struct {
	Label     string
	Verdict   logic4.Tribool
	Rationale string
}

// RunResult is the outcome of replaying one property.
type RunResult struct {
	Verdict    logic4.Tribool
	Vacuous    bool
	Disabled   bool
	Threads    []*Thread
	Candidates []VerdictCandidate
}

// Engine replays properties under one semantics version.
type Engine struct{ Version Version }

type outcome int

const (
	outOK outcome = iota
	outTruncated
	outDisabled
	outKilled
)

type evalState struct {
	ticks    []Tick // kept ticks only
	resets   []ResetInterval
	thread   *Thread
	run      *RunResult
	disables []prop.Node
	version  Version
}

func (st *evalState) step(tick int, detail string) {
	t := st.ticks[tick]
	st.thread.Steps = append(st.thread.Steps, StepRec{
		Tick: tick, Time: t.Time, Region: t.Region, Detail: detail,
	})
}

// resetCrossed reports whether a reset interval starts strictly between
// the times of kept ticks a and b.
func (st *evalState) resetCrossed(a, b int) bool {
	ta, tb := st.ticks[a].Time, st.ticks[b].Time
	for _, r := range st.resets {
		if r.From > ta && r.From < tb {
			return true
		}
	}
	return false
}

// checkDisable evaluates pending disable-iff conditions at tick u.
func (st *evalState) checkDisable(u int) outcome {
	for _, cond := range st.disables {
		v := st.evalExpr(cond, u)
		switch v {
		case logic4.True:
			st.step(u, fmt.Sprintf("disable iff (%s) = true -> attempt disabled", prop.String(cond)))
			st.thread.Disabled = true
			return outDisabled
		case logic4.Unknown:
			if st.version == Strict {
				st.step(u, fmt.Sprintf("disable iff (%s) = X/Z -> UNKNOWN (sva-strict)", prop.String(cond)))
				return outTruncated
			}
			st.step(u, fmt.Sprintf("disable iff (%s) = X/Z -> treated as not disabled (sva-v1)", prop.String(cond)))
		}
	}
	return outOK
}

// EvalProperty replays one property over the sampled ticks.
func (e *Engine) EvalProperty(body prop.Node, ticks []Tick, resets []ResetInterval) *RunResult {
	run := &RunResult{Verdict: logic4.True, Vacuous: true}
	var kept []Tick
	for _, t := range ticks {
		if !t.Skipped {
			kept = append(kept, t)
		}
	}
	for _, tk := range kept {
		th := &Thread{StartTick: tk.Index}
		st := &evalState{ticks: kept, resets: resets, thread: th, run: run, version: e.Version}
		v := st.evalNode(body, tk.Index)
		th.Verdict = v
		switch {
		case th.Status == "killed-by-reset":
			// contributes nothing
		case th.Disabled:
			th.Status = "disabled"
			run.Disabled = true
		case th.Status == "truncated":
			// keep status
		case v == logic4.False:
			th.Status = "failed"
			run.Vacuous = false
		case th.Vacuous:
			th.Status = "vacuous"
		case v == logic4.Unknown:
			th.Status = "unknown"
			run.Vacuous = false
		default:
			th.Status = "passed"
			run.Vacuous = false
		}
		run.Threads = append(run.Threads, th)
	}
	agg := logic4.True
	for _, th := range run.Threads {
		switch th.Status {
		case "failed":
			agg = logic4.False
		case "unknown", "truncated":
			if agg != logic4.False {
				agg = logic4.Unknown
			}
		}
	}
	run.Verdict = agg
	return run
}

func (st *evalState) evalNode(n prop.Node, t int) logic4.Tribool {
	switch node := n.(type) {
	case prop.Ident, prop.Not, prop.And, prop.Or:
		if oc := st.checkDisable(t); oc != outOK {
			return st.abortVerdict(oc)
		}
		v := st.evalExpr(n, t)
		st.step(t, fmt.Sprintf("expr %s = %s", prop.String(n), v))
		return v
	case *prop.Seq:
		_, m, oc := st.matchSeq(node, t)
		if oc != outOK {
			return st.abortVerdict(oc)
		}
		return m
	case prop.Implies:
		return st.evalImplies(node, t)
	case prop.DisableIff:
		st.disables = append(st.disables, node.Cond)
		v := st.evalNode(node.Body, t)
		st.disables = st.disables[:len(st.disables)-1]
		return v
	}
	return logic4.Unknown
}

func (st *evalState) abortVerdict(oc outcome) logic4.Tribool {
	switch oc {
	case outDisabled:
		return logic4.True
	case outKilled:
		st.thread.Status = "killed-by-reset"
		return logic4.Unknown
	default:
		st.thread.Status = "truncated"
		return logic4.Unknown
	}
}

func (st *evalState) evalImplies(node prop.Implies, t int) logic4.Tribool {
	if oc := st.checkDisable(t); oc != outOK {
		return st.abortVerdict(oc)
	}
	end, m, oc := st.matchSeq(node.Ante, t)
	if oc != outOK {
		return st.abortVerdict(oc)
	}
	if m == logic4.False {
		st.thread.Vacuous = true
		st.step(t, "antecedent false -> empty match (vacuous)")
		return logic4.True
	}
	start := end
	if node.Kind == prop.NonOverlapped {
		start = end + 1
	}
	op := "|->"
	if node.Kind == prop.NonOverlapped {
		op = "|=>"
	}
	if start >= len(st.ticks) {
		st.thread.Status = "truncated"
		st.step(len(st.ticks)-1, "consequent past end of trace -> truncated")
		return logic4.Unknown
	}
	if st.resetCrossed(t, start) {
		st.thread.Status = "killed-by-reset"
		st.step(t, "reset interval crossed before consequent -> thread killed")
		return logic4.Unknown
	}
	c := st.evalNode(node.Conseq, start)
	st.step(t, fmt.Sprintf("implication %s: ante=%s conseq=%s", op, m, c))
	if m == logic4.Unknown {
		// keep every legal candidate when the outcome is not unique
		st.run.Candidates = append(st.run.Candidates,
			VerdictCandidate{Label: "assume-antecedent-matched", Verdict: c,
				Rationale: "X/Z in antecedent: evaluate consequent"},
			VerdictCandidate{Label: "assume-antecedent-unmatched", Verdict: logic4.True,
				Rationale: "X/Z in antecedent: treat as empty match (vacuous)"},
		)
	}
	return logic4.Implies(m, c)
}

// matchSeq matches a linear sequence starting at tick t.
func (st *evalState) matchSeq(s *prop.Seq, t int) (int, logic4.Tribool, outcome) {
	cur := t
	if cur >= len(st.ticks) {
		return 0, logic4.Unknown, outTruncated
	}
	if oc := st.checkDisable(cur); oc != outOK {
		return cur, logic4.Unknown, oc
	}
	m := st.evalExpr(s.First, cur)
	st.step(cur, fmt.Sprintf("seq: %s = %s", prop.String(s.First), m))
	for _, step := range s.Rest {
		next := cur + step.Delay
		if next >= len(st.ticks) {
			return 0, logic4.Unknown, outTruncated
		}
		if st.resetCrossed(cur, next) {
			return next, logic4.Unknown, outKilled
		}
		if oc := st.checkDisable(next); oc != outOK {
			return next, logic4.Unknown, oc
		}
		v := st.evalExpr(step.Expr, next)
		st.step(next, fmt.Sprintf("seq ##%d: %s = %s", step.Delay, prop.String(step.Expr), v))
		m = logic4.And(m, v)
		cur = next
	}
	return cur, m, outOK
}

func (st *evalState) evalExpr(n prop.Node, t int) logic4.Tribool {
	tk := st.ticks[t]
	switch node := n.(type) {
	case prop.Ident:
		v := tk.Values[node.Name]
		st.thread.Steps = append(st.thread.Steps, StepRec{
			Tick: t, Time: tk.Time, Region: tk.Region,
			Detail: fmt.Sprintf("sample %s = %s", node.Name, v),
			Reads:  []Read{{Signal: node.Name, Tick: t, Value: v}},
		})
		return v.Truth()
	case prop.Not:
		return logic4.Not(st.evalExpr(node.X, t))
	case prop.And:
		return logic4.And(st.evalExpr(node.L, t), st.evalExpr(node.R, t))
	case prop.Or:
		return logic4.Or(st.evalExpr(node.L, t), st.evalExpr(node.R, t))
	}
	return logic4.Unknown
}
