package core

import "sort"

// Minimized 是最小反例：保留时钟（时间坐标基准）与失败线程实际读取的
// 证据变化，移除其余与失败无关的区间。所有时刻保持原始值，时间原点不变。
type Minimized struct {
	ThreadID         string        `json:"thread_id"`
	KeptChanges      []ValueChange `json:"kept_changes"`
	RemovedIntervals [][2]int64    `json:"removed_intervals"` // 原始时标
	OriginPreserved  bool          `json:"origin_preserved"`
	Recheck          Tri           `json:"recheck"` // 在缩减后波形上重放同一线程的结果
}

// Minimize 对失败线程做反例缩减。
// 规则：只移除与失败无关的区间（非时钟、非证据的原始值变化），
// 不改变时间原点——时钟变化全部保留，保留的变化时刻不移动。
func Minimize(wf *Waveform, prop *Property, clock ClockDomain, sem Semantics, thread *Thread) *Minimized {
	type key struct {
		sig string
		t   int64
	}
	evidence := map[key]bool{}
	for _, s := range thread.Samples {
		if s.ChangeTime >= 0 {
			evidence[key{s.Signal, s.ChangeTime}] = true
		}
	}
	var kept []ValueChange
	for _, c := range wf.Changes {
		if c.Signal == clock.Signal || evidence[key{c.Signal, c.Time}] {
			kept = append(kept, c)
		}
	}

	var evTimes []int64
	for _, c := range kept {
		if c.Signal != clock.Signal {
			evTimes = append(evTimes, c.Time)
		}
	}
	sort.Slice(evTimes, func(i, j int) bool { return evTimes[i] < evTimes[j] })
	end := wf.EndTime()
	var removed [][2]int64
	prev := int64(0)
	for _, t := range evTimes {
		if t > prev {
			removed = append(removed, [2]int64{prev, t})
		}
		prev = t
	}
	if end > prev {
		removed = append(removed, [2]int64{prev, end})
	}

	minWf := &Waveform{
		ID:            wf.ID + ":min",
		Name:          wf.Name + ":min",
		Version:       wf.Version,
		TimescaleMag:  wf.TimescaleMag,
		TimescaleUnit: wf.TimescaleUnit,
		Signals:       wf.Signals,
		Changes:       kept,
	}
	recheck := TriUnknown
	rerun := Replay(minWf, prop, clock, sem)
	for _, th := range rerun.Threads {
		if th.StartTick == thread.StartTick {
			recheck = th.Result
		}
	}
	return &Minimized{
		ThreadID:         thread.ID,
		KeptChanges:      kept,
		RemovedIntervals: removed,
		OriginPreserved:  true,
		Recheck:          recheck,
	}
}
