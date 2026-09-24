// Package eval replays sequence properties over a waveform under fixed
// sampling semantics: values are sampled exactly at posedges of the
// property clock. Every step yields an explicit three-valued result;
// ambiguous situations keep all legal candidate matches.
package eval

import (
	"fmt"

	"pairwise-gsb/internal/logic"
	"pairwise-gsb/internal/prop"
	"pairwise-gsb/internal/vcd"
)

// Outcome of one attempt thread.
type Outcome string

const (
	Pass     Outcome = "pass"      // definite success
	Fail     Outcome = "fail"      // definite failure
	Unknown  Outcome = "unknown"   // X/Z or undecidable
	Vacuous  Outcome = "vacuous"   // antecedent never matched
	Disabled Outcome = "disabled"  // disable iff fired (vacuous pass)
)

// Read is one sampled four-state read, kept as provenance.
type Read struct {
	Signal string   `json:"signal"`
	Tick   int64    `json:"tick"`
	Value  logic.V  `json:"value"`
}

// ElemMatch records one sequence element matching at one sample.
type ElemMatch struct {
	Elem   int      `json:"elem"`
	Sample int      `json:"sample"`
	Tick   int64    `json:"tick"`
	Value  logic.T  `json:"value"`
	Reads  []Read   `json:"reads"`
}

// Match is one legal candidate match of a whole sequence.
type Match struct {
	Elems     []ElemMatch `json:"elems"`
	Empty     bool        `json:"empty"`
	EmptyRes  logic.T     `json:"empty_res"` // explicit three-valued empty-match result
	EndSample int         `json:"end_sample"`
}

// Thread is one attempt started at one sampling point.
type Thread struct {
	ID          string    `json:"id"`
	StartSample int       `json:"start_sample"`
	StartTick   int64     `json:"start_tick"`
	Outcome     Outcome   `json:"outcome"`
	Antecedent  []Match   `json:"antecedent"`  // all legal candidate matches
	Consequent  []Match   `json:"consequent"`  // candidates for the decisive antecedent match
	Reason      string    `json:"reason"`
	Truncated   bool      `json:"truncated"`
}

// SampleRegion annotates one sampling step for display.
type SampleRegion struct {
	Index int   `json:"index"`
	Tick  int64 `json:"tick"`
}

// Result is the full replay record of one property under one semantics
// version against one waveform.
type Result struct {
	Property   string         `json:"property"`
	Semantics  string         `json:"semantics"`
	Outcome    Outcome        `json:"outcome"`
	Samples    []SampleRegion `json:"samples"`
	Threads    []Thread       `json:"threads"`
	ResetSpans []vcd.Interval `json:"reset_spans"`
}

const maxCandidates = 64

// Run replays property p over waveform w.
func Run(w *vcd.Waveform, p *prop.Property, semantics string) (*Result, error) {
	if semantics != prop.SemV10 && semantics != prop.SemV11 {
		return nil, fmt.Errorf("unsupported semantics version %q", semantics)
	}
	edges := w.Edges(p.Clock)
	res := &Result{Property: p.Name, Semantics: semantics, Outcome: Pass,
		ResetSpans: w.ResetIntervals(p.DisableIff)}
	for i, e := range edges {
		res.Samples = append(res.Samples, SampleRegion{Index: i, Tick: e.Tick})
	}
	sampler := &sampler{w: w, edges: edges}
	for i := range edges {
		th := runAttempt(sampler, p, semantics, i)
		res.Threads = append(res.Threads, th)
	}
	res.Outcome = aggregate(res.Threads)
	return res, nil
}

func aggregate(threads []Thread) Outcome {
	out := Pass
	for _, t := range threads {
		switch t.Outcome {
		case Fail:
			return Fail
		case Unknown:
			out = Unknown
		}
	}
	return out
}

type sampler struct {
	w     *vcd.Waveform
	edges []vcd.Edge
}

func (s *sampler) tick(sample int) int64 { return s.edges[sample].Tick }

func (s *sampler) evalBool(e *prop.BoolExpr, sample int) (logic.T, []Read) {
	switch e.Op {
	case "sig":
		v := s.w.ValueAt(e.Name, s.tick(sample))
		return logic.BoolOf(v), []Read{{Signal: e.Name, Tick: s.tick(sample), Value: v}}
	case "not":
		v, r := s.evalBool(e.Left, sample)
		return logic.Not(v), r
	case "and":
		lv, lr := s.evalBool(e.Left, sample)
		rv, rr := s.evalBool(e.Right, sample)
		return logic.And(lv, rv), append(lr, rr...)
	case "or":
		lv, lr := s.evalBool(e.Left, sample)
		rv, rr := s.evalBool(e.Right, sample)
		return logic.Or(lv, rv), append(lr, rr...)
	}
	return logic.Unknown, nil
}

