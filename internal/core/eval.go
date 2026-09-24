package core

import (
	"fmt"
	"sort"
)

// Semantics 是语义版本。版本差异只影响明确声明的规则，
// 旧版本计算出的结论保留在原分支，不被全局替换。
type Semantics struct {
	Version int `json:"version"`
}

// EmptyMatchResult 空匹配（[*0]）的三态结果：
// v1 视为 vacuous true；v2 起视为 unknown（无法唯一决定，需裁定）。
func (s Semantics) EmptyMatchResult() Tri {
	if s.Version >= 2 {
		return TriUnknown
	}
	return TriTrue
}

// DisabledResult disable iff 命中时的三态结果：
// v1 视为 vacuous true；v2 起视为 unknown。
func (s Semantics) DisabledResult() Tri {
	if s.Version >= 2 {
		return TriUnknown
	}
	return TriTrue
}

// Step 是线程的一步，标明采样区域 [RegionStart, RegionEnd]。
type Step struct {
	Tick        int    `json:"tick"`
	EdgeTime    int64  `json:"edge_time"`
	RegionStart int64  `json:"region_start"`
	RegionEnd   int64  `json:"region_end"`
	Note        string `json:"note,omitempty"`
}

// SampleEvent 记录一次采样的证据：哪个 tick、哪个信号、
// 由哪一时刻的原始值变化决定。反例缩减只移除与失败无关的变化。
type SampleEvent struct {
	Tick       int    `json:"tick"`
	Signal     string `json:"signal"`
	ChangeTime int64  `json:"change_time"` // -1 表示无原始变化
	Value      string `json:"value"`
}

// Thread 是一次属性尝试（attempt）。多个线程可并存。
type Thread struct {
	ID        string        `json:"id"`
	StartTick int           `json:"start_tick"`
	Result    Tri           `json:"result"`
	Reason    string        `json:"reason"` // vacuous|passed|failed|xz-propagation|disable-iff|reset-boundary|empty-match
	Reads     []int         `json:"reads"`
	Steps     []Step        `json:"steps"`
	Samples   []SampleEvent `json:"samples"`
}

// Run 是一次重放结论，携带完整来源链。
type Run struct {
	ID         string    `json:"id"`
	WaveformID string    `json:"waveform_id"`
	PropertyID string    `json:"property_id"`
	ClockID    string    `json:"clock_id"`
	Semantics  int       `json:"semantics"`
	Result     Tri       `json:"result"`
	Threads    []*Thread `json:"threads"`
	Sources    []string  `json:"sources"`
}

type evalCtx struct {
	wf     *Waveform
	edges  []int64
	sem    Semantics
	resets [][2]int64
	th     *Thread
}

func (c *evalCtx) region(tick int) (int64, int64) {
	start := int64(0)
	if tick > 0 {
		start = c.edges[tick-1]
	}
	return start, c.edges[tick]
}

func (c *evalCtx) sample(signal string, tick int) Tri {
	edge := c.edges[tick]
	v, ct, ok := c.wf.sampleEvidence(signal, edge)
	if !ok {
		ct = -1
	}
	c.th.Samples = append(c.th.Samples, SampleEvent{Tick: tick, Signal: signal, ChangeTime: ct, Value: v})
	return FourStateToTri(v)
}

func (c *evalCtx) evalExpr(e *Expr, tick int) Tri {
	switch e.Kind {
	case "sig":
		return c.sample(e.Name, tick)
	case "not":
		return Not3(c.evalExpr(e.A, tick))
	case "and":
		return And3(c.evalExpr(e.A, tick), c.evalExpr(e.B, tick))
	case "or":
		return Or3(c.evalExpr(e.A, tick), c.evalExpr(e.B, tick))
	}
	return TriUnknown
}

func (c *evalCtx) noteRead(tick int) {
	for _, r := range c.th.Reads {
		if r == tick {
			return
		}
	}
	c.th.Reads = append(c.th.Reads, tick)
}

