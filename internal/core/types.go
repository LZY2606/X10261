// Package core 实现信号言词庭的领域核心：四态逻辑、三态结果、
// 时标单位、VCD 波形、序列属性语言、固定取样语义重放与反例缩减。
package core

// Tri 是三态结果。所有边界情形（X/Z 传播、disable iff、空匹配、
// reset 边界）都必须落到明确的三态结果，不允许悬空。
type Tri int

const (
	TriFalse Tri = iota
	TriTrue
	TriUnknown
)

func (t Tri) String() string {
	switch t {
	case TriTrue:
		return "true"
	case TriFalse:
		return "false"
	default:
		return "unknown"
	}
}

func ParseTri(s string) Tri {
	switch s {
	case "true":
		return TriTrue
	case "false":
		return TriFalse
	default:
		return TriUnknown
	}
}

func And3(a, b Tri) Tri {
	if a == TriFalse || b == TriFalse {
		return TriFalse
	}
	if a == TriUnknown || b == TriUnknown {
		return TriUnknown
	}
	return TriTrue
}

func Or3(a, b Tri) Tri {
	if a == TriTrue || b == TriTrue {
		return TriTrue
	}
	if a == TriUnknown || b == TriUnknown {
		return TriUnknown
	}
	return TriFalse
}

func Not3(a Tri) Tri {
	switch a {
	case TriTrue:
		return TriFalse
	case TriFalse:
		return TriTrue
	default:
		return TriUnknown
	}
}

// FourStateToTri 把原始四态比特串映射为三态值。
// X/Z 传播规则：任何 x/z 比特使布尔语境结果变为 unknown；
// 全 0 为 false，含 1 为 true。原始表示不被改写。
func FourStateToTri(v string) Tri {
	if v == "" {
		return TriUnknown
	}
	hasOne := false
	for _, c := range v {
		switch c {
		case 'x', 'X', 'z', 'Z', '?':
			return TriUnknown
		case '1':
			hasOne = true
		case '0':
		default:
			return TriUnknown
		}
	}
	if hasOne {
		return TriTrue
	}
	return TriFalse
}

// unitToPs 是派生层的规范化表：原始层只保存 {ticks, mag, unit}，
// 单位换算只在派生层发生。无法识别的单位不做转换。
var unitToPs = map[string]int64{
	"ps": 1,
	"ns": 1_000,
	"us": 1_000_000,
	"ms": 1_000_000_000,
	"s":  1_000_000_000_000,
}

// NormalizeTime 把 (ticks * mag) unit 规范化为皮秒。
// 第二个返回值表示是否可转换；不可转换时调用方必须保留原值并附诊断。
func NormalizeTime(ticks int64, mag int, unit string) (int64, bool) {
	if mag <= 0 {
		mag = 1
	}
	f, ok := unitToPs[unit]
	if !ok {
		return 0, false
	}
	return ticks * int64(mag) * f, true
}
