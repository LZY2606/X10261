// Package vcd 提供四态逻辑值与 VCD 波形解析。
// 原始字节、时标精度与值变化在导入层原样保留；规范化只发生在派生层。
package vcd

import (
	"fmt"
	"strings"
)

// Tri 是三态判定结果：假 / 真 / 未知(X/Z 传播)。
type Tri int8

const (
	False   Tri = 0
	True    Tri = 1
	Unknown Tri = 2
)

func (t Tri) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	default:
		return "unknown"
	}
}

// And/Or/Not 为 Kleene 三态逻辑。
func And(a, b Tri) Tri {
	if a == False || b == False {
		return False
	}
	if a == Unknown || b == Unknown {
		return Unknown
	}
	return True
}

func Or(a, b Tri) Tri {
	if a == True || b == True {
		return True
	}
	if a == Unknown || b == Unknown {
		return Unknown
	}
	return False
}

func Not(a Tri) Tri {
	if a == Unknown {
		return Unknown
	}
	if a == True {
		return False
	}
	return True
}

// Logic 是四态逻辑向量，Chars 按 MSB 在前，每位取 '0','1','x','z'。
type Logic struct {
	Width int    `json:"width"`
	Chars string `json:"chars"`
}

func (l Logic) String() string { return l.Chars }

// ParseConst 解析形如 4'b10xz / 'b1 / 十进制数 的常量。
func ParseConst(tok string) (Logic, error) {
	tok = strings.TrimSpace(strings.ToLower(tok))
	if i := strings.Index(tok, "'b"); i >= 0 {
		width := -1
		if i > 0 {
			var w int
			if _, err := fmt.Sscanf(tok[:i], "%d", &w); err != nil {
				return Logic{}, fmt.Errorf("非法位宽 %q", tok)
			}
			width = w
		}
		digits := tok[i+2:]
		for _, c := range digits {
			if c != '0' && c != '1' && c != 'x' && c != 'z' {
				return Logic{}, fmt.Errorf("非法四态数字 %q", tok)
			}
		}
		if width < 0 {
			width = len(digits)
		}
		return Logic{Width: width, Chars: padLeft(digits, width)}, nil
	}
	var n uint64
	if _, err := fmt.Sscanf(tok, "%d", &n); err != nil {
		return Logic{}, fmt.Errorf("非法常量 %q", tok)
	}
	bits := fmt.Sprintf("%b", n)
	return Logic{Width: len(bits), Chars: bits}, nil
}

func padLeft(s string, w int) string {
	for len(s) < w {
		s = "0" + s
	}
	if len(s) > w {
		s = s[len(s)-w:]
	}
	return s
}

// Scalar 构造单位四态值。
func Scalar(c byte) Logic { return Logic{Width: 1, Chars: string(c)} }

// HasXZ 报告是否含 x/z。
func (l Logic) HasXZ() bool { return strings.ContainsAny(l.Chars, "xz") }

// IsZero 报告是否确定为 0（不含 x/z 且全 0）。
func (l Logic) IsZero() bool { return !l.HasXZ() && !strings.Contains(l.Chars, "1") }

// Truthy 把向量按布尔语境求三态值。
func (l Logic) Truthy() Tri {
	if l.HasXZ() {
		return Unknown
	}
	if l.IsZero() {
		return False
	}
	return True
}

// Eq4 四态相等：双方完全已知时给出确定结果，否则为 Unknown。
func Eq4(a, b Logic) Tri {
	if a.HasXZ() || b.HasXZ() {
		return Unknown
	}
	w := a.Width
	if b.Width > w {
		w = b.Width
	}
	if padLeft(a.Chars, w) == padLeft(b.Chars, w) {
		return True
	}
	return False
}
