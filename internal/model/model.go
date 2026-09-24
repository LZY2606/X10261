// Package model 定义信号言词庭的核心领域类型。
// 原则：来源（原始字节）与解释（派生层）分开保存；一切领域量携带单位或坐标基准；
// 每个显示值都有稳定身份（ID 在导出/删除/再导入后保持不变）。
package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Value 是四态逻辑值。
type Value byte

const (
	V0 Value = '0'
	V1 Value = '1'
	VX Value = 'x'
	VZ Value = 'z'
)

// Tri 是三态判定结果。X/Z 传播、implication、disable iff、空匹配
// 都必须落到明确的三态结果上。
type Tri string

const (
	TriTrue    Tri = "true"
	TriFalse   Tri = "false"
	TriUnknown Tri = "unknown"
)

// ValueChange 是原波形中的一次值变化，保留原始表示（raw）与时间戳。
type ValueChange struct {
	Time  int64  `json:"time"` // 以波形时标精度为单位
	Raw   string `json:"raw"`  // 原始表示，如 "4'b10xz"
	Value string `json:"value"`
}

// Signal 是一条信号的完整值变化历史。
type Signal struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Width   int           `json:"width"`
	Changes []ValueChange `json:"changes"`
}

// ValueAt 返回 t 时刻（含）之前最后一次变化的值；无变化时返回全 x。
func (s *Signal) ValueAt(t int64) string {
	val := ""
	for _, c := range s.Changes {
		if c.Time > t {
			break
		}
		val = c.Value
	}
	if val == "" {
		for i := 0; i < s.Width; i++ {
			val += "x"
		}
		if s.Width == 0 {
			val = "x"
		}
	}
	return val
}

// Waveform 是解析后的波形（派生层），原始 VCD 字节仍保存在 sources 表。
type Waveform struct {
	TimescaleNum  int64    `json:"timescale_num"`  // 时标数值，如 1
	TimescaleUnit string   `json:"timescale_unit"` // 时标单位，如 "ns"/"ps"
	Signals       []Signal `json:"signals"`
	EndTime       int64    `json:"end_time"`
}

// Find 按名字找信号。
func (w *Waveform) Find(name string) *Signal {
	for i := range w.Signals {
		if w.Signals[i].Name == name {
			return &w.Signals[i]
		}
	}
	return nil
}

// ClockDomain 描述一个时钟域（坐标基准：以波形时标计）。
type ClockDomain struct {
	Name   string `json:"name"`
	Signal string `json:"signal"` // 为空则按 Period/Offset 生成
	Edge   string `json:"edge"`   // "posedge" | "negedge"
	Period int64  `json:"period"` // 波形时标单位
	Offset int64  `json:"offset"`
}

// ResetInterval 是一个 reset 生效区间 [Start, End)，坐标为波形时标。
type ResetInterval struct {
	Signal string `json:"signal"`
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
}

// ClockConfig 是时钟域与 reset 区间的参数集合。
type ClockConfig struct {
	Clocks  []ClockDomain   `json:"clocks"`
	Resets  []ResetInterval `json:"resets"`
	Sample  string          `json:"sample"` // 采样区域语义，固定为 "observed"
	Version int             `json:"version"`
}

// ThreadStatus 是重放线程的明确状态。
type ThreadStatus string

const (
	TSPass     ThreadStatus = "pass"
	TSFail     ThreadStatus = "fail"
	TSUnknown  ThreadStatus = "unknown"
	TSDisabled ThreadStatus = "disabled" // disable iff 命中
	TSVacuous  ThreadStatus = "vacuous"  // 空匹配/前件不成立
)

// TraceStep 记录一次采样：采样区域、时间、读到的原始值与判定。
type TraceStep struct {
	Tick   int    `json:"tick"`
	Time   int64  `json:"time"`
	Region string `json:"region"` // 采样区域，如 "clk:observed"
	Expr   string `json:"expr"`
	Raw    string `json:"raw"`    // 采样读回的原始值表示
	Result Tri    `json:"result"` // 明确三态
	Note   string `json:"note,omitempty"`
}

