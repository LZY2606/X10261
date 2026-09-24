package prop

import (
	"fmt"
	"sort"

	"sigtribunal/internal/fourval"
	"sigtribunal/internal/vcd"
)

// Params are evaluation parameters; changing them creates a new params
// version and invalidates dependent runs through the dependency graph.
type Params struct {
	MaxDelay              int  `json:"max_delay"`                // unfolding bound for delay ranges
	DisableUnknownAsFalse bool `json:"disable_unknown_as_false"` // disable iff X handling
}

// Semantics versions the evaluation rules. v1.1 differs from v1.0 by
// treating an unknown consequent as failure and an unknown antecedent as
// no-match, instead of propagating X.
type Semantics struct {
	Version                    string `json:"version"`
	AntecedentUnknownAsNoMatch bool   `json:"antecedent_unknown_as_no_match"`
	UnknownConsequenceAsFail   bool   `json:"unknown_consequence_as_fail"`
}

// Sampled holds the fixed-sampling view of a waveform: posedge times of the
// clock, the preponed sampling region of each cycle, and per-cycle sampled
// values. Times carry the waveform's raw time unit (Unit).
type Sampled struct {
	Clock   string              `json:"clock"`
	Unit    string              `json:"unit"`
	Times   []int64             `json:"times"`
	Regions [][2]int64          `json:"regions"`
	Values  []map[string]string `json:"values"`
	vals    []map[string]fourval.V
}

// Sample computes posedge sampling of w on clock. The sampled value of a
// signal at an edge is its last change strictly before the edge (preponed
// region); the sampling region of cycle i is [prevEdge, edge).
func Sample(w *vcd.File, clock string) (*Sampled, error) {
	cs := w.ByName[clock]
	if cs == nil {
		return nil, fmt.Errorf("clock signal %q not found", clock)
	}
	s := &Sampled{Clock: clock, Unit: w.Timescale}
	var prev fourval.V
	var prevEdge int64
	for i, ch := range cs.Changes {
		if ch.OK && ch.Val == fourval.One && (i == 0 || prev != fourval.One) {
			s.Times = append(s.Times, ch.Time)
			s.Regions = append(s.Regions, [2]int64{prevEdge, ch.Time})
			prevEdge = ch.Time
		}
		if ch.OK {
			prev = ch.Val
		}
	}
	for _, t := range s.Times {
		row := map[string]fourval.V{}
		srow := map[string]string{}
		for _, sig := range w.Signals {
			if ch, ok := sig.SampledAt(t); ok && ch.OK {
				row[sig.Name] = ch.Val
				srow[sig.Name] = ch.Val.String()
			} else {
				row[sig.Name] = fourval.X
				srow[sig.Name] = "x"
			}
		}
		s.vals = append(s.vals, row)
		s.Values = append(s.Values, srow)
	}
	return s, nil
}

// RuleEvent is a machine-readable record of a derivation-rule outcome
// (X/Z propagation, implication kind, disable iff, empty match, reduction).
type RuleEvent struct {
	Rule   string `json:"rule"`
	Cycle  int    `json:"cycle"`
	Result string `json:"result"` // explicit three-state result T/F/X or note
	Detail string `json:"detail"`
}

