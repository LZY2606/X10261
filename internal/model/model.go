// Package model 定义领域核心类型：四态逻辑值、三态判定结果、
// 波形（保留原始表示与单位基准，派生层才做规范化）。
package model

import "strings"

// Tri 是三态判定结果：TRUE / FALSE / UNKNOWN。
// X/Z 传播、overlapped/non-overlapped implication、disable iff、
// 空匹配都必须落到明确的三态结果上。
type Tri int

const (
	TriFalse Tri = iota
	TriTrue
	TriUnknown
)

func (t Tri) String() string {
	switch t {
	case TriTrue:
		return "TRUE"
	case TriFalse:
		return "FALSE"
	default:
		return "UNKNOWN"
	}
}

func TriNot(a Tri) Tri {
	switch a {
	case TriTrue:
		return TriFalse
	case TriFalse:
		return TriTrue
	default:
		return TriUnknown
	}
}

func TriAnd(a, b Tri) Tri {
	if a == TriFalse || b == TriFalse {
		return TriFalse
	}
	if a == TriTrue && b == TriTrue {
		return TriTrue
	}
	return TriUnknown
}

func TriOr(a, b Tri) Tri {
	if a == TriTrue || b == TriTrue {
		return TriTrue
	}
	if a == TriFalse && b == TriFalse {
		return TriFalse
	}
	return TriUnknown
}

// 四态逻辑位运算，位字符为 '0' '1' 'x' 'z'（大写一律归一化为小写）。
func normBit(b byte) byte {
	switch b {
	case 'X':
		return 'x'
	case 'Z':
		return 'z'
	case '0', '1', 'x', 'z':
		return b
	}
	return 'x'
}

func LNot(a byte) byte {
	switch normBit(a) {
	case '0':
		return '1'
	case '1':
		return '0'
	default:
		return 'x'
	}
}

func LAnd(a, b byte) byte {
	a, b = normBit(a), normBit(b)
	if a == '0' || b == '0' {
		return '0'
	}
	if a == '1' && b == '1' {
		return '1'
	}
	return 'x'
}

func LOr(a, b byte) byte {
	a, b = normBit(a), normBit(b)
	if a == '1' || b == '1' {
		return '1'
	}
	if a == '0' && b == '0' {
		return '0'
	}
	return 'x'
}

// LEq 逐位等价：任一位含 X/Z 则该位为 x。
func LEq(a, b byte) byte {
	a, b = normBit(a), normBit(b)
	if a == 'x' || a == 'z' || b == 'x' || b == 'z' {
		return 'x'
	}
	if a == b {
		return '1'
	}
	return '0'
}

// ValueToTri 把四态值向量归约为三态布尔。
func ValueToTri(v string) Tri {
	if v == "" {
		return TriUnknown
	}
	anyOne := false
	for i := 0; i < len(v); i++ {
		switch normBit(v[i]) {
		case '1':
			anyOne = true
		case 'x', 'z':
			return TriUnknown
		}
	}
	if anyOne {
		return TriTrue
	}
	return TriFalse
}

// ValueEq 四态等价比较（左对齐补零到同宽），含 X/Z 时结果为 UNKNOWN。
func ValueEq(a, b string) Tri {
	for len(a) < len(b) {
		a = "0" + a
	}
	for len(b) < len(a) {
		b = "0" + b
	}
	res := TriTrue
	for i := 0; i < len(a); i++ {
		switch LEq(a[i], b[i]) {
		case 'x':
			res = TriUnknown
		case '0':
			if res == TriTrue {
				res = TriFalse
			}
		}
	}
	return res
}

// Change 是一条原始波形值变化，Time 以源文件自身时标为单位（原始表示）。
type Change struct {
	Time   int64  `json:"time"`
	Signal string `json:"signal"`
	Value  string `json:"value"`
}

// SignalDef 是信号声明，ID 为 VCD 内部标识符。
type SignalDef struct {
	Name  string `json:"name"`
	ID    string `json:"id"`
	Width int    `json:"width"`
}

// Waveform 保留原始表示：TimescaleRaw 为源文件时标原文，
// PsPerUnit 是派生层规范化结果（皮秒/单位），0 表示无法转换。
type Waveform struct {
	SourceID     string      `json:"source_id"`
	TimescaleRaw string      `json:"timescale_raw"`
	PsPerUnit    int64       `json:"ps_per_unit"`
	Signals      []SignalDef `json:"signals"`
	Changes      []Change    `json:"changes"`
}

func (w *Waveform) Width(sig string) int {
	for _, s := range w.Signals {
		if s.Name == sig {
			return s.Width
		}
	}
	return 1
}

// ValueBefore 返回 t 时刻之前（preponed 采样区域）的值；
// 无任何变化时返回全 X。
func (w *Waveform) ValueBefore(sig string, t int64) string {
	val := ""
	for _, c := range w.Changes {
		if c.Signal == sig && c.Time < t {
			val = c.Value
		}
	}
	if val == "" {
		return strings.Repeat("x", w.Width(sig))
	}
	return val
}

// ValueAt 返回 t 时刻（含）的值。
func (w *Waveform) ValueAt(sig string, t int64) string {
	val := ""
	for _, c := range w.Changes {
		if c.Signal == sig && c.Time <= t {
			val = c.Value
		}
	}
	if val == "" {
		return strings.Repeat("x", w.Width(sig))
	}
	return val
}

// Edges 计算时钟边沿时刻（源单位）。posedge：新值为 1 且旧值非 1。
func (w *Waveform) Edges(sig, edge string) []int64 {
	var out []int64
	prev := "x"
	for _, c := range w.Changes {
		if c.Signal != sig {
			continue
		}
		cur := c.Value
		switch edge {
		case "posedge":
			if cur == "1" && prev != "1" {
				out = append(out, c.Time)
			}
		case "negedge":
			if cur == "0" && prev != "0" {
				out = append(out, c.Time)
			}
		}
		prev = cur
	}
	return out
}

// ParseTimescale 把 "1ns" 之类时标规范化为 皮秒/单位。
// 无法转换时返回 ok=false，调用方保留原始表示并附诊断。
func ParseTimescale(raw string) (psPerUnit int64, ok bool) {
	raw = strings.TrimSpace(raw)
	i := 0
	for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	num := int64(0)
	for _, ch := range raw[:i] {
		num = num*10 + int64(ch-'0')
	}
	if num != 1 && num != 10 && num != 100 {
		return 0, false
	}
	units := map[string]int64{
		"fs": 1, "ps": 1000, "ns": 1000_000,
		"us": 1000_000_000, "ms": 1000_000_000_000, "s": 1000_000_000_000_000,
	}
	mult, found := units[strings.ToLower(strings.TrimSpace(raw[i:]))]
	if !found {
		return 0, false
	}
	return num * mult, true
}
