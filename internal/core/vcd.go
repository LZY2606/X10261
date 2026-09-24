package core

import (
	"bufio"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ValueChange 是一条原始波形值变化。Time/Unit 保持导入时的原始表示，
// 任何规范化（皮秒换算）都发生在派生层，不改写这里。
type ValueChange struct {
	Time   int64  `json:"time"`
	Unit   string `json:"unit"`
	Signal string `json:"signal"`
	Value  string `json:"value"` // 原始四态比特串，如 "0","1","x","z","10x1"
}

// Waveform 是一份导入的波形。原始字节由存储层另行保存，这里保存解析视图。
type Waveform struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Version       int           `json:"version"`
	TimescaleMag  int           `json:"timescale_mag"`
	TimescaleUnit string        `json:"timescale_unit"` // "" 表示未知，禁止猜测
	Signals       []string      `json:"signals"`
	Changes       []ValueChange `json:"changes"`
	Diagnostics   []string      `json:"diagnostics"`
	Supersedes    string        `json:"supersedes,omitempty"`
}

// ParseVCD 解析 VCD。解析失败返回错误，调用方负责整体回滚，不留半份记录。
func ParseVCD(name string, data []byte) (*Waveform, error) {
	w := &Waveform{Name: name, Version: 1, TimescaleMag: 1}
	ids := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var curTime int64
	sawTimescale := false
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "$timescale"):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "$timescale"))
			for !strings.Contains(rest, "$end") && sc.Scan() {
				rest += " " + strings.TrimSpace(sc.Text())
			}
			rest = strings.ReplaceAll(rest, "$end", "")
			if f := strings.Fields(rest); len(f) > 0 {
				mag, unit, err := parseTimescale(f[0])
				if err != nil {
					return nil, fmt.Errorf("vcd %s: %v", name, err)
				}
				w.TimescaleMag, w.TimescaleUnit = mag, unit
				sawTimescale = true
			}
		case strings.HasPrefix(line, "$var"):
			f := strings.Fields(line)
			if len(f) < 5 {
				return nil, fmt.Errorf("vcd %s line %d: malformed $var %q", name, lineNo, line)
			}
			ids[f[3]] = f[4]
			w.Signals = append(w.Signals, f[4])
		case strings.HasPrefix(line, "$"):
			// 其他头部命令忽略
		case line[0] == '#':
			t, err := strconv.ParseInt(line[1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd %s line %d: bad timestamp %q", name, lineNo, line)
			}
			curTime = t
		case line[0] == 'b' || line[0] == 'B':
			f := strings.Fields(line)
			if len(f) != 2 {
				return nil, fmt.Errorf("vcd %s line %d: bad vector change %q", name, lineNo, line)
			}
			sig, ok := ids[f[1]]
			if !ok {
				return nil, fmt.Errorf("vcd %s line %d: unknown id code %q", name, lineNo, f[1])
			}
			w.Changes = append(w.Changes, ValueChange{Time: curTime, Signal: sig, Value: strings.ToLower(f[0][1:])})
		default:
			v := line[:1]
			code := strings.TrimSpace(line[1:])
			sig, ok := ids[code]
			if !ok {
				return nil, fmt.Errorf("vcd %s line %d: unknown id code %q", name, lineNo, code)
			}
			w.Changes = append(w.Changes, ValueChange{Time: curTime, Signal: sig, Value: strings.ToLower(v)})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(w.Signals) == 0 {
		return nil, fmt.Errorf("vcd %s: no signals declared", name)
	}
	if !sawTimescale {
		w.TimescaleUnit = ""
		w.Diagnostics = append(w.Diagnostics,
			"missing $timescale: timestamps kept as raw ticks, no unit conversion possible")
	}
	for i := range w.Changes {
		w.Changes[i].Unit = w.TimescaleUnit
	}
	sort.SliceStable(w.Changes, func(i, j int) bool { return w.Changes[i].Time < w.Changes[j].Time })
	return w, nil
}

func parseTimescale(s string) (int, string, error) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i == len(s) {
		return 0, "", fmt.Errorf("bad timescale %q", s)
	}
	mag, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, "", fmt.Errorf("bad timescale %q", s)
	}
	return mag, strings.ToLower(s[i:]), nil
}

// ValueAt 返回 t 时刻（含）之前最后一次变化的值。
func (w *Waveform) ValueAt(signal string, t int64) string {
	best := ""
	for _, c := range w.Changes {
		if c.Signal == signal && c.Time <= t {
			best = c.Value
		}
	}
	return best
}

// sampleEvidence 实现固定取样语义（preponed）：沿时钟边沿采样边沿之前
// 最后一次变化。返回采样值与决定该采样的原始变化时刻（证据链）。
// 没有变化时 changeTime 为 -1，值为 ""。
func (w *Waveform) sampleEvidence(signal string, edge int64) (value string, changeTime int64, ok bool) {
	bestT := int64(-1)
	for _, c := range w.Changes {
		if c.Signal == signal && c.Time < edge && c.Time >= bestT {
			bestT, value, ok = c.Time, c.Value, true
		}
	}
	if !ok {
		for _, c := range w.Changes {
			if c.Signal == signal && c.Time == edge {
				return c.Value, c.Time, true
			}
		}
	}
	return value, bestT, ok
}

// SampleBefore 是 sampleEvidence 的简写形式。
func (w *Waveform) SampleBefore(signal string, edge int64) string {
	v, _, _ := w.sampleEvidence(signal, edge)
	return v
}

// Edges 返回信号的上升沿时刻序列（0->1 跳变）。
func (w *Waveform) Edges(clk string) []int64 {
	var edges []int64
	prev := "0"
	for _, c := range w.Changes {
		if c.Signal != clk {
			continue
		}
		if prev != "1" && c.Value == "1" {
			edges = append(edges, c.Time)
		}
		prev = c.Value
	}
	return edges
}

// ResetIntervals 返回 reset 信号处于有效值的区间 [start, end)。
func (w *Waveform) ResetIntervals(signal, active string) [][2]int64 {
	var iv [][2]int64
	var start int64
	inReset := false
	for _, c := range w.Changes {
		if c.Signal != signal {
			continue
		}
		if c.Value == active && !inReset {
			inReset, start = true, c.Time
		}
		if c.Value != active && inReset {
			inReset = false
			iv = append(iv, [2]int64{start, c.Time})
		}
	}
	if inReset {
		iv = append(iv, [2]int64{start, w.EndTime()})
	}
	return iv
}

// EndTime 是波形中最后一个变化时刻。
func (w *Waveform) EndTime() int64 {
	var end int64
	for _, c := range w.Changes {
		if c.Time > end {
			end = c.Time
		}
	}
	return end
}

// NormTime 把原始时刻规范化为皮秒；不可转换时返回 false。
func (w *Waveform) NormTime(t int64) (int64, bool) {
	return NormalizeTime(t, w.TimescaleMag, w.TimescaleUnit)
}

// ClockDomain 描述采样时钟与 reset 坐标基准。
type ClockDomain struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Signal      string `json:"signal"`
	Edge        string `json:"edge"` // 目前支持 posedge
	ResetSignal string `json:"reset_signal"`
	ResetActive string `json:"reset_active"`
}
