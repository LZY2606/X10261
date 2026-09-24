package engine

import (
	"crypto/sha256"
	"encoding/hex"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/property"
)

// endpoint is a possible match endpoint of a sequence starting at k0.
type endpoint struct {
	k       int
	t3      fourstate.T3
	empty   bool
	nodeID  string
	via     string
}

func threadID(prop, nodeID string, start int) string {
	sum := sha256.Sum256([]byte(prop + "|" + nodeID + "|" + itoa(start)))
	return "thr_" + hex.EncodeToString(sum[:8])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// seqEnd enumerates all distinct endpoints for sequence s starting at k0.
// Bounded sequences make the set finite; unknown endpoints are kept, never
// collapsed into pass or fail.
func (e *Engine) seqEndpoints(s *property.Sequence, k0, horizon int) []endpoint {
	switch s.Kind {
	case property.SeqEmpty:
		return []endpoint{{k: k0, t3: fourstate.TTrue, empty: true, nodeID: s.NodeID, via: "empty"}}

	case property.SeqLit:
		var out []endpoint
		for r := s.RepMin; r <= s.RepMax; r++ {
			if !s.HasRep {
				r = 1
			}
			lo := k0 + r - 1
			hi := lo
			if r == 0 {
				out = append(out, endpoint{k: k0, t3: fourstate.TTrue, empty: true, nodeID: s.NodeID, via: "rep0"})
				if !s.HasRep {
					return out
				}
				continue
			}
			_ = hi
			t := fourstate.TTrue
			possible := false
			for k := lo; k <= lo; k++ {
				if k > horizon {
					possible = true
					t = or3(t, fourstate.TUnknown)
					continue
				}
				v, _ := e.evalBool(s.Cond, k)
				t = and3(t, v)
			}
			out = append(out, endpoint{k: lo, t3: t, nodeID: s.NodeID, via: "lit"})
			_ = possible
			if !s.HasRep {
				return out
			}
		}
		return dedupEndpoints(out)

	case property.SeqDelay:
		// Flatten a chain of literals joined by ##[lo:hi]: enumerate every
		// legal offset path so each window member becomes a distinct endpoint.
		return e.enumerateDelay(s, k0, horizon, fourstate.TTrue, false)

	case property.SeqOr:
		return dedupEndpoints(append(
			prefixEndpoints(e.seqEndpoints(s.Left, k0, horizon), "orL"),
			prefixEndpoints(e.seqEndpoints(s.Right, k0, horizon), "orR")...,
		))

	case property.SeqAnd:
		l := e.seqEndpoints(s.Left, k0, horizon)
		rg := e.seqEndpoints(s.Right, k0, horizon)
		var out []endpoint
		for _, a := range l {
			for _, b := range rg {
				k := a.k
				if b.k > k {
					k = b.k
				}
				out = append(out, endpoint{k: k, t3: and3(a.t3, b.t3), empty: a.empty && b.empty, nodeID: s.NodeID, via: "and"})
			}
		}
		return dedupEndpoints(out)
	}
	return nil
}

func prefixEndpoints(es []endpoint, tag string) []endpoint {
	for i := range es {
		es[i].via = tag
	}
	return es
}

func dedupEndpoints(es []endpoint) []endpoint {
	seen := map[[3]any]bool{}
	var out []endpoint
	for _, x := range es {
		key := [3]any{x.k, x.t3, x.empty}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, x)
	}
	return out
}

func and3(a, b fourstate.T3) fourstate.T3 { return fourstate.And(a, b) }
func or3(a, b fourstate.T3) fourstate.T3  { return fourstate.Or(a, b) }

