// Package engine replays parsed gate-sequence properties over a clock
// domain under fixed sampling semantics. Every step records its sampling
// region; four-state values are projected to three-state truth with
// explicit Z/X accounting; attempts produce live threads whose identifiers
// are stable and content-derived.
package engine

import (
	"fmt"

	"signalcourt/internal/clock"
	"signalcourt/internal/fourstate"
	"signalcourt/internal/property"
)

// Verdict is the three-state property verdict plus the vacuity label.
type Verdict string

const (
	VPass    Verdict = "pass"
	VFail    Verdict = "fail"
	VUnknown Verdict = "unknown"
)

// Sample is one sampled four-state value at a tick.
type Sample struct {
	Tick       int            `json:"tick"`
	Signal     string         `json:"signal"`
	Value      string         `json:"value"`
	T3         string         `json:"t3"`
	CoercedZ   bool           `json:"coerced_z"`
	Known      bool           `json:"known"`
	Region     clock.Region   `json:"region"`
	ChangeTick int64          `json:"change_tick"`
	HasChange  bool           `json:"has_change"`
}

// Thread is one active or resolved sequence attempt.
type Thread struct {
	ID         string `json:"id"`
	StartTick  int    `json:"start_tick"`
	NodeID     string `json:"node_id"`
	State      string `json:"state"` // live/pass/fail/unknown/vacuous
	EndTick    int    `json:"end_tick"`
	MatchEmpty bool   `json:"match_empty"`
	Kind       string `json:"kind"` // ante/cons/seq
	Label      string `json:"label"`
}

// RuleEvent is a machine-readable rationale event.
type RuleEvent struct {
	Rule      string         `json:"rule"`
	Tick      int            `json:"tick"`
	T3        string         `json:"t3"`
	Vacuous   bool           `json:"vacuous"`
	Details   map[string]any `json:"details"`
	NodeID    string         `json:"node_id"`
	ThreadID  string         `json:"thread_id,omitempty"`
	Prop      string         `json:"prop"`
}

// TickResult is the per-clock-tick evaluation result for one property.
type TickResult struct {
	Tick       int         `json:"tick"`
	Region     clock.Region `json:"region"`
	Verdict    Verdict     `json:"verdict"`
	Vacuous    bool        `json:"vacuous"`
	Disabled   string      `json:"disabled"` // ""/"true"/"false"/"unknown"
	Threads    []Thread    `json:"threads"`
	RuleEvents []RuleEvent `json:"rule_events"`
	Samples    []Sample    `json:"samples"`
}

// Replay is the full replay of one property.
type Replay struct {
	PropName    string       `json:"prop_name"`
	Semver      string       `json:"semver"`
	Strength    string       `json:"strength"`
	Results     []TickResult `json:"results"`
	FailureTick int          `json:"failure_tick"`
	FirstPass   int          `json:"first_pass"`
	NodeID      string       `json:"node_id"`
}

// Engine evaluates one module over one domain.
type Engine struct {
	domain  *clock.Domain
	module  *property.Module
	semver  string
	sampled map[int]map[string]Sample
}

// New creates an engine for a module/domain under a semantic version.
func New(m *property.Module, d *clock.Domain, semver string) *Engine {
	return &Engine{domain: d, module: m, semver: semver, sampled: map[int]map[string]Sample{}}
}

func (e *Engine) sampleAt(tick int, name string) Sample {
	if m, ok := e.sampled[tick]; ok {
		if s, ok2 := m[name]; ok2 {
			return s
		}
	}
	v, has := e.domain.Sample(name, e.semver, tick)
	ct, hasCT := e.domain.ChangeTick(name, e.semver, tick)
	t3 := fourstate.ToT3(v)
	s := Sample{
		Tick: tick, Signal: name, Value: v.String(), T3: t3.String(),
		CoercedZ: v == fourstate.VZ, Known: has, Region: e.domain.RegionAt(tick, e.semver),
		ChangeTick: ct, HasChange: hasCT,
	}
	if e.sampled[tick] == nil {
		e.sampled[tick] = map[string]Sample{}
	}
	e.sampled[tick][name] = s
	return s
}

func (e *Engine) evalBool(expr *property.BoolExpr, tick int) (fourstate.T3, []RuleEvent) {
	var events []RuleEvent
	switch expr.Op {
	case property.BConst:
		switch expr.Const {
		case "1":
			return fourstate.TTrue, nil
		case "0":
			return fourstate.TFalse, nil
		default:
			return fourstate.TUnknown, []RuleEvent{{Rule: "xz_propagation", Tick: tick, T3: "unknown", NodeID: expr.NodeID, Details: map[string]any{"source": "literal", "raw": expr.Const}}}
		}
	case property.BVar:
		s := e.sampleAt(tick, expr.Name)
		ev := RuleEvent{Rule: "xz_propagation", Tick: tick, T3: s.T3, NodeID: expr.NodeID, Details: map[string]any{
			"signal": expr.Name, "v4": s.Value, "t3": s.T3, "coerced_z": s.CoercedZ,
			"region": s.Region, "known_signal": s.Known,
		}}
		return fourstate.ToT3(mustV4(s.Value)), append(events, ev)
	case property.BNot:
		a, evs := e.evalBool(expr.Args[0], tick)
		return fourstate.Not(a), append(events, evs...)
	case property.BAnd:
		a, e1 := e.evalBool(expr.Args[0], tick)
		b, e2 := e.evalBool(expr.Args[1], tick)
		return fourstate.And(a, b), append(events, append(e1, e2...)...)
	case property.BOr:
		a, e1 := e.evalBool(expr.Args[0], tick)
		b, e2 := e.evalBool(expr.Args[1], tick)
		return fourstate.Or(a, b), append(events, append(e1, e2...)...)
	}
	return fourstate.TUnknown, events
}

func mustV4(s string) fourstate.V4 {
	v, _ := fourstate.Parse(s)
	return v
}

// Run replays every declared property across all clock edges.
func (e *Engine) Run() ([]*Replay, error) {
	var out []*Replay
	for _, decl := range e.module.Decls {
		r, err := e.runProp(decl)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (e *Engine) runProp(decl *property.Decl) (*Replay, error) {
	r := &Replay{PropName: decl.Name, Semver: e.semver, FailureTick: -1, FirstPass: -1, NodeID: decl.NodeID}
	n := e.domain.NEdges()
	for tick := 0; tick < n; tick++ {
		tr := e.evaluateAt(decl, tick)
		r.Results = append(r.Results, tr)
		if tr.Verdict == VFail && r.FailureTick < 0 {
			r.FailureTick = tick
		}
		if tr.Verdict == VPass && r.FirstPass < 0 {
			r.FirstPass = tick
		}
	}
	r.Strength = strongest(decl.Property)
	return r, nil
}

func strongest(p *property.Property) string {
	switch p.Kind {
	case property.PropSeq:
		return p.Strength
	case property.PropDisable:
		return strongest(p.Inner)
	case property.PropImp:
		return strongest(p.Cons)
	}
	return ""
}

func errf(format string, a ...any) error { return fmt.Errorf(format, a...) }
