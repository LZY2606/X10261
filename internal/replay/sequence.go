package replay

import (
	"signalcourt/internal/property"
)

// stepResult is the explicit three-state read of one sequence element.
type stepResult string

const (
	stepHold    stepResult = "hold"    // element known false at this tick
	stepMatch   stepResult = "match"   // element known true
	stepUnknown stepResult = "unknown" // x/z: undecided (provenance kept)
)

// stage is one boolean element of a compiled sequence. minOff/maxOff are the
// clock-tick offsets from the previous element at which this element may be
// read (##N => N..N; ##[lo:hi] => lo..hi). The first stage's offsets are
// measured from the sequence start tick, so a leading delay works naturally.
type stage struct {
	expr           property.Expr
	minOff, maxOff int
	// accLo/accHi > 0 mark a trailing delay into an empty suffix: the
	// vacuous endpoint can be reached at these offsets after this element.
	accLo, accHi int
}

// compiled is the lowered form of a sequence. An explicit empty sequence has
// no stages and vacuous=true; emptiness is never silently erased.
type compiled struct {
	stages  []stage
	vacuous bool
}

func compileSeq(s property.Seq) compiled {
	switch t := s.(type) {
	case *property.ExprSeq:
		return compiled{stages: []stage{{expr: t.Expr}}}
	case *property.EmptySeq:
		return compiled{vacuous: true}
	case *property.DelaySeq:
		pre := compileSeq(t.Prefix)
		suf := compileSeq(t.Suffix)
		out := compiled{vacuous: pre.vacuous || suf.vacuous}
		out.stages = append(out.stages, pre.stages...)
		base := len(out.stages)
		out.stages = append(out.stages, suf.stages...)
		// The delay sits on the boundary between prefix and suffix: attach
		// it to the first suffix stage. If the prefix is empty, the offset is
		// measured from the start tick (leading delay).
		if len(suf.stages) > 0 {
			out.stages[base].minOff = t.Lo
			out.stages[base].maxOff = t.Hi
		} else if len(pre.stages) > 0 {
			// suffix empty: its vacuous endpoint can be reached lo..hi ticks
			// after the last prefix element; encode as an extra accepting
			// offset carried on the last stage via acceptLo/acceptHi.
			last := len(out.stages) - 1
			out.stages[last].accLo = t.Lo
			out.stages[last].accHi = t.Hi
		}
		return out
	case *property.RepeatSeq:
		var out compiled
		for i := 0; i < t.Count; i++ {
			cp := compileSeq(t.Inner)
			if cp.vacuous {
				out.vacuous = true
			}
			if i == 0 {
				out.stages = append(out.stages, cp.stages...)
				continue
			}
			// Copies join with ##1: offset the first stage of this copy.
			base := len(out.stages)
			out.stages = append(out.stages, cp.stages...)
			if len(cp.stages) > 0 {
				out.stages[base].minOff = 1
				out.stages[base].maxOff = 1
			}
		}
		return out
	}
	return compiled{}
}

// thread is one concrete alignment walking the compiled stages.
type thread struct {
	id       int
	stageIdx int
	wait     int   // ticks until the current element is read
	offUsed  int   // offset chosen for the current element (provenance)
	pathOffs []int // offsets chosen along the path
	blocked  bool  // parked on an x/z element, retrying next tick
	matched  bool
}

// matchRec records one successful alignment.
type matchRec struct {
	endTick int
	path    []int
	vacuous bool
}

// snapRec is the machine-readable per-tick active-thread snapshot.
type snapRec struct {
	Tick     int    `json:"tick"`
	ThreadID int    `json:"thread_id"`
	StageIdx int    `json:"stage_idx"`
	State    string `json:"state"` // alive|blocked-x|matched|dead
	Offset   int    `json:"offset"`
}

// seqResult is the outcome of running a sequence over [start,end].
type seqResult struct {
	matches []matchRec
	// unknown: at least one alignment reached x/z at its scheduled element
	// tick (it remains undecided); failed: every alignment died on a known
	// false element at its scheduled tick.
	unknown bool
	failed  bool
	snaps   []snapRec
	threads []snapRec
}

type seqEngine struct {
	c       compiled
	smp     *Sampler
	nextID  int
	live    []*thread
	blocked []*thread
	dead    int
	snaps   []snapRec
	matches []matchRec
}

// thread follows one concrete alignment. Offsets are fixed when the thread
// is spawned, so elements are evaluated exactly at their scheduled ticks:
// there is no sliding retry, and a known-false element kills the alignment.
type thread struct {
	id       int
	stageIdx int
	wait     int
	offUsed  int
	pathOffs []int
}