// Candidate 是无法唯一决定时保留的一个合法候选。
type Candidate struct {
	ID     string       `json:"id"`
	Desc   string       `json:"desc"`
	Status ThreadStatus `json:"status"`
	Trace  []TraceStep  `json:"trace"`
}

// Thread 是一次属性尝试（attempt），多线程可并存。
type Thread struct {
	ID         string       `json:"id"`
	Property   string       `json:"property"`
	StartTick  int          `json:"start_tick"`
	EndTick    int          `json:"end_tick"`
	Status     ThreadStatus `json:"status"`
	Tri        Tri          `json:"tri"` // 明确三态结果
	Trace      []TraceStep  `json:"trace"`
	Candidates []Candidate  `json:"candidates,omitempty"`
}

// ReplayResult 是一次重放的完整结论。
type ReplayResult struct {
	Property  string   `json:"property"`
	Clock     string   `json:"clock"`
	Tri       Tri      `json:"tri"`
	Threads   []Thread `json:"threads"`
	Semantics string   `json:"semantics"` // 语义版本
	SampledAt []int64  `json:"sampled_at"`
}

// MinInterval 是被缩减移除的区间（与失败无关），时间原点不变。
type MinInterval struct {
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	Reason string `json:"reason"`
}

// MinCex 是最小反例：只保留与失败相关的采样点，时间原点保持。
type MinCex struct {
	Property  string        `json:"property"`
	ThreadID  string        `json:"thread_id"`
	KeptTicks []int         `json:"kept_ticks"`
	KeptTimes []int64       `json:"kept_times"`
	Removed   []MinInterval `json:"removed"`
	Origin    int64         `json:"origin"` // 时间原点，恒为 0
	Justify   string        `json:"justify"`
}

// Event 是机器可读取的判断依据记录。
type Event struct {
	ID       string `json:"id"`
	BranchID string `json:"branch_id"`
	Seq      int    `json:"seq"`
	Kind     string `json:"kind"`
	Payload  string `json:"payload"` // JSON
	Actor    string `json:"actor"`
	Base     string `json:"base"` // 提交时基于的 head event
	Created  string `json:"created"`
}

// Branch 是一条解释分支。
type Branch struct {
	ID      string `json:"id"`
	Parent  string `json:"parent"`
	Name    string `json:"name"`
	Created string `json:"created"`
}

// Source 是一份原始材料（字节不可变）。
type Source struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // vcd | props | clocks
	Name    string `json:"name"`
	Content []byte `json:"content"`
	Created string `json:"created"`
	EventID string `json:"event_id"`
}

// Derived 是派生对象，记录依赖与语义版本；失效只标记不删除。
type Derived struct {
	ID        string   `json:"id"`
	BranchID  string   `json:"branch_id"`
	Kind      string   `json:"kind"` // params | waveform | ast | replay | mincex
	Payload   string   `json:"payload"`
	Deps      []string `json:"deps"`
	Semantics string   `json:"semantics"`
	Status    string   `json:"status"` // valid | stale
	SourceID  string   `json:"source_id"`
	EventID   string   `json:"event_id"`
	Created   string   `json:"created"`
}

// Ruling 是一次人工裁定（接受/否定候选），可撤销。
type Ruling struct {
	ID       string `json:"id"`
	BranchID string `json:"branch_id"`
	TargetID string `json:"target_id"` // 候选或线程 ID
	Decision string `json:"decision"`  // accepted | rejected
	Note     string `json:"note"`
	Actor    string `json:"actor"`
	BaseHead string `json:"base_head"` // 提交时看到的分支 head
	Created  string `json:"created"`
	Reverted string `json:"reverted_by"` // 撤销事件 ID，空为有效
}

// NewID 生成稳定身份：种类前缀 + 随机 hex。身份一旦写入即不再变化。
func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// Now 返回统一时间戳格式。
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