// matchSeq 匹配序列，返回三态结果、结束 tick、是否用到空匹配。
// 空匹配不读任何采样（reads 不变），但结果按语义版本明确给出。
func (c *evalCtx) matchSeq(seq *Seq, start int) (Tri, int, bool) {
	t := start
	res := TriTrue
	empty := false
	for _, item := range seq.Items {
		t += item.Delay
		if t >= len(c.edges) {
			return TriUnknown, t, empty
		}
		if item.Rep0 {
			empty = true
			res = And3(res, c.sem.EmptyMatchResult())
			continue
		}
		c.noteRead(t)
		v := c.evalExpr(item.Expr, t)
		res = And3(res, v)
		if res == TriFalse {
			return res, t, empty
		}
	}
	return res, t, empty
}

func (c *evalCtx) inReset(tick int) bool {
	s, e := c.region(tick)
	for _, iv := range c.resets {
		if e > iv[0] && s < iv[1] {
			return true
		}
	}
	return false
}

// attempt 在 tick t0 启动一个线程，给出明确三态结果。
// 判定顺序：reset 边界 > disable iff > 前件否（vacuous）> X/Z 传播 > 后件。
func (c *evalCtx) attempt(prop *Property, t0 int) *Thread {
	c.th = &Thread{ID: fmt.Sprintf("th-%04d", t0), StartTick: t0}
	th := c.th

	ra, te, emptyAnt := c.matchSeq(&prop.Ant, t0)
	end := te
	rc := TriTrue
	emptyCons := false
	reached := false
	if ra == TriTrue {
		start := te
		if prop.Op == "|=>" { // non-overlapped：后件从前件结束的下一拍开始
			start = te + 1
		}
		if start >= len(c.edges) {
			rc = TriUnknown
		} else {
			rc, end, emptyCons = c.matchSeq(&prop.Cons, start)
			reached = true
		}
	}

	disabled := false
	if prop.DisableIff != nil {
		limit := end
		if limit >= len(c.edges) {
			limit = len(c.edges) - 1
		}
		for t := t0; t <= limit; t++ {
			c.noteRead(t)
			if c.evalExpr(prop.DisableIff, t) == TriTrue {
				disabled = true
			}
		}
	}
	resetHit := false
	for _, t := range th.Reads {
		if c.inReset(t) {
			resetHit = true
		}
	}

	switch {
	case resetHit:
		th.Result, th.Reason = TriUnknown, "reset-boundary"
	case disabled:
		th.Result, th.Reason = c.sem.DisabledResult(), "disable-iff"
	case ra == TriFalse:
		th.Result, th.Reason = TriTrue, "vacuous"
	case ra == TriUnknown:
		th.Result, th.Reason = TriUnknown, reasonUnknown(emptyAnt)
	case !reached && rc == TriUnknown:
		th.Result, th.Reason = TriUnknown, "window-exhausted"
	default:
		th.Result = rc
		switch rc {
		case TriFalse:
			th.Reason = "failed"
		case TriUnknown:
			th.Reason = reasonUnknown(emptyCons)
		default:
			th.Reason = "passed"
		}
	}

	sort.Ints(th.Reads)
	for _, t := range th.Reads {
		s, e := c.region(t)
		th.Steps = append(th.Steps, Step{Tick: t, EdgeTime: c.edges[t], RegionStart: s, RegionEnd: e})
	}
	return th
}

func reasonUnknown(empty bool) string {
	if empty {
		return "empty-match"
	}
	return "xz-propagation"
}

// Replay 在固定取样语义下重放一条属性：每个时钟沿启动一个线程，
// 线程并存；属性结果为所有线程的三态聚合（false > unknown > true）。
func Replay(wf *Waveform, prop *Property, clock ClockDomain, sem Semantics) *Run {
	edges := wf.Edges(clock.Signal)
	resets := wf.ResetIntervals(clock.ResetSignal, clock.ResetActive)
	ctx := &evalCtx{wf: wf, edges: edges, sem: sem, resets: resets}
	run := &Run{
		WaveformID: wf.ID,
		Semantics:  sem.Version,
		Sources: []string{
			wf.ID, prop.Name, clock.ID, fmt.Sprintf("semantics-v%d", sem.Version),
		},
	}
	result := TriTrue
	for t := 0; t < len(edges); t++ {
		th := ctx.attempt(prop, t)
		run.Threads = append(run.Threads, th)
		if th.Result == TriFalse {
			result = TriFalse
		}
	}
	if result != TriFalse {
		for _, th := range run.Threads {
			if th.Result == TriUnknown {
				result = TriUnknown
				break
			}
		}
	}
	run.Result = result
	return run
}
