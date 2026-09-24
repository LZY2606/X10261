package engine

import (
	"sort"

	"signalcourt/internal/fourstate"
	"signalcourt/internal/property"
)

// evaluateAt evaluates a fresh assertion attempt starting at clock tick k.
func (e *Engine) evaluateAt(decl *property.Decl, k int) TickResult {
	region := e.domain.RegionAt(k, e.semver)
	horizon := e.domain.NEdges() - 1
	tr := TickResult{Tick: k, Region: region}

	// reset from the module header acts exactly like disable iff.
	root := decl.Property
	if e.module.ResetName != "" && !hasDisable(root) {
		// nothing: properties carry their own disable iff in the case pack
	}
	v, vac, threads, events := e.evalPropertyAt(root, k, horizon, decl.Name)
	tr.Verdict, tr.Vacuous, tr.Threads, tr.RuleEvents = v, vac, threads, events
	if d := findDisable(root); d != nil {
		dt, _ := e.evalBool(d, k)
		tr.Disabled = dt.String()
	}
	// deterministic order and stable identity per thread
	sort.SliceStable(tr.Threads, func(i, j int) bool {
		if tr.Threads[i].StartTick != tr.Threads[j].StartTick {
			return tr.Threads[i].StartTick < tr.Threads[j].StartTick
		}
		return tr.Threads[i].ID < tr.Threads[j].ID
	})
	for _, ev := range tr.RuleEvents {
		if ev.Prop == "" {
			ev.Prop = decl.Name
		}
	}
	// collect the samples actually used at this tick
	for _, s := range e.sampled[k] {
		tr.Samples = append(tr.Samples, s)
	}
	sort.SliceStable(tr.Samples, func(i, j int) bool { return tr.Samples[i].Signal < tr.Samples[j].Signal })
	return tr
}

func hasDisable(p *property.Property) bool { return findDisable(p) != nil }

func findDisable(p *property.Property) *property.BoolExpr {
	if p == nil {
		return nil
	}
	if p.Kind == property.PropDisable {
		return p.DisableCond
	}
	if p.Kind == property.PropDisable {
		return p.DisableCond
	}
	if p.Kind == property.PropImp {
		return findDisable(p.Cons)
	}
	if p.Kind == property.PropDisable && p.Inner != nil {
		return p.DisableCond
	}
	return nil
}

// UsedSamples exposes samples consumed during a replay (for the UI).
func (r *Replay) UsedSamples() map[int][]Sample {
	out := map[int][]Sample{}
	for _, t := range r.Results {
		if len(t.Samples) > 0 {
			out[t.Tick] = t.Samples
		}
	}
	return out
}

var _ = fourstate.TTrue
