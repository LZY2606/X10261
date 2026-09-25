// Package sem replays parsed properties over merged VCD waveforms under a
// fixed sampled (clocked) semantics. It models four-state sampling, three
// state verdicts, reset/disable boundaries, overlapping and non-overlapping
// implication, vacuous matches and multiple concurrent threads.
package sem

import (
	"sort"

	"signalcourt/internal/logic"
)

// UnitFS documents the canonical time base: every stored time is an absolute
// int64 femtosecond count and is never shifted so the time origin is stable.
const UnitFS int64 = 1

// Point is one raw change of one signal. RawTime/RawValue are the exact source
// tokens; TimeFS is derived. Src identifies the originating VCD source.
type Point struct {
	Src       string      `json:"src"`
	SrcLine   int         `json:"src_line"`
	Name      string      `json:"name"`
	Bit       int         `json:"bit"`
	RawTime   string      `json:"raw_time"`
	RawValue  string      `json:"raw_value"`
	TimeFS    int64       `json:"time_fs"`
	Value     logic.Value `json:"value"`
	Decodable bool        `json:"decodable"`
	Note      string      `json:"note,omitempty"`
}

// SignalView is the merged change stream of one sampled bit.
type SignalView struct {
	Name   string  `json:"name"`
	Bit    int     `json:"bit"`
	Width  int     `json:"width"`
	Points []Point `json:"points"`
}

// Wave is the merged, time-ordered material under replay.
type Wave struct {
	Signals map[string]*SignalView `json:"-"`
	Order   []string               `json:"order"` // stable insertion order name|bit
}

func NewWave() *Wave {
	return &Wave{Signals: map[string]*SignalView{}}
}

