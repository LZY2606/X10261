// Package vcd 解析 VCD 波形。解析是派生层行为：原始字节由调用方保存在
// sources 表，这里只产出解释结果；时标精度与四态值原样保留。
package vcd

import (
	"fmt"
	"strconv"
	"strings"

	"signalcourt/internal/model"
)

// Parse 解析 VCD 文本为 Waveform。无法识别的行返回诊断错误，
// 调用方决定整体回滚（导入失败不留下半份记录）。
func Parse(text string) (*model.Waveform, error) {
	w := &model.Waveform{TimescaleNum: 1, TimescaleUnit: "ns"}
	ids := map[string]int{} // vcd 短码 -> signal index
	var curTime int64
	inHeader := true

	lines := strings.Split(text, "\n")
	for ln, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if inHeader {
			switch {
			case strings.HasPrefix(line, "$timescale"):
				// 形如 "$timescale 1ns $end" 或跨行
				body := strings.TrimSuffix(strings.TrimPrefix(line, "$timescale"), "$end")
				body = strings.TrimSpace(body)
				if body == "" {
					// 时标在下一行
					for ln+1 < len(lines) {
						ln++
						body = strings.TrimSpace(lines[ln])
						if body != "" && !strings.HasPrefix(body, "$") {
							break
						}
					}
				}
				num, unit, err := parseTimescale(body)
				if err != nil {
					return nil, fmt.Errorf("vcd 第%d行: %w", ln+1, err)
				}
				w.TimescaleNum, w.TimescaleUnit = num, unit
			case strings.HasPrefix(line, "$var"):
				// $var wire 1 ! clk $end
				f := strings.Fields(line)
				if len(f) < 5 {
					return nil, fmt.Errorf("vcd 第%d行: $var 字段不足", ln+1)
				}
				width, err := strconv.Atoi(f[2])
				if err != nil {
					return nil, fmt.Errorf("vcd 第%d行: 位宽非法: %v", ln+1, f[2])
				}
				code := f[3]
				name := f[4]
				ids[code] = len(w.Signals)
				w.Signals = append(w.Signals, model.Signal{
					ID: fmt.Sprintf("sig_%s", name), Name: name, Width: width,
				})
			case strings.HasPrefix(line, "$enddefinitions"):
				inHeader = false
			case strings.HasPrefix(line, "$"):
				// 其它头指令忽略（$scope/$date 等），不丢失：原始字节仍在 sources
			}
			continue
		}
		// 值变化区
		switch {
		case line[0] == '#':
			t, err := strconv.ParseInt(line[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd 第%d行: 时标非法: %q", ln+1, line)
			}
			curTime = t
			if t > w.EndTime {
				w.EndTime = t
			}
		case line[0] == '0' || line[0] == '1' || line[0] == 'x' || line[0] == 'X' ||
			line[0] == 'z' || line[0] == 'Z':
			val := strings.ToLower(string(line[0]))
			code := strings.TrimSpace(line[1:])
			idx, ok := ids[code]
			if !ok {
				return nil, fmt.Errorf("vcd 第%d行: 未声明的信号码 %q", ln+1, code)
			}
			w.Signals[idx].Changes = append(w.Signals[idx].Changes,
				model.ValueChange{Time: curTime, Raw: val, Value: val})
		case line[0] == 'b' || line[0] == 'B':
			f := strings.Fields(line)
			if len(f) != 2 {
				return nil, fmt.Errorf("vcd 第%d行: 向量变化格式非法: %q", ln+1, line)
			}
			val := strings.ToLower(f[0][1:])
			idx, ok := ids[f[1]]
			if !ok {
				return nil, fmt.Errorf("vcd 第%d行: 未声明的信号码 %q", ln+1, f[1])
			}
			w.Signals[idx].Changes = append(w.Signals[idx].Changes,
				model.ValueChange{Time: curTime, Raw: "b" + f[0][1:], Value: val})
		case strings.HasPrefix(line, "$"):
			// $dumpvars 等块标记忽略
		default:
			return nil, fmt.Errorf("vcd 第%d行: 无法识别的行 %q", ln+1, line)
		}
	}
	return w, nil
}

func parseTimescale(body string) (int64, string, error) {
	body = strings.TrimSpace(body)
	i := 0
	for i < len(body) && body[i] >= '0' && body[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, "", fmt.Errorf("时标缺少数值: %q", body)
	}
	num, err := strconv.ParseInt(body[:i], 10, 64)
	if err != nil {
		return 0, "", err
	}
	unit := strings.TrimSpace(body[i:])
	switch unit {
	case "s", "ms", "us", "ns", "ps", "fs":
	default:
		return 0, "", fmt.Errorf("时标单位非法: %q", unit)
	}
	return num, unit, nil
}