// evalPropertyAt evaluates property p for an attempt started at tick k.
// It returns the verdict, vacuity and rule events for the *current* tick
// horizon; threads still in flight are reported by evaluateAt separately.
func (e *Engine) evalPropertyAt(p *property.Property, k, horizon int, propName string) (Verdict, bool, []Thread, []RuleEvent) {
	switch p.Kind {
	case property.PropSeq:
		ends := e.seqEndpoints(p.Seq, k, horizon)
		return seqVerdict(ends, p.Strength), isVacuous(ends), threadsOf(propName, k, ends, "seq"), nil

	case property.PropDisable:
		cond, cevents := e.evalBool(p.DisableCond, k)
		ev := RuleEvent{Rule: "disable_iff", Tick: k, T3: cond.String(), NodeID: p.NodeID, Prop: propName,
			Details: map[string]any{"condition_t3": cond.String()}}
		v, vac, th, evs := e.evalPropertyAt(p.Inner, k, horizon, propName)
		evs = append(evs, cevents...)
		switch cond {
		case fourstate.TTrue:
			ev.Details["resolution"] = "disabled"
			return VPass, false, markThreads(th, "disabled"), append(evs, ev)
		case fourstate.TUnknown:
			ev.Details["resolution"] = "unknown_disable"
			return VUnknown, false, markThreads(th, "unknown_disable"), append(evs, ev)
		default:
			ev.Details["resolution"] = "enabled"
			return v, vac, th, append(evs, ev)
		}

	case property.PropImp:
		ante := e.seqEndpoints(p.Ante, k, horizon)
		// vacuity: an antecedent that has a definite empty match and no
		// non-empty match makes the implication vacuous.
		var threads []Thread
		var events []RuleEvent
		hasNonEmpty, hasEmptyDef, hasEmptyFail := false, false, false
		var anteT threeAgg
		var consResults []endpoint
		var hasUnknownAnte bool
		for _, a := range ante {
			anteT.add(a.t3)
			if a.empty && a.t3 == fourstate.TTrue {
				hasEmptyDef = true
			}
			if a.empty && a.t3 == fourstate.TFalse {
				hasEmptyFail = true
			}
			if a.t3 == fourstate.TUnknown {
				hasUnknownAnte = true
				threads = append(threads, Thread{
					ID: threadID(propName, a.nodeID+"@ante", k), StartTick: k, EndTick: a.k,
					NodeID: a.nodeID, State: "live", Kind: "ante", Label: "unknown_ante",
				})
				continue
			}
			if !a.empty && a.t3 == fourstate.TTrue {
				hasNonEmpty = true
				start := a.k
				flavor := "overlapped"
				if !p.Overlapped {
					start = a.k + 1
					flavor = "non_overlapped"
				}
				ev := RuleEvent{Rule: "implication_" + flavor, Tick: k, NodeID: p.NodeID, Prop: propName,
					Details: map[string]any{"ante_end": a.k, "cons_start": start, "ante_t3": a.t3.String()}}
				events = append(events, ev)
				if start > horizon {
					threads = append(threads, Thread{ID: threadID(propName, p.Cons.NodeID+"@"+flavor, start), StartTick: start, NodeID: p.Cons.NodeID, State: "live", Kind: "cons", Label: flavor})
					consResults = append(consResults, endpoint{k: start, t3: fourstate.TUnknown})
					continue
				}
				cv, _, cth, cev := e.evalPropertyAt(p.Cons, start, horizon, propName)
				events = append(events, cev...)
				threads = append(threads, cth...)
				consResults = append(consResults, endpoint{k: start, t3: verdictT3(cv)})
			}
		}
		threads = append(threads, threadsOf(propName, k, ante, "ante")...)
		// Antecedent definite false -> implication holds vacuously at this
		// attempt (a standard SVA vacuous success).
		if !hasNonEmpty && !hasEmptyDef && anteT.known && !anteT.any {
			events = append(events, RuleEvent{Rule: "empty_match", Tick: k, T3: "true", Vacuous: true, NodeID: p.NodeID, Prop: propName,
				Details: map[string]any{"resolution": "antecedent_did_not_match", "three_state": "pass"}})
			return VPass, true, threads, events
		}
		if hasEmptyDef && !hasNonEmpty {
			// empty antecedent matched: implication fires on an empty match;
			// verdict is the consequent at the same tick; recorded explicitly.
			events = append(events, RuleEvent{Rule: "empty_match", Tick: k, T3: "true", Vacuous: true, NodeID: p.NodeID, Prop: propName,
				Details: map[string]any{"resolution": "empty_antecedent", "three_state": "explicit"}})
			return aggregateCons(consResults, p.Cons, e, horizon), true, threads, events
		}
		if anteT.known && len(consResults) == 0 && !hasNonEmpty {
			return VPass, true, threads, events
		}
		_ = hasEmptyFail
		if hasUnknownAnte && len(consResults) == 0 {
			events = append(events, RuleEvent{Rule: "xz_propagation", Tick: k, T3: "unknown", NodeID: p.Ante.NodeID, Prop: propName,
				Details: map[string]any{"resolution": "unknown_antecedent_kept_live", "three_state": "unknown"}})
			return VUnknown, false, threads, events
		}
		return aggregateCons(consResults, p.Cons, e, horizon), false, threads, events
	}
	return VUnknown, false, nil, nil
}