// Key is the stable identity of one sampled bit.
func Key(name string, bit int) string {
	if bit == 0 {
		return name
	}
	return name + "#" + itoa(bit)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// AddPoint merges one point, keeping every source change (no destructive
// cleaning). Equal-time changes from distinct sources are all retained in
// source order; the effective value resolves them.
func (w *Wave) AddPoint(p Point) {
	k := Key(p.Name, p.Bit)
	sv := w.Signals[k]
	if sv == nil {
		sv = &SignalView{Name: p.Name, Bit: p.Bit}
		w.Signals[k] = sv
		w.Order = append(w.Order, k)
	}
	sv.Points = append(sv.Points, p)
	sort.SliceStable(sv.Points, func(i, j int) bool {
		if sv.Points[i].TimeFS != sv.Points[j].TimeFS {
			return sv.Points[i].TimeFS < sv.Points[j].TimeFS
		}
		if sv.Points[i].Src != sv.Points[j].Src {
			return sv.Points[i].Src < sv.Points[j].Src
		}
		return sv.Points[i].SrcLine < sv.Points[j].SrcLine
	})
}

// EffectiveAt resolves the value of a sampled bit at absolute time t (value
// of the last change with TimeFS <= t). ok=false means the bit has no value
// yet (treated as unknown). The index of the winning point is returned so
// verdicts keep an exact provenance chain.
func (w *Wave) EffectiveAt(k string, t int64) (logic.Bit, Point, bool) {
	sv := w.Signals[k]
	if sv == nil || len(sv.Points) == 0 {
		return logic.BX, Point{}, false
	}
	// Binary search: last point with TimeFS <= t.
	idx := sort.Search(len(sv.Points), func(i int) bool { return sv.Points[i].TimeFS > t }) - 1
	if idx < 0 {
		return logic.BX, Point{}, false
	}
	// Resolve same-time drivers (all points at the winning timestamp).
	win := sv.Points[idx]
	res := bitOf(win)
	for j := idx - 1; j >= 0 && sv.Points[j].TimeFS == win.TimeFS; j-- {
		res = logic.Resolve(bitOf(sv.Points[j]), res)
	}
	return res, win, true
}

// ValueAt samples a key as a verdict at time t.
func (w *Wave) ValueAt(k string, t int64) logic.Trit {
	b, _, _ := w.EffectiveAt(k, t)
	return logic.Sample(b)
}

func bitOf(p Point) logic.Bit {
	if !p.Value.Scalar {
		if p.Value.Vec != nil && p.Bit < len(p.Value.Vec.Bits) {
			// VCD LSB index: stored bit n maps to vector position width-1-n.
			pos := len(p.Value.Vec.Bits) - 1 - p.Bit
			if pos >= 0 {
				return p.Value.Vec.Bits[pos]
			}
		}
		return logic.BX
	}
	return p.Value.Bit
}

// ClockDomain describes how a clock edge grid is derived.
type ClockDomain struct {
	Clock   string `json:"clock"`
	Edge    string `json:"edge"` // pos / neg / any
	MinFS   int64  `json:"min_fs"`
	MaxFS   int64  `json:"max_fs"`
}

// ResetInterval is one [StartFS,EndFS] inclusive-ish range where reset is
// active. Source is "signal" (derived from waveform) or "manual".
type ResetInterval struct {
	StartFS int64  `json:"start_fs"`
	EndFS   int64  `json:"end_fs"`
	Active  bool   `json:"active"`
	Source  string `json:"source"`
	Note    string `json:"note,omitempty"`
}

// ResetSpec derives reset intervals from a level signal or explicit ranges.
type ResetSpec struct {
	Signal   string          `json:"signal,omitempty"`
	ActiveHigh bool          `json:"active_high"`
	Manual   []ResetInterval `json:"manual,omitempty"`
}

// Config is the replay parameter set. Versions matter: semantic version
// changes rule interpretation; parameters change horizon etc. and invalidate
// only dependents.
type Config struct {
	Clock     ClockDomain `json:"clock"`
	Reset     ResetSpec   `json:"reset"`
	SemVer    string      `json:"sem_ver"`
	Horizon   int         `json:"horizon"`     // max cycles a thread may live
	ParamRev  int         `json:"param_rev"`  // parameter revision number
}

// Region labels the three fixed sampling regions of every clock tick.
type Region string

const (
	RPreponed  Region = "preponed"
	RObserved  Region = "observed"
	RPostponed Region = "postponed"
)

// Tick is one active clock edge. All three sampling regions read at the exact
// edge time under the fixed semantics; preponed is the read-before-edge
// snapshot (values strictly below the edge), observed/postponed read the value
// at/after the edge. We record both and their winning point provenance.
type Tick struct {
	Index  int   `json:"index"`
	EdgeFS int64 `json:"edge_fs"`
	// Preponed uses strict-less time; Observed uses <= edge.
	PreKey string `json:"pre_key"`
}

// EdgeGrid builds active clock-edge ticks from the merged wave.
func (w *Wave) EdgeGrid(c ClockDomain) []Tick {
	sv := w.Signals[c.Clock]
	if sv == nil {
		return nil
	}
	var ticks []Tick
	prev := logic.BX
	idx := 0
	// Consider every change point of the clock as a candidate edge.
	for _, p := range sv.Points {
		cur := bitOf(p)
		edge := false
		switch c.Edge {
		case "pos":
			edge = prev == logic.B0 && cur == logic.B1
		case "neg":
			edge = prev == logic.B1 && cur == logic.B0
		default:
			edge = (prev == logic.B0 && cur == logic.B1) || (prev == logic.B1 && cur == logic.B0)
		}
		if edge {
			if (c.MinFS == 0 || p.TimeFS >= c.MinFS) && (c.MaxFS == 0 || p.TimeFS <= c.MaxFS) {
				ticks = append(ticks, Tick{Index: idx, EdgeFS: p.TimeFS, PreKey: p.RawTime})
				idx++
			}
		}
		if cur != logic.BZ { // Z clock edge transitions are not definite
			prev = cur
		}
	}
	return ticks
}
