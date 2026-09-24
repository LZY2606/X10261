// Package replay replays properties under fixed preponed-region sampling
// semantics. Every evaluated rule yields an explicit three-valued result;
// X/Z contamination and vacuous (empty) matches are first-class outcomes.
package replay

import (
	"strconv"

	"gsb/internal/domain"
)

// Match is one automaton thread: a sequence attempt starting at sample
// Start and observed at End. Vacuous means the match came from an empty
// antecedent; Support lists the raw change IDs justifying it, so counterexample
// reduction may only remove intervals unrelated to the failure.
type Match struct {
	Node     *domain.Node
	Start    int
	End      int
	Result   domain.Tri
	Vacuous  bool
	Support  []string
	ThreadID string
	Note     string
}

// StepTrace is the per-sample view rendered by the review UI.
type StepTrace struct {
	Sample     domain.Sample        `json:"sample"`
	InReset    bool                 `json:"in_reset"`
	Active     []domain.ThreadState `json:"active"`
	RegionNote string               `json:"region_note"`
}

// Result is a full property replay outcome.
type Result struct {
	PropertyID  string
	SemVer      string
	Verdict     domain.Tri
	Vacuous     bool
	FailureAt   int
	OriginTick  int64
	Matches     []Match
	Steps       []StepTrace
	Candidates  []domain.Candidate
	Events      []TraceEvent
	Support     []string
	FailedCount int
	TrueCount   int
	VacCount    int
}

// TraceEvent is a machine-readable rule event produced during replay.
type TraceEvent struct {
	Kind     domain.EventKind
	Sample   int
	AtTick   int64
	Refs     []string
	Detail   map[string]string
	ThreadID string
}

// Engine holds a prepared replay over one branch's data.
type Engine struct {
	semVer    string
	sigs      []domain.Signal
	sigByName map[string]string
	props     []*domain.Property
	domains   map[string]*Sampled
	events    []TraceEvent
}

// NewEngine prepares sampling tables for every clock domain referenced.
func NewEngine(semVer string, sigs []domain.Signal, changes []domain.Change, ts domain.Timescale, domains []domain.ClockDomain) *Engine {
	e := &Engine{semVer: semVer, sigs: sigs, sigByName: map[string]string{}, domains: map[string]*Sampled{}}
	for _, sg := range sigs {
		e.sigByName[sg.Name] = sg.ID
	}
	for _, cd := range domains {
		e.domains[cd.Name] = BuildSampled(cd, sigs, changes, ts)
	}
	return e
}

type liveThread struct {
	start int
	root  *domain.Node
	last  []Match
}

// Run replays one property and returns its complete verdict record.
func (e *Engine) Run(pr *domain.Property) *Result {
	s, ok := e.domains[pr.ClockDomain]
	if !ok {
		return &Result{PropertyID: pr.ID, SemVer: e.semVer, Verdict: domain.TriUnknown,
			Events: []TraceEvent{{Kind: domain.EvDiagnostic, Detail: map[string]string{"message": "clock domain not found"}}}}
	}
	res := &Result{PropertyID: pr.ID, SemVer: e.semVer}
	disabled := make([]bool, len(s.Samples))
	for i := range s.Samples {
		disabled[i] = e.isDisabled(pr, s, i, res)
		if s.InReset(i) {
			e.emit(res, domain.EvResetBoundary, i, s.Samples[i].AtTick, nil, map[string]string{
				"signal": s.Domain.ResetSignal, "level": strconv.Itoa(int(s.Domain.ResetLevel)),
				"boundary": "sample_inside_reset_window",
			})
		}
	}

	// One automaton thread is allowed to start at every sample; threads from
	// earlier antecedent firings stay live concurrently.
	var threads []*liveThread
	for i := range s.Samples {
		lt := &liveThread{start: i, root: pr.AST}
		threads = append(threads, lt)

		var still []*liveThread
		var states []domain.ThreadState
		for _, t := range threads {
			matches := e.evalNode(t.root, s, i, t.start, res, disabled)
			t.last = matches
			finished := true
			for _, m := range matches {
				stID := domain.StableID("th", pr.ID, strconv.Itoa(t.start), strconv.Itoa(i), m.Node.Kind, strconv.Itoa(int(m.Result)))
				m.ThreadID = stID
				st := domain.ThreadState{
					ID: stID, Property: pr.Name, StartedAt: t.start, Sample: i,
					NodePath: pathOf(m.Node), Result: m.Result, Note: m.Note,
				}
				switch {
				case m.Result == domain.TriTrue && (m.Vacuous || m.Note != "" && noteVacuous(m.Note)):
					st.Status = "empty"
					st.BranchOf = ""
				case m.Result == domain.TriTrue && m.Vacuous:
					st.Status = "empty"
				case m.Result == domain.TriTrue:
					st.Status = "matched"
				case m.Result == domain.TriFalse:
					st.Status = "failed"
				default:
					st.Status = "active"
				}
				if st.Status == "active" || st.Status == "failed" || st.Status == "matched" || st.Status == "empty" {
					states = append(states, st)
				}
				res.Matches = append(res.Matches, m)
				if st.Status == "active" {
					finished = false
				}
			}
			if !finished || i == t.start {
				still = append(still, t)
			}
		}
		if n := countNonActive(states); n >= 2 {
			e.emit(res, domain.EvMultithread, i, s.Samples[i].AtTick, nil, map[string]string{
				"coexisting": strconv.Itoa(n),
			})
		}
		threads = still

		res.Steps = append(res.Steps, StepTrace{
			Sample: s.Samples[i], InReset: s.InReset(i) || disabled[i], Active: states,
			RegionNote: "preponed: last change strictly before tick " + strconv.FormatInt(s.Samples[i].AtTick, 10),
		})
	}

	e.summarize(pr, s, res)
	return res
}