// matchSeq enumerates legal candidate matches of seq starting at sample
// start. All candidates are preserved up to maxCandidates.
func (s *sampler) matchSeq(seq *prop.Sequence, start int, semantics string) ([]Match, bool) {
	if len(seq.Elems) == 0 {
		// Empty match: explicit three-valued result per semantics version.
		er := logic.True
		if semantics == prop.SemV11 {
			er = logic.Unknown
		}
		return []Match{{Empty: true, EmptyRes: er, EndSample: start}}, false
	}
	type partial struct {
		m   Match
		cur int // sample of last matched element
	}
	first := seq.Elems[0]
	if first.DelayMin != 0 || first.DelayMax != 0 {
		// First element delay is relative to the attempt start.
	}
	var matches []Match
	truncated := false
	queue := []partial{}
	v0, r0 := s.evalBool(first.Expr, start)
	if v0 != logic.False {
		queue = append(queue, partial{m: Match{Elems: []ElemMatch{{Elem: 0, Sample: start, Tick: s.tick(start), Value: v0, Reads: r0}}}, cur: start})
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		next := len(p.m.Elems)
		if next == len(seq.Elems) {
			p.m.EndSample = p.cur
			matches = append(matches, p.m)
			continue
		}
		el := seq.Elems[next]
		for d := el.DelayMin; d <= el.DelayMax; d++ {
			smp := p.cur + d
			if smp >= len(s.edges) {
				// Beyond the observation window: keep an explicit
				// unknown candidate rather than guessing.
				m := Match{Elems: append(append([]ElemMatch{}, p.m.Elems...), ElemMatch{Elem: next, Sample: -1, Tick: -1, Value: logic.Unknown})}
				m.EndSample = -1
				matches = append(matches, m)
				continue
			}
			v, r := s.evalBool(el.Expr, smp)
			if v == logic.False {
				continue
			}
			nm := Match{Elems: append(append([]ElemMatch{}, p.m.Elems...), ElemMatch{Elem: next, Sample: smp, Tick: s.tick(smp), Value: v, Reads: r})}
			queue = append(queue, partial{m: nm, cur: smp})
			if len(queue)+len(matches) > maxCandidates {
				truncated = true
				queue = queue[:0]
				break
			}
		}
	}
	return matches, truncated
}

func matchValue(m Match) logic.T {
	if m.Empty {
		return m.EmptyRes
	}
	v := logic.True
	for _, e := range m.Elems {
		v = logic.And(v, e.Value)
	}
	return v
}

func runAttempt(s *sampler, p *prop.Property, sem string, start int) Thread {
	th := Thread{ID: fmt.Sprintf("%s#%d", p.Name, start), StartSample: start,
		StartTick: s.tick(start), Outcome: Pass}
	disabled := func(from, to int) bool {
		if p.DisableIff == "" {
			return false
		}
		for smp := from; smp <= to && smp < len(s.edges); smp++ {
			if s.w.ValueAt(p.DisableIff, s.tick(smp)) == logic.V1 {
				return true
			}
		}
		return false
	}
	ant, trunc := s.matchSeq(&p.Antecedent, start, sem)
	th.Truncated = trunc
	var trueAnt []Match
	unknownAnt := false
	maxEnd := start
	for _, m := range ant {
		if m.EndSample > maxEnd {
			maxEnd = m.EndSample
		}
		switch matchValue(m) {
		case logic.True:
			trueAnt = append(trueAnt, m)
		case logic.Unknown:
			unknownAnt = true
		}
	}
	th.Antecedent = ant
	if len(trueAnt) == 0 {
		if disabled(start, maxEnd) {
			th.Outcome = Disabled
			th.Reason = "disable iff fired during antecedent window"
			return th
		}
		if unknownAnt {
			th.Outcome = Unknown
			th.Reason = "antecedent undecidable (X/Z or open window)"
		} else {
			th.Outcome = Vacuous
			th.Reason = "antecedent never matched"
		}
		return th
	}
	// Evaluate consequent for every true antecedent candidate; the worst
	// result decides, but all candidates stay in the record.
	worst := logic.True
	for _, am := range trueAnt {
		cs := am.EndSample
		if !p.Overlapped {
			cs++
		}
		if cs >= len(s.edges) {
			worst = logic.And(worst, logic.Unknown)
			th.Reason = "consequent window extends past end of waveform"
			continue
		}
		cons, trunc2 := s.matchSeq(&p.Consequent, cs, sem)
		th.Truncated = th.Truncated || trunc2
		th.Consequent = append(th.Consequent, cons...)
		end := cs
		for _, cm := range cons {
			if cm.EndSample > end {
				end = cm.EndSample
			}
		}
		if disabled(start, end) {
			th.Outcome = Disabled
			th.Reason = "disable iff fired during attempt"
			return th
		}
		cv := logic.False
		seenUnknown := false
		for _, cm := range cons {
			switch matchValue(cm) {
			case logic.True:
				cv = logic.True
			case logic.Unknown:
				seenUnknown = true
			}
		}
		if cv != logic.True && seenUnknown {
			cv = logic.Unknown
		}
		worst = logic.And(worst, cv)
	}
	switch worst {
	case logic.True:
		th.Outcome = Pass
		th.Reason = "consequent satisfied"
	case logic.False:
		th.Outcome = Fail
		th.Reason = "consequent failed for all candidates"
	default:
		th.Outcome = Unknown
		if th.Reason == "" {
			th.Reason = "consequent undecidable"
		}
	}
	return th
}
