// Package casepack 是仓库自带的离线案例包"信号言词庭：HDL 波形断言、
// 取样区域与最小失败轨迹"。它提供领域材料（VCD 波形、时钟域、序列属性），
// 以及叙事步骤：初始导入、参数修订、局部源数据改动、冲突人工决定。
package casepack

import (
	"fmt"
	"sort"
	"strings"
)

// Properties 是案例的属性集（小门序列属性语言）。
// 覆盖：disable iff、空匹配 [*0]、X/Z 传播、overlapped(|->) 与
// non-overlapped(|=>) implication。
const Properties = `// 信号言词庭 属性集：固定取样语义（posedge clk，采样边沿前的值）
property p_disable;
  disable iff (!reset_n)
  req |-> ##2 ack
endproperty

property p_empty;
  req |-> ack[*0] ##1 done
endproperty

property p_xz;
  data |-> ##1 done
endproperty

property p_ov;
  req |-> ##1 ack
endproperty

property p_nov;
  req |=> ##1 ack
endproperty
`

type change struct {
	t   int64
	sig string
	v   string
}

// buildVCD 生成案例主波形。unit/factor 控制时标精度；
// ackFix 是"局部源数据改动"：ack 在 52 追加一次 0->1；
// withTimescale=false 时省略 $timescale（未知单位，留在原位置并附诊断）。
func buildVCD(unit string, factor int64, ackFix, withTimescale bool) []byte {
	var b strings.Builder
	if withTimescale {
		fmt.Fprintf(&b, "$timescale 1%s $end\n", unit)
	}
	b.WriteString("$scope module top $end\n")
	codes := map[string]string{
		"clk": "c", "reset_n": "n", "rst": "r", "req": "q",
		"ack": "a", "done": "d", "data": "k",
	}
	names := []string{"clk", "reset_n", "rst", "req", "ack", "done", "data"}
	for _, n := range names {
		fmt.Fprintf(&b, "$var wire 1 %s %s $end\n", codes[n], n)
	}
	b.WriteString("$upscope $end\n$enddefinitions $end\n")

	var changes []change
	add := func(t int64, sig, v string) { changes = append(changes, change{t, sig, v}) }
	// 时钟：周期 10，占空 50%，上升沿在 5,15,...,195（坐标基准）
	for t := int64(0); t <= 200; t += 5 {
		v := "0"
		if (t/5)%2 == 1 {
			v = "1"
		}
		add(t, "clk", v)
	}
	// reset_n：上电复位，12 释放（disable iff 的边界）
	add(0, "reset_n", "0")
	add(12, "reset_n", "1")
	// rst：窗口中段的一次复位脉冲 [100,110)，制造 reset 边界
	add(0, "rst", "0")
	add(100, "rst", "1")
	add(110, "rst", "0")
	// req：reset 期间一次脉冲 + 连续多拍高（多线程并存）+ 复位前一次脉冲
	add(0, "req", "0")
	add(2, "req", "1")
	add(6, "req", "0")
	add(16, "req", "1")
	add(60, "req", "0")
	add(80, "req", "1")
	add(90, "req", "0")
	// ack：40 拉高，50 拉低（制造失败线程）；ackFix 在 52 再拉高
	add(0, "ack", "0")
	add(40, "ack", "1")
	add(50, "ack", "0")
	if ackFix {
		add(52, "ack", "1")
	}
	// done
	add(0, "done", "0")
	add(30, "done", "1")
	add(120, "done", "0")
	add(150, "done", "1")
	// data：四态逻辑——x 区间 [20,30)，z 区间 [70,80)
	add(0, "data", "0")
	add(20, "data", "x")
	add(30, "data", "0")
	add(70, "data", "z")
	add(80, "data", "1")

	sort.SliceStable(changes, func(i, j int) bool { return changes[i].t < changes[j].t })
	curT := int64(-1)
	for _, c := range changes {
		t := c.t * factor
		if t != curT {
			fmt.Fprintf(&b, "#%d\n", t)
			curT = t
		}
		fmt.Fprintf(&b, "%s%s\n", c.v, codes[c.sig])
	}
	return []byte(b.String())
}

// MainVCD 主波形，时标 1ns。
func MainVCD() []byte { return buildVCD("ns", 1, false, true) }

// MainVCDRevised 局部源数据改动后的主波形（ack 52ns 追加 0->1）。
func MainVCDRevised() []byte { return buildVCD("ns", 1, true, true) }

// MainVCDps 同一逻辑场景、时标 1ps（不同时标精度）。
func MainVCDps() []byte { return buildVCD("ps", 1000, false, true) }

// AuxVCD 无 $timescale 的辅助波形：无法转换的值留在原位置并附诊断。
func AuxVCD() []byte {
	return []byte(`$scope module aux $end
$var wire 1 c clk $end
$var wire 1 s sig_a $end
$upscope $end
$enddefinitions $end
#0
0c
0s
#5
1c
1s
#10
0c
#15
1c
zs
#20
0c
xs
#25
1c
0s
`)
}