func noteVacuous(note string) bool {
	return len(note) >= 8 && note[:8] == "vacuous:"
}

func countNonActive(states []domain.ThreadState) int {
	n := 0
	for _, st := range states {
		if st.Status == "matched" || st.Status == "failed" || st.Status == "empty" {
			n++
		}
	}
	return n
}

func (e *Engine) summarize(pr *domain.Property, s *Sampled, res *Result) {
	// Property verdict over all attempt threads:
	//   any failed substantive implication -> false (counterexample)
	//   all ended threads vacuous           -> vacuous true (empty match)
	//   any substantive true, no failure    -> true
	//   otherwise                            -> unknown (X/Z / pending)
	seenCand := map[string]bool{}
	var support []string
	for _, m := range res.Matches {
		support = mergeStrings(support, m.Support)
		switch {
		case m.Result == domain.TriFalse && !m.Vacuous:
			res.FailedCount++
		case m.Result == domain.TriTrue && m.Vacuous:
			res.VacCount++
		case m.Result == domain.TriTrue:
			res.TrueCount++
		}
		key := m.Result.String() + "/" + strconv.FormatBool(m.Vacuous)
		if !seenCand[key] {
			seenCand[key] = true
			basis := domain.EvReplay.String()
			res.Candidates = append(res.Candidates, domain.Candidate{
				ID:     domain.StableID("cand", pr.ID, key, e.semVer),
				Result: m.Result, Reason: "thread outcome " + key, BasisEvent: basis,
			})
		}
	}
	res.Support = support

	switch {
	case res.FailedCount > 0:
		res.Verdict = domain.TriFalse
		for i := range res.Steps {
			for _, st := range res.Steps[i].Active {
				if st.Status == "failed" {
					res.FailureAt = i
					res.OriginTick = s.Samples[i].AtTick
					return
				}
			}
		}
	case res.TrueCount > 0:
		res.Verdict = domain.TriTrue
	case res.VacCount > 0:
		res.Verdict = domain.TriTrue
		res.Vacuous = true
		e.emit(res, domain.EvEmptyMatch, len(s.Samples)-1, lastTick(s), nil, map[string]string{
			"scope": "property", "property": pr.Name,
		})
	default:
		res.Verdict = domain.TriUnknown
	}
}

func lastTick(s *Sampled) int64 {
	if len(s.Samples) == 0 {
		return 0
	}
	return s.Samples[len(s.Samples)-1].AtTick
}

func (e *Engine) emit(res *Result, kind domain.EventKind, sample int, tick int64, refs []string, detail map[string]string) TraceEvent {
	ev := TraceEvent{Kind: kind, Sample: sample, AtTick: tick, Refs: refs, Detail: detail}
	e.events = append(e.events, ev)
	res.Events = append(res.Events, ev)
	return ev
}

func (e *Engine) isDisabled(pr *domain.Property, s *Sampled, i int, res *Result) bool {
	if pr.DisableSignal == "" {
		return false
	}
	id, ok := e.sigByName[pr.DisableSignal]
	if !ok {
		return false
	}
	v := s.Values[id][i]
	if v == domain.LX || v == domain.LZ {
		e.emit(res, domain.EvXZPropagate, i, s.Samples[i].AtTick, nil, map[string]string{
			"where": "disable iff", "value": v.String(), "semver": e.semVer,
		})
		if e.semVer == "1.1" {
			// v1.1: unknown disable condition makes the sampled step unknown.
			return false
		}
	}
	return v == pr.DisableLevel
}

func pathOf(n *domain.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case domain.ASTBool:
		return n.Name
	case domain.ASTNot:
		return "!(" + pathOf(n.Children[0]) + ")"
	case domain.ASTAnd, domain.ASTOr:
		return pathOf(n.Children[0]) + n.Kind + pathOf(n.Children[1])
	case domain.ASTSeq:
		return pathOf(n.Children[0]) + "##" + strconv.Itoa(n.Cycle) + pathOf(n.Children[1])
	case domain.ASTOverlap:
		return pathOf(n.Children[0]) + "|->" + pathOf(n.Children[1])
	case domain.ASTNoOverlap:
		return pathOf(n.Children[0]) + "|=>" + pathOf(n.Children[1])
	}
	return n.Kind
}

// SampleTicks returns the ordered sampling tick for each clock domain, used by
// reduction to attribute changes to sampling regions.
func (e *Engine) SampleTicks() map[string][]int64 {
	out := map[string][]int64{}
	for name, s := range e.domains {
		ticks := make([]int64, len(s.Samples))
		for i, smp := range s.Samples {
			ticks[i] = smp.AtTick
		}
		out[name] = ticks
	}
	return out
}

// Domains exposes prepared sampling tables for the UI renderer.
func (e *Engine) Domains() map[string]*Sampled { return e.domains }