type threeAgg struct {
	known bool
	any   bool
	unk   bool
}

func (a *threeAgg) add(t fourstate.T3) {
	if t == fourstate.TUnknown {
		a.unk = true
		return
	}
	a.known = true
	if t == fourstate.TTrue {
		a.any = true
	}
}

func verdictT3(v Verdict) fourstate.T3 {
	switch v {
	case VPass:
		return fourstate.TTrue
	case VFail:
		return fourstate.TFalse
	}
	return fourstate.TUnknown
}

func seqVerdict(ends []endpoint, strength string) Verdict {
	var hasTrue, hasUnknown bool
	var hasNonEmptyTrue bool
	for _, x := range ends {
		switch x.t3 {
		case fourstate.TTrue:
			hasTrue = true
			if !x.empty {
				hasNonEmptyTrue = true
			}
		case fourstate.TUnknown:
			hasUnknown = true
		}
	}
	if hasNonEmptyTrue || hasTrue && !looksOnlyEmpty(ends) {
		return VPass
	}
	if hasTrue {
		return VPass
	}
	if hasUnknown {
		return VUnknown
	}
	return VFail
}

func looksOnlyEmpty(ends []endpoint) bool {
	for _, x := range ends {
		if !x.empty {
			return false
		}
	}
	return true
}

func isVacuous(ends []endpoint) bool {
	for _, x := range ends {
		if x.empty && x.t3 == fourstate.TTrue {
			return true
		}
	}
	return false
}

func aggregateCons(results []endpoint, cons *property.Property, e *Engine, horizon int) Verdict {
	var hasTrue, hasFail, hasUnk bool
	for _, r := range results {
		switch r.t3 {
		case fourstate.TTrue:
			hasTrue = true
		case fourstate.TFalse:
			hasFail = true
		case fourstate.TUnknown:
			hasUnk = true
		}
	}
	// All legal consequent candidates are retained: any definite success
	// makes the implication hold; otherwise unknown beats fail for weak
	// properties (failure requires a definite miss within the horizon).
	if hasTrue {
		return VPass
	}
	if hasUnk {
		return VUnknown
	}
	if hasFail {
		return VFail
	}
	return VPass
}

func threadsOf(prop string, start int, ends []endpoint, kind string) []Thread {
	var out []Thread
	for _, x := range ends {
		state := "live"
		switch x.t3 {
		case fourstate.TTrue:
			state = "pass"
			if x.empty {
				state = "vacuous"
			}
		case fourstate.TFalse:
			state = "fail"
		case fourstate.TUnknown:
			state = "live"
		}
		out = append(out, Thread{ID: threadID(prop, x.nodeID, start), StartTick: start, EndTick: x.k, NodeID: x.nodeID, State: state, Kind: kind, MatchEmpty: x.empty, Label: x.via})
	}
	return out
}

func markThreads(th []Thread, state string) []Thread {
	out := append([]Thread(nil), th...)
	for i := range out {
		out[i].State = state
	}
	return out
}
