package replay

import (
	"strconv"

	"gsb/internal/domain"
)

// evalNode evaluates a node as a *sequence starting at sample start*, observed
// at sample now. It returns zero or more threads: bounded nodes produce a
// single thread, while ranges/unbounded delays and disjunction keep every
// legal candidate alive concurrently.
func (e *Engine) evalNode(n *domain.Node, s *Sampled, now, start int, res *Result, disabled []bool) []Match {
	if disabled[now] {
		return []Match{{Node: n, Start: start, End: now, Result: domain.TriUnknown, Note: "disabled by disable iff / reset"}}
	}
	switch n.Kind {
	case domain.ASTBool:
		return []Match{e.evalBool(n, s, now, res)}
	case domain.ASTNot:
		inner := e.evalNode(n.Children[0], s, now, start, res, disabled)
		out := make([]Match, len(inner))
		for i, m := range inner {
			m.Node = n
			m.Result = negateTri(m.Result)
			out[i] = m
		}
		return out
	case domain.ASTAnd, domain.ASTOr:
		l := e.evalNode(n.Children[0], s, now, start, res, disabled)
		r := e.evalNode(n.Children[1], s, now, start, res, disabled)
		return combineBool(n.Kind, n, l, r)
	case domain.ASTSeq:
		return e.evalSeq(n, s, now, start, res, disabled)
	case domain.ASTOverlap, domain.ASTNoOverlap:
		return e.evalImpl(n, s, now, start, res, disabled)
	}
	return nil
}

func negateTri(t domain.Tri) domain.Tri {
	if t == domain.TriUnknown {
		return domain.TriUnknown
	}
	if t == domain.TriTrue {
		return domain.TriFalse
	}
	return domain.TriTrue
}

func combineBool(kind string, n *domain.Node, l, r []Match) []Match {
	// Same-sample conjunct/disjunct: pair index-wise (children are booleans
	// at this point), keeping cross products for candidate sets.
	var out []Match
	for _, a := range l {
		for _, b := range r {
			m := Match{Node: n, Start: a.Start, End: maxSample(a.End, b.End)}
			m.Support = mergeStrings(a.Support, b.Support)
			if kind == domain.ASTAnd {
				m.Result = triAnd(a.Result, b.Result)
			} else {
				m.Result = triOr(a.Result, b.Result)
			}
			m.Vacuous = a.Vacuous || b.Vacuous
			m.ThreadID = a.ThreadID
			out = append(out, m)
		}
	}
	return out
}

func triAnd(a, b domain.Tri) domain.Tri {
	if a == domain.TriFalse || b == domain.TriFalse {
		return domain.TriFalse
	}
	if a == domain.TriTrue && b == domain.TriTrue {
		return domain.TriTrue
	}
	return domain.TriUnknown
}

func triOr(a, b domain.Tri) domain.Tri {
	if a == domain.TriTrue || b == domain.TriTrue {
		return domain.TriTrue
	}
	if a == domain.TriFalse && b == domain.TriFalse {
		return domain.TriFalse
	}
	return domain.TriUnknown
}

func (e *Engine) evalBool(n *domain.Node, s *Sampled, now int, res *Result) Match {
	id := e.sigByName[n.Name]
	v, ok := s.Values[id]
	m := Match{Node: n, Start: now, End: now}
	if !ok {
		m.Result = domain.TriUnknown
		m.Note = "signal not found: " + n.Name
		return m
	}
	val := v[now]
	m.Result = domain.BoolToTri(val)
	// Provenance: link to the raw change record producing the sampled value.
	if ch := e.changeSupporting(s, id, s.Samples[now].AtTick); ch != nil {
		m.Support = []string{ch.ID}
	}
	if val == domain.LX || val == domain.LZ {
		e.emit(res, domain.EvXZPropagate, now, s.Samples[now].AtTick, m.Support, map[string]string{
			"signal": n.Name, "value": val.String(), "rule": "bool->tri",
		})
		m.Note = "X/Z propagated to unknown: " + n.Name
	}
	return m
}

func (e *Engine) changeSupporting(s *Sampled, sigID string, tick int64) *domain.Change {
	chgs := s.changeBySignal[sigID]
	var last *domain.Change
	for i := range chgs {
		if chgs[i].AtTick < tick {
			c := chgs[i]
			last = &c
		} else {
			break
		}
	}
	return last
}