func (e *Replayer) runSequence(c compiled, start, end int) seqResult {
	eng := &seqEngine{c: c, smp: e.smp, nextID: 1}
	if c.vacuous && len(c.stages) == 0 {
		eng.snaps = append(eng.snaps, snapRec{Tick: start, ThreadID: 0, State: "matched-empty"})
		return seqResult{
			matches: []matchRec{{endTick: start, vacuous: true}},
			snaps:   eng.snaps,
		}
	}
	eng.spawnRange(0, c.stages[0].minOff, c.stages[0].maxOff)
	for ti := start; ti <= end; ti++ {
		eng.step(ti, end)
		if len(eng.live) == 0 && len(eng.blocked) == 0 {
			break
		}
	}
	res := seqResult{matches: eng.matches, snaps: eng.snaps}
	res.unknown = len(eng.blocked) > 0
	res.failed = len(eng.matches) == 0 && len(eng.blocked) == 0
	for _, th := range eng.blocked {
		res.threads = append(res.threads, snapRec{
			Tick: end, ThreadID: th.id, StageIdx: th.stageIdx, State: "blocked-x",
		})
	}
	for _, th := range eng.live {
		res.threads = append(res.threads, snapRec{
			Tick: end, ThreadID: th.id, StageIdx: th.stageIdx, State: "alive",
		})
	}
	return res
}

// spawnRange forks one thread per legal offset for stage stageIdx.
func (eng *seqEngine) spawnRange(stageIdx, lo, hi int) {
	if hi < lo {
		hi = lo
	}
	for off := lo; off <= hi; off++ {
		eng.live = append(eng.live, &thread{
			id: eng.nextID, stageIdx: stageIdx, wait: off, offUsed: off,
		})
		eng.nextID++
	}
}

func (eng *seqEngine) step(ti, end int) {
	var waiters []*thread
	for _, th := range eng.live {
		if th.wait > 0 {
			th.wait--
			eng.snaps = append(eng.snaps, snapRec{
				Tick: ti, ThreadID: th.id, StageIdx: th.stageIdx,
				State: "alive", Offset: th.offUsed,
			})
			waiters = append(waiters, th)
			continue
		}
		st := eng.c.stages[th.stageIdx]
		switch evalStep(st.expr, eng.smp, ti) {
		case stepHold:
			eng.dead++
			eng.snaps = append(eng.snaps, snapRec{
				Tick: ti, ThreadID: th.id, StageIdx: th.stageIdx, State: "dead",
			})
		case stepUnknown:
			eng.snaps = append(eng.snaps, snapRec{
				Tick: ti, ThreadID: th.id, StageIdx: th.stageIdx, State: "blocked-x",
			})
			eng.blocked = append(eng.blocked, th)
		case stepMatch:
			th.pathOffs = append(th.pathOffs, th.offUsed)
			nextIdx := th.stageIdx + 1
			if nextIdx >= len(eng.c.stages) {
				path := append([]int(nil), th.pathOffs...)
				eng.matches = append(eng.matches, matchRec{endTick: ti, path: path})
				eng.snaps = append(eng.snaps, snapRec{
					Tick: ti, ThreadID: th.id, StageIdx: th.stageIdx, State: "matched",
				})
				if st.accHi > 0 {
					for off := st.accLo; off <= st.accHi; off++ {
						eng.matches = append(eng.matches, matchRec{
							endTick: ti + off, path: append(path, off), vacuous: true,
						})
					}
				}
				continue
			}
			nxt := eng.c.stages[nextIdx]
			eng.snaps = append(eng.snaps, snapRec{
				Tick: ti, ThreadID: th.id, StageIdx: th.stageIdx, State: "alive",
			})
			// Fork the fixed-offset successors (one per legal gap).
			for off := nxt.minOff; off <= maxi(nxt.minOff, nxt.maxOff); off++ {
				fork := &thread{
					id: eng.nextID, stageIdx: nextIdx, wait: off, offUsed: off,
					pathOffs: append([]int(nil), th.pathOffs...),
				}
				eng.nextID++
				waiters = append(waiters, fork)
			}
		}
	}
	eng.live = waiters
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func evalStep(ex property.Expr, smp *Sampler, ti int) stepResult {
	tb := smp.EvalExpr(ex, ti)
	switch tb.Value {
	case "true":
		return stepMatch
	case "false":
		return stepHold
	default:
		return stepUnknown
	}
}