// Thread is one evaluation attempt started at a clock edge.
type Thread struct {
	ID    string `json:"id"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Path  []int  `json:"path"`
	State string `json:"state"` // pass/fail/unknown/disabled/vacuous
	Note  string `json:"note"`
}

// CexPath is one failing path of a failing thread: the referenced cycles and
// the attempt window, in cycle coordinates.
type CexPath struct {
	ThreadID string `json:"thread_id"`
	Cycles   []int  `json:"cycles"`
	Window   [2]int `json:"window"`
}

// Run is the full result of evaluating one property on one waveform.
type Run struct {
	Property  string      `json:"property"`
	Op        string      `json:"op"`
	Status    string      `json:"status"` // pass/fail/unknown/vacuous/disabled
	Unit      string      `json:"unit"`
	Threads   []Thread    `json:"threads"`
	Events    []RuleEvent `json:"events"`
	FailPaths []CexPath   `json:"fail_paths"`
	Sampled   *Sampled    `json:"sampled"`
	Signals   []string    `json:"signals"`
	SemVer    string      `json:"sem_ver"`
}

type evaluator struct {
	run     *Run
	s       *Sampled
	w       *vcd.File
	p       *Property
	par     Params
	sem     Semantics
	disable []fourval.T
	touched int // max cycle actually evaluated by the current attempt
	probes  []probe // failed boolean probes of the current consequent eval
}

// probe records where a boolean item evaluated false: a leaf failure point
// used to build one counterexample candidate per legal failing path.
type probe struct {
	path []int
	end  int
}

// Evaluate replays p on w under fixed sampling semantics.
func Evaluate(w *vcd.File, p *Property, par Params, sem Semantics) (*Run, error) {
	s, err := Sample(w, p.Clock)
	if err != nil {
		return nil, err
	}
	run := &Run{
		Property: p.Name,
		Op:       p.Op.String(),
		Status:   "vacuous",
		Unit:     w.Timescale,
		Sampled:  s,
		Signals:  p.ReferencedSignals(),
		SemVer:   sem.Version,
	}
	ev := &evaluator{run: run, s: s, w: w, p: p, par: par, sem: sem}
	if p.DisableIff != nil {
		ev.disable = make([]fourval.T, len(s.Times))
		for c := range s.Times {
			ev.disable[c] = ev.evalDisable(c)
		}
	}
	n := len(s.Times)
	for start := 0; start < n; start++ {
		ev.attempt(start)
	}
	run.Status = aggregate(run.Threads)
	return run, nil
}

func aggregate(threads []Thread) string {
	rank := map[string]int{"fail": 5, "unknown": 4, "pass": 3, "disabled": 2, "vacuous": 1}
	best, bestR := "vacuous", 0
	for _, t := range threads {
		if r := rank[t.State]; r > bestR {
			best, bestR = t.State, r
		}
	}
	return best
}

func (ev *evaluator) rule(rule string, cycle int, res fourval.T, detail string) {
	ev.run.Events = append(ev.run.Events, RuleEvent{
		Rule: rule, Cycle: cycle, Result: res.String(), Detail: detail,
	})
}

// evalDisable evaluates disable iff on current (not sampled) values at the
// edge time: a reset asserted exactly on the edge is still observed.
func (ev *evaluator) evalDisable(cycle int) fourval.T {
	t := ev.s.Times[cycle]
	res := ev.evalBoolAt(ev.p.DisableIff, func(name string) fourval.V {
		sig := ev.w.ByName[name]
		if sig == nil {
			return fourval.X
		}
		if ch, ok := sig.At(t); ok && ch.OK {
			return ch.Val
		}
		return fourval.X
	})
	ev.rule("disable-iff", cycle, res, fmt.Sprintf("disable iff evaluated on current values at t=%d (%s)", t, ev.s.Unit))
	return res
}

func (ev *evaluator) evalBool(e BExpr, c int) fourval.T {
	if c > ev.touched {
		ev.touched = c
	}
	return ev.evalBoolAt(e, func(name string) fourval.V {
		if c < len(ev.s.vals) {
			if v, ok := ev.s.vals[c][name]; ok {
				return v
			}
		}
		return fourval.X
	})
}

func (ev *evaluator) evalBoolAt(e BExpr, get func(string) fourval.V) fourval.T {
	switch n := e.(type) {
	case Signal:
		return fourval.Bit(get(n.Name))
	case Const:
		return fourval.Bit(n.Val)
	case Not:
		return fourval.Not(ev.evalBoolAt(n.E, get))
	case And:
		return fourval.And(ev.evalBoolAt(n.L, get), ev.evalBoolAt(n.R, get))
	case Or:
		return fourval.Or(ev.evalBoolAt(n.L, get), ev.evalBoolAt(n.R, get))
	case Cmp:
		lv := ev.operandVal(n.L, get)
		rv := ev.operandVal(n.R, get)
		eq := fourval.Eq(lv, rv)
		if n.Op == "!=" {
			return fourval.Not(eq)
		}
		return eq
	}
	return fourval.Unknown
}

func (ev *evaluator) operandVal(e BExpr, get func(string) fourval.V) fourval.V {
	switch n := e.(type) {
	case Signal:
		return get(n.Name)
	case Const:
		return n.Val
	default:
		return fourval.X
	}
}

type match struct {
	end   int
	path  []int
	res   fourval.T
	empty bool
}

func (ev *evaluator) capDelay(d int) int {
	if ev.par.MaxDelay > 0 && d > ev.par.MaxDelay {
		return ev.par.MaxDelay
	}
	return d
}

// matchSeq matches items[i:] with item i starting at cycle c.
func (ev *evaluator) matchSeq(items []Item, i, c int, path []int) []match {
	if i >= len(items) {
		return []match{{end: c - 1, path: path, res: fourval.True}}
	}
	it := items[i]
	var out []match
	if it.Rep == 0 {
		// Empty match: explicit three-state result True, recorded as an event.
		ev.rule("empty-match", c, fourval.True,
			fmt.Sprintf("zero repetition of item %d matches empty at cycle %d", i, c))
		lo, hi := it.Delay.Min, ev.capDelay(it.Delay.Max)
		if i == len(items)-1 {
			lo, hi = 0, 0
		}
		for d := lo; d <= hi; d++ {
			for _, m := range ev.matchSeq(items, i+1, c+d, path) {
				m.empty = true
				out = append(out, m)
			}
		}
		return out
	}
	res := fourval.True
	cc := c
	p := append([]int{}, path...)
	for r := 0; r < it.Rep; r++ {
		if cc >= len(ev.s.Times) {
			return out
		}
		t := ev.evalBool(it.Expr, cc)
		if t == fourval.Unknown {
			ev.rule("xz-propagation", cc, fourval.Unknown,
				fmt.Sprintf("item %d evaluates X at cycle %d", i, cc))
		}
		res = fourval.And(res, t)
		if t == fourval.False {
			ev.probes = append(ev.probes, probe{path: append(append([]int{}, path...), cc), end: cc})
			return out
		}
		p = append(p, cc)
		cc++
	}
	end := cc - 1
	if i == len(items)-1 {
		return []match{{end: end, path: p, res: res}}
	}
	lo, hi := it.Delay.Min, ev.capDelay(it.Delay.Max)
	for d := lo; d <= hi; d++ {
		for _, m := range ev.matchSeq(items, i+1, end+d, p) {
			m.res = fourval.And(res, m.res)
			out = append(out, m)
		}
	}
	return out
}

func (ev *evaluator) matchWhole(s Seq, start int) []match {
	var out []match
	lo, hi := s.Pre.Min, ev.capDelay(s.Pre.Max)
	for d := lo; d <= hi; d++ {
		out = append(out, ev.matchSeq(s.Items, 0, start+d, nil)...)
	}
	return out
}

// maxSpan is a conservative cycle span of s, used to bound attempt windows.
func maxSpan(s Seq, par Params) int {
	span := s.Pre.Max
	if par.MaxDelay > 0 && span > par.MaxDelay {
		span = par.MaxDelay
	}
	for _, it := range s.Items {
		r := it.Rep
		if r == 0 {
			r = 0
		}
		d := it.Delay.Max
		if par.MaxDelay > 0 && d > par.MaxDelay {
			d = par.MaxDelay
		}
		span += r + d
	}
	return span
}

func (ev *evaluator) attempt(start int) {
	n := len(ev.s.Times)
	th := Thread{ID: fmt.Sprintf("t%d", start), Start: start, State: "vacuous"}
	ev.touched = start
	ants := ev.matchWhole(ev.p.Antecedent, start)
	passFound, anyUnknown := false, false
	var failed []CexPath
	var failPath []int
	for _, a := range ants {
		if a.res == fourval.False {
			continue
		}
		if a.res == fourval.Unknown {
			if ev.sem.AntecedentUnknownAsNoMatch {
				ev.rule("xz-propagation", start, fourval.Unknown,
					"antecedent X treated as no-match (sem "+ev.sem.Version+")")
				continue
			}
			ev.rule("xz-propagation", start, fourval.Unknown,
				"antecedent X propagates to thread result")
			anyUnknown = true
			continue
		}
		cstart := a.end
		rule := "overlapped-implication"
		if ev.p.Op == NonOverlapped {
			cstart = a.end + 1
			rule = "non-overlapped-implication"
		}
		ev.rule(rule, cstart, fourval.True,
			fmt.Sprintf("attempt@%d antecedent ends cycle %d, consequent starts cycle %d", start, a.end, cstart))
		ev.probes = nil
		cons := ev.matchWhole(ev.p.Consequent, cstart)
		probes := ev.probes
		ok, unk := false, false
		for _, cm := range cons {
			switch cm.res {
			case fourval.True:
				ok = true
			case fourval.Unknown:
				unk = true
			}
		}
		if ok {
			passFound = true
			continue
		}
		if unk && !ev.sem.UnknownConsequenceAsFail {
			anyUnknown = true
			continue
		}
		// definite failure for this antecedent match: one failing path per
		// legal explanation (X-as-fail matches and false probes).
		seen := map[string]bool{}
		addPath := func(cycles []int, end int) {
			cycles = dedup(append(append([]int{}, a.path...), cycles...))
			if end > n-1 {
				end = n - 1
			}
			key := fmt.Sprintf("%v/%d", cycles, end)
			if seen[key] {
				return
			}
			seen[key] = true
			failed = append(failed, CexPath{ThreadID: th.ID, Cycles: cycles, Window: [2]int{start, end}})
			failPath = cycles
		}
		for _, cm := range cons {
			if cm.res == fourval.Unknown && ev.sem.UnknownConsequenceAsFail {
				addPath(cm.path, cm.end)
			}
		}
		for _, pr := range probes {
			addPath(pr.path, pr.end)
		}
	}

	// disable iff: explicit three-state outcome per attempt, checked only
	// over the cycles the attempt actually evaluated (a reset that rises
	// after the attempt completed does not abort it).
	disabled, disableX := false, false
	if ev.disable != nil {
		for c := start; c <= ev.touched && c < n; c++ {
			switch ev.disable[c] {
			case fourval.True:
				disabled = true
			case fourval.Unknown:
				disableX = true
			}
		}
	}
	th.End = ev.touched
	if disabled {
		th.State = "disabled"
		th.Note = "disable iff true during attempt"
		ev.run.Threads = append(ev.run.Threads, th)
		return
	}
	if disableX && !ev.par.DisableUnknownAsFalse {
		th.State = "unknown"
		th.Note = "disable iff evaluated X during attempt"
		ev.run.Threads = append(ev.run.Threads, th)
		return
	}

	switch {
	case passFound:
		th.State = "pass"
	case anyUnknown:
		th.State = "unknown"
		th.Note = "three-state X propagated to thread result"
	case len(failed) > 0:
		th.State = "fail"
		th.Path = failPath
		th.End = failed[0].Window[1]
		ev.run.FailPaths = append(ev.run.FailPaths, failed...)
	default:
		th.State = "vacuous"
	}
	ev.run.Threads = append(ev.run.Threads, th)
}

func dedup(in []int) []int {
	set := map[int]bool{}
	var out []int
	for _, v := range in {
		if !set[v] {
			set[v] = true
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}