// evalSeq handles a ##delay b. Fixed delay yields one thread; a range or
// unbounded delay keeps one thread per legal offset so all candidates survive.
func (e *Engine) evalSeq(n *domain.Node, s *Sampled, now, start int, res *Result, disabled []bool) []Match {
	lo := n.Cycle
	hi := n.Hi // -1 == unbounded
	target := start + lo
	if now < target {
		return []Match{{Node: n, Start: start, End: now, Result: domain.TriUnknown, Note: "awaiting delay"}}
	}
	var offsets []int
	if hi < 0 {
		if now >= target {
			offsets = []int{now - start}
		}
	} else {
		if now-start >= lo && now-start <= hi {
			offsets = []int{now - start}
		}
	}
	// Record the full legal candidate space at each observation point.
	var out []Match
	for _, off := range offsets {
		_ = off
		lm := e.evalNode(n.Children[0], s, start, start, res, disabled)
		rm := e.evalNode(n.Children[1], s, now, start, res, disabled)
		for _, a := range lm {
			for _, b := range rm {
				m := Match{
					Node: n, Start: start, End: now,
					Result:  triAnd(a.Result, b.Result),
					Support: mergeStrings(a.Support, b.Support),
					Vacuous: a.Vacuous || b.Vacuous,
					Note:    "delay " + strconv.Itoa(now-start),
				}
				out = append(out, m)
			}
		}
	}
	if len(out) == 0 {
		return []Match{{Node: n, Start: start, End: now, Result: domain.TriFalse, Note: "delay window missed"}}
	}
	return out
}

// evalImpl handles overlapped (|->) and non-overlapped (|=>) implications.
// Overlapped consequent starts in the same cycle; non-overlapped starts one
// cycle later. Empty antecedent matches make the implication vacuously true;
// that outcome is kept distinct from a substantive true.
func (e *Engine) evalImpl(n *domain.Node, s *Sampled, now, start int, res *Result, disabled []bool) []Match {
	ant := e.evalNode(n.Children[0], s, now, start, res, disabled)
	shift := 0
	if n.Kind == domain.ASTNoOverlap {
		shift = 1
	}
	var out []Match
	for _, a := range ant {
		if now < start+shift {
			out = append(out, Match{Node: n, Start: start, End: now, Result: domain.TriUnknown, Note: "awaiting non-overlap cycle", ThreadID: a.ThreadID})
			continue
		}
		cons := e.evalNode(n.Children[1], s, now, start+shift, res, disabled)
		for _, c := range cons {
			m := Match{Node: n, Start: start, End: now, ThreadID: a.ThreadID, Support: mergeStrings(a.Support, c.Support)}
			m.Result, m.Vacuous = imply(a, c)
			m.Note = implicationNote(n.Kind, a, c)
			if m.Vacuous {
				e.emit(res, domain.EvEmptyMatch, now, s.Samples[now].AtTick, m.Support, map[string]string{
					"operator": n.Kind, "antecedent": pathOf(n.Children[0]),
				})
			}
			out = append(out, m)
		}
	}
	return out
}

// imply is the explicit three-valued implication table:
//
//	ant false / empty(true) -> vacuous true (kept distinct)
//	ant unknown             -> unknown unless consequent independently false
//	ant true, cons true     -> true; ant true, cons false -> false; X -> unknown
func imply(ant, cons Match) (domain.Tri, bool) {
	switch ant.Result {
	case domain.TriFalse:
		return domain.TriTrue, true
	case domain.TriTrue:
		if ant.Vacuous || isEmptyNode(ant.Node) {
			return domain.TriTrue, true
		}
		return cons.Result, false
	default:
		if cons.Result == domain.TriFalse {
			return domain.TriUnknown, false
		}
		return domain.TriUnknown, false
	}
}

func isEmptyNode(n *domain.Node) bool {
	return n != nil && n.Empty
}

func implicationNote(kind string, ant, cons Match) string {
	if ant.Result == domain.TriFalse || ant.Vacuous {
		return "vacuous: antecedent did not fire (" + kind + ")"
	}
	return "antecedent fired; consequent " + cons.Result.String()
}

func maxSample(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func mergeStrings(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, x := range append(append([]string{}, a...), b...) {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
